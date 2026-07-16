// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package volume — use-case (бизнес-логика) ресурса Volume.
//
// Use-case слой чистой архитектуры: импортирует domain + порты (Reader/Writer,
// Geo/IAM peer-клиенты) + corelib operations; НЕ тянет pgx/grpc-transport.
// Публичные Get/List — read-only (sync); мутации Create/Update/Delete возвращают
// operation.Operation (async LRO): sync-фаза валидирует и пишет LRO-строку
// (done=false), фоновый corelib-worker выполняет доменную запись и финализирует
// (done=true, response=Volume/Empty либо error). Клиент поллит
// OperationService.Get(id) до done. Internal Attach/Detach/ListAttachments (:9091,
// sync CAS) реализованы (S2); GetInternal (infra-проекция) — анкер data-plane (§0.3).
package volume

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/filter"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"
)

// Pagination — вход для List: cursor-пагинация (page_size + opaque page_token) +
// project-scope и filter=name (listauthz-фильтрация энфорсится authz-слоем).
type Pagination struct {
	PageSize  int64
	PageToken string
	ProjectID string
	Filter    string // name=<v> уже распарсен use-case-слоем в чистое значение
}

// VolumeUpdate — резолвнутый набор mutable-изменений для Writer.Update. nil-поле →
// без изменения (COALESCE); LabelsSet различает «labels в маске» от «не трогать».
type VolumeUpdate struct {
	Name        *string
	Description *string
	Labels      map[string]string
	LabelsSet   bool
	SizeBytes   *int64
}

// Reader — read-порт томов (Get/List + internal-проекции). CQRS-разделён с Writer.
type Reader interface {
	Get(ctx context.Context, id string) (*domain.Volume, error)
	List(ctx context.Context, p Pagination) ([]*domain.Volume, string, error)
	// GetInternal — full (infra) проекция Volume, internal-only (:9091) — S2.
	GetInternal(ctx context.Context, id string) (*domain.Volume, error)
	// ListAttachments — батч-чтение attachments по instance_id (compute-mirror) — S2.
	ListAttachments(ctx context.Context, instanceIDs []string) ([]*domain.VolumeAttachment, error)
}

// Writer — write-порт мутаций томов (Insert/Update/Delete + attach/detach CAS).
// Update — атомарный размер-CAS increase-only + mutable COALESCE (data-integrity.md),
// НЕ software TOCTOU.
type Writer interface {
	Insert(ctx context.Context, v *domain.Volume) (*domain.Volume, error)
	Update(ctx context.Context, id string, u VolumeUpdate) (*domain.Volume, error)
	Delete(ctx context.Context, id string) error
	Attach(ctx context.Context, a *domain.VolumeAttachment) error
	Detach(ctx context.Context, volumeID, instanceID string) error
}

// GeoClient — порт peer-валидации zone_id через kacho-geo (ZoneService.Get,
// fail-closed). Ребро storage→geo (one-way).
type GeoClient interface {
	EnsureZoneExists(ctx context.Context, zoneID string) error
}

// IAMClient — порт peer-валидации project_id через kacho-iam (ProjectService.Get,
// fail-closed). Ребро storage→iam (one-way).
type IAMClient interface {
	EnsureProjectExists(ctx context.Context, projectID string) error
}

// OwnerConfirmer — read-after-register owner-tuple проба Volume.Create (opgate P5).
// Confirm возвращает confirmed=true, когда creator имеет mutate-relation (editor) на
// storage_volume:<id> в FGA — т.е. gateway scope_extractor Check немедленной мутации
// (Update/Delete) уже даёт ALLOW (FIX-2 consistency: та же read-after-register проба,
// что энфорсит анти-BOLA-резолв gateway'я). Read-only, идемпотентна. Impl — reuse
// существующего InternalIAMService.Check-клиента (ребро storage→iam, тот же authzConn),
// живёт в internal/check; нового cross-service ребра нет (OTG-08). nil → confirm-gate
// пропускается (dev/no-authz): прежний плоский Run без owner-tuple opgate.
type OwnerConfirmer interface {
	Confirm(ctx context.Context, principal operations.Principal, volumeID string) (bool, error)
}

// ErrToStatus маппит доменную/repo sentinel-ошибку в transport-status, сохраняемый
// async-worker'ом в Operation.error. Инжектится composition root'ом
// (serviceerr.ToStatus). Пустой (nil) → identity.
type ErrToStatus func(error) error

// knownUpdateFields — mutable-поля Volume.Update (update_mask discipline).
// Immutable-поля НЕ входят в known-set (immutable-switch отвергает их раньше
// конвенц-сообщением, а не generic «unknown field»).
var knownUpdateFields = map[string]struct{}{
	"name":        {},
	"description": {},
	"labels":      {},
	"size_bytes":  {},
}

// UseCase — бизнес-логика Volume поверх CQRS-портов Reader/Writer, peer-клиентов,
// LRO-стека operations и инжектированного transport-mapper'а errStatus.
type UseCase struct {
	reader    Reader
	writer    Writer
	geo       GeoClient
	iam       IAMClient
	ops       operations.Repo
	errStatus ErrToStatus
	// registrar — синхронная регистрация owner-tuple в kacho-iam после commit
	// (immediate анти-BOLA-резолв; nil → sync-путь пропускается, остаётся async
	// register-drainer как at-least-once backstop). Инжектится WithRegistrar.
	registrar fgaregister.Registrar
	// confirmer — read-after-register owner-tuple проба (opgate P5). non-nil →
	// Create-op достигает success-`done` ТОЛЬКО после confirmed=true; nil →
	// прежний плоский Run без gate. Инжектится WithOwnerConfirm.
	confirmer OwnerConfirmer
	// confirmWorker — явный LRO-worker для confirm-gated Create-dispatch (кастомный
	// drain-target / тесты с коротким confirmation-deadline). nil → package-level
	// default-registry (operations.RunWithConfirm), как остальные async-мутации
	// storage. Инжектится WithConfirmWorker.
	confirmWorker *operations.Worker
}

// New собирает UseCase для Volume. reader/writer — CQRS-разделённые порты;
// geo/iam — peer-клиенты cross-domain валидации; ops — corelib LRO-репозиторий;
// errStatus — инжектированный маппер sentinel→gRPC-status.
func New(reader Reader, writer Writer, geo GeoClient, iam IAMClient, ops operations.Repo, errStatus ErrToStatus) *UseCase {
	if errStatus == nil {
		errStatus = func(err error) error { return err }
	}
	return &UseCase{reader: reader, writer: writer, geo: geo, iam: iam, ops: ops, errStatus: errStatus}
}

// WithRegistrar подключает синхронный owner-tuple registrar (Decision 2, парити vpc):
// после успешного Create-commit owner-grant регистрируется сразу, чтобы public
// Get/Update/Delete и internal Attach/Detach на свежий том разрешались без гонки с
// async drainer'ом. Best-effort: durable outbox-intent + register-drainer —
// at-least-once backstop, поэтому sync-ошибка НЕ валит Create (мутатор ban #9 async).
// nil registrar → sync-путь пропускается (dev/no-iam).
func (u *UseCase) WithRegistrar(r fgaregister.Registrar) *UseCase {
	u.registrar = r
	return u
}

// WithOwnerConfirm подключает read-after-register owner-tuple пробу (opgate P5).
// non-nil → Volume.Create-Operation достигает `done=true,result=response` ТОЛЬКО
// после подтверждения owner-tuple (creator имеет editor на storage_volume:<id> в
// FGA), закрывая 403-окно «no direct relations granted» на немедленной мутации
// (OTG-04). Fail-closed: confirm не достигнут за confirmation-deadline → op.error
// UNAVAILABLE «owner-tuple registration not confirmed» (worker, OTG-05); ресурс/
// register-intent durable во всех ветках. nil (dev/no-authz) → gate пропускается.
func (u *UseCase) WithOwnerConfirm(c OwnerConfirmer) *UseCase {
	u.confirmer = c
	return u
}

// WithConfirmWorker задаёт явный LRO-worker для confirm-gated Create-dispatch —
// кастомный drain-target либо тесты с коротким confirmation-deadline
// (operations.WithConfirmationDeadline). nil (прод-дефолт storage) → package-level
// default-registry (тот же worker, что дренирует остальные async-мутации; deadline
// из operations.ConfigureDefault в cmd/storage).
func (u *UseCase) WithConfirmWorker(w *operations.Worker) *UseCase {
	u.confirmWorker = w
	return u
}

// registerOwnerTuple — best-effort синхронная регистрация owner-tuple после commit.
// Ошибка НЕ пробрасывается: durable outbox-intent уже записан в writer-TX, а
// register-drainer применит его at-least-once (idempotent). Логируем WARN, чтобы
// потерянная sync-регистрация была видна (async backstop подхватит).
func (u *UseCase) registerOwnerTuple(ctx context.Context, item fgaregister.Item) {
	if u.registrar == nil {
		return
	}
	if err := u.registrar.Register(ctx, []fgaregister.Item{item}); err != nil {
		slog.WarnContext(ctx, "sync owner-tuple register failed; async drainer will apply",
			"object", item.Tuple.Object, "err", err)
	}
}

// idInvalid — malformed vol-id первым стейтментом (api-conventions.md): sync
// InvalidArgument "invalid volume id '<X>'". well-formed-но-нет → NotFound (repo.Get).
func idInvalid(id string) error {
	if !ids.IsValid(id, domain.PrefixVolume) {
		return fmt.Errorf("%w: invalid volume id '%s'", ports.ErrInvalidArg, id)
	}
	return nil
}

// Get возвращает Volume по id (malformed → sync InvalidArgument первым стейтментом).
func (u *UseCase) Get(ctx context.Context, id string) (*domain.Volume, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	v, err := u.reader.Get(ctx, id)
	if err != nil {
		return nil, u.errStatus(err)
	}
	return v, nil
}

// List возвращает тома (cursor-пагинация; garbage page_size → InvalidArgument;
// filter=name whitelisted через corelib filter).
func (u *UseCase) List(ctx context.Context, p Pagination) ([]*domain.Volume, string, error) {
	// projectId — обязательный scope публичного List (in-service backstop к gateway
	// scope_extractor {project,project_id}): пустой projectId вернул бы строки ВСЕХ
	// проектов (repo сужает лишь при ProjectID!=""), поэтому отвергаем СИНХРОННО
	// первым стейтментом — кросс-проектной утечки нет by construction (INV-10;
	// docs/architecture/overview.md; acceptance CS1-S1-13/GAP-C).
	if p.ProjectID == "" {
		return nil, "", u.errStatus(fmt.Errorf("%w: projectId is required", ports.ErrInvalidArg))
	}
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	p.PageSize = size
	// filter=name — whitelist через corelib filter; невалидное поле/форма →
	// InvalidArgument. Repo получает уже чистое значение name (не raw-выражение).
	if p.Filter != "" {
		ast, ferr := filter.Parse(p.Filter, []string{"name"})
		if ferr != nil {
			return nil, "", u.errStatus(fmt.Errorf("%w: %s", ports.ErrInvalidArg, ferr.Error()))
		}
		p.Filter = ast.Value
	}
	vols, next, err := u.reader.List(ctx, p)
	if err != nil {
		return nil, "", u.errStatus(err)
	}
	return vols, next, nil
}

// Create создаёт Volume (async Operation). Малформ/невалидный вход отвергается
// СИНХРОННО (InvalidArgument: size/name), cross-domain ссылки (zone→geo,
// project→iam) валидируются на request-path fail-closed (peer Unavailable →
// UNAVAILABLE). Валидный вход → LRO-строка + worker (writer.Insert; state→READY
// сразу; disk_type/source_snapshot FK → Operation error).
func (u *UseCase) Create(ctx context.Context, v *domain.Volume) (*operations.Operation, error) {
	if err := v.Validate(); err != nil {
		return nil, u.errStatus(fmt.Errorf("%w: %s", ports.ErrInvalidArg, err.Error()))
	}
	if err := u.geo.EnsureZoneExists(ctx, v.ZoneID); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.iam.EnsureProjectExists(ctx, v.ProjectID); err != nil {
		return nil, u.errStatus(err)
	}
	v.ID = ids.NewID(domain.PrefixVolume)
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Create volume %s", v.ID),
		&storagev1.CreateVolumeMetadata{VolumeId: v.ID})
	if err != nil {
		return nil, err
	}
	op.ResourceID = v.ID
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	created := *v
	fn := func(ctx context.Context) (*anypb.Any, error) {
		res, derr := u.writer.Insert(ctx, &created)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		// owner-tuple: durable register-intent уже в writer-TX (repo); синхронно
		// регистрируем для immediate анти-BOLA-резолва (best-effort, backstop — drainer).
		// sync-registrar идёт ЗДЕСЬ (post-commit, внутри fn); confirm-проба — ПОСЛЕ fn.
		u.registerOwnerTuple(ctx, fgaregister.VolumeItem(res.ProjectID, res.ID, res.Labels))
		return marshalVolume(res)
	}
	u.dispatchCreate(ctx, op.ID, op.Principal, v.ID, fn)
	return &op, nil
}

// dispatchCreate маршрутизирует Create-fn в LRO-worker (owner-tuple opgate P5). При
// подключённом confirmer success-`done` достигается ТОЛЬКО после read-after-register
// confirm owner-tuple (creator ↦ editor @ storage_volume:<id> в FGA — та же проба,
// что энфорсит gateway scope_extractor на немедленной мутации, FIX-2), иначе
// fail-closed по confirmation-deadline (worker → op.error Unavailable «owner-tuple
// registration not confirmed»). confirm-проба идёт ПОСЛЕ fn (sync-registrar уже
// отработал внутри fn post-commit). Без confirmer (dev/no-authz) confirm==nil →
// прежний плоский Run без gate (nil-safe, back-compat). confirmWorker!=nil → явный
// worker (тесты/кастомный drain-target), иначе package-level default-registry.
func (u *UseCase) dispatchCreate(ctx context.Context, opID string, principal operations.Principal, volumeID string, fn func(context.Context) (*anypb.Any, error)) {
	var confirm operations.ConfirmFunc
	if u.confirmer != nil {
		confirm = func(cctx context.Context) (bool, error) {
			return u.confirmer.Confirm(cctx, principal, volumeID)
		}
	}
	if u.confirmWorker != nil {
		operations.RunWithWorkerConfirm(u.confirmWorker, ctx, u.ops, opID, fn, confirm)
		return
	}
	operations.RunWithConfirm(ctx, u.ops, opID, fn, confirm)
}

// Update меняет mutable-поля Volume (async Operation). Sync-фаза: malformed-id
// первым стейтментом → immutable-switch (ДО UpdateMask, api-conventions gotcha) →
// UpdateMask known-set → name-format. Пустой mask → full-object PATCH (immutable из
// тела silently игнорируются). Async: writer.Update (size-CAS increase-only →
// Operation error "Volume size can only be increased").
func (u *UseCase) Update(ctx context.Context, id string, mask []string, name, description string, labels map[string]string, sizeBytes int64) (*operations.Operation, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	// immutable-switch ДО UpdateMask: known-set НЕ содержит immutable-полей, иначе
	// UpdateMask отверг бы их generic'ом «unknown field» вместо конвенц-сообщения.
	for _, p := range mask {
		switch p {
		case "zone_id", "disk_type_id", "block_size", "source_snapshot_id", "used_by":
			return nil, u.errStatus(fmt.Errorf("%w: %s is immutable after Volume.Create", ports.ErrInvalidArg, p))
		}
	}
	if err := validate.UpdateMask("update_mask", mask, knownUpdateFields); err != nil {
		return nil, err
	}
	upd, err := resolveUpdate(mask, name, description, labels, sizeBytes)
	if err != nil {
		return nil, u.errStatus(err)
	}
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Update volume %s", id),
		&storagev1.UpdateVolumeMetadata{VolumeId: id})
	if err != nil {
		return nil, err
	}
	op.ResourceID = id
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		res, derr := u.writer.Update(ctx, id, upd)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		return marshalVolume(res)
	})
	return &op, nil
}

// Delete удаляет Volume (async Operation). Malformed-id → sync InvalidArgument.
// Привязанный том → FK RESTRICT → Operation error FailedPrecondition
// "Volume <id> is in use" (§3.6). Успех → response=Empty.
func (u *UseCase) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Delete volume %s", id),
		&storagev1.DeleteVolumeMetadata{VolumeId: id})
	if err != nil {
		return nil, err
	}
	op.ResourceID = id
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		if derr := u.writer.Delete(ctx, id); derr != nil {
			return nil, u.errStatus(derr)
		}
		return anypb.New(&emptypb.Empty{})
	})
	return &op, nil
}

// ListOperations возвращает операции по конкретному Volume (corelib-standard:
// resource_id-фильтр общей operations-таблицы). Malformed vol-id → sync
// InvalidArgument (парити с Get).
func (u *UseCase) ListOperations(ctx context.Context, volumeID string, p Pagination) ([]operations.Operation, string, error) {
	if err := idInvalid(volumeID); err != nil {
		return nil, "", u.errStatus(err)
	}
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	return u.ops.List(ctx, operations.ListFilter{ResourceID: volumeID, PageSize: size, PageToken: p.PageToken})
}

// Attach — атомарный CAS-insert строки volume_attachments (internal :9091, §3.2).
// Malformed vol-id → sync InvalidArgument первым стейтментом (парити с Get). Успех →
// обновлённый Volume (derived IN_USE) для AttachVolumeResponse. Sync (CAS мгновенный);
// tenant-мутация остаётся async через compute-AttachDisk (ban #9 не нарушен).
func (u *UseCase) Attach(ctx context.Context, a *domain.VolumeAttachment) (*domain.Volume, error) {
	if err := idInvalid(a.VolumeID); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.writer.Attach(ctx, a); err != nil {
		return nil, u.errStatus(err)
	}
	v, err := u.reader.Get(ctx, a.VolumeID)
	if err != nil {
		return nil, u.errStatus(err)
	}
	return v, nil
}

// Detach — идемпотентное удаление строки volume_attachments (internal :9091, §3.3).
// Malformed vol-id → sync InvalidArgument. Успех → обновлённый Volume (derived
// AVAILABLE) для DetachVolumeResponse.
func (u *UseCase) Detach(ctx context.Context, volumeID, instanceID string) (*domain.Volume, error) {
	if err := idInvalid(volumeID); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.writer.Detach(ctx, volumeID, instanceID); err != nil {
		return nil, u.errStatus(err)
	}
	v, err := u.reader.Get(ctx, volumeID)
	if err != nil {
		return nil, u.errStatus(err)
	}
	return v, nil
}

// ListAttachments — батч-чтение attachments по instance_id (internal :9091) — S2.
func (u *UseCase) ListAttachments(ctx context.Context, instanceIDs []string) ([]*domain.VolumeAttachment, error) {
	return u.reader.ListAttachments(ctx, instanceIDs)
}

// GetInternal — full (infra) проекция Volume (internal :9091) — S2/data-plane.
func (u *UseCase) GetInternal(ctx context.Context, id string) (*domain.Volume, error) {
	return u.reader.GetInternal(ctx, id)
}

// resolveUpdate резолвит mutable-изменения из mask + тела. Пустой mask →
// full-object PATCH (все mutable из тела; size применяется лишь если >0 — 0 не
// «уменьшение до нуля», а «не задано»). Непустой mask → только перечисленные поля.
// name (если применяется) валидируется по тем же правилам, что Create.
func resolveUpdate(mask []string, name, description string, labels map[string]string, sizeBytes int64) (VolumeUpdate, error) {
	var u VolumeUpdate
	apply := func(field string) bool {
		if len(mask) == 0 {
			return true // full-object PATCH
		}
		for _, m := range mask {
			if m == field {
				return true
			}
		}
		return false
	}
	if apply("name") {
		if err := domain.VolumeName(name).Validate(); err != nil {
			return VolumeUpdate{}, fmt.Errorf("%w: %s", ports.ErrInvalidArg, err.Error())
		}
		n := name
		u.Name = &n
	}
	if apply("description") {
		d := description
		u.Description = &d
	}
	if apply("labels") {
		u.Labels = labels
		u.LabelsSet = true
	}
	if apply("size_bytes") {
		if len(mask) == 0 && sizeBytes <= 0 {
			// full-patch без явного размера — не трогаем (0 не значит «shrink to 0»).
		} else {
			s := sizeBytes
			u.SizeBytes = &s
		}
	}
	return u, nil
}

// marshalVolume упаковывает domain.Volume в Operation.response через единый
// protoconv.Volume (та же проекция, что handler и LRO-recovery — без дрейфа полей).
func marshalVolume(v *domain.Volume) (*anypb.Any, error) {
	return anypb.New(protoconv.Volume(v))
}
