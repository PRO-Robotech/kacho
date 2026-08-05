// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package catalogparity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Imported for its side effect: it registers the registry v1 descriptors in
	// the global proto registry, so ScopeObjectType can resolve the request type
	// of the real methods used below. The hide axis runs only after the scope axis
	// has agreed, so a made-up FQN would skip it and the test would assert nothing.
	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogparity"
)

// catalogparity_hide_test.go — the HIDE axis: not who decides and not what is
// required, but how a refusal SOUNDS.
//
// Both deciders refuse the same call. If one answers the owning service's own
// NotFound and the other answers PermissionDenied, the caller learns which of the
// two answered — and that is exactly the difference hiding existence is meant to
// erase. The two do disagree in practice: each keeps its own positive-verdict
// cache with its own window, so a call the edge lets through can still be refused
// by the service one hop later.
//
// The axis was introduced after a read of a just-deleted registry came back as a
// refusal from the service while the neighbouring case in the same collection
// requires the owner's verbatim miss.

const (
	regGet    = "/kacho.cloud.registry.v1.RegistryService/Get"
	regDelete = "/kacho.cloud.registry.v1.RegistryService/Delete"
)

// registryScoped — a service map entry anchored on the registry object, shaped
// like the real one (the id comes from the request; an empty request yields an
// empty id, which is all the scope axis needs).
func registryScoped(relation string, hide bool) authz.RPCEntry {
	return authz.RPCEntry{
		Relation:      relation,
		HideExistence: hide,
		Extract: authz.StaticExtractor("registry_registry", func(req any) (string, error) {
			switch r := req.(type) {
			case *registryv1.GetRegistryRequest:
				return r.GetRegistryId(), nil
			case *registryv1.DeleteRegistryRequest:
				return r.GetRegistryId(), nil
			default:
				return "", nil
			}
		}),
	}
}

func hideDivergences(rep catalogparity.Report) []catalogparity.Divergence {
	var out []catalogparity.Divergence
	for _, d := range rep.Divergences {
		if d.Kind == "hide" {
			out = append(out, d)
		}
	}
	return out
}

func catHideEntry(fqn, relation, fromField string, explicit bool) catalogparity.Entry {
	e := catalogparity.Entry{
		FQN:              fqn,
		Permission:       "registry.registries.x",
		RequiredRelation: relation,
		HideExistence:    explicit,
	}
	e.ScopeExtractor.ObjectType = "registry_registry"
	e.ScopeExtractor.FromRequestField = fromField
	return e
}

// TestCompare_Hide_RedWhenTheServiceRefusesWhereTheEdgeHides — the injection: the
// real defect, in the shape it actually had. The edge marks the mutation
// hide-existence; the service map does not, so the same call is answered "no such
// thing" by one decider and "not allowed" by the other.
func TestCompare_Hide_RedWhenTheServiceRefusesWhereTheEdgeHides(t *testing.T) {
	catalog := map[string]catalogparity.Entry{
		regDelete: catHideEntry("kacho.cloud.registry.v1.RegistryService/Delete", "v_delete", "registry_id", true),
	}
	rep := catalogparity.Compare(authz.RPCMap{regDelete: registryScoped("v_delete", false)}, catalog)

	d := hideDivergences(rep)
	require.Len(t, d, 1, "a service map that refuses where the edge hides must be reported")
	assert.True(t, d[0].CatalogHidesExistence)
	assert.False(t, d[0].ServiceHidesExistence)
}

// TestCompare_Hide_RedWhenTheServiceHidesWhereTheEdgeRefuses — the mirror
// direction. It is a divergence too: the operator reads the catalog to learn what
// a refusal looks like, and a service that answers a miss where the catalog
// declares a refusal describes deployed behaviour the document does not.
func TestCompare_Hide_RedWhenTheServiceHidesWhereTheEdgeRefuses(t *testing.T) {
	catalog := map[string]catalogparity.Entry{
		regDelete: catHideEntry("kacho.cloud.registry.v1.RegistryService/Delete", "v_delete", "registry_id", false),
	}
	rep := catalogparity.Compare(authz.RPCMap{regDelete: registryScoped("v_delete", true)}, catalog)

	d := hideDivergences(rep)
	require.Len(t, d, 1, "a service map that hides where the edge refuses must be reported")
	assert.False(t, d[0].CatalogHidesExistence)
	assert.True(t, d[0].ServiceHidesExistence)
}

// TestCompare_Hide_SilentOnLegitimateAgreement — the twin the gate must NOT flag.
// Without it the axis would be indistinguishable from "always red", and the first
// false alarm would get it deleted.
func TestCompare_Hide_SilentOnLegitimateAgreement(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		catalog catalogparity.Entry
		service authz.RPCEntry
	}{
		{
			// A per-object read: neither side needs a mark — both derive hiding from
			// the shape of the RPC, and both arrive at the same answer.
			name:    "derived read on both sides",
			method:  regGet,
			catalog: catHideEntry("kacho.cloud.registry.v1.RegistryService/Get", "v_get", "registry_id", false),
			service: registryScoped("v_get", false),
		},
		{
			// A mutation both sides mark.
			name:    "explicitly marked mutation on both sides",
			method:  regDelete,
			catalog: catHideEntry("kacho.cloud.registry.v1.RegistryService/Delete", "v_delete", "registry_id", true),
			service: registryScoped("v_delete", true),
		},
		{
			// A mutation neither side marks: a refusal is a refusal, and saying "not
			// found" here would erase the difference between "no rights" and "no
			// resource" where there is nothing to hide.
			name:    "unmarked mutation on both sides",
			method:  regDelete,
			catalog: catHideEntry("kacho.cloud.registry.v1.RegistryService/Delete", "v_delete", "registry_id", false),
			service: registryScoped("v_delete", false),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := catalogparity.Compare(
				authz.RPCMap{tc.method: tc.service},
				map[string]catalogparity.Entry{tc.method: tc.catalog})
			assert.Empty(t, rep.Divergences, "legitimate agreement must not be reported")
			assert.Equal(t, 1, rep.Compared, "the method must actually have been compared — "+
				"a skipped comparison would make the silence above meaningless")
		})
	}
}

// TestCompare_Hide_PremiseHolds — the axis rests on the scope object type being
// recoverable from the service map. If it stops being recoverable, the hide axis
// silently stops running and reports nothing — the same "zero findings because
// nothing was read" the gate exists to prevent. Assert the premise directly.
func TestCompare_Hide_PremiseHolds(t *testing.T) {
	ot, ok := catalogparity.ScopeObjectType(regGet, registryScoped("v_get", false))
	require.True(t, ok, "scope object type is no longer recoverable — the hide axis below never runs")
	assert.Equal(t, "registry_registry", ot)
}
