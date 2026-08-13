// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cidrgroup

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/pbconv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// Handler — реализация vpcv1.CidrGroupServiceServer поверх use-case'ов. Тонкий
// транспорт: разбор запроса → domain → use-case → формат ответа. Бизнес-логики
// здесь нет.
type Handler struct {
	vpcv1.UnimplementedCidrGroupServiceServer

	create           *CreateCidrGroupUseCase
	update           *UpdateCidrGroupUseCase
	delete           *DeleteCidrGroupUseCase
	get              *GetCidrGroupUseCase
	list             *ListCidrGroupsUseCase
	addCidrBlocks    *AddCidrBlocksUseCase
	removeCidrBlocks *RemoveCidrBlocksUseCase
	listOperations   *ListOperationsUseCase
}

// NewHandler собирает Handler из готовых use-case'ов.
func NewHandler(
	create *CreateCidrGroupUseCase,
	update *UpdateCidrGroupUseCase,
	deleteUC *DeleteCidrGroupUseCase,
	get *GetCidrGroupUseCase,
	list *ListCidrGroupsUseCase,
	addCidr *AddCidrBlocksUseCase,
	removeCidr *RemoveCidrBlocksUseCase,
	listOps *ListOperationsUseCase,
) *Handler {
	return &Handler{
		create:           create,
		update:           update,
		delete:           deleteUC,
		get:              get,
		list:             list,
		addCidrBlocks:    addCidr,
		removeCidrBlocks: removeCidr,
		listOperations:   listOps,
	}
}

// Get — синхронное чтение. Пообъектную видимость (включая скрытие
// существования на отказе) энфорсит per-RPC authz-интерсептор.
func (h *Handler) Get(ctx context.Context, req *vpcv1.GetCidrGroupRequest) (*vpcv1.CidrGroup, error) {
	if req.CidrGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "cidr_group_id required")
	}
	rec, err := h.get.Execute(ctx, req.CidrGroupId)
	if err != nil {
		return nil, err
	}
	return cidrGroupToPb(rec)
}

// List — project_id обязателен; проектную область энфорсит per-RPC
// authz-интерсептор, а страница сужается построчно внутри use-case'а.
func (h *Handler) List(ctx context.Context, req *vpcv1.ListCidrGroupsRequest) (*vpcv1.ListCidrGroupsResponse, error) {
	groups, nextToken, err := h.list.Execute(ctx, CidrGroupFilter{
		ProjectID: req.ProjectId,
		Filter:    req.Filter,
	}, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListCidrGroupsResponse{NextPageToken: nextToken}
	for _, rec := range groups {
		pb, cerr := cidrGroupToPb(rec)
		if cerr != nil {
			return nil, cerr
		}
		resp.CidrGroups = append(resp.CidrGroups, pb)
	}
	return resp, nil
}

// Create — proto → domain → use-case.
func (h *Handler) Create(ctx context.Context, req *vpcv1.CreateCidrGroupRequest) (*operationpb.Operation, error) {
	g := domain.CidrGroup{
		ProjectID:    req.ProjectId,
		Name:         domain.RcNameVPC(req.Name),
		Description:  domain.RcDescription(req.Description),
		Labels:       domain.LabelsFromMap(req.Labels),
		V4CidrBlocks: req.V4CidrBlocks,
		V6CidrBlocks: req.V6CidrBlocks,
	}
	op, err := h.create.Execute(ctx, g)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// Update — существование проверяется синхронно (NotFound), затем use-case.
func (h *Handler) Update(ctx context.Context, req *vpcv1.UpdateCidrGroupRequest) (*operationpb.Operation, error) {
	if req.CidrGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "cidr_group_id required")
	}
	if _, err := h.get.Execute(ctx, req.CidrGroupId); err != nil {
		return nil, err
	}
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	op, err := h.update.Execute(ctx, UpdateInput{
		CidrGroupID: req.CidrGroupId,
		CidrGroup: domain.CidrGroup{
			Name:        domain.RcNameVPC(req.Name),
			Description: domain.RcDescription(req.Description),
			Labels:      domain.LabelsFromMap(req.Labels),
		},
		UpdateMask: mask,
	})
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// AddCidrBlocks — глагол состава. Малформированный id ловится первым
// стейтментом внутри use-case'а.
func (h *Handler) AddCidrBlocks(ctx context.Context, req *vpcv1.AddCidrGroupCidrBlocksRequest) (*operationpb.Operation, error) {
	if req.CidrGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "cidr_group_id required")
	}
	if _, err := h.get.Execute(ctx, req.CidrGroupId); err != nil {
		return nil, err
	}
	op, err := h.addCidrBlocks.Execute(ctx, req.CidrGroupId, req.GetV4CidrBlocks(), req.GetV6CidrBlocks())
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// RemoveCidrBlocks — парный глагол состава.
func (h *Handler) RemoveCidrBlocks(ctx context.Context, req *vpcv1.RemoveCidrGroupCidrBlocksRequest) (*operationpb.Operation, error) {
	if req.CidrGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "cidr_group_id required")
	}
	if _, err := h.get.Execute(ctx, req.CidrGroupId); err != nil {
		return nil, err
	}
	op, err := h.removeCidrBlocks.Execute(ctx, req.CidrGroupId, req.GetV4CidrBlocks(), req.GetV6CidrBlocks())
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// Delete — существование проверяется синхронно, затем use-case (который сначала
// отвергает удаление набора с живыми ссылками).
func (h *Handler) Delete(ctx context.Context, req *vpcv1.DeleteCidrGroupRequest) (*operationpb.Operation, error) {
	if req.CidrGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "cidr_group_id required")
	}
	if _, err := h.get.Execute(ctx, req.CidrGroupId); err != nil {
		return nil, err
	}
	op, err := h.delete.Execute(ctx, req.CidrGroupId)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// ListOperations — история операций набора. Ресурс удалён (NotFound от чтения)
// → пропускаем: история обязана оставаться доступной. Прочая ошибка чтения
// возвращается.
func (h *Handler) ListOperations(ctx context.Context, req *vpcv1.ListCidrGroupOperationsRequest) (*vpcv1.ListCidrGroupOperationsResponse, error) {
	if req.CidrGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "cidr_group_id required")
	}
	if _, gerr := h.get.Execute(ctx, req.CidrGroupId); gerr != nil && status.Code(gerr) != codes.NotFound {
		return nil, gerr
	}
	ops, nextToken, err := h.listOperations.Execute(ctx, req.CidrGroupId, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListCidrGroupOperationsResponse{NextPageToken: nextToken}
	for i := range ops {
		resp.Operations = append(resp.Operations, pbconv.OperationToProto(&ops[i]))
	}
	return resp, nil
}
