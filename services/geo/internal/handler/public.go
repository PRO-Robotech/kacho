// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — тонкий gRPC-transport для kacho-geo (parse → use-case →
// format, без бизнес-логики). Здесь живут публичные read-only хендлеры
// (RegionService/ZoneService Get/List); admin-CRUD — в internal.go
// (InternalRegionService / InternalZoneService, регистрируется только на
// листенере :9091 — cluster-internal).
package handler

import (
	"context"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/protoconv"
)

// RegionHandler реализует geov1.RegionServiceServer (публичный, read-only).
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

// ZoneHandler реализует geov1.ZoneServiceServer (публичный, read-only).
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
