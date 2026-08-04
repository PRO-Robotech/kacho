// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/pkg/filter"
	"github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// InstanceRepo — реализация ports.InstanceRepo поверх pgxpool.
//
// Storage-split cutover: compute больше НЕ держит local attach-state — таблица
// `attached_disks` удалена (миграция 0013). Том↔Instance-привязка живёт в
// kacho-storage; `Instance.boot_volume`/`secondary_volumes` — read-only зеркало,
// заполняемое use-case'ом на чтении из storage. Здесь остаётся только строка
// `instances` (+ same-DB NIC-mirror child таблица, cascade).
type InstanceRepo struct {
	pool *pgxpool.Pool
}

// NewInstanceRepo создаёт InstanceRepo.
func NewInstanceRepo(pool *pgxpool.Pool) *InstanceRepo { return &InstanceRepo{pool: pool} }

// instanceCols — колонки таблицы instances (COMP-1 redesign; vendor-cruft-колонки
// сняты миграцией 0016). effective_resources распакованы в eff_* скаляры;
// boot_source — bs_* скаляры; vm_spec/container_spec — JSONB.
const instanceCols = `id, project_id, created_at, name, description, labels, zone_id, status, status_reason, ` +
	`metadata, hostname, fqdn, cpu_guarantee_percent, service_account_id, ` +
	`instance_kind, machine_type_id, eff_vcpu, eff_memory_mib, eff_gpus, eff_gpu_type, ` +
	`bs_type, bs_id, bs_image_kind, placement_group_id, vm_spec, container_spec`

// instanceSelectCols — тот же список для SELECT/RETURNING, но machine_type_id
// читается через COALESCE: колонка NULLable (0017 — FK на machine_types(id),
// а NOT NULL с пустой строкой по умолчанию не FK-able), тогда как domain-тип
// остаётся `string`. NULL («тип не задан») читается как "" — как и до 0017.
// Симметрично на записи — NULLIF (см. Insert/Update).
const instanceSelectCols = `id, project_id, created_at, name, description, labels, zone_id, status, status_reason, ` +
	`metadata, hostname, fqdn, cpu_guarantee_percent, service_account_id, ` +
	`instance_kind, COALESCE(machine_type_id,'') AS machine_type_id, eff_vcpu, eff_memory_mib, eff_gpus, eff_gpu_type, ` +
	`bs_type, bs_id, bs_image_kind, placement_group_id, vm_spec, container_spec`

// Get возвращает ВМ по id. AttachedDisks НЕ заполняются здесь — это зеркало из
// kacho-storage, use-case подтягивает его на чтении (graceful-degrade).
func (r *InstanceRepo) Get(ctx context.Context, id string) (*domain.Instance, error) {
	q := fmt.Sprintf(`SELECT %s FROM instances WHERE id = $1`, instanceSelectCols)
	in, err := scanInstance(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, wrapPgErr(err, "Instance", id)
	}
	return in, nil
}

// List возвращает ВМ по project с cursor-pagination.
func (r *InstanceRepo) List(ctx context.Context, f ports.InstanceFilter, p ports.Pagination) ([]*domain.Instance, string, error) {
	pageSize, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	var args []any
	var conditions []string
	argIdx := 1
	if f.ProjectID != "" {
		conditions = append(conditions, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, f.ProjectID)
		argIdx++
	}
	if f.Filter != "" {
		ast, perr := filter.Parse(f.Filter, []string{"name"})
		if perr != nil {
			return nil, "", invalidFilterErr(perr)
		}
		if ast != nil {
			frag, fargs := ast.ToSQL(argIdx)
			conditions = append(conditions, frag)
			args = append(args, fargs...)
			argIdx += len(fargs)
		}
	}
	if p.PageToken != "" {
		tsv, id, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", invalidPageTokenErr(derr)
		}
		conditions = append(conditions, fmt.Sprintf("(created_at, id) > ($%d, $%d)", argIdx, argIdx+1))
		args = append(args, tsv, id)
		argIdx += 2
	}
	var where string
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	q := fmt.Sprintf(`SELECT %s FROM instances %s ORDER BY created_at ASC, id ASC LIMIT $%d`, instanceSelectCols, where, argIdx)
	args = append(args, pageSize+1)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", wrapPgErr(err, "Instance", "")
	}
	defer rows.Close()
	var result []*domain.Instance
	for rows.Next() {
		in, serr := scanInstance(rows)
		if serr != nil {
			return nil, "", wrapPgErr(serr, "Instance", "")
		}
		result = append(result, in)
	}
	if err := rows.Err(); err != nil {
		return nil, "", wrapPgErr(err, "Instance", "")
	}
	var nextToken string
	if int64(len(result)) > pageSize {
		last := result[pageSize-1]
		nextToken = encodePageToken(last.CreatedAt, last.ID)
		result = result[:pageSize]
	}
	return result, nextToken, nil
}

// Insert вставляет строку ВМ + outbox CREATED + FGA register-intent в одной
// writer-tx. Никаких attached_disks / inline-дисков — compute local attach-state
// упразднён (storage-split).
func (r *InstanceRepo) Insert(ctx context.Context, in *domain.Instance) (*domain.Instance, error) {
	insertArgs, err := instanceInsertArgs(in)
	if err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const qIns = `INSERT INTO instances (` + instanceCols + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NULLIF($16,''),$17,$18,$19,$20,$21,$22,$23,$24,$25,$26) RETURNING ` + instanceSelectCols
	created, err := scanInstance(tx.QueryRow(ctx, qIns, insertArgs...))
	if err != nil {
		return nil, wrapPgErr(err, "Instance", in.Name)
	}
	if err := emitCompute(ctx, tx, "Instance", created.ID, "CREATED", instancePayload(created)); err != nil {
		return nil, ports.ErrInternal
	}
	// FGA owner-tuple register-intent for the Instance in the SAME writer-tx,
	// carrying the instance labels + parent-scope to feed IAM resource_mirror.
	if err := emitFGARegisterIntent(ctx, tx, fgaintent.EventRegister, "Instance", created.ID, created.ProjectID, created.Labels); err != nil {
		return nil, ports.ErrInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, wrapPgErr(err, "Instance", in.Name)
	}
	return created, nil
}

// Update обновляет mutable descriptive/resource поля ВМ + outbox UPDATED.
// status НЕ трогается — им владеет исключительно SetStatusCAS/MarkDeleting.
//
// emitLabelsRegister: when true a fresh FGA register-intent carrying the updated
// labels + parent-scope is emitted IN THE SAME writer-tx as the UPDATE (atomic) so
// the IAM resource_mirror stays in sync.
func (r *InstanceRepo) Update(ctx context.Context, in *domain.Instance, emitLabelsRegister bool, changed []string) (*domain.Instance, error) {
	ch := changedSet(changed)
	us := newUpdateSet(in.ID)
	if _, ok := ch["name"]; ok {
		us.add("name", in.Name)
	}
	if _, ok := ch["description"]; ok {
		us.add("description", in.Description)
	}
	if _, ok := ch["labels"]; ok {
		labelsJSON, err := marshalJSONB(in.Labels, "Instance.labels")
		if err != nil {
			return nil, err
		}
		us.add("labels", labelsJSON)
	}
	if _, ok := ch["service_account_id"]; ok {
		us.add("service_account_id", in.ServiceAccountID)
	}
	// status_reason — next-boot deferral marker ("takes effect on next boot", COMP-1
	// F10); LIVE-применяется вместе с vm_spec/ssh next-boot полями.
	if _, ok := ch["status_reason"]; ok {
		us.add("status_reason", in.StatusReason)
	}
	if _, ok := ch["vm_spec"]; ok {
		vmSpecJSON, err := marshalSpecJSONB(in.VMSpec, "Instance.vm_spec")
		if err != nil {
			return nil, err
		}
		us.add("vm_spec", vmSpecJSON)
	}
	// requireStopped: sizing (machine_type_id/cpu_guarantee_percent) и placement
	// (placement_group_id) разрешены ТОЛЬКО пока instance STOPPED (COMP-1 F10). В
	// COMP-1 STOPPED недостижимо (Stop=COMP-2) → service отвергает эти маски sync
	// первым (always-reject). CAS `AND status='STOPPED'` — DB-level backstop
	// (defense-in-depth; NOT software Get→check→UPDATE), актуален в COMP-2.
	requireStopped := false
	if _, ok := ch["machine_type_id"]; ok {
		// NULLable FK-колонка (0017) — "" пишется как NULL, см. instanceSelectCols.
		us.addNullIfEmpty("machine_type_id", in.MachineTypeID)
		requireStopped = true
	}
	if _, ok := ch["cpu_guarantee_percent"]; ok {
		us.add("cpu_guarantee_percent", in.CPUGuaranteePercent)
		requireStopped = true
	}
	if _, ok := ch["placement_group_id"]; ok {
		us.add("placement_group_id", in.PlacementGroupID)
		requireStopped = true
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var updated *domain.Instance
	if us.empty() {
		// mask не задел ни одной mutable-колонки — no-op: перечитываем строку
		// (NotFound если её нет) и всё равно эмитим UPDATED (behaviour-preserving).
		updated, err = scanInstance(tx.QueryRow(ctx, fmt.Sprintf(`SELECT %s FROM instances WHERE id = $1`, instanceSelectCols), in.ID))
	} else {
		where := ` WHERE id = $1`
		if requireStopped {
			us.args = append(us.args, instanceStatusName(domain.InstanceStatusStopped))
			where += fmt.Sprintf(` AND status = $%d`, len(us.args))
		}
		q := `UPDATE instances ` + us.clause() + where + ` RETURNING ` + instanceSelectCols
		updated, err = scanInstance(tx.QueryRow(ctx, q, us.args...))
	}
	if err != nil {
		if requireStopped && errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			if e2 := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM instances WHERE id = $1)`, in.ID).Scan(&exists); e2 != nil {
				return nil, wrapPgErr(e2, "Instance", in.ID)
			}
			if !exists {
				return nil, fmt.Errorf("%w: Instance %s not found", ports.ErrNotFound, in.ID)
			}
			return nil, fmt.Errorf("%w: instance must be STOPPED to change sizing or placement", ports.ErrFailedPrecondition)
		}
		return nil, wrapPgErr(err, "Instance", in.ID)
	}
	if err := emitCompute(ctx, tx, "Instance", updated.ID, "UPDATED", instancePayload(updated)); err != nil {
		return nil, ports.ErrInternal
	}
	if emitLabelsRegister {
		if err := emitFGARegisterIntent(ctx, tx, fgaintent.EventRegister, "Instance", updated.ID, updated.ProjectID, updated.Labels); err != nil {
			return nil, ports.ErrInternal
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, wrapPgErr(err, "Instance", in.ID)
	}
	return updated, nil
}

// SetStatusCAS атомарно переводит instance из expected-status в next-status
// (within-service-инвариант на DB-уровне, conditional UPDATE WHERE id AND status).
func (r *InstanceRepo) SetStatusCAS(ctx context.Context, id string, expected, next domain.InstanceStatus) (*domain.Instance, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE instances SET status = $3 WHERE id = $1 AND status = $2`,
		id, instanceStatusName(expected), instanceStatusName(next))
	if err != nil {
		return nil, wrapPgErr(err, "Instance", id)
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM instances WHERE id = $1)`, id).Scan(&exists); err != nil {
			return nil, wrapPgErr(err, "Instance", id)
		}
		if !exists {
			return nil, fmt.Errorf("%w: Instance %s not found", ports.ErrNotFound, id)
		}
		return nil, fmt.Errorf("%w: state transition not allowed from current status", ports.ErrFailedPrecondition)
	}
	q := fmt.Sprintf(`SELECT %s FROM instances WHERE id = $1`, instanceSelectCols)
	in, err := scanInstance(tx.QueryRow(ctx, q, id))
	if err != nil {
		return nil, wrapPgErr(err, "Instance", id)
	}
	if err := emitCompute(ctx, tx, "Instance", in.ID, "UPDATED", instancePayload(in)); err != nil {
		return nil, ports.ErrInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, wrapPgErr(err, "Instance", id)
	}
	return in, nil
}

// GateForAttach — ОДНОСТЕЙТМЕНТНАЯ ПРЕДПРОВЕРКА attach-саги (disk/NIC), не
// compare-and-swap. Возвращает self-describing payload (zone_id/project_id/name) для
// форварда в storage/vpc, если инстанс в {RUNNING, STOPPED}.
//
// ЧТО ОНА ГАРАНТИРУЕТ. Существование и состояние решаются ОДНИМ стейтментом, то есть
// на одном снимке: не бывает ответа, в котором «инстанса нет» и «инстанс есть, но не в
// том состоянии» приходят из двух разных моментов времени. Прежде здесь стояли ДВА
// запроса — conditional SELECT, а на 0 rows отдельный EXISTS, — и между ними строка
// могла исчезнуть или сменить статус: полоса ошибки называла не то, что произошло
// (FailedPrecondition про уже удалённый инстанс, NotFound про существующий).
//
// ЧЕГО ОНА НЕ ГАРАНТИРУЕТ, И ЭТО ВАЖНО. Она ничего не пишет и не держит строку,
// поэтому гонку attach-vs-delete она СУЖАЕТ, а НЕ ЗАКРЫВАЕТ: после её возврата
// конкурентный Delete успевает поставить DELETING и отпустить привязки, пока форвард в
// storage/vpc ещё в пути, — и привязка ляжет на инстанс, которого уже нет. Прежний
// godoc заявлял обратное («атомарно», «CAS», «закрывает гонку»), и это ровно тот класс,
// из-за которого следующий контрибьютор чинит код под неверный комментарий.
//
// ОТКРЫТЫЙ ОСТАТОК. Настоящее закрытие требует сериализации, которой у предпроверки
// нет: либо счётчик attach-in-flight на строке инстанса (миграция + отпускание на всех
// путях), либо advisory-lock на id, удерживаемый обоими сагами, — и то и другое меняет
// схему/контракт и идёт через db- и system-design-ревью. До этого остаток обязан
// закрываться компенсацией инициатора (compensation-outbox) и sweeper'ом владельца
// привязки; см. правило data-integrity о компенсации кросс-сервисной саги.
func (r *InstanceRepo) GateForAttach(ctx context.Context, id string) (string, string, string, error) {
	var zoneID, projectID, name string
	var eligible bool
	// Один стейтмент, один снимок: и существование, и пригодность состояния.
	err := r.pool.QueryRow(ctx,
		`SELECT zone_id, project_id, name, status IN ('RUNNING','STOPPED')
		   FROM instances WHERE id = $1`, id).
		Scan(&zoneID, &projectID, &name, &eligible)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", fmt.Errorf("%w: Instance %s not found", ports.ErrNotFound, id)
		}
		return "", "", "", wrapPgErr(err, "Instance", id)
	}
	if !eligible {
		return "", "", "", fmt.Errorf("%w: Instance must be RUNNING or STOPPED", ports.ErrFailedPrecondition)
	}
	return zoneID, projectID, name, nil
}

// MarkDeleting атомарно переводит инстанс в DELETING (идемпотентно). Ставится ПЕРЕД
// release'ом привязок в delete-саге, чтобы конкурентный AttachDisk-гейт видел
// DELETING и падал (attach-vs-delete race). Повтор на уже-DELETING — no-op OK.
func (r *InstanceRepo) MarkDeleting(ctx context.Context, id string) (*domain.Instance, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// deleting_since штампуется ТОЛЬКО на фактическом переходе (предикат
	// `status <> 'DELETING'` уже это обеспечивает): повторный вызов не вправе
	// омолодить строку, иначе застрявшая машина вечно моложе отсрочки добивателя
	// и он не возьмёт её никогда.
	tag, err := tx.Exec(ctx, `UPDATE instances SET status = 'DELETING', deleting_since = now() WHERE id = $1 AND status <> 'DELETING'`, id)
	if err != nil {
		return nil, wrapPgErr(err, "Instance", id)
	}
	q := fmt.Sprintf(`SELECT %s FROM instances WHERE id = $1`, instanceSelectCols)
	in, err := scanInstance(tx.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: Instance %s not found", ports.ErrNotFound, id)
		}
		return nil, wrapPgErr(err, "Instance", id)
	}
	if tag.RowsAffected() > 0 {
		// эмитим UPDATED только на фактическом переходе (не на идемпотентном повторе).
		if err := emitCompute(ctx, tx, "Instance", in.ID, "UPDATED", instancePayload(in)); err != nil {
			return nil, ports.ErrInternal
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, wrapPgErr(err, "Instance", id)
	}
	return in, nil
}

// MergeMetadata атомарно применяет delete+upsert дельту к map metadata одним
// SQL-statement'ом + outbox UPDATED (within-service-инвариант на DB-уровне).
func (r *InstanceRepo) MergeMetadata(ctx context.Context, id string, del []string, upsert map[string]string) (*domain.Instance, error) {
	upsertJSON, err := marshalJSONB(orEmptyMap(upsert), "Instance.metadata.upsert")
	if err != nil {
		return nil, err
	}
	delKeys := del
	if delKeys == nil {
		delKeys = []string{}
	}
	return r.mutateAndReload(ctx, id, "UPDATED", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE instances
			    SET metadata = (COALESCE(metadata, '{}'::jsonb) - $2::text[]) || $3::jsonb
			  WHERE id = $1`,
			id, delKeys, upsertJSON)
		return err
	})
}

// Delete удаляет строку ВМ + outbox DELETED + FGA unregister-intent в одной
// writer-tx. ФИНАЛЬНЫЙ шаг delete-саги — том/NIC-привязки уже сняты в use-case
// (storage.Detach/vpc.Detach) ДО этого вызова; строка инстанса удаляется ПОСЛЕДНЕЙ,
// чтобы crash не осиротил привязки. Никакого attached_disks-sweep (таблицы нет).
// ListStuckDeleting возвращает id машин, вошедших в удаление раньше чем olderThan
// назад и там оставшихся.
//
// Строки без отметки (deleting_since IS NULL) НЕ возвращаются: NULL означает «в
// удаление не входила». Значение по умолчанию вместо NULL сделало бы каждую
// строку вечно просроченной — защита выглядела бы исполненной и пропускала всё.
//
// Выборка ложится на частичный индекс миграции 0027 (только строки в удалении),
// поэтому на живой базе она почти всегда читает пустой индекс.
func (r *InstanceRepo) ListStuckDeleting(ctx context.Context, olderThan time.Duration) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id FROM instances
		 WHERE status = 'DELETING'
		   AND deleting_since IS NOT NULL
		   AND deleting_since < now() - make_interval(secs => $1::double precision)
		 ORDER BY deleting_since
		 LIMIT $2`, olderThan.Seconds(), stuckDeleteBatch)
	if err != nil {
		return nil, wrapPgErr(err, "Instance", "stuck-delete-sweep")
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, wrapPgErr(err, "Instance", "stuck-delete-sweep")
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapPgErr(err, "Instance", "stuck-delete-sweep")
	}
	return out, nil
}

// stuckDeleteBatch — сколько застрявших удалений разбирается за один проход.
// Каждое несёт вызовы к двум соседям, поэтому проход ограничен: остаток разберёт
// следующий, а соседи не получат разом сотни снятий.
const stuckDeleteBatch = 50

func (r *InstanceRepo) Delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var projectID string
	err = tx.QueryRow(ctx, `DELETE FROM instances WHERE id = $1 RETURNING project_id`, id).Scan(&projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: Instance %s not found", ports.ErrNotFound, id)
		}
		return wrapPgErr(err, "Instance", id)
	}
	// instance_network_interfaces (same-DB cascade child) снимается FK CASCADE.
	if err := emitCompute(ctx, tx, "Instance", id, "DELETED", map[string]any{"id": id}); err != nil {
		return ports.ErrInternal
	}
	if err := emitFGARegisterIntent(ctx, tx, fgaintent.EventUnregister, "Instance", id, projectID, nil); err != nil {
		return ports.ErrInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return wrapPgErr(err, "Instance", id)
	}
	return nil
}

// ---- internal helpers ----

func (r *InstanceRepo) mutateAndReload(ctx context.Context, id, eventType string, mutate func(context.Context, pgx.Tx) error) (*domain.Instance, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM instances WHERE id = $1)`, id).Scan(&exists); err != nil {
		return nil, wrapPgErr(err, "Instance", id)
	}
	if !exists {
		return nil, fmt.Errorf("%w: Instance %s not found", ports.ErrNotFound, id)
	}
	if err := mutate(ctx, tx); err != nil {
		return nil, wrapPgErr(err, "Instance", id)
	}
	q := fmt.Sprintf(`SELECT %s FROM instances WHERE id = $1`, instanceSelectCols)
	in, err := scanInstance(tx.QueryRow(ctx, q, id))
	if err != nil {
		return nil, wrapPgErr(err, "Instance", id)
	}
	if err := emitCompute(ctx, tx, "Instance", in.ID, eventType, instancePayload(in)); err != nil {
		return nil, ports.ErrInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, wrapPgErr(err, "Instance", id)
	}
	return in, nil
}

// ---- scan / args ----

func instanceInsertArgs(in *domain.Instance) ([]any, error) {
	labelsJSON, err := marshalJSONB(orEmptyMap(in.Labels), "Instance.labels")
	if err != nil {
		return nil, err
	}
	mdJSON, err := marshalJSONB(orEmptyMap(in.Metadata), "Instance.metadata")
	if err != nil {
		return nil, err
	}
	vmSpecJSON, err := marshalSpecJSONB(in.VMSpec, "Instance.vm_spec")
	if err != nil {
		return nil, err
	}
	ctrSpecJSON, err := marshalSpecJSONB(in.ContainerSpec, "Instance.container_spec")
	if err != nil {
		return nil, err
	}
	return []any{
		in.ID, in.ProjectID, in.CreatedAt, in.Name, in.Description, labelsJSON, in.ZoneID,
		instanceStatusName(in.Status), in.StatusReason,
		mdJSON, in.Hostname, in.FQDN, in.CPUGuaranteePercent, in.ServiceAccountID,
		int32(in.InstanceKind), in.MachineTypeID,
		in.EffectiveResources.VCPU, in.EffectiveResources.MemoryMiB, in.EffectiveResources.GPUs, in.EffectiveResources.GPUType,
		in.BootSource.Type, in.BootSource.ID, int32(in.BootSource.ImageKind), in.PlacementGroupID,
		vmSpecJSON, ctrSpecJSON,
	}, nil
}

// marshalSpecJSONB сериализует vm_spec/container_spec в JSONB (nil → NULL-байты).
func marshalSpecJSONB(v any, field string) ([]byte, error) {
	switch spec := v.(type) {
	case *domain.VMSpec:
		if spec == nil {
			return nil, nil
		}
		return marshalJSONB(spec, field)
	case *domain.ContainerSpec:
		if spec == nil {
			return nil, nil
		}
		return marshalJSONB(spec, field)
	default:
		return nil, nil
	}
}

func scanInstance(row scannable) (*domain.Instance, error) {
	var in domain.Instance
	var labelsJSON, mdJSON, vmSpecJSON, ctrSpecJSON []byte
	var statusName string
	var kind, imageKind int32
	if err := row.Scan(
		&in.ID, &in.ProjectID, &in.CreatedAt, &in.Name, &in.Description, &labelsJSON, &in.ZoneID,
		&statusName, &in.StatusReason,
		&mdJSON, &in.Hostname, &in.FQDN, &in.CPUGuaranteePercent, &in.ServiceAccountID,
		&kind, &in.MachineTypeID,
		&in.EffectiveResources.VCPU, &in.EffectiveResources.MemoryMiB, &in.EffectiveResources.GPUs, &in.EffectiveResources.GPUType,
		&in.BootSource.Type, &in.BootSource.ID, &imageKind, &in.PlacementGroupID,
		&vmSpecJSON, &ctrSpecJSON,
	); err != nil {
		return nil, err
	}
	if err := unmarshalJSONB(labelsJSON, &in.Labels, "Instance.labels"); err != nil {
		return nil, err
	}
	if err := unmarshalJSONB(mdJSON, &in.Metadata, "Instance.metadata"); err != nil {
		return nil, err
	}
	in.Status = instanceStatusFromName(statusName)
	in.InstanceKind = domain.InstanceKind(kind)
	in.BootSource.ImageKind = domain.ImageKind(imageKind)
	if len(vmSpecJSON) > 0 {
		in.VMSpec = &domain.VMSpec{}
		if err := unmarshalJSONB(vmSpecJSON, in.VMSpec, "Instance.vm_spec"); err != nil {
			return nil, err
		}
	}
	if len(ctrSpecJSON) > 0 {
		in.ContainerSpec = &domain.ContainerSpec{}
		if err := unmarshalJSONB(ctrSpecJSON, in.ContainerSpec, "Instance.container_spec"); err != nil {
			return nil, err
		}
	}
	return &in, nil
}

func instanceStatusName(s domain.InstanceStatus) string {
	if v, ok := computev1.Instance_Status_name[int32(s)]; ok { // #nosec G115 -- s — domain.InstanceStatus (малый enum, зеркалит proto); индекс в Instance_Status_name
		return v
	}
	return "STATUS_UNSPECIFIED"
}

func instanceStatusFromName(s string) domain.InstanceStatus {
	if v, ok := computev1.Instance_Status_value[s]; ok {
		return domain.InstanceStatus(v)
	}
	return domain.InstanceStatusUnspecified
}

// orEmptyMap возвращает пустую map вместо nil (JSONB-колонки NOT NULL DEFAULT '{}').
func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
