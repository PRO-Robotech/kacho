// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// nlb1a_catalog_test.go — sub-phase NLB-1a regression at the api-gateway edge:
// the embedded permission catalog must key every loadbalancer per-RPC authz scope
// on the renamed `nlb_*` FGA object type (not legacy `lb_*`), and the two embedded
// copies (gateway middleware + iam seed) must stay byte-identical. Traceability:
// NLB-1a-01 (scope_extractor→nlb_* target) / NLB-1a-02 (catalog byte-identical).

// repoRoot walks up from this test's source file to the module root (go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	dir := filepath.Dir(file)
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("go.mod not found walking up from %s", file)
	return ""
}

// TestNLB1a01_CatalogScopeExtractorNlbTarget — verifies NLB-1a-01: each loadbalancer
// object-self RPC in the embedded catalog resolves its scope on the renamed `nlb_*`
// object type via the id request-field (anti-BOLA); Create resolves the parent project.
func TestNLB1a01_CatalogScopeExtractorNlbTarget(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	want := []struct {
		fqn      string
		relation string
		objType  string
		field    string
	}{
		{"kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Get", "v_get", "nlb_network_load_balancer", "network_load_balancer_id"},
		{"kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Update", "v_update", "nlb_network_load_balancer", "network_load_balancer_id"},
		{"kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Delete", "v_delete", "nlb_network_load_balancer", "network_load_balancer_id"},
		{"kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Create", "editor", "project", "project_id"},
		{"kacho.cloud.loadbalancer.v1.ListenerService/Create", "editor", "nlb_network_load_balancer", "load_balancer_id"},
		{"kacho.cloud.loadbalancer.v1.ListenerService/Get", "v_get", "nlb_listener", "listener_id"},
		{"kacho.cloud.loadbalancer.v1.ListenerService/Delete", "v_delete", "nlb_listener", "listener_id"},
		{"kacho.cloud.loadbalancer.v1.TargetGroupService/Get", "v_get", "nlb_target_group", "target_group_id"},
		{"kacho.cloud.loadbalancer.v1.TargetGroupService/Update", "v_update", "nlb_target_group", "target_group_id"},
		{"kacho.cloud.loadbalancer.v1.TargetGroupService/Create", "editor", "project", "project_id"},
	}
	for _, w := range want {
		t.Run(w.fqn, func(t *testing.T) {
			entry, ok := c.Lookup(w.fqn)
			require.Truef(t, ok, "fqn missing from embedded catalog: %s", w.fqn)
			assert.Equal(t, w.relation, entry.RequiredRelation, "required_relation on %s", w.fqn)
			assert.Equalf(t, w.objType, entry.ScopeExtractor.ObjectType,
				"scope object_type on %s must be the renamed nlb_* token", w.fqn)
			assert.Equal(t, w.field, entry.ScopeExtractor.FromRequestField, "from_request_field on %s", w.fqn)
		})
	}
}

// TestNLB1a02_CatalogNoLegacyLbType — verifies NLB-1a-02: NO loadbalancer catalog
// entry keys on a legacy `lb_*` object type (rename complete, no dangling route).
func TestNLB1a02_CatalogNoLegacyLbType(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "gateway", "internal", "middleware", "embed", "permission_catalog.json"))
	require.NoError(t, err)

	var entries []struct {
		FQN            string `json:"fqn"`
		ScopeExtractor struct {
			ObjectType string `json:"object_type"`
		} `json:"scope_extractor"`
	}
	require.NoError(t, json.Unmarshal(raw, &entries))

	sawNlb := false
	for _, e := range entries {
		ot := e.ScopeExtractor.ObjectType
		require.Falsef(t, strings.HasPrefix(ot, "lb_"),
			"catalog entry %s still keys on legacy object_type %q — NLB-1a rename incomplete", e.FQN, ot)
		if strings.HasPrefix(ot, "nlb_") {
			sawNlb = true
			require.Contains(t, []string{"nlb_network_load_balancer", "nlb_listener", "nlb_target_group"}, ot,
				"unexpected nlb_* object_type %q for %s", ot, e.FQN)
		}
	}
	require.True(t, sawNlb, "expected loadbalancer catalog entries keyed on nlb_* object types")
}

// TestNLB1a02_CatalogCopiesByteIdentical — verifies NLB-1a-02: the two embedded
// catalog copies (gateway middleware + iam seed) are byte-identical, so the edge
// Check and the iam grant-taxonomy resolve against exactly the same authz surface.
func TestNLB1a02_CatalogCopiesByteIdentical(t *testing.T) {
	root := repoRoot(t)
	gw, err := os.ReadFile(filepath.Join(root, "gateway", "internal", "middleware", "embed", "permission_catalog.json"))
	require.NoError(t, err)
	iam, err := os.ReadFile(filepath.Join(root, "services", "iam", "internal", "apps", "kacho", "seed", "embedded", "permission_catalog.json"))
	require.NoError(t, err)
	require.Truef(t, bytes.Equal(gw, iam),
		"embedded permission_catalog.json copies drifted (gateway=%d bytes, iam=%d bytes) — both must be renamed in lockstep",
		len(gw), len(iam))
}
