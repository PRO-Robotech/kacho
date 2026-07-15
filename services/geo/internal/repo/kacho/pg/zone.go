// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/outbox"

	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/dberr"
)

// ZoneRepo — реализация zone.Repo поверх pgx.
type ZoneRepo struct {
	pool *pgxpool.Pool
}

// NewZoneRepo создает ZoneRepo поверх pgxpool.
func NewZoneRepo(pool *pgxpool.Pool) *ZoneRepo { return &ZoneRepo{pool: pool} }

// Get возвращает зону по id.
func (r *ZoneRepo) Get(ctx context.Context, id string) (*domain.Zone, error) {
	var z domain.Zone
	var statusName string
	err := r.pool.QueryRow(ctx, `SELECT id, region_id, name, status, created_at FROM zones WHERE id = $1`, id).
		Scan(&z.ID, &z.RegionID, &z.Name, &statusName, &z.CreatedAt)
	if err != nil {
		return nil, dberr.Wrap(err, "Zone", id)
	}
	z.Status = zoneStatusFromName(statusName)
	return &z, nil
}

// List возвращает зоны с курсорной пагинацией по id. pageSize уже нормализован
// use-case-слоем (zone.UseCase.List), repo его не валидирует.
func (r *ZoneRepo) List(ctx context.Context, p zone.Pagination) ([]*domain.Zone, string, error) {
	pageSize := p.PageSize
	args := []any{}
	where := ""
	if p.PageToken != "" {
		cursorID, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", invalidPageTokenErr(derr)
		}
		where = "WHERE id > $1"
		args = append(args, cursorID)
	}
	q := fmt.Sprintf(`SELECT id, region_id, name, status, created_at FROM zones %s ORDER BY id ASC LIMIT $%d`, where, len(args)+1)
	args = append(args, pageSize+1)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", dberr.Wrap(err, "Zone", "")
	}
	defer rows.Close()
	var out []*domain.Zone
	for rows.Next() {
		var z domain.Zone
		var statusName string
		if err := rows.Scan(&z.ID, &z.RegionID, &z.Name, &statusName, &z.CreatedAt); err != nil {
			return nil, "", dberr.Wrap(err, "Zone", "")
		}
		z.Status = zoneStatusFromName(statusName)
		out = append(out, &z)
	}
	if err := rows.Err(); err != nil {
		return nil, "", dberr.Wrap(err, "Zone", "")
	}
	var next string
	if int64(len(out)) > pageSize {
		last := out[pageSize-1]
		next = encodePageToken(last.ID)
		out = out[:pageSize]
	}
	return out, next, nil
}

// Insert создает зону (admin-only) + пишет geo_outbox CREATED в той же tx.
// Несуществующий region_id всплывает как FK-нарушение (23503) → FailedPrecondition.
// Audit-payload фиксирует actor admin-мутации.
func (r *ZoneRepo) Insert(ctx context.Context, z *domain.Zone) (*domain.Zone, error) {
	actor := actorFromCtx(ctx)
	var created domain.Zone
	var statusName string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx,
			`INSERT INTO zones (id, region_id, name, status, created_at) VALUES ($1,$2,$3,$4,$5) RETURNING id, region_id, name, status, created_at`,
			z.ID, z.RegionID, z.Name, zoneStatusName(z.Status), time.Now().UTC()).
			Scan(&created.ID, &created.RegionID, &created.Name, &statusName, &created.CreatedAt)
		if serr != nil {
			return dberr.Wrap(serr, "Zone", z.ID)
		}
		created.Status = zoneStatusFromName(statusName)
		return outbox.Emit(ctx, tx, outboxTable, "Zone", created.ID, "CREATED", map[string]any{
			"id":        created.ID,
			"region_id": created.RegionID,
			"name":      created.Name,
			"status":    statusName,
			"actor":     actor,
		})
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// Update меняет зону (admin-only) одним атомарным statement (без предварительного
// Get / TOCTOU): обновляются только переданные поля, неуказанные сохраняются
// (COALESCE) + пишет geo_outbox UPDATED в той же tx. 0 rows из RETURNING →
// ErrNotFound. Несуществующий новый region_id → FK 23503 → FailedPrecondition.
func (r *ZoneRepo) Update(ctx context.Context, id string, p zone.UpdateParams) (*domain.Zone, error) {
	actor := actorFromCtx(ctx)
	var statusName *string
	if p.Status != nil {
		s := zoneStatusName(*p.Status)
		statusName = &s
	}
	var updated domain.Zone
	var outStatus string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx,
			`UPDATE zones
			    SET region_id = COALESCE($2, region_id),
			        name      = COALESCE($3, name),
			        status    = COALESCE($4, status)
			  WHERE id = $1
			RETURNING id, region_id, name, status, created_at`,
			id, p.RegionID, p.Name, statusName).
			Scan(&updated.ID, &updated.RegionID, &updated.Name, &outStatus, &updated.CreatedAt)
		if serr != nil {
			return dberr.Wrap(serr, "Zone", id)
		}
		updated.Status = zoneStatusFromName(outStatus)
		return outbox.Emit(ctx, tx, outboxTable, "Zone", updated.ID, "UPDATED", map[string]any{
			"id":        updated.ID,
			"region_id": updated.RegionID,
			"name":      updated.Name,
			"status":    outStatus,
			"actor":     actor,
		})
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// Delete удаляет зону (admin-only) + пишет geo_outbox DELETED.
func (r *ZoneRepo) Delete(ctx context.Context, id string) error {
	actor := actorFromCtx(ctx)
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM zones WHERE id = $1`, id)
		if err != nil {
			return dberr.Wrap(err, "Zone", id)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: Zone %s not found", geoerrors.ErrNotFound, id)
		}
		return outbox.Emit(ctx, tx, outboxTable, "Zone", id, "DELETED", map[string]any{
			"id":    id,
			"actor": actor,
		})
	})
}

func zoneStatusName(s domain.ZoneStatus) string {
	switch s {
	case domain.ZoneStatusUp:
		return "UP"
	case domain.ZoneStatusDown:
		return "DOWN"
	default:
		return "STATUS_UNSPECIFIED"
	}
}

func zoneStatusFromName(s string) domain.ZoneStatus {
	switch s {
	case "UP":
		return domain.ZoneStatusUp
	case "DOWN":
		return domain.ZoneStatusDown
	default:
		return domain.ZoneStatusUnspecified
	}
}

var _ zone.Repo = (*ZoneRepo)(nil)
