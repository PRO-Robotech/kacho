// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// AddTargetsUseCase — добавляет targets в TG.
//
// Sync (handler-thread):
//   - target_group_id required, len(targets) >= 1;
//   - per-target domain.Target.Validate (4-way oneof, weight bounds,
//     external_ip bogon-check).
//
// Async worker:
//   - TG.Get + status guard (DELETING → FailedPrecondition);
//   - per-target peer-validate:
//   - instance: compute.InstanceService.Get + region match (006);
//   - nic: vpc.NetworkInterfaceService.Get + region match via subnet;
//   - ip_ref: vpc.SubnetService.Get + region match + IP-in-CIDR (008);
//   - external_ip: bogon-check уже в Validate, peer-validate нет;
//   - Writer-TX → AddTargets (reactivate same-identity DRAINING row via CAS,
//     иначе INSERT ON CONFLICT DO NOTHING per partial UNIQUE) + outbox UPDATED
//     только если >0 строк вставлено/реактивировано (idempotent no-op) → Commit.
type AddTargetsUseCase struct {
	repo           Repo
	opsRepo        OpsRepo
	instanceClient InstanceClient
	nicClient      NetworkInterfaceClient
	subnetClient   SubnetClient
	zoneRegion     ZoneRegionClient
	logger         *slog.Logger
}

// NewAddTargetsUseCase конструктор. `zoneRegion` — авторитетный zone→region
// резолвер (geo, владелец Geography); обязателен для instance-таргетов, у которых
// region есть только у зоны. nil → region-coherence неверифицируема → мутация
// fail-closed UNAVAILABLE (регион НИКОГДА не выводится из имени зоны).
func NewAddTargetsUseCase(
	repo Repo, opsRepo OpsRepo,
	inst InstanceClient, nic NetworkInterfaceClient, sub SubnetClient,
	zoneRegion ZoneRegionClient,
	logger *slog.Logger,
) *AddTargetsUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &AddTargetsUseCase{
		repo: repo, opsRepo: opsRepo,
		instanceClient: inst, nicClient: nic, subnetClient: sub,
		zoneRegion: zoneRegion,
		logger:     logger,
	}
}

// Execute — sync validate + ops insert + spawn worker.
func (u *AddTargetsUseCase) Execute(
	ctx context.Context, req *lbv1.AddTargetsRequest,
) (*operations.Operation, error) {
	tgID := req.GetTargetGroupId()
	if tgID == "" {
		return nil, errInvalidArg("target_group_id", "required")
	}
	if err := validateTargetGroupID(tgID); err != nil {
		return nil, err
	}
	if len(req.GetTargets()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one target is required")
	}
	if len(req.GetTargets()) > domain.MaxTargetsPerGroup {
		return nil, status.Errorf(codes.InvalidArgument,
			"too many targets in a single AddTargets call (max %d)", domain.MaxTargetsPerGroup)
	}

	targets := targetsFromPb(req.GetTargets())
	for i := range targets {
		if err := targets[i].Validate(); err != nil {
			return nil, mapDomainErr(err)
		}
	}

	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("AddTargets to TargetGroup %s (n=%d)", tgID, len(targets)),
		&lbv1.AddTargetsMetadata{TargetGroupId: tgID},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doAdd(workerCtx, tgID, targets)
	})
	return &op, nil
}

// doAdd — async worker. См. описание use-case'а.
func (u *AddTargetsUseCase) doAdd(ctx context.Context, tgID string, targets []domain.Target) (*anypb.Any, error) {
	// 1. TG.Get + status guard.
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	tg, err := rd.TargetGroups().Get(ctx, tgID)
	_ = rd.Close()
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if tg.Status == domain.TargetGroupStatusDeleting {
		return nil, status.Error(codes.FailedPrecondition, "target group is being deleted")
	}

	// 2. Per-target peer-validate.
	for i := range targets {
		if err := u.validateTargetPeer(ctx, tg.RegionID, i, &targets[i]); err != nil {
			return nil, err
		}
	}

	// 3. Writer-TX → AddTargets + (conditional) outbox UPDATED → Commit.
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	defer w.Abort()

	inserted, err := w.TargetGroups().AddTargets(ctx, tgID, targets)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if inserted > 0 {
		if err := w.Outbox().Emit(ctx,
			kachorepo.OutboxResourceTargetGroup, tgID, string(tg.ProjectID),
			kachorepo.OutboxActionUpdated, map[string]any{
				"id":             tgID,
				"project_id":     string(tg.ProjectID),
				"region_id":      string(tg.RegionID),
				"trigger":        "add_targets",
				"inserted_count": inserted,
			},
		); err != nil {
			return nil, mapDomainErr(err)
		}
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}

	// Re-read TG (с inline targets) для response.
	rd2, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	updated, err := rd2.TargetGroups().Get(ctx, tgID)
	_ = rd2.Close()
	if err != nil {
		return nil, mapDomainErr(err)
	}
	return marshalTargetGroup(updated)
}

// validateTargetPeer — per-target peer-validate. idx — индекс target'а в request
// массиве (для с фиксированным текстом error text `"target[N]..."`). Errors → gRPC InvalidArgument.
func (u *AddTargetsUseCase) validateTargetPeer(
	ctx context.Context, tgRegion domain.RegionID, idx int, t *domain.Target,
) error {
	switch {
	case isInstanceTarget(t):
		return u.validateInstanceTarget(ctx, tgRegion, idx, t)
	case isNicTarget(t):
		return u.validateNicTarget(ctx, tgRegion, idx, t)
	case t.IPRef != nil:
		return u.validateIPRefTarget(ctx, tgRegion, idx, t)
	case t.ExternalIP != nil:
		// bogon-check уже в domain.Target.Validate; peer-validate нет.
		return nil
	}
	return nil
}

func (u *AddTargetsUseCase) validateInstanceTarget(
	ctx context.Context, tgRegion domain.RegionID, idx int, t *domain.Target,
) error {
	instID, _ := t.InstanceID.Maybe()
	if u.instanceClient == nil {
		return status.Error(codes.Unavailable, "compute instance client not configured")
	}
	inst, err := u.instanceClient.Get(ctx, string(instID))
	if err != nil {
		return mapPeerTargetErr(idx, "instance_id", string(instID), err)
	}
	// Instance — всегда ZONAL; его регион знает ТОЛЬКО владелец Geography.
	r, err := u.regionOfZone(ctx, inst.ZoneID)
	if err != nil {
		return regionUnverifiableErr(idx, "instance_id", string(instID))
	}
	if r != string(tgRegion) {
		return status.Errorf(codes.InvalidArgument,
			"target[%d].instance_id '%s' region '%s' does not match target_group region '%s'",
			idx, instID, r, tgRegion)
	}
	return nil
}

// regionOfZone — авторитетный zone→region резолв через geo (владелец Geography).
// Регион НИКОГДА не выводится из имени зоны: имена региона и зоны — произвольные
// строки, между ними нет выводимой связи (data-integrity.md §Placement-coherence
// «валидировать peer-вызовом geo.v1.ZoneService.Get, не локально»). Пустая зона /
// нет резолвера / geo недоступен → ошибка (fail-closed на мутации).
func (u *AddTargetsUseCase) regionOfZone(ctx context.Context, zoneID string) (string, error) {
	if u.zoneRegion == nil {
		return "", fmt.Errorf("%w: zone→region resolver not configured", domain.ErrUnavailable)
	}
	if zoneID == "" {
		return "", fmt.Errorf("%w: peer resource carries no zone", domain.ErrUnavailable)
	}
	r, err := u.zoneRegion.RegionOfZone(ctx, zoneID)
	if err != nil {
		return "", err
	}
	if r == "" {
		return "", fmt.Errorf("%w: zone→region resolved empty", domain.ErrUnavailable)
	}
	return r, nil
}

// subnetRegion — авторитетный регион подсети: `vpc.Subnet.region_id` (REGIONAL
// несёт его напрямую; ZONAL — zone→region резолв через geo внутри adapter'а).
// Пустое зеркало = регион неизвестен → coherence неверифицируема, fail-closed.
func subnetRegion(sn *vpcclient.Subnet) (string, bool) {
	if sn == nil || sn.RegionID == "" {
		return "", false
	}
	return sn.RegionID, true
}

// regionUnverifiableErr — fail-closed на мутации, когда регион peer-ресурса
// неустановим (geo недоступен / зеркало не заполнено). Никакой инфра-детали
// наружу — только факт недоступности резолва.
func regionUnverifiableErr(idx int, field, id string) error {
	return status.Errorf(codes.Unavailable,
		"target[%d].%s '%s': region lookup unavailable", idx, field, id)
}

func (u *AddTargetsUseCase) validateNicTarget(
	ctx context.Context, tgRegion domain.RegionID, idx int, t *domain.Target,
) error {
	nicID, _ := t.NicID.Maybe()
	if u.nicClient == nil || u.subnetClient == nil {
		return status.Error(codes.Unavailable, "vpc nic/subnet client not configured")
	}
	nic, err := u.nicClient.Get(ctx, string(nicID))
	if err != nil {
		return mapPeerTargetErr(idx, "nic_id", string(nicID), err)
	}
	// NIC region resolved via parent subnet zone.
	sub, err := u.subnetClient.Get(ctx, nic.SubnetID)
	if err != nil {
		return mapPeerTargetErr(idx, "nic_id", string(nicID), err)
	}
	r, ok := subnetRegion(sub)
	if !ok {
		return regionUnverifiableErr(idx, "nic_id", string(nicID))
	}
	if r != string(tgRegion) {
		return status.Errorf(codes.InvalidArgument,
			"target[%d].nic_id '%s' region '%s' does not match target_group region '%s'",
			idx, nicID, r, tgRegion)
	}
	return nil
}

func (u *AddTargetsUseCase) validateIPRefTarget(
	ctx context.Context, tgRegion domain.RegionID, idx int, t *domain.Target,
) error {
	if u.subnetClient == nil {
		return status.Error(codes.Unavailable, "vpc subnet client not configured")
	}
	subID := string(t.IPRef.SubnetID)
	sub, err := u.subnetClient.Get(ctx, subID)
	if err != nil {
		return mapPeerTargetErr(idx, "ip_ref.subnet_id", subID, err)
	}
	r, ok := subnetRegion(sub)
	if !ok {
		return regionUnverifiableErr(idx, "ip_ref.subnet_id", subID)
	}
	if r != string(tgRegion) {
		return status.Errorf(codes.InvalidArgument,
			"target[%d].ip_ref.subnet_id '%s' region '%s' does not match target_group region '%s'",
			idx, subID, r, tgRegion)
	}
	// IP ∈ CIDR check.
	addr, err := netip.ParseAddr(string(t.IPRef.Address))
	if err != nil {
		return status.Errorf(codes.InvalidArgument,
			"target[%d].ip_ref.address %s is not a valid IP", idx, t.IPRef.Address)
	}
	cidrs := sub.V4CIDRBlocks
	if addr.Is6() {
		cidrs = sub.V6CIDRBlocks
	}
	if !addressInAnyCIDR(addr, cidrs) {
		return status.Errorf(codes.InvalidArgument,
			"target[%d].ip_ref.address %s is not in subnet %s CIDR %s",
			idx, t.IPRef.Address, subID, strings.Join(cidrs, ","))
	}
	return nil
}

// mapPeerTargetErr — peer-error → InvalidArgument с с фиксированным текстом per-target context.
// NotFound от peer → "target[N].<field> '<id>' not found".
// Unavailable → Unavailable. Прочие → InvalidArgument.
func mapPeerTargetErr(idx int, field, id string, err error) error {
	if err == nil {
		return nil
	}
	// Sentinel-strip: peer-clients оборачивают `domain.ErrInvalidArg: <Resource> <id> not found`.
	switch {
	case errIsKind(err, domain.ErrNotFound):
		return status.Errorf(codes.InvalidArgument,
			"target[%d].%s '%s' not found", idx, field, id)
	case errIsKind(err, domain.ErrInvalidArg):
		// compute/vpc peer-clients мапят NotFound → InvalidArgument внутри
		// сентинела — это нормальный путь (см. instance_client.mapInstanceErr).
		return status.Errorf(codes.InvalidArgument,
			"target[%d].%s '%s' not found", idx, field, id)
	case errIsKind(err, domain.ErrFailedPrecondition):
		return status.Errorf(codes.FailedPrecondition,
			"target[%d].%s '%s': %v", idx, field, id, err)
	case errIsKind(err, domain.ErrUnavailable):
		return status.Errorf(codes.Unavailable,
			"target[%d].%s '%s': peer lookup unavailable", idx, field, id)
	}
	return status.Errorf(codes.Internal,
		"target[%d].%s '%s': peer lookup failed", idx, field, id)
}

// errIsKind — errors.Is с nil-guard для удобства switch'а.
func errIsKind(err error, sentinel error) bool {
	if err == nil || sentinel == nil {
		return false
	}
	return errors.Is(err, sentinel)
}

// addressInAnyCIDR — true если addr ∈ хотя бы одного prefix'а из cidrs.
// Невалидный CIDR в slice пропускается без ошибки (тест выше).
func addressInAnyCIDR(addr netip.Addr, cidrs []string) bool {
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			continue
		}
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// isInstanceTarget / isNicTarget — small predicates для switch (option Maybe
// возвращает (val, ok); хотим только ok).
func isInstanceTarget(t *domain.Target) bool {
	_, ok := t.InstanceID.Maybe()
	return ok
}

func isNicTarget(t *domain.Target) bool {
	_, ok := t.NicID.Maybe()
	return ok
}
