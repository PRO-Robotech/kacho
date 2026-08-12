// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// DiskTypeRepo — реализация disktype.Repo поверх pgxpool. Public read (Get/List) +
// admin write (Insert/UpdateFields/Delete, sync). Delete под FK RESTRICT: том,
// ссылающийся на тип (volumes.disk_type_id), блокирует удаление на DB-уровне (не
// software refcount).
type DiskTypeRepo struct {
	pool *pgxpool.Pool
}

// NewDiskTypeRepo создаёт DiskTypeRepo поверх pgxpool.
func NewDiskTypeRepo(pool *pgxpool.Pool) *DiskTypeRepo { return &DiskTypeRepo{pool: pool} }

// DiskTypeUpdate — набор ИЗМЕНЯЕМЫХ полей класса: nil-указатель означает «не
// менять», а не «обнулить». Форма выбрана под маску правки: поле, названное маской,
// приезжает сюда указателем, а не названное остаётся nil — поэтому провязка маски
// ложится на репозиторий без второй переделки SQL.
//
// Полное замещение (Update ниже) выражено ЧЕРЕЗ этот набор, а не отдельным
// стейтментом: два места, пишущие одну строку, расходятся — вопрос лишь когда.
type DiskTypeUpdate struct {
	Name            *string
	Description     *string
	ZoneIDs         *[]string
	PerformanceTier *domain.PerformanceTier
	Lifecycle       *domain.DiskTypeLifecycle
	MinSizeBytes    *int64
	MaxSizeBytes    *int64
	SizeStepBytes   *int64
}

// diskTypeSelect — единственная форма чтения класса: собственные колонки плюс
// ВЫВЕДЕННЫЕ способности.
//
// Способности на классе не хранятся. Умеет ли хранилище снимки, растёт ли том
// вживую, шифрует ли он данные — свойство БЭКЕНДА, а не продуктовой политики, и
// живёт оно на ревизии привязки. Копия на классе была бы вторым источником истины об
// одном факте: администратор сменил бэкенд — и каталог продолжает обещать умение,
// которого больше нет, причём молча, потому что расхождение двух мест ничем не
// наблюдается.
//
// Пересечение КОНСЕРВАТИВНОЕ (bool_and): класс предлагается в нескольких зонах, зоны
// обслуживаются разными бэкендами, и способность объявляется, только если её несут
// ВСЕ действующие ревизии. Ноль действующих ревизий → агрегат отдаёт NULL → COALESCE
// в false: не объявляется ничего. Обратный выбор (объявить умение, которое есть хотя
// бы у одной зоны) означал бы отказ в момент использования там, где каталог обещал
// успех — то есть арендатор узнаёт правду отказом, ровно от чего способности и
// публичны.
//
// Соединение LATERAL, а не подзапрос на строку: страница обязана стоить один запрос
// независимо от числа классов (пообъектный добор — это N+1, и заметен он только под
// нагрузкой, когда чинить дорого).
const diskTypeSelect = `
SELECT d.id, d.name, d.description, d.zone_ids, d.performance_tier,
       d.lifecycle, d.min_size_bytes, d.max_size_bytes, d.size_step_bytes,
       COALESCE(c.snapshots, false), COALESCE(c.clone_from_snapshot, false),
       COALESCE(c.clone_from_image, false), COALESCE(c.online_grow, false),
       COALESCE(c.multi_attach, false), COALESCE(c.encryption_at_rest, false),
       d.created_at
  FROM disk_types d
  LEFT JOIN LATERAL (
      SELECT bool_and(b.cap_snapshots)           AS snapshots,
             bool_and(b.cap_clone_from_snapshot) AS clone_from_snapshot,
             bool_and(b.cap_clone_from_image)    AS clone_from_image,
             bool_and(b.cap_online_grow)         AS online_grow,
             bool_and(b.cap_multi_attach)        AS multi_attach,
             bool_and(b.cap_encryption_at_rest)  AS encryption_at_rest
        FROM disk_type_bindings b
       WHERE b.disk_type_id = d.id AND b.status = 'ACTIVE'
  ) c ON TRUE`

// rowScanner — общий знаменатель pgx.Row и pgx.Rows. Нужен, чтобы Get и List читали
// строку ОДНОЙ функцией: разъехавшиеся списки колонок дают поле, живущее только в
// одном из двух чтений, и расходится это молча.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanDiskType читает строку diskTypeSelect в domain.DiskType. Возвращает ещё и
// created_at — он не поле ресурса, а координата курсора списка.
func scanDiskType(s rowScanner) (*domain.DiskType, time.Time, error) {
	var (
		d           domain.DiskType
		tier        string
		lifecycle   string
		zoneIDsJSON []byte
		createdAt   time.Time
	)
	if err := s.Scan(
		&d.ID, &d.Name, &d.Description, &zoneIDsJSON, &tier,
		&lifecycle, &d.Limits.MinSizeBytes, &d.Limits.MaxSizeBytes, &d.Limits.SizeStepBytes,
		&d.Capabilities.Snapshots, &d.Capabilities.CloneFromSnapshot,
		&d.Capabilities.CloneFromImage, &d.Capabilities.OnlineGrow,
		&d.Capabilities.MultiAttach, &d.Capabilities.EncryptionAtRest,
		&createdAt,
	); err != nil {
		return nil, time.Time{}, err
	}
	d.PerformanceTier = domain.PerformanceTier(tier)
	d.Lifecycle = domain.DiskTypeLifecycle(lifecycle)
	if len(zoneIDsJSON) > 0 {
		if err := json.Unmarshal(zoneIDsJSON, &d.ZoneIDs); err != nil {
			return nil, time.Time{}, err
		}
	}
	return &d, createdAt, nil
}

// Get реализует disktype.Repo: тип по id-слагу вместе с выведенными способностями.
// Отсутствует → NotFound.
func (r *DiskTypeRepo) Get(ctx context.Context, id string) (*domain.DiskType, error) {
	d, _, err := scanDiskType(r.pool.QueryRow(ctx, diskTypeSelect+` WHERE d.id = $1`, id))
	if err != nil {
		return nil, mapDiskTypeErr(err, dtErrCtx{diskTypeID: id})
	}
	return d, nil
}

// List реализует disktype.Repo: cursor-пагинация (created_at,id) ASC каталога.
// Способности выводятся тем же стейтментом — страница стоит один запрос.
func (r *DiskTypeRepo) List(ctx context.Context, p disktype.Pagination) ([]*domain.DiskType, string, error) {
	var (
		conds []string
		args  []any
	)
	if p.PageToken != "" {
		cur, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", derr
		}
		args = append(args, cur.createdAt, cur.id)
		conds = append(conds, fmt.Sprintf("(d.created_at, d.id) > ($%d, $%d)", len(args)-1, len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, p.PageSize+1)
	q := fmt.Sprintf(`%s%s ORDER BY d.created_at ASC, d.id ASC LIMIT $%d`, diskTypeSelect, where, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", mapDiskTypeErr(err, dtErrCtx{})
	}
	defer rows.Close()
	var (
		out        []*domain.DiskType
		createdAts []time.Time
	)
	for rows.Next() {
		d, createdAt, serr := scanDiskType(rows)
		if serr != nil {
			return nil, "", mapDiskTypeErr(serr, dtErrCtx{})
		}
		out = append(out, d)
		createdAts = append(createdAts, createdAt)
	}
	if err := rows.Err(); err != nil {
		return nil, "", mapDiskTypeErr(err, dtErrCtx{})
	}
	var next string
	if int64(len(out)) > p.PageSize {
		idx := p.PageSize - 1
		next = encodePageToken(cursor{createdAt: createdAts[idx], id: out[idx].ID})
		out = out[:p.PageSize]
	}
	return out, next, nil
}

// Insert реализует disktype.Repo: admin-создание. id — admin-assigned slug (PK);
// дубликат → 23505 → AlreadyExists. zone_ids nil → '[]' (CHECK jsonb_typeof='array').
//
// Состояние обращения и границы размера идут в БД КАК НАЗВАНЫ: репозиторий пустое
// поле не досочиняет. Умолчание — решение края (disktype.CreateAdmin), а домен
// объявляет пустое состояние невалидным; подставь репозиторий 'ACTIVE' сам — и
// опечатка админа завела бы класс принимающим новые тома. Согласованность границ
// (min ≤ max, кратность шагу, неотрицательность) держат CHECK'и миграции 0016, а не
// проверка здесь: инвариант одной таблицы живёт на уровне БД.
func (r *DiskTypeRepo) Insert(ctx context.Context, d *domain.DiskType) (*domain.DiskType, error) {
	zoneIDs, err := json.Marshal(nonNilSlice(d.ZoneIDs))
	if err != nil {
		return nil, storageerr.ErrInternal
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO disk_types
		   (id, name, description, zone_ids, performance_tier,
		    lifecycle, min_size_bytes, max_size_bytes, size_step_bytes)
		 VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9)`,
		d.ID, d.Name, d.Description, zoneIDs, string(d.PerformanceTier),
		string(d.Lifecycle), d.Limits.MinSizeBytes, d.Limits.MaxSizeBytes, d.Limits.SizeStepBytes)
	if err != nil {
		return nil, mapDiskTypeErr(err, dtErrCtx{diskTypeID: d.ID})
	}
	return r.Get(ctx, d.ID)
}

// UpdateFields применяет НАЗВАННЫЕ поля класса одним стейтментом: nil-указатель →
// COALESCE оставляет колонку как есть. Пустая строка и пустой список — законные
// ЗНАЧЕНИЯ (COALESCE их пропускает, потому что NULL'ом является только отсутствие
// указателя), поэтому «снять описание» и «не трогать описание» различимы.
// 0 rows → NotFound.
func (r *DiskTypeRepo) UpdateFields(ctx context.Context, id string, u DiskTypeUpdate) (*domain.DiskType, error) {
	var zoneJSON []byte
	if u.ZoneIDs != nil {
		b, err := json.Marshal(nonNilSlice(*u.ZoneIDs))
		if err != nil {
			return nil, storageerr.ErrInternal
		}
		zoneJSON = b
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE disk_types SET
		    name             = COALESCE($2, name),
		    description      = COALESCE($3, description),
		    zone_ids         = COALESCE($4::jsonb, zone_ids),
		    performance_tier = COALESCE($5, performance_tier),
		    lifecycle        = COALESCE($6, lifecycle),
		    min_size_bytes   = COALESCE($7, min_size_bytes),
		    max_size_bytes   = COALESCE($8, max_size_bytes),
		    size_step_bytes  = COALESCE($9, size_step_bytes)
		  WHERE id = $1`,
		id, u.Name, u.Description, zoneJSON,
		strPtr(u.PerformanceTier), strPtr(u.Lifecycle),
		u.MinSizeBytes, u.MaxSizeBytes, u.SizeStepBytes)
	if err != nil {
		return nil, mapDiskTypeErr(err, dtErrCtx{diskTypeID: id})
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("%w: DiskType %s not found", storageerr.ErrNotFound, id)
	}
	return r.Get(ctx, id)
}

// Update реализует disktype.Repo в его сегодняшней форме — полное замещение
// названных mutable-полей (запрос правки контракта пока без маски). Выражено через
// UpdateFields, чтобы строку класса писал ровно один стейтмент: состояние обращения
// и границы размера этим путём не приходят и остаются как были.
func (r *DiskTypeRepo) Update(ctx context.Context, id, name, description string, zoneIDs []string, performanceTier string) (*domain.DiskType, error) {
	tier := domain.PerformanceTier(performanceTier)
	return r.UpdateFields(ctx, id, DiskTypeUpdate{
		Name:            &name,
		Description:     &description,
		ZoneIDs:         &zoneIDs,
		PerformanceTier: &tier,
	})
}

// Delete реализует disktype.Repo: admin-удаление. Ссылающийся том →
// volumes.disk_type_id RESTRICT (23503) → FailedPrecondition "DiskType <id> is in
// use" (Q4). 0 rows → NotFound.
func (r *DiskTypeRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM disk_types WHERE id = $1`, id)
	if err != nil {
		return mapDiskTypeErr(err, dtErrCtx{diskTypeID: id})
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: DiskType %s not found", storageerr.ErrNotFound, id)
	}
	return nil
}

// nonNilSlice гарантирует непустой JSON-массив ('[]', не 'null') для zone_ids —
// CHECK disk_types_zone_ids_is_array отвергает jsonb-null.
func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// strPtr переводит указатель на строковый доменный тип в *string, сохраняя
// различие «не названо» (nil → NULL → COALESCE не трогает колонку) и «названо
// пустым» (значение уходит в БД и получает ответ её ограничения).
func strPtr[T ~string](v *T) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

var _ disktype.Repo = (*DiskTypeRepo)(nil)
