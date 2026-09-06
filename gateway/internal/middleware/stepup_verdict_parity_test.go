// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// stepup_verdict_parity_test.go — SEC-ACR-16 / I8 / O1 (R3/B-4), PUBLIC arm.
//
// The step-up floor is enforced at two points — the public gateway
// (StepUpGate.Check) and the iam cluster-internal listener
// (authzguard.ACRFloor). Since the consolidation they share ONE implementation
// (grpcsrv.EvaluateStepUp) and neither may re-derive any arm of it.
//
// This guard drives the REAL public entrypoint against that shared rule over the
// FULL input space — principal type × presented acr × required acr × freshness
// window × auth_time — and asserts an identical verdict, deny REASON included.
// The iam arm carries the mirror guard
// (services/iam/internal/authzguard/acr_floor_stepup_parity_test.go); both being
// anchored to the same rule is what makes the two halves agree transitively, and
// either half growing a local override goes red here or there.
//
// WHY THE PRINCIPAL-TYPE AXIS IS THE POINT. The previous version of this guard
// built a NON-MACHINE token only and compared the two ranking tables. Ranking
// was the one axis on which the halves already agreed, so the guard stayed green
// while they disagreed on the axis that mattered: the gateway exempted machine
// principals and the iam floor did not, so a service account cleared the front
// door and was then denied — permanently — on the acr-gated credential/grant
// RPCs. A guard that never builds a machine token cannot see that. Machine rows
// are now first-class here (and pinned explicitly below), so dropping the
// exemption from EITHER half fails a test instead of shipping.

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// evalInstant — the fixed clock for every parity case (freshness must be decided
// by the rule, never by wall-clock drift between the two evaluations).
var evalInstant = time.Unix(1_700_000_000, 0)

// gatewayVerdict maps the public entrypoint's error back onto the shared verdict
// vocabulary, so the comparison is verdict-vs-verdict (deny REASON included) and
// not merely pass-vs-fail.
func gatewayVerdict(t *testing.T, err error, authTimeMissing bool) grpcsrv.StepUpVerdict {
	t.Helper()
	switch {
	case err == nil:
		return grpcsrv.StepUpAllow
	case errors.Is(err, middleware.ErrStepUpRequired):
		return grpcsrv.StepUpDenyACR
	case errors.Is(err, middleware.ErrMFAStale):
		if authTimeMissing {
			return grpcsrv.StepUpDenyAuthTimeMissing
		}
		return grpcsrv.StepUpDenyMFAStale
	default:
		t.Fatalf("unexpected error from StepUpGate.Check: %v", err)
		return grpcsrv.StepUpAllow
	}
}

func verdictName(v grpcsrv.StepUpVerdict) string {
	switch v {
	case grpcsrv.StepUpAllow:
		return "ALLOW"
	case grpcsrv.StepUpDenyACR:
		return "DENY_ACR"
	case grpcsrv.StepUpDenyAuthTimeMissing:
		return "DENY_AUTH_TIME_MISSING"
	case grpcsrv.StepUpDenyMFAStale:
		return "DENY_MFA_STALE"
	default:
		return fmt.Sprintf("verdict(%d)", v)
	}
}

// TestStepUp_VerdictParity_GatewayVsSharedRule — the public entrypoint must
// return exactly the shared rule's verdict for every combination of inputs,
// INCLUDING every machine-principal row.
func TestStepUp_VerdictParity_GatewayVsSharedRule(t *testing.T) {
	g := middleware.NewStepUpGate(func() time.Time { return evalInstant })

	principalTypes := []string{
		"",                 // absent — never exempt (fail-closed)
		"user",             // human — never exempt
		"system",           // system fallback principal — never exempt
		"service_account",  // MACHINE — the contested branch
		"Service_Account",  // case variant — must NOT exempt (exact match only)
		"service_account ", // trailing space — must NOT exempt
		"serviceaccount",   // near-miss — must NOT exempt
	}
	acrValues := []string{"", "0", "1", "2", "3", "weird-unknown"}
	maxAges := []time.Duration{0, time.Hour}
	authTimes := map[string]time.Time{
		"missing": {},
		"fresh":   evalInstant.Add(-time.Minute),
		"stale":   evalInstant.Add(-24 * time.Hour),
	}

	machineRowsSeen, machineRowsAllowed := 0, 0

	for _, pType := range principalTypes {
		for _, presented := range acrValues {
			for _, required := range acrValues {
				for _, maxAge := range maxAges {
					for atName, authTime := range authTimes {
						// Alternate the claim placement so BOTH supported carriers
						// (top-level promotion and nested ext_claims) are exercised on the
						// real entrypoint — the rule must not depend on placement.
						tok := &middleware.VerifiedToken{ACR: presented, AuthTime: authTime}
						if pType != "" {
							if len(presented)%2 == 0 {
								tok.Claims = map[string]any{"kaname_principal_type": pType}
							} else {
								tok.ExtClaims = map[string]any{"kaname_principal_type": pType}
							}
						}
						req := middleware.PermissionRequirement{RequiredACRMin: required, MFAMaxAge: maxAge}

						want := grpcsrv.EvaluateStepUp(grpcsrv.StepUpInput{
							PrincipalType: pType,
							PresentedACR:  presented,
							AuthTime:      authTime,
							RequiredACR:   required,
							MFAMaxAge:     maxAge,
							Now:           evalInstant,
						})
						got := gatewayVerdict(t, g.Check(tok, req), authTime.IsZero())

						assert.Equalf(t, want, got,
							"verdict drift: principal_type=%q presented=%q required=%q max_age=%s auth_time=%s — shared rule says %s, gateway says %s",
							pType, presented, required, maxAge, atName, verdictName(want), verdictName(got))

						if pType == grpcsrv.PrincipalTypeServiceAccount {
							machineRowsSeen++
							if got == grpcsrv.StepUpAllow {
								machineRowsAllowed++
							}
						}
					}
				}
			}
		}
	}

	// The guard is only meaningful if it actually walked the contested branch:
	// every machine row must have been evaluated AND allowed. Without this, a
	// refactor that stops producing machine tokens would quietly return the guard
	// to the vacuous state it was in before.
	require.Positive(t, machineRowsSeen, "the matrix must contain machine-principal rows")
	require.Equal(t, machineRowsSeen, machineRowsAllowed,
		"every machine-principal row must be ALLOWED by the public gate (the exemption branch must be live)")
}

// TestStepUp_VerdictParity_MachineBranch_Pinned — the contested branch pinned
// explicitly, independent of the matrix bookkeeping above.
//
// A machine principal presenting NO acr and NO auth_time must clear the highest
// floor with a freshness window attached. If either half stops honouring the
// exemption, this is the row that goes red.
func TestStepUp_VerdictParity_MachineBranch_Pinned(t *testing.T) {
	g := middleware.NewStepUpGate(func() time.Time { return evalInstant })

	req := middleware.PermissionRequirement{RequiredACRMin: "3", MFAMaxAge: time.Hour}
	machine := &middleware.VerifiedToken{
		ACR:       "", // a machine has no interactive ceremony — ranks 0
		ExtClaims: map[string]any{"kaname_principal_type": grpcsrv.PrincipalTypeServiceAccount},
	}

	assert.Equal(t, grpcsrv.StepUpAllow,
		grpcsrv.EvaluateStepUp(grpcsrv.StepUpInput{
			PrincipalType: grpcsrv.PrincipalTypeServiceAccount,
			PresentedACR:  "",
			RequiredACR:   "3",
			MFAMaxAge:     time.Hour,
			Now:           evalInstant,
		}),
		"shared rule must exempt the machine principal")
	assert.NoError(t, g.Check(machine, req),
		"public gate must return the shared rule's ALLOW for a machine principal")

	// MECHANISM-LOCK — the exemption must not widen. The same shape as a human is
	// still challenged, on both the rule and the real entrypoint.
	human := &middleware.VerifiedToken{
		ACR:       "",
		ExtClaims: map[string]any{"kaname_principal_type": "user"},
	}
	assert.Equal(t, grpcsrv.StepUpDenyACR,
		grpcsrv.EvaluateStepUp(grpcsrv.StepUpInput{
			PrincipalType: "user",
			PresentedACR:  "",
			RequiredACR:   "3",
			MFAMaxAge:     time.Hour,
			Now:           evalInstant,
		}))
	assert.ErrorIs(t, g.Check(human, req), middleware.ErrStepUpRequired,
		"a human below the floor must still be challenged")
}
