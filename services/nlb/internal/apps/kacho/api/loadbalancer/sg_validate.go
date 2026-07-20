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
