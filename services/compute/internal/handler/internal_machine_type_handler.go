// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — internal_machine_type_handler.go: admin-CRUD над каталогом
// MachineType (kacho-only RPC, COMP-1 F7). Регистрируется ТОЛЬКО на internal
// listener (:9091), проброшен через api-gateway internal mux на самоописываемый
// путь /compute/v1/internal/machineTypes. На external TLS endpoint НЕ доступен
// (Internal-vs-external, security.md). Мутации async: возвращают operation.Operation (done=false), клиент
// поллит OperationService.Get(id).
package handler

import (
	"context"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	svc "github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

// InternalMachineTypeHandler реализует computev1.InternalMachineTypeServiceServer.
type InternalMachineTypeHandler struct {
	computev1.UnimplementedInternalMachineTypeServiceServer
	svc *svc.MachineTypeService
}

// NewInternalMachineTypeHandler создаёт InternalMachineTypeHandler.
func NewInternalMachineTypeHandler(s *svc.MachineTypeService) *InternalMachineTypeHandler {
	return &InternalMachineTypeHandler{svc: s}
}

// Create инициирует создание MachineType (admin-only, async Operation).
func (h *InternalMachineTypeHandler) Create(ctx context.Context, req *computev1.CreateMachineTypeRequest) (*operationpb.Operation, error) {
	op, err := h.svc.Create(ctx, svc.CreateMachineTypeReq{
		Name:               req.Name,
		Description:        req.Description,
		Family:             domain.MachineTypeFamily(req.Family),
		EffectiveResources: effectiveResourcesFromProto(req.EffectiveResources),
		AvailableZones:     req.AvailableZones,
		Status:             domain.MachineTypeStatus(req.Status),
		Labels:             req.Labels,
	})
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// Update инициирует обновление MachineType (admin-only, async Operation).
func (h *InternalMachineTypeHandler) Update(ctx context.Context, req *computev1.UpdateMachineTypeRequest) (*operationpb.Operation, error) {
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	op, err := h.svc.Update(ctx, svc.UpdateMachineTypeReq{
		ID:                 req.MachineTypeId,
		Description:        req.Description,
		Family:             domain.MachineTypeFamily(req.Family),
		EffectiveResources: effectiveResourcesFromProto(req.EffectiveResources),
		AvailableZones:     req.AvailableZones,
		Status:             domain.MachineTypeStatus(req.Status),
		Labels:             req.Labels,
		UpdateMask:         mask,
	})
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// Delete инициирует удаление MachineType (admin-only, async Operation).
func (h *InternalMachineTypeHandler) Delete(ctx context.Context, req *computev1.DeleteMachineTypeRequest) (*operationpb.Operation, error) {
	op, err := h.svc.Delete(ctx, req.MachineTypeId)
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// effectiveResourcesFromProto конвертит computev1.EffectiveResources → domain
// (nil-safe: пустой блок → нулевой domain-объект, отвергается sync-валидацией).
func effectiveResourcesFromProto(r *computev1.EffectiveResources) domain.EffectiveResources {
	if r == nil {
		return domain.EffectiveResources{}
	}
	return domain.EffectiveResources{
		VCPU:      r.VCpu,
		MemoryMiB: r.MemoryMib,
		GPUs:      r.Gpus,
		GPUType:   r.GpuType,
	}
}
