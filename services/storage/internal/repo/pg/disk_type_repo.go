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

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/disktype"
)

// DiskTypeRepo — реализация disktype.Repo поверх pgxpool. Public read (Get/List) +
// admin write (Insert/Update/Delete, sync). Delete под FK RESTRICT: том, ссылающийся
// на тип (volumes.disk_type_id), блокирует удаление на DB-уровне (не software refcount).
type DiskTypeRepo struct {
	pool *pgxpool.Pool
}

// NewDiskTypeRepo создаёт DiskTypeRepo поверх pgxpool.
func NewDiskTypeRepo(pool *pgxpool.Pool) *DiskTypeRepo { return &DiskTypeRepo{pool: pool} }

// scanDiskType читает id/name/description/zone_ids/performance_tier в domain.DiskType.
// zone_ids — jsonb-массив → []string.
func scanDiskType(id, name, description, performanceTier string, zoneIDsJSON []byte) (*domain.DiskType, error) {
	d := domain.DiskType{ID: id, Name: name, Description: description, PerformanceTier: performanceTier}
	if len(zoneIDsJSON) > 0 {
		if err := json.Unmarshal(zoneIDsJSON, &d.ZoneIDs); err != nil {
			return nil, err
		}
	}
	return &d, nil
}

// Get реализует disktype.Repo: тип по id-слагу. Отсутствует → NotFound.
func (r *DiskTypeRepo) Get(ctx context.Context, id string) (*domain.DiskType, error) {
	var (
		name, description, performanceTier string
		zoneIDsJSON                        []byte
	)
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, description, zone_ids, performance_tier FROM disk_types WHERE id = $1`, id).
		Scan(&id, &name, &description, &zoneIDsJSON, &performanceTier)
	if err != nil {
		return nil, mapDiskTypeErr(err, dtErrCtx{diskTypeID: id})
	}
	d, serr := scanDiskType(id, name, description, performanceTier, zoneIDsJSON)
	if serr != nil {
		return nil, mapDiskTypeErr(serr, dtErrCtx{diskTypeID: id})
	}
	return d, nil
}

// List реализует disktype.Repo: cursor-пагинация (created_at,id) ASC каталога.
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
		conds = append(conds, fmt.Sprintf("(created_at, id) > ($%d, $%d)", len(args)-1, len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, p.PageSize+1)
	q := fmt.Sprintf(`SELECT id, name, description, zone_ids, performance_tier, created_at
		FROM disk_types %s ORDER BY created_at ASC, id ASC LIMIT $%d`, where, len(args))

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
		var (
			id, name, description, performanceTier string
			zoneIDsJSON                            []byte
			createdAt                              time.Time
		)
		if serr := rows.Scan(&id, &name, &description, &zoneIDsJSON, &performanceTier, &createdAt); serr != nil {
			return nil, "", mapDiskTypeErr(serr, dtErrCtx{})
		}
		d, serr := scanDiskType(id, name, description, performanceTier, zoneIDsJSON)
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
func (r *DiskTypeRepo) Insert(ctx context.Context, d *domain.DiskType) (*domain.DiskType, error) {
	zoneIDs, err := json.Marshal(nonNilSlice(d.ZoneIDs))
	if err != nil {
		return nil, ports.ErrInternal
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO disk_types (id, name, description, zone_ids, performance_tier)
		 VALUES ($1,$2,$3,$4::jsonb,$5)`,
		d.ID, d.Name, d.Description, zoneIDs, d.PerformanceTier)
	if err != nil {
		return nil, mapDiskTypeErr(err, dtErrCtx{diskTypeID: d.ID})
	}
	return r.Get(ctx, d.ID)
}

// Update реализует disktype.Repo: full-replace mutable-полей (proto без FieldMask).
// 0 rows → NotFound. zone_ids nil → '[]' (CHECK).
func (r *DiskTypeRepo) Update(ctx context.Context, id, name, description string, zoneIDs []string, performanceTier string) (*domain.DiskType, error) {
	zoneJSON, err := json.Marshal(nonNilSlice(zoneIDs))
	if err != nil {
		return nil, ports.ErrInternal
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE disk_types SET name=$2, description=$3, zone_ids=$4::jsonb, performance_tier=$5 WHERE id=$1`,
		id, name, description, zoneJSON, performanceTier)
	if err != nil {
		return nil, mapDiskTypeErr(err, dtErrCtx{diskTypeID: id})
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("%w: DiskType %s not found", ports.ErrNotFound, id)
	}
	return r.Get(ctx, id)
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
		return fmt.Errorf("%w: DiskType %s not found", ports.ErrNotFound, id)
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

var _ disktype.Repo = (*DiskTypeRepo)(nil)
