// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	reference "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/reference"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/pbconv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"

	// Blank-import регистрирует Subnet/Address/time DTO-трансферы.
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
)

// Handler — реализация vpcv1.SubnetServiceServer на основе use-case'ов.
// Тонкий transport-слой: proto-request → domain → use-case → proto-response.
// Никакой бизнес-логики.
type Handler struct {
	vpcv1.UnimplementedSubnetServiceServer

	create            *CreateSubnetUseCase
	update            *UpdateSubnetUseCase
	delete            *DeleteSubnetUseCase
	get               *GetSubnetUseCase
	list              *ListSubnetsUseCase
	addCidrBlocks     *AddCidrBlocksUseCase
	removeCidrBlocks  *RemoveCidrBlocksUseCase
	listUsedAddresses *ListUsedAddressesUseCase
	listOperations    *ListOperationsUseCase
}

// NewHandler собирает Handler из готовых use-case'ов. Конструктор намеренно
// принимает все use-case'ы — composition-root (cmd/vpc/main.go) собирает их
// с одинаковыми зависимостями (repo / networkReader / projectClient / zoneReg /
// opsRepo / addrRefRepo / nicRepo).
func NewHandler(
	create *CreateSubnetUseCase,
	update *UpdateSubnetUseCase,
	deleteUC *DeleteSubnetUseCase,
	get *GetSubnetUseCase,
	list *ListSubnetsUseCase,
	addCidr *AddCidrBlocksUseCase,
	removeCidr *RemoveCidrBlocksUseCase,
	listUsedAddrs *ListUsedAddressesUseCase,
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
		listUsedAddresses: listUsedAddrs,
		listOperations:    listOps,
	}
}

// Get — sync read. Per-object AuthZ (включая existence-hiding на deny) энфорсит
// per-RPC authz-interceptor прямым Check'ом — см. GetSubnetUseCase.
func (h *Handler) Get(ctx context.Context, req *vpcv1.GetSubnetRequest) (*vpcv1.Subnet, error) {
	if req.SubnetId == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	s, err := h.get.Execute(ctx, req.SubnetId)
	if err != nil {
		return nil, err
	}
	return subnetToPb(s)
}

// List — project_id required + per-object FGA list-filter. Project-scope AuthZ
// (`viewer @ project:<project_id>`) энфорсит per-RPC authz-interceptor.
func (h *Handler) List(ctx context.Context, req *vpcv1.ListSubnetsRequest) (*vpcv1.ListSubnetsResponse, error) {
	subject := pbconv.SubjectFromContext(ctx)
	subs, nextToken, err := h.list.Execute(ctx, subject, SubnetFilter{
		ProjectID: req.ProjectId,
		Filter:    req.Filter,
	}, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListSubnetsResponse{NextPageToken: nextToken}
	for _, s := range subs {
		pb, err := subnetToPb(s)
		if err != nil {
			return nil, err
		}
		resp.Subnets = append(resp.Subnets, pb)
	}
	return resp, nil
}

// Create — proto → domain → use-case. Project-scope AuthZ (`editor @
// project:<project_id>`) энфорсит per-RPC authz-interceptor.
func (h *Handler) Create(ctx context.Context, req *vpcv1.CreateSubnetRequest) (*operationpb.Operation, error) {
	s := domain.Subnet{
		ProjectID:     req.ProjectId,
		Name:          domain.RcNameVPC(req.Name),
		Description:   domain.RcDescription(req.Description),
		Labels:        domain.LabelsFromMap(req.Labels),
		NetworkID:     req.NetworkId,
		PlacementType: placementFromPb(req.PlacementType),
		ZoneID:        req.ZoneId,
		RegionID:      req.RegionId,
		// VPC-1 F7: Create carries only the immutable primary anchor. Additional
		// ranges arrive later via AddCidrBlocks. The flat domain array holds the
		// primary as blocks[0]; empty primary → v-only family (subnet may be v6-only).
		V4CidrBlocks: cidrPrimaryToBlocks(req.Ipv4CidrPrimary),
		V6CidrBlocks: cidrPrimaryToBlocks(req.Ipv6CidrPrimary),
		RouteTableID: req.RouteTableId,
	}
	// VPC-1-43: DhcpOptions dropped by design — no dhcp_options on Create.
	op, err := h.create.Execute(ctx, s)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// cidrPrimaryToBlocks оборачивает непустой primary-anchor в одноэлементный
// массив блоков (domain хранит плоский массив, primary = blocks[0]). Пустой
// primary → nil (v-only family: подсеть может быть создана без этого семейства).
func cidrPrimaryToBlocks(primary string) []string {
	if primary == "" {
		return nil
	}
	return []string{primary}
}

// placementFromPb — proto-enum дискриминатора размещения → domain. UNSPECIFIED
// (или неизвестное) → PlacementUnspecified; use-case отвергает его InvalidArgument.
func placementFromPb(p vpcv1.SubnetPlacementType) domain.SubnetPlacementType {
	switch p {
	case vpcv1.SubnetPlacementType_ZONAL:
		return domain.PlacementZonal
	case vpcv1.SubnetPlacementType_REGIONAL:
		return domain.PlacementRegional
	default:
		return domain.PlacementUnspecified
	}
}

// Update — sync repo.Get (existence → NotFound) + use-case. Per-object AuthZ
// энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) Update(ctx context.Context, req *vpcv1.UpdateSubnetRequest) (*operationpb.Operation, error) {
	if req.SubnetId == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	if _, err := h.get.Execute(ctx, req.SubnetId); err != nil {
		return nil, err
	}
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	in := UpdateInput{
		SubnetID: req.SubnetId,
		Subnet: domain.Subnet{
			Name:         domain.RcNameVPC(req.Name),
			Description:  domain.RcDescription(req.Description),
			Labels:       domain.LabelsFromMap(req.Labels),
			RouteTableID: req.RouteTableId,
		},
		// VPC-1 F7: CIDR is immutable via Update (no CIDR fields on UpdateSubnetRequest);
		// ipv4_cidr_primary/ipv4_cidr_blocks in update_mask → immutable-reject (use-case).
		UpdateMask: mask,
	}
	// VPC-1-43: DhcpOptions dropped by design — no dhcp_options on Update.
	op, err := h.update.Execute(ctx, in)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// Delete — sync repo.Get (existence → NotFound), затем use-case. Per-object
// AuthZ энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) Delete(ctx context.Context, req *vpcv1.DeleteSubnetRequest) (*operationpb.Operation, error) {
	if req.SubnetId == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	if _, err := h.get.Execute(ctx, req.SubnetId); err != nil {
		return nil, err
	}
	op, err := h.delete.Execute(ctx, req.SubnetId)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// AddCidrBlocks — sync repo.Get (existence → NotFound), затем use-case.
// Per-object AuthZ энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) AddCidrBlocks(ctx context.Context, req *vpcv1.AddSubnetCidrBlocksRequest) (*operationpb.Operation, error) {
	if req.SubnetId == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	if _, err := h.get.Execute(ctx, req.SubnetId); err != nil {
		return nil, err
	}
	op, err := h.addCidrBlocks.Execute(ctx, req.SubnetId, req.GetIpv4CidrBlocks(), req.GetIpv6CidrBlocks())
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// RemoveCidrBlocks — sync repo.Get (existence → NotFound), затем use-case.
// Per-object AuthZ энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) RemoveCidrBlocks(ctx context.Context, req *vpcv1.RemoveSubnetCidrBlocksRequest) (*operationpb.Operation, error) {
	if req.SubnetId == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	if _, err := h.get.Execute(ctx, req.SubnetId); err != nil {
		return nil, err
	}
	op, err := h.removeCidrBlocks.Execute(ctx, req.SubnetId, req.GetIpv4CidrBlocks(), req.GetIpv6CidrBlocks())
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// ListUsedAddresses — sync read; existence parent Subnet'а (→ NotFound). Доступ
// к parent'у гейтит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) ListUsedAddresses(ctx context.Context, req *vpcv1.ListUsedAddressesRequest) (*vpcv1.ListUsedAddressesResponse, error) {
	if req.SubnetId == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	if _, err := h.get.Execute(ctx, req.SubnetId); err != nil {
		return nil, err
	}
	addrs, refs, nextToken, err := h.listUsedAddresses.Execute(ctx, req.SubnetId, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListUsedAddressesResponse{NextPageToken: nextToken}
	for _, a := range addrs {
		ua := &vpcv1.UsedAddress{
			IpVersion: vpcv1.IpVersion(a.IpVersion),
		}
		if a.InternalIpv4 != nil {
			ua.Address = a.InternalIpv4.Address
		} else if a.ExternalIpv4 != nil {
			ua.Address = a.ExternalIpv4.Address
		}
		// references[] — кто использует адрес (referrer-tracking).
		if ref, ok := refs[a.ID]; ok && ref != nil {
			ua.References = []*reference.Reference{{
				Referrer: &reference.Referrer{Type: ref.ReferrerType, Id: ref.ReferrerID},
				Type:     reference.Reference_USED_BY,
			}}
		}
		resp.Addresses = append(resp.Addresses, ua)
	}
	return resp, nil
}

// ListOperations — best-effort existence-probe: ресурс удален (NotFound от get)
// → пропускаем (история операций должна оставаться доступной), прочая ошибка
// возвращается. Per-object AuthZ энфорсит per-RPC authz-interceptor.
func (h *Handler) ListOperations(ctx context.Context, req *vpcv1.ListSubnetOperationsRequest) (*vpcv1.ListSubnetOperationsResponse, error) {
	if req.SubnetId == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	if _, gerr := h.get.Execute(ctx, req.SubnetId); gerr != nil && status.Code(gerr) != codes.NotFound {
		return nil, gerr
	}
	ops, nextToken, err := h.listOperations.Execute(ctx, req.SubnetId, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListSubnetOperationsResponse{NextPageToken: nextToken}
	for i := range ops {
		resp.Operations = append(resp.Operations, pbconv.OperationToProto(&ops[i]))
	}
	return resp, nil
}

// subnetToPb — repo-entity Subnet → proto Subnet через DTO-реестр.
func subnetToPb(rec *kachorepo.SubnetRecord) (*vpcv1.Subnet, error) {
	var dst *vpcv1.Subnet
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, status.Error(codes.Internal, "dto.Transfer Subnet failed")
	}
	return dst, nil
}
