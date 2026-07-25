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

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/compute/internal/service"
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

// capImageScenario builds a store whose compute_image population exceeds the FGA
// cap and returns the handler plus the id of the tenant's OWN image — which sorts
// AFTER the cap boundary and is therefore the one truncation erases.
//
// realID is the ONLY image that exists as a row; the 1000 filler ids are
// grant-store objects of the same type belonging to the rest of the (long-lived)
// cluster. That is the real-world shape: a handful of rows in the project, >1000
// objects of the type in the store.
func capImageScenario(t *testing.T) (*ImageHandler, *cappedAuthorizeClient, string) {
	t.Helper()

	// "epd-img-zzzowned" sorts after every "epd-img-fill…" filler → cut by the cap.
	const realID = "epd-img-zzzowned"

	granted := make([]string, 0, fgaListObjectsCap+1)
	for i := 0; i < fgaListObjectsCap; i++ {
		granted = append(granted, fmt.Sprintf("epd-img-fill%06d", i))
	}
	granted = append(granted, realID)

	cli := newCappedAuthorizeClient(granted...)
	h, imgRepo := newImageHandlerWithFilter(t, newFilter(t, cli))
	imgRepo.Seed(&domain.Image{
		ID: realID, ProjectID: "proj", Name: "own", Family: "ubuntu",
		Status: domain.ImageStatusReady,
	})
	return h, cli, realID
}

// GetLatestByFamily of the tenant's OWN image must return it. Before the fix the
// enumeration truncates the id away and the RPC answers NotFound for a row that
// exists and is granted — the reported defect, at the observable (gRPC) level.
func TestImageGetLatestByFamily_OwnResourceBeyondFGAListObjectsCap(t *testing.T) {
	h, _, realID := capImageScenario(t)

	got, err := h.GetLatestByFamily(ctxWithSubject("user:usr_alice"),
		&computev1.GetImageLatestByFamilyRequest{ProjectId: "proj", Family: "ubuntu"})

	require.NoError(t, err, "own, granted, existing image must be readable; "+
		"NotFound here means visibility is gated on the truncated ListObjects enumeration")
	require.NotNil(t, got)
	assert.Equal(t, realID, got.GetId())
}

// List must contain the tenant's OWN image. Before the fix the row is filtered
// out because its id is not in the truncated enumeration.
func TestImageList_OwnResourceBeyondFGAListObjectsCap(t *testing.T) {
	h, _, realID := capImageScenario(t)

	resp, err := h.List(ctxWithSubject("user:usr_alice"),
		&computev1.ListImagesRequest{ProjectId: "proj"})

	require.NoError(t, err)
	ids := make([]string, 0, len(resp.GetImages()))
	for _, im := range resp.GetImages() {
		ids = append(ids, im.GetId())
	}
	assert.Contains(t, ids, realID, "own, granted, existing image must appear in List; "+
		"absence here means the page is filtered by the truncated ListObjects enumeration")
}

// Cost regression: List must resolve visibility from the PAGE, never by
// enumerating every object the subject may see. Enumeration is both the source of
// the cap defect and O(universe) per call.
func TestImageList_DoesNotEnumerateUniverse(t *testing.T) {
	h, cli, _ := capImageScenario(t)

	_, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListImagesRequest{ProjectId: "proj"})
	require.NoError(t, err)

	assert.Zero(t, cli.listObjectsCalls.Load(),
		"List must not call AuthorizeService.ListObjects (O(universe), capped at 1000)")
	assert.LessOrEqual(t, cli.batchCheckedIDs.Load(), int64(1),
		"visibility must be checked for the rows on the page only (1 row seeded)")
}

// GetLatestByFamily resolves ONE image — it must ask the direct per-object
// question about that id, never enumerate the type.
func TestImageGetLatestByFamily_DoesNotEnumerateUniverse(t *testing.T) {
	h, cli, _ := capImageScenario(t)

	_, err := h.GetLatestByFamily(ctxWithSubject("user:usr_alice"),
		&computev1.GetImageLatestByFamilyRequest{ProjectId: "proj", Family: "ubuntu"})
	require.NoError(t, err)

	assert.Zero(t, cli.listObjectsCalls.Load(),
		"GetLatestByFamily must not call AuthorizeService.ListObjects (capped enumeration)")
	assert.LessOrEqual(t, cli.batchCheckedIDs.Load(), int64(1),
		"exactly the resolved image id is checked")
}

// No weakening: an existing row the subject was never granted stays absent from
// List, and GetLatestByFamily on it keeps hiding existence. This is the guard that
// stops the fix from "solving" truncation by simply showing everything.
func TestImageList_UngrantedResourceStaysInvisible(t *testing.T) {
	cli := newCappedAuthorizeClient("epd-img-granted")
	h, imgRepo := newImageHandlerWithFilter(t, newFilter(t, cli))
	imgRepo.Seed(&domain.Image{
		ID: "epd-img-granted", ProjectID: "proj", Name: "granted", Family: "ubuntu",
		Status: domain.ImageStatusReady,
	})
	imgRepo.Seed(&domain.Image{
		ID: "epd-img-secret", ProjectID: "proj", Name: "secret", Family: "debian",
		Status: domain.ImageStatusReady,
	})

	resp, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListImagesRequest{ProjectId: "proj"})
	require.NoError(t, err)
	ids := make([]string, 0, len(resp.GetImages()))
	for _, im := range resp.GetImages() {
		ids = append(ids, im.GetId())
	}
	assert.Contains(t, ids, "epd-img-granted")
	assert.NotContains(t, ids, "epd-img-secret", "ungranted image must never appear in List")

	// Read-by-id-equivalent on the ungranted image must hide existence, not leak it.
	_, err = h.GetLatestByFamily(ctxWithSubject("user:usr_alice"),
		&computev1.GetImageLatestByFamilyRequest{ProjectId: "proj", Family: "debian"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Equal(t, "Image debian not found", status.Convert(err).Message(),
		"denial must be indistinguishable from a genuinely empty family (no existence oracle)")
}

// Every public List path (Disk / Image / Snapshot / Instance) shares the same
// contract: page first, then ask per-object. None may enumerate the type.
func TestAllPublicLists_DoNotEnumerateUniverse(t *testing.T) {
	t.Run("disk", func(t *testing.T) {
		cli := newCappedAuthorizeClient()
		h, ops := setupDiskHandler(t, newFilter(t, cli))
		createDisks(t, h, ops, "proj", "d1")
		_, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListDisksRequest{ProjectId: "proj"})
		require.NoError(t, err)
		assert.Zero(t, cli.listObjectsCalls.Load())
	})
	t.Run("image", func(t *testing.T) {
		cli := newCappedAuthorizeClient()
		h, imgRepo := newImageHandlerWithFilter(t, newFilter(t, cli))
		imgRepo.Seed(&domain.Image{ID: "epd-img-1", ProjectID: "proj", Name: "i", Family: "f", Status: domain.ImageStatusReady})
		_, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListImagesRequest{ProjectId: "proj"})
		require.NoError(t, err)
		assert.Zero(t, cli.listObjectsCalls.Load())
	})
	t.Run("snapshot", func(t *testing.T) {
		cli := newCappedAuthorizeClient()
		snapSvc := service.NewSnapshotService(portmock.NewSnapshotRepo(), portmock.NewDiskRepo(),
			&portmock.ProjectClient{OK: true}, portmock.NewOpsRepo())
		h := NewSnapshotHandler(snapSvc, newFilter(t, cli))
		_, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListSnapshotsRequest{ProjectId: "proj"})
		require.NoError(t, err)
		assert.Zero(t, cli.listObjectsCalls.Load())
	})
	t.Run("instance", func(t *testing.T) {
		cli := newCappedAuthorizeClient()
		insSvc := service.NewInstanceService(
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
