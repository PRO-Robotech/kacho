// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/db/pgfault"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/storagebackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// StorageBackendRepo — хранилище зарегистрированных бэкендов плоскости данных
// (миграция 0015). Ресурс живёт ТОЛЬКО на внутреннем листенере: и координата, и
// перечень зон, и вид — сведения о физике, которым на публичной поверхности не место.
//
// Секрет здесь не хранится и хранится не будет: колонка несёт ССЫЛКУ на учётные
// данные, форму которой держит домен (domain.CredentialsRef). Строка таблицы
// переживает ротацию ключа и уезжает в каждую резервную копию — материал, попавший
// в неё однажды, оттуда уже не отзывается.
type StorageBackendRepo struct {
	pool *pgxpool.Pool
}

// NewStorageBackendRepo создаёт StorageBackendRepo поверх pgxpool.
func NewStorageBackendRepo(pool *pgxpool.Pool) *StorageBackendRepo {
	return &StorageBackendRepo{pool: pool}
}

// StorageBackendUpdate — набор ИЗМЕНЯЕМЫХ полей бэкенда: nil-указатель означает «не
// менять», а не «обнулить». Форма выбрана под маску правки — поле, названное маской,
// приезжает указателем, — поэтому провязка маски ложится на репозиторий без второй
// переделки SQL.
//
// Вида бэкенда здесь НЕТ, и это не пропуск. Смена вида означала бы, что уже
// созданные объекты продолжают лежать в хранилище прежней технологии, а мы адресуем
// их адаптером другой — то есть данные становятся недостижимыми молча. Поле,
// отсутствующее в наборе, не пишется ни одним стейтментом дерева; проверка в коде
// такого свойства не даёт — её обходит следующий стейтмент.
// Набор изменяемых полей объявлен в ПАКЕТЕ USE-CASE: порт принадлежит тому, кто им
// пользуется, а не адаптеру. Здесь только псевдоним, чтобы прочие места файла
// читались короче.
type StorageBackendUpdate = storagebackend.Update

// storageBackendCols — ЕДИНСТВЕННЫЙ список колонок бэкенда. Один список на чтение,
// вставку и правку: разъехавшись, они дают поле, живущее только в одном из путей, и
// расходится это молча.
const storageBackendCols = `id, name, kind, description, zone_ids, endpoint,
	credentials_ref, status, created_at, updated_at`

// Имена DB-constraint'ов бэкенда (миграция 0015). PK и inline-FK именуются
// Postgres'ом, остальные — как в CREATE.
const (
	cnStorageBackendPK       = "storage_backends_pkey"
	cnStorageBackendNameUniq = "storage_backends_name_uniq"
)

// scanStorageBackend читает строку storageBackendCols в domain.StorageBackend.
func scanStorageBackend(s rowScanner) (*domain.StorageBackend, error) {
	var (
		b           domain.StorageBackend
		kind        string
		credentials string
		status      string
		zoneIDsJSON []byte
	)
	if err := s.Scan(
		&b.ID, &b.Name, &kind, &b.Description, &zoneIDsJSON, &b.Endpoint,
		&credentials, &status, &b.CreatedAt, &b.UpdatedAt,
	); err != nil {
		return nil, err
	}
	b.Kind = domain.BackendKind(kind)
	b.CredentialsRef = domain.CredentialsRef(credentials)
	b.Status = domain.BackendStatus(status)
	if len(zoneIDsJSON) > 0 {
		if err := json.Unmarshal(zoneIDsJSON, &b.ZoneIDs); err != nil {
			return nil, err
		}
	}
	return &b, nil
}

// Get возвращает бэкенд по id. Отсутствует → NotFound контрактного тона.
func (r *StorageBackendRepo) Get(ctx context.Context, id string) (*domain.StorageBackend, error) {
	b, err := scanStorageBackend(r.pool.QueryRow(ctx,
		`SELECT `+storageBackendCols+` FROM storage_backends WHERE id = $1`, id))
	if err != nil {
		return nil, mapStorageBackendErr(err, sbErrCtx{backendID: id})
	}
	return b, nil
}

// List возвращает страницу бэкендов курсором (created_at, id) ASC.
//
// pageSize приходит УЖЕ нормализованным (validate.PageSize на стороне use-case,
// 0→50, потолок 1000): второй нормализатор здесь разошёлся бы с первым молча —
// разошёлся бы именно там, где расхождение не видно, потому что на законном входе
// оба отвечают одинаково.
func (r *StorageBackendRepo) List(ctx context.Context, pageSize int64, pageToken string) ([]*domain.StorageBackend, string, error) {
	var (
		conds []string
		args  []any
	)
	if pageToken != "" {
		cur, derr := decodePageToken(pageToken)
		if derr != nil {
			return nil, "", derr
		}
		args = append(args, cur.createdAt, cur.id)
		conds = append(conds, fmt.Sprintf("(created_at, id) > ($%d, $%d)", len(args)-1, len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, pageSize+1)
	q := fmt.Sprintf(`SELECT %s FROM storage_backends%s ORDER BY created_at ASC, id ASC LIMIT $%d`,
		storageBackendCols, where, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", mapStorageBackendErr(err, sbErrCtx{})
	}
	defer rows.Close()
	var out []*domain.StorageBackend
	for rows.Next() {
		b, serr := scanStorageBackend(rows)
		if serr != nil {
			return nil, "", mapStorageBackendErr(serr, sbErrCtx{})
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, "", mapStorageBackendErr(err, sbErrCtx{})
	}
	var next string
	if int64(len(out)) > pageSize {
		last := out[pageSize-1]
		next = encodePageToken(cursor{createdAt: last.CreatedAt, id: last.ID})
		out = out[:pageSize]
	}
	return out, next, nil
}

// Insert регистрирует бэкенд. Вид и состояние обращения идут в БД КАК НАЗВАНЫ:
// незаполненное поле репозиторий не досочиняет, а словарь держит CHECK миграции —
// подставь репозиторий 'ACTIVE' сам, и опечатка администратора завела бы бэкенд
// принимающим новые привязки. Дубль id или имени → 23505 → AlreadyExists.
func (r *StorageBackendRepo) Insert(ctx context.Context, b *domain.StorageBackend) (*domain.StorageBackend, error) {
	zoneIDs, err := json.Marshal(nonNilSlice(b.ZoneIDs))
	if err != nil {
		return nil, storageerr.ErrInternal
	}
	created, err := scanStorageBackend(r.pool.QueryRow(ctx,
		`INSERT INTO storage_backends
		   (id, name, kind, description, zone_ids, endpoint, credentials_ref, status)
		 VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8)
		 RETURNING `+storageBackendCols,
		b.ID, b.Name, string(b.Kind), b.Description, zoneIDs,
		b.Endpoint, string(b.CredentialsRef), string(b.Status)))
	if err != nil {
		return nil, mapStorageBackendErr(err, sbErrCtx{backendID: b.ID, backendName: b.Name})
	}
	return created, nil
}

// Update применяет НАЗВАННЫЕ поля одним стейтментом: nil-указатель → COALESCE
// оставляет колонку как есть. Пустая строка и пустой список — законные ЗНАЧЕНИЯ
// (NULL'ом является только отсутствие указателя), поэтому «снять описание» и «не
// трогать описание» различимы.
//
// Момент изменения двигается ТОЛЬКО когда правка что-то назвала: строка, которой
// никто не касался, не вправе выглядеть изменённой — иначе сверщик и оператор
// читают следом за правкой чужую свежесть. Условие считает те же параметры, что
// пишут колонки, поэтому «назвала» здесь означает ровно то же, что и в SET.
// 0 rows → NotFound.
func (r *StorageBackendRepo) Update(ctx context.Context, id string, u StorageBackendUpdate) (*domain.StorageBackend, error) {
	var zoneJSON []byte
	if u.ZoneIDs != nil {
		raw, err := json.Marshal(nonNilSlice(*u.ZoneIDs))
		if err != nil {
			return nil, storageerr.ErrInternal
		}
		zoneJSON = raw
	}
	updated, err := scanStorageBackend(r.pool.QueryRow(ctx,
		`UPDATE storage_backends SET
		    name            = COALESCE($2::text, name),
		    description     = COALESCE($3::text, description),
		    zone_ids        = COALESCE($4::jsonb, zone_ids),
		    endpoint        = COALESCE($5::text, endpoint),
		    credentials_ref = COALESCE($6::text, credentials_ref),
		    status          = COALESCE($7::text, status),
		    updated_at      = CASE
		                        WHEN num_nonnulls($2::text,$3::text,$4::jsonb,
		                                          $5::text,$6::text,$7::text) > 0
		                        THEN now() ELSE updated_at END
		  WHERE id = $1
		 RETURNING `+storageBackendCols,
		id, u.Name, u.Description, zoneJSON, u.Endpoint,
		strPtr(u.CredentialsRef), strPtr(u.Status)))
	if err != nil {
		return nil, mapStorageBackendErr(err, sbErrCtx{backendID: id, backendName: derefStr(u.Name)})
	}
	return updated, nil
}

// Delete снимает регистрацию бэкенда. Бэкенд, на который ссылается ревизия привязки,
// не удаляется: держит ОГРАНИЧИТЕЛЬНАЯ внешняя связь (0015), а не счётчик в коде —
// программный подсчёт ссылок здесь был бы «прочитал → проверил → удалил», между
// шагами которого проходит конкурентная регистрация. 0 rows → NotFound.
func (r *StorageBackendRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM storage_backends WHERE id = $1`, id)
	if err != nil {
		return mapStorageBackendErr(err, sbErrCtx{backendID: id})
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: StorageBackend %s not found", storageerr.ErrNotFound, id)
	}
	return nil
}

// sbErrCtx — контекстные hint'ы для constraint-aware маппинга ошибок бэкенда.
type sbErrCtx struct {
	backendID   string
	backendName string
}

// mapStorageBackendErr транслирует pgx/pgconn-ошибку в чистый sentinel с
// контрактным текстом Kachō. Сырой pgx/SQL наружу не течёт: неклассифицированный
// SQLSTATE → storageerr.ErrInternal (фиксированный "internal error" на границе), сам
// код при этом логируется — след оператору остаётся, вызывающему не достаётся.
//
// Значение credentials_ref в текст НЕ попадает ни одной веткой: поле заведено ради
// того, чтобы секрет не оказался в БД, и отправить его в журнал отказом значило бы
// отдать ровно то, что защищали.
func mapStorageBackendErr(err error, c sbErrCtx) error {
	if err == nil {
		return nil
	}
	// Уже замапленный sentinel пробрасывается как есть — иначе default-ветка ниже
	// схлопнула бы его в ErrInternal, потеряв контрактный текст.
	switch {
	case errors.Is(err, storageerr.ErrNotFound), errors.Is(err, storageerr.ErrAlreadyExists),
		errors.Is(err, storageerr.ErrFailedPrecondition), errors.Is(err, storageerr.ErrInvalidArg),
		errors.Is(err, storageerr.ErrInternal):
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: StorageBackend %s not found", storageerr.ErrNotFound, c.backendID)
	}
	f := pgfault.Classify(err)
	if f.FromDatabase() {
		switch f.Class {
		case pgfault.Unique:
			switch f.Constraint {
			case cnStorageBackendPK:
				return fmt.Errorf("%w: StorageBackend %s already exists", storageerr.ErrAlreadyExists, c.backendID)
			case cnStorageBackendNameUniq:
				return fmt.Errorf("%w: storage backend with name %s already exists",
					storageerr.ErrAlreadyExists, c.backendName)
			}
			return fmt.Errorf("%w: storage backend already exists", storageerr.ErrAlreadyExists)
		case pgfault.ForeignKey: // ссылка ревизии привязки (RESTRICT)
			if f.Constraint == cnBindingBackendFK {
				return fmt.Errorf("%w: StorageBackend %s is in use", storageerr.ErrFailedPrecondition, c.backendID)
			}
			return fmt.Errorf("%w: storage backend violates a reference constraint", storageerr.ErrFailedPrecondition)
		case pgfault.Check: // вид, состояние, обязательные координата и ссылка
			return fmt.Errorf("%w: Illegal argument", storageerr.ErrInvalidArg)
		}
		slog.Error("uncategorized postgres error mapped to internal",
			append([]any{"storage_backend_id", c.backendID}, f.LogAttrs()...)...)
		return storageerr.ErrInternal
	}
	slog.Error("uncategorized db error mapped to internal", "err", err.Error(), "storage_backend_id", c.backendID)
	return storageerr.ErrInternal
}

// Соответствие порту заперто на этапе компиляции: разошедшись, адаптер и порт иначе
// разъехались бы молча — до первой сборки композиционного корня.
var _ storagebackend.Repo = (*StorageBackendRepo)(nil)
