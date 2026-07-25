// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
	"time"

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

// Regression: the OpenFGA ListObjects hard cap (OPENFGA_LIST_OBJECTS_MAX_RESULTS,
// default 1000, NO continuation token) silently truncates a tenant's OWN listeners
// out of List. Measured live: `nlb_listener` = 5208 tuples — 5× past the cap.
// Full rationale: loadbalancer/fga_listobjects_cap_test.go.
//
// The fake answers BOTH shapes from ONE authoritative grant set, so the test can
// never pass by weakening authorization:
//   - ListObjects → truncating enumeration (what OpenFGA really does);
//   - BatchCheck  → honest per-object oracle (no cap).

const fgaListObjectsCap = 1000

type cappedAuthorizeClient struct {
	granted          map[string]bool
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

func (c *cappedAuthorizeClient) BatchCheck(_ context.Context, in *iamv1.BatchAuthorizeCheckRequest, _ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	if n := len(in.GetChecks()); n > 100 {
		return nil, status.Errorf(codes.InvalidArgument, "Illegal argument checks: batch size %d > 100", n)
	}
	out := &iamv1.BatchAuthorizeCheckResponse{
		Responses: make([]*iamv1.AuthorizeCheckResponse, len(in.GetChecks())),
	}
	for i, chk := range in.GetChecks() {
		c.batchCheckedIDs.Add(1)
		out.Responses[i] = &iamv1.AuthorizeCheckResponse{Allowed: c.granted[chk.GetResource().GetId()]}
	}
	return out, nil
}

// seedListenerWithID seeds a listener under an EXPLICIT id — this regression turns
// on where the id sorts relative to the cap boundary.
func seedListenerWithID(repo *fakeRepo, projectID, lbID, id, name string) {
	repo.seedListener(&kachorepo.ListenerRecord{
		Listener: domain.Listener{
			ID:             domain.ResourceID(id),
			LoadBalancerID: domain.ResourceID(lbID),
			ProjectID:      domain.ProjectID(projectID),
			RegionID:       "ru-central1",
			Name:           domain.LbName(name),
			Labels:         domain.LbLabels{},
			Protocol:       domain.ProtoTCP,
			Port:           80,
			TargetPort:     80,
			Status:         domain.ListenerStatusActive,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
}

// lsnCapScenario: >1000 grant-store objects of the type; the tenant's OWN listener
// sorts AFTER the cap boundary and is the one truncation erases.
func lsnCapScenario(t *testing.T) (*ListUseCase, *cappedAuthorizeClient, string) {
	t.Helper()

	const realID = "lsn-zzzowned"
	granted := make([]string, 0, fgaListObjectsCap+1)
	for i := 0; i < fgaListObjectsCap; i++ {
		granted = append(granted, fmt.Sprintf("lsn-fill%06d", i))
	}
	granted = append(granted, realID)

	cli := newCappedAuthorizeClient(granted...)
	filter := authzfilter.NewFGAFilter(cli, authzfilter.Config{Enabled: true, CacheMaxEntries: 10000})

	repo := newFakeRepo()
	seedListenerWithID(repo, "prj-a", "nlb_lb1", realID, "l-owned")

	return NewListUseCase(repo, filter), cli, realID
}

func TestListListeners_OwnResourceBeyondFGAListObjectsCap(t *testing.T) {
	uc, _, realID := lsnCapScenario(t)

	resp, err := uc.Run(ctxWithUser("usr_alice"), &lbv1.ListListenersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)

	ids := make([]string, 0, len(resp.GetListeners()))
	for _, l := range resp.GetListeners() {
		ids = append(ids, l.GetId())
	}
	assert.Contains(t, ids, realID, "own, granted, existing listener must appear in List; "+
		"absence here means the page is filtered by the truncated ListObjects enumeration")
}

func TestListListeners_DoesNotEnumerateUniverse(t *testing.T) {
	uc, cli, _ := lsnCapScenario(t)

	_, err := uc.Run(ctxWithUser("usr_alice"), &lbv1.ListListenersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)

	assert.Zero(t, cli.listObjectsCalls.Load(),
		"List must not call AuthorizeService.ListObjects (O(universe), capped at 1000)")
	assert.LessOrEqual(t, cli.batchCheckedIDs.Load(), int64(1),
		"visibility must be checked for the rows on the page only (1 row seeded)")
}

// No weakening: an ungranted row stays invisible.
func TestListListeners_UngrantedResourceStaysInvisible(t *testing.T) {
	cli := newCappedAuthorizeClient("lsn-granted")
	filter := authzfilter.NewFGAFilter(cli, authzfilter.Config{Enabled: true, CacheMaxEntries: 10000})
	repo := newFakeRepo()
	seedListenerWithID(repo, "prj-a", "nlb_lb1", "lsn-granted", "l-granted")
	seedListenerWithID(repo, "prj-a", "nlb_lb1", "lsn-secret", "l-secret")

	uc := NewListUseCase(repo, filter)
	resp, err := uc.Run(ctxWithUser("usr_alice"), &lbv1.ListListenersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)

	ids := make([]string, 0, len(resp.GetListeners()))
	for _, l := range resp.GetListeners() {
		ids = append(ids, l.GetId())
	}
	assert.Contains(t, ids, "lsn-granted")
	assert.NotContains(t, ids, "lsn-secret", "ungranted resource must never appear in List")
}
