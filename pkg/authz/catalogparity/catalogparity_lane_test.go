// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package catalogparity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogparity"
)

// catalogparity_lane_test.go — the LANE axis.
//
// The relation and scope axes only ever ran for methods where BOTH artefacts
// declared a per-RPC check. That left the most consequential disagreement of all
// invisible: a method the owning service declares scope-filtered — "there is no
// single object to ask about, I narrow the answer myself" — while the catalog
// tells the operator the edge checks relation R on object O. Whatever the edge
// then asks is, by the service's own account, not the question. Both such rows in
// the tree named `viewer` on the `cluster` singleton, a relation the bootstrap
// grants to `user:*`, so the check admitted every authenticated subject.
//
// The reverse is just as wrong and had no test either: a catalog row promising
// data-level narrowing for a method whose service map performs one ordinary check
// means nothing narrows per element and the edge asks nothing extra.
//
// Note which direction is deliberately NOT a divergence: catalog `<exempt>` with a
// checking service map. There the catalog says "the edge does not authorize this",
// which is true, and the service adds a layer on top. A declaration can be
// exceeded; it may not be contradicted.

// entry builds a catalog row. relation=="" means the row declares no per-RPC check.
func catEntry(fqn, permission, relation, objectType string, scopeFiltered bool) catalogparity.Entry {
	e := catalogparity.Entry{
		FQN:              fqn,
		Permission:       permission,
		RequiredRelation: relation,
		ScopeFiltered:    scopeFiltered,
	}
	e.ScopeExtractor.ObjectType = objectType
	return e
}

func laneKinds(rep catalogparity.Report) []catalogparity.Divergence {
	var out []catalogparity.Divergence
	for _, d := range rep.Divergences {
		if d.Kind == "lane" {
			out = append(out, d)
		}
	}
	return out
}

func TestCompare_Lane(t *testing.T) {
	const m = "/kacho.cloud.example.v1.ExampleService/List"

	cases := []struct {
		name        string
		svc         authz.RPCEntry
		cat         catalogparity.Entry
		wantLane    bool
		wantBucket  func(t *testing.T, rep catalogparity.Report)
		explanation string
	}{
		{
			name:     "service_scope_filtered_catalog_declares_a_relation",
			svc:      authz.RPCEntry{ScopeFiltered: true, Permission: "example.things.list"},
			cat:      catEntry("kacho.cloud.example.v1.ExampleService/List", "example.things.list", "viewer", "cluster", false),
			wantLane: true,
			explanation: "the catalog tells the operator the edge checks viewer@cluster while the owning " +
				"service declares the call unanswerable by one check",
		},
		{
			name:     "service_scope_filtered_catalog_scope_filtered",
			svc:      authz.RPCEntry{ScopeFiltered: true, Permission: "example.things.list"},
			cat:      catEntry("kacho.cloud.example.v1.ExampleService/List", "example.things.list", "", "", true),
			wantLane: false,
			wantBucket: func(t *testing.T, rep catalogparity.Report) {
				assert.Contains(t, rep.SkippedNonChecking, m, "agreeing lanes are reported, not compared")
			},
		},
		{
			name:     "service_scope_filtered_catalog_exempt",
			svc:      authz.RPCEntry{ScopeFiltered: true, Permission: "example.things.list"},
			cat:      catEntry("kacho.cloud.example.v1.ExampleService/List", catalogparity.ExemptPermission, "", "", false),
			wantLane: false,
			explanation: "`<exempt>` also says the edge does not authorize this — a weaker statement than " +
				"scope-filtered, but not a contradiction of it",
		},
		{
			name:     "catalog_scope_filtered_service_checks",
			svc:      authz.RPCEntry{Relation: "v_list", Permission: "example.things.list"},
			cat:      catEntry("kacho.cloud.example.v1.ExampleService/List", "example.things.list", "", "", true),
			wantLane: true,
			explanation: "the catalog promises per-element narrowing the service does not do, and on that " +
				"promise the edge stops asking anything",
		},
		{
			name:        "catalog_scope_filtered_service_public",
			svc:         authz.RPCEntry{Public: true, Permission: "example.things.list"},
			cat:         catEntry("kacho.cloud.example.v1.ExampleService/List", "example.things.list", "", "", true),
			wantLane:    true,
			explanation: "public means the service does not check either — so nothing narrows anywhere",
		},
		{
			name:     "catalog_exempt_service_checks_is_not_a_divergence",
			svc:      authz.RPCEntry{Relation: "v_list", Permission: "example.things.list"},
			cat:      catEntry("kacho.cloud.example.v1.ExampleService/List", catalogparity.ExemptPermission, "", "", false),
			wantLane: false,
			wantBucket: func(t *testing.T, rep catalogparity.Report) {
				assert.Contains(t, rep.SkippedExempt, m)
			},
		},
		{
			name:     "both_check_and_agree",
			svc:      authz.RPCEntry{Relation: "v_list", Permission: "example.things.list"},
			cat:      catEntry("kacho.cloud.example.v1.ExampleService/List", "example.things.list", "v_list", "example_thing", false),
			wantLane: false,
			wantBucket: func(t *testing.T, rep catalogparity.Report) {
				assert.Equal(t, 1, rep.Compared)
				assert.Empty(t, rep.Divergences)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := catalogparity.Compare(
				authz.RPCMap{m: c.svc},
				map[string]catalogparity.Entry{m: c.cat},
			)
			lanes := laneKinds(rep)
			if c.wantLane {
				require.Len(t, lanes, 1, "expected exactly one lane divergence (%s); report=%+v", c.explanation, rep)
				assert.Equal(t, m, lanes[0].Method)
				assert.NotEmpty(t, lanes[0].String(), "a divergence must render itself for the known-list")
				assert.Contains(t, lanes[0].String(), m)
			} else {
				assert.Empty(t, lanes, "unexpected lane divergence: %+v", lanes)
			}
			if c.wantBucket != nil {
				c.wantBucket(t, rep)
			}
		})
	}
}

// TestCompare_LaneDivergenceRendersDistinctly — the known-divergence lists are
// keyed by the rendered string, so a lane divergence must not collide with the
// relation/scope wording of the same method; otherwise silencing one would
// silence the other.
func TestCompare_LaneDivergenceRendersDistinctly(t *testing.T) {
	const m = "/kacho.cloud.example.v1.ExampleService/List"
	rep := catalogparity.Compare(
		authz.RPCMap{m: {ScopeFiltered: true, Permission: "example.things.list"}},
		map[string]catalogparity.Entry{m: catEntry(
			"kacho.cloud.example.v1.ExampleService/List", "example.things.list", "viewer", "cluster", false)},
	)
	keys := rep.Keys()
	require.Len(t, keys, 1)
	assert.NotContains(t, keys[0], "service map requires", "lane wording must not be the relation wording")
	assert.NotContains(t, keys[0], "anchors scope on", "lane wording must not be the scope wording")
}

// TestLoadCatalog_ReadsScopeFilteredOffTheRealCatalog — the parity engine must read
// the lane from the artefact the gateway actually enforces, not from a struct that
// merely has the field. A decoder that silently drops the key would make every
// lane comparison vacuously agreeable.
func TestLoadCatalog_ReadsScopeFilteredOffTheRealCatalog(t *testing.T) {
	cat, err := catalogparity.LoadCatalog(".")
	require.NoError(t, err)
	require.NotEmpty(t, cat)

	var scopeFiltered []string
	for fqn, e := range cat {
		if e.ScopeFiltered {
			scopeFiltered = append(scopeFiltered, fqn)
			assert.Empty(t, e.RequiredRelation, "%s: a scope-filtered row must name no relation", fqn)
			assert.Empty(t, e.ScopeExtractor.ObjectType, "%s: a scope-filtered row must name no scope", fqn)
			assert.NotEqual(t, catalogparity.ExemptPermission, e.Permission,
				"%s: scope-filtered and exempt are different lanes", fqn)
		}
	}
	assert.NotEmpty(t, scopeFiltered,
		"no catalog row declares the scope-filtered lane — either the lane was reverted or the decoder "+
			"is dropping the key, and every lane comparison below is then vacuous")
}
