// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// stepup_verdict_parity_test.go — SEC-ACR-16 / I8 / O1 (R3/B-4).
//
// The public step-up floor (gateway StepUpGate.Check) and the internal acr-floor
// (iam grpcsrv.ACRSatisfies) read the SAME catalog value but use TWO SEPARATE,
// functionally-identical ranking tables. This lock asserts they produce an
// IDENTICAL pass/deny VERDICT (not merely equal rank numbers) over the full
// {presented} × {required} matrix — so a drift in either wrapper (e.g. `<`→`<=`,
// or the `!=""` guard) is caught. Comparing the two REAL enforcement entrypoints,
// not the rank functions, is the whole point (R3/B-4).

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

func TestStepUp_VerdictParity_GatewayVsIAM(t *testing.T) {
	g := middleware.NewStepUpGate(nil)
	acrValues := []string{"", "0", "1", "2", "3", "weird-unknown"}

	for _, presented := range acrValues {
		for _, required := range acrValues {
			// Gateway entrypoint: non-SA token, MFAMaxAge=0 so Check reduces to the
			// pure acr-guard being compared.
			tok := &middleware.VerifiedToken{ACR: presented}
			gwPass := g.Check(tok, middleware.PermissionRequirement{RequiredACRMin: required}) == nil

			// iam entrypoint.
			iamPass := grpcsrv.ACRSatisfies(presented, required)

			assert.Equalf(t, iamPass, gwPass,
				"verdict drift: presented=%q required=%q — gateway pass=%v, iam pass=%v",
				presented, required, gwPass, iamPass)
		}
	}
}
