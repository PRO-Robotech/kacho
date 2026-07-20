// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"fmt"
	"strings"
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

// Get возвращает публичную проекцию зоны + status родит-региона (JOIN) для
// деривации openForPlacement°/placementBlockedReason°. infra НЕ читается тут
// (two-projection, см. GetInternal).
func (r *ZoneRepo) Get(ctx context.Context, id string) (*domain.Zone, error) {
	var z domain.Zone
	var statusName, regionStatusName string
	err := r.pool.QueryRow(ctx,
		`SELECT z.id, z.region_id, z.name, z.status, z.created_at, r.status
		   FROM zones z JOIN regions r ON r.id = z.region_id
		  WHERE z.id = $1`, id).
		Scan(&z.ID, &z.RegionID, &z.Name, &statusName, &z.CreatedAt, &regionStatusName)
	if err != nil {
		return nil, dberr.Wrap(err, "Zone", id)
	}
	z.Status = geoStatusFromName(statusName)
	z.RegionStatus = geoStatusFromName(regionStatusName)
	return &z, nil
}

// GetInternal возвращает full admin-проекцию зоны (status + infra°). :9091-only.
func (r *ZoneRepo) GetInternal(ctx context.Context, id string) (*domain.Zone, error) {
	var z domain.Zone
	var statusName string
	err := r.pool.QueryRow(ctx,
		`SELECT id, region_id, name, status,
		        numeric_infra_id, host_classes, failure_domain_count, underlay_anchor, capacity_hint,
		        created_at
		   FROM zones WHERE id = $1`, id).
		Scan(&z.ID, &z.RegionID, &z.Name, &statusName,
			&z.Infra.NumericInfraID, &z.Infra.HostClasses, &z.Infra.FailureDomainCount, &z.Infra.UnderlayAnchor, &z.Infra.CapacityHint,
			&z.CreatedAt)
	if err != nil {
		return nil, dberr.Wrap(err, "Zone", id)
	}
	z.Status = geoStatusFromName(statusName)
	return &z, nil
}

// List возвращает публичные проекции зон (+region_status JOIN) с курсорной
// пагинацией по id. Фильтры: region_id (по региону), openForPlacement=true
// (zone.status='UP' И region.status='UP'). pageSize нормализован use-case'ом.
func (r *ZoneRepo) List(ctx context.Context, p zone.Pagination) ([]*domain.Zone, string, error) {
	pageSize := p.PageSize
	var conds []string
	args := []any{}
	if p.PageToken != "" {
		cursorID, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", invalidPageTokenErr(derr)
		}
		args = append(args, cursorID)
		conds = append(conds, fmt.Sprintf("z.id > $%d", len(args)))
	}
	if p.RegionID != "" {
		args = append(args, p.RegionID)
		conds = append(conds, fmt.Sprintf("z.region_id = $%d", len(args)))
	}
	if p.OpenForPlacement {
		conds = append(conds, "z.status = 'UP'", "r.status = 'UP'")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, pageSize+1)
	q := fmt.Sprintf(
		`SELECT z.id, z.region_id, z.name, z.status, z.created_at, r.status
		   FROM zones z JOIN regions r ON r.id = z.region_id
		   %s ORDER BY z.id ASC LIMIT $%d`, where, len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", dberr.Wrap(err, "Zone", "")
	}
	defer rows.Close()
	var out []*domain.Zone
	for rows.Next() {
		var z domain.Zone
		var statusName, regionStatusName string
		if err := rows.Scan(&z.ID, &z.RegionID, &z.Name, &statusName, &z.CreatedAt, &regionStatusName); err != nil {
			return nil, "", dberr.Wrap(err, "Zone", "")
		}
		z.Status = geoStatusFromName(statusName)
		z.RegionStatus = geoStatusFromName(regionStatusName)
		out = append(out, &z)
	}
	if err := rows.Err(); err != nil {
		return nil, "", dberr.Wrap(err, "Zone", "")
	}
	var next string
	if int64(len(out)) > pageSize {
		next = encodePageToken(out[pageSize-1].ID)
		out = out[:pageSize]
	}
	return out, next, nil
}

// Insert создает зону (admin-only) + пишет geo_outbox CREATED в той же tx.
// Несуществующий region_id → FK 23503 → FailedPrecondition (DB-backstop, ban #10).
// В той же tx подтягивает status родит-региона для деривации openForPlacement° в
// синхронном Operation.response.
func (r *ZoneRepo) Insert(ctx context.Context, z *domain.Zone) (*domain.Zone, error) {
	actor := actorFromCtx(ctx)
	// host_classes — NOT NULL TEXT[]; nil Go-slice pgx кодирует как NULL и явный
	// INSERT обходит DB-DEFAULT '{}' → нормализуем nil→[] на границе adapter'а.
	hostClasses := z.Infra.HostClasses
	if hostClasses == nil {
		hostClasses = []string{}
	}
	var created domain.Zone
	var statusName, regionStatusName string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx,
			`INSERT INTO zones (id, region_id, name, status,
			                    numeric_infra_id, host_classes, failure_domain_count, underlay_anchor, capacity_hint,
			                    created_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			 RETURNING id, region_id, name, status,
			           numeric_infra_id, host_classes, failure_domain_count, underlay_anchor, capacity_hint,
			           created_at`,
			z.ID, z.RegionID, z.Name, geoStatusName(z.Status),
			z.Infra.NumericInfraID, hostClasses, z.Infra.FailureDomainCount, z.Infra.UnderlayAnchor, z.Infra.CapacityHint,
			time.Now().UTC()).
			Scan(&created.ID, &created.RegionID, &created.Name, &statusName,
				&created.Infra.NumericInfraID, &created.Infra.HostClasses, &created.Infra.FailureDomainCount, &created.Infra.UnderlayAnchor, &created.Infra.CapacityHint,
				&created.CreatedAt)
		if serr != nil {
			return dberr.Wrap(serr, "Zone", z.ID)
		}
		// FK гарантирует существование региона → SELECT вернёт ровно строку.
		if serr := tx.QueryRow(ctx, `SELECT status FROM regions WHERE id = $1`, created.RegionID).
			Scan(&regionStatusName); serr != nil {
			return dberr.Wrap(serr, "Region", created.RegionID)
		}
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
	created.Status = geoStatusFromName(statusName)
	created.RegionStatus = geoStatusFromName(regionStatusName)
	return &created, nil
}

// Update — атомарный partial-update зоны (name/status/infra-subset) одним
// statement (COALESCE, без TOCTOU) + geo_outbox UPDATED. region_id НЕ меняется
// (immutable — поля в UpdateParams нет). nil-поля не меняются. 0 rows → ErrNotFound.
// Подтягивает region_status для деривации openForPlacement° в Operation.response.
func (r *ZoneRepo) Update(ctx context.Context, id string, p zone.UpdateParams) (*domain.Zone, error) {
	actor := actorFromCtx(ctx)
	var statusName *string
	if p.Status != nil {
		s := geoStatusName(*p.Status)
		statusName = &s
	}
	var updated domain.Zone
	var outStatus, regionStatusName string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx,
			`UPDATE zones
			    SET name                 = COALESCE($2, name),
			        status               = COALESCE($3, status),
			        host_classes         = COALESCE($4, host_classes),
			        failure_domain_count = COALESCE($5, failure_domain_count),
			        underlay_anchor      = COALESCE($6, underlay_anchor),
			        capacity_hint        = COALESCE($7, capacity_hint)
			  WHERE id = $1
			RETURNING id, region_id, name, status,
			          numeric_infra_id, host_classes, failure_domain_count, underlay_anchor, capacity_hint,
			          created_at`,
			id, p.Name, statusName, p.HostClasses, p.FailureDomainCount, p.UnderlayAnchor, p.CapacityHint).
			Scan(&updated.ID, &updated.RegionID, &updated.Name, &outStatus,
				&updated.Infra.NumericInfraID, &updated.Infra.HostClasses, &updated.Infra.FailureDomainCount, &updated.Infra.UnderlayAnchor, &updated.Infra.CapacityHint,
				&updated.CreatedAt)
		if serr != nil {
			return dberr.Wrap(serr, "Zone", id)
		}
		if serr := tx.QueryRow(ctx, `SELECT status FROM regions WHERE id = $1`, updated.RegionID).
			Scan(&regionStatusName); serr != nil {
			return dberr.Wrap(serr, "Region", updated.RegionID)
		}
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
	updated.Status = geoStatusFromName(outStatus)
	updated.RegionStatus = geoStatusFromName(regionStatusName)
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

// geoStatusName маппит domain.GeoStatus → строку колонки status ('UP'/'DOWN').
// Unspecified → 'DOWN' (fail-safe, совпадает с DB-DEFAULT): use-case коэрсит
// fresh→DOWN до repo, а прямой repo-insert без явного статуса тоже поднимается
// DOWN (закрытый по умолчанию) — CHECK(status IN ('UP','DOWN')) не нарушается.
func geoStatusName(s domain.GeoStatus) string {
	if s == domain.GeoStatusUp {
		return "UP"
	}
	return "DOWN"
}

func geoStatusFromName(s string) domain.GeoStatus {
	switch s {
	case "UP":
		return domain.GeoStatusUp
	case "DOWN":
		return domain.GeoStatusDown
	default:
		return domain.GeoStatusUnspecified
	}
}

var _ zone.Repo = (*ZoneRepo)(nil)
