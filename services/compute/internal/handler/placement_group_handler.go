// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/placementgroup"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"
)

// PlacementGroupHandler — транспортный слой групп размещения.
type PlacementGroupHandler struct {
	computev1.UnimplementedPlacementGroupServiceServer
	svc        *placementgroup.Service
	listFilter *listnarrow.Narrower
}

// NewPlacementGroupHandler создаёт обработчик.
//
// Сужатель может быть nil — тогда страница не сужается, и это допустимо ТОЛЬКО
// объявленным аварийным режимом: право проекта не отвечает на вопрос «можно ли
// этому вызывающему видеть ЭТИ строки».
func NewPlacementGroupHandler(s *placementgroup.Service, listFilter *listnarrow.Narrower) *PlacementGroupHandler {
	return &PlacementGroupHandler{svc: s, listFilter: listFilter}
}

// Get возвращает группу.
func (h *PlacementGroupHandler) Get(ctx context.Context, req *computev1.GetPlacementGroupRequest) (*computev1.PlacementGroup, error) {
	g, err := h.svc.Get(ctx, req.PlacementGroupId)
	if err != nil {
		return nil, err
	}
	return protoconv.PlacementGroup(g), nil
}

// List возвращает страницу групп проекта.
func (h *PlacementGroupHandler) List(ctx context.Context, req *computev1.ListPlacementGroupsRequest) (*computev1.ListPlacementGroupsResponse, error) {
	groups, next, err := h.svc.List(ctx, req.ProjectId, req.Filter,
		ports.Pagination{PageToken: req.PageToken, PageSize: req.PageSize})
	if err != nil {
		return nil, err
	}
	visible, err := listnarrow.Page(ctx, h.listFilter,
		authzfilter.ResourceTypePlacementGroup, authzfilter.ActionPlacementGroupRead, groups,
		func(g *domain.PlacementGroup) string { return g.ID })
	if err != nil {
		return nil, err
	}
	resp := &computev1.ListPlacementGroupsResponse{NextPageToken: next}
	for _, g := range visible {
		resp.PlacementGroups = append(resp.PlacementGroups, protoconv.PlacementGroup(g))
	}
	return resp, nil
}

// Create заводит группу.
func (h *PlacementGroupHandler) Create(ctx context.Context, req *computev1.CreatePlacementGroupRequest) (*operationpb.Operation, error) {
	op, err := h.svc.Create(ctx, placementgroup.CreateReq{
		ProjectID:     req.ProjectId,
		Name:          req.Name,
		Description:   req.Description,
		Labels:        req.Labels,
		Strategy:      domain.PlacementStrategy(req.Strategy),        // proto зеркалит domain
		PlacementType: domain.PlacementAnchorType(req.PlacementType), // proto зеркалит domain
		ZoneID:        req.ZoneId,
		RegionID:      req.RegionId,
	})
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// Update правит косметическую часть группы.
func (h *PlacementGroupHandler) Update(ctx context.Context, req *computev1.UpdatePlacementGroupRequest) (*operationpb.Operation, error) {
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.GetPaths()
	}
	op, err := h.svc.Update(ctx, placementgroup.UpdateReq{
		ID:          req.PlacementGroupId,
		UpdateMask:  mask,
		Name:        req.Name,
		Description: req.Description,
		Labels:      req.Labels,
	})
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// Delete снимает группу.
func (h *PlacementGroupHandler) Delete(ctx context.Context, req *computev1.DeletePlacementGroupRequest) (*operationpb.Operation, error) {
	op, err := h.svc.Delete(ctx, req.PlacementGroupId)
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// ListOperations возвращает операции над группой.
//
// Сторож стоит ЗДЕСЬ, до чтения страницы операций: вызывающий, который не видит
// саму группу, не должен получать её историю.
func (h *PlacementGroupHandler) ListOperations(ctx context.Context, req *computev1.ListPlacementGroupOperationsRequest) (*computev1.ListPlacementGroupOperationsResponse, error) {
	if _, err := h.svc.Get(ctx, req.PlacementGroupId); err != nil {
		return nil, err
	}
	ops, next, err := h.svc.ListOperations(ctx, req.PlacementGroupId,
		ports.Pagination{PageToken: req.PageToken, PageSize: req.PageSize})
	if err != nil {
		return nil, err
	}
	resp := &computev1.ListPlacementGroupOperationsResponse{NextPageToken: next}
	for i := range ops {
		resp.Operations = append(resp.Operations, operationToProto(&ops[i]))
	}
	return resp, nil
}
