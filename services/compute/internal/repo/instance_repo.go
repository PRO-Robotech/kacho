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

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/pkg/singlepass"
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
	`hostname, fqdn, cpu_guarantee_percent, service_account_id, ` +
	`instance_kind, machine_type_id, eff_vcpu, eff_memory_mib, eff_gpus, eff_gpu_type, ` +
	`bs_type, bs_id, bs_image_kind, placement_group_id, vm_spec, container_spec`

// instanceSelectCols — тот же список для SELECT/RETURNING, но machine_type_id
// читается через COALESCE: колонка NULLable (0017 — FK на machine_types(id),
// а NOT NULL с пустой строкой по умолчанию не FK-able), тогда как domain-тип
// остаётся `string`. NULL («тип не задан») читается как "" — как и до 0017.
// Симметрично на записи — NULLIF (см. Insert/Update).
// Набор ключей входа читается ТЕМ ЖЕ стейтментом, подзапросом. Отдельное чтение
// на каждую машину дало бы запрос на строку списка — цена, которую платит
// вызывающий за то, что мы разложили связь по двум таблицам, а не он.
const instanceSelectCols = `id, project_id, created_at, name, description, labels, zone_id, status, status_reason, ` +
	`hostname, fqdn, cpu_guarantee_percent, service_account_id, ` +
	`instance_kind, COALESCE(machine_type_id,'') AS machine_type_id, eff_vcpu, eff_memory_mib, eff_gpus, eff_gpu_type, ` +
	`bs_type, bs_id, bs_image_kind, placement_group_id, vm_spec, container_spec, ` +
	`COALESCE((SELECT array_agg(g.guest_access_key_id ORDER BY g.guest_access_key_id) ` +
	`FROM instance_guest_access_keys g WHERE g.instance_id = instances.id), '{}') AS guest_access_key_ids`

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
func (r *InstanceRepo) Insert(ctx context.Context, in *domain.Instance) (*domain.Instance, []ownerregister.Registration, error) {
	insertArgs, err := instanceInsertArgs(in)
	if err != nil {
		return nil, nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Списания предела здесь НЕТ, и это не пропуск: его делает триггер учёта,
	// стоящий на самой таблице (миграция 0036). Вызов отсюда защищал бы ровно
	// тот путь, который через него проходит, — а «появился второй писатель» и
	// есть механизм, которым счётчик расходится с реальностью. Отказ триггера
	// приезжает сюда обычной ошибкой вставки и классифицируется `wrapPgErr`.

	// Когерентность размещения — УСЛОВИЕ САМОЙ ВСТАВКИ, а не вопрос перед ней.
	//
	// Строка появляется либо когерентной, либо не появляется вовсе. Проверка
	// «прочитал группу → сравнил → вставил» разъезжается под конкуренцией и
	// защищает ровно тот путь, который через неё проходит.
	//
	// Ноль строк из вставки означает: группа не той зоны, не того региона, не
	// того проекта либо её нет. Все четыре исхода отвечают ОДИНАКОВО — иначе по
	// различию ответов читался бы состав чужого проекта.
	if err := checkPlacementCoherence(ctx, tx, in); err != nil {
		return nil, nil, err
	}

	const qIns = `INSERT INTO instances (` + instanceCols + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULLIF($15,''),$16,$17,$18,$19,$20,$21,$22,NULLIF($23,''),$24,$25) RETURNING ` + instanceSelectCols
	created, err := scanInstance(tx.QueryRow(ctx, qIns, insertArgs...))
	if err != nil {
		return nil, nil, wrapPgErr(err, "Instance", in.Name)
	}
	// Связь с ключами входа — в той же транзакции, что сама машина. Иначе
	// машина существовала бы с пустым набором ключей ровно до второй записи, а
	// на её отказе — навсегда: в неё было бы некому войти, и это выглядело бы
	// как успешное создание.
	if err := replaceGuestKeyBindings(ctx, tx, created.ID, created.ProjectID, in.GuestAccessKeyIDs); err != nil {
		return nil, nil, err
	}
	// Набор перечитывается, а не зеркалится из входа: вставка идёт ПОСЛЕ строки
	// машины, поэтому подзапрос в её RETURNING видел пустой набор — а отдать
	// вызывающему вход вместо записанного значило бы подтвердить то, чего мы не
	// проверяли.
	if created.GuestAccessKeyIDs, err = guestKeyIDsOf(ctx, tx, created.ID); err != nil {
		return nil, nil, err
	}
	if err := emitCompute(ctx, tx, "Instance", created.ID, "CREATED", instancePayload(created)); err != nil {
		return nil, nil, ports.ErrInternal
	}
	// Журнал — В ТОЙ ЖЕ транзакции. Провайдер про наши принадлежности не знает,
	// значит «кто это сделал» способны записать только мы, и только здесь: после
	// коммита запись теряется ровно при том отказе, ради которого журнал нужен.
	actor, onBehalf := auditPrincipals(ctx)
	if err := emitAudit(ctx, tx, AuditEvent{
		EventType:    "instance.create",
		ResourceType: "Instance",
		ResourceID:   created.ID,
		ProjectID:    created.ProjectID,
		Actor:        actor,
		OnBehalfOf:   onBehalf,
		Payload:      map[string]any{"name": created.Name, "zone_id": created.ZoneID},
	}); err != nil {
		return nil, nil, ports.ErrInternal
	}
	// FGA owner-tuple register-intent for the Instance in the SAME writer-tx,
	// carrying the instance labels + parent-scope to feed IAM resource_mirror.
	reg, err := emitFGARegisterIntent(ctx, tx, fgaintent.EventRegister, "Instance", created.ID, created.ProjectID, created.Labels)
	if err != nil {
		return nil, nil, ports.ErrInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, wrapPgErr(err, "Instance", in.Name)
	}
	return created, registrationsOf(reg), nil
}

// Update обновляет mutable descriptive/resource поля ВМ + outbox UPDATED.
// status НЕ трогается — им владеет исключительно SetStatusCAS/MarkDeleting.
//
// emitLabelsRegister: when true a fresh FGA register-intent carrying the updated
// labels + parent-scope is emitted IN THE SAME writer-tx as the UPDATE (atomic) so
// the IAM resource_mirror stays in sync.
func (r *InstanceRepo) Update(ctx context.Context, in *domain.Instance, emitLabelsRegister bool, changed []string) (*domain.Instance, []ownerregister.Registration, error) {
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
			return nil, nil, err
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
			return nil, nil, err
		}
		us.add("vm_spec", vmSpecJSON)
	}
	// requireStopped: sizing (machine_type_id/cpu_guarantee_percent) и placement
	// (placement_group_id) разрешены ТОЛЬКО пока instance STOPPED (COMP-1 F10). В
	// COMP-1 STOPPED недостижимо (Stop=COMP-2) → service отвергает эти маски sync
	// первым (always-reject). CAS `AND status='STOPPED'` — DB-level backstop
	// (defense-in-depth; NOT software Get→check→UPDATE), актуален в COMP-2.
	requireStopped := false
	checkGroupCoherence := false
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
		us.addNullIfEmpty("placement_group_id", in.PlacementGroupID)
		checkGroupCoherence = true
		requireStopped = true
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Когерентность размещения проверяется и на правке — в ТОЙ ЖЕ транзакции.
	// Проверка только на создании защищала бы ровно один путь: перевести машину
	// в группу другой зоны можно было бы правкой, и результат был бы тем же
	// несогласованным размещением, только заведённым позже.
	if checkGroupCoherence {
		if err := checkPlacementCoherence(ctx, tx, in); err != nil {
			return nil, nil, err
		}
	}

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
				return nil, nil, wrapPgErr(e2, "Instance", in.ID)
			}
			if !exists {
				return nil, nil, fmt.Errorf("%w: Instance %s not found", ports.ErrNotFound, in.ID)
			}
			return nil, nil, fmt.Errorf("%w: instance must be STOPPED to change sizing or placement", ports.ErrFailedPrecondition)
		}
		return nil, nil, wrapPgErr(err, "Instance", in.ID)
	}
	// Набор ключей входа заменяется ЦЕЛИКОМ и только когда маска его назвала.
	// Не названный маской набор не трогается — иначе правка имени машины молча
	// снимала бы с неё все ключи, и это выглядело бы как успешное переименование.
	if _, ok := ch["guest_access_key_ids"]; ok {
		if err := replaceGuestKeyBindings(ctx, tx, updated.ID, updated.ProjectID, in.GuestAccessKeyIDs); err != nil {
			return nil, nil, err
		}
		if updated.GuestAccessKeyIDs, err = guestKeyIDsOf(ctx, tx, updated.ID); err != nil {
			return nil, nil, err
		}
	}

	if err := emitCompute(ctx, tx, "Instance", updated.ID, "UPDATED", instancePayload(updated)); err != nil {
		return nil, nil, ports.ErrInternal
	}
	var reg ownerregister.Registration
	if emitLabelsRegister {
		var eerr error
		if reg, eerr = emitFGARegisterIntent(ctx, tx, fgaintent.EventRegister, "Instance", updated.ID, updated.ProjectID, updated.Labels); eerr != nil {
			return nil, nil, ports.ErrInternal
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, wrapPgErr(err, "Instance", in.ID)
	}
	return updated, registrationsOf(reg), nil
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
// схему/контракт и идёт через db- и system-design-ревью.
//
// ЧЕМ ОСТАТОК ЗАКРЫТ СЕГОДНЯ — и чем НЕ закрыт (перемерено 2026-08-07).
// Прежняя редакция отсылала к «компенсации инициатора (compensation-outbox) и
// sweeper'у владельца привязки». Ни того, ни другого в дереве НЕТ:
// `git grep -ln compensation_outbox` даёт 10 файлов и ни одного под services/compute
// (число включает ЭТОТ файл: слово в комментарии само стало совпадением — поэтому
// предикат о собственном отсутствии предмета обязан вычитать место, где он написан)
// (совпадения принадлежат iam и относятся к другому предмету — компенсации
// регистрации провайдера); sweeper'а, реклеймящего отвязанные ресурсы, у vpc и
// storage — 0 файлов. То есть комментарий называл механизм существующим и
// провоцировал ровно то, от чего предостерегает абзацем выше.
//
// Что закрывает остаток НА САМОМ ДЕЛЕ: возобновляемая сага удаления поверх
// собственной durable-строки — `instances.deleting_since` (миграция 0027) плюс
// добиватель `cmd/compute/stuck_delete_finisher.go`. Он работает потому, что
// снимаемое ПЕРЕОТКРЫВАЕТСЯ у владельцев (ListByInstance / ListAttachments), а не
// хранится списком в памяти воркера.
//
// Очередь компенсации здесь не нужна и завести её сейчас НЕЛЬЗЯ: у неё не было бы
// производителя. Саги запуска, которую B12 компенсирует, в дереве ещё нет —
// `Launch-*Specs` структурно валидируются и НЕ материализуются (контракт сам
// говорит: «the IPAM/NIC materialize saga is COMP-2»), клиента адреса/IPAM у
// compute нет, а `StorageClient` несёт только Attach/Detach/ListAttachments.
// Очередь, не получающая ни одной строки, неотличима от исправной — этот класс
// уже стоил нам инцидента: отказ в правах классифицировался временным, голова
// партиции заклинивала, и ни одна строка не доезжала за всю жизнь очереди.
// Компенсация запуска приземляется ВМЕСТЕ с материализующей сагой, не раньше.
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
//
// Предел имеет смысл ровно потому, что проход разведён между репликами
// (TryClaimStuckDeleteSweep). Без развода он умножался бы на число реплик, и
// обещание «соседи не получат разом сотни снятий» было бы ложным при первом же
// срабатывании автомасштабирования.
const stuckDeleteBatch = 50

// stuckDeleteSweepLock — имя замка прохода добивателя. Домен в имени обязателен:
// пространство ключей общее на всю базу.
const stuckDeleteSweepLock = "kacho.compute.stuck-delete-finisher"

// TryClaimStuckDeleteSweep берёт проход добивателя на одну реплику.
//
// # Почему замок прохода, а не клейм строки
//
// Добиватель — бэкстоп: он ходит раз в пять минут по партии в stuckDeleteBatch
// строк и не обязан ускоряться от числа реплик. Клейм строки потребовал бы
// собственной колонки-аренды и миграции ради работы, которой в здоровой системе
// нет вовсе (частичный индекс миграции 0027 почти всегда пуст).
//
// # Что здесь стояло раньше
//
// Развода не было, и это было ЗАПИСАНО как решение: «отдельной блокировки не
// нужно, каждый шаг идемпотентен». Про корректность это верно — повторное снятие
// привязки у владельца есть no-op, — и потому дубль ничего не портил. Неверен был
// вывод: выборка детерминирована (`ORDER BY deleting_since LIMIT 50`), поэтому N
// реплик берут ТЕ ЖЕ пятьдесят строк и зовут по каждой двух соседей. Предел
// партии, обоснованный бережностью к соседям, молча умножался на число реплик.
func (r *InstanceRepo) TryClaimStuckDeleteSweep(ctx context.Context) (func(context.Context), bool, error) {
	release, ok, err := singlepass.TryAcquire(ctx, r.pool, stuckDeleteSweepLock)
	if err != nil || !ok {
		return nil, false, err
	}
	return func(rctx context.Context) { release(rctx) }, true, nil
}

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

	// Возврат предела делает тот же триггер учёта, в ТОЙ ЖЕ транзакции, что
	// удаление строки: возврат вне её оставил бы счётчик завышенным при откате —
	// проект платил бы местом за машину, которой нет. Свойство получается by
	// construction, на КАЖДОМ пути высвобождения, а не перечислением путей.

	actorDel, onBehalfDel := auditPrincipals(ctx)
	if err := emitAudit(ctx, tx, AuditEvent{
		EventType:    "instance.delete",
		ResourceType: "Instance",
		ResourceID:   id,
		ProjectID:    projectID,
		Actor:        actorDel,
		OnBehalfOf:   onBehalfDel,
	}); err != nil {
		return ports.ErrInternal
	}
	if err := emitCompute(ctx, tx, "Instance", id, "DELETED", map[string]any{"id": id}); err != nil {
		return ports.ErrInternal
	}
	if _, err := emitFGARegisterIntent(ctx, tx, fgaintent.EventUnregister, "Instance", id, projectID, nil); err != nil {
		return ports.ErrInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return wrapPgErr(err, "Instance", id)
	}
	return nil
}

// ---- scan / args ----

func instanceInsertArgs(in *domain.Instance) ([]any, error) {
	labelsJSON, err := marshalJSONB(orEmptyMap(in.Labels), "Instance.labels")
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
		in.Hostname, in.FQDN, in.CPUGuaranteePercent, in.ServiceAccountID,
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
	var labelsJSON, vmSpecJSON, ctrSpecJSON []byte
	// Колонка NULLable: отсутствие ссылки представлено NULL-ом, а не пустой
	// строкой. Доменный тип остаётся строкой, и NULL читается как "" — так же,
	// как это уже сделано для типа машины.
	var placementGroupID *string
	var statusName string
	var kind, imageKind int32
	if err := row.Scan(
		&in.ID, &in.ProjectID, &in.CreatedAt, &in.Name, &in.Description, &labelsJSON, &in.ZoneID,
		&statusName, &in.StatusReason,
		&in.Hostname, &in.FQDN, &in.CPUGuaranteePercent, &in.ServiceAccountID,
		&kind, &in.MachineTypeID,
		&in.EffectiveResources.VCPU, &in.EffectiveResources.MemoryMiB, &in.EffectiveResources.GPUs, &in.EffectiveResources.GPUType,
		&in.BootSource.Type, &in.BootSource.ID, &imageKind, &placementGroupID,
		&vmSpecJSON, &ctrSpecJSON, &in.GuestAccessKeyIDs,
	); err != nil {
		return nil, err
	}
	if placementGroupID != nil {
		in.PlacementGroupID = *placementGroupID
	}
	if err := unmarshalJSONB(labelsJSON, &in.Labels, "Instance.labels"); err != nil {
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
