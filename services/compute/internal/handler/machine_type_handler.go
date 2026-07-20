// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"
	svc "github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

// MachineTypeHandler реализует computev1.MachineTypeServiceServer (COMP-1 F7) —
// тонкий transport-слой для public read каталога machine-type. Read ambient
// (cluster-scoped viewer, project-scope EXEMPT — авторизацию несёт api-gateway;
// хендлер НЕ вызывает AssertProjectOwnership, паритет с geo-каталогом).
type MachineTypeHandler struct {
	computev1.UnimplementedMachineTypeServiceServer
	svc *svc.MachineTypeService
}

// NewMachineTypeHandler создаёт MachineTypeHandler.
func NewMachineTypeHandler(s *svc.MachineTypeService) *MachineTypeHandler {
	return &MachineTypeHandler{svc: s}
}

// Get возвращает MachineType по id.
func (h *MachineTypeHandler) Get(ctx context.Context, req *computev1.GetMachineTypeRequest) (*computev1.MachineType, error) {
	mt, err := h.svc.Get(ctx, req.MachineTypeId)
	if err != nil {
		return nil, err
	}
	return protoconv.MachineType(mt), nil
}

// List возвращает каталог machine-type с whitelist-фильтрами name=/family=/minGpus=.
// Валидация формата (page_size/page_token) — в repo (до любого short-circuit).
func (h *MachineTypeHandler) List(ctx context.Context, req *computev1.ListMachineTypesRequest) (*computev1.ListMachineTypesResponse, error) {
	filter := svc.MachineTypeFilter{Name: req.Name, MinGPUs: req.MinGpus}
	if req.Family != "" {
		fam, ok := domain.ParseMachineTypeFamily(req.Family)
		if !ok {
			return nil, status.Errorf(codes.InvalidArgument, "unknown family filter %q (want STANDARD, COMPUTE, MEMORY or GPU)", req.Family)
		}
		filter.Family = fam
	}
	mts, next, err := h.svc.List(ctx, filter, svc.Pagination{PageToken: req.PageToken, PageSize: req.PageSize})
	if err != nil {
		return nil, err
	}
	resp := &computev1.ListMachineTypesResponse{NextPageToken: next}
	for _, mt := range mts {
		resp.MachineTypes = append(resp.MachineTypes, protoconv.MachineType(mt))
	}
	return resp, nil
}
