// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/pbconv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"

	// Blank-import регистрирует Network/time DTO трансферы через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Handler — реализация vpcv1.NetworkServiceServer на основе use-case'ов. Тонкий
// transport-слой: proto-request → domain → use-case → proto-response. Никакой
// бизнес-логики.
type Handler struct {
	vpcv1.UnimplementedNetworkServiceServer

	create            *CreateNetworkUseCase
	update            *UpdateNetworkUseCase
	delete            *DeleteNetworkUseCase
	get               *GetNetworkUseCase
	list              *ListNetworksUseCase
	addCidrBlocks     *AddCidrBlocksUseCase
	removeCidrBlocks  *RemoveCidrBlocksUseCase
	listSubnets       *ListSubnetsUseCase
	listSecurityGroup *ListSecurityGroupsUseCase
	listRouteTables   *ListRouteTablesUseCase
	listOperations    *ListOperationsUseCase
}

// NewHandler собирает Handler из готовых use-case'ов. Конструктор намеренно
// принимает все use-case'ы — composition-root (cmd/vpc/main.go) собирает их
// с одинаковыми зависимостями (repo / projectClient / opsRepo).
func NewHandler(
	create *CreateNetworkUseCase,
	update *UpdateNetworkUseCase,
	deleteUC *DeleteNetworkUseCase,
	get *GetNetworkUseCase,
	list *ListNetworksUseCase,
	addCidr *AddCidrBlocksUseCase,
	removeCidr *RemoveCidrBlocksUseCase,
	listSubnets *ListSubnetsUseCase,
	listSG *ListSecurityGroupsUseCase,
	listRT *ListRouteTablesUseCase,
	listOps *ListOperationsUseCase,
) *Handler {
	return &Handler{
		create:            create,
		update:            update,
		delete:            deleteUC,
		get:               get,
		list:              list,
		addCidrBlocks:     addCidr,
		removeCidrBlocks:  removeCidr,
		listSubnets:       listSubnets,
		listSecurityGroup: listSG,
		listRouteTables:   listRT,
		listOperations:    listOps,
	}
}

// Get — sync read. Per-object AuthZ (включая existence-hiding на deny) энфорсит
// per-RPC authz-interceptor прямым Check'ом — см. GetNetworkUseCase.
func (h *Handler) Get(ctx context.Context, req *vpcv1.GetNetworkRequest) (*vpcv1.Network, error) {
	if req.NetworkId == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	n, err := h.get.Execute(ctx, req.NetworkId)
	if err != nil {
		return nil, err
	}
	pb, err := networkToPb(n)
	if err != nil {
		return nil, err
	}
	return pb, nil
}

// List — project_id required + FGA list-filter. Project-scope AuthZ (`viewer @
// project:<project_id>`) энфорсит per-RPC authz-interceptor — чужой project
// отвергается там.
//
// Subject из ctx (principal-extractor) → страница из БД → per-object BatchCheck,
// который сужает результат ПОВЕРХ Check'а, но не заменяет его.
func (h *Handler) List(ctx context.Context, req *vpcv1.ListNetworksRequest) (*vpcv1.ListNetworksResponse, error) {
	nets, nextToken, err := h.list.Execute(ctx, NetworkFilter{
		ProjectID: req.ProjectId,
		Filter:    req.Filter,
	}, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListNetworksResponse{NextPageToken: nextToken}
	for _, n := range nets {
		pb, err := networkToPb(n)
		if err != nil {
			return nil, err
		}
		resp.Networks = append(resp.Networks, pb)
	}
	return resp, nil
}

// Create — proto → domain → use-case. Project-scope AuthZ (`editor @
// project:<project_id>`) энфорсит per-RPC authz-interceptor.
func (h *Handler) Create(ctx context.Context, req *vpcv1.CreateNetworkRequest) (*operationpb.Operation, error) {
	n := domain.Network{
		ProjectID:      req.ProjectId,
		Name:           domain.RcNameVPC(req.Name),
		Description:    domain.RcDescription(req.Description),
		Labels:         domain.LabelsFromMap(req.Labels),
		IPv4CidrBlocks: req.Ipv4CidrBlocks,
		IPv6CidrBlocks: req.Ipv6CidrBlocks,
	}
	// `create_default_security_group` — tri-state (`optional bool`), и различие
	// «не прислано» / `false` обязано дойти до решения: поле объявляет, что явный
	// выбор вызывающего сильнее конфига стенда, а молчание падает на конфиг.
	// Прежде поле здесь просто не читалось, и решение всегда принимал конфиг.
	op, err := h.create.Execute(ctx, n)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// Update — sync repo.Get (existence → NotFound) + use-case. Per-object AuthZ
// энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) Update(ctx context.Context, req *vpcv1.UpdateNetworkRequest) (*operationpb.Operation, error) {
	if req.NetworkId == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	if _, err := h.get.Execute(ctx, req.NetworkId); err != nil {
		return nil, err
	}
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	in := UpdateInput{
		NetworkID: req.NetworkId,
		Network: domain.Network{
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

// AddCidrBlocks — verb-action: расширяет declared-супернет сети. Тонкий transport:
// existence-check (get network → NotFound) → use-case.Execute. Per-object AuthZ
// энфорсит per-RPC authz-interceptor. Малформед id ловится first-statement внутри
// use-case (corevalidate.ResourceID).
func (h *Handler) AddCidrBlocks(ctx context.Context, req *vpcv1.AddNetworkCidrBlocksRequest) (*operationpb.Operation, error) {
	if req.NetworkId == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	if _, err := h.get.Execute(ctx, req.NetworkId); err != nil {
		return nil, err
	}
	op, err := h.addCidrBlocks.Execute(ctx, req.NetworkId, req.GetIpv4CidrBlocks(), req.GetIpv6CidrBlocks())
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// RemoveCidrBlocks — verb-action: сужает declared-супернет сети (∉-guard на живые
// подсети — в use-case). Тонкий transport (existence-check → use-case.Execute);
// per-object AuthZ энфорсит per-RPC authz-interceptor.
func (h *Handler) RemoveCidrBlocks(ctx context.Context, req *vpcv1.RemoveNetworkCidrBlocksRequest) (*operationpb.Operation, error) {
	if req.NetworkId == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	if _, err := h.get.Execute(ctx, req.NetworkId); err != nil {
		return nil, err
	}
	op, err := h.removeCidrBlocks.Execute(ctx, req.NetworkId, req.GetIpv4CidrBlocks(), req.GetIpv6CidrBlocks())
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// ListOperations — best-effort existence-probe: ресурс удален (NotFound от get)
// → пропускаем (история операций должна оставаться доступной), прочая ошибка
// возвращается. Per-object AuthZ энфорсит per-RPC authz-interceptor.
func (h *Handler) ListOperations(ctx context.Context, req *vpcv1.ListNetworkOperationsRequest) (*vpcv1.ListNetworkOperationsResponse, error) {
	if req.NetworkId == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	if _, gerr := h.get.Execute(ctx, req.NetworkId); gerr != nil && status.Code(gerr) != codes.NotFound {
		return nil, gerr
	}
	ops, nextToken, err := h.listOperations.Execute(ctx, req.NetworkId, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListNetworkOperationsResponse{NextPageToken: nextToken}
	for i := range ops {
		resp.Operations = append(resp.Operations, pbconv.OperationToProto(&ops[i]))
	}
	return resp, nil
}

// Delete — sync repo.Get (existence → NotFound), затем use-case. Per-object
// AuthZ энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) Delete(ctx context.Context, req *vpcv1.DeleteNetworkRequest) (*operationpb.Operation, error) {
	if req.NetworkId == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	if _, err := h.get.Execute(ctx, req.NetworkId); err != nil {
		return nil, err
	}
	op, err := h.delete.Execute(ctx, req.NetworkId)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// networkToPb — repo-entity Network → proto Network через DTO-реестр.
func networkToPb(rec *kachorepo.NetworkRecord) (*vpcv1.Network, error) {
	var dst *vpcv1.Network
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, status.Error(codes.Internal, "dto.Transfer Network failed")
	}
	return dst, nil
}

// Здесь стояли три преобразователя дочерних ресурсов — снят вместе со своим предметом.
//
// Они обслуживали под-перечисления сети, снятые с контракта: второй путь к ответу, который
// уже даёт список самого ресурса с сужением по сети. Функции пережили снятие методов на
// один заход и держались только тем, что компилятор их не проверяет на достижимость.
//
// Одноимённые преобразователи в пакетах САМИХ ресурсов живы и используются их списками —
// это разные функции с совпадающими именами. Классифицировать совпадения по референту
// обязательно: удаление «всех вхождений имени» вынесло бы отсюда мёртвое вместе с живым.
