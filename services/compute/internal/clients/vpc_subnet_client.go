// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/retry"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// defaultSubnetCallTimeout — per-call deadline ребра compute→vpc
// (vpc.v1.SubnetService.Get). Как у geo-ребра: зависший (не отвечающий, не
// Unavailable) peer иначе парковал бы worker-горутину до op-timeout'а
// (architecture.md «per-call deadline на КАЖДОМ внешнем вызове»).
const defaultSubnetCallTimeout = 3 * time.Second

// VPCSubnetClient реализует ports.SubnetRegistry поверх публичного
// vpc.v1.SubnetService (ребро compute→vpc). Читается ПОД ИДЕНТИЧНОСТЬЮ ВЫЗЫВАЮЩЕГО
// (auth.PropagateOutgoing): подсеть, которую тенант не видит, отвечает
// PermissionDenied и здесь неотличима от несуществующей (hide-existence,
// анти-BOLA) — consumer не превращается в оракул чужого placement'а.
type VPCSubnetClient struct {
	subnets vpcv1.SubnetServiceClient
	timeout time.Duration
}

// NewVPCSubnetClient оборачивает grpc-conn (публичный listener kacho-vpc) в
// typed adapter.
func NewVPCSubnetClient(conn grpc.ClientConnInterface) *VPCSubnetClient {
	if conn == nil {
		return nil
	}
	return &VPCSubnetClient{subnets: vpcv1.NewSubnetServiceClient(conn), timeout: defaultSubnetCallTimeout}
}

// NewVPCSubnetClientWith — seam для unit-тестов поверх готового stub'а.
func NewVPCSubnetClientWith(subnets vpcv1.SubnetServiceClient) *VPCSubnetClient {
	if subnets == nil {
		return nil
	}
	return &VPCSubnetClient{subnets: subnets, timeout: defaultSubnetCallTimeout}
}

// GetSubnet возвращает placement-проекцию подсети. Дискриминатор placement_type
// переносится как есть: ZONAL → zone_id, REGIONAL (anycast) → region_id.
func (c *VPCSubnetClient) GetSubnet(ctx context.Context, subnetID string) (*ports.SubnetPlacement, error) {
	var resp *vpcv1.Subnet
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()
		var rerr error
		resp, rerr = c.subnets.Get(auth.PropagateOutgoing(callCtx), &vpcv1.GetSubnetRequest{SubnetId: subnetID})
		if rerr != nil {
			if st, ok := status.FromError(rerr); ok {
				switch st.Code() {
				case codes.NotFound, codes.PermissionDenied, codes.InvalidArgument:
					// Нет подсети / нет доступа / нечитаемый id — одинаково:
					// «ссылка не резолвится». Различать их наружу нельзя.
					return ports.ErrNotFound
				}
			}
			return rerr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ports.SubnetPlacement{
		ID:            resp.GetId(),
		ProjectID:     resp.GetProjectId(),
		PlacementType: resp.GetPlacementType().String(),
		ZoneID:        resp.GetZoneId(),
		RegionID:      resp.GetRegionId(),
	}, nil
}

// NoopSubnetClient — заглушка для KACHO_COMPUTE_SKIP_PEER_VALIDATION=true
// (cross-service проверки отключены, dev/test-only). Возвращает МАРКЕР
// ErrPeerValidationSkipped, а не выдуманный placement: фейковая «подходящая»
// подсеть была бы ложью о чужом ресурсе, а fail-closed противоречил бы смыслу
// самого флага. Use-case на этом маркере пропускает проверку явно.
type NoopSubnetClient struct{}

// GetSubnet всегда возвращает маркер «проверка не выполнялась».
func (NoopSubnetClient) GetSubnet(_ context.Context, _ string) (*ports.SubnetPlacement, error) {
	return nil, ports.ErrPeerValidationSkipped
}

// ensure compile-time: both impls satisfy the use-case port.
var (
	_ ports.SubnetRegistry = (*VPCSubnetClient)(nil)
	_ ports.SubnetRegistry = NoopSubnetClient{}
)
