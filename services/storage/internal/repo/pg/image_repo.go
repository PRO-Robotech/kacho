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
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/image"
)

// ImageRepo — реализация image.Reader/Writer поверх pgxpool (handwritten pgx, БЕЗ
// ORM). Within-service инварианты — на DB (source at-most-one mutual-exclusion CHECK,
// source FK SET NULL — provenance, partial UNIQUE(name)); мутации пишут storage_outbox
// + fga_register_outbox в той же writer-TX (атомарно, один commit).
type ImageRepo struct {
	pool *pgxpool.Pool
}

// NewImageRepo создаёт ImageRepo поверх pgxpool.
func NewImageRepo(pool *pgxpool.Pool) *ImageRepo { return &ImageRepo{pool: pool} }

// imageSelectCols — проекционный список для Get/List. Image — всегда REGIONAL
// (placement const), поэтому колонки placement нет; source_* nullable → COALESCE ”.
const imageSelectCols = `
	i.id, i.project_id, i.created_at, i.updated_at, i.name, i.description, i.labels,
	i.region_id, COALESCE(i.source_snapshot_id, ''), COALESCE(i.source_volume_id, ''),
	i.size_bytes, i.min_disk_bytes, i.format, i.state`

// scanImage читает одну строку проекции imageSelectCols в domain.Image, деривя
// Status/Format/Placement (Image всегда REGIONAL; format single-tier STANDARD).
func scanImage(row pgx.Row) (*domain.Image, error) {
	var (
		i          domain.Image
		labelsJSON []byte
		format     string
		state      string
	)
	if err := row.Scan(
		&i.ID, &i.ProjectID, &i.CreatedAt, &i.UpdatedAt, &i.Name, &i.Description, &labelsJSON,
		&i.RegionID, &i.SourceSnapshot, &i.SourceVolume, &i.SizeBytes, &i.MinDiskBytes, &format, &state,
	); err != nil {
		return nil, err
	}
	if len(labelsJSON) > 0 {
		if err := json.Unmarshal(labelsJSON, &i.Labels); err != nil {
			return nil, err
		}
	}
	i.Placement = domain.ImagePlacementRegional
	i.Format = imageFormatFromDB(format)
	i.Status = domain.ImageStatusFromState(state)
	return &i, nil
}

// Get реализует image.Reader: образ по id.
func (r *ImageRepo) Get(ctx context.Context, id string) (*domain.Image, error) {
	q := `SELECT ` + imageSelectCols + ` FROM images i WHERE i.id = $1`
	i, err := scanImage(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, mapImageErr(err, imgErrCtx{imageID: id})
	}
	return i, nil
}

// List реализует image.Reader: cursor-пагинация (created_at,id) ASC, project-scope
// (WHERE i.project_id = $ — listauthz posture, make audit-list-filter) и filter=name.
func (r *ImageRepo) List(ctx context.Context, p image.Pagination) ([]*domain.Image, string, error) {
	var (
		conds []string
		args  []any
	)
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if p.ProjectID != "" {
		add("i.project_id = $%d", p.ProjectID)
	}
	if p.Filter != "" {
		add("i.name = $%d", p.Filter)
	}
	if p.PageToken != "" {
		cur, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", derr
		}
		args = append(args, cur.createdAt, cur.id)
		conds = append(conds, fmt.Sprintf("(i.created_at, i.id) > ($%d, $%d)", len(args)-1, len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, p.PageSize+1)
	q := fmt.Sprintf(`SELECT %s FROM images i %s
		ORDER BY i.created_at ASC, i.id ASC
		LIMIT $%d`, imageSelectCols, where, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", mapImageErr(err, imgErrCtx{})
	}
	defer rows.Close()
	var out []*domain.Image
	for rows.Next() {
		i, serr := scanImage(rows)
		if serr != nil {
			return nil, "", mapImageErr(serr, imgErrCtx{})
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, "", mapImageErr(err, imgErrCtx{})
	}
	var next string
	if int64(len(out)) > p.PageSize {
		last := out[p.PageSize-1]
		next = encodePageToken(cursor{createdAt: last.CreatedAt, id: last.ID})
		out = out[:p.PageSize]
	}
	return out, next, nil
}

// Insert реализует image.Writer: state=READY сразу; size_bytes/min_disk_bytes derived
// из размера источника (snapshot ЛИБО volume) на INSERT; source_* ”→NULL. source FK
// (23503) / source at-most-one mutual-exclusion CHECK (23514) / partial UNIQUE(name)
// (23505) → контрактные sentinel'ы. exactly-one на Create — domain.Validate() (sync).
// storage_outbox CREATED + fga_register_outbox (owner-tuple
// storage_image) — та же writer-TX (один commit).
func (r *ImageRepo) Insert(ctx context.Context, i *domain.Image) (*domain.Image, error) {
	labels, err := json.Marshal(nonNilLabels(i.Labels))
	if err != nil {
		return nil, ports.ErrInternal
	}
	var srcSnap, srcVol *string
	if i.SourceSnapshot != "" {
		srcSnap = &i.SourceSnapshot
	}
	if i.SourceVolume != "" {
		srcVol = &i.SourceVolume
	}
	created := *i
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx, `
			INSERT INTO images
				(id, project_id, name, description, labels, region_id,
				 source_snapshot_id, source_volume_id, size_bytes, min_disk_bytes, format, state)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,
				COALESCE((SELECT size_bytes FROM snapshots WHERE id=$7),(SELECT size_bytes FROM volumes WHERE id=$8),0),
				COALESCE((SELECT size_bytes FROM snapshots WHERE id=$7),(SELECT size_bytes FROM volumes WHERE id=$8),0),
				$9,'READY')
			RETURNING created_at, updated_at, size_bytes, min_disk_bytes`,
			i.ID, i.ProjectID, i.Name, i.Description, labels, i.RegionID,
			srcSnap, srcVol, domain.FormatStandard).
			Scan(&created.CreatedAt, &created.UpdatedAt, &created.SizeBytes, &created.MinDiskBytes)
		if serr != nil {
			return serr
		}
		if oerr := outbox.Emit(ctx, tx, outboxTable, "Image", i.ID, "CREATED", map[string]any{
			"id":         i.ID,
			"project_id": i.ProjectID,
			"region_id":  i.RegionID,
		}); oerr != nil {
			return oerr
		}
		// owner-tuple register-intent в той же writer-TX (F13/STOR-1-27): анти-BOLA.
		return emitFGARegister(ctx, tx, fgaregister.EventRegister,
			fgaregister.ImageItem(i.ProjectID, i.ID, i.Labels))
	})
	if txErr != nil {
		return nil, mapImageErr(txErr, imgErrCtx{
			imageID: i.ID, imageName: i.Name, snapshotID: i.SourceSnapshot, volumeID: i.SourceVolume,
		})
	}
	created.Format = domain.ImageFormatStandard
	created.Placement = domain.ImagePlacementRegional
	created.Status = domain.ImageStatusFromState("READY")
	return &created, nil
}

// Update реализует image.Writer: mutable name/description/labels (COALESCE, nil →
// без изменения). Один UPDATE, БЕЗ Get (нет TOCTOU). 0 rows → NotFound.
// storage_outbox UPDATED в той же tx.
func (r *ImageRepo) Update(ctx context.Context, id string, u image.ImageUpdate) (*domain.Image, error) {
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
		serr := tx.QueryRow(ctx, `UPDATE images SET
				name        = COALESCE($2, name),
				description = COALESCE($3, description),
				labels      = COALESCE($4::jsonb, labels),
				updated_at  = now()
			WHERE id = $1
			RETURNING id`, id, u.Name, u.Description, labelsArg).Scan(&rowID)
		if serr == nil {
			return outbox.Emit(ctx, tx, outboxTable, "Image", id, "UPDATED", map[string]any{"id": id})
		}
		if errors.Is(serr, pgx.ErrNoRows) {
			return fmt.Errorf("%w: Image %s not found", ports.ErrNotFound, id)
		}
		return serr
	})
	if txErr != nil {
		return nil, mapImageErr(txErr, imgErrCtx{imageID: id, imageName: derefStr(u.Name)})
	}
	return r.Get(ctx, id)
}

// Delete реализует image.Writer: DELETE строки образа + storage_outbox DELETED +
// fga unregister в той же tx. Образ, засевший в томе, удаляется — volumes.
// source_image_id FK ON DELETE SET NULL очищает lineage (STOR-1-28), не RESTRICT.
// 0 rows → NotFound.
func (r *ImageRepo) Delete(ctx context.Context, id string) error {
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var projectID string
		err := tx.QueryRow(ctx, `DELETE FROM images WHERE id = $1 RETURNING project_id`, id).Scan(&projectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: Image %s not found", ports.ErrNotFound, id)
			}
			return err
		}
		if oerr := outbox.Emit(ctx, tx, outboxTable, "Image", id, "DELETED", map[string]any{"id": id}); oerr != nil {
			return oerr
		}
		return emitFGARegister(ctx, tx, fgaregister.EventUnregister,
			fgaregister.Item{Tuple: fgaregister.StorageImage(projectID, id)})
	})
	if txErr != nil {
		return mapImageErr(txErr, imgErrCtx{imageID: id})
	}
	return nil
}

// GetInternal реализует image.Reader (full infra-проекция, :9091). Инфра-поля —
// будущий data-plane инкремент (reserved в ImageInternal); сейчас возвращает публичную
// проекцию, которую handler оборачивает в ImageInternal (STOR-1-25).
func (r *ImageRepo) GetInternal(ctx context.Context, id string) (*domain.Image, error) {
	return r.Get(ctx, id)
}

// imageFormatFromDB маппит text-колонку format → domain.ImageFormat (single-tier).
func imageFormatFromDB(s string) domain.ImageFormat {
	if s == domain.FormatStandard {
		return domain.ImageFormatStandard
	}
	return domain.ImageFormatUnspecified
}

var (
	_ image.Reader = (*ImageRepo)(nil)
	_ image.Writer = (*ImageRepo)(nil)
)
