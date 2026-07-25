// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// ─────────────────────────────────────────────────────────────────────────────
// Regression: the OpenFGA ListObjects hard cap silently truncates a tenant's OWN
// load balancers out of existence.
//
// OpenFGA bounds ListObjects server-side (OPENFGA_LIST_OBJECTS_MAX_RESULTS,
// default 1000) and offers NO continuation token. Any authz path shaped as
// "enumerate every id the subject may see, then filter the DB with it" is
// therefore capped at 1000 objects PER TYPE PER STORE — cluster-wide, not
// per-tenant. On a long-lived store the type's population exceeds the cap and the
// enumeration returns an arbitrary 1000-id prefix; a tenant's own load balancer
// that falls outside that prefix becomes permanently invisible in List, while the
// row exists, the grant exists, and Get/Update/Delete (which ask a DIRECT
// per-object question through the interceptor) keep working. Measured live:
// `nlb_network_load_balancer` = 20739 tuples, `nlb_target_group` = 10447,
// `nlb_listener` = 5208 — all far past the cap.
//
// The fake below reproduces exactly that asymmetry at the kacho-iam transport
// boundary — the boundary the defect lives at, so the SAME test body holds before
// and after the fix:
//   - ListObjects  → the truncating enumeration (what OpenFGA really does).
//   - BatchCheck   → the honest per-object oracle (same grant set, no cap).
//
// Both answer from ONE authoritative `granted` set, so the test can never pass by
// weakening authorization: an id absent from `granted` is denied by both.
// ─────────────────────────────────────────────────────────────────────────────

// fgaListObjectsCap mirrors OpenFGA's default OPENFGA_LIST_OBJECTS_MAX_RESULTS.
const fgaListObjectsCap = 1000

// cappedAuthorizeClient — fake kacho-iam AuthorizeService.
type cappedAuthorizeClient struct {
	// granted — the authoritative truth: ids this subject genuinely may see.
	granted map[string]bool
	// listObjectsCalls / batchCheckedIDs — cost observability for the
	// "List must not enumerate the universe" regression.
	listObjectsCalls atomic.Int64
	batchCheckedIDs  atomic.Int64
}

func newCappedAuthorizeClient(granted ...string) *cappedAuthorizeClient {
	g := make(map[string]bool, len(granted))
	for _, id := range granted {
		g[id] = true
	}
	return &cappedAuthorizeClient{granted: g}
}

// ListObjects returns the truncated 1000-id prefix, exactly as OpenFGA does: the
// full authorised set sorted, then cut at the hard cap. No page token — OpenFGA's
// ListObjects has none, which is why the truncation is silent.
func (c *cappedAuthorizeClient) ListObjects(_ context.Context, _ *iamv1.ListObjectsRequest, _ ...grpc.CallOption) (*iamv1.ListObjectsResponse, error) {
	c.listObjectsCalls.Add(1)
	ids := make([]string, 0, len(c.granted))
	for id := range c.granted {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	truncated := len(ids) > fgaListObjectsCap
	if truncated {
		ids = ids[:fgaListObjectsCap]
	}
	return &iamv1.ListObjectsResponse{ResourceIds: ids, Truncated: truncated}, nil
}

// BatchCheck answers each (subject, object) DIRECTLY from the same authoritative
// set — no cap, no enumeration. Order-preserving, as the RPC contract requires.
func (c *cappedAuthorizeClient) BatchCheck(_ context.Context, in *iamv1.BatchAuthorizeCheckRequest, _ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	if n := len(in.GetChecks()); n > 100 {
		return nil, status.Errorf(codes.InvalidArgument, "Illegal argument checks: batch size %d > 100", n)
	}
	out := &iamv1.BatchAuthorizeCheckResponse{
		Responses: make([]*iamv1.AuthorizeCheckResponse, len(in.GetChecks())),
	}
	for i, chk := range in.GetChecks() {
		c.batchCheckedIDs.Add(1)
		out.Responses[i] = &iamv1.AuthorizeCheckResponse{
			Allowed: c.granted[chk.GetResource().GetId()],
		}
	}
	return out, nil
}

// seedLBWithID seeds a load balancer under an EXPLICIT id (the stock seedLB mints
// a random one, and this regression turns on where the id sorts relative to the
// cap boundary).
func seedLBWithID(repo *fakeRepo, projectID, id, name string) {
	repo.lbs[id] = &kachorepo.LoadBalancerRecord{
		LoadBalancer: domain.LoadBalancer{
			ID: domain.ResourceID(id), ProjectID: domain.ProjectID(projectID),
			RegionID: "ru-central1", Name: domain.LbName(name),
			Type: domain.LBTypeExternal, Status: domain.LBStatusInactive,
			SessionAffinity: domain.SessionAffinity5Tuple,
			AdminState:      domain.AdminStateEnabled,
		},
	}
}

// lbCapScenario builds a store whose load-balancer population exceeds the FGA cap
// and returns the List use-case plus the id of the tenant's OWN load balancer —
// which sorts AFTER the cap boundary and is therefore the one truncation erases.
//
// realID is the ONLY load balancer that exists as a row; the 1000 filler ids are
// grant-store objects of the same type belonging to the rest of the (long-lived)
// cluster. That is the real-world shape: a handful of rows in the project, >1000
// objects of the type in the store.
func lbCapScenario(t *testing.T) (*ListLoadBalancersUseCase, *cappedAuthorizeClient, string) {
	t.Helper()

	// "nlb-zzzowned" sorts after every "nlb-fill…" filler → cut by the cap.
	const realID = "nlb-zzzowned"

	granted := make([]string, 0, fgaListObjectsCap+1)
	for i := 0; i < fgaListObjectsCap; i++ {
		granted = append(granted, fmt.Sprintf("nlb-fill%06d", i))
	}
	granted = append(granted, realID)

	cli := newCappedAuthorizeClient(granted...)
	filter := authzfilter.NewFGAFilter(cli, authzfilter.Config{
		Enabled:         true,
		CacheMaxEntries: 10000,
	})

	repo := newFakeRepo()
	seedLBWithID(repo, "prj-a", realID, "lb-owned")

	return NewListLoadBalancersUseCase(repo, filter), cli, realID
}

// List must contain the tenant's OWN load balancer. Before the fix the row is
// filtered out because its id is not in the truncated enumeration.
func TestListLoadBalancers_OwnResourceBeyondFGAListObjectsCap(t *testing.T) {
	uc, _, realID := lbCapScenario(t)

	resp, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})

	require.NoError(t, err)
	ids := make([]string, 0, len(resp.GetNetworkLoadBalancers()))
	for _, lb := range resp.GetNetworkLoadBalancers() {
		ids = append(ids, lb.GetId())
	}
	assert.Contains(t, ids, realID, "own, granted, existing load balancer must appear in List; "+
		"absence here means the page is filtered by the truncated ListObjects enumeration")
}

// Cost regression: List must resolve visibility from the PAGE, never by
// enumerating every object the subject may see. Enumeration is both the source of
// the cap defect and O(universe) per call.
func TestListLoadBalancers_DoesNotEnumerateUniverse(t *testing.T) {
	uc, cli, _ := lbCapScenario(t)

	_, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)

	assert.Zero(t, cli.listObjectsCalls.Load(),
		"List must not call AuthorizeService.ListObjects (O(universe), capped at 1000)")
	assert.LessOrEqual(t, cli.batchCheckedIDs.Load(), int64(1),
		"visibility must be checked for the rows on the page only (1 row seeded)")
}

// No weakening: an existing row the subject was never granted stays absent from
// List. This is the guard that stops the fix from "solving" truncation by simply
// showing everything.
func TestListLoadBalancers_UngrantedResourceStaysInvisible(t *testing.T) {
	cli := newCappedAuthorizeClient("nlb-granted")
	filter := authzfilter.NewFGAFilter(cli, authzfilter.Config{
		Enabled: true, CacheMaxEntries: 10000,
	})
	repo := newFakeRepo()
	seedLBWithID(repo, "prj-a", "nlb-granted", "lb-granted")
	seedLBWithID(repo, "prj-a", "nlb-secret", "lb-secret")

	uc := NewListLoadBalancersUseCase(repo, filter)
	resp, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)

	ids := make([]string, 0, len(resp.GetNetworkLoadBalancers()))
	for _, lb := range resp.GetNetworkLoadBalancers() {
		ids = append(ids, lb.GetId())
	}
	assert.Contains(t, ids, "nlb-granted")
	assert.NotContains(t, ids, "nlb-secret", "ungranted resource must never appear in List")
}
