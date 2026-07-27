// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/authz/catalogparity"
)

// knownCatalogDivergences — vpc's map asks for the READ TIER (`viewer`) on List where
// the catalog asks for the VERB relation (`v_list`).
//
// Left in place deliberately rather than mirrored: `viewer` and `v_list` are
// independent sets in the model, so the effective requirement today is their
// intersection, and dropping the `viewer` conjunct would ADMIT principals who hold
// only `v_list`. That is what the catalog says should happen, but widening access is
// not something to do as a side effect of a consistency fix. The gate exists so the
// set cannot grow while the decision is pending.
var knownCatalogDivergences = []string{
	`/kacho.cloud.vpc.v1.AddressService/List: catalog requires relation "v_list", service map requires "viewer"`,
	`/kacho.cloud.vpc.v1.GatewayService/List: catalog requires relation "v_list", service map requires "viewer"`,
	`/kacho.cloud.vpc.v1.NetworkInterfaceService/List: catalog requires relation "v_list", service map requires "viewer"`,
	`/kacho.cloud.vpc.v1.RouteTableService/List: catalog requires relation "v_list", service map requires "viewer"`,
	`/kacho.cloud.vpc.v1.SecurityGroupService/List: catalog requires relation "v_list", service map requires "viewer"`,
	`/kacho.cloud.vpc.v1.SubnetService/List: catalog requires relation "v_list", service map requires "viewer"`,
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
