// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// lbTypeFromPb — proto enum → domain.LBType. UNSPECIFIED → InvalidArgument.
func lbTypeFromPb(t lbv1.NetworkLoadBalancer_Type) (domain.LBType, error) {
	switch t {
	case lbv1.NetworkLoadBalancer_EXTERNAL:
		return domain.LBTypeExternal, nil
	case lbv1.NetworkLoadBalancer_INTERNAL:
		return domain.LBTypeInternal, nil
	}
	return "", errInvalidArg("type", "type must be one of: EXTERNAL, INTERNAL")
}

// domainSessionAffinity — proto enum → domain.SessionAffinity.
func domainSessionAffinity(a lbv1.NetworkLoadBalancer_SessionAffinity) domain.SessionAffinity {
	switch a {
	case lbv1.NetworkLoadBalancer_SESSION_AFFINITY_UNSPECIFIED, lbv1.NetworkLoadBalancer_FIVE_TUPLE:
		return domain.SessionAffinity5Tuple
	case lbv1.NetworkLoadBalancer_CLIENT_IP_ONLY:
		return domain.SessionAffinityClientIPOnly
	}
	return domain.SessionAffinity(a.String())
}

// lbSessionAffinityFromPb — fail-fast вариант: каноничная InvalidArgument на out-of-domain.
func lbSessionAffinityFromPb(a lbv1.NetworkLoadBalancer_SessionAffinity) (domain.SessionAffinity, error) {
	sa := domainSessionAffinity(a)
	if err := sa.Validate(); err != nil {
		return "", err
	}
	return sa, nil
}

// placementModeFromPb — proto enum → domain.Placement (merged). UNSPECIFIED → ""
// (unset); the Create use-case derives the canonical value from type/placement_type
// and, when a non-empty input is supplied, validates it for consistency.
func placementModeFromPb(p lbv1.NetworkLoadBalancer_Placement) domain.Placement {
	switch p {
	case lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL:
		return domain.PlacementExternalRegional
	case lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL:
		return domain.PlacementInternalRegional
	case lbv1.NetworkLoadBalancer_INTERNAL_ZONAL:
		return domain.PlacementInternalZonal
	}
	return ""
}

// adminStateFromPb — proto enum → domain.AdminState. UNSPECIFIED → "" (unset);
// caller keeps the builder default (ENABLED) on Create / current value on Update
// (NLB-1b EXPAND — Update never auto-ENABLE/DISABLE without an explicit value).
func adminStateFromPb(a lbv1.NetworkLoadBalancer_AdminState) domain.AdminState {
	switch a {
	case lbv1.NetworkLoadBalancer_ADMIN_STATE_DISABLED:
		return domain.AdminStateDisabled
	case lbv1.NetworkLoadBalancer_ADMIN_STATE_ENABLED:
		return domain.AdminStateEnabled
	}
	return ""
}
