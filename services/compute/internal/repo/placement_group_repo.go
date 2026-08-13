// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// ErrPlacementGroupInUse — группа держит машины и потому не снимается.
//
// Несёт ПЕРЕЧЕНЬ машин: арендатор видит радиус до отказа, а не выясняет его
// перебором.
type ErrPlacementGroupInUse struct {
	GroupID     string
	InstanceIDs []string
	Truncated   bool
}

func (e *ErrPlacementGroupInUse) Error() string {
	tail := ""
	if e.Truncated {
		tail = " and more"
	}
	return fmt.Sprintf("placement group %s still holds instances: %s%s",
		e.GroupID, strings.Join(e.InstanceIDs, ", "), tail)
}

// Unwrap делает отказ отображаемым общей таблицей: состояние ресурса не
// позволяет операцию.
func (e *ErrPlacementGroupInUse) Unwrap() error { return ports.ErrFailedPrecondition }

// PlacementGroupRepo — хранение групп размещения.
type PlacementGroupRepo struct {
	pool *pgxpool.Pool
}

// NewPlacementGroupRepo создаёт репозиторий групп.
func NewPlacementGroupRepo(pool *pgxpool.Pool) *PlacementGroupRepo {
	return &PlacementGroupRepo{pool: pool}
}

const placementGroupCols = `id, project_id, name, description, labels, created_at, ` +
	`strategy, placement_type, zone_id, region_id`

func scanPlacementGroup(row pgx.Row) (*domain.PlacementGroup, error) {
	var g domain.PlacementGroup
	var labels []byte
	var strategy, placementType string
	if err := row.Scan(&g.ID, &g.ProjectID, &g.Name, &g.Description, &labels, &g.CreatedAt,
		&strategy, &placementType, &g.ZoneID, &g.RegionID); err != nil {
		return nil, err
	}
	if err := unmarshalJSONB(labels, &g.Labels, "PlacementGroup.labels"); err != nil {
		return nil, err
	}
	// Неизвестное имя из базы даёт НУЛЕВОЕ значение, а не догадку: схема
	// ограничена тем же набором, поэтому расхождение здесь означало бы, что
	// набор разъехался, — и молча подставить ближайшее значило бы это скрыть.
	g.Strategy, _ = domain.ParsePlacementStrategy(strategy)
	g.PlacementType, _ = domain.ParsePlacementType(placementType)
	return &g, nil
}

// Get возвращает группу по идентификатору.
func (r *PlacementGroupRepo) Get(ctx context.Context, id string) (*domain.PlacementGroup, error) {
	q := fmt.Sprintf(`SELECT %s FROM placement_groups WHERE id = $1`, placementGroupCols)
	g, err := scanPlacementGroup(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, wrapPgErr(err, "PlacementGroup", id)
	}
	return g, nil
}

// List возвращает страницу групп проекта тем же курсором, что у прочих ресурсов.
func (r *PlacementGroupRepo) List(ctx context.Context, projectID string, p ports.Pagination) ([]*domain.PlacementGroup, string, error) {
	pageSize, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}

	args := []any{projectID}
	where := "WHERE project_id = $1"
	if p.PageToken != "" {
		tsv, id, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", invalidPageTokenErr(derr)
		}
		where += " AND (created_at, id) > ($2, $3)"
		args = append(args, tsv, id)
	}
	q := fmt.Sprintf(`SELECT %s FROM placement_groups %s ORDER BY created_at ASC, id ASC LIMIT $%d`,
		placementGroupCols, where, len(args)+1)
	args = append(args, pageSize+1)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", wrapPgErr(err, "PlacementGroup", "")
	}
	defer rows.Close()

	var out []*domain.PlacementGroup
	for rows.Next() {
		g, serr := scanPlacementGroup(rows)
		if serr != nil {
			return nil, "", wrapPgErr(serr, "PlacementGroup", "")
		}
		out = append(out, g)
	}
	if rows.Err() != nil {
		return nil, "", wrapPgErr(rows.Err(), "PlacementGroup", "")
	}

	var next string
	if int64(len(out)) > pageSize {
		last := out[pageSize-1]
		next = encodePageToken(last.CreatedAt, last.ID)
		out = out[:pageSize]
	}
	return out, next, nil
}

// Insert записывает группу, журнал и намерение регистрации прав одной транзакцией.
func (r *PlacementGroupRepo) Insert(ctx context.Context, g *domain.PlacementGroup) (*domain.PlacementGroup, []ownerregister.Registration, error) {
	labelsJSON, err := marshalJSONB(orEmptyMap(g.Labels), "PlacementGroup.labels")
	if err != nil {
		return nil, nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := fmt.Sprintf(`INSERT INTO placement_groups
		(id, project_id, name, description, labels, created_at, strategy, placement_type, zone_id, region_id)
		VALUES ($1,$2,$3,$4,$5,now(),$6,$7,$8,$9) RETURNING %s`, placementGroupCols)
	created, err := scanPlacementGroup(tx.QueryRow(ctx, q,
		g.ID, g.ProjectID, g.Name, g.Description, labelsJSON,
		g.Strategy.StrategyName(), g.PlacementType.PlacementTypeName(), g.ZoneID, g.RegionID))
	if err != nil {
		return nil, nil, wrapPgErr(err, "PlacementGroup", g.Name)
	}

	actor, onBehalf := auditPrincipals(ctx)
	if err := emitAudit(ctx, tx, AuditEvent{
		EventType:    "placement_group.create",
		ResourceType: "PlacementGroup",
		ResourceID:   created.ID,
		ProjectID:    created.ProjectID,
		Actor:        actor,
		OnBehalfOf:   onBehalf,
		Payload: map[string]any{
			"name": created.Name, "strategy": created.Strategy.StrategyName(),
			"placement_type": created.PlacementType.PlacementTypeName(),
		},
	}); err != nil {
		return nil, nil, ports.ErrInternal
	}

	reg, err := emitFGARegisterIntent(ctx, tx, fgaintent.EventRegister, "PlacementGroup",
		created.ID, created.ProjectID, created.Labels)
	if err != nil {
		return nil, nil, ports.ErrInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, wrapPgErr(err, "PlacementGroup", g.Name)
	}
	return created, registrationsOf(reg), nil
}

// Update пишет ТОЛЬКО названные маской колонки одним стейтментом.
//
// Стратегии и якоря в наборе нет by construction: смена любого из них поменяла
// бы смысл размещения уже стоящих машин, а перекладывать их задним числом мы не
// будем.
func (r *PlacementGroupRepo) Update(ctx context.Context, id string, u ports.PlacementGroupUpdate) (*domain.PlacementGroup, error) {
	if !u.Touched() {
		return r.Get(ctx, id)
	}

	set := make([]string, 0, 3)
	args := []any{id}
	add := func(col string, val any) {
		args = append(args, val)
		set = append(set, fmt.Sprintf("%s=$%d", col, len(args)))
	}
	if u.Name != nil {
		add("name", *u.Name)
	}
	if u.Description != nil {
		add("description", *u.Description)
	}
	if u.LabelsSet {
		labelsJSON, err := marshalJSONB(orEmptyMap(u.Labels), "PlacementGroup.labels")
		if err != nil {
			return nil, err
		}
		add("labels", labelsJSON)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := fmt.Sprintf(`UPDATE placement_groups SET %s WHERE id=$1 RETURNING %s`,
		strings.Join(set, ", "), placementGroupCols)
	updated, err := scanPlacementGroup(tx.QueryRow(ctx, q, args...))
	if err != nil {
		return nil, wrapPgErr(err, "PlacementGroup", id)
	}

	actor, onBehalf := auditPrincipals(ctx)
	if err := emitAudit(ctx, tx, AuditEvent{
		EventType:    "placement_group.update",
		ResourceType: "PlacementGroup",
		ResourceID:   updated.ID,
		ProjectID:    updated.ProjectID,
		Actor:        actor,
		OnBehalfOf:   onBehalf,
		Payload:      map[string]any{"name": updated.Name},
	}); err != nil {
		return nil, ports.ErrInternal
	}
	if _, err := emitFGARegisterIntent(ctx, tx, fgaintent.EventRegister, "PlacementGroup",
		updated.ID, updated.ProjectID, updated.Labels); err != nil {
		return nil, ports.ErrInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, wrapPgErr(err, "PlacementGroup", id)
	}
	return updated, nil
}

// Delete снимает группу.
//
// Группа, в которой стоят машины, не снимается, и отказ называет машины.
// Ссылочная целостность отвергла бы снятие и сама, но её отказ называет
// ограничение, а не машины — по нему нельзя сделать следующего шага.
func (r *PlacementGroupRepo) Delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	holders, truncated, err := instancesInPlacementGroup(ctx, tx, id, maxNamedHolders)
	if err != nil {
		return err
	}
	if len(holders) > 0 {
		return &ErrPlacementGroupInUse{GroupID: id, InstanceIDs: holders, Truncated: truncated}
	}

	var projectID string
	err = tx.QueryRow(ctx, `DELETE FROM placement_groups WHERE id = $1 RETURNING project_id`, id).Scan(&projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: PlacementGroup %s not found", ports.ErrNotFound, id)
		}
		return wrapPgErr(err, "PlacementGroup", id)
	}

	actor, onBehalf := auditPrincipals(ctx)
	if err := emitAudit(ctx, tx, AuditEvent{
		EventType:    "placement_group.delete",
		ResourceType: "PlacementGroup",
		ResourceID:   id,
		ProjectID:    projectID,
		Actor:        actor,
		OnBehalfOf:   onBehalf,
	}); err != nil {
		return ports.ErrInternal
	}
	if _, err := emitFGARegisterIntent(ctx, tx, fgaintent.EventUnregister, "PlacementGroup",
		id, projectID, nil); err != nil {
		return ports.ErrInternal
	}
	return tx.Commit(ctx)
}

// instancesInPlacementGroup — машины, стоящие в группе.
func instancesInPlacementGroup(ctx context.Context, q pgx.Tx, groupID string, limit int) ([]string, bool, error) {
	rows, err := q.Query(ctx,
		`SELECT id FROM instances WHERE placement_group_id = $1 ORDER BY id LIMIT $2`, groupID, limit+1)
	if err != nil {
		return nil, false, wrapPgErr(err, "PlacementGroup", groupID)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if serr := rows.Scan(&id); serr != nil {
			return nil, false, wrapPgErr(serr, "PlacementGroup", groupID)
		}
		out = append(out, id)
	}
	if rows.Err() != nil {
		return nil, false, wrapPgErr(rows.Err(), "PlacementGroup", groupID)
	}
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}
