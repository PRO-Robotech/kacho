// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package region — use-case (бизнес-логика) каталога Region.
//
// Use-case слой чистой архитектуры: импортирует domain + порт Repo + corelib
// operations, не тянет pgx/transport. Публичные RegionService.Get/List —
// read-only (sync), возвращают LEAN public-проекцию. Admin CRUD идёт через
// InternalRegionService на :9091 и возвращает синхронно-завершённый
// Operation{done:true} (config-INSERT, без саги — module-geo rule 4): мутация
// пишет строку, финализирует операцию done=true и отдаёт её сразу
// (response=public Region либо Empty для Delete, либо error). GetInternal
// возвращает FULL Internal-проекцию (status + infra°) синхронно.
package region

import (
	"context"
	"errors"
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

// Pagination — вход для List (page_size + opaque page_token + openForPlacement фильтр).
type Pagination struct {
	PageSize         int64
	PageToken        string
	OpenForPlacement bool
}

// UpdateParams — опциональные mutable-поля partial-Update региона. nil → поле не
// меняется (repo COALESCE, single-statement, без TOCTOU).
type UpdateParams struct {
	Status      *domain.GeoStatus
	CountryCode *string
}

// CreateInput — вход InternalRegionService.Create (transport-neutral).
type CreateInput struct {
	ID          string
	CountryCode string
	Status      domain.GeoStatus
	Infra       domain.RegionInfra
}

// UpdateInput — вход InternalRegionService.Update (transport-neutral).
type UpdateInput struct {
	ID          string
	Mask        []string
	CountryCode string
	Status      domain.GeoStatus
}

// Reader — read-порт (Get/GetInternal/List). CQRS-разделён с Writer.
type Reader interface {
	Get(ctx context.Context, id string) (*domain.Region, error)
	GetInternal(ctx context.Context, id string) (*domain.Region, error)
	List(ctx context.Context, p Pagination) ([]*domain.Region, string, error)
}

// Writer — write-порт admin-мутаций (+outbox-emit в writer-tx).
type Writer interface {
	Insert(ctx context.Context, r *domain.Region) (*domain.Region, error)
	Update(ctx context.Context, id string, p UpdateParams) (*domain.Region, error)
	Delete(ctx context.Context, id string) error
}

// Repo — композит Reader+Writer.
type Repo interface {
	Reader
	Writer
}

// ErrToStatus маппит sentinel repo-ошибку в transport-status, сохраняемый в
// Operation.error. Инжектится composition root'ом (serviceerr.ToStatus).
type ErrToStatus func(error) error

// regionUpdatable — known-set update_mask (mutable-поля). Immutable (id,
// infra.numericInfraId) в набор НЕ входят — отвергаются отдельным immutable-switch
// ДО UpdateMask (конвенционный текст вместо generic "unknown field").
// `name` в набор НЕ входит и не может: поля у ресурса больше нет (#716).
// Маска, назвавшая его, получает generic «unknown field» — тот же исход, что у
// любого другого несуществующего поля, и это верно: переименовывать нечего.
// ОБЕ формы имени многословного поля — намеренно, как у registry (единственного,
// кто это уже держал). Край разбирает `updateMask` через protojson, а тот приводит
// lowerCamelCase к именам полей контракта, то есть в сервис приходит `country_code`.
// Набор из одной camelCase-формы означал бы поле, которое объявлено изменяемым и не
// изменяется НИ ПРИ КАКОМ входе через край. Односложные поля (`status`) совпадают в
// обеих формах, поэтому класс не вскрывался, пока не появилось первое многословное.
var regionUpdatable = map[string]struct{}{
	"status":       {},
	"countryCode":  {},
	"country_code": {},
}

// UseCase — бизнес-логика Region поверх Reader/Writer, LRO-стека и errStatus.
type UseCase struct {
	reader    Reader
	writer    Writer
	ops       syncop.Repo
	errStatus ErrToStatus
}

// New собирает UseCase для Region.
func New(reader Reader, writer Writer, ops syncop.Repo, errStatus ErrToStatus) *UseCase {
	if errStatus == nil {
		errStatus = func(err error) error { return err }
	}
	return &UseCase{reader: reader, writer: writer, ops: ops, errStatus: errStatus}
}

// Get возвращает LEAN public-проекцию региона по id.
func (u *UseCase) Get(ctx context.Context, id string) (*domain.Region, error) {
	if err := domain.ValidateID("region", id); err != nil {
		return nil, invalidArg(err.Error())
	}
	return u.reader.Get(ctx, id)
}

// GetInternal возвращает FULL Internal-проекцию (status + infra°). :9091-only.
func (u *UseCase) GetInternal(ctx context.Context, id string) (*domain.Region, error) {
	if err := domain.ValidateID("region", id); err != nil {
		return nil, invalidArg(err.Error())
	}
	return u.reader.GetInternal(ctx, id)
}

// List возвращает регионы (cursor-пагинация; garbage page_size → InvalidArgument).
func (u *UseCase) List(ctx context.Context, p Pagination) ([]*domain.Region, string, error) {
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	p.PageSize = size
	return u.reader.List(ctx, p)
}

// Create — admin-создание региона, возвращает синхронно-завершённый Operation.
// Малформ id / невалидный countryCode отвергаются СИНХРОННО (InvalidArgument,
// операция не пишется). Fresh-default fail-safe: омитнутый status → DOWN
// (module-geo rule 16). DB-ошибка (дубль id) → op.error.
func (u *UseCase) Create(ctx context.Context, in CreateInput) (*operations.Operation, error) {
	if err := domain.ValidateID("region", in.ID); err != nil {
		return nil, invalidArg(err.Error())
	}
	// Проверка имени стояла ЗДЕСЬ и снята вместе с полем (#716). Форму
	// идентификатора судит `domain.ValidateID` выше — она и есть единственная
	// проверка идентичности региона: назначает её администратор, и она
	// человекочитаема by construction.
	if err := domain.ValidateCountryCode(in.CountryCode); err != nil {
		return nil, invalidArg(err.Error())
	}
	if err := in.Status.Validate(); err != nil {
		return nil, invalidArg(err.Error())
	}
	st := in.Status
	if st == domain.GeoStatusUnspecified {
		st = domain.GeoStatusDown // fail-safe: fresh region поднимается DOWN, admin явно открывает
	}
	r := domain.Region{ID: in.ID, CountryCode: in.CountryCode, Status: st, Infra: in.Infra}

	// Строка операции — ДО INSERT'а. Warnings° вычисляются по СОЗДАННОЙ строке и на
	// этот момент неизвестны, поэтому metadata здесь минимальна и уточняется
	// терминальным переходом (syncop.Commit → MarkDoneWithMetadata).
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Create region %s", in.ID),
		&geov1.CreateRegionMetadata{RegionId: in.ID})
	if err != nil {
		return nil, err
	}
	if err := syncop.Begin(ctx, u.ops, op); err != nil {
		return nil, err
	}

	created, derr := u.writer.Insert(ctx, &r)
	if derr != nil {
		return syncop.Fail(ctx, u.ops, op, u.errStatus(derr))
	}
	resp, err := marshalRegion(created)
	if err != nil {
		return syncop.Fail(ctx, u.ops, op, u.errStatus(err))
	}
	meta, err := anypb.New(&geov1.CreateRegionMetadata{RegionId: in.ID, Warnings: closedWarnings(created)})
	if err != nil {
		return syncop.Fail(ctx, u.ops, op, u.errStatus(err))
	}
	return syncop.Commit(ctx, u.ops, op, meta, resp)
}

// Update — admin partial-смена региона (status/countryCode). Immutable-поля
// (id, infra.numericInfraId) в update_mask → синхронный InvalidArgument ДО
// UpdateMask. not-found → op.error.
func (u *UseCase) Update(ctx context.Context, in UpdateInput) (*operations.Operation, error) {
	if err := domain.ValidateID("region", in.ID); err != nil {
		return nil, invalidArg(err.Error())
	}
	// Immutable-switch ДО UpdateMask: known-set НЕ содержит immutable-полей, иначе
	// UpdateMask отверг бы их generic "unknown field" вместо конвенционного текста.
	for _, f := range in.Mask {
		switch f {
		case "id":
			return nil, invalidArg("id is immutable after Region.Create")
		case "infra.numericInfraId", "infra.numeric_infra_id", "numericInfraId":
			return nil, invalidArg("numericInfraId is immutable after Region.Create")
		}
	}
	if err := validate.UpdateMask("update_mask", in.Mask, regionUpdatable); err != nil {
		return nil, err
	}
	p, err := u.buildUpdateParams(in)
	if err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Update region %s", in.ID),
		&geov1.UpdateRegionMetadata{RegionId: in.ID})
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
	resp, err := marshalRegion(updated)
	if err != nil {
		return syncop.Fail(ctx, u.ops, op, u.errStatus(err))
	}
	return syncop.Commit(ctx, u.ops, op, nil, resp)
}

// buildUpdateParams транслирует UpdateInput+mask в UpdateParams по тому же одному
// правилу, что и Zone (единая дисциплина маски для всех ресурсов сервиса):
//
//   - маска НАЗЫВАЕТ поле → применяется ДОСЛОВНО, включая значение-очистку, и
//     валидируется как на Create. Назвать поле в маске — единственный способ его
//     очистить (для региона это `countryCode`);
//   - маска ПУСТА → применяется только то, что вызывающий действительно принёс,
//     потому что proto3 не отличает неприсланный скаляр от нуля и «обнулить всё,
//     чего в теле нет» стирало у региона код страны при обычном переименовании.
//
// Поле, названное маской, но пустое там, где ресурс пустоту хранить не может
// (status — CHECK IN ('UP','DOWN')), отвергается СИНХРОННО тем же текстом, что на
// Create: молчаливое «принял и выбросил» запрещено (api-conventions.md).
func (u *UseCase) buildUpdateParams(in UpdateInput) (UpdateParams, error) {
	var p UpdateParams
	named := func(field string) bool { return len(in.Mask) > 0 && maskHas(in.Mask, field) }
	apply := func(field string, carried bool) bool {
		if len(in.Mask) == 0 {
			return carried
		}
		return named(field)
	}
	if apply("countryCode", in.CountryCode != "") {
		if err := domain.ValidateCountryCode(in.CountryCode); err != nil {
			return p, invalidArg(err.Error())
		}
		cc := in.CountryCode
		p.CountryCode = &cc
	}
	if apply("status", in.Status != domain.GeoStatusUnspecified) {
		if err := in.Status.Validate(); err != nil {
			return p, invalidArg(err.Error())
		}
		if in.Status == domain.GeoStatusUnspecified {
			return p, invalidArg("region status is required")
		}
		st := in.Status
		p.Status = &st
	}
	return p, nil
}

// Delete — admin-удаление региона, возвращает синхронно-завершённый Operation.
// FK RESTRICT (есть зоны) → op.error FailedPrecondition "region <id> is not empty".
func (u *UseCase) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := domain.ValidateID("region", id); err != nil {
		return nil, invalidArg(err.Error())
	}
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Delete region %s", id),
		&geov1.DeleteRegionMetadata{RegionId: id})
	if err != nil {
		return nil, err
	}
	if err := syncop.Begin(ctx, u.ops, op); err != nil {
		return nil, err
	}

	derr := u.writer.Delete(ctx, id)
	if derr != nil {
		// FK RESTRICT (есть зоны) — конвенционный "region <id> is not empty"
		// (module-geo rule 13; DB-backstop, не software-precheck). Прочие ошибки —
		// как есть. Держим доменные sentinel'ы, errStatus конвертит в gRPC.
		if errors.Is(derr, geoerrors.ErrFailedPrecondition) {
			derr = failedPrecondition(fmt.Sprintf("region %s is not empty", id))
		}
		return syncop.Fail(ctx, u.ops, op, u.errStatus(derr))
	}
	empty, err := anypb.New(&emptypb.Empty{})
	if err != nil {
		return syncop.Fail(ctx, u.ops, op, u.errStatus(err))
	}
	return syncop.Commit(ctx, u.ops, op, nil, empty)
}

// closedWarnings — громкий no-op: если регион создан CLOSED (own status != UP),
// warnings° несёт запись (module-geo rule 16). Живёт в CreateRegionMetadata
// (geo-owned, НЕ shared Operation, НЕ public response).
func closedWarnings(r *domain.Region) []string {
	if r.Status == domain.GeoStatusUp {
		return nil
	}
	return []string{fmt.Sprintf(
		"region %s created but CLOSED to placement (status DOWN); no tenant can place here — Internal Update status=UP to open",
		r.ID)}
}

// marshalRegion упаковывает public-проекцию в Operation.response (единый
// protoconv.Region — без дрейфа с handler).
func marshalRegion(r *domain.Region) (*anypb.Any, error) {
	return anypb.New(protoconv.Region(r))
}

func invalidArg(msg string) error {
	return fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, msg)
}

func failedPrecondition(msg string) error {
	return fmt.Errorf("%w: %s", geoerrors.ErrFailedPrecondition, msg)
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
