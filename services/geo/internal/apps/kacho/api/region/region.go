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
	Name        *string
	Status      *domain.GeoStatus
	CountryCode *string
}

// CreateInput — вход InternalRegionService.Create (transport-neutral).
type CreateInput struct {
	ID          string
	Name        string
	CountryCode string
	Status      domain.GeoStatus
	Infra       domain.RegionInfra
}

// UpdateInput — вход InternalRegionService.Update (transport-neutral).
type UpdateInput struct {
	ID          string
	Mask        []string
	Name        string
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
var regionUpdatable = map[string]struct{}{
	"name":        {},
	"status":      {},
	"countryCode": {},
}

// UseCase — бизнес-логика Region поверх Reader/Writer, LRO-стека и errStatus.
type UseCase struct {
	reader    Reader
	writer    Writer
	ops       operations.Repo
	errStatus ErrToStatus
}

// New собирает UseCase для Region.
func New(reader Reader, writer Writer, ops operations.Repo, errStatus ErrToStatus) *UseCase {
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
// Малформ id / пустой name / невалидный countryCode отвергаются СИНХРОННО
// (InvalidArgument, операция не пишется). Fresh-default fail-safe: омитнутый
// status → DOWN (module-geo rule 16). DB-ошибки (дубль id/name) → op.error.
func (u *UseCase) Create(ctx context.Context, in CreateInput) (*operations.Operation, error) {
	if err := domain.ValidateID("region", in.ID); err != nil {
		return nil, invalidArg(err.Error())
	}
	if in.Name == "" {
		return nil, invalidArg("region name is required")
	}
	if err := domain.ValidateName("region name", in.Name); err != nil {
		return nil, invalidArg(err.Error())
	}
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
	r := domain.Region{ID: in.ID, Name: in.Name, CountryCode: in.CountryCode, Status: st, Infra: in.Infra}

	created, derr := u.writer.Insert(ctx, &r)
	if derr != nil {
		return u.fail(ctx, in.ID, u.errStatus(derr))
	}
	resp, err := marshalRegion(created)
	if err != nil {
		return nil, err
	}
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Create region %s", in.ID),
		&geov1.CreateRegionMetadata{RegionId: in.ID, Warnings: closedWarnings(created)})
	if err != nil {
		return nil, err
	}
	return syncop.Commit(ctx, u.ops, op, resp)
}

// Update — admin partial-смена региона (name/status/countryCode). Immutable-поля
// (id, infra.numericInfraId) в update_mask → синхронный InvalidArgument ДО
// UpdateMask. not-found/дубль-name → op.error.
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

	updated, derr := u.writer.Update(ctx, in.ID, p)
	if derr != nil {
		return u.failUpdate(ctx, in.ID, u.errStatus(derr))
	}
	resp, err := marshalRegion(updated)
	if err != nil {
		return nil, err
	}
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Update region %s", in.ID),
		&geov1.UpdateRegionMetadata{RegionId: in.ID})
	if err != nil {
		return nil, err
	}
	return syncop.Commit(ctx, u.ops, op, resp)
}

// buildUpdateParams транслирует UpdateInput+mask в UpdateParams. mask непустой →
// применяются только перечисленные поля; mask пустой → full-object PATCH (все
// переданные mutable-поля). Каждое применяемое поле валидируется как на Create.
func (u *UseCase) buildUpdateParams(in UpdateInput) (UpdateParams, error) {
	var p UpdateParams
	apply := func(field string) bool { return len(in.Mask) == 0 || maskHas(in.Mask, field) }
	if apply("name") && in.Name != "" {
		if err := domain.ValidateName("region name", in.Name); err != nil {
			return p, invalidArg(err.Error())
		}
		name := in.Name
		p.Name = &name
	}
	if apply("countryCode") {
		if err := domain.ValidateCountryCode(in.CountryCode); err != nil {
			return p, invalidArg(err.Error())
		}
		cc := in.CountryCode
		p.CountryCode = &cc
	}
	if apply("status") && in.Status != domain.GeoStatusUnspecified {
		if err := in.Status.Validate(); err != nil {
			return p, invalidArg(err.Error())
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
	derr := u.writer.Delete(ctx, id)
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Delete region %s", id),
		&geov1.DeleteRegionMetadata{RegionId: id})
	if err != nil {
		return nil, err
	}
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
		return nil, err
	}
	return syncop.Commit(ctx, u.ops, op, empty)
}

func (u *UseCase) fail(ctx context.Context, id string, statusErr error) (*operations.Operation, error) {
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Create region %s", id),
		&geov1.CreateRegionMetadata{RegionId: id})
	if err != nil {
		return nil, err
	}
	return syncop.Fail(ctx, u.ops, op, statusErr)
}

func (u *UseCase) failUpdate(ctx context.Context, id string, statusErr error) (*operations.Operation, error) {
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Update region %s", id),
		&geov1.UpdateRegionMetadata{RegionId: id})
	if err != nil {
		return nil, err
	}
	return syncop.Fail(ctx, u.ops, op, statusErr)
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
		if f == field {
			return true
		}
	}
	return false
}
