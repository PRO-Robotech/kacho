// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/pbconv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"

	// Blank-import регистрирует Gateway/time DTO-трансферы через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
)

// Handler — реализация vpcv1.GatewayServiceServer на основе use-case'ов. Тонкий
// transport-слой: proto-request → domain → use-case → proto-response. Никакой
// бизнес-логики.
type Handler struct {
	vpcv1.UnimplementedGatewayServiceServer

	create         *CreateGatewayUseCase
	update         *UpdateGatewayUseCase
	delete         *DeleteGatewayUseCase
	get            *GetGatewayUseCase
	list           *ListGatewaysUseCase
	listOperations *ListOperationsUseCase
}

// NewHandler собирает Handler из готовых use-case'ов.
func NewHandler(
	create *CreateGatewayUseCase,
	update *UpdateGatewayUseCase,
	deleteUC *DeleteGatewayUseCase,
	get *GetGatewayUseCase,
	list *ListGatewaysUseCase,
	listOps *ListOperationsUseCase,
) *Handler {
	return &Handler{
		create:         create,
		update:         update,
		delete:         deleteUC,
		get:            get,
		list:           list,
		listOperations: listOps,
	}
}

// Get — sync read. Per-object AuthZ (включая existence-hiding на deny) энфорсит
// per-RPC authz-interceptor прямым Check'ом — см. GetGatewayUseCase.
func (h *Handler) Get(ctx context.Context, req *vpcv1.GetGatewayRequest) (*vpcv1.Gateway, error) {
	if req.GatewayId == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id required")
	}
	g, err := h.get.Execute(ctx, req.GatewayId)
	if err != nil {
		return nil, err
	}
	pb, err := gatewayToPb(g)
	if err != nil {
		return nil, err
	}
	return pb, nil
}

// List — project_id required + FGA list-filter. Project-scope AuthZ (`viewer @
// project:<project_id>`) энфорсит per-RPC authz-interceptor.
func (h *Handler) List(ctx context.Context, req *vpcv1.ListGatewaysRequest) (*vpcv1.ListGatewaysResponse, error) {
	gws, nextToken, err := h.list.Execute(ctx, GatewayFilter{
		ProjectID: req.ProjectId,
		Filter:    req.Filter,
	}, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListGatewaysResponse{NextPageToken: nextToken}
	for _, g := range gws {
		pb, err := gatewayToPb(g)
		if err != nil {
			return nil, err
		}
		resp.Gateways = append(resp.Gateways, pb)
	}
	return resp, nil
}

// Create — proto → domain → use-case. Project-scope AuthZ (`editor @
// project:<project_id>`) энфорсит per-RPC authz-interceptor.
func (h *Handler) Create(ctx context.Context, req *vpcv1.CreateGatewayRequest) (*operationpb.Operation, error) {
	g := domain.Gateway{
		ProjectID:   req.ProjectId,
		Name:        domain.RcNameVPC(req.Name),
		Description: domain.RcDescription(req.Description),
		Labels:      domain.LabelsFromMap(req.Labels),
		GatewayType: gatewayTypeFromCreateSpec(req),
		SubnetID:    req.SubnetId,
	}
	op, err := h.create.Execute(ctx, g)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// Update — sync repo.Get (existence → NotFound) + use-case. Per-object AuthZ
// энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) Update(ctx context.Context, req *vpcv1.UpdateGatewayRequest) (*operationpb.Operation, error) {
	if req.GatewayId == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id required")
	}
	if _, err := h.get.Execute(ctx, req.GatewayId); err != nil {
		return nil, err
	}
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	// Вид шлюза и его привязка на пути обновления НЕ читаются, и это не пропуск:
	// оба неизменяемы после Create, поэтому их ветвь oneof снята с
	// `UpdateGatewayRequest` с резервированием номера и имени. Указание их имён в
	// `update_mask` отвергает use-case конвенционным тоном.
	in := UpdateInput{
		GatewayID: req.GatewayId,
		Gateway: domain.Gateway{
			Name:        domain.RcNameVPC(req.Name),
			Description: domain.RcDescription(req.Description),
			Labels:      domain.LabelsFromMap(req.Labels),
		},
		UpdateMask: mask,
	}
	op, err := h.update.Execute(ctx, in)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// Delete — sync repo.Get (existence → NotFound), затем use-case. Per-object
// AuthZ энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) Delete(ctx context.Context, req *vpcv1.DeleteGatewayRequest) (*operationpb.Operation, error) {
	if req.GatewayId == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id required")
	}
	if _, err := h.get.Execute(ctx, req.GatewayId); err != nil {
		return nil, err
	}
	op, err := h.delete.Execute(ctx, req.GatewayId)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// ListOperations — best-effort existence-probe: ресурс удален (NotFound от get)
// → пропускаем (история операций должна оставаться доступной), прочая ошибка
// возвращается. Per-object AuthZ энфорсит per-RPC authz-interceptor.
func (h *Handler) ListOperations(ctx context.Context, req *vpcv1.ListGatewayOperationsRequest) (*vpcv1.ListGatewayOperationsResponse, error) {
	if req.GatewayId == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id required")
	}
	if _, gerr := h.get.Execute(ctx, req.GatewayId); gerr != nil && status.Code(gerr) != codes.NotFound {
		return nil, gerr
	}
	ops, nextToken, err := h.listOperations.Execute(ctx, req.GatewayId, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListGatewayOperationsResponse{NextPageToken: nextToken}
	for i := range ops {
		resp.Operations = append(resp.Operations, pbconv.OperationToProto(&ops[i]))
	}
	return resp, nil
}

// gatewayToPb — repo-entity Gateway → proto Gateway через DTO-реестр.
func gatewayToPb(rec *kacho.GatewayRecord) (*vpcv1.Gateway, error) {
	var dst *vpcv1.Gateway
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, status.Error(codes.Internal, "dto.Transfer Gateway failed")
	}
	return dst, nil
}
