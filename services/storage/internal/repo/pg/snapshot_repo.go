// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
)

// SnapshotRepo — реализация snapshot.Repo поверх pgxpool. Within-service инварианты
// на DB (partial UNIQUE(name), FK SET NULL обе стороны, from-READY-CAS), не software
// TOCTOU. Owner-tuple-intent пишется в fga_register_outbox в той же writer-tx
// (атомарно, ban #16).
type SnapshotRepo struct {
	pool *pgxpool.Pool
}

// NewSnapshotRepo создаёт SnapshotRepo поверх pgxpool.
func NewSnapshotRepo(pool *pgxpool.Pool) *SnapshotRepo { return &SnapshotRepo{pool: pool} }

// snapshotSelectCols — общий проекционный список для Get/List.
//
// source_volume_id nullable (FK SET NULL) → COALESCE в ”. Размещение, ревизия
// политики и имя объекта читаются ИЗ СВОЕЙ строки, а не добираются через том:
// ссылка на том обнуляется при его удалении, и всё, что берётся через неё,
// исчезает у пережившего снимка ровно тогда, когда становится нужным.
//
// Перечень засеянных томов собирается ОДНИМ коррелированным подзапросом, а не
// вторым обращением на строку: страница по контракту доходит до тысячи строк, и
// запрос на строку превратил бы её в тысячу обращений. Индекс
// volumes_source_snapshot_idx заведён миграцией 0003 именно под этот вывод.
const snapshotSelectCols = `
	s.id, s.project_id, s.created_at, s.updated_at, s.name, s.description, s.labels,
	COALESCE(s.source_volume_id, ''), s.size_bytes, s.state, s.zone_id, s.status_reason,
	COALESCE(s.binding_id, ''), COALESCE(s.backend_object, ''),
	COALESCE(b.namespace_template, ''),
	COALESCE((SELECT array_agg(sv.id ORDER BY sv.created_at, sv.id)
	            FROM volumes sv WHERE sv.source_snapshot_id = s.id), '{}')`

// snapshotFrom — источник строк проекции. Ревизия политики подтягивается слева:
// у строк прежней схемы её нет, и отсутствие ревизии не должно скрывать сам снимок.
const snapshotFrom = `
	FROM snapshots s
	LEFT JOIN disk_type_bindings b ON b.id = s.binding_id`

// scanSnapshot читает одну строку snapshotSelectCols в domain.Snapshot, деривя Status
// из state (1:1, у снимка нет attach-derive).
//
// Единица изоляции арендатора не хранится колонкой, а ВЫВОДИТСЯ из шаблона
// унаследованной ревизии и собственного проекта. Обе величины на снимке неизменяемы,
// поэтому вывод даёт ровно то пространство, в котором лежит том-источник, и
// разойтись с ним не может; хранимая копия такой гарантии не даёт — она бы просто
// стала вторым местом об одном факте. Без ревизии пространства нет вовсе: назвать
// его было бы утверждением об объекте, которого никто не создавал.
func scanSnapshot(row pgx.Row) (*domain.Snapshot, error) {
	var (
		s          domain.Snapshot
		labelsJSON []byte
		state      string
		reason     string
		bindingID  string
		backendObj string
		nsTemplate string
		seeded     []string
	)
	if err := row.Scan(
		&s.ID, &s.ProjectID, &s.CreatedAt, &s.UpdatedAt, &s.Name, &s.Description, &labelsJSON,
		&s.SourceVolumeID, &s.SizeBytes, &state, &s.ZoneID, &reason,
		&bindingID, &backendObj, &nsTemplate, &seeded,
	); err != nil {
		return nil, err
	}
	if len(labelsJSON) > 0 {
		if err := json.Unmarshal(labelsJSON, &s.Labels); err != nil {
			return nil, err
		}
	}
	s.Status = domain.SnapshotStatusFromState(state)
	s.StatusReason = domain.StatusReason(reason)
	s.Backend.BindingID = bindingID
	s.Backend.BackendObject = backendObj
	if bindingID != "" {
		s.Backend.BackendNamespace = blockbackend.NamespaceOfProject(nsTemplate, s.ProjectID)
	}
	s.SeededVolumeIDs = seeded
	return &s, nil
}

// Get реализует snapshot.Repo: снимок по id.
func (r *SnapshotRepo) Get(ctx context.Context, id string) (*domain.Snapshot, error) {
	q := `SELECT ` + snapshotSelectCols + snapshotFrom + ` WHERE s.id = $1`
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
		add("s.project_id = $%d", p.ProjectID)
	}
	if p.Filter != "" {
		add("s.name = $%d", p.Filter)
	}
	if p.PageToken != "" {
		cur, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", derr
		}
		args = append(args, cur.createdAt, cur.id)
		conds = append(conds, fmt.Sprintf("(s.created_at, s.id) > ($%d, $%d)", len(args)-1, len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, p.PageSize+1)
	q := fmt.Sprintf(`SELECT %s%s %s
		ORDER BY s.created_at ASC, s.id ASC LIMIT $%d`, snapshotSelectCols, snapshotFrom, where, len(args))

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

// snapshotInsertCAS — атомарная вставка-если-можно: том-источник существует, лежит В
// ТОМ ЖЕ проекте, что снимок, И готов; size_bytes, ЗОНА и ревизия политики снимаются
// с той же строки тома ТЕМ ЖЕ стейтментом.
//
// Размещение и политика берутся здесь, а не вторым запросом, по той же причине, по
// какой здесь же проверяется готовность: между чтением и записью том может смениться,
// и снимок унаследовал бы зону, которой у источника уже нет. Своя зона снимку нужна
// потому, что он ПЕРЕЖИВАЕТ том: ссылка на источник обнуляется, и добираемая через
// неё зона исчезает — а с ней вырождается в тождественно истинную проверка
// когерентности при восстановлении.
//
// Состояние рождения — СОЗДАВАЕМЫЙ. Готовым снимок объявляет сверщик, увидев объект
// у бэкенда: операция фиксирует намерение, а исход провижининга несёт статус
// ресурса. Объявить готовность здесь значило бы утверждать о плоскости данных то,
// чего никто не проверял, — и засев тома из такого снимка склонировал бы объект,
// которого ещё нет.
//
// project-предикат обязателен: без него caller снимал бы снимок с ЧУЖОГО приватного
// тома и вычитывал его содержимое (cross-project disclosure/BOLA). 0 rows →
// disambiguation (том не резолвится в проекте / не готов). partial UNIQUE(name)
// collision → 23505 (не 0-row).
const snapshotInsertCAS = `
	INSERT INTO snapshots (id, project_id, name, description, labels, source_volume_id,
	                       size_bytes, state, zone_id, status_reason, binding_id, backend_object)
	SELECT $1, $2, $3, $4, $5::jsonb, v.id, v.size_bytes, 'CREATING',
	       v.zone_id, $7::text, v.binding_id, $8::text
	  FROM volumes v
	 WHERE v.id = $6 AND v.project_id = $2 AND v.state = 'READY'
	RETURNING created_at, updated_at, size_bytes, zone_id, COALESCE(binding_id, '')`

// Insert реализует snapshot.Repo: from-READY-volume CAS + fga_register-intent в
// той же tx. Никакого Get→check→INSERT (том мог смениться) — только атомарный
// INSERT…SELECT. Existence, готовность источника, зона и ревизия политики — на DB
// (ban #10).
//
// Имя объекта у бэкенда приходит от use-case: оно выводится из НЕИЗМЕНЯЕМОГО
// идентификатора снимка и префикса установки, поэтому повтор идемпотентен by
// construction. Пустое имя пишется NULL, а не пустой строкой: частичная
// уникальность имени объекта считает пустую строку значением, и два снимка без
// имени столкнулись бы друг с другом вместо того, чтобы остаться безымянными.
func (r *SnapshotRepo) Insert(ctx context.Context, s *domain.Snapshot) (*domain.Snapshot, []ownerregister.Registration, error) {
	var regs []ownerregister.Registration
	labels, err := json.Marshal(nonNilLabels(s.Labels))
	if err != nil {
		return nil, nil, storageerr.ErrInternal
	}
	var backendObject *string
	if s.Backend.BackendObject != "" {
		obj := s.Backend.BackendObject
		backendObject = &obj
	}
	created := *s
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var (
			createdAt, updatedAt time.Time
			zoneID, bindingID    string
		)
		serr := tx.QueryRow(ctx, snapshotInsertCAS,
			s.ID, s.ProjectID, s.Name, s.Description, labels, s.SourceVolumeID,
			string(s.StatusReason), backendObject).
			Scan(&createdAt, &updatedAt, &created.SizeBytes, &zoneID, &bindingID)
		if serr == nil {
			created.CreatedAt, created.UpdatedAt = createdAt, updatedAt
			created.ZoneID = zoneID
			created.Backend.BindingID = bindingID
			// owner-tuple register-intent в той же writer-TX (SEC-D): project#project@storage_snapshot.
			reg, eerr := emitFGARegister(ctx, tx, fgaregister.EventRegister,
				fgaregister.SnapshotItem(s.ProjectID, s.ID, s.Labels))
			if eerr != nil {
				return eerr
			}
			regs = []ownerregister.Registration{reg}
			return nil
		}
		if !errors.Is(serr, pgx.ErrNoRows) {
			return serr // 23505 name-collision / 23514 CHECK → mapSnapshotErr снаружи
		}
		return disambiguateSnapshotSource(ctx, tx, s.ProjectID, s.SourceVolumeID) // 0 rows → sentinel
	})
	if txErr != nil {
		return nil, nil, mapSnapshotErr(txErr, snapErrCtx{
			snapshotID: s.ID, snapshotName: s.Name, sourceVolumeID: s.SourceVolumeID,
		})
	}
	created.Status = domain.SnapshotStatusFromState("CREATING")
	return &created, regs, nil
}

// disambiguateSnapshotSource разбирает 0-row исход from-READY-CAS (в той же tx). Резолв
// тома — СТРОГО в проекте снимка: том не резолвится (нет вовсе ЛИБО принадлежит чужому
// проекту) → "Volume <id> not found"; свой том, но state != READY → "Volume <id> is not
// ready" (оба FailedPrecondition — existence same-DB, не cross-service). project-скоуп
// обязателен и здесь: неограниченный SELECT отдавал бы на ЧУЖОЙ не-READY том
// отличимое "is not ready" — existence/state-oracle (security.md §6, hide-existence
// byte-identity с настоящим miss'ом).
func disambiguateSnapshotSource(ctx context.Context, tx pgx.Tx, projectID, srcVolumeID string) error {
	var state string
	verr := tx.QueryRow(ctx, `SELECT state FROM volumes WHERE id = $1 AND project_id = $2`,
		srcVolumeID, projectID).Scan(&state)
	if verr != nil {
		if errors.Is(verr, pgx.ErrNoRows) {
			return fmt.Errorf("%w: Volume %s not found", storageerr.ErrFailedPrecondition, srcVolumeID)
		}
		return verr
	}
	if state != "READY" {
		return fmt.Errorf("%w: Volume %s is not ready", storageerr.ErrFailedPrecondition, srcVolumeID)
	}
	// READY, том есть, но 0 rows — состояние сменилось между INSERT и disambiguation. Opaque.
	return storageerr.ErrInternal
}

// Update реализует snapshot.Repo: mutable name/description/labels (COALESCE, nil →
// без изменения). 0 rows → NotFound. partial
// UNIQUE(name) collision → 23505 → AlreadyExists.
func (r *SnapshotRepo) Update(ctx context.Context, id string, u snapshot.SnapshotUpdate) (*domain.Snapshot, []ownerregister.Registration, error) {
	var regs []ownerregister.Registration
	var labelsArg any
	if u.LabelsSet {
		b, err := json.Marshal(nonNilLabels(u.Labels))
		if err != nil {
			return nil, nil, storageerr.ErrInternal
		}
		labelsArg = b
	}
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var (
			rowID       string
			projectID   string
			labelsAfter []byte
		)
		serr := tx.QueryRow(ctx, `UPDATE snapshots SET
				name        = COALESCE($2, name),
				description = COALESCE($3, description),
				labels      = COALESCE($4::jsonb, labels),
				updated_at  = now()
			WHERE id = $1
			RETURNING id, project_id, labels`,
			id, u.Name, u.Description, labelsArg).Scan(&rowID, &projectID, &labelsAfter)
		if serr == nil {
			reg, eerr := reEmitLabelMirror(ctx, tx, u.LabelsSet, labelsAfter,
				func(labels map[string]string) fgaregister.Item {
					return fgaregister.SnapshotItem(projectID, rowID, labels)
				})
			if eerr != nil {
				return eerr
			}
			if !reg.SourceVersion.IsZero() {
				regs = []ownerregister.Registration{reg}
			}
			return nil
		}
		if errors.Is(serr, pgx.ErrNoRows) {
			return fmt.Errorf("%w: Snapshot %s not found", storageerr.ErrNotFound, id)
		}
		return serr
	})
	if txErr != nil {
		return nil, nil, mapSnapshotErr(txErr, snapErrCtx{snapshotID: id, snapshotName: derefStr(u.Name)})
	}
	updated, gerr := r.Get(ctx, id)
	return updated, regs, gerr
}

// Delete реализует snapshot.Repo: DELETE строки + fga_register unregister-intent в
// той же tx.
// Ссылки volumes.source_snapshot_id → SET NULL (не RESTRICT) — delete НЕ блокируется
// (§1.2, S1-09). 0 rows → NotFound.
func (r *SnapshotRepo) Delete(ctx context.Context, id string) error {
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// RETURNING project_id — нужен для unregister owner-tuple; 0 rows → NotFound.
		var projectID string
		err := tx.QueryRow(ctx, `DELETE FROM snapshots WHERE id = $1 RETURNING project_id`, id).Scan(&projectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: Snapshot %s not found", storageerr.ErrNotFound, id)
			}
			return err
		}
		// owner-tuple unregister-intent в той же writer-TX (SEC-D).
		_, uerr := emitFGARegister(ctx, tx, fgaregister.EventUnregister,
			fgaregister.Item{Tuple: fgaregister.StorageSnapshot(projectID, id)})
		return uerr
	})
	if txErr != nil {
		return mapSnapshotErr(txErr, snapErrCtx{snapshotID: id})
	}
	return nil
}

var _ snapshot.Repo = (*SnapshotRepo)(nil)
