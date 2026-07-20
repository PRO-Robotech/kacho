// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
)

// TestValidateID_TextAndCharset — malformed id → конвенционный текст
// "invalid <res> id '<X>'" (GEO-1-19/31); slug-charset энфорсится.
func TestValidateID_TextAndCharset(t *testing.T) {
	valid := []string{"region-1", "ru-central1", "ru-central1-a", "r", "region-10-abc"}
	for _, v := range valid {
		if err := domain.ValidateID("zone", v); err != nil {
			t.Fatalf("ValidateID(%q) = %v, want nil", v, err)
		}
	}
	invalid := []string{"", "ZZ!", "Region-1", "region 1", "region_1", "-region", "region-", "region--1", "1region"}
	for _, v := range invalid {
		if err := domain.ValidateID("zone", v); err == nil {
			t.Fatalf("ValidateID(%q) = nil, want error", v)
		}
	}
	// Точный текст (часть контракта api-conventions.md).
	if got := domain.ValidateID("region", "ZZ!").Error(); got != "invalid region id 'ZZ!'" {
		t.Fatalf("region malformed text = %q, want \"invalid region id 'ZZ!'\"", got)
	}
	if got := domain.ValidateID("zone", "ZZ!").Error(); got != "invalid zone id 'ZZ!'" {
		t.Fatalf("zone malformed text = %q, want \"invalid zone id 'ZZ!'\"", got)
	}
	// длина: 63 ок, 64 — нет.
	if err := domain.ValidateID("region", "r"+strings.Repeat("a", 62)); err != nil {
		t.Fatalf("ValidateID(63 chars) = %v, want nil", err)
	}
	if err := domain.ValidateID("region", "r"+strings.Repeat("a", 63)); err == nil {
		t.Fatalf("ValidateID(64 chars) = nil, want error")
	}
}

// TestValidateZoneCoupling_StrictStartsWith — coupling zone.id == regionId+"-"+suffix
// строгий (GEO-1-29/30): контрпример "ru-central10-a" под "ru-central1" → REJECT.
func TestValidateZoneCoupling_StrictStartsWith(t *testing.T) {
	if err := domain.ValidateZoneCoupling("ru-central1-a", "ru-central1"); err != nil {
		t.Fatalf("valid coupling rejected: %v", err)
	}
	// GEO-1-29: mismatch text.
	err := domain.ValidateZoneCoupling("ru-central1-a", "eu-west1")
	if err == nil {
		t.Fatal("coupling mismatch not rejected")
	}
	if want := "zone id 'ru-central1-a' must be prefixed by its regionId 'eu-west1'"; err.Error() != want {
		t.Fatalf("coupling text = %q, want %q", err.Error(), want)
	}
	// GEO-1-30: строгий startsWith — "ru-central10-a" НЕ префиксован "ru-central1-"
	// (следующий символ '0', не '-') ⇒ REJECT (иначе ложный bare-startsWith).
	if err := domain.ValidateZoneCoupling("ru-central10-a", "ru-central1"); err == nil {
		t.Fatal("strict startsWith failed: 'ru-central10-a' wrongly accepted under 'ru-central1'")
	}
}

// TestValidateCountryCode — ISO-3166 alpha-2 (GEO-1-39); пустой — OK.
func TestValidateCountryCode(t *testing.T) {
	for _, ok := range []string{"", "RU", "NL", "US"} {
		if err := domain.ValidateCountryCode(ok); err != nil {
			t.Fatalf("ValidateCountryCode(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"ru", "R1", "RUS", "R", "1", "рф"} {
		err := domain.ValidateCountryCode(bad)
		if err == nil {
			t.Fatalf("ValidateCountryCode(%q) = nil, want error", bad)
		}
		if want := "countryCode must be an ISO-3166 alpha-2 code"; err.Error() != want {
			t.Fatalf("countryCode text = %q, want %q", err.Error(), want)
		}
	}
}

// TestOpenForPlacement_Derivation — GEO-1-06/07/08/09: derived openForPlacement° и
// placementBlockedReason° по всем 4 состояниям zone×region.
func TestOpenForPlacement_Derivation(t *testing.T) {
	up, down := domain.GeoStatusUp, domain.GeoStatusDown
	// Region.
	if !(domain.Region{Status: up}).OpenForPlacement() {
		t.Fatal("region UP must be openForPlacement")
	}
	if (domain.Region{Status: down}).OpenForPlacement() {
		t.Fatal("region DOWN must NOT be openForPlacement (GEO-1-09)")
	}
	// Zone matrix.
	cases := []struct {
		name       string
		zoneS      domain.GeoStatus
		regionS    domain.GeoStatus
		wantOpen   bool
		wantReason domain.PlacementBlockedReason
	}{
		{"both up", up, up, true, domain.PlacementBlockedReasonNone},                      // GEO-1-06
		{"zone up region down", up, down, false, domain.PlacementBlockedReasonRegionDown}, // GEO-1-07
		{"zone down region up", down, up, false, domain.PlacementBlockedReasonZoneDown},   // GEO-1-08
		{"both down → zone precedence", down, down, false, domain.PlacementBlockedReasonZoneDown},
	}
	for _, c := range cases {
		z := domain.Zone{Status: c.zoneS, RegionStatus: c.regionS}
		if z.OpenForPlacement() != c.wantOpen {
			t.Errorf("%s: openForPlacement = %v, want %v", c.name, z.OpenForPlacement(), c.wantOpen)
		}
		if z.PlacementBlockedReason() != c.wantReason {
			t.Errorf("%s: reason = %v, want %v", c.name, z.PlacementBlockedReason(), c.wantReason)
		}
	}
}

// TestGeoStatusValues — parity с нумерацией proto-enum geo.v1.GeoStatus.
func TestGeoStatusValues(t *testing.T) {
	if domain.GeoStatusUnspecified != 0 || domain.GeoStatusUp != 1 || domain.GeoStatusDown != 2 {
		t.Fatalf("GeoStatus enum values drifted from proto (UNSPECIFIED=0, UP=1, DOWN=2)")
	}
	if err := domain.GeoStatus(99).Validate(); err == nil {
		t.Fatal("out-of-range status must be rejected")
	}
}

// TestCapacityUnavailableMessage_Anonymized — GEO-1-05 (ungated): канонический
// public capacity-текст обезличен — НЕ содержит host-class (two-projection
// security-инвариант; regression-lock NotContains).
func TestCapacityUnavailableMessage_Anonymized(t *testing.T) {
	msg := domain.CapacityUnavailableMessage("ru-central1-a")
	want := "zone ru-central1-a has insufficient capacity for the requested configuration"
	if msg != want {
		t.Fatalf("capacity text = %q, want %q", msg, want)
	}
	for _, hostClass := range []string{"std-v3", "gpu-a1", "mem-v2"} {
		if strings.Contains(msg, hostClass) {
			t.Fatalf("public capacity error leaked host-class %q: %q", hostClass, msg)
		}
	}
}
