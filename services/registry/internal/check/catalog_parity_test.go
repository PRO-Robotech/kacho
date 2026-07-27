// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/authz/catalogparity"
)

// knownCatalogDivergences — two internal admin RPCs where the artefacts disagree.
//
// GetRegistryStats disagrees on BOTH axes (cluster-wide `system_viewer` in the
// catalog, per-registry `v_get` in the map): not comparable, so mirroring changes the
// question rather than tightening it, and it cuts both ways.
//
// TriggerGarbageCollection agrees on the object but not the relation (`admin` vs
// `v_delete`), which are independent sets in the model — mirroring would admit a
// direct `admin` holder who lacks `v_delete`.
//
// Both are left in place deliberately; resolving them is a decision about the admin
// surface, not a by-product of aligning two tables. The gate exists so the set cannot
// grow meanwhile.
var knownCatalogDivergences = []string{
	`/kacho.cloud.registry.v1.InternalRegistryService/GetRegistryStats: catalog anchors scope on object type "cluster", service map anchors on "registry_registry"`,
	`/kacho.cloud.registry.v1.InternalRegistryService/GetRegistryStats: catalog requires relation "system_viewer", service map requires "v_get"`,
	`/kacho.cloud.registry.v1.InternalRegistryService/TriggerGarbageCollection: catalog requires relation "admin", service map requires "v_delete"`,
}

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
