// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// validateSecurityGroups — NLB-1b MIGRATE (F2/NLB-1-51/52) peer-validate of
// NetworkLoadBalancer.security_group_ids. Rules:
//   - INTERNAL-only: SGs are network-scoped → set is illegal on non-INTERNAL LBs
//     (mirrors the DB CHECK load_balancers_sg_internal_check with a clean error).
//   - same-project existence: each SG resolved via vpc.SecurityGroupService.Get
//     under tenant identity; absent / not-accessible / cross-project → FAILED_PRECONDITION
//     (anti-oracle — no reveal that a cross-project SG exists); vpc unavailable →
//     UNAVAILABLE (fail-closed mutation).
//   - NO region-coherence (SGs carry no zone/region).
//
// nil sgClient → peer-validate skipped (dev/unwired); the DB CHECK remains the
// backstop for the INTERNAL invariant.
//
// Cardinality is bounded BEFORE this function runs: the set is deduplicated on
// intake (normalizeSecurityGroupIDs) and capped by domain.LoadBalancer.Validate
// at MaxSecurityGroupsPerLB, so the loop below is ≤50 peer round-trips — it must
// never be reached with an unbounded caller-supplied list (each element costs a
// full 5s-deadline gRPC call on the synchronous handler goroutine).
func validateSecurityGroups(
	ctx context.Context, sgc SecurityGroupClient, lbType domain.LBType, projectID string, sgIDs []string,
) error {
	if len(sgIDs) == 0 {
		return nil
	}
	if lbType != domain.LBTypeInternal {
		return status.Error(codes.InvalidArgument, "security_group_ids is only valid for INTERNAL load balancer")
	}
	if sgc == nil {
		return nil
	}
	for _, id := range sgIDs {
		sg, err := sgc.Get(ctx, id)
		if err != nil {
			return mapDomainErr(err)
		}
		if sg.ProjectID != projectID {
			// cross-project → same generic precondition failure (anti-oracle).
			return status.Errorf(codes.FailedPrecondition, "SecurityGroup %s not found", id)
		}
	}
	return nil
}

// normalizeSecurityGroupIDs — dedup + отбрасывание пустых + стабильный
// (first-seen) порядок набора SG-ссылок. Ровно тот же приём, что
// `normalizeZones` для disabled_announce_zones: security_group_ids —
// МНОЖЕСТВО ссылок, дубликат не несёт семантики, но стоит полного
// vpc.SecurityGroupService.Get на request-path.
//
// Нормализация делается на intake (Create / applyUpdateMask), а не внутри
// цикла peer-validate, чтобы дедуп попал ОДИНАКОВО во все три места: фазу
// peer-validate, персист (text[] в БД) и `LoadBalancer.Equal` (noop-detection
// на Update) — иначе `[a,a]` и `[a]` считались бы разными состояниями.
func normalizeSecurityGroupIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
