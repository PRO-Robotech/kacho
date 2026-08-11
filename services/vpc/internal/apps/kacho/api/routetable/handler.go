// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/pbconv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"

	// Blank-import регистрирует RouteTable/time DTO трансферы.
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Handler — реализация vpcv1.RouteTableServiceServer на основе use-case'ов.
type Handler struct {
	vpcv1.UnimplementedRouteTableServiceServer

	create         *CreateRouteTableUseCase
	update         *UpdateRouteTableUseCase
	delete         *DeleteRouteTableUseCase
	get            *GetRouteTableUseCase
	list           *ListRouteTablesUseCase
	listOperations *ListOperationsUseCase
}

// NewHandler собирает Handler из готовых use-case'ов.
func NewHandler(
	create *CreateRouteTableUseCase,
	update *UpdateRouteTableUseCase,
	deleteUC *DeleteRouteTableUseCase,
	get *GetRouteTableUseCase,
	list *ListRouteTablesUseCase,
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
// per-RPC authz-interceptor прямым Check'ом — см. GetRouteTableUseCase.
func (h *Handler) Get(ctx context.Context, req *vpcv1.GetRouteTableRequest) (*vpcv1.RouteTable, error) {
	if req.RouteTableId == "" {
		return nil, status.Error(codes.InvalidArgument, "route_table_id required")
	}
	rt, err := h.get.Execute(ctx, req.RouteTableId)
	if err != nil {
		return nil, err
	}
	return routeTableToPb(rt)
}

// List — project_id required + FGA list-filter. Project-scope AuthZ (`viewer @
// project:<project_id>`) энфорсит per-RPC authz-interceptor.
func (h *Handler) List(ctx context.Context, req *vpcv1.ListRouteTablesRequest) (*vpcv1.ListRouteTablesResponse, error) {
	rts, nextToken, err := h.list.Execute(ctx, RouteTableFilter{
		ProjectID: req.ProjectId,
		Filter:    req.Filter,
	}, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListRouteTablesResponse{NextPageToken: nextToken}
	for _, rt := range rts {
		pb, err := routeTableToPb(rt)
		if err != nil {
			return nil, err
		}
		resp.RouteTables = append(resp.RouteTables, pb)
	}
	return resp, nil
}

// Create — proto → domain → use-case. Project-scope AuthZ (`editor @
// project:<project_id>`) энфорсит per-RPC authz-interceptor.
func (h *Handler) Create(ctx context.Context, req *vpcv1.CreateRouteTableRequest) (*operationpb.Operation, error) {
	rt := domain.RouteTable{
		ProjectID:   req.ProjectId,
		NetworkID:   req.NetworkId,
		Name:        domain.RcNameVPC(req.Name),
		Description: domain.RcDescription(req.Description),
		Labels:      domain.LabelsFromMap(req.Labels),
	}
	routes, err := staticRoutesFromProto(req.StaticRoutes)
	if err != nil {
		return nil, err
	}
	rt.StaticRoutes = routes
	op, err := h.create.Execute(ctx, rt)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// Update — sync repo.Get (existence → NotFound) + use-case. Per-object AuthZ
// энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) Update(ctx context.Context, req *vpcv1.UpdateRouteTableRequest) (*operationpb.Operation, error) {
	if req.RouteTableId == "" {
		return nil, status.Error(codes.InvalidArgument, "route_table_id required")
	}
	// Непринимаемая ветка следующего перехода — до чтения из репозитория:
	// отказ по форме запроса не должен зависеть от того, существует ли ресурс.
	routes, err := staticRoutesFromProto(req.StaticRoutes)
	if err != nil {
		return nil, err
	}
	if _, gerr := h.get.Execute(ctx, req.RouteTableId); gerr != nil {
		return nil, gerr
	}
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	in := UpdateInput{
		RouteTableID: req.RouteTableId,
		RouteTable: domain.RouteTable{
			Name:        domain.RcNameVPC(req.Name),
			Description: domain.RcDescription(req.Description),
			Labels:      domain.LabelsFromMap(req.Labels),
		},
		UpdateMask: mask,
	}
	in.RouteTable.StaticRoutes = routes
	op, err := h.update.Execute(ctx, in)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// Delete — sync repo.Get (existence → NotFound), затем use-case. Per-object
// AuthZ энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) Delete(ctx context.Context, req *vpcv1.DeleteRouteTableRequest) (*operationpb.Operation, error) {
	if req.RouteTableId == "" {
		return nil, status.Error(codes.InvalidArgument, "route_table_id required")
	}
	if _, err := h.get.Execute(ctx, req.RouteTableId); err != nil {
		return nil, err
	}
	op, err := h.delete.Execute(ctx, req.RouteTableId)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// ListOperations — best-effort existence-probe: ресурс удален (NotFound от get)
// → пропускаем, прочая ошибка возвращается. Per-object AuthZ энфорсит per-RPC
// authz-interceptor.
func (h *Handler) ListOperations(ctx context.Context, req *vpcv1.ListRouteTableOperationsRequest) (*vpcv1.ListRouteTableOperationsResponse, error) {
	if req.RouteTableId == "" {
		return nil, status.Error(codes.InvalidArgument, "route_table_id required")
	}
	if _, gerr := h.get.Execute(ctx, req.RouteTableId); gerr != nil && status.Code(gerr) != codes.NotFound {
		return nil, gerr
	}
	ops, nextToken, err := h.listOperations.Execute(ctx, req.RouteTableId, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListRouteTableOperationsResponse{NextPageToken: nextToken}
	for i := range ops {
		resp.Operations = append(resp.Operations, pbconv.OperationToProto(&ops[i]))
	}
	return resp, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Гранулярная правка маршрутов — ЯВНЫЙ отказ (исход 2), а не молчаливый.
//
// Три verb-RPC объявлены контрактом, и до этих методов ни один не был
// переопределён: вызывающий получал `UNIMPLEMENTED` с текстом
// «method AddRoutes not implemented» — то есть без причины и без рабочего пути,
// при том что сайт документации описывал все три полностью и советовал ими
// пользоваться.
//
// Реализовать два из трёх нельзя: `RemoveRoutes` и `UpdateRoute` адресуют
// маршрут по идентификатору, которого `StaticRoute` не несёт НИ в контракте, НИ
// в домене, НИ в хранилище (`static_routes jsonb` — массив объектов без ключа).
// Идентичность маршрута — отдельный инкремент со своей приёмкой; разбор и
// предикат снятия — `docs/engineering/architecture/07-known-divergences.md`,
// запись 26.
//
// `AddRoutes` идентификатора не требует и реализуем сам по себе, но отказывает
// вместе с соседями НАМЕРЕННО: «добавить гранулярно можно, удалить нельзя» —
// поверхность, на которой набор маршрутов только растёт, и вызывающий узнаёт об
// этом на втором вызове, а не на первом.
//
// Тон и содержание сообщения — часть контракта (проба
// granular_route_refusal_test.go утверждает СООБЩЕНИЕ, а не только код: у
// названного отказа и у унаследованного код один и тот же).
const granularRouteRefusal = "granular route editing is not available: a static route id " +
	"does not exist in this API — StaticRoute carries destination, next hop and labels only. " +
	"Edit the set with RouteTableService.Update and update_mask [\"static_routes\"], sending the " +
	"full list"

func (h *Handler) AddRoutes(_ context.Context, _ *vpcv1.AddRouteTableRoutesRequest) (*operationpb.Operation, error) {
	return nil, status.Error(codes.Unimplemented, granularRouteRefusal)
}

func (h *Handler) RemoveRoutes(_ context.Context, _ *vpcv1.RemoveRouteTableRoutesRequest) (*operationpb.Operation, error) {
	return nil, status.Error(codes.Unimplemented, granularRouteRefusal)
}

func (h *Handler) UpdateRoute(_ context.Context, _ *vpcv1.UpdateRouteTableRouteRequest) (*operationpb.Operation, error) {
	return nil, status.Error(codes.Unimplemented, granularRouteRefusal)
}

// routeTableToPb — repo-entity RouteTable → proto RouteTable через DTO-реестр.
func routeTableToPb(rec *kachorepo.RouteTableRecord) (*vpcv1.RouteTable, error) {
	var dst *vpcv1.RouteTable
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, status.Error(codes.Internal, "dto.Transfer RouteTable failed")
	}
	return dst, nil
}
