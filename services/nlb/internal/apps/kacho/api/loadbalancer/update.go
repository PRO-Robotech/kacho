// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// UpdateLoadBalancerUseCase — UpdateMask discipline + async update.
// Mutable: name / description / labels / deletion_protection / session_affinity /
// disabled_announce_zones (REGIONAL only). Immutable: type / placement_type /
// v4_source / v6_source (→ bound address) / region_id / project_id.
type UpdateLoadBalancerUseCase struct {
	repo       Repo
	opsRepo    operations.Repo
	zoneClient ZoneClient
	// sgClient — NLB-1b MIGRATE peer-validate of security_group_ids on Update. nil →
	// SG validation skipped (DB CHECK backstop). См. WithSecurityGroupClient.
	sgClient SecurityGroupClient
	logger   *slog.Logger
}

// NewUpdateLoadBalancerUseCase конструктор.
func NewUpdateLoadBalancerUseCase(repo Repo, opsRepo operations.Repo, zc ZoneClient, logger *slog.Logger) *UpdateLoadBalancerUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &UpdateLoadBalancerUseCase{repo: repo, opsRepo: opsRepo, zoneClient: zc, logger: logger}
}

// WithSecurityGroupClient wires the vpc SecurityGroup peer-client for Update-time
// security_group_ids peer-validate (same-project existence, fail-closed). nil →
// validation skipped (DB CHECK backstop). Returns self for chaining.
func (u *UpdateLoadBalancerUseCase) WithSecurityGroupClient(c SecurityGroupClient) *UpdateLoadBalancerUseCase {
	u.sgClient = c
	return u
}

// knownUpdateFields — whitelist для update_mask. Поле вне списка → InvalidArgument.
var knownUpdateFields = map[string]bool{
	"name":                    true,
	"description":             true,
	"labels":                  true,
	"deletion_protection":     true,
	"session_affinity":        true,
	"disabled_announce_zones": true,
	// NLB-1b EXPAND (additive): admin_state is LIVE-mutable.
	"admin_state": true,
	// NLB-1b MIGRATE (revival): cross_zone_enabled is LIVE-mutable (REGIONAL-only).
	"cross_zone_enabled": true,
	// NLB-1b MIGRATE (revival): security_group_ids is LIVE-mutable (replace-whole).
	"security_group_ids": true,
}

// immutableUpdateFields — hard-immutable; в mask → InvalidArgument.
var immutableUpdateFields = map[string]string{
	"type": "type is immutable after NetworkLoadBalancer.Create",
	// NLB-1b EXPAND (additive): merged placement is immutable, like type/placement_type.
	"placement":      "placement is immutable after NetworkLoadBalancer.Create",
	"placement_type": "placement_type is immutable after NetworkLoadBalancer.Create",
	"region_id":      "region_id is immutable after NetworkLoadBalancer.Create",
	"project_id":     "project_id is immutable; use NetworkLoadBalancerService.Move",
	"v4_source":      "v4_source is immutable after NetworkLoadBalancer.Create",
	"v6_source":      "v6_source is immutable after NetworkLoadBalancer.Create",
	"v4_address_id":  "v4_address_id is immutable after NetworkLoadBalancer.Create",
	"v6_address_id":  "v6_address_id is immutable after NetworkLoadBalancer.Create",
}

// Execute — sync mask validation + read existing → apply diff → ops insert → worker.
func (u *UpdateLoadBalancerUseCase) Execute(
	ctx context.Context, req *lbv1.UpdateNetworkLoadBalancerRequest,
) (*operations.Operation, error) {
	id := req.GetNetworkLoadBalancerId()
	if id == "" {
		return nil, errInvalidArg("network_load_balancer_id", "required")
	}
	if err := validateLoadBalancerID(id); err != nil {
		return nil, err
	}

	mask := req.GetUpdateMask().GetPaths()
	for _, p := range mask {
		if msg, ok := immutableUpdateFields[p]; ok {
			return nil, status.Errorf(codes.InvalidArgument, "%s", msg)
		}
		if !knownUpdateFields[p] {
			return nil, status.Errorf(codes.InvalidArgument, "unknown update_mask field: %s", p)
		}
	}

	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	cur, err := rd.LoadBalancers().Get(ctx, id)
	_ = rd.Close()
	if err != nil {
		return nil, mapDomainErr(err)
	}

	updated := applyUpdateMask(cur.LoadBalancer, req, mask)
	if err := updated.Validate(); err != nil {
		return nil, mapDomainErr(err)
	}

	// NLB-1b MIGRATE (F3/NLB-1-16): cross_zone_enabled is REGIONAL-only. placement_type
	// is immutable, so guard the merged value against the LB's placement — true on a
	// ZONAL LB → InvalidArgument (verbatim contract tone).
	if updated.CrossZoneEnabled && !domain.CrossZoneApplicable(updated.PlacementType) {
		return nil, status.Error(codes.InvalidArgument, crossZoneZonalMsg)
	}

	// NLB-1b MIGRATE (F2/NLB-1-51/52): peer-validate security_group_ids when the mask
	// touches them (INTERNAL-only + same-project existence via vpc; fail-closed).
	if securityGroupsInMask(mask) {
		if err := validateSecurityGroups(ctx, u.sgClient, updated.Type, string(updated.ProjectID), updated.SecurityGroupIDs); err != nil {
			return nil, err
		}
	}

	// disabled_announce_zones — перевалидируется только когда mask её трогает
	// (REGIONAL-only + зоны ∈ регион + не все зоны, теми же правилами, что Create).
	if disabledAnnounceZonesInMask(mask) {
		if err := checkDisabledAnnounceZones(ctx, u.zoneClient,
			updated.PlacementType, string(updated.RegionID), updated.DisabledAnnounceZones); err != nil {
			return nil, err
		}
	}

	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("Update NetworkLoadBalancer %s", id),
		&lbv1.UpdateNetworkLoadBalancerMetadata{NetworkLoadBalancerId: id},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}

	emitMirror := labelsInMask(mask)
	expectedXmin := cur.Xmin
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doUpdate(workerCtx, updated, expectedXmin, emitMirror)
	})

	return &op, nil
}

// labelsInMask — Update трогает labels: явный "labels" в mask либо пустой mask.
func labelsInMask(mask []string) bool {
	if len(mask) == 0 {
		return true
	}
	for _, p := range mask {
		if p == "labels" {
			return true
		}
	}
	return false
}

// securityGroupsInMask — Update трогает security_group_ids: явный путь в mask либо
// пустой mask (full-object PATCH переприменяет все mutable-поля).
func securityGroupsInMask(mask []string) bool {
	if len(mask) == 0 {
		return true
	}
	for _, p := range mask {
		if p == "security_group_ids" {
			return true
		}
	}
	return false
}

// disabledAnnounceZonesInMask — Update трогает disabled_announce_zones: явный путь
// в mask либо пустой mask (full-object PATCH переприменяет все mutable-поля).
func disabledAnnounceZonesInMask(mask []string) bool {
	if len(mask) == 0 {
		return true
	}
	for _, p := range mask {
		if p == "disabled_announce_zones" {
			return true
		}
	}
	return false
}

// doUpdate — worker: Writer → Update + outbox UPDATED (+ FGA-register при labels) → Commit.
func (u *UpdateLoadBalancerUseCase) doUpdate(ctx context.Context, lb domain.LoadBalancer, expectedXmin string, emitMirror bool) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	defer w.Abort()

	updated, err := w.LoadBalancers().Update(ctx, &lb, expectedXmin)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Outbox().Emit(ctx,
		kachorepo.OutboxResourceLoadBalancer, string(updated.ID), string(updated.ProjectID),
		kachorepo.OutboxActionUpdated, lbOutboxPayload(updated),
	); err != nil {
		return nil, mapDomainErr(err)
	}
	if emitMirror {
		if err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventRegister,
			lbMirrorIntent(updated)); err != nil {
			return nil, mapDomainErr(err)
		}
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}

	pb, err := lbRecordToProto(updated)
	if err != nil {
		return nil, err
	}
	out, err := anypb.New(pb)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	return out, nil
}

// applyUpdateMask — наложить mask на текущий LB. Empty mask → full PATCH:
// mutable полностью перезаписываются из req; immutable silent-ignored.
func applyUpdateMask(
	cur domain.LoadBalancer, req *lbv1.UpdateNetworkLoadBalancerRequest, mask []string,
) domain.LoadBalancer {
	apply := func(field string) bool {
		if len(mask) == 0 {
			return true
		}
		for _, p := range mask {
			if p == field {
				return true
			}
		}
		return false
	}
	out := cur
	if apply("name") {
		out.Name = domain.LbName(req.GetName())
	}
	if apply("description") {
		out.Description = domain.LbDescription(req.GetDescription())
	}
	if apply("labels") {
		out.Labels = domain.LabelsFromMap(req.GetLabels())
	}
	if apply("deletion_protection") {
		out.DeletionProtection = req.GetDeletionProtection()
	}
	if apply("session_affinity") {
		out.SessionAffinity = domainSessionAffinity(req.GetSessionAffinity())
	}
	if apply("disabled_announce_zones") {
		out.DisabledAnnounceZones = normalizeZones(req.GetDisabledAnnounceZones())
	}
	// NLB-1b EXPAND (additive): admin_state LIVE-mutable. Only overwrite when an
	// explicit ENABLED/DISABLED is supplied — UNSPECIFIED (incl. an empty-mask
	// full-PATCH that omits admin_state) preserves the current state, so Update
	// never auto-flips admin_state (NLB-1-14).
	if apply("admin_state") {
		if as := adminStateFromPb(req.GetAdminState()); as != "" {
			out.AdminState = as
		}
	}
	// NLB-1b MIGRATE (revival): cross_zone_enabled LIVE-mutable (REGIONAL-only; the
	// ZONAL-guard runs in Execute after the merge).
	if apply("cross_zone_enabled") {
		out.CrossZoneEnabled = req.GetCrossZoneEnabled()
	}
	// NLB-1b MIGRATE (revival): security_group_ids LIVE-mutable (replace-whole; the
	// peer-validate runs in Execute when the mask touches it).
	if apply("security_group_ids") {
		out.SecurityGroupIDs = req.GetSecurityGroupIds()
	}
	return out
}
