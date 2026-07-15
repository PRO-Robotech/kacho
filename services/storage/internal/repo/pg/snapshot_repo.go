// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/outbox"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/snapshot"
)

// SnapshotRepo — реализация snapshot.Repo поверх pgxpool. Within-service инварианты
// на DB (partial UNIQUE(name), FK SET NULL обе стороны, from-READY-CAS), не software
// TOCTOU. Мутации пишут storage_outbox в той же writer-tx (атомарно, ban #16).
type SnapshotRepo struct {
	pool *pgxpool.Pool
}

// NewSnapshotRepo создаёт SnapshotRepo поверх pgxpool.
func NewSnapshotRepo(pool *pgxpool.Pool) *SnapshotRepo { return &SnapshotRepo{pool: pool} }

// snapshotSelectCols — общий проекционный список для Get/List. source_volume_id
// nullable (FK SET NULL) → COALESCE в ”.
const snapshotSelectCols = `
	id, project_id, created_at, name, description, labels,
	COALESCE(source_volume_id, ''), size_bytes, state`

// scanSnapshot читает одну строку snapshotSelectCols в domain.Snapshot, деривя Status
// из state (§1.4; 1:1, у снапшота нет attach-derive).
func scanSnapshot(row pgx.Row) (*domain.Snapshot, error) {
	var (
		s          domain.Snapshot
		labelsJSON []byte
		state      string
	)
	if err := row.Scan(
		&s.ID, &s.ProjectID, &s.CreatedAt, &s.Name, &s.Description, &labelsJSON,
		&s.SourceVolumeID, &s.SizeBytes, &state,
	); err != nil {
		return nil, err
	}
	if len(labelsJSON) > 0 {
		if err := json.Unmarshal(labelsJSON, &s.Labels); err != nil {
			return nil, err
		}
	}
	s.Status = domain.SnapshotStatusFromState(state)
	return &s, nil
}

// Get реализует snapshot.Repo: снимок по id.
func (r *SnapshotRepo) Get(ctx context.Context, id string) (*domain.Snapshot, error) {
	q := `SELECT ` + snapshotSelectCols + ` FROM snapshots WHERE id = $1`
	s, err := scanSnapshot(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, mapSnapshotErr(err, snapErrCtx{snapshotID: id})
	}
	return s, nil
}

// List реализует snapshot.Repo: cursor-пагинация (created_at,id) ASC, project-scope,
// filter=name. pageSize уже нормализован use-case-слоем.
func (r *SnapshotRepo) List(ctx context.Context, p snapshot.Pagination) ([]*domain.Snapshot, string, error) {
	var (
		conds []string
		args  []any
	)
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if p.ProjectID != "" {
		add("project_id = $%d", p.ProjectID)
	}
	if p.Filter != "" {
		add("name = $%d", p.Filter)
	}
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
	q := fmt.Sprintf(`SELECT %s FROM snapshots %s
		ORDER BY created_at ASC, id ASC LIMIT $%d`, snapshotSelectCols, where, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", mapSnapshotErr(err, snapErrCtx{})
	}
	defer rows.Close()
	var out []*domain.Snapshot
	for rows.Next() {
		s, serr := scanSnapshot(rows)
		if serr != nil {
			return nil, "", mapSnapshotErr(serr, snapErrCtx{})
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, "", mapSnapshotErr(err, snapErrCtx{})
	}
	var next string
	if int64(len(out)) > p.PageSize {
		last := out[p.PageSize-1]
		next = encodePageToken(cursor{createdAt: last.CreatedAt, id: last.ID})
		out = out[:p.PageSize]
	}
	return out, next, nil
}

// snapshotInsertCAS — атомарная вставка-если-можно: source volume существует И READY;
// size_bytes снимается из volumes на момент; state→READY сразу (§1.4). 0 rows →
// disambiguation (том нет / не READY). partial UNIQUE(name) collision → 23505 (не 0-row).
const snapshotInsertCAS = `
	INSERT INTO snapshots (id, project_id, name, description, labels, source_volume_id, size_bytes, state)
	SELECT $1, $2, $3, $4, $5::jsonb, v.id, v.size_bytes, 'READY'
	  FROM volumes v
	 WHERE v.id = $6 AND v.state = 'READY'
	RETURNING created_at, size_bytes`

// Insert реализует snapshot.Repo: from-READY-volume CAS + storage_outbox CREATED в
// той же tx. Никакого Get→check→INSERT (том мог смениться) — только атомарный
// INSERT…SELECT. Existence + state-инвариант — на DB (ban #10).
func (r *SnapshotRepo) Insert(ctx context.Context, s *domain.Snapshot) (*domain.Snapshot, error) {
	labels, err := json.Marshal(nonNilLabels(s.Labels))
	if err != nil {
		return nil, ports.ErrInternal
	}
	created := *s
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx, snapshotInsertCAS,
			s.ID, s.ProjectID, s.Name, s.Description, labels, s.SourceVolumeID).
			Scan(&created.CreatedAt, &created.SizeBytes)
		if serr == nil {
			if oerr := outbox.Emit(ctx, tx, outboxTable, "Snapshot", s.ID, "CREATED", map[string]any{
				"id":               s.ID,
				"project_id":       s.ProjectID,
				"source_volume_id": s.SourceVolumeID,
			}); oerr != nil {
				return oerr
			}
			// owner-tuple register-intent в той же writer-TX (SEC-D): project#project@storage_snapshot.
			return emitFGARegister(ctx, tx, fgaregister.EventRegister,
				fgaregister.SnapshotItem(s.ProjectID, s.ID, s.Labels))
		}
		if !errors.Is(serr, pgx.ErrNoRows) {
			return serr // 23505 name-collision / 23514 CHECK → mapSnapshotErr снаружи
		}
		return disambiguateSnapshotSource(ctx, tx, s.SourceVolumeID) // 0 rows → sentinel
	})
	if txErr != nil {
		return nil, mapSnapshotErr(txErr, snapErrCtx{
			snapshotID: s.ID, snapshotName: s.Name, sourceVolumeID: s.SourceVolumeID,
		})
	}
	created.Status = domain.SnapshotStatusFromState("READY")
	return &created, nil
}

// disambiguateSnapshotSource разбирает 0-row исход from-READY-CAS (в той же tx): том
// не существует → "Volume <id> not found"; существует, но state != READY → "Volume
// <id> is not ready" (оба FailedPrecondition — existence same-DB, не cross-service).
func disambiguateSnapshotSource(ctx context.Context, tx pgx.Tx, srcVolumeID string) error {
	var state string
	verr := tx.QueryRow(ctx, `SELECT state FROM volumes WHERE id = $1`, srcVolumeID).Scan(&state)
	if verr != nil {
		if errors.Is(verr, pgx.ErrNoRows) {
			return fmt.Errorf("%w: Volume %s not found", ports.ErrFailedPrecondition, srcVolumeID)
		}
		return verr
	}
	if state != "READY" {
		return fmt.Errorf("%w: Volume %s is not ready", ports.ErrFailedPrecondition, srcVolumeID)
	}
	// READY, том есть, но 0 rows — состояние сменилось между INSERT и disambiguation. Opaque.
	return ports.ErrInternal
}

// Update реализует snapshot.Repo: mutable name/description/labels (COALESCE, nil →
// без изменения) + storage_outbox UPDATED в той же tx. 0 rows → NotFound. partial
// UNIQUE(name) collision → 23505 → AlreadyExists.
func (r *SnapshotRepo) Update(ctx context.Context, id string, u snapshot.SnapshotUpdate) (*domain.Snapshot, error) {
	var labelsArg any
	if u.LabelsSet {
		b, err := json.Marshal(nonNilLabels(u.Labels))
		if err != nil {
			return nil, ports.ErrInternal
		}
		labelsArg = b
	}
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var rowID string
		serr := tx.QueryRow(ctx, `UPDATE snapshots SET
				name        = COALESCE($2, name),
				description = COALESCE($3, description),
				labels      = COALESCE($4::jsonb, labels),
				updated_at  = now()
			WHERE id = $1
			RETURNING id`, id, u.Name, u.Description, labelsArg).Scan(&rowID)
		if serr == nil {
			return outbox.Emit(ctx, tx, outboxTable, "Snapshot", id, "UPDATED", map[string]any{"id": id})
		}
		if errors.Is(serr, pgx.ErrNoRows) {
			return fmt.Errorf("%w: Snapshot %s not found", ports.ErrNotFound, id)
		}
		return serr
	})
	if txErr != nil {
		return nil, mapSnapshotErr(txErr, snapErrCtx{snapshotID: id, snapshotName: derefStr(u.Name)})
	}
	return r.Get(ctx, id)
}

// Delete реализует snapshot.Repo: DELETE строки + storage_outbox DELETED в той же tx.
// Ссылки volumes.source_snapshot_id → SET NULL (не RESTRICT) — delete НЕ блокируется
// (§1.2, S1-09). 0 rows → NotFound.
func (r *SnapshotRepo) Delete(ctx context.Context, id string) error {
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// RETURNING project_id — нужен для unregister owner-tuple; 0 rows → NotFound.
		var projectID string
		err := tx.QueryRow(ctx, `DELETE FROM snapshots WHERE id = $1 RETURNING project_id`, id).Scan(&projectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: Snapshot %s not found", ports.ErrNotFound, id)
			}
			return err
		}
		if oerr := outbox.Emit(ctx, tx, outboxTable, "Snapshot", id, "DELETED", map[string]any{"id": id}); oerr != nil {
			return oerr
		}
		// owner-tuple unregister-intent в той же writer-TX (SEC-D).
		return emitFGARegister(ctx, tx, fgaregister.EventUnregister,
			fgaregister.Item{Tuple: fgaregister.StorageSnapshot(projectID, id)})
	})
	if txErr != nil {
		return mapSnapshotErr(txErr, snapErrCtx{snapshotID: id})
	}
	return nil
}

var _ snapshot.Repo = (*SnapshotRepo)(nil)
