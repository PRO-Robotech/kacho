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

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
)

// ImageRepo — реализация image.Reader/Writer поверх pgxpool (handwritten pgx, БЕЗ
// ORM). Within-service инварианты — на DB (source at-most-one mutual-exclusion CHECK,
// source FK SET NULL — provenance, partial UNIQUE(name)); мутации пишут
// fga_register_outbox в той же writer-TX (атомарно, один commit).
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

// imageInsertCoherentSQL — атомарная вставка-если-можно (INSERT…SELECT, ban #10 — НЕ
// Get→check→INSERT): источник (snapshot ЛИБО volume) обязан быть когерентен
// создаваемому Image по ДВУМ осям сразу, и обе проверяются в ТОМ ЖЕ стейтменте, что
// вставка (row-lock FK-проверки), поэтому TOCTOU-окна нет ни у одной.
//
// Ось 1 — ПРОЕКТ. Голого FK недостаточно: он проверяет лишь существование строки,
// поэтому caller мог засеять свой образ ЧУЖИМ приватным снапшотом/томом и вычитать
// содержимое чужого тома (cross-project disclosure/BOLA).
//
// Ось 2 — РАЗМЕЩЕНИЕ. Image — REGIONAL, Volume — ZONAL, и они когерентны только
// когда зона тома принадлежит региону образа (migration 0007 и image.proto заявляют
// именно это). Обратное направление — Volume ИЗ Image — это уже энфорсило; захват
// Image ИЗ Volume не энфорсил ничего, кроме проекта, поэтому region-1-образ можно
// было собрать из region-2-тома: и расхождение с заявленным контрактом, и тихий
// перенос данных через границу размещения.
//
// $10 — зоны, СОСТАВЛЯЮЩИЕ регион образа, полученные у ВЛАДЕЛЬЦА Geography (kacho-geo).
// Регион зоны НИКОГДА не выводится разбором имени (data-integrity.md прямо это
// запрещает: строковая деривация молча даёт пустую строку и превращает проверку в
// no-op). Решение принимает САМ стейтмент, сверяя живую строку источника с этим
// набором, — не Go-код до него.
//
// Снапшот СВОЕЙ зоны не несёт (колонки нет): его единственное свидетельство
// размещения — том, с которого он снят (`snapshots.source_volume_id`). Поэтому для
// snapshot-источника сверяется зона ЭТОГО тома — иначе проверка обходилась бы одним
// лишним шагом (сначала снять снапшот, потом собрать образ). Лineage может быть уже
// NULL (FK на volumes — ON DELETE SET NULL): тогда у снапшота размещения нет вовсе,
// сравнивать не с чем, и придумывать его нельзя — строка проходит (см.
// image_source_region_integration_test.go, где эта граница зафиксирована явно).
//
// size_bytes/min_disk_bytes снимаются с той же уже РАЗРЕШЁННОЙ строки источника.
// Источник не задан (”→NULL, пост-SET-NULL форма) → оба предиката тривиально истинны,
// size 0 — прежнее поведение сохранено.
const imageInsertCoherentSQL = `
	INSERT INTO images
		(id, project_id, name, description, labels, region_id,
		 source_snapshot_id, source_volume_id, size_bytes, min_disk_bytes, format, state)
	SELECT $1::text,$2::text,$3::text,$4::text,$5::jsonb,$6::text,$7::text,$8::text,
	       COALESCE((SELECT s.size_bytes FROM snapshots s WHERE s.id=$7 AND s.project_id=$2),
	                (SELECT v.size_bytes FROM volumes   v WHERE v.id=$8 AND v.project_id=$2), 0),
	       COALESCE((SELECT s.size_bytes FROM snapshots s WHERE s.id=$7 AND s.project_id=$2),
	                (SELECT v.size_bytes FROM volumes   v WHERE v.id=$8 AND v.project_id=$2), 0),
	       $9::text,'READY'
	 WHERE ($7::text IS NULL OR EXISTS (
	            SELECT 1 FROM snapshots s
	             WHERE s.id=$7 AND s.project_id=$2
	               AND (s.source_volume_id IS NULL OR EXISTS (
	                       SELECT 1 FROM volumes lv
	                        WHERE lv.id = s.source_volume_id
	                          AND lv.zone_id = ANY($10::text[])))))
	   AND ($8::text IS NULL OR EXISTS (
	            SELECT 1 FROM volumes v
	             WHERE v.id=$8 AND v.project_id=$2
	               AND v.zone_id = ANY($10::text[])))
	RETURNING created_at, updated_at, size_bytes, min_disk_bytes`

// Insert реализует image.Writer: state=READY сразу; size_bytes/min_disk_bytes derived
// из размера источника (snapshot ЛИБО volume) на INSERT; source_* ”→NULL. Источник
// обязан лежать в ТОМ ЖЕ проекте (project-coherent CAS, imageInsertCoherentSQL) —
// иначе 0 rows → hide-existence "<Resource> <id> not found" (byte-identical настоящему
// miss'у, security.md §6). source FK (23503) / source at-most-one mutual-exclusion
// CHECK (23514) / partial UNIQUE(name) (23505) → контрактные sentinel'ы. exactly-one
// на Create — domain.Validate() (sync). В той же writer-TX (один commit) пишется
// fga_register_outbox (owner-tuple storage_image). Прежняя редакция называла рядом с ним
// доменный outbox — таблица дропнута миграцией 0011, и вставки в неё в этой транзакции
// нет; упоминание описывало запись, которой не происходит.
func (r *ImageRepo) Insert(ctx context.Context, i *domain.Image, regionZones []string) (*domain.Image, error) {
	labels, err := json.Marshal(nonNilLabels(i.Labels))
	if err != nil {
		return nil, storageerr.ErrInternal
	}
	var srcSnap, srcVol *string
	if i.SourceSnapshot != "" {
		srcSnap = &i.SourceSnapshot
	}
	if i.SourceVolume != "" {
		srcVol = &i.SourceVolume
	}
	// nil и пустой срез в `= ANY($10)` ведут себя одинаково (ни одна зона не
	// совпадает), но передаём всегда непустой типизированный срез, чтобы драйвер
	// не отправил NULL: `zone_id = ANY(NULL)` даёт NULL, а не false, и предикат
	// стал бы неопределённым вместо отказа.
	if regionZones == nil {
		regionZones = []string{}
	}
	created := *i
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx, imageInsertCoherentSQL,
			i.ID, i.ProjectID, i.Name, i.Description, labels, i.RegionID,
			srcSnap, srcVol, domain.FormatStandard, regionZones).
			Scan(&created.CreatedAt, &created.UpdatedAt, &created.SizeBytes, &created.MinDiskBytes)
		if serr != nil {
			if errors.Is(serr, pgx.ErrNoRows) {
				return imageSourceUnavailable(i.SourceSnapshot, i.SourceVolume)
			}
			return serr
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

// imageSourceUnavailable разбирает 0-row исход project-coherent CAS: заданный источник
// не резолвится В ПРОЕКТЕ образа — либо его нет вовсе, либо он принадлежит ЧУЖОМУ
// проекту. Оба исхода отдают ОДИН И ТОТ ЖЕ контрактный текст "<Resource> <id> not
// found" (FailedPrecondition) — byte-identical с настоящим FK-miss'ом (security.md §6):
// различимый ответ был бы existence-oracle («чужой ресурс существует»). 0 rows без
// заданного источника невозможно (предикат тривиально истинен) → opaque INTERNAL.
func imageSourceUnavailable(snapshotID, volumeID string) error {
	switch {
	case snapshotID != "":
		return fmt.Errorf("%w: Snapshot %s not found", storageerr.ErrFailedPrecondition, snapshotID)
	case volumeID != "":
		return fmt.Errorf("%w: Volume %s not found", storageerr.ErrFailedPrecondition, volumeID)
	default:
		return storageerr.ErrInternal
	}
}

// Update реализует image.Writer: mutable name/description/labels (COALESCE, nil →
// без изменения). Один UPDATE, БЕЗ Get (нет TOCTOU). 0 rows → NotFound.
func (r *ImageRepo) Update(ctx context.Context, id string, u image.ImageUpdate) (*domain.Image, error) {
	var labelsArg any
	if u.LabelsSet {
		b, err := json.Marshal(nonNilLabels(u.Labels))
		if err != nil {
			return nil, storageerr.ErrInternal
		}
		labelsArg = b
	}
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var (
			rowID       string
			projectID   string
			labelsAfter []byte
		)
		serr := tx.QueryRow(ctx, `UPDATE images SET
				name        = COALESCE($2, name),
				description = COALESCE($3, description),
				labels      = COALESCE($4::jsonb, labels),
				updated_at  = now()
			WHERE id = $1
			RETURNING id, project_id, labels`,
			id, u.Name, u.Description, labelsArg).Scan(&rowID, &projectID, &labelsAfter)
		if serr == nil {
			return reEmitLabelMirror(ctx, tx, u.LabelsSet, labelsAfter,
				func(labels map[string]string) fgaregister.Item {
					return fgaregister.ImageItem(projectID, rowID, labels)
				})
		}
		if errors.Is(serr, pgx.ErrNoRows) {
			return fmt.Errorf("%w: Image %s not found", storageerr.ErrNotFound, id)
		}
		return serr
	})
	if txErr != nil {
		return nil, mapImageErr(txErr, imgErrCtx{imageID: id, imageName: derefStr(u.Name)})
	}
	return r.Get(ctx, id)
}

// Delete реализует image.Writer: DELETE строки образа + fga unregister-intent
// в той же tx. Образ, засевший в томе, удаляется — volumes.
// source_image_id FK ON DELETE SET NULL очищает lineage (STOR-1-28), не RESTRICT.
// 0 rows → NotFound.
func (r *ImageRepo) Delete(ctx context.Context, id string) error {
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var projectID string
		err := tx.QueryRow(ctx, `DELETE FROM images WHERE id = $1 RETURNING project_id`, id).Scan(&projectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: Image %s not found", storageerr.ErrNotFound, id)
			}
			return err
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
