// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

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
)

// Regression: the OpenFGA ListObjects hard cap (OPENFGA_LIST_OBJECTS_MAX_RESULTS,
// default 1000, NO continuation token) silently truncates a tenant's OWN target
// groups out of List. Measured live: `nlb_target_group` = 10447 tuples — 10× past
// the cap. Full rationale: loadbalancer/fga_listobjects_cap_test.go.
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

// seedTGWithID seeds a target group under an EXPLICIT id — this regression turns
// on where the id sorts relative to the cap boundary.
func seedTGWithID(repo *fakeRepo, projectID, id, name string) {
	rec := makeTG(projectID, name)
	rec.ID = domain.ResourceID(id)
	repo.seedTG(rec)
}

// tgCapScenario: >1000 grant-store objects of the type; the tenant's OWN target
// group sorts AFTER the cap boundary and is the one truncation erases.
func tgCapScenario(t *testing.T) (*ListTargetGroupsUseCase, *cappedAuthorizeClient, string) {
	t.Helper()

	const realID = "tgp-zzzowned"
	granted := make([]string, 0, fgaListObjectsCap+1)
	for i := 0; i < fgaListObjectsCap; i++ {
		granted = append(granted, fmt.Sprintf("tgp-fill%06d", i))
	}
	granted = append(granted, realID)

	cli := newCappedAuthorizeClient(granted...)
	filter := authzfilter.NewFGAFilter(cli, authzfilter.Config{Enabled: true, CacheMaxEntries: 10000})

	repo := newFakeRepo()
	seedTGWithID(repo, "prj-a", realID, "tg-owned")

	return NewListTargetGroupsUseCase(repo, filter), cli, realID
}

func TestListTargetGroups_OwnResourceBeyondFGAListObjectsCap(t *testing.T) {
	uc, _, realID := tgCapScenario(t)

	resp, err := uc.Execute(ctxWithUser("usr_alice"), &lbv1.ListTargetGroupsRequest{ProjectId: "prj-a"})
	require.NoError(t, err)

	ids := make([]string, 0, len(resp.GetTargetGroups()))
	for _, tg := range resp.GetTargetGroups() {
		ids = append(ids, tg.GetId())
	}
	assert.Contains(t, ids, realID, "own, granted, existing target group must appear in List; "+
		"absence here means the page is filtered by the truncated ListObjects enumeration")
}

func TestListTargetGroups_DoesNotEnumerateUniverse(t *testing.T) {
	uc, cli, _ := tgCapScenario(t)

	_, err := uc.Execute(ctxWithUser("usr_alice"), &lbv1.ListTargetGroupsRequest{ProjectId: "prj-a"})
	require.NoError(t, err)

	assert.Zero(t, cli.listObjectsCalls.Load(),
		"List must not call AuthorizeService.ListObjects (O(universe), capped at 1000)")
	assert.LessOrEqual(t, cli.batchCheckedIDs.Load(), int64(1),
		"visibility must be checked for the rows on the page only (1 row seeded)")
}

// No weakening: an ungranted row stays invisible.
func TestListTargetGroups_UngrantedResourceStaysInvisible(t *testing.T) {
	cli := newCappedAuthorizeClient("tgp-granted")
	filter := authzfilter.NewFGAFilter(cli, authzfilter.Config{Enabled: true, CacheMaxEntries: 10000})
	repo := newFakeRepo()
	seedTGWithID(repo, "prj-a", "tgp-granted", "tg-granted")
	seedTGWithID(repo, "prj-a", "tgp-secret", "tg-secret")

	uc := NewListTargetGroupsUseCase(repo, filter)
	resp, err := uc.Execute(ctxWithUser("usr_alice"), &lbv1.ListTargetGroupsRequest{ProjectId: "prj-a"})
	require.NoError(t, err)

	ids := make([]string, 0, len(resp.GetTargetGroups()))
	for _, tg := range resp.GetTargetGroups() {
		ids = append(ids, tg.GetId())
	}
	assert.Contains(t, ids, "tgp-granted")
	assert.NotContains(t, ids, "tgp-secret", "ungranted resource must never appear in List")
}
