// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

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
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// ─────────────────────────────────────────────────────────────────────────────
// Regression: OpenFGA ListObjects hard cap silently truncates a tenant's OWN
// resources out of existence.
//
// OpenFGA bounds ListObjects server-side (OPENFGA_LIST_OBJECTS_MAX_RESULTS,
// default 1000) and offers NO continuation token. Any authz path shaped as
// "enumerate every id the subject may see, then match/filter against it" is
// therefore capped at 1000 objects PER TYPE PER STORE — cluster-wide, not
// per-tenant. On a long-lived store the type's object population exceeds the
// cap and the enumeration returns an arbitrary 1000-id prefix; a tenant's own
// resource that falls outside that prefix becomes permanently invisible:
// Get → 404 and List → absent, while the row exists, the grant exists, and
// Update/Delete (which ask a DIRECT per-object question) keep working.
//
// The fake below reproduces exactly that asymmetry at the kacho-iam transport
// boundary — the boundary is what the defect lives at, so the SAME test body
// holds before and after the fix:
//   - ListObjects  → the truncating enumeration (what OpenFGA really does).
//   - BatchCheck   → the honest per-object oracle (same grant set, no cap).
//
// Both answer from ONE authoritative `granted` set, so the test can never pass
// by weakening authorization: an id absent from `granted` is denied by both.
// ─────────────────────────────────────────────────────────────────────────────

// fgaCap mirrors OpenFGA's default OPENFGA_LIST_OBJECTS_MAX_RESULTS.
const fgaCap = 1000

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

// ListObjects returns the truncated 1000-id prefix, exactly as OpenFGA does:
// the full authorised set sorted, then cut at the hard cap. No page token —
// OpenFGA's ListObjects has none, which is why the truncation is silent.
func (c *cappedAuthorizeClient) ListObjects(_ context.Context, _ *iamv1.ListObjectsRequest, _ ...grpc.CallOption) (*iamv1.ListObjectsResponse, error) {
	c.listObjectsCalls.Add(1)
	ids := make([]string, 0, len(c.granted))
	for id := range c.granted {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	truncated := len(ids) > fgaCap
	if truncated {
		ids = ids[:fgaCap]
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

// capScenario builds a store whose route-table population exceeds the FGA cap,
// and returns the handler plus the id of the tenant's OWN route table — which
// sorts AFTER the cap boundary and is therefore the one truncation erases.
//
// realID is the ONLY route table that exists as a row; the 1000 filler ids are
// grant-store objects of the same type belonging to the rest of the (long-lived)
// cluster. That is the real-world shape: 177 rows in the project, >1000 objects
// of the type in the store.
func capScenario(t *testing.T) (*Handler, *cappedAuthorizeClient, string) {
	t.Helper()

	const projectID = "prj_1"
	// "rtb_z…" sorts after every "rtb_fill…" filler → cut by the cap.
	const realID = "rtb_zzzowned"

	granted := make([]string, 0, fgaCap+1)
	for i := 0; i < fgaCap; i++ {
		granted = append(granted, fmt.Sprintf("rtb_fill%06d", i))
	}
	granted = append(granted, realID)

	cli := newCappedAuthorizeClient(granted...)
	filter := authzfilter.NewFGAFilter(cli, authzfilter.Config{
		Enabled:         true,
		CacheMaxEntries: 10000,
	})

	kr := kachomock.NewRepository()
	seedRouteTablesLabeled(t, kr, projectID, "enp_net1", realID)

	port := authzfilter.AsPort(filter)
	h := NewHandler(nil, nil, nil,
		NewGetRouteTableUseCase(kr),
		NewListRouteTablesUseCase(kr, port),
		nil)
	return h, cli, realID
}

// capScenarioCtx — an authenticated tenant principal (the grantee).
func capScenarioCtx() context.Context {
	return operations.WithPrincipal(context.Background(), operations.Principal{
		Type: "user", ID: "usr_alice",
	})
}

// Get of the tenant's OWN route table must return it. Before the fix the
// enumeration truncates the id away and Get answers 404 for a row that exists
// and is granted — the reported defect, at the observable (gRPC) level.
func TestRouteTableGet_OwnResourceBeyondFGAListObjectsCap(t *testing.T) {
	h, _, realID := capScenario(t)

	got, err := h.Get(capScenarioCtx(), &vpcv1.GetRouteTableRequest{RouteTableId: realID})

	require.NoError(t, err, "own, granted, existing route table must be readable; "+
		"404 here means visibility is gated on the truncated ListObjects enumeration")
	require.NotNil(t, got)
	assert.Equal(t, realID, got.GetId())
}

// List must contain the tenant's OWN route table. Before the fix the row is
// filtered out because its id is not in the truncated enumeration.
func TestRouteTableList_OwnResourceBeyondFGAListObjectsCap(t *testing.T) {
	h, _, realID := capScenario(t)

	resp, err := h.List(capScenarioCtx(), &vpcv1.ListRouteTablesRequest{ProjectId: "prj_1"})

	require.NoError(t, err)
	ids := make([]string, 0, len(resp.GetRouteTables()))
	for _, rt := range resp.GetRouteTables() {
		ids = append(ids, rt.GetId())
	}
	assert.Contains(t, ids, realID, "own, granted, existing route table must appear in List; "+
		"absence here means the page is filtered by the truncated ListObjects enumeration")
}

// Cost regression: List must resolve visibility from the PAGE, never by
// enumerating every object the subject may see. Enumeration is both the source
// of the cap defect and O(universe) per call.
func TestRouteTableList_DoesNotEnumerateUniverse(t *testing.T) {
	h, cli, _ := capScenario(t)

	_, err := h.List(capScenarioCtx(), &vpcv1.ListRouteTablesRequest{ProjectId: "prj_1"})
	require.NoError(t, err)

	assert.Zero(t, cli.listObjectsCalls.Load(),
		"List must not call AuthorizeService.ListObjects (O(universe), capped at 1000)")
	assert.LessOrEqual(t, cli.batchCheckedIDs.Load(), int64(1),
		"visibility must be checked for the rows on the page only (1 row seeded)")
}

// No weakening: an existing row the subject was never granted stays absent from
// List. This is the guard that stops the fix from "solving" truncation by simply
// showing everything.
//
// The Get counterpart is enforced one layer up and is locked there, not here:
// read-by-id authz is the per-RPC interceptor's direct per-object Check, whose
// deny-on-existing → NOT_FOUND (handler never runs) is covered by
// pkg/authz.TestInterceptor_HideExistenceMapsToNotFound, the ErrHideExistence
// signal by check.TestCheckClient* (check_client_existence_test.go), and the
// premise that Get actually goes through that Check by
// check.TestGetRPCsCarryPerObjectCheck.
func TestRouteTableList_UngrantedResourceStaysInvisible(t *testing.T) {
	const projectID = "prj_1"
	cli := newCappedAuthorizeClient("rtb_granted")
	filter := authzfilter.NewFGAFilter(cli, authzfilter.Config{
		Enabled: true, CacheMaxEntries: 10000,
	})
	kr := kachomock.NewRepository()
	seedRouteTablesLabeled(t, kr, projectID, "enp_net1", "rtb_granted", "rtb_secret")

	h := NewHandler(nil, nil, nil,
		NewGetRouteTableUseCase(kr),
		NewListRouteTablesUseCase(kr, authzfilter.AsPort(filter)),
		nil)

	resp, err := h.List(capScenarioCtx(), &vpcv1.ListRouteTablesRequest{ProjectId: projectID})
	require.NoError(t, err)
	ids := make([]string, 0, len(resp.GetRouteTables()))
	for _, rt := range resp.GetRouteTables() {
		ids = append(ids, rt.GetId())
	}
	assert.Contains(t, ids, "rtb_granted")
	assert.NotContains(t, ids, "rtb_secret", "ungranted resource must never appear in List")
}
