// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package pg — Postgres-adapter (handwritten pgx, БЕЗ ORM) для kacho-storage
// (схема kacho_storage). Реализует порты use-case-слоя (volume.Reader/Writer,
// snapshot.Repo, disktype.Repo). pgx живёт ЗДЕСЬ, не в use-case (dependency rule).
// Within-service инварианты — на DB (size increase-only CAS, partial UNIQUE, FK
// RESTRICT), НЕ software TOCTOU (data-integrity.md). Мутации пишут storage_outbox
// в той же writer-tx (атомарно, ban #16).
package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/outbox"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/volume"
)

// outboxTable — транзакционный outbox мутаций storage (миграция 0005).
const outboxTable = "storage_outbox"

// defaultBlockSize — дефолтный block_size тома (§1.1), если не задан на Create.
const defaultBlockSize = 4096

// VolumeRepo — реализация volume.Reader/Writer поверх pgxpool.
type VolumeRepo struct {
	pool *pgxpool.Pool
}

// NewVolumeRepo создаёт VolumeRepo поверх pgxpool.
func NewVolumeRepo(pool *pgxpool.Pool) *VolumeRepo { return &VolumeRepo{pool: pool} }

// volumeSelectCols — общий проекционный список для Get/List: колонки тома +
// LEFT JOIN volume_attachments (0..1 строка, PK volume_id). Nullable attach-колонки
// сканируются в указатели → nil == нет привязки (status derived AVAILABLE).
const volumeSelectCols = `
	v.id, v.project_id, v.created_at, v.updated_at, v.name, v.description, v.labels,
	v.zone_id, v.disk_type_id, v.size_bytes, v.block_size,
	COALESCE(v.source_snapshot_id, ''), v.state,
	va.instance_id, va.instance_name, va.device_name, va.is_boot, va.mode, va.auto_delete, va.attached_at`

// scanVolume читает одну строку проекции volumeSelectCols в domain.Volume, деривя
// Status (§1.3) и заполняя Attachments (output-only) при наличии привязки.
func scanVolume(row pgx.Row) (*domain.Volume, error) {
	var (
		v          domain.Volume
		labelsJSON []byte
		state      string
		// nullable attach-колонки (LEFT JOIN)
		instanceID   *string
		instanceName *string
		deviceName   *string
		isBoot       *bool
		mode         *string
		autoDelete   *bool
		attachedAt   *time.Time
	)
	if err := row.Scan(
		&v.ID, &v.ProjectID, &v.CreatedAt, &v.UpdatedAt, &v.Name, &v.Description, &labelsJSON,
		&v.ZoneID, &v.DiskTypeID, &v.SizeBytes, &v.BlockSize, &v.SourceSnapshot, &state,
		&instanceID, &instanceName, &deviceName, &isBoot, &mode, &autoDelete, &attachedAt,
	); err != nil {
		return nil, err
	}
	if len(labelsJSON) > 0 {
		if err := json.Unmarshal(labelsJSON, &v.Labels); err != nil {
			return nil, err
		}
	}
	attached := instanceID != nil
	v.Status = domain.DeriveStatus(state, attached)
	if attached {
		att := domain.VolumeAttachment{
			VolumeID:   v.ID,
			InstanceID: derefStr(instanceID),
		}
		att.InstanceName = derefStr(instanceName)
		att.DeviceName = derefStr(deviceName)
		if isBoot != nil {
			att.IsBoot = *isBoot
		}
		att.Mode = attachModeFromDB(derefStr(mode))
		if autoDelete != nil {
			att.AutoDelete = *autoDelete
		}
		if attachedAt != nil {
			att.AttachedAt = *attachedAt
		}
		v.Attachments = []domain.VolumeAttachment{att}
	}
	return &v, nil
}

// Get реализует volume.Reader: том по id + derive-on-read status/attachments.
func (r *VolumeRepo) Get(ctx context.Context, id string) (*domain.Volume, error) {
	q := `SELECT ` + volumeSelectCols + `
		FROM volumes v LEFT JOIN volume_attachments va ON va.volume_id = v.id
		WHERE v.id = $1`
	v, err := scanVolume(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, mapVolumeErr(err, volErrCtx{volumeID: id})
	}
	return v, nil
}

// List реализует volume.Reader: cursor-пагинация (created_at,id) ASC, опц.
// project-scope и filter=name. pageSize уже нормализован use-case-слоем.
func (r *VolumeRepo) List(ctx context.Context, p volume.Pagination) ([]*domain.Volume, string, error) {
	var (
		conds []string
		args  []any
	)
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if p.ProjectID != "" {
		add("v.project_id = $%d", p.ProjectID)
	}
	if p.Filter != "" {
		add("v.name = $%d", p.Filter)
	}
	if p.PageToken != "" {
		cur, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", derr
		}
		// keyset по (created_at,id): строки строго после курсора.
		args = append(args, cur.createdAt, cur.id)
		conds = append(conds, fmt.Sprintf("(v.created_at, v.id) > ($%d, $%d)", len(args)-1, len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, p.PageSize+1)
	q := fmt.Sprintf(`SELECT %s
		FROM volumes v LEFT JOIN volume_attachments va ON va.volume_id = v.id
		%s
		ORDER BY v.created_at ASC, v.id ASC
		LIMIT $%d`, volumeSelectCols, where, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", mapVolumeErr(err, volErrCtx{})
	}
	defer rows.Close()
	var out []*domain.Volume
	for rows.Next() {
		v, serr := scanVolume(rows)
		if serr != nil {
			return nil, "", mapVolumeErr(serr, volErrCtx{})
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, "", mapVolumeErr(err, volErrCtx{})
	}
	var next string
	if int64(len(out)) > p.PageSize {
		last := out[p.PageSize-1]
		next = encodePageToken(cursor{createdAt: last.CreatedAt, id: last.ID})
		out = out[:p.PageSize]
	}
	return out, next, nil
}

// Insert реализует volume.Writer: state=READY сразу (§1.4), storage_outbox CREATED
// в той же tx. source_snapshot_id=” → NULL (иначе FK ловит пустую ссылку).
// disk_type_id RESTRICT / source_snapshot_id FK / partial UNIQUE(name) → контрактные
// sentinel'ы через mapVolumeErr.
func (r *VolumeRepo) Insert(ctx context.Context, v *domain.Volume) (*domain.Volume, error) {
	blockSize := v.BlockSize
	if blockSize == 0 {
		blockSize = defaultBlockSize
	}
	labels, err := json.Marshal(nonNilLabels(v.Labels))
	if err != nil {
		return nil, ports.ErrInternal
	}
	var srcSnap *string
	if v.SourceSnapshot != "" {
		srcSnap = &v.SourceSnapshot
	}
	created := *v
	created.BlockSize = blockSize
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx, `
			INSERT INTO volumes
				(id, project_id, name, description, labels, zone_id, disk_type_id,
				 size_bytes, block_size, source_snapshot_id, state)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'READY')
			RETURNING created_at, updated_at`,
			v.ID, v.ProjectID, v.Name, v.Description, labels, v.ZoneID, v.DiskTypeID,
			v.SizeBytes, blockSize, srcSnap).
			Scan(&created.CreatedAt, &created.UpdatedAt)
		if serr != nil {
			return serr
		}
		if oerr := outbox.Emit(ctx, tx, outboxTable, "Volume", v.ID, "CREATED", map[string]any{
			"id":         v.ID,
			"project_id": v.ProjectID,
			"zone_id":    v.ZoneID,
		}); oerr != nil {
			return oerr
		}
		// owner-tuple register-intent в той же writer-TX (SEC-D): project#project@storage_volume.
		return emitFGARegister(ctx, tx, fgaregister.EventRegister,
			fgaregister.VolumeItem(v.ProjectID, v.ID, v.Labels))
	})
	if txErr != nil {
		return nil, mapVolumeErr(txErr, volErrCtx{
			volumeID: v.ID, volumeName: v.Name, diskTypeID: v.DiskTypeID, snapshotID: v.SourceSnapshot,
		})
	}
	created.Status = domain.DeriveStatus("READY", false) // just created → AVAILABLE
	return &created, nil
}

// Update реализует volume.Writer: атомарный размер-CAS increase-only (§3b) +
// mutable name/description/labels (COALESCE, nil-указатель → без изменения). Один
// UPDATE-стейтмент, БЕЗ предварительного Get (нет TOCTOU). 0 rows →
// disambiguation: строка есть → size-CAS не прошёл (InvalidArgument "Volume size
// can only be increased"); строки нет → NotFound. storage_outbox UPDATED в той же tx.
func (r *VolumeRepo) Update(ctx context.Context, id string, u volume.VolumeUpdate) (*domain.Volume, error) {
	var labelsArg any
	if u.LabelsSet {
		b, err := json.Marshal(nonNilLabels(u.Labels))
		if err != nil {
			return nil, ports.ErrInternal
		}
		labelsArg = b
	}
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// size-CAS: $5 IS NULL → размер не меняется; иначе применяется ТОЛЬКО если
		// строго больше текущего (increase-only, не software-compare).
		var rowID string
		serr := tx.QueryRow(ctx, `UPDATE volumes SET
				name        = COALESCE($2, name),
				description = COALESCE($3, description),
				labels      = COALESCE($4::jsonb, labels),
				size_bytes  = COALESCE($5, size_bytes),
				updated_at  = now()
			WHERE id = $1 AND ($5::bigint IS NULL OR $5 > size_bytes)
			RETURNING id`, id, u.Name, u.Description, labelsArg, u.SizeBytes).Scan(&rowID)
		if serr == nil {
			return outbox.Emit(ctx, tx, outboxTable, "Volume", id, "UPDATED", map[string]any{"id": id})
		}
		if !errors.Is(serr, pgx.ErrNoRows) {
			return serr
		}
		// 0 rows: строка есть → size-CAS отверг; строки нет → NotFound.
		var exists bool
		if perr := tx.QueryRow(ctx, `SELECT true FROM volumes WHERE id = $1`, id).Scan(&exists); perr != nil {
			if errors.Is(perr, pgx.ErrNoRows) {
				return fmt.Errorf("%w: Volume %s not found", ports.ErrNotFound, id)
			}
			return perr
		}
		return fmt.Errorf("%w: Volume size can only be increased", ports.ErrInvalidArg)
	})
	if txErr != nil {
		return nil, mapVolumeErr(txErr, volErrCtx{volumeID: id, volumeName: derefStr(u.Name)})
	}
	return r.Get(ctx, id)
}

// Delete реализует volume.Writer: DELETE строки тома + storage_outbox DELETED в
// той же tx. Привязанный том → FK volume_attachments.volume_id RESTRICT (23503) →
// FailedPrecondition "Volume <id> is in use" (§3.6). 0 rows → NotFound.
func (r *VolumeRepo) Delete(ctx context.Context, id string) error {
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// RETURNING project_id — нужен для unregister owner-tuple (subject project:<id>);
		// 0 rows → pgx.ErrNoRows → NotFound. FK RESTRICT (attached) → 23503 из DELETE.
		var projectID string
		err := tx.QueryRow(ctx, `DELETE FROM volumes WHERE id = $1 RETURNING project_id`, id).Scan(&projectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: Volume %s not found", ports.ErrNotFound, id)
			}
			return err
		}
		if oerr := outbox.Emit(ctx, tx, outboxTable, "Volume", id, "DELETED", map[string]any{"id": id}); oerr != nil {
			return oerr
		}
		// owner-tuple unregister-intent в той же writer-TX (SEC-D).
		return emitFGARegister(ctx, tx, fgaregister.EventUnregister,
			fgaregister.Item{Tuple: fgaregister.StorageVolume(projectID, id)})
	})
	if txErr != nil {
		return mapVolumeErr(txErr, volErrCtx{volumeID: id})
	}
	return nil
}

// ── S2: InternalVolumeService attach-CAS (:9091) ───────────────────────────
// Attach — атомарный INSERT … ON CONFLICT CAS в ОДНОЙ tx (§3.2), НЕ Get→check→INSERT
// (TOCTOU, ban #10). Self-describing payload (instance zone/project) сверяется со
// СВОЕЙ строкой volumes — storage НЕ зовёт compute (ацикличность, INV-1).

// attachCASSQL — атомарная вставка-если-можно: том READY, та же зона/проект. Свободен
// (нет строки) — вставляет; конфликт по PK volume_id → DO NOTHING (0 rows). device/boot
// UNIQUE/EXCLUDE НЕ поглощаются arbiter'ом volume_id → всплывают 23505/23P01.
const attachCASSQL = `
	INSERT INTO volume_attachments
		(volume_id, instance_id, instance_name, project_id, zone_id, device_name, is_boot, mode, auto_delete)
	SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9
	  FROM volumes v
	 WHERE v.id = $1 AND v.state = 'READY' AND v.zone_id = $5 AND v.project_id = $4
	ON CONFLICT (volume_id) DO NOTHING
	RETURNING volume_id`

// maxAutoDeviceAttempts — верхняя граница retry авто-назначения device_name.
// Пространство имён sdb..sdz = 25; больше 25 различных попыток бессмысленно (§2.7).
const maxAutoDeviceAttempts = 25

// Attach реализует volume.Writer: атомарный CAS-insert строки volume_attachments +
// disambiguation 0-row исхода (§3.2). Explicit device_name → одна попытка (коллизия →
// контрактный 23505-текст). Пустой device_name → авто-назначение с retry-until-free:
// конкурент, занявший выбранное имя между выбором и вставкой, даёт 23505 на
// UNIQUE(instance_id,device_name) → пересчитываем следующее свободное и повторяем
// (bounded ≤25); 23505 auto-пути наружу НЕ всплывает (S4-07/08). Идемпотентный replay
// (та же строка/инстанс) → OK. Никаких contact'ов с compute (ацикличность, INV-1).
func (r *VolumeRepo) Attach(ctx context.Context, a *domain.VolumeAttachment) error {
	if a.DeviceName != "" {
		// explicit device — одна попытка; device-collision → контрактный 23505-текст.
		if err := r.attachOnce(ctx, a, a.DeviceName); err != nil {
			return mapVolumeErr(err, volErrCtx{
				volumeID: a.VolumeID, deviceName: a.DeviceName, instanceID: a.InstanceID,
			})
		}
		return nil
	}
	// пустой device — авто-назначение первого свободного sdb..sdz с retry-until-free.
	for attempt := 0; attempt < maxAutoDeviceAttempts; attempt++ {
		device, derr := nextFreeDevice(ctx, r.pool, a.InstanceID)
		if derr != nil {
			return mapVolumeErr(derr, volErrCtx{volumeID: a.VolumeID, instanceID: a.InstanceID})
		}
		if device == "" {
			return fmt.Errorf("%w: no free device name on Instance %s", ports.ErrFailedPrecondition, a.InstanceID)
		}
		err := r.attachOnce(ctx, a, device)
		if err == nil {
			return nil
		}
		if isDeviceCollision(err) {
			continue // конкурент занял это имя между выбором и вставкой → пересчитать
		}
		return mapVolumeErr(err, volErrCtx{volumeID: a.VolumeID, instanceID: a.InstanceID})
	}
	// исчерпали bounded-retry под жёсткой конкуренцией → трактуем как «нет свободного».
	return fmt.Errorf("%w: no free device name on Instance %s", ports.ErrFailedPrecondition, a.InstanceID)
}

// attachOnce выполняет ОДИН атомарный CAS-insert строки volume_attachments в своей tx с
// уже разрешённым device. Возвращает СЫРУЮ pgx/pgconn-ошибку (23505/23P01) — маппинг
// делает вызывающий Attach, чтобы retry-логика распознала device-collision ДО маппинга.
// disambiguateAttach возвращает уже-замапленные ports-sentinel'ы (mapVolumeErr
// пробрасывает их идемпотентно).
func (r *VolumeRepo) attachOnce(ctx context.Context, a *domain.VolumeAttachment, device string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var got string
		serr := tx.QueryRow(ctx, attachCASSQL,
			a.VolumeID, a.InstanceID, a.InstanceName, a.ProjectID, a.ZoneID,
			device, a.IsBoot, attachModeToDB(a.Mode), a.AutoDelete).Scan(&got)
		if serr == nil {
			return nil // CAS-insert прошёл (1 row) → attached
		}
		if !errors.Is(serr, pgx.ErrNoRows) {
			return serr // 23505/23P01/… сырое → распознаётся/маппится вызывающим
		}
		return disambiguateAttach(ctx, tx, a) // 0 rows → корректный sentinel
	})
}

// isDeviceCollision — сырая ошибка 23505 на UNIQUE(instance_id,device_name)? Только на
// этот класс auto-путь ретраит (пересчитать имя); прочие ошибки — терминальны.
func isDeviceCollision(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == cnAttachDeviceUniq
}

// disambiguateAttach разбирает 0-row исход CAS (§3.2, в той же tx): конфликт-строка
// нашего инстанса → идемпотентный OK; чужого → "Volume <id> is in use"; строки нет и
// том не READY/не существует → "Volume is not available for attachment". zone и project —
// ДВА разных предиката (INV-4): расходится зона → "Volume and Instance must be in the
// same zone"; расходится проект → ОТДЕЛЬНЫЙ "Volume and Instance must be in the same
// project" (zone-текст не переиспользуется — исправление относительно companion S2-04).
func disambiguateAttach(ctx context.Context, tx pgx.Tx, a *domain.VolumeAttachment) error {
	var owner string
	oerr := tx.QueryRow(ctx, `SELECT instance_id FROM volume_attachments WHERE volume_id = $1`, a.VolumeID).Scan(&owner)
	if oerr == nil {
		if owner == a.InstanceID {
			return nil // идемпотентный replay (уже наш)
		}
		return fmt.Errorf("%w: Volume %s is in use", ports.ErrFailedPrecondition, a.VolumeID)
	}
	if !errors.Is(oerr, pgx.ErrNoRows) {
		return oerr
	}
	// нет привязки → причина в volumes-предикате (state / zone / project).
	var state, zoneID, projectID string
	verr := tx.QueryRow(ctx, `SELECT state, zone_id, project_id FROM volumes WHERE id = $1`, a.VolumeID).
		Scan(&state, &zoneID, &projectID)
	if verr != nil {
		if errors.Is(verr, pgx.ErrNoRows) {
			return fmt.Errorf("%w: Volume is not available for attachment", ports.ErrFailedPrecondition)
		}
		return verr
	}
	if state != "READY" {
		return fmt.Errorf("%w: Volume is not available for attachment", ports.ErrFailedPrecondition)
	}
	if zoneID != a.ZoneID {
		return fmt.Errorf("%w: Volume and Instance must be in the same zone", ports.ErrFailedPrecondition)
	}
	if projectID != a.ProjectID {
		return fmt.Errorf("%w: Volume and Instance must be in the same project", ports.ErrFailedPrecondition)
	}
	// READY + zone/project совпали + нет привязки, но CAS ничего не вставил — состояние
	// изменилось между INSERT и disambiguation (attach+detach гонка). Opaque INTERNAL.
	return ports.ErrInternal
}

// rowsQuerier — минимальный read-порт (pgxpool.Pool ИЛИ pgx.Tx), чтобы nextFreeDevice
// читал used-set вне attach-tx (каждая retry-попытка видит свежий committed-снимок).
type rowsQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// nextFreeDevice возвращает первое свободное имя устройства (sdb..sdz) в инстансе для
// авто-назначения при пустом device_name (§2.2). Пустая строка (без ошибки) → все 25
// имён заняты (вызывающий отдаёт "no free device name"). UNIQUE-констрейнт + retry в
// Attach — разрешение гонки (не полагаемся на этот read как на источник истины).
func nextFreeDevice(ctx context.Context, q rowsQuerier, instanceID string) (string, error) {
	rows, err := q.Query(ctx, `SELECT device_name FROM volume_attachments WHERE instance_id = $1`, instanceID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	used := map[string]struct{}{}
	for rows.Next() {
		var d string
		if serr := rows.Scan(&d); serr != nil {
			return "", serr
		}
		used[d] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	for c := byte('b'); c <= 'z'; c++ {
		cand := "sd" + string(c)
		if _, taken := used[cand]; !taken {
			return cand, nil
		}
	}
	return "", nil // все имена sdb..sdz заняты → "no free device name"
}

// Detach реализует volume.Writer: идемпотентное удаление строки volume_attachments
// (§3.3). 0 rows → уже отвязан → OK. Derived status тома возвращается к AVAILABLE
// автоматически (наличие строки — единственный источник, §1.3).
func (r *VolumeRepo) Detach(ctx context.Context, volumeID, instanceID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM volume_attachments WHERE volume_id = $1 AND instance_id = $2`, volumeID, instanceID)
	if err != nil {
		return mapVolumeErr(err, volErrCtx{volumeID: volumeID, instanceID: instanceID})
	}
	return nil
}

// ListAttachments реализует volume.Reader: батч-чтение attachments по instance_id
// (compute-mirror, не N+1, §3.5). Один запрос на всё множество инстансов.
func (r *VolumeRepo) ListAttachments(ctx context.Context, instanceIDs []string) ([]*domain.VolumeAttachment, error) {
	if len(instanceIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT volume_id, instance_id, instance_name, project_id, zone_id,
		       device_name, is_boot, mode, auto_delete, attached_at
		  FROM volume_attachments
		 WHERE instance_id = ANY($1)
		 ORDER BY instance_id ASC, device_name ASC`, instanceIDs)
	if err != nil {
		return nil, mapVolumeErr(err, volErrCtx{})
	}
	defer rows.Close()
	var out []*domain.VolumeAttachment
	for rows.Next() {
		var (
			a    domain.VolumeAttachment
			mode string
		)
		if serr := rows.Scan(
			&a.VolumeID, &a.InstanceID, &a.InstanceName, &a.ProjectID, &a.ZoneID,
			&a.DeviceName, &a.IsBoot, &mode, &a.AutoDelete, &a.AttachedAt,
		); serr != nil {
			return nil, mapVolumeErr(serr, volErrCtx{})
		}
		a.Mode = attachModeFromDB(mode)
		out = append(out, &a)
	}
	if err := rows.Err(); err != nil {
		return nil, mapVolumeErr(err, volErrCtx{})
	}
	return out, nil
}

// ── data-plane (out of scope §0.3) ──────────────────────────────────────────
// GetInternal (infra-проекция) — будущий data-plane инкремент. Анкер: notReady.

func (r *VolumeRepo) notReady(op string) error {
	if r.pool == nil {
		return fmt.Errorf("%w: %s (nil pool)", ports.ErrInternal, op)
	}
	return fmt.Errorf("%w: repo.%s", ports.ErrUnimplemented, op)
}

// GetInternal реализует volume.Reader (full infra-проекция, :9091) — data-plane (§0.3).
func (r *VolumeRepo) GetInternal(ctx context.Context, id string) (*domain.Volume, error) {
	return nil, r.notReady("Volume.GetInternal")
}

// attachModeToDB маппит domain.AttachmentMode → text-значение колонки mode
// (CHECK IN ('READ_WRITE','READ_ONLY')). Unspecified → READ_WRITE (§1.1 default).
func attachModeToDB(m domain.AttachmentMode) string {
	if m == domain.AttachmentModeReadOnly {
		return "READ_ONLY"
	}
	return "READ_WRITE"
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nonNilLabels(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func attachModeFromDB(s string) domain.AttachmentMode {
	switch s {
	case "READ_WRITE":
		return domain.AttachmentModeReadWrite
	case "READ_ONLY":
		return domain.AttachmentModeReadOnly
	default:
		return domain.AttachmentModeUnspecified
	}
}

var (
	_ volume.Reader = (*VolumeRepo)(nil)
	_ volume.Writer = (*VolumeRepo)(nil)
)
