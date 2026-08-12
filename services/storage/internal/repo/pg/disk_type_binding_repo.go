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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktypebinding"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// DiskTypeBindingRepo — хранилище НЕИЗМЕНЯЕМЫХ ревизий привязки класса диска к
// бэкенду в одной зоне (миграция 0015). Ресурс только внутреннего листенера.
//
// # Почему у этого репозитория нет правки
//
// Ресурс ссылается на ту ревизию, под которой создан. Пока строка ревизии не
// меняется, правка каталога физически не может задним числом изменить свойства уже
// созданного тома: джойн к ревизии не может уехать, потому что цель неизменяема.
// Отсюда состав методов — чтение, список, регистрация, — и ОТСУТСТВИЕ обновления
// здесь не пробел, а сам механизм. Держится это гейтом
// (TestDiskTypeBindingRepoHasNoMutatingPath), а не этим абзацем: абзац переживёт
// первый же добавленный метод.
//
// Единственный переход состояния — вытеснение прежней ревизии новой — исполняется
// ВНУТРИ регистрации, одним стейтментом с вставкой (см. Register). Отдельного пути,
// которым можно было бы перевести ревизию в другое состояние, нет: он немедленно стал
// бы тем самым обновлением, ради отсутствия которого всё и устроено.
//
// Удаления тоже нет. Ревизия живёт, пока на неё ссылаются, и это держат
// ОГРАНИЧИТЕЛЬНЫЕ внешние связи ресурсов (0017), а не воздержание вызывающего.
type DiskTypeBindingRepo struct {
	pool *pgxpool.Pool
}

// NewDiskTypeBindingRepo создаёт DiskTypeBindingRepo поверх pgxpool.
func NewDiskTypeBindingRepo(pool *pgxpool.Pool) *DiskTypeBindingRepo {
	return &DiskTypeBindingRepo{pool: pool}
}

// diskTypeBindingCols — ЕДИНСТВЕННЫЙ список колонок ревизии: один и тот же на
// чтение, список и возврат регистрации.
const diskTypeBindingCols = `id, disk_type_id, zone_id, backend_id, revision, pool,
	namespace_template, cap_snapshots, cap_clone_from_snapshot, cap_clone_from_image,
	cap_clone_keeps_parent, cap_online_grow, cap_multi_attach, cap_encryption_at_rest,
	trash_ttl_seconds, qos, status, created_at`

// Имена DB-constraint'ов ревизии (миграция 0015). PK и inline-FK именуются
// Postgres'ом как <table>_<column>_fkey, частичные индексы — как в CREATE.
const (
	cnBindingPK           = "disk_type_bindings_pkey"
	cnBindingActiveUniq   = "disk_type_bindings_active_uniq"
	cnBindingRevisionUniq = "disk_type_bindings_revision_uniq"
	cnBindingDiskTypeFK   = "disk_type_bindings_disk_type_id_fkey"
	cnBindingBackendFK    = "disk_type_bindings_backend_id_fkey"
)

// bindingQoSDoc — форма документа в колонке qos.
//
// Ключи НАМЕРЕННО не совпадают с формой обмена (camelCase на проводе): это разные
// предметы. Документ лежит в строке, которая не редактируется никогда, поэтому смена
// формы обмена не вправе переписывать историю — совпади они, правка контракта молча
// сделала бы уже записанные ревизии нечитаемыми либо потребовала бы миграции по
// каждой из них.
//
// Пропуск ключа и ноль означают одно и то же — «не объявлено» (domain.BindingQoS),
// поэтому нулевые значения не пишутся: документ остаётся ровно перечнем объявленного.
type bindingQoSDoc struct {
	BaselineIOPS int64 `json:"baseline_iops,omitempty"`
	IOPSPerGiB   int64 `json:"iops_per_gib,omitempty"`
	MaxIOPS      int64 `json:"max_iops,omitempty"`

	BaselineThroughputMiBps int64   `json:"baseline_throughput_mibps,omitempty"`
	ThroughputPerGiBMiBps   float64 `json:"throughput_per_gib_mibps,omitempty"`
	MaxThroughputMiBps      int64   `json:"max_throughput_mibps,omitempty"`
}

// scanDiskTypeBinding читает строку diskTypeBindingCols в domain.DiskTypeBinding.
func scanDiskTypeBinding(s rowScanner) (*domain.DiskTypeBinding, error) {
	var (
		b       domain.DiskTypeBinding
		status  string
		qosJSON []byte
	)
	if err := s.Scan(
		&b.ID, &b.DiskTypeID, &b.ZoneID, &b.BackendID, &b.Revision,
		&b.Locator.Pool, &b.Locator.NamespaceTemplate,
		&b.Capabilities.Snapshots, &b.Capabilities.CloneFromSnapshot,
		&b.Capabilities.CloneFromImage, &b.Capabilities.CloneKeepsParent,
		&b.Capabilities.OnlineGrow, &b.Capabilities.MultiAttach,
		&b.Capabilities.EncryptionAtRest, &b.Capabilities.TrashTTLSeconds,
		&qosJSON, &status, &b.CreatedAt,
	); err != nil {
		return nil, err
	}
	b.Status = domain.BindingStatus(status)
	if len(qosJSON) > 0 {
		var doc bindingQoSDoc
		if err := json.Unmarshal(qosJSON, &doc); err != nil {
			return nil, err
		}
		b.QoS = domain.BindingQoS{
			BaselineIOPS:            doc.BaselineIOPS,
			IOPSPerGiB:              doc.IOPSPerGiB,
			MaxIOPS:                 doc.MaxIOPS,
			BaselineThroughputMiBps: doc.BaselineThroughputMiBps,
			ThroughputPerGiBMiBps:   doc.ThroughputPerGiBMiBps,
			MaxThroughputMiBps:      doc.MaxThroughputMiBps,
		}
	}
	return &b, nil
}

// Get возвращает ревизию по id — и действующую, и вытесненную: вытесненная описывает
// то, что обещали созданным под ней ресурсам, и читать её надо ровно затем.
// Отсутствует → NotFound контрактного тона.
func (r *DiskTypeBindingRepo) Get(ctx context.Context, id string) (*domain.DiskTypeBinding, error) {
	b, err := scanDiskTypeBinding(r.pool.QueryRow(ctx,
		`SELECT `+diskTypeBindingCols+` FROM disk_type_bindings WHERE id = $1`, id))
	if err != nil {
		return nil, mapDiskTypeBindingErr(err, dtbErrCtx{bindingID: id})
	}
	return b, nil
}

// List возвращает страницу ревизий курсором (created_at, id) ASC.
//
// pageSize приходит УЖЕ нормализованным (validate.PageSize на стороне use-case):
// второй нормализатор разошёлся бы с первым молча — на законном входе оба отвечают
// одинаково, и заметить расхождение будет негде.
func (r *DiskTypeBindingRepo) List(ctx context.Context, pageSize int64, pageToken string) ([]*domain.DiskTypeBinding, string, error) {
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
	q := fmt.Sprintf(`SELECT %s FROM disk_type_bindings%s ORDER BY created_at ASC, id ASC LIMIT $%d`,
		diskTypeBindingCols, where, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", mapDiskTypeBindingErr(err, dtbErrCtx{})
	}
	defer rows.Close()
	var out []*domain.DiskTypeBinding
	for rows.Next() {
		b, serr := scanDiskTypeBinding(rows)
		if serr != nil {
			return nil, "", mapDiskTypeBindingErr(serr, dtbErrCtx{})
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, "", mapDiskTypeBindingErr(err, dtbErrCtx{})
	}
	var next string
	if int64(len(out)) > pageSize {
		last := out[pageSize-1]
		next = encodePageToken(cursor{createdAt: last.CreatedAt, id: last.ID})
		out = out[:pageSize]
	}
	return out, next, nil
}

// bindingRegisterSQL — регистрация ревизии ОДНИМ стейтментом: прежняя действующая
// переводится в SUPERSEDED, новая вставляется со следующим номером.
//
// # Почему один стейтмент, а не «прочитал → проверил → записал»
//
// Две конкурентные регистрации на одну пару прошли бы чтение обе и записали бы обе:
// каждая увидела бы одну действующую ревизию, каждая сочла бы себя вправе её
// вытеснить. Здесь такого окна нет by construction — вытеснение и вставка суть одно
// действие, а «ровно одна действующая на пару» держит ЧАСТИЧНЫЙ УНИКАЛЬНЫЙ ИНДЕКС
// (0015), то есть БД, а не порядок вызовов.
//
// Как это разрешается под конкуренцией: обе регистрации берут снимок данных до
// вытеснения, поэтому обе метят в один и тот же следующий номер. Первая коммитится,
// вторая упирается в уникальность (номера ревизии либо действующей строки) и
// получает ALREADY_EXISTS — повтор её запроса уже увидит новое состояние и займёт
// следующий номер.
//
// Вытесняющая часть НАМЕРЕННО не связана с вставляющей выборкой: свяжи их, и первая
// регистрация пары (вытеснять нечего, ноль строк) не вставила бы ничего вовсе.
// Отдельным стейтментом она быть не может — это вернуло бы то самое окно.
const bindingRegisterSQL = `
	WITH superseded AS (
		UPDATE disk_type_bindings
		   SET status = 'SUPERSEDED'
		 WHERE disk_type_id = $2::text AND zone_id = $3::text AND status = 'ACTIVE'
		RETURNING id
	), next_revision AS (
		SELECT COALESCE(max(revision), 0) + 1 AS revision
		  FROM disk_type_bindings
		 WHERE disk_type_id = $2::text AND zone_id = $3::text
	)
	INSERT INTO disk_type_bindings
		(id, disk_type_id, zone_id, backend_id, revision, pool, namespace_template,
		 cap_snapshots, cap_clone_from_snapshot, cap_clone_from_image, cap_clone_keeps_parent,
		 cap_online_grow, cap_multi_attach, cap_encryption_at_rest, trash_ttl_seconds,
		 qos, status)
	SELECT $1::text, $2::text, $3::text, $4::text, n.revision, $5::text, $6::text,
	       $7::boolean, $8::boolean, $9::boolean, $10::boolean, $11::boolean, $12::boolean,
	       $13::boolean, $14::bigint, $15::jsonb, 'ACTIVE'
	  FROM next_revision n
	RETURNING `

// Register заводит НОВУЮ ревизию привязки на пару (класс, зона) и тем же стейтментом
// вытесняет прежнюю действующую. Возвращает записанную ревизию с назначенным номером.
//
// Номер и состояние назначает регистрация, а не вызывающий: номер обязан быть
// следующим В МОМЕНТ ЗАПИСИ (выбери его вызывающий заранее — и он устареет ровно
// между чтением и вставкой), а зарегистрированная ревизия действует по определению.
// Названное вызывающему возвращается ОТКАЗОМ, а не молча выбрасывается: принять
// параметр и не применить его значит отдать успех на невыполненное.
func (r *DiskTypeBindingRepo) Register(ctx context.Context, b *domain.DiskTypeBinding) (*domain.DiskTypeBinding, error) {
	if b.Revision != 0 {
		return nil, fmt.Errorf(
			"%w: disk_type_binding revision is assigned on registration and must not be supplied",
			storageerr.ErrInvalidArg)
	}
	if b.Status != "" && b.Status != domain.BindingStatusActive {
		return nil, fmt.Errorf(
			"%w: disk_type_binding is registered ACTIVE: status %q must not be supplied",
			storageerr.ErrInvalidArg, b.Status)
	}
	qos, err := json.Marshal(bindingQoSDoc{
		BaselineIOPS:            b.QoS.BaselineIOPS,
		IOPSPerGiB:              b.QoS.IOPSPerGiB,
		MaxIOPS:                 b.QoS.MaxIOPS,
		BaselineThroughputMiBps: b.QoS.BaselineThroughputMiBps,
		ThroughputPerGiBMiBps:   b.QoS.ThroughputPerGiBMiBps,
		MaxThroughputMiBps:      b.QoS.MaxThroughputMiBps,
	})
	if err != nil {
		return nil, storageerr.ErrInternal
	}
	created, serr := scanDiskTypeBinding(r.pool.QueryRow(ctx,
		bindingRegisterSQL+diskTypeBindingCols,
		b.ID, b.DiskTypeID, b.ZoneID, b.BackendID,
		b.Locator.Pool, b.Locator.NamespaceTemplate,
		b.Capabilities.Snapshots, b.Capabilities.CloneFromSnapshot,
		b.Capabilities.CloneFromImage, b.Capabilities.CloneKeepsParent,
		b.Capabilities.OnlineGrow, b.Capabilities.MultiAttach,
		b.Capabilities.EncryptionAtRest, b.Capabilities.TrashTTLSeconds, qos))
	if serr != nil {
		return nil, mapDiskTypeBindingErr(serr, dtbErrCtx{
			bindingID:  b.ID,
			diskTypeID: b.DiskTypeID,
			zoneID:     b.ZoneID,
			backendID:  b.BackendID,
		})
	}
	return created, nil
}

// dtbErrCtx — контекстные hint'ы для constraint-aware маппинга ошибок ревизии.
type dtbErrCtx struct {
	bindingID  string
	diskTypeID string
	zoneID     string
	backendID  string
}

// mapDiskTypeBindingErr транслирует pgx/pgconn-ошибку в чистый sentinel с
// контрактным текстом Kachō. Сырой pgx/SQL наружу не течёт: неклассифицированный
// SQLSTATE → storageerr.ErrInternal, сам код логируется на границе.
//
// Обе стороны ссылки отвечают полосой ПРЕДУСЛОВИЯ, а не «не найдено»: класс и бэкенд
// — чужие для этой строки предметы, и отсутствие любого из них означает не «мы не
// нашли свою ревизию», а «условие на другой ресурс не выполнено».
func mapDiskTypeBindingErr(err error, c dtbErrCtx) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, storageerr.ErrNotFound), errors.Is(err, storageerr.ErrAlreadyExists),
		errors.Is(err, storageerr.ErrFailedPrecondition), errors.Is(err, storageerr.ErrInvalidArg),
		errors.Is(err, storageerr.ErrInternal):
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: DiskTypeBinding %s not found", storageerr.ErrNotFound, c.bindingID)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			switch pgErr.ConstraintName {
			case cnBindingPK:
				return fmt.Errorf("%w: DiskTypeBinding %s already exists", storageerr.ErrAlreadyExists, c.bindingID)
			case cnBindingActiveUniq, cnBindingRevisionUniq:
				// Конкурентная регистрация на ту же пару успела первой. Текст один на
				// оба индекса намеренно: вызывающему сообщается ФАКТ («ревизия на эту
				// пару уже заведена»), а не то, какой из двух индексов сработал первым
				// — выбор между ними зависит от порядка проверки, а не от его запроса.
				return fmt.Errorf("%w: DiskTypeBinding for DiskType %s in zone %s already exists",
					storageerr.ErrAlreadyExists, c.diskTypeID, c.zoneID)
			}
			return fmt.Errorf("%w: disk type binding already exists", storageerr.ErrAlreadyExists)
		case "23503": // foreign_key_violation — класс либо бэкенд
			switch pgErr.ConstraintName {
			case cnBindingDiskTypeFK:
				return fmt.Errorf("%w: DiskType %s not found", storageerr.ErrFailedPrecondition, c.diskTypeID)
			case cnBindingBackendFK:
				return fmt.Errorf("%w: StorageBackend %s not found", storageerr.ErrFailedPrecondition, c.backendID)
			}
			return fmt.Errorf("%w: disk type binding violates a reference constraint", storageerr.ErrFailedPrecondition)
		case "23514": // check_violation — состояние, номер, зона, пространство размещения, срок корзины
			return fmt.Errorf("%w: Illegal argument", storageerr.ErrInvalidArg)
		}
		slog.Error("uncategorized postgres error mapped to internal",
			"sqlstate", pgErr.Code, "constraint", pgErr.ConstraintName, "disk_type_binding_id", c.bindingID)
		return storageerr.ErrInternal
	}
	slog.Error("uncategorized db error mapped to internal", "err", err.Error(), "disk_type_binding_id", c.bindingID)
	return storageerr.ErrInternal
}

// Соответствие порту заперто на этапе компиляции.
var _ disktypebinding.Repo = (*DiskTypeBindingRepo)(nil)
