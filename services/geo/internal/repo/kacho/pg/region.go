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
// наблюдаемый маркер в geo_outbox, а НЕ пустую строку, чтобы утрата атрибуции
// была видна в самой audit-строке при разборе инцидента (CWE-778). В штатном
// no-auth пути этой ветки нет: PrincipalFromContext отдаёт SystemPrincipal
// (system:bootstrap), а не пустой ID.
const actorUnknown = "unknown"

// actorFromCtx форматирует trusted principal вызывающего как "<type>:<id>" для
// audit-payload (например "user:usr_...", "service_account:sva_..."). Пустой ID
// (явно выставленный principal без ID) → actorUnknown-sentinel, а не blank.
func actorFromCtx(ctx context.Context) string {
	p := operations.PrincipalFromContext(ctx)
	if p.ID == "" {
		return actorUnknown
	}
	return p.Type + ":" + p.ID
}

// RegionRepo — реализация region.Repo поверх pgx.
type RegionRepo struct {
	pool *pgxpool.Pool
}

// NewRegionRepo создает RegionRepo поверх pgxpool.
func NewRegionRepo(pool *pgxpool.Pool) *RegionRepo { return &RegionRepo{pool: pool} }

// Get возвращает регион по id.
func (r *RegionRepo) Get(ctx context.Context, id string) (*domain.Region, error) {
	var rg domain.Region
	err := r.pool.QueryRow(ctx, `SELECT id, name, created_at FROM regions WHERE id = $1`, id).
		Scan(&rg.ID, &rg.Name, &rg.CreatedAt)
	if err != nil {
		return nil, dberr.Wrap(err, "Region", id)
	}
	return &rg, nil
}

// List возвращает регионы с курсорной пагинацией по id. pageSize уже
// нормализован use-case-слоем (region.UseCase.List), repo его не валидирует.
func (r *RegionRepo) List(ctx context.Context, p region.Pagination) ([]*domain.Region, string, error) {
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
	q := fmt.Sprintf(`SELECT id, name, created_at FROM regions %s ORDER BY id ASC LIMIT $%d`, where, len(args)+1)
	args = append(args, pageSize+1)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", dberr.Wrap(err, "Region", "")
	}
	defer rows.Close()
	var out []*domain.Region
	for rows.Next() {
		var rg domain.Region
		if err := rows.Scan(&rg.ID, &rg.Name, &rg.CreatedAt); err != nil {
			return nil, "", dberr.Wrap(err, "Region", "")
		}
		out = append(out, &rg)
	}
	if err := rows.Err(); err != nil {
		return nil, "", dberr.Wrap(err, "Region", "")
	}
	var next string
	if int64(len(out)) > pageSize {
		last := out[pageSize-1]
		next = encodePageToken(last.ID)
		out = out[:pageSize]
	}
	return out, next, nil
}

// Insert создает регион (admin-only) и пишет geo_outbox-строку CREATED в той же
// tx (атомарно). Audit-payload фиксирует actor admin-мутации.
func (r *RegionRepo) Insert(ctx context.Context, rg *domain.Region) (*domain.Region, error) {
	actor := actorFromCtx(ctx)
	var created domain.Region
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx,
			`INSERT INTO regions (id, name, created_at) VALUES ($1,$2,$3) RETURNING id, name, created_at`,
			rg.ID, rg.Name, time.Now().UTC()).
			Scan(&created.ID, &created.Name, &created.CreatedAt)
		if serr != nil {
			return dberr.Wrap(serr, "Region", rg.ID)
		}
		return outbox.Emit(ctx, tx, outboxTable, "Region", created.ID, "CREATED", map[string]any{
			"id":    created.ID,
			"name":  created.Name,
			"actor": actor,
		})
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// Update меняет name региона (admin-only) одним атомарным statement (без
// предварительного Get / TOCTOU) + пишет geo_outbox UPDATED в той же tx.
// name=nil → поле не меняется (COALESCE). 0 rows из RETURNING → ErrNotFound.
func (r *RegionRepo) Update(ctx context.Context, id string, name *string) (*domain.Region, error) {
	actor := actorFromCtx(ctx)
	var updated domain.Region
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx,
			`UPDATE regions SET name = COALESCE($2, name) WHERE id=$1 RETURNING id, name, created_at`,
			id, name).
			Scan(&updated.ID, &updated.Name, &updated.CreatedAt)
		if serr != nil {
			return dberr.Wrap(serr, "Region", id)
		}
		return outbox.Emit(ctx, tx, outboxTable, "Region", updated.ID, "UPDATED", map[string]any{
			"id":    updated.ID,
			"name":  updated.Name,
			"actor": actor,
		})
	})
	if err != nil {
		return nil, err
	}
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
