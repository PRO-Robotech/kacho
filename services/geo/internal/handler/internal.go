// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — internal.go: admin-CRUD над каталогом Region/Zone
// (InternalRegionService / InternalZoneService) + Internal-проекция (GetInternal).
// Регистрируется ТОЛЬКО на cluster-internal листенере (:9091), проброшен через
// internal mux api-gateway на /geo/v1/internal/… — НИКОГДА на внешнем TLS
// endpoint (ban #6, security.md §Internal-vs-external).
//
// Admin-мутации возвращают синхронно-завершённый Operation (done=true сразу,
// config-INSERT без саги — module-geo rule 4); клиент разворачивает .response.
// Handler — тонкий transport: parse → use-case → format, без бизнес-логики.
package handler

import (
	"context"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/protoconv"
)

// InternalRegionHandler реализует geov1.InternalRegionServiceServer (admin CRUD +
// GetInternal).
type InternalRegionHandler struct {
	geov1.UnimplementedInternalRegionServiceServer
	uc *region.UseCase
}

// NewInternalRegionHandler конструирует InternalRegionHandler.
func NewInternalRegionHandler(uc *region.UseCase) *InternalRegionHandler {
	return &InternalRegionHandler{uc: uc}
}

// Create синхронно создаёт регион и возвращает Operation{done:true}.
func (h *InternalRegionHandler) Create(ctx context.Context, req *geov1.CreateRegionRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Create(ctx, region.CreateInput{
		ID:          req.GetId(),
		Name:        req.GetName(),
		CountryCode: req.GetCountryCode(),
		Status:      domain.GeoStatus(req.GetStatus()),
		Infra:       domain.RegionInfra{NumericInfraID: req.GetInfra().GetNumericInfraId()},
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Update синхронно меняет регион и возвращает Operation{done:true}.
func (h *InternalRegionHandler) Update(ctx context.Context, req *geov1.UpdateRegionRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Update(ctx, region.UpdateInput{
		ID:          req.GetRegionId(),
		Mask:        req.GetUpdateMask().GetPaths(),
		Name:        req.GetName(),
		CountryCode: req.GetCountryCode(),
		Status:      domain.GeoStatus(req.GetStatus()),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Delete синхронно удаляет регион. FK RESTRICT (есть зоны) → Operation.error.
func (h *InternalRegionHandler) Delete(ctx context.Context, req *geov1.DeleteRegionRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Delete(ctx, req.GetRegionId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// GetInternal возвращает FULL Internal-проекцию региона (status + infra°).
func (h *InternalRegionHandler) GetInternal(ctx context.Context, req *geov1.GetInternalRegionRequest) (*geov1.InternalRegion, error) {
	r, err := h.uc.GetInternal(ctx, req.GetRegionId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.InternalRegion(r), nil
}

// InternalZoneHandler реализует geov1.InternalZoneServiceServer (admin CRUD +
// GetInternal).
type InternalZoneHandler struct {
	geov1.UnimplementedInternalZoneServiceServer
	uc *zone.UseCase
}

// NewInternalZoneHandler конструирует InternalZoneHandler.
func NewInternalZoneHandler(uc *zone.UseCase) *InternalZoneHandler {
	return &InternalZoneHandler{uc: uc}
}

// Create синхронно создаёт зону и возвращает Operation{done:true}.
func (h *InternalZoneHandler) Create(ctx context.Context, req *geov1.CreateZoneRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Create(ctx, zone.CreateInput{
		ID:       req.GetId(),
		RegionID: req.GetRegionId(),
		Name:     req.GetName(),
		Status:   domain.GeoStatus(req.GetStatus()),
		Infra:    zoneInfraFromProto(req.GetInfra()),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Update синхронно меняет зону и возвращает Operation{done:true}.
func (h *InternalZoneHandler) Update(ctx context.Context, req *geov1.UpdateZoneRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Update(ctx, zone.UpdateInput{
		ID:     req.GetZoneId(),
		Mask:   req.GetUpdateMask().GetPaths(),
		Name:   req.GetName(),
		Status: domain.GeoStatus(req.GetStatus()),
		Infra:  zoneInfraFromProto(req.GetInfra()),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Delete синхронно удаляет зону.
func (h *InternalZoneHandler) Delete(ctx context.Context, req *geov1.DeleteZoneRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Delete(ctx, req.GetZoneId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// GetInternal возвращает FULL Internal-проекцию зоны (status + infra°).
func (h *InternalZoneHandler) GetInternal(ctx context.Context, req *geov1.GetInternalZoneRequest) (*geov1.InternalZone, error) {
	z, err := h.uc.GetInternal(ctx, req.GetZoneId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.InternalZone(z), nil
}

// zoneInfraFromProto маппит proto ZoneInfra → domain (nil-safe).
func zoneInfraFromProto(in *geov1.ZoneInfra) domain.ZoneInfra {
	return domain.ZoneInfra{
		NumericInfraID:     in.GetNumericInfraId(),
		HostClasses:        in.GetHostClasses(),
		FailureDomainCount: in.GetFailureDomainCount(),
		UnderlayAnchor:     in.GetUnderlayAnchor(),
		CapacityHint:       in.GetCapacityHint(),
	}
}
