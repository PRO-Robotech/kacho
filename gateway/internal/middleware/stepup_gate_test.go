// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// stepup_gate_test.go — step-up (ACR / MFA freshness) gate scenarios.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

func TestStepUp_ACR_RequiresElevation(t *testing.T) {
	g := middleware.NewStepUpGate(nil)
	tok := &middleware.VerifiedToken{ACR: "2"}
	err := g.Check(tok, middleware.PermissionRequirement{RequiredACRMin: "3"})
	assert.ErrorIs(t, err, middleware.ErrStepUpRequired)
}

func TestStepUp_ACR_Satisfied(t *testing.T) {
	g := middleware.NewStepUpGate(nil)
	tok := &middleware.VerifiedToken{ACR: "3"}
	assert.NoError(t, g.Check(tok, middleware.PermissionRequirement{RequiredACRMin: "3"}))
	assert.NoError(t, g.Check(tok, middleware.PermissionRequirement{RequiredACRMin: "2"}))
	assert.NoError(t, g.Check(tok, middleware.PermissionRequirement{RequiredACRMin: "1"}))
}

func TestStepUp_NoRequirement_AlwaysPass(t *testing.T) {
	g := middleware.NewStepUpGate(nil)
	tok := &middleware.VerifiedToken{ACR: ""}
	assert.NoError(t, g.Check(tok, middleware.PermissionRequirement{}))
}

func TestStepUp_MFAStale(t *testing.T) {
	g := middleware.NewStepUpGate(func() time.Time {
		return time.Unix(1_000_000, 0)
	})
	tok := &middleware.VerifiedToken{
		ACR:      "3",
		AuthTime: time.Unix(900_000, 0), // 100k seconds ago
	}
	err := g.Check(tok, middleware.PermissionRequirement{
		RequiredACRMin: "3",
		MFAMaxAge:      1 * time.Hour,
	})
	assert.ErrorIs(t, err, middleware.ErrMFAStale)
}

func TestStepUp_MFAStale_NoAuthTime(t *testing.T) {
	g := middleware.NewStepUpGate(nil)
	tok := &middleware.VerifiedToken{ACR: "3"}
	err := g.Check(tok, middleware.PermissionRequirement{
		RequiredACRMin: "3",
		MFAMaxAge:      1 * time.Hour,
	})
	assert.ErrorIs(t, err, middleware.ErrMFAStale)
}

func TestStepUp_BuildChallenge(t *testing.T) {
	c := middleware.BuildStepUpChallenge(middleware.PermissionRequirement{
		RequiredACRMin: "3",
	}, "2")
	assert.Contains(t, c, `Bearer error="insufficient_user_authentication"`)
	assert.Contains(t, c, `acr_values="3"`)
	assert.Contains(t, c, "Required ACR 3")
	assert.Contains(t, c, "presented ACR 2")
}

func TestStepUp_BuildChallenge_WithMaxAge(t *testing.T) {
	c := middleware.BuildStepUpChallenge(middleware.PermissionRequirement{
		RequiredACRMin: "3",
		MFAMaxAge:      5 * time.Minute,
	}, "")
	assert.Contains(t, c, `max_age="300"`)
}

func TestStepUp_UnknownACRTreatedAsZero(t *testing.T) {
	g := middleware.NewStepUpGate(nil)
	tok := &middleware.VerifiedToken{ACR: "weird-value"}
	err := g.Check(tok, middleware.PermissionRequirement{RequiredACRMin: "1"})
	assert.ErrorIs(t, err, middleware.ErrStepUpRequired)
}

// ── O-1 (#58): service-account acr-exemption (narrow) + mechanism-lock ──────────

// A service-account principal (kacho_principal_type=service_account, acr=0) is
// EXEMPT from the acr step-up floor — parity with the iam :9091 acr-floor
// (security.md §4.1.2). Covers the bootstrap-admin SA calling an acr>=2 RPC.
func TestStepUp_ServiceAccountPrincipal_ExemptFromACRFloor(t *testing.T) {
	g := middleware.NewStepUpGate(nil)
	tok := &middleware.VerifiedToken{
		ACR:       "0",
		ExtClaims: map[string]any{"kacho_principal_type": "service_account"},
	}
	assert.NoError(t, g.Check(tok, middleware.PermissionRequirement{RequiredACRMin: "2"}),
		"service-account principal must be exempt from the acr floor (O-1)")
	// Also exempt from MFA-freshness (a service principal has no auth_time).
	assert.NoError(t, g.Check(tok, middleware.PermissionRequirement{RequiredACRMin: "2", MFAMaxAge: time.Hour}))
}

// Exemption reads the top-level claim too (Hydra allowed_top_level_claims promotion).
func TestStepUp_ServiceAccountPrincipal_TopLevelClaim_Exempt(t *testing.T) {
	g := middleware.NewStepUpGate(nil)
	tok := &middleware.VerifiedToken{
		ACR:    "0",
		Claims: map[string]any{"kacho_principal_type": "service_account"},
	}
	assert.NoError(t, g.Check(tok, middleware.PermissionRequirement{RequiredACRMin: "2"}))
}

// MECHANISM-LOCK: a normal (user) principal with acr < floor STILL gets step-up —
// the SA-exemption must NOT widen into a blanket bypass (O-1 narrow-scoping).
func TestStepUp_UserPrincipal_BelowFloor_StillStepUp(t *testing.T) {
	g := middleware.NewStepUpGate(nil)
	tok := &middleware.VerifiedToken{
		ACR:       "1",
		ExtClaims: map[string]any{"kacho_principal_type": "user"},
	}
	assert.ErrorIs(t, g.Check(tok, middleware.PermissionRequirement{RequiredACRMin: "2"}),
		middleware.ErrStepUpRequired,
		"a user principal below the acr floor must still be challenged (mechanism-lock)")
}

// MECHANISM-LOCK: an ABSENT principal type is NOT exempt (fail-closed — only an
// exact service_account match exempts).
func TestStepUp_NoPrincipalType_BelowFloor_StillStepUp(t *testing.T) {
	g := middleware.NewStepUpGate(nil)
	tok := &middleware.VerifiedToken{ACR: "0"}
	assert.ErrorIs(t, g.Check(tok, middleware.PermissionRequirement{RequiredACRMin: "2"}),
		middleware.ErrStepUpRequired,
		"absent principal type must not be treated as exempt")
}
