// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// vipAnchor — NLB-1b F5: разобранный VIP-анкер листенера. Ровно один из
// {addressID (BYO), subnetID (auto)} непуст, либо оба пусты (без собственного VIP
// — optional-first fallback на VIP LB). origin — дискриминатор release-ветки.
type vipAnchor struct {
	addressID string           // BYO: линк существующего vpc Address
	subnetID  string           // auto: подсеть для аллокации свежего Address
	origin    domain.VipOrigin // byo | auto
}

// present — есть ли собственный VIP-анкер листенера.
func (a vipAnchor) present() bool { return a.addressID != "" || a.subnetID != "" }

// resolveVIPAnchor — sync-парсинг + peer-validate VIP-анкера. address_id и
// subnet_id взаимоисключающие; foreign vpc id → НЕ nlb-prefix-check (B4).
//   - BYO (address_id): existence проверяется в worker'е (AttachExisting-CAS).
//   - auto (subnet_id): placement/region-coherence peer-validate (NLB-1-32).
//   - оба: InvalidArgument. ни одного: без VIP (fallback на VIP LB).
func (u *CreateUseCase) resolveVIPAnchor(
	ctx context.Context, req *lbv1.CreateListenerRequest, lb domain.LoadBalancer,
) (vipAnchor, error) {
	addressID := req.GetAddressId()
	subnetID := req.GetSubnetId()
	switch {
	case addressID != "" && subnetID != "":
		return vipAnchor{}, status.Error(codes.InvalidArgument,
			"listener VIP anchor: set exactly one of addressId or subnetId")
	case addressID != "":
		return vipAnchor{addressID: addressID, origin: domain.VipOriginBYO}, nil
	case subnetID != "":
		if err := u.validateVIPSubnet(ctx, subnetID, lb); err != nil {
			return vipAnchor{}, err
		}
		return vipAnchor{subnetID: subnetID, origin: domain.VipOriginAuto}, nil
	default:
		// Без собственного VIP-анкера. origin auto (дефолт-дискриминатор), но пустой
		// address_id делает release-ветку Delete no-op.
		return vipAnchor{origin: domain.VipOriginAuto}, nil
	}
}

// validateVIPSubnet — NLB-1-32: cross-service peer-validate VIP-подсети (auto).
// subnet.placement_type обязан совпасть с placement родительского LB, регион
// подсети — с регионом LB (placement-coherence, cross-service — не within-service
// TOCTOU). nil subnetClient → пропуск (минимальный existence через acquire).
func (u *CreateUseCase) validateVIPSubnet(ctx context.Context, subnetID string, lb domain.LoadBalancer) error {
	if u.subnetClient == nil {
		return nil
	}
	if lb.PlacementType == "" {
		// EXTERNAL LB: subnet-VIP неприменим (subnet_id — INTERNAL-only источник).
		return status.Error(codes.InvalidArgument,
			"subnet VIP anchor is only valid for INTERNAL load balancer")
	}
	sn, err := u.subnetClient.Get(ctx, subnetID)
	if err != nil {
		return vipSubnetPeerErr(err, subnetID)
	}
	if !subnetPlacementMatchesLB(sn.PlacementType, lb.PlacementType) {
		return status.Error(codes.FailedPrecondition,
			"VIP subnet placement does not match load balancer placement")
	}
	// Region-coherence: подсеть self-describing region (REGIONAL → region_id;
	// ZONAL → zone→region резолв в adapter'е). Пусто (adapter без zone-resolver'а)
	// → пропуск. Cross-service peer-validate — не within-service TOCTOU.
	if sn.RegionID != "" && sn.RegionID != string(lb.RegionID) {
		return status.Errorf(codes.FailedPrecondition,
			"VIP subnet region %s does not match load balancer region %s", sn.RegionID, lb.RegionID)
	}
	return nil
}

// subnetPlacementMatchesLB — placement-type когерентность VIP-подсети и LB:
// REGIONAL LB ⟹ REGIONAL подсеть; ZONAL LB ⟹ зональная (не REGIONAL/пустая).
func subnetPlacementMatchesLB(subnetPlacement string, lbPlacement domain.PlacementType) bool {
	switch lbPlacement {
	case domain.PlacementRegional:
		return subnetPlacement == vpcclient.SubnetPlacementRegional
	case domain.PlacementZonal:
		return subnetPlacement != vpcclient.SubnetPlacementRegional && subnetPlacement != ""
	}
	return false
}

// vipSubnetPeerErr — peer-validate lane маппинг (api-conventions by-lane split):
// subnet_id — foreign vpc id → miss/inaccessible = FAILED_PRECONDITION (не NotFound —
// это чужой ресурс), владелец недоступен = UNAVAILABLE (fail-closed мутации).
func vipSubnetPeerErr(err error, id string) error {
	switch {
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrInvalidArg):
		return status.Errorf(codes.FailedPrecondition, "VIP subnet %s not found", id)
	case errors.Is(err, domain.ErrUnavailable):
		return status.Error(codes.Unavailable, "subnet lookup unavailable")
	}
	return status.Error(codes.Internal, "subnet lookup failed")
}

// vipAllocation — итог acquire-ветки VIP листенера.
type vipAllocation struct {
	addressID string
	address   string
}

// acquireVIP — внешний side-effect: BYO link (AttachExisting) либо auto-аллокация
// свежего internal Address (AllocateInternalIP[v6]). SetReference/used_by=
// nlb_listener:<id> ставится vpc атомарно. Анти-oracle-семантика ошибок — из
// клиента (link-конфликт → FailedPrecondition; not-found → generic; недоступность
// → Unavailable). nil client → Unavailable (fail-closed).
func (u *CreateUseCase) acquireVIP(ctx context.Context, in createInput) (vipAllocation, error) {
	if u.internalAddrs == nil {
		return vipAllocation{}, status.Error(codes.Unavailable, "vpc internal-address client not configured")
	}
	owner := listenerAddressOwner(string(in.listener.ID), string(in.listener.Name))
	if in.vipAnchor.origin == domain.VipOriginBYO {
		resp, err := u.internalAddrs.AttachExisting(ctx, vpcclient.AttachExistingRequest{
			AddressID: in.vipAnchor.addressID,
			Owner:     owner,
			Owned:     false,
		})
		if err != nil {
			return vipAllocation{}, mapDomainErr(err)
		}
		return vipAllocation{addressID: resp.AddressID, address: resp.Value}, nil
	}
	// auto: аллоцируем свежий internal Address из VIP-подсети (family — из
	// vestigial ip_version листенера, деривнутого из LB).
	req := vpcclient.AllocateInternalIPRequest{
		ProjectID: string(in.listener.ProjectID),
		Name:      domain.ListenerAutoAddressName(in.listener.ID),
		SubnetID:  in.vipAnchor.subnetID,
		Owner:     owner,
	}
	var (
		resp *vpcclient.AllocateResponse
		err  error
	)
	if in.listener.IPVersion == domain.IPVersionV6 {
		resp, err = u.internalAddrs.AllocateInternalIPv6(ctx, req)
	} else {
		resp, err = u.internalAddrs.AllocateInternalIP(ctx, req)
	}
	if err != nil {
		return vipAllocation{}, mapDomainErr(err)
	}
	return vipAllocation{addressID: resp.AddressID, address: resp.Value}, nil
}

// compensateVIP — best-effort worker-компенсация на откате саги Create: byo →
// ClearReference (адрес остаётся у tenant'а), auto → FreeIP (vpc удаляет Address).
// Зеркалит recycle-on-delete (delete.go releaseVIP). Идемпотентно (NotFound → ok).
// Ошибка ЛОГИРУЕТСЯ (CWE-252) — исходную saga-ошибку не маскируем.
func (u *CreateUseCase) compensateVIP(ctx context.Context, addressID string, byo bool) {
	if u.internalAddrs == nil || addressID == "" {
		return
	}
	var err error
	if byo {
		err = u.internalAddrs.ClearReference(ctx, addressID)
	} else {
		err = u.internalAddrs.FreeIP(ctx, addressID)
	}
	if err != nil {
		loggerOrDiscard(u.logger).Warn("listener.Create VIP compensation release failed; lease may leak until reconcile",
			"address_id", addressID, "byo", byo, "err", err)
	}
}
