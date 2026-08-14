// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/applystate"

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
	// applyState — заполнитель публичного поля состояния применения.
	// Провязывается композиционным корнем; нулевое значение означает
	// «утверждения нет» и к базе не ходит.
	applyState *applystate.Filler
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

// WithApplyState провязывает заполнитель состояния применения.
//
// Отдельным методом, а не аргументом конструктора: у семи ресурсов
// конструкторы разной формы, и добавление восьмого позиционного аргумента
// сделало бы каждую их правку правкой всех вызывающих. Провязку боевой
// сборки держит гейт по дереву — он же ловит забытый вызов.
func (h *Handler) WithApplyState(f *applystate.Filler) *Handler {
	h.applyState = f
	return h
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
	pb, err := routeTableToPb(rt)
	if err != nil {
		return nil, err
	}
	// Состояние применения — ОТДЕЛЬНЫМ вопросом к проекции подтверждений, а не
	// полем строки ресурса: оно выводится сравнением ревизий и живёт в другой
	// таблице. Незаполненное поле означает «утверждения нет».
	if pb.ApplyState, err = h.applyState.One(ctx, pb.GetId()); err != nil {
		return nil, err
	}
	return pb, nil
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
	// Состояние применения СТРАНИЦЫ — одним обращением к проекции: стоимость
	// принадлежит запросу, а не популяции проекта. Спрашивается ПОСЛЕ того, как
	// страница отобрана и сужена правами, то есть по идентификаторам, которые
	// вызывающий и так увидит.
	if ferr := applystate.FillPage(ctx, h.applyState, resp.RouteTables,
		func(p *vpcv1.RouteTable) string { return p.GetId() },
		func(p *vpcv1.RouteTable, st *vpcv1.ApplyState) { p.ApplyState = st },
	); ferr != nil {
		return nil, ferr
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

// Здесь стоял ЯВНЫЙ отказ трёх методов гранулярной правки маршрутов и текст этого
// отказа — снято вместе с предметом.
//
// Отказ был исходом 2 конвенции: методы объявлены контрактом, реализовать их нельзя
// (два из трёх адресуют маршрут по идентификатору, которого набор не несёт ни в
// контракте, ни в домене, ни в хранилище), поэтому они отказывали явно и с причиной
// вместо молчаливого «не реализовано». Волна снятий перевела предмет в исход 3: методы
// сняты с контракта с резервированием номеров и имён, разрыв объявлен в перечне.
//
// Константа пережила своих вызывающих на один заход и держалась тем, что компилятор не
// проверяет достижимость. Её комментарий при этом ссылался на пробу, которой в дереве
// НЕТ — то есть утверждал, что текст закреплён, когда закреплять было уже нечего.
// Решение и предикат снятия остаются в записи 26
// docs/engineering/architecture/07-known-divergences.md — там им и место.

// routeTableToPb — repo-entity RouteTable → proto RouteTable через DTO-реестр.
func routeTableToPb(rec *kachorepo.RouteTableRecord) (*vpcv1.RouteTable, error) {
	var dst *vpcv1.RouteTable
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, status.Error(codes.Internal, "dto.Transfer RouteTable failed")
	}
	return dst, nil
}
