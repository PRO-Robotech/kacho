// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stepup_enforcement_test.go — every stand must state that it applies the
// per-RPC authentication floor.
//
// The floor lived behind the toggle that mounts proof-of-possession checking,
// and no profile set that toggle. So the floor was declared per RPC, mirrored
// into the identity service, documented as enforced — and applied by nothing, on
// every stand, for its whole life. Nothing about a running stand said so: the
// toggle's absence reads as "sender-constrained tokens are not verified here",
// which is a different and defensible statement.
//
// Like its neighbours this reads the DECLARATIONS rather than a render: the
// contract is what a profile states, it needs no chart dependencies, and it can
// therefore never skip. It merges each stack the way helm does, so an overlay
// that blanks the value is caught rather than hidden behind the base that set it.
package deploy_test

import (
	"strings"
	"testing"
)

// stepUpEnforcementPath — where a profile states that the floor is applied.
var stepUpEnforcementPath = []string{"authn", "enforceStepUp"}

// resolveStackBool reads a boolean gateway value out of a merged stack.
func resolveStackBool(t *testing.T, stack []string, path ...string) (bool, bool) {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	var cur any = merged
	for _, key := range append([]string{"api-gateway"}, path...) {
		m, ok := cur.(map[string]any)
		if !ok {
			return false, false
		}
		if cur, ok = m[key]; !ok {
			return false, false
		}
	}
	b, ok := cur.(bool)
	return b, ok
}

// Every stack that deploys the gateway must state that it applies the floor.
// Silence is not a neutral default here — it is the state in which the whole
// control was inert, and a stand in that state now refuses to start.
func TestStacks_DeclareStepUpEnforcement(t *testing.T) {
	for name, stack := range deployableStacks {
		t.Run(name, func(t *testing.T) {
			got, ok := resolveStackBool(t, stack, stepUpEnforcementPath...)
			if !ok {
				t.Fatalf("%s (%s): api-gateway.authn.enforceStepUp is not declared — the per-RPC "+
					"authentication floor would be stated by the catalog and applied by nothing",
					name, strings.Join(stack, " + "))
			}
			if !got {
				t.Errorf("%s: api-gateway.authn.enforceStepUp is false — the catalog's floor is "+
					"not applied on this stand", name)
			}
		})
	}
}

// A value nothing renders is a decision that never reaches the process. That is
// how the sender-constrained knob spent its whole life documented and unemitted,
// and it is why this is asserted separately from the profiles above.
func TestChart_EmitsStepUpEnforcementEnv(t *testing.T) {
	deployment := readRepoFile(t, "gateway", "deploy", "templates", "deployment.yaml")
	const name = "KACHO_API_GATEWAY_AUTHN_ENFORCE_STEP_UP"
	if !strings.Contains(deployment, "name: "+name) {
		t.Errorf("the api-gateway template no longer emits %s — the values knob would be silently "+
			"inert and the profiles above would assert nothing", name)
	}
}

// The premise of the two cases above: a floor is declared at all. If the catalog
// stopped declaring one, they would keep passing while asserting nothing about a
// control that no longer exists.
func TestCatalog_StillDeclaresAFloor(t *testing.T) {
	catalog := readRepoFile(t, "gateway", "internal", "middleware", "embed", "permission_catalog.json")
	if !strings.Contains(catalog, `"required_acr_min": "2"`) {
		t.Error("the catalog declares no raised floor any more — the enforcement contract asserted " +
			"above has lost its subject; either restore the declaration or retire these guards " +
			"together with the knob")
	}
}
