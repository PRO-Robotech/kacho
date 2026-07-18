// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package reconcile

// forward_test.go — use-case unit tests for the ADDITIVE forward fast-path
// (ReconcileObjectForward). Driven against the in-memory fakeStore (no Postgres — a
// service-layer test requiring Postgres would be adapter leakage). The pg integration
// twin (reconcile_forward_integration_test.go) exercises the real advisory-lock-free
// SQL + concurrency.
//
// These pin the fast-path contract:
//   - it materializes EXACTLY the one registered object's ACTIVE tuples for a matching
//     binding, WITHOUT taking the per-binding advisory lock (the throughput property);
//   - a matched-but-foreign object is NOT granted (additive-only leaves REJECTED/audit
//     to the async full backstop);
//   - a cluster `*.*` binding (empty-ObjectTypes selectors) materializes nothing
//     per-object (the D-9 flat short-circuit is preserved).

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
)

// TestReconcileObjectForward_MaterializesSingleObject_NoExclusiveLock — the fast-path
// materializes the freshly-registered object's ACTIVE member + per-object tuples for the
// matching binding and takes NO EXCLUSIVE advisory lock (f.locks stays 0), only the SHARE
// lock (f.sharedLocks>=1); it reads the binding via the unlocked load. This is the
// throughput-critical property: SHARE ∥ SHARE do not conflict, so N concurrent
// registrations sharing one binding never serialize on each other (unlike the EXCLUSIVE
// full path).
func TestReconcileObjectForward_MaterializesSingleObject_NoExclusiveLock(t *testing.T) {
	fp := domain.Rule{
		Module: "compute", Resources: []string{"instance"}, Verbs: []string{"get", "update"},
	}.Fingerprint()
	f := &fakeStore{
		scope:       domain.ScopeAnchor{Type: "project", ID: "prj-1"},
		subjectType: "user", subjectID: "usr-1", active: true,
		selectors: []domain.RuleSelector{{
			Arm: domain.ArmAnchor, RuleFP: fp,
			ObjectTypes: []string{"compute.instance"},
			Verbs:       []string{"get", "update"},
		}},
		mirror: map[string][]domain.MirrorObject{
			"compute.instance": {
				{ObjectType: "compute.instance", ObjectID: "i-new", ParentProjectID: "prj-1"},
			},
		},
		// The scope-narrowed fast-path source returns the matching binding.
		selectorBindings: []domain.AccessBindingID{"acb-1"},
	}
	rec := New(fakeRunner{s: f}, nil)
	require.NoError(t, rec.ReconcileObjectForward(context.Background(), "compute.instance", "i-new"))

	// NO EXCLUSIVE advisory lock — the additive forward path removes the serialization
	// point; it takes only the SHARE lock (coexists with sibling forwards).
	assert.Equal(t, 0, f.locks, "forward fast-path must NOT take the EXCLUSIVE advisory lock (throughput)")
	assert.GreaterOrEqual(t, f.sharedLocks, 1, "forward takes the SHARE advisory lock (mutual-exclusion vs full only)")
	assert.GreaterOrEqual(t, f.unlockedLoads, 1, "forward reads the binding via the UNLOCKED load")

	// Exactly the ONE registered object is materialized ACTIVE (no full-scope recompute).
	require.Len(t, f.upserts, 1, "only the registered object materialized")
	assert.Equal(t, "i-new", f.upserts[0].ObjectID)
	assert.Equal(t, domain.VerificationActive, f.upserts[0].VerificationStatus)
	assert.Equal(t, fp, f.upserts[0].RuleFP)

	w := allWrites(f)
	assert.True(t, hasTuple(w, "v_get", "compute_instance:i-new"), "v_get on the registered object")
	assert.True(t, hasTuple(w, "v_update", "compute_instance:i-new"), "v_update on the registered object")
	assert.True(t, hasTuple(w, "v_delete", "compute_instance:i-new"), "v_update⟹v_delete co-materialized (leaf editor)")
	assert.True(t, hasTuple(w, "editor", "compute_instance:i-new"), "back-compat tier on the registered object")
	// The tuples were recorded into the ledger in the SAME pass (symmetric-revoke lineage).
	require.NotEmpty(t, f.recorded, "forward co-commits the emitted tuples into the ledger")
	// Additive-only: nothing revoked/deleted/audited.
	assert.Empty(t, f.tdeletes, "forward never revokes")
	assert.Empty(t, f.deletes, "forward never deletes a member")
	assert.Empty(t, f.audits, "forward never audits (async full backstop owns REJECTED)")
}

// TestReconcileObjectForward_ForeignScope_SkipsNoTuple — a matched-but-foreign object
// (label/name arm can match cross-scope) is NOT granted by the additive path: no tuple,
// no member, no audit. The async full backstop owns the REJECTED member + containment
// audit.
func TestReconcileObjectForward_ForeignScope_SkipsNoTuple(t *testing.T) {
	fp := domain.Rule{
		Module: "compute", Resources: []string{"instance"}, Verbs: []string{"get"},
		MatchLabels: map[string]string{"env": "prod"},
	}.Fingerprint()
	f := &fakeStore{
		scope:       domain.ScopeAnchor{Type: "project", ID: "prj-1"},
		subjectType: "user", subjectID: "usr-1", active: true,
		selectors: []domain.RuleSelector{{
			Arm: domain.ArmLabels, RuleFP: fp, ObjectTypes: []string{"compute.instance"},
			MatchLabels: map[string]string{"env": "prod"}, Verbs: []string{"get"},
		}},
		mirror: map[string][]domain.MirrorObject{
			"compute.instance": {
				// matches labels but lives under a FOREIGN project.
				{ObjectType: "compute.instance", ObjectID: "i-foreign", ParentProjectID: "prj-OTHER", Labels: map[string]string{"env": "prod"}},
			},
		},
		selectorBindings: []domain.AccessBindingID{"acb-1"},
	}
	rec := New(fakeRunner{s: f}, nil)
	require.NoError(t, rec.ReconcileObjectForward(context.Background(), "compute.instance", "i-foreign"))

	assert.Empty(t, f.upserts, "additive forward does NOT write a REJECTED member")
	assert.Empty(t, allWrites(f), "foreign-scope object gets NO tuple")
	assert.Empty(t, f.audits, "forward defers the containment audit to the async full backstop")
	assert.Equal(t, 0, f.locks, "still no EXCLUSIVE advisory lock")
}

// TestReconcileObjectForward_ClusterSuperAdmin_NoPerObject — a cluster `*.*` binding
// carries selectors with EMPTY ObjectTypes (the scope-aware projection yields no content
// types for a CLUSTER scope — the D-9 flat short-circuit owns cluster super-admin). The
// forward path must materialize NOTHING per-object for it (never re-introduce per-object-
// on-cluster).
func TestReconcileObjectForward_ClusterSuperAdmin_NoPerObject(t *testing.T) {
	f := &fakeStore{
		scope:       domain.ScopeAnchor{Type: "cluster", ID: "cluster_kacho_root"},
		subjectType: "user", subjectID: "usr-root", active: true,
		// cluster-scope wildcard → empty ObjectTypes (short-circuit, not per-object).
		selectors: []domain.RuleSelector{{
			Arm: domain.ArmAnchor, RuleFP: "wildcard", ObjectTypes: nil, Verbs: []string{"get", "update", "delete"},
		}},
		mirror: map[string][]domain.MirrorObject{
			"compute.instance": {
				{ObjectType: "compute.instance", ObjectID: "i-any", ParentProjectID: "prj-1"},
			},
		},
		selectorBindings: []domain.AccessBindingID{"acb-cluster"},
	}
	rec := New(fakeRunner{s: f}, nil)
	require.NoError(t, rec.ReconcileObjectForward(context.Background(), "compute.instance", "i-any"))

	assert.Empty(t, f.upserts, "cluster super-admin is NOT materialized per-object (D-9 short-circuit preserved)")
	assert.Empty(t, allWrites(f), "no per-object tuple on a cluster `*.*` binding")
}

// TestReconcileObjectForward_ObjectNotInMirror_NoOp — a fast-path call for an object not
// (yet) in the mirror is a safe no-op (the async backstop / PENDING re-verify owns it).
func TestReconcileObjectForward_ObjectNotInMirror_NoOp(t *testing.T) {
	f := &fakeStore{
		scope:       domain.ScopeAnchor{Type: "project", ID: "prj-1"},
		subjectType: "user", subjectID: "usr-1", active: true,
		selectorBindings: []domain.AccessBindingID{"acb-1"},
	}
	rec := New(fakeRunner{s: f}, nil)
	require.NoError(t, rec.ReconcileObjectForward(context.Background(), "compute.instance", "i-absent"))
	assert.Empty(t, f.upserts)
	assert.Empty(t, allWrites(f))
	assert.Equal(t, 0, f.unlockedLoads, "no binding is loaded when the object is not in the mirror")
}
