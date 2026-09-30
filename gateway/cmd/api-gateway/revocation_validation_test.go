// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Startup-validation tests for the revocation-path configuration implemented in
// revocation_validation.go — the ENVIRONMENT half of the guard.
//
// The property under test: a production-class gateway must not boot with the
// revocation path aimed at nothing, and only the explicit dev-class labels may
// relax that. What the guard demands per posture — and the transport rules for
// our revocation authority — are pinned in own_lane_revocation_authority_test.go
// and identity_lane_validation_test.go.
//
// The previous provider's axis is gone (#2734): cases that asked for its
// introspection and admin addresses were retired together with those knobs.
package main

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"
)

// An empty/unset environment label is production-class — a forgotten label must
// not silently downgrade the guard (same rule as the sibling authz guard).
func TestUnlabelledEnvIsProductionClass(t *testing.T) {
	err := validateProductionRevocationConfig("", RevocationConfig{IdentityProvider: identityposture.Own})
	if err == nil {
		t.Fatalf("an unset KACHO_APP_ENV must be treated as production-class, got nil")
	}
	if !strings.Contains(err.Error(), platformRevocationURLKnob) {
		t.Fatalf("the refusal must name the unset knob, got: %v", err)
	}
}

// Staging is production-class too.
func TestStagingRefusesAnUnsetAuthority(t *testing.T) {
	if err := validateProductionRevocationConfig("staging", RevocationConfig{IdentityProvider: identityposture.Own}); err == nil {
		t.Fatalf("expected refusal in staging, got nil")
	}
}

// The explicit dev-class labels tolerate an unconfigured revocation path — a
// local stand may run with no reachable authority at all.
func TestDevClassToleratesAnUnsetAuthority(t *testing.T) {
	for _, env := range []string{"dev", "local", "test"} {
		if err := validateProductionRevocationConfig(env, RevocationConfig{IdentityProvider: identityposture.Own}); err != nil {
			t.Fatalf("%s: expected tolerance, got: %v", env, err)
		}
	}
}

// The sanctioned production shape — our authority over TLS, anchored, with the
// edge's identity to present — boots.
//
// This is the other half of the guard: it must stay SILENT on a configuration
// that is actually correct. A guard that only ever fires is indistinguishable
// from one that fires at random, and the first false refusal gets it removed.
func TestProdAcceptsTheSanctionedShape(t *testing.T) {
	if err := validateProductionRevocationConfig("production", ownLane()); err != nil {
		t.Fatalf("expected nil for a fully configured production revocation path, got: %v", err)
	}
}

// A malformed address is refused — an operator who set the knob to a hostname
// without a scheme gets told at startup, not on the first request.
func TestProdRefusesAnUnparseableAuthorityURL(t *testing.T) {
	cfg := ownLane()
	cfg.PlatformRevocationURL = "kaname-internal:9097/internal/tokens/introspect"
	err := validateProductionRevocationConfig("production", cfg)
	if err == nil {
		t.Fatalf("expected refusal for a schemeless authority address, got nil")
	}
	if !strings.Contains(err.Error(), platformRevocationURLKnob) {
		t.Fatalf("the refusal must name the knob, got: %v", err)
	}
}
