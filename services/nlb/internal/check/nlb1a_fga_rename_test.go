// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/check"
)

// nlb1a_fga_rename_test.go — behaviour-level regression-lock for sub-phase NLB-1a
// (FGA object-type hard-rename `lb_*` → `nlb_*`). These tests key on the *token*
// (`nlb_network_load_balancer` / `nlb_listener` / `nlb_target_group`) that the
// per-RPC authz interceptor resolves, NOT merely on "access allowed" — a partial
// rename (any site left on the legacy `lb_*` type) is caught here and by the
// catalog byte-identity gate, per testing.md §Regression-lock.
//
// Traceability: acceptance sub-phase-NLB-1a-fga-relation-rename NLB-1a-01/03/05.

// New object-type tokens (post-rename). Duplicated here as string literals (not
// imported from the package under test) so the assertion pins the *value*, not a
// symbol that could itself be wrong.
const (
	wantTypeLoadBalancer = "nlb_network_load_balancer"
	wantTypeListener     = "nlb_listener"
	wantTypeTargetGroup  = "nlb_target_group"
	// legacyPrefix — the pre-rename FGA object-domain prefix. After the hard
	// rename NO Extract may yield a `lb_*` object type (no dangling old-type path).
	legacyPrefix = "lb_"
)

// TestNLB1a01_ScopeExtractorResolvesNlbTarget — verifies NLB-1a-01: the per-RPC
// interceptor keys mutation/read on the renamed `nlb_*` object type; Create/List
// resolve the parent `project` scope; Listener.Create resolves the parent LB.
func TestNLB1a01_ScopeExtractorResolvesNlbTarget(t *testing.T) {
	m := check.PermissionMap()
	const id = "res-id"

	type tc struct {
		fm         string
		req        any
		wantType   string
		wantReln   string
		wantScopeK string // human label
	}
	cases := []tc{
		// object-self mutation → resolves the resource's own nlb_* type + verb-bearing relation.
		{"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Update",
			&lbv1.UpdateNetworkLoadBalancerRequest{NetworkLoadBalancerId: id},
			wantTypeLoadBalancer, "v_update", "self"},
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/Update",
			&lbv1.UpdateTargetGroupRequest{TargetGroupId: id},
			wantTypeTargetGroup, "v_update", "self"},
		{"/kacho.cloud.loadbalancer.v1.ListenerService/Update",
			&lbv1.UpdateListenerRequest{ListenerId: id},
			wantTypeListener, "v_update", "self"},
		// Create on parent project → editor on project.
		{"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Create",
			&lbv1.CreateNetworkLoadBalancerRequest{ProjectId: id},
			"project", "editor", "project"},
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/Create",
			&lbv1.CreateTargetGroupRequest{ProjectId: id},
			"project", "editor", "project"},
		// Listener.Create → editor on parent LB (nlb_network_load_balancer), anti-BOLA.
		{"/kacho.cloud.loadbalancer.v1.ListenerService/Create",
			&lbv1.CreateListenerRequest{LoadBalancerId: id},
			wantTypeLoadBalancer, "editor", "parent-lb"},
	}
	for _, c := range cases {
		t.Run(c.fm, func(t *testing.T) {
			e, ok := m[c.fm]
			require.Truef(t, ok, "PermissionMap missing %q", c.fm)
			require.NotNil(t, e.Extract)
			gotType, gotID, err := e.Extract(c.req)
			require.NoError(t, err)
			require.Equalf(t, c.wantType, gotType,
				"scope object_type for %s (%s) must be the renamed nlb_* token", c.fm, c.wantScopeK)
			require.Equal(t, id, gotID)
			require.Equalf(t, c.wantReln, e.Relation,
				"required relation for %s must gate the resolved scope", c.fm)
		})
	}
}

// TestNLB1a03_ReadViewerFloorAndListScopeNlb — verifies NLB-1a-03: read RPCs pass
// under the viewer-floor (Get → v_get on the renamed nlb_* type; List → viewer on
// the parent project with ScopeFiltered listauthz), never on a legacy `lb_*` type.
func TestNLB1a03_ReadViewerFloorAndListScopeNlb(t *testing.T) {
	m := check.PermissionMap()
	const id = "res-id"

	// Get → v_get on the resource's own nlb_* type.
	getCases := []struct {
		fm       string
		req      any
		wantType string
	}{
		{"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Get",
			&lbv1.GetNetworkLoadBalancerRequest{NetworkLoadBalancerId: id}, wantTypeLoadBalancer},
		{"/kacho.cloud.loadbalancer.v1.ListenerService/Get",
			&lbv1.GetListenerRequest{ListenerId: id}, wantTypeListener},
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/Get",
			&lbv1.GetTargetGroupRequest{TargetGroupId: id}, wantTypeTargetGroup},
	}
	for _, c := range getCases {
		t.Run("get "+c.fm, func(t *testing.T) {
			e := m[c.fm]
			gotType, _, err := e.Extract(c.req)
			require.NoError(t, err)
			require.Equal(t, c.wantType, gotType)
			require.Equal(t, "v_get", e.Relation, "read must be viewer-floor verb v_get")
		})
	}

	// List → viewer on parent project, ScopeFiltered (per-object listauthz under nlb_*).
	listCases := []struct {
		fm  string
		req any
	}{
		{"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/List",
			&lbv1.ListNetworkLoadBalancersRequest{ProjectId: id}},
		{"/kacho.cloud.loadbalancer.v1.ListenerService/List",
			&lbv1.ListListenersRequest{ProjectId: id}},
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/List",
			&lbv1.ListTargetGroupsRequest{ProjectId: id}},
	}
	for _, c := range listCases {
		t.Run("list "+c.fm, func(t *testing.T) {
			e := m[c.fm]
			gotType, _, err := e.Extract(c.req)
			require.NoError(t, err)
			require.Equal(t, "project", gotType, "List scope is the parent project")
			require.Equal(t, "viewer", e.Relation, "List gated by viewer-floor")
			require.True(t, e.ScopeFiltered, "List must be listauthz ScopeFiltered")
		})
	}
}

// TestNLB1a05_NoLegacyObjectTypeAnywhere — verifies NLB-1a-05: the hard rename left
// NO `lb_*` object-type route. Every non-Public Extract in the interceptor map
// yields either a renamed nlb_* type or a hierarchy scope (project/cluster) — never
// a legacy `lb_*` token. A single missed site re-introduces a `lb_*` here → RED.
func TestNLB1a05_NoLegacyObjectTypeAnywhere(t *testing.T) {
	m := check.PermissionMap()

	// A representative typed request per RPC so Extract yields the static object type.
	const id = "x"
	reqFor := map[string]any{
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Get":               &lbv1.GetNetworkLoadBalancerRequest{NetworkLoadBalancerId: id},
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/List":              &lbv1.ListNetworkLoadBalancersRequest{ProjectId: id},
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Create":            &lbv1.CreateNetworkLoadBalancerRequest{ProjectId: id},
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Update":            &lbv1.UpdateNetworkLoadBalancerRequest{NetworkLoadBalancerId: id},
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Delete":            &lbv1.DeleteNetworkLoadBalancerRequest{NetworkLoadBalancerId: id},
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Start":             &lbv1.StartNetworkLoadBalancerRequest{NetworkLoadBalancerId: id},
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Stop":              &lbv1.StopNetworkLoadBalancerRequest{NetworkLoadBalancerId: id},
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Move":              &lbv1.MoveNetworkLoadBalancerRequest{NetworkLoadBalancerId: id, DestinationProjectId: "p2"},
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/AttachTargetGroup": &lbv1.AttachNetworkLoadBalancerTargetGroupRequest{NetworkLoadBalancerId: id},
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/DetachTargetGroup": &lbv1.DetachNetworkLoadBalancerTargetGroupRequest{NetworkLoadBalancerId: id},
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/GetTargetStates":   &lbv1.GetTargetStatesRequest{NetworkLoadBalancerId: id},
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/ListOperations":    &lbv1.ListNetworkLoadBalancerOperationsRequest{NetworkLoadBalancerId: id},
		"/kacho.cloud.loadbalancer.v1.ListenerService/Get":                          &lbv1.GetListenerRequest{ListenerId: id},
		"/kacho.cloud.loadbalancer.v1.ListenerService/List":                         &lbv1.ListListenersRequest{ProjectId: id},
		"/kacho.cloud.loadbalancer.v1.ListenerService/Create":                       &lbv1.CreateListenerRequest{LoadBalancerId: id},
		"/kacho.cloud.loadbalancer.v1.ListenerService/Update":                       &lbv1.UpdateListenerRequest{ListenerId: id},
		"/kacho.cloud.loadbalancer.v1.ListenerService/Delete":                       &lbv1.DeleteListenerRequest{ListenerId: id},
		"/kacho.cloud.loadbalancer.v1.ListenerService/ListOperations":               &lbv1.ListListenerOperationsRequest{ListenerId: id},
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/Get":                       &lbv1.GetTargetGroupRequest{TargetGroupId: id},
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/List":                      &lbv1.ListTargetGroupsRequest{ProjectId: id},
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/Create":                    &lbv1.CreateTargetGroupRequest{ProjectId: id},
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/Update":                    &lbv1.UpdateTargetGroupRequest{TargetGroupId: id},
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/Delete":                    &lbv1.DeleteTargetGroupRequest{TargetGroupId: id},
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/Move":                      &lbv1.MoveTargetGroupRequest{TargetGroupId: id, DestinationProjectId: "p2"},
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/AddTargets":                &lbv1.AddTargetsRequest{TargetGroupId: id},
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/RemoveTargets":             &lbv1.RemoveTargetsRequest{TargetGroupId: id},
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/ListOperations":            &lbv1.ListTargetGroupOperationsRequest{TargetGroupId: id},
		"/kacho.cloud.loadbalancer.v1.InternalLoadBalancerAnnounceService/GetAnnounceState":    &lbv1.GetLoadBalancerAnnounceStateRequest{NetworkLoadBalancerId: id},
		"/kacho.cloud.loadbalancer.v1.InternalLoadBalancerAnnounceService/ReportAnnounceState": &lbv1.ReportLoadBalancerAnnounceStateRequest{NetworkLoadBalancerId: id},
	}

	sawNlbType := false
	for fm, e := range m {
		if e.Public || e.Extract == nil {
			continue
		}
		req, ok := reqFor[fm]
		if !ok {
			// Stream/cluster-floor RPC (Subscribe) uses a static extractor with a
			// nil request; skip typed-request table but still check its object type.
			gotType, _, err := e.Extract(nil)
			require.NoError(t, err, "static extractor for %s", fm)
			require.Falsef(t, strings.HasPrefix(gotType, legacyPrefix),
				"RPC %s resolves legacy object_type %q — rename incomplete", fm, gotType)
			continue
		}
		gotType, _, err := e.Extract(req)
		require.NoErrorf(t, err, "extract %s", fm)
		require.Falsef(t, strings.HasPrefix(gotType, legacyPrefix),
			"RPC %s resolves legacy object_type %q — rename incomplete (no dangling lb_* route)", fm, gotType)
		if strings.HasPrefix(gotType, "nlb_") {
			sawNlbType = true
			// Positive lock: the three renamed types are the only nlb_* forms.
			require.Contains(t, []string{wantTypeLoadBalancer, wantTypeListener, wantTypeTargetGroup}, gotType,
				"unexpected nlb_* object type %q for %s", gotType, fm)
		}
	}
	require.True(t, sawNlbType,
		"expected at least one RPC to resolve a renamed nlb_* object type (interceptor keys on nlb_*)")
}

// TestNLB1a05_InterceptorSendsNlbObjectToFGA — verifies NLB-1a-05 at the wire of the
// FGA Check: the object string handed to InternalIAMService.Check for an object-self
// mutation is `nlb_network_load_balancer:<id>` (renamed), proving the interceptor —
// not just a constant — keys on the new token end-to-end.
func TestNLB1a05_InterceptorSendsNlbObjectToFGA(t *testing.T) {
	intr, count, calls := newTestInterceptor(t,
		func(_ context.Context, _, _, _ string) (bool, error) { return true, nil })

	const lbID = "nlb-abc123"
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Update"}
	_, err := intr.Unary()(
		principalCtx("user", "usr_alice"),
		&lbv1.UpdateNetworkLoadBalancerRequest{NetworkLoadBalancerId: lbID},
		info,
		func(ctx context.Context, req any) (any, error) { return "ok", nil },
	)
	require.NoError(t, err)
	require.Equal(t, 1, *count, "exactly one FGA Check")
	require.Len(t, *calls, 1)
	require.Equal(t, "nlb_network_load_balancer:"+lbID, (*calls)[0].object,
		"interceptor must Check the renamed nlb_* object, not legacy lb_*")
}
