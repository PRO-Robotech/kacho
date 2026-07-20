// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import coreerrors "github.com/PRO-Robotech/kacho/pkg/errors"

// NLB-1b EXPAND (additive) — new LoadBalancer domain newtypes added ALONGSIDE the
// legacy type/placement_type model. They are persisted + echoed but NOT yet
// authoritative: the legacy inputs keep driving behaviour until the MIGRATE phase
// (NLB-1c) flips authority. Nothing here removes or overrides the AS-IS model.

// ---- AdminState ------------------------------------------------------------
// AdminState — desired administrative state of a NetworkLoadBalancer (redesign
// replacement for the :start/:stop power-verbs). LIVE-mutable. In EXPAND it is
// stored + echoed but does not yet gate status recompute (0013 trigger rewrite —
// MIGRATE). Empty string means "unset" and is normalised to ENABLED at persist.

type AdminState string

const (
	AdminStateEnabled  AdminState = "ENABLED"
	AdminStateDisabled AdminState = "DISABLED"
)

// Validate — empty (unset → default ENABLED) or one of ENABLED/DISABLED.
func (a AdminState) Validate() error {
	switch a {
	case "", AdminStateEnabled, AdminStateDisabled:
		return nil
	}
	return coreerrors.InvalidArgument().
		AddFieldViolation("admin_state", "admin_state must be one of: ENABLED, DISABLED").
		Err()
}

// ---- Placement -------------------------------------------------------------
// Placement — merged immutable placement discriminator of a NetworkLoadBalancer
// (redesign fusion of type + placement_type into one input). "external+zonal" is
// inexpressible by construction (no such value). In EXPAND it is derived-consistent
// with the legacy type/placement_type; authority (reject legacy inputs) — MIGRATE.

type Placement string

const (
	PlacementExternalRegional Placement = "EXTERNAL_REGIONAL"
	PlacementInternalRegional Placement = "INTERNAL_REGIONAL"
	PlacementInternalZonal    Placement = "INTERNAL_ZONAL"
)

// Validate — empty (unset) or one of the three legal merged values.
func (p Placement) Validate() error {
	switch p {
	case "", PlacementExternalRegional, PlacementInternalRegional, PlacementInternalZonal:
		return nil
	}
	return coreerrors.InvalidArgument().
		AddFieldViolation("placement",
			"placement must be one of: EXTERNAL_REGIONAL, INTERNAL_REGIONAL, INTERNAL_ZONAL").
		Err()
}

// PlacementFromTypeAndPlacementType — canonical derivation of the merged Placement
// from the legacy (type, placement_type) pair. Single source of truth used by both
// the Create use-case (persist a consistent placement) and type2pb (echo
// placement° from legacy columns when the placement column is empty — compat).
//
//   - EXTERNAL           → EXTERNAL_REGIONAL (external is always regional/anycast).
//   - INTERNAL+REGIONAL  → INTERNAL_REGIONAL.
//   - INTERNAL+ZONAL     → INTERNAL_ZONAL.
//   - anything else (e.g. INTERNAL with no placement_type yet) → "" (unspecified).
func PlacementFromTypeAndPlacementType(t LBType, pt PlacementType) Placement {
	switch {
	case t == LBTypeExternal:
		return PlacementExternalRegional
	case t == LBTypeInternal && pt == PlacementRegional:
		return PlacementInternalRegional
	case t == LBTypeInternal && pt == PlacementZonal:
		return PlacementInternalZonal
	}
	return ""
}

// TypeAndPlacementTypeFromPlacement — inverse of PlacementFromTypeAndPlacementType
// used by the MIGRATE Create/Update flow where `placement` is the AUTHORITATIVE
// input and the legacy (type, placement_type) columns are derived FROM it (not the
// reverse). EXTERNAL is always regional/anycast, so its placement_type is empty.
//
//   - EXTERNAL_REGIONAL → (EXTERNAL, unspecified).
//   - INTERNAL_REGIONAL → (INTERNAL, REGIONAL).
//   - INTERNAL_ZONAL    → (INTERNAL, ZONAL).
//   - "" (unset)        → ("", "").
func TypeAndPlacementTypeFromPlacement(p Placement) (LBType, PlacementType) {
	switch p {
	case PlacementExternalRegional:
		return LBTypeExternal, PlacementUnspecified
	case PlacementInternalRegional:
		return LBTypeInternal, PlacementRegional
	case PlacementInternalZonal:
		return LBTypeInternal, PlacementZonal
	}
	return "", ""
}
