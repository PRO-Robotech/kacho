// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// NLB-1b EXPAND (additive): AdminState newtype (LB adminState LIVE-mutable field).
func TestAdminState_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		value   domain.AdminState
		wantErr bool
	}{
		{"ENABLED ok", domain.AdminStateEnabled, false},
		{"DISABLED ok", domain.AdminStateDisabled, false},
		{"empty (unset) ok", "", false},
		{"unknown rejected", "PAUSED", true},
		{"lowercase rejected", "enabled", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.value.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("AdminState(%q).Validate() err=%v wantErr=%v", tc.value, err, tc.wantErr)
			}
		})
	}
}

// NLB-1b EXPAND (additive): Placement newtype (merged EXTERNAL_REGIONAL |
// INTERNAL_REGIONAL | INTERNAL_ZONAL). In EXPAND it is derived-consistent with the
// legacy type/placement_type inputs (authority switch — NLB-1c/MIGRATE).
func TestPlacement_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		value   domain.Placement
		wantErr bool
	}{
		{"EXTERNAL_REGIONAL ok", domain.PlacementExternalRegional, false},
		{"INTERNAL_REGIONAL ok", domain.PlacementInternalRegional, false},
		{"INTERNAL_ZONAL ok", domain.PlacementInternalZonal, false},
		{"empty (unset) ok", "", false},
		{"EXTERNAL_ZONAL inexpressible/rejected", "EXTERNAL_ZONAL", true},
		{"unknown rejected", "PUBLIC", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.value.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Placement(%q).Validate() err=%v wantErr=%v", tc.value, err, tc.wantErr)
			}
		})
	}
}

// PlacementFromTypeAndPlacementType — canonical derivation used by both the Create
// use-case (persist a consistent placement) and type2pb (echo placement° from the
// legacy columns when the placement column is empty — compat).
func TestPlacementFromTypeAndPlacementType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		typ  domain.LBType
		pt   domain.PlacementType
		want domain.Placement
	}{
		{"EXTERNAL → EXTERNAL_REGIONAL (anycast)", domain.LBTypeExternal, domain.PlacementUnspecified, domain.PlacementExternalRegional},
		{"INTERNAL+REGIONAL → INTERNAL_REGIONAL", domain.LBTypeInternal, domain.PlacementRegional, domain.PlacementInternalRegional},
		{"INTERNAL+ZONAL → INTERNAL_ZONAL", domain.LBTypeInternal, domain.PlacementZonal, domain.PlacementInternalZonal},
		{"INTERNAL+unspecified → unspecified (not yet placed)", domain.LBTypeInternal, domain.PlacementUnspecified, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.PlacementFromTypeAndPlacementType(tc.typ, tc.pt); got != tc.want {
				t.Fatalf("PlacementFromTypeAndPlacementType(%q,%q)=%q want %q", tc.typ, tc.pt, got, tc.want)
			}
		})
	}
}
