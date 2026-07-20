// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package pg — Postgres-adapter (handwritten pgx) для справочника regions /
// zones kacho-geo. Реализует порты region.Repo / zone.Repo. Admin-мутации
// (Insert/Update/Delete) пишут audit-строку в geo_outbox атомарно в той же
// writer-tx (без software check-then-act, аудит не может потеряться).
package pg

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/outbox"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/dberr"
)

// outboxTable — таблица audit-outbox для admin-мутаций kacho-geo (конвенция
// <domain>_outbox, parity с compute_outbox / vpc_outbox).
const outboxTable = "geo_outbox"

// actorUnknown — sentinel для audit-actor, когда атрибуция утрачена (в ctx явно
// выставлен principal с пустым ID: misconfig / wiring-регрессия). Пишем
// наблюдаемый маркер в geo_outbox, а НЕ пустую строку (CWE-778).
const actorUnknown = "unknown"

// actorFromCtx форматирует trusted principal вызывающего как "<type>:<id>" для
// audit-payload. Пустой ID → actorUnknown-sentinel, а не blank.
func actorFromCtx(ctx context.Context) string {
	p := operations.PrincipalFromContext(ctx)
	if p.ID == "" {
		return actorUnknown
	}
	return p.Type + ":" + p.ID
}

// openZoneCountExpr — read-time rollup числа зон региона с openForPlacement°=true
// (advisory-hint, НЕ persisted). Зона open ⟺ zone.status='UP' И region.status='UP',
// поэтому для DOWN-региона hint=0 by construction.
const openZoneCountExpr = `CASE WHEN r.status = 'UP'
	THEN (SELECT count(*) FROM zones z WHERE z.region_id = r.id AND z.status = 'UP')
	ELSE 0 END`

// RegionRepo — реализация region.Repo поверх pgx.
type RegionRepo struct {
	pool *pgxpool.Pool
}

// NewRegionRepo создает RegionRepo поверх pgxpool.
func NewRegionRepo(pool *pgxpool.Pool) *RegionRepo { return &RegionRepo{pool: pool} }

// Get возвращает публичную проекцию региона (со status для деривации
// openForPlacement° + read-time openZoneCount rollup). infra НЕ читается тут —
// она two-projection (см. GetInternal).
func (r *RegionRepo) Get(ctx context.Context, id string) (*domain.Region, error) {
	var rg domain.Region
	var statusName string
	err := r.pool.QueryRow(ctx,
		`SELECT r.id, r.name, r.country_code, r.status, r.created_at, `+openZoneCountExpr+`
		   FROM regions r WHERE r.id = $1`, id).
		Scan(&rg.ID, &rg.Name, &rg.CountryCode, &statusName, &rg.CreatedAt, &rg.OpenZoneCount)
	if err != nil {
		return nil, dberr.Wrap(err, "Region", id)
	}
	rg.Status = geoStatusFromName(statusName)
	return &rg, nil
}

// GetInternal возвращает full admin-проекцию региона (status + infra°). :9091-only.
// Region-infra capacity_hint° — advisory read-time rollup (не persisted); текущая
// фаза его не наполняет (остаётся пустым — инвариант на hint не строим).
func (r *RegionRepo) GetInternal(ctx context.Context, id string) (*domain.Region, error) {
	var rg domain.Region
	var statusName string
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, country_code, status, numeric_infra_id, created_at
		   FROM regions WHERE id = $1`, id).
		Scan(&rg.ID, &rg.Name, &rg.CountryCode, &statusName, &rg.Infra.NumericInfraID, &rg.CreatedAt)
	if err != nil {
		return nil, dberr.Wrap(err, "Region", id)
	}
	rg.Status = geoStatusFromName(statusName)
	return &rg, nil
}

// List возвращает публичные проекции регионов с курсорной пагинацией по id.
// openForPlacement=true фильтрует по status='UP'. pageSize нормализован use-case'ом.
func (r *RegionRepo) List(ctx context.Context, p region.Pagination) ([]*domain.Region, string, error) {
	pageSize := p.PageSize
	var conds []string
	args := []any{}
	if p.PageToken != "" {
		cursorID, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", invalidPageTokenErr(derr)
		}
		args = append(args, cursorID)
		conds = append(conds, fmt.Sprintf("r.id > $%d", len(args)))
	}
	if p.OpenForPlacement {
		conds = append(conds, "r.status = 'UP'")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, pageSize+1)
	q := fmt.Sprintf(
		`SELECT r.id, r.name, r.country_code, r.status, r.created_at, %s
		   FROM regions r %s ORDER BY r.id ASC LIMIT $%d`,
		openZoneCountExpr, where, len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", dberr.Wrap(err, "Region", "")
	}
	defer rows.Close()
	var out []*domain.Region
	for rows.Next() {
		var rg domain.Region
		var statusName string
		if err := rows.Scan(&rg.ID, &rg.Name, &rg.CountryCode, &statusName, &rg.CreatedAt, &rg.OpenZoneCount); err != nil {
			return nil, "", dberr.Wrap(err, "Region", "")
		}
		rg.Status = geoStatusFromName(statusName)
		out = append(out, &rg)
	}
	if err := rows.Err(); err != nil {
		return nil, "", dberr.Wrap(err, "Region", "")
	}
	var next string
	if int64(len(out)) > pageSize {
		next = encodePageToken(out[pageSize-1].ID)
		out = out[:pageSize]
	}
	return out, next, nil
}

// Insert создает регион (admin-only) + пишет geo_outbox CREATED атомарно в той
// же tx. Дубль id/name → 23505 → ErrAlreadyExists.
func (r *RegionRepo) Insert(ctx context.Context, rg *domain.Region) (*domain.Region, error) {
	actor := actorFromCtx(ctx)
	var created domain.Region
	var statusName string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx,
			`INSERT INTO regions (id, name, country_code, status, numeric_infra_id, created_at)
			 VALUES ($1,$2,$3,$4,$5,$6)
			 RETURNING id, name, country_code, status, numeric_infra_id, created_at`,
			rg.ID, rg.Name, rg.CountryCode, geoStatusName(rg.Status), rg.Infra.NumericInfraID, time.Now().UTC()).
			Scan(&created.ID, &created.Name, &created.CountryCode, &statusName, &created.Infra.NumericInfraID, &created.CreatedAt)
		if serr != nil {
			return dberr.Wrap(serr, "Region", rg.ID)
		}
		return outbox.Emit(ctx, tx, outboxTable, "Region", created.ID, "CREATED", map[string]any{
			"id":           created.ID,
			"name":         created.Name,
			"country_code": created.CountryCode,
			"status":       statusName,
			"actor":        actor,
		})
	})
	if err != nil {
		return nil, err
	}
	created.Status = geoStatusFromName(statusName)
	return &created, nil
}

// Update — атомарный partial-update региона (name/status/country_code) одним
// statement (COALESCE, без TOCTOU) + geo_outbox UPDATED. nil-поля не меняются.
// 0 rows из RETURNING → ErrNotFound. Дубль name → 23505 → ErrAlreadyExists.
func (r *RegionRepo) Update(ctx context.Context, id string, p region.UpdateParams) (*domain.Region, error) {
	actor := actorFromCtx(ctx)
	var statusName *string
	if p.Status != nil {
		s := geoStatusName(*p.Status)
		statusName = &s
	}
	var updated domain.Region
	var outStatus string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx,
			`UPDATE regions
			    SET name         = COALESCE($2, name),
			        status       = COALESCE($3, status),
			        country_code = COALESCE($4, country_code)
			  WHERE id = $1
			RETURNING id, name, country_code, status, numeric_infra_id, created_at`,
			id, p.Name, statusName, p.CountryCode).
			Scan(&updated.ID, &updated.Name, &updated.CountryCode, &outStatus, &updated.Infra.NumericInfraID, &updated.CreatedAt)
		if serr != nil {
			return dberr.Wrap(serr, "Region", id)
		}
		return outbox.Emit(ctx, tx, outboxTable, "Region", updated.ID, "UPDATED", map[string]any{
			"id":           updated.ID,
			"name":         updated.Name,
			"country_code": updated.CountryCode,
			"status":       outStatus,
			"actor":        actor,
		})
	})
	if err != nil {
		return nil, err
	}
	updated.Status = geoStatusFromName(outStatus)
	return &updated, nil
}

// Delete удаляет регион (admin-only) + пишет geo_outbox DELETED. FK RESTRICT
// (zones→regions) всплывает как SQLSTATE 23503 → ErrFailedPrecondition.
func (r *RegionRepo) Delete(ctx context.Context, id string) error {
	actor := actorFromCtx(ctx)
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM regions WHERE id = $1`, id)
		if err != nil {
			return dberr.Wrap(err, "Region", id)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: Region %s not found", geoerrors.ErrNotFound, id)
		}
		return outbox.Emit(ctx, tx, outboxTable, "Region", id, "DELETED", map[string]any{
			"id":    id,
			"actor": actor,
		})
	})
}

var _ region.Repo = (*RegionRepo)(nil)
