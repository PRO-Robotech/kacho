// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package zone — use-case (бизнес-логика) каталога Zone.
//
// Use-case слой чистой архитектуры: импортирует domain + порт Repo + corelib
// operations, не тянет pgx/transport. Публичные ZoneService.Get/List — read-only
// (sync), LEAN public-проекция. Admin CRUD идёт через InternalZoneService на
// :9091 и возвращает синхронно-завершённый Operation{done:true} (config-INSERT,
// module-geo rule 4). GetInternal возвращает FULL Internal-проекцию (status +
// infra°) синхронно.
package zone

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/lro"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/syncop"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/protoconv"
)

// Pagination — вход для List (page_size + opaque page_token + region_id +
// openForPlacement фильтры).
type Pagination struct {
	PageSize         int64
	PageToken        string
	RegionID         string
	OpenForPlacement bool
}

// UpdateParams — опциональные mutable-поля partial-Update зоны. nil → поле не
// меняется (repo COALESCE, single-statement, без TOCTOU). region_id НЕ входит —
// immutable после Create.
type UpdateParams struct {
	Status             *domain.GeoStatus
	HostClasses        *[]string
	FailureDomainCount *int32
	UnderlayAnchor     *string
	CapacityHint       *string
}

// CreateInput — вход InternalZoneService.Create (transport-neutral).
type CreateInput struct {
	ID       string
	RegionID string
	Status   domain.GeoStatus
	Infra    domain.ZoneInfra
}

// UpdateInput — вход InternalZoneService.Update (transport-neutral).
type UpdateInput struct {
	ID     string
	Mask   []string
	Status domain.GeoStatus
	Infra  domain.ZoneInfra
}

// Reader — read-порт (Get/GetInternal/List). CQRS-разделён с Writer.
type Reader interface {
	Get(ctx context.Context, id string) (*domain.Zone, error)
	GetInternal(ctx context.Context, id string) (*domain.Zone, error)
	List(ctx context.Context, p Pagination) ([]*domain.Zone, string, error)
}

// Writer — write-порт admin-мутаций (+outbox-emit в writer-tx).
type Writer interface {
	Insert(ctx context.Context, z *domain.Zone) (*domain.Zone, error)
	Update(ctx context.Context, id string, p UpdateParams) (*domain.Zone, error)
	Delete(ctx context.Context, id string) error
}

// Repo — композит Reader+Writer.
type Repo interface {
	Reader
	Writer
}

// ErrToStatus маппит sentinel repo-ошибку в transport-status (Operation.error).
type ErrToStatus func(error) error

// zoneUpdatable — known-set update_mask (mutable-поля). Immutable (id, regionId,
// infra.numericInfraId) НЕ входят — отвергаются immutable-switch ДО UpdateMask.
// `name` в набор НЕ входит и не может: поля у ресурса больше нет (#716).
// ОБЕ формы имени каждого многословного поля — см. разбор у regionUpdatable.
var zoneUpdatable = map[string]struct{}{
	"status":                     {},
	"infra.hostClasses":          {},
	"infra.host_classes":         {},
	"infra.failureDomainCount":   {},
	"infra.failure_domain_count": {},
	"infra.underlayAnchor":       {},
	"infra.underlay_anchor":      {},
	"infra.capacityHint":         {},
	"infra.capacity_hint":        {},
}

// UseCase — бизнес-логика Zone поверх Reader/Writer, LRO-стека и errStatus.
type UseCase struct {
	reader    Reader
	writer    Writer
	ops       syncop.Repo
	errStatus ErrToStatus
}

// New собирает UseCase для Zone.
func New(reader Reader, writer Writer, ops syncop.Repo, errStatus ErrToStatus) *UseCase {
	if errStatus == nil {
		errStatus = func(err error) error { return err }
	}
	return &UseCase{reader: reader, writer: writer, ops: ops, errStatus: errStatus}
}

// Get возвращает LEAN public-проекцию зоны по id.
func (u *UseCase) Get(ctx context.Context, id string) (*domain.Zone, error) {
	if err := domain.ValidateID("zone", id); err != nil {
		return nil, invalidArg(err.Error())
	}
	return u.reader.Get(ctx, id)
}

// GetInternal возвращает FULL Internal-проекцию (status + infra°). :9091-only.
func (u *UseCase) GetInternal(ctx context.Context, id string) (*domain.Zone, error) {
	if err := domain.ValidateID("zone", id); err != nil {
		return nil, invalidArg(err.Error())
	}
	return u.reader.GetInternal(ctx, id)
}

// List возвращает зоны (cursor-пагинация; garbage page_size → InvalidArgument).
func (u *UseCase) List(ctx context.Context, p Pagination) ([]*domain.Zone, string, error) {
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	p.PageSize = size
	return u.reader.List(ctx, p)
}

// Create — admin-создание зоны, возвращает синхронно-завершённый Operation.
// Порядок sync-валидации (первым стейтментом): malformed id → coupling.
// Fresh-default fail-safe: омитнутый status → DOWN. Несуществующий region_id →
// FK 23503 → op.error FailedPrecondition (DB-backstop; см. PHASE-0-gate ниже).
func (u *UseCase) Create(ctx context.Context, in CreateInput) (*operations.Operation, error) {
	if err := domain.ValidateID("zone", in.ID); err != nil {
		return nil, invalidArg(err.Error())
	}
	if err := domain.ValidateZoneCoupling(in.ID, in.RegionID); err != nil {
		return nil, invalidArg(err.Error())
	}
	// Проверка имени стояла ЗДЕСЬ и снята вместе с полем (#716): идентичность у
	// зоны одна — идентификатор, и его форму судят две проверки выше.
	if err := in.Status.Validate(); err != nil {
		return nil, invalidArg(err.Error())
	}
	st := in.Status
	if st == domain.GeoStatusUnspecified {
		st = domain.GeoStatusDown // fail-safe: fresh zone поднимается DOWN
	}
	z := domain.Zone{ID: in.ID, RegionID: in.RegionID, Status: st, Infra: in.Infra}

	// Строка операции — ДО INSERT'а. Warnings° вычисляются по СОЗДАННОЙ строке и на
	// этот момент неизвестны, поэтому metadata здесь минимальна и уточняется
	// терминальным переходом (syncop.Commit → MarkDoneWithMetadata).
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Create zone %s", in.ID),
		&geov1.CreateZoneMetadata{ZoneId: in.ID})
	if err != nil {
		return nil, err
	}
	if err := syncop.Begin(ctx, u.ops, op); err != nil {
		return nil, err
	}

	created, derr := u.writer.Insert(ctx, &z)
	if derr != nil {
		// [PHASE-0-GATED] within-service create absent-parent остаётся текущим
		// FK-FAILED_PRECONDITION (23503). By-lane split в NOT_FOUND "Region <id>
		// not found" + reason-token приземляется ТОЛЬКО после Phase-0 governance
		// change-set (§Definition of Done merge-gate) — НЕ вводим pre-flight resolve.
		return syncop.Fail(ctx, u.ops, op, u.errStatus(derr))
	}
	resp, err := marshalZone(created)
	if err != nil {
		return syncop.Fail(ctx, u.ops, op, u.errStatus(err))
	}
	meta, err := anypb.New(&geov1.CreateZoneMetadata{ZoneId: in.ID, Warnings: closedWarnings(created)})
	if err != nil {
		return syncop.Fail(ctx, u.ops, op, u.errStatus(err))
	}
	return syncop.Commit(ctx, u.ops, op, meta, resp)
}

// Update — admin partial-смена зоны (status/infra-subset). Immutable-поля
// (id, regionId, infra.numericInfraId) в update_mask → синхронный InvalidArgument
// ДО UpdateMask. not-found → op.error.
func (u *UseCase) Update(ctx context.Context, in UpdateInput) (*operations.Operation, error) {
	if err := domain.ValidateID("zone", in.ID); err != nil {
		return nil, invalidArg(err.Error())
	}
	for _, f := range in.Mask {
		switch f {
		case "id":
			return nil, invalidArg("id is immutable after Zone.Create")
		case "regionId", "region_id":
			return nil, invalidArg("regionId is immutable after Zone.Create")
		case "infra.numericInfraId", "infra.numeric_infra_id", "numericInfraId":
			return nil, invalidArg("numericInfraId is immutable after Zone.Create")
		}
	}
	if err := validate.UpdateMask("update_mask", in.Mask, zoneUpdatable); err != nil {
		return nil, err
	}
	p, err := u.buildUpdateParams(in)
	if err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Update zone %s", in.ID),
		&geov1.UpdateZoneMetadata{ZoneId: in.ID})
	if err != nil {
		return nil, err
	}
	if err := syncop.Begin(ctx, u.ops, op); err != nil {
		return nil, err
	}

	updated, derr := u.writer.Update(ctx, in.ID, p)
	if derr != nil {
		return syncop.Fail(ctx, u.ops, op, u.errStatus(derr))
	}
	resp, err := marshalZone(updated)
	if err != nil {
		return syncop.Fail(ctx, u.ops, op, u.errStatus(err))
	}
	return syncop.Commit(ctx, u.ops, op, nil, resp)
}

// buildUpdateParams транслирует UpdateInput+mask в UpdateParams по ОДНОМУ правилу,
// одинаковому для всех полей:
//
//   - маска НАЗЫВАЕТ поле → поле применяется ДОСЛОВНО, включая значение-очистку, и
//     валидируется теми же правилами, что на Create (api-conventions.md). Назвать
//     поле в маске — единственный способ его очистить;
//   - маска ПУСТА → применяется только то, что вызывающий действительно принёс.
//
// Вторая половина — не послабление, а единственное прочтение, при котором обе ветви
// не противоречат друг другу. proto3 не отличает неприсланный скаляр от нуля, поэтому
// «пустая маска = обнулить всё, чего в теле нет» стирала у зоны якорь подложки,
// подсказку ёмкости и число доменов отказа при обычном переименовании — а имя от
// ровно того же затирания было защищено. Защита имени была права, безусловное
// применение остальных — нет: домен прямо документирует, что пустое значение на
// Update означает «не менять поле». Теперь по этому правилу живут ВСЕ поля, а
// очистка любого из них делается явно — через маску.
//
// Поле, названное маской, но пришедшее пустым там, где ресурс пустоту хранить не
// может (status — CHECK IN ('UP','DOWN')), отвергается СИНХРОННО тем же текстом,
// что на Create. Молча принять и выбросить
// такое поле нельзя (api-conventions.md §«принято-и-проигнорировано»): вызывающий
// получил бы успех и уверенность, что очистил поле, которого сервис не трогал.
func (u *UseCase) buildUpdateParams(in UpdateInput) (UpdateParams, error) {
	var p UpdateParams
	named := func(field string) bool { return len(in.Mask) > 0 && maskHas(in.Mask, field) }
	// apply: маска называет поле ⟹ применяем дословно; маска пуста ⟹ применяем,
	// только если вызывающий поле принёс (carried).
	apply := func(field string, carried bool) bool {
		if len(in.Mask) == 0 {
			return carried
		}
		return named(field)
	}
	if apply("status", in.Status != domain.GeoStatusUnspecified) {
		if err := in.Status.Validate(); err != nil {
			return p, invalidArg(err.Error())
		}
		if in.Status == domain.GeoStatusUnspecified {
			return p, invalidArg("zone status is required")
		}
		st := in.Status
		p.Status = &st
	}
	if apply("infra.hostClasses", len(in.Infra.HostClasses) > 0) {
		hc := in.Infra.HostClasses
		p.HostClasses = &hc
	}
	if apply("infra.failureDomainCount", in.Infra.FailureDomainCount != 0) {
		fdc := in.Infra.FailureDomainCount
		p.FailureDomainCount = &fdc
	}
	if apply("infra.underlayAnchor", in.Infra.UnderlayAnchor != "") {
		ua := in.Infra.UnderlayAnchor
		p.UnderlayAnchor = &ua
	}
	if apply("infra.capacityHint", in.Infra.CapacityHint != "") {
		ch := in.Infra.CapacityHint
		p.CapacityHint = &ch
	}
	return p, nil
}

// Delete — admin-удаление зоны, возвращает синхронно-завершённый Operation.
func (u *UseCase) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := domain.ValidateID("zone", id); err != nil {
		return nil, invalidArg(err.Error())
	}
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Delete zone %s", id),
		&geov1.DeleteZoneMetadata{ZoneId: id})
	if err != nil {
		return nil, err
	}
	if err := syncop.Begin(ctx, u.ops, op); err != nil {
		return nil, err
	}

	if derr := u.writer.Delete(ctx, id); derr != nil {
		return syncop.Fail(ctx, u.ops, op, u.errStatus(derr))
	}
	empty, err := anypb.New(&emptypb.Empty{})
	if err != nil {
		return syncop.Fail(ctx, u.ops, op, u.errStatus(err))
	}
	return syncop.Commit(ctx, u.ops, op, nil, empty)
}

// closedWarnings — громкий no-op: зона создана CLOSED (own status != UP) →
// warnings° (module-geo rule 16), в CreateZoneMetadata (geo-owned, НЕ shared
// Operation, НЕ public response).
func closedWarnings(z *domain.Zone) []string {
	if z.Status == domain.GeoStatusUp {
		return nil
	}
	return []string{fmt.Sprintf(
		"zone %s created but CLOSED to placement (status DOWN); no tenant can place here — Internal Update status=UP to open",
		z.ID)}
}

// marshalZone упаковывает public-проекцию в Operation.response (единый protoconv.Zone).
func marshalZone(z *domain.Zone) (*anypb.Any, error) {
	return anypb.New(protoconv.Zone(z))
}

func invalidArg(msg string) error {
	return fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, msg)
}

// maskHas — содержит ли update_mask поле (camelCase путь).
func maskHas(mask []string, field string) bool {
	for _, f := range mask {
		// Сравнение по форме имени контракта, а не дословно: форму выбирает край,
		// и на пути ЗАПИСИ дословное сравнение отвечает «нет» на верном входе —
		// тихо, потому что запрос при этом успешен. Разбор — у validate.FieldNameEq.
		if validate.FieldNameEq(f, field) {
			return true
		}
	}
	return false
}
