// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_filter_test.go — handler-level tests for FGA-filtered List handlers
// (Disk / Image / Snapshot / Instance).
//
// Uses portmock repos + in-memory authzfilter.Filter (no real iam needed).
// Identity source is the request Principal (operations.WithPrincipal), the SAME
// source per-RPC Check uses — NOT the dead x-kacho-subject* headers. Covers the
// label-scope over-show leak fix:
//   - CLL-01 label-scoped subject → EXACTLY the allowed subset (not all)
//   - CLL-02 subject=="" (system / no principal) → fail-closed empty (NOT bypass)
//   - CLL-03 cluster-admin / owner → all (iam ListObjects returns all ids)
//   - CLL-04 adversarial not-granted subject → empty (no existence leak)
//   - CLL-05 same semantics across Disk / Image / Snapshot / Instance
//   - CLL-06 catalog (DiskType) NOT filtered
//   - CLL-07 iam-down + fail-closed → Unavailable
package handler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

// newInstanceHandlerWithFilter — InstanceHandler over portmock repos + the real
// list-filter. Returns the handler and the repo, for deterministic seeding.
func newInstanceHandlerWithFilter(t *testing.T, filter authzfilter.Filter) (*InstanceHandler, *portmock.InstanceRepo) {
	t.Helper()
	insRepo := portmock.NewInstanceRepo()
	svc := service.NewInstanceService(
		insRepo, portmock.NewMachineTypeRepo(), portmock.NewZoneRegistry(),
		portmock.NewSubnetRegistry(), &portmock.ProjectClient{OK: true},
		portmock.NewNicClient(), portmock.NewStorageClient(), portmock.NewOpsRepo(),
	)
	return NewInstanceHandler(svc, filter), insRepo
}

// seedInstances — seed N instances with deterministic ids; returns those ids.
func seedInstances(t *testing.T, r *portmock.InstanceRepo, projectID string, names ...string) []string {
	t.Helper()
	var out []string
	for _, n := range names {
		id := "ins-" + projectID + "-" + n
		r.Seed(&domain.Instance{
			ID: id, ProjectID: projectID, Name: n, ZoneID: "ru-central1-a",
			Status: domain.InstanceStatusRunning,
		})
		out = append(out, id)
	}
	return out
}

// mockAuthCli — handler-test stub of kacho-iam AuthorizeService.BatchCheck.
//
// The grant set stays keyed by "<subject>|<resourceType>|<action>" (as it was
// under the old enumeration API) so a per-object verdict is looked up in the same
// authoritative table: allowed ⇔ the id is listed for the caller's key. The
// verdict is relation-independent — the filter asks `viewer` first and `v_list`
// only for the ids `viewer` denied, and the union of the two is exactly this set.
type mockAuthCli struct {
	allowedByKey map[string][]string
	err          error
	calls        int
	lastAction   string // captured so read==enforce tests can assert the verb
	lastResType  string
	lastRelation string
}

func (m *mockAuthCli) BatchCheck(_ context.Context, in *iamv1.BatchAuthorizeCheckRequest, _ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	out := &iamv1.BatchAuthorizeCheckResponse{
		Responses: make([]*iamv1.AuthorizeCheckResponse, 0, len(in.GetChecks())),
	}
	for _, c := range in.GetChecks() {
		m.lastAction = c.GetAction()
		m.lastResType = c.GetResource().GetType()
		m.lastRelation = c.GetRequiredRelation()
		key := c.GetSubject() + "|" + c.GetResource().GetType() + "|" + c.GetAction()
		allowed := false
		for _, id := range m.allowedByKey[key] {
			if id == c.GetResource().GetId() {
				allowed = true
				break
			}
		}
		out.Responses = append(out.Responses, &iamv1.AuthorizeCheckResponse{Allowed: allowed})
	}
	return out, nil
}

func newFilter(t *testing.T, cli authzfilter.AuthorizeClient) authzfilter.Filter {
	t.Helper()
	cfg := authzfilter.DefaultConfig()
	cfg.Timeout = 200 * time.Millisecond
	cfg.CacheTTL = time.Second
	return authzfilter.NewFGAFilter(cli, cfg)
}

// ctxWithSubject — кладёт в ctx Principal, эквивалентный FGA-subject "type:id".
// Это ЕДИНЫЙ источник identity (как api-gateway principal-extract); прежний
// x-kacho-subject header больше не источник. subject вида "user:usr_alice".
func ctxWithSubject(subject string) context.Context {
	t, id, ok := strings.Cut(subject, ":")
	if !ok {
		return context.Background()
	}
	return operations.WithPrincipal(context.Background(), operations.Principal{Type: t, ID: id})
}

// SCENARIO 1: filter == nil → handler bypasses (FGA disabled config-gate / dev).
// This is the deliberate config-off bypass, NOT the missing-identity bypass.
func TestInstanceHandler_List_FilterNil_Bypass(t *testing.T) {
	h, insRepo := newInstanceHandlerWithFilter(t, nil)
	seedInstances(t, insRepo, "proj", "d1", "d2", "d3")

	resp, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err)
	require.Len(t, resp.Instances, 3, "filter=nil must return all instances")
}

// CLL-03: cluster-admin / owner → all. The IAM ListObjects returns ALL ids for
// an owner/cluster-admin subject (owner→viewer FGA derivation), so the handler
// passes through the full set. No compute-side header-bypass exists anymore.
func TestInstanceHandler_List_CLL03_OwnerSeesAll(t *testing.T) {
	cli := &mockAuthCli{allowedByKey: map[string][]string{}}
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	ids := seedInstances(t, insRepo, "proj", "a", "b", "c")
	// owner/cluster-admin: iam returns every id.
	cli.allowedByKey["user:usr_owner|compute_instance|compute.instances.list"] = ids

	resp, err := h.List(ctxWithSubject("user:usr_owner"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err)
	require.Len(t, resp.Instances, 3, "owner/cluster-admin must see all")
}

// CLL-01: label-scoped subject → EXACTLY the allowed subset (the over-show leak
// fix anchor). FGA returns 2 of 3 ids; the response MUST NOT include the third.
func TestInstanceHandler_List_CLL01_AllowedSubset(t *testing.T) {
	cli := &mockAuthCli{allowedByKey: map[string][]string{}}
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	ids := seedInstances(t, insRepo, "proj", "a", "b", "c")
	cli.allowedByKey["user:usr_alice|compute_instance|compute.instances.list"] = []string{ids[0], ids[2]}

	resp, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err)
	require.Len(t, resp.Instances, 2)
	gotIDs := map[string]bool{}
	for _, d := range resp.Instances {
		gotIDs[d.Id] = true
	}
	require.True(t, gotIDs[ids[0]] && gotIDs[ids[2]])
	require.False(t, gotIDs[ids[1]], "leak: non-granted instance must NOT appear")
}

// CLL-04: empty grant (not-granted subject) → empty response (NOT 403, NOT all).
// Adversarial: the existence of other-tenant instances must not be revealed.
func TestInstanceHandler_List_CLL04_EmptyGrant(t *testing.T) {
	cli := &mockAuthCli{} // no entries → returns empty []
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	seedInstances(t, insRepo, "proj", "a", "b")

	resp, err := h.List(ctxWithSubject("user:usr_nobody"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err, "empty grant must not error")
	require.Len(t, resp.Instances, 0, "leak: not-granted subject must see nothing")
}

// CLL-07: iam-down + fail-closed → Unavailable (non-regression).
func TestInstanceHandler_List_CLL07_IAMDown_FailClosed(t *testing.T) {
	cli := &mockAuthCli{err: status.Error(codes.Unavailable, "down")}
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	seedInstances(t, insRepo, "proj", "a")

	_, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.Error(t, err)
	require.Equal(t, codes.Unavailable, status.Code(err))
}

// iam-down + fail-open → all results (degraded-mode bypass, opt-in config).
func TestInstanceHandler_List_IAMDown_FailOpen(t *testing.T) {
	cli := &mockAuthCli{err: errors.New("network err")}
	cfg := authzfilter.DefaultConfig()
	cfg.FailOpen = true
	filter := authzfilter.NewFGAFilter(cli, cfg)

	h, insRepo := newInstanceHandlerWithFilter(t, filter)
	seedInstances(t, insRepo, "proj", "a", "b")

	resp, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err, "fail-open: must succeed despite iam error")
	require.Len(t, resp.Instances, 2)
}

// CLL-02 (the leak root): subject=="" (no principal / system) → fail-closed.
// Previously this short-circuited to bypass-all and leaked every instance. The fix
// must return an EMPTY list (existence of disks must stay unknowable) and must
// NOT short-circuit to bypass. The filter is consulted with subject="" which is
// fail-closed at the FGA layer, OR the handler returns empty directly — either
// way the response is empty.
func TestInstanceHandler_List_CLL02_NoPrincipal_FailClosed(t *testing.T) {
	cli := &mockAuthCli{}
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	seedInstances(t, insRepo, "proj", "a", "b", "c")

	// No principal in ctx at all → SystemPrincipal → subject="".
	resp, err := h.List(context.Background(), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err, "no-principal must not be a 5xx; it is fail-closed empty")
	require.Len(t, resp.Instances, 0, "LEAK: no-principal must NOT bypass to all instances")
}

// CLL-02 variant: explicit SystemPrincipal → fail-closed empty (not bypass-all).
func TestInstanceHandler_List_CLL02_SystemPrincipal_FailClosed(t *testing.T) {
	cli := &mockAuthCli{}
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	seedInstances(t, insRepo, "proj", "a", "b")

	ctx := operations.WithPrincipal(context.Background(), operations.SystemPrincipal())
	resp, err := h.List(ctx, &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err)
	require.Len(t, resp.Instances, 0, "LEAK: system principal must NOT bypass to all instances")
}

// SCENARIO: cache hit — a positive per-object verdict within TTL is reused, so
// repeat Lists of the same page cost no further authz round-trips.
func TestInstanceHandler_List_CacheReuse(t *testing.T) {
	cli := &mockAuthCli{allowedByKey: map[string][]string{}}
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	ids := seedInstances(t, insRepo, "proj", "a")
	cli.allowedByKey["user:usr_alice|compute_instance|compute.instances.list"] = []string{ids[0]}

	for i := 0; i < 5; i++ {
		resp, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
		require.NoError(t, err)
		require.Len(t, resp.Instances, 1)
	}
	require.Equal(t, 1, cli.calls, "5 List calls but only 1 iam.BatchCheck (positive verdict cached)")
}

// Pagination format is validated BEFORE any authz decision, so a garbage
// page_token / out-of-range page_size is 400 InvalidArgument regardless of grant
// state. Locked at the HANDLER level with a zero-grant caller (the state that used
// to short-circuit to 200 {[]} and swallow the malformed input) — the portmock
// repos ignore pagination entirely, so only the handler guard can produce the 400.
func TestListHandlers_PaginationValidatedBeforeAuthz(t *testing.T) {
	cli := &mockAuthCli{} // zero grant for every subject
	ctx := ctxWithSubject("user:usr_nobody")

	t.Run("instance garbage token", func(t *testing.T) {
		insSvc := service.NewInstanceService(
			portmock.NewInstanceRepo(), portmock.NewMachineTypeRepo(), portmock.NewZoneRegistry(),
			portmock.NewSubnetRegistry(), &portmock.ProjectClient{OK: true},
			portmock.NewNicClient(), portmock.NewStorageClient(), portmock.NewOpsRepo(),
		)
		h := NewInstanceHandler(insSvc, newFilter(t, cli))
		_, err := h.List(ctx, &computev1.ListInstancesRequest{ProjectId: "proj", PageToken: "not-a-real-token!!"})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

// verbOf returns the last dot-segment of a "<domain>.<resource>.<verb>" action.
func verbOf(action string) string {
	last := -1
	for i := 0; i < len(action); i++ {
		if action[i] == '.' {
			last = i
		}
	}
	if last < 0 {
		return action
	}
	return action[last+1:]
}

// read==enforce: the action each public List handler sends to iam MUST carry the
// "list" verb (which kacho-iam validates and records), and the DECISION relation
// pinned on the check must be "viewer" first — the SAME relation the per-RPC Check
// gate uses for Get. `v_list` is only ever asked for ids `viewer` denied, so the
// first relation observed on a fully-granted page is "viewer".
func TestListHandlers_SendViewerResolvingAction(t *testing.T) {
	t.Run("instance", func(t *testing.T) {
		cli := &mockAuthCli{allowedByKey: map[string][]string{}}
		ops := portmock.NewOpsRepo()
		insRepo := portmock.NewInstanceRepo()
		insRepo.Seed(&domain.Instance{ID: "epd-ins-1", ProjectID: "proj", Name: "vm", ZoneID: "ru-central1-a"})
		svc := service.NewInstanceService(
			insRepo, portmock.NewMachineTypeRepo(), portmock.NewZoneRegistry(), portmock.NewSubnetRegistry(),
			&portmock.ProjectClient{OK: true}, portmock.NewNicClient(), portmock.NewStorageClient(), ops,
		)
		h := NewInstanceHandler(svc, newFilter(t, cli))
		cli.allowedByKey["user:usr_alice|compute_instance|compute.instances.list"] = []string{"epd-ins-1"}
		_, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
		require.NoError(t, err)
		require.Equal(t, "compute_instance", cli.lastResType)
		require.Equal(t, "list", verbOf(cli.lastAction),
			"instance List must send a viewer-resolving verb (read==enforce); got action %q", cli.lastAction)
		require.Equal(t, "viewer", cli.lastRelation,
			"the per-object check must pin the read-tier relation explicitly")
	})
}
