// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — тонкий gRPC-transport для kacho-geo (parse → use-case →
// format, без бизнес-логики). Здесь живут публичные хендлеры RegionService /
// ZoneService: чтение справочника (Get/List) и административные глаголы каталога
// (Create/Update/Delete, ADM-1 — право system_admin @ cluster исполняет край и
// интерсептор службы по аннотациям контракта). Публичный вход блока infra° не
// несёт; полная плоскость администрирования (infra°, GetInternal) — в
// internal.go (InternalRegionService / InternalZoneService, регистрируется только
// на листенере :9091 — cluster-internal).
package handler

import (
	"context"

	operationpb "github.com/PRO-Robotech/corelib/api/corelib/operation"
	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/protoconv"
)

// RegionHandler реализует geov1.RegionServiceServer (публичный).
type RegionHandler struct {
	geov1.UnimplementedRegionServiceServer
	uc *region.UseCase
}

// NewRegionHandler конструирует RegionHandler.
func NewRegionHandler(uc *region.UseCase) *RegionHandler { return &RegionHandler{uc: uc} }

// Get возвращает Region по id.
func (h *RegionHandler) Get(ctx context.Context, req *geov1.GetRegionRequest) (*geov1.Region, error) {
	r, err := h.uc.Get(ctx, req.GetRegionId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.Region(r), nil
}

// List возвращает регионы (cursor-пагинация).
func (h *RegionHandler) List(ctx context.Context, req *geov1.ListRegionsRequest) (*geov1.ListRegionsResponse, error) {
	regions, next, err := h.uc.List(ctx, region.Pagination{
		PageSize:         req.GetPageSize(),
		PageToken:        req.GetPageToken(),
		OpenForPlacement: req.GetOpenForPlacement(),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	resp := &geov1.ListRegionsResponse{NextPageToken: next}
	for _, r := range regions {
		resp.Regions = append(resp.Regions, protoconv.Region(r))
	}
	return resp, nil
}

// Create синхронно создаёт регион без infra° (numericInfraId = 0 — «не назначен»)
// и возвращает Operation{done:true}.
func (h *RegionHandler) Create(ctx context.Context, req *geov1.CreatePublicRegionRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Create(ctx, region.CreateInput{
		ID:          req.GetId(),
		CountryCode: req.GetCountryCode(),
		Status:      domain.GeoStatus(req.GetStatus()),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Update синхронно меняет регион (status/countryCode) и возвращает
// Operation{done:true}. Изменяемых подполей infra° у региона нет, поэтому known-set
// маски у публичной и внутренней правки региона один и тот же.
func (h *RegionHandler) Update(ctx context.Context, req *geov1.UpdatePublicRegionRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Update(ctx, region.UpdateInput{
		ID:          req.GetRegionId(),
		Mask:        req.GetUpdateMask().GetPaths(),
		CountryCode: req.GetCountryCode(),
		Status:      domain.GeoStatus(req.GetStatus()),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Delete синхронно удаляет регион. FK RESTRICT (есть зоны) → Operation.error.
func (h *RegionHandler) Delete(ctx context.Context, req *geov1.DeleteRegionRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Delete(ctx, req.GetRegionId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// ZoneHandler реализует geov1.ZoneServiceServer (публичный).
type ZoneHandler struct {
	geov1.UnimplementedZoneServiceServer
	uc *zone.UseCase
}

// NewZoneHandler конструирует ZoneHandler.
func NewZoneHandler(uc *zone.UseCase) *ZoneHandler { return &ZoneHandler{uc: uc} }

// Get возвращает Zone по id.
func (h *ZoneHandler) Get(ctx context.Context, req *geov1.GetZoneRequest) (*geov1.Zone, error) {
	z, err := h.uc.Get(ctx, req.GetZoneId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.Zone(z), nil
}

// List возвращает зоны (cursor-пагинация).
func (h *ZoneHandler) List(ctx context.Context, req *geov1.ListZonesRequest) (*geov1.ListZonesResponse, error) {
	zones, next, err := h.uc.List(ctx, zone.Pagination{
		PageSize:         req.GetPageSize(),
		PageToken:        req.GetPageToken(),
		RegionID:         req.GetRegionId(),
		OpenForPlacement: req.GetOpenForPlacement(),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	resp := &geov1.ListZonesResponse{NextPageToken: next}
	for _, z := range zones {
		resp.Zones = append(resp.Zones, protoconv.Zone(z))
	}
	return resp, nil
}

// Create синхронно создаёт зону без infra° и возвращает Operation{done:true}.
func (h *ZoneHandler) Create(ctx context.Context, req *geov1.CreatePublicZoneRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Create(ctx, zone.CreateInput{
		ID:       req.GetId(),
		RegionID: req.GetRegionId(),
		Status:   domain.GeoStatus(req.GetStatus()),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Update синхронно меняет статус зоны и возвращает Operation{done:true}. Маска
// публичной поверхности до infra° не достаёт (zone.UseCase.UpdatePublic).
func (h *ZoneHandler) Update(ctx context.Context, req *geov1.UpdatePublicZoneRequest) (*operationpb.Operation, error) {
	op, err := h.uc.UpdatePublic(ctx, zone.PublicUpdateInput{
		ID:     req.GetZoneId(),
		Mask:   req.GetUpdateMask().GetPaths(),
		Status: domain.GeoStatus(req.GetStatus()),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Delete синхронно удаляет зону.
func (h *ZoneHandler) Delete(ctx context.Context, req *geov1.DeleteZoneRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Delete(ctx, req.GetZoneId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}
