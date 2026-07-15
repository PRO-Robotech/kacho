// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — internal.go: admin-CRUD над каталогом Region/Zone
// (InternalRegionService / InternalZoneService). Регистрируется ТОЛЬКО на
// cluster-internal листенере (:9091), проброшен через internal mux api-gateway
// на /geo/v1/regions, /geo/v1/zones — НИКОГДА не на внешнем TLS endpoint
// (только cluster-internal).
//
// Admin-мутации async (стандартная LRO-форма Kachō): handler возвращает
// operation.Operation (done=false); use-case создает LRO-строку и запускает
// фоновый worker, клиент поллит OperationService.Get(id) до done. Малформ/пустой
// id отвергается синхронно (InvalidArgument) ещё до создания операции.
package handler

import (
	"context"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
)

// InternalRegionHandler реализует geov1.InternalRegionServiceServer (admin CRUD).
type InternalRegionHandler struct {
	geov1.UnimplementedInternalRegionServiceServer
	uc *region.UseCase
}

// NewInternalRegionHandler конструирует InternalRegionHandler.
func NewInternalRegionHandler(uc *region.UseCase) *InternalRegionHandler {
	return &InternalRegionHandler{uc: uc}
}

// Create запускает async-создание региона и возвращает Operation (done=false).
func (h *InternalRegionHandler) Create(ctx context.Context, req *geov1.CreateRegionRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Create(ctx, req.GetId(), req.GetName())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Update запускает async-смену имени региона и возвращает Operation.
func (h *InternalRegionHandler) Update(ctx context.Context, req *geov1.UpdateRegionRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Update(ctx, req.GetRegionId(), req.GetName())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Delete запускает async-удаление региона и возвращает Operation. FK RESTRICT
// (есть зоны) доезжает как Operation.error FailedPrecondition.
func (h *InternalRegionHandler) Delete(ctx context.Context, req *geov1.DeleteRegionRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Delete(ctx, req.GetRegionId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// InternalZoneHandler реализует geov1.InternalZoneServiceServer (admin CRUD).
type InternalZoneHandler struct {
	geov1.UnimplementedInternalZoneServiceServer
	uc *zone.UseCase
}

// NewInternalZoneHandler конструирует InternalZoneHandler.
func NewInternalZoneHandler(uc *zone.UseCase) *InternalZoneHandler {
	return &InternalZoneHandler{uc: uc}
}

// Create запускает async-создание зоны и возвращает Operation (done=false).
func (h *InternalZoneHandler) Create(ctx context.Context, req *geov1.CreateZoneRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Create(ctx, req.GetId(), req.GetRegionId(), req.GetName(), domain.ZoneStatus(req.GetStatus()))
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Update запускает async-смену зоны и возвращает Operation.
func (h *InternalZoneHandler) Update(ctx context.Context, req *geov1.UpdateZoneRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Update(ctx, req.GetZoneId(), req.GetRegionId(), req.GetName(), domain.ZoneStatus(req.GetStatus()))
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Delete запускает async-удаление зоны и возвращает Operation.
func (h *InternalZoneHandler) Delete(ctx context.Context, req *geov1.DeleteZoneRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Delete(ctx, req.GetZoneId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}
