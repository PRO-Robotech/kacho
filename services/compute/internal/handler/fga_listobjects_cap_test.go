// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

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

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/instance"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
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
// resource outside that prefix becomes permanently invisible: List → absent and
// GetLatestByFamily → NotFound, while the row exists, the grant exists, and
// Update/Delete (which ask a DIRECT per-object question through the per-RPC
// interceptor) keep working. Asking for max_results=10000 does NOT widen it —
// that is only a client-side trim of an already-truncated answer.
//
// The fake below reproduces exactly that asymmetry at the kacho-iam transport
// boundary — the boundary is where the defect lives, so the SAME test body holds
// before and after the fix:
//   - ListObjects  → the truncating enumeration (what OpenFGA really does).
//   - BatchCheck   → the honest per-object oracle (same grant set, no cap).
//
// Both answer from ONE authoritative `granted` set, so the test can never pass
// by weakening authorization: an id absent from `granted` is denied by both.
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

// capInstanceScenario builds a store whose compute_instance population exceeds the
// FGA cap and returns the handler plus the id of the tenant's OWN instance — which
// sorts AFTER the cap boundary and is therefore the one truncation erases.
//
// realID is the ONLY instance that exists as a row; the 1000 filler ids are
// grant-store objects of the same type belonging to the rest of the (long-lived)
// cluster. That is the real-world shape: a handful of rows in the project, >1000
// objects of the type in the store.
//
// The regression was originally written over compute's own Image resource. Image
// is retired (kacho-storage owns block storage), but the defect belongs to the
// shared list-filter rather than to any one resource, so the scenario moved to
// Instance instead of leaving with it.
func capInstanceScenario(t *testing.T) (*InstanceHandler, *cappedAuthorizeClient, string) {
	t.Helper()

	// "ins-zzzowned" sorts after every "ins-fill…" filler → cut by the cap.
	const realID = "ins-zzzowned"

	granted := make([]string, 0, fgaListObjectsCap+1)
	for i := 0; i < fgaListObjectsCap; i++ {
		granted = append(granted, fmt.Sprintf("ins-fill%06d", i))
	}
	granted = append(granted, realID)

	cli := newCappedAuthorizeClient(granted...)
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	insRepo.Seed(&domain.Instance{
		ID: realID, ProjectID: "proj", Name: "own",
		Status: domain.InstanceStatusRunning,
	})
	return h, cli, realID
}

// List must contain the tenant's OWN instance. Before the fix the row is filtered
// out because its id is not in the truncated enumeration.
func TestInstanceList_OwnResourceBeyondFGAListObjectsCap(t *testing.T) {
	h, _, realID := capInstanceScenario(t)

	resp, err := h.List(ctxWithSubject("user:usr_alice"),
		&computev1.ListInstancesRequest{ProjectId: "proj"})

	require.NoError(t, err)
	ids := make([]string, 0, len(resp.GetInstances()))
	for _, im := range resp.GetInstances() {
		ids = append(ids, im.GetId())
	}
	assert.Contains(t, ids, realID, "own, granted, existing instance must appear in List; "+
		"absence here means the page is filtered by the truncated ListObjects enumeration")
}

// Cost regression: List must resolve visibility from the PAGE, never by
// enumerating every object the subject may see. Enumeration is both the source of
// the cap defect and O(universe) per call.
func TestInstanceList_DoesNotEnumerateUniverse(t *testing.T) {
	h, cli, _ := capInstanceScenario(t)

	_, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err)

	assert.Zero(t, cli.listObjectsCalls.Load(),
		"List must not call AuthorizeService.ListObjects (O(universe), capped at 1000)")
	assert.LessOrEqual(t, cli.batchCheckedIDs.Load(), int64(1),
		"visibility must be checked for the rows on the page only (1 row seeded)")
}

// No weakening: an existing row the subject was never granted stays absent from
// List. This is the guard that stops the fix from "solving" truncation by simply
// showing everything.
func TestInstanceList_UngrantedResourceStaysInvisible(t *testing.T) {
	cli := newCappedAuthorizeClient("ins-granted")
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	insRepo.Seed(&domain.Instance{
		ID: "ins-granted", ProjectID: "proj", Name: "granted",
		Status: domain.InstanceStatusRunning,
	})
	insRepo.Seed(&domain.Instance{
		ID: "ins-secret", ProjectID: "proj", Name: "secret",
		Status: domain.InstanceStatusRunning,
	})

	resp, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err)
	ids := make([]string, 0, len(resp.GetInstances()))
	for _, im := range resp.GetInstances() {
		ids = append(ids, im.GetId())
	}
	assert.Contains(t, ids, "ins-granted")
	assert.NotContains(t, ids, "ins-secret", "ungranted instance must never appear in List")
}

// The public List path shares one contract: page first, then ask per-object. It
// may never enumerate the type. Disk/Image/Snapshot arms went with the retired
// block-storage duplicates; kacho-storage carries the same guard for its own.
func TestAllPublicLists_DoNotEnumerateUniverse(t *testing.T) {
	t.Run("instance", func(t *testing.T) {
		cli := newCappedAuthorizeClient()
		insSvc := instance.NewInstanceService(
			portmock.NewInstanceRepo(), portmock.NewMachineTypeRepo(), portmock.NewZoneRegistry(),
			portmock.NewSubnetRegistry(), &portmock.ProjectClient{OK: true},
			portmock.NewNicClient(), portmock.NewStorageClient(), portmock.NewOpsRepo(),
		)
		h := NewInstanceHandler(insSvc, newFilter(t, cli))
		_, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
		require.NoError(t, err)
		assert.Zero(t, cli.listObjectsCalls.Load())
	})
}
