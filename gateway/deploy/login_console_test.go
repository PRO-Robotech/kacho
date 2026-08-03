// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// login_console_test.go — a production-class stand must resolve the sign-in
// console, because the console is what conducts the ceremony a HUMAN completes
// to end up holding a bearer the edge accepts (IAM-INT-1, S2).
//
// THE UNIT IS THE FOLDED STACK, NOT A SINGLE PROFILE — and that is the whole
// point of this file rather than a detail of it. Two honest idioms live in this
// package and they answer DIFFERENT questions:
//
//   - resolveStackAt / mergeInto / deployableStacks — "what does the RELEASE get",
//     i.e. helm's own left-to-right overlay of every `-f` in the chain;
//   - token_shape_test.go — "does this profile declare its own", which is right
//     for ITS question, because `dev` and `prod` there are alternatives rather
//     than layers, and it says so itself.
//
// Picking the wrong one silently answers the other question. An earlier draft of
// the IAM-INT-1 acceptance did exactly that: it grepped ONE overlay, found the
// console unmentioned, and reported a second independent gap in the platform's
// ability to sign a human in. There was none — the late overlay is merely SILENT
// about the key, and helm's merge is additive, so silence changes nothing. The
// finding was an artefact of the measuring unit.
//
// So this gate folds. And because "the merge is additive" is the premise the
// whole answer rests on, the premise is asserted here too rather than assumed:
// see TestLoginConsole_PremiseLateSilenceDoesNotClearAnEarlierKey.

// consolePath — where the sign-in console declares itself in the umbrella values.
// The sub-chart is `kratos-selfservice-ui`; its own values sit under the nested
// `kratosSelfServiceUI` key.
var consolePath = []string{"kratos-selfservice-ui", "kratosSelfServiceUI", "enabled"}

// gitignoredStackMember — the fourth `-f` of the live fe3455 chain, deliberately
// OUTSIDE the tree: it carries per-cluster Ory secrets. Its absence is therefore
// NOT a finding, and this gate says so out loud instead of quietly reporting
// "3 of 4" as if it were completeness.
const gitignoredStackMember = "values.fe3455-ory.yaml"

// The BOOLEAN leaf is read through the package's existing `resolveStackBoolAt`
// (admin_hop_transport_test.go) rather than a second copy of the same walk. Its
// two return values are what this gate needs and a string resolver could not
// give: `enabled: false` declared and `enabled` never mentioned are OPPOSITE
// findings — "somebody turned the console off" versus "nobody ever wired it" —
// and a gate that collapses them names the wrong cause in its own message.

// TestStacks_ProductionClassResolvesTheLoginConsole — scenario IAM-INT-1-18.
//
// Today this passes by construction, and that is precisely its worth: it pins
// the state an earlier reading declared broken, so the next person to reach the
// same wrong conclusion is contradicted by a gate rather than by an argument.
func TestStacks_ProductionClassResolvesTheLoginConsole(t *testing.T) {
	// SCOPE OF INSPECTION IS AN ASSERTION OF ITS OWN. "No findings" has to be
	// distinguishable from "nothing was read" — a table that quietly emptied, or
	// a production-class predicate that stopped matching anything, would otherwise
	// report exactly the same green as a healthy tree.
	inspected := 0
	for name, stack := range deployableStacks {
		if !stackIsProductionClass(t, stack) {
			t.Logf("%s: dev-class by its own declaration — skipped (%d profile(s))", name, len(stack))
			continue
		}
		inspected++
		t.Logf("%s: production-class, folding %d profile(s): %s",
			name, len(stack), strings.Join(stack, " -> "))

		enabled, declared := resolveStackBoolAt(t, stack, consolePath...)
		switch {
		case !declared:
			t.Errorf("%s: the folded stack never declares %s. A production-class stand with no "+
				"sign-in console cannot conduct the ceremony a human completes to obtain a bearer, "+
				"and every human-caller check on that stand asserts against nobody.",
				name, strings.Join(consolePath, "."))
		case !enabled:
			t.Errorf("%s: the folded stack resolves %s to false. Somebody turned the sign-in "+
				"console OFF — this is a deliberate value, not an omission, so it is refused here "+
				"rather than defaulted.",
				name, strings.Join(consolePath, "."))
		}
	}
	if inspected == 0 {
		t.Fatalf("this gate inspected ZERO production-class stacks out of %d declared. "+
			"Either the stack table emptied or stackIsProductionClass stopped matching — "+
			"whichever it is, the green above means nothing.", len(deployableStacks))
	}
	t.Logf("inspected %d production-class stack(s) of %d declared", inspected, len(deployableStacks))
}

// TestLoginConsole_PremiseLateSilenceDoesNotClearAnEarlierKey — the gate above
// is only meaningful while helm's merge is ADDITIVE, i.e. while a later profile
// that says nothing about a key leaves the earlier value standing. That is the
// exact property an earlier reading of this stand got wrong, so it is asserted
// rather than trusted: if mergeInto is ever changed to a replacing merge, this
// fails and names the reason, instead of the gate above silently starting to
// measure the last profile only.
func TestLoginConsole_PremiseLateSilenceDoesNotClearAnEarlierKey(t *testing.T) {
	base := map[string]any{
		"kratos-selfservice-ui": map[string]any{
			"kratosSelfServiceUI": map[string]any{"enabled": true, "image": "base"},
		},
	}
	// A late overlay that touches a NEIGHBOURING key and says nothing about
	// `enabled` — the shape values.fe3455-prod.yaml actually has.
	late := map[string]any{
		"kratos-selfservice-ui": map[string]any{
			"kratosSelfServiceUI": map[string]any{"image": "late"},
		},
	}
	merged := mergeInto(mergeInto(map[string]any{}, base), late)
	sub, _ := merged["kratos-selfservice-ui"].(map[string]any)
	ui, _ := sub["kratosSelfServiceUI"].(map[string]any)
	if enabled, _ := ui["enabled"].(bool); !enabled {
		t.Fatalf("PREMISE BROKEN: a later profile that never mentions `enabled` cleared it. "+
			"Every stack answer in this file is derived from an additive merge; if the merge "+
			"replaces instead, those answers are about the last profile and not about the "+
			"release. got merged sub-tree: %#v", ui)
	}
	// The paired positive: an overlay that DOES speak still wins. Without this the
	// assertion above is satisfied just as well by a merge that ignores overlays
	// altogether, which would be a different and equally wrong gate.
	if got, _ := ui["image"].(string); got != "late" {
		t.Fatalf("PREMISE BROKEN in the other direction: a later profile that DOES declare a key "+
			"did not win (image=%q, want %q). A merge that ignores overlays would satisfy the "+
			"silence check above while measuring nothing.", got, "late")
	}
}

// TestLoginConsole_GitignoredStackMemberExclusionStillHasASubject — the exclusion
// this file relies on must expire by itself.
//
// The live fe3455 chain is invoked with FOUR `-f` files and deployableStacks
// lists three; the fourth is out of the tree on purpose (per-cluster Ory
// secrets). That is a real limit on what the gate above can see, and an
// unstated limit is how "3 of 4" gets read as completeness. So the basis of the
// exclusion — the ignore rule — is checked here: if it goes away, the exclusion
// has lost its subject and this fails, which is the same discipline every other
// known-gap list in this repository is held to.
func TestLoginConsole_GitignoredStackMemberExclusionStillHasASubject(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(raw), gitignoredStackMember) {
		t.Fatalf("%s is no longer named in .gitignore. This file excludes it from the fold and "+
			"says so; with the ignore rule gone the exclusion has no subject — either fold the "+
			"profile in and delete this test, or restore the rule.", gitignoredStackMember)
	}
	for name, stack := range deployableStacks {
		for _, profile := range stack {
			if profile == gitignoredStackMember {
				t.Fatalf("%s now appears in stack %q. It is excluded precisely because it is not "+
					"in the tree; if it has become foldable, remove the exclusion instead of "+
					"carrying both.", gitignoredStackMember, name)
			}
		}
	}
	t.Logf("declared limit: the live fe3455 chain carries %s as a fourth -f; it is outside the "+
		"tree by design, so the fold above sees 3 of its 4 members and does not call that "+
		"completeness", gitignoredStackMember)
}
