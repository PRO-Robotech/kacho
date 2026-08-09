// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/authz/catalogparity"
)

// knownCatalogDivergences — none. vpc's map mirrors the catalog exactly.
//
// It did not always. Six top-level List RPCs used to sit here because the catalog
// named the project's `v_list` verb while this map enforced the read tier `viewer`,
// and in the model neither relation is derived from the other — so the effective
// requirement was their INTERSECTION and a subject granted exactly what the catalog
// declared could not list at all. The entry stood on the reading that the catalog was
// right and the map over-strict, which made removing the `viewer` conjunct a widening
// nobody wanted to do in passing.
//
// That reading was wrong, and the platform's own convention settles it: the write
// tier `editor` — not `v_create` — already gated Create on the very same project
// scope in BOTH artefacts, and storage's three project-scoped List RPCs already
// named `viewer` there with no divergence at all. (A fourth witness, iam's
// ConditionsService/List, stood here until that surface was retired; the argument
// never rested on it alone.)
// `v_list` on a project is object-level access to the PROJECT ITSELF (the model says
// of that verb set: "never to its contents"), so it was answering a different
// question than "may this subject list what is inside". The annotations were
// corrected and the catalog regenerated; the requirement the catalog states is now
// the one enforced.
var knownCatalogDivergences []string

// TestPermissionMapMirrorsCatalog locks this service's in-process authz map to the
// generated permission catalog — the artefact the api-gateway enforces.
//
// Two authorization decisions are taken on one call: the gateway checks the catalog
// entry, then the owning service re-checks its own map. The catalog is the authority
// (it is generated from the proto annotations), so the map must mirror it on both
// axes — required relation AND scope object type. When they disagree, the deployed
// behaviour is no longer the one the catalog documents: the effective requirement
// becomes the INTERSECTION, which 403s principals the catalog admits, and anything
// that reaches the service without traversing the gateway is judged by a different
// rule than the one written down.
//
// The gate is a DIFF, not a boolean. Divergences that exist today are enumerated
// below with the reason each cannot simply be mirrored; a new one fails as
// unexpected, and a resolved one fails too, so an exemption cannot outlive its cause.
func TestPermissionMapMirrorsCatalog(t *testing.T) {
	catalog, err := catalogparity.LoadCatalog(".")
	if err != nil {
		t.Fatalf("load permission catalog: %v", err)
	}
	rep := catalogparity.Compare(PermissionMap(), catalog)
	if rep.Compared == 0 {
		t.Fatal("no method was compared against the catalog — the join produced nothing, so this test asserts nothing")
	}

	unexpected, resolved := rep.Diff(knownCatalogDivergences)
	for _, d := range unexpected {
		t.Errorf("NEW divergence from the permission catalog (the catalog is the authority): %s", d)
	}
	for _, d := range resolved {
		t.Errorf("a listed divergence no longer exists — delete it from knownCatalogDivergences "+
			"so the list cannot outlive its cause: %s", d)
	}
	t.Logf("compared %d methods; %d known divergences; %d exempt, %d non-checking, %d absent from catalog",
		rep.Compared, len(knownCatalogDivergences), len(rep.SkippedExempt),
		len(rep.SkippedNonChecking), len(rep.NotInCatalog))
}
