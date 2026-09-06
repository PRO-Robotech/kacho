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
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/instance"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

// newInstanceHandlerWithFilter — InstanceHandler over portmock repos + the real
// list-filter. Returns the handler and the repo, for deterministic seeding.
func newInstanceHandlerWithFilter(t *testing.T, filter *listnarrow.Narrower) (*InstanceHandler, *portmock.InstanceRepo) {
	t.Helper()
	insRepo := portmock.NewInstanceRepo()
	svc := instance.NewInstanceService(
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

// mockAuthCli — handler-test stub of kaname AuthorizeService.BatchCheck.
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

// newFilter — НАСТОЯЩИЙ сужатель над подставным соседом: подменяется только тот, кто
// отвечает, а все ветки — личность, посадка, партии, окно вердиктов — исполняются те
// же, что в бою.
func newFilter(t *testing.T, cli listnarrow.AuthorizeClient) *listnarrow.Narrower {
	t.Helper()
	return listnarrow.New(cli, listnarrow.Config{
		Relations: authzfilter.PageRelations,
		Timeout:   200 * time.Millisecond,
		CacheTTL:  time.Second,
	})
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

// SCENARIO 1: сужателя нет → ОТКАЗ, а не сквозной проход.
//
// Полярность сменилась: прежде nil означал «сужение выключено, страницу отдать», и
// посадка без модели показывала каждому участнику проекта каждую его строку. Пропуск
// теперь возможен только объявленным аварийным режимом — и он считается.
func TestInstanceHandler_List_AbsentModelIsRefused(t *testing.T) {
	h, insRepo := newInstanceHandlerWithFilter(t, nil)
	seedInstances(t, insRepo, "proj", "d1", "d2", "d3")

	resp, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.Error(t, err, "спросить негде — значит отказ, а не «да»")
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Empty(t, resp.GetInstances())
}

// ПАРНЫЙ к предыдущему: аварийный режим остаётся, но он ОБЪЯВЛЕН и посчитан.
func TestInstanceHandler_List_BreakglassPassesAndIsCounted(t *testing.T) {
	n := narrowtest.Breakglass()
	h, insRepo := newInstanceHandlerWithFilter(t, n)
	seedInstances(t, insRepo, "proj", "d1", "d2", "d3")

	resp, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err)
	require.Len(t, resp.Instances, 3)
	require.Equal(t, uint64(1), n.Counts().Breakglass,
		"аварийный режим без счётчика становится тихим штатным")
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
	filter := listnarrow.New(cli, listnarrow.Config{
		Relations:             authzfilter.PageRelations,
		SoftPassOnPeerFailure: true,
	})

	h, insRepo := newInstanceHandlerWithFilter(t, filter)
	seedInstances(t, insRepo, "proj", "a", "b")

	resp, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err, "fail-open: must succeed despite iam error")
	require.Len(t, resp.Instances, 2)
}

// CLL-02 (the leak root): subject=="" (no principal / system) → fail-closed.
// Прежде это замыкалось в сквозной проход и отдавало каждую машину. Теперь ответ —
// ОТКАЗ, а не пустая страница: «пусто» неотличимо от «личность потеряна по дороге»,
// и именно этим неразличением класс живёт годами. Модель при этом не тревожится
// вовсе — спрашивать не о ком.
func TestInstanceHandler_List_CLL02_NoPrincipalIsRefused(t *testing.T) {
	cli := &mockAuthCli{}
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	seedInstances(t, insRepo, "proj", "a", "b", "c")

	resp, err := h.List(context.Background(), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.Error(t, err, "запрос никого не назвал — страница не отдаётся")
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Empty(t, resp.GetInstances(), "LEAK: no-principal must NOT bypass to all instances")
	require.Zero(t, cli.calls, "спрашивать не о ком — модель не тревожат")
}

// CLL-02 вариант: явный служебный принципал — тот же отказ. Служебный ТИП объявляет
// отправитель заголовков, поэтому личностью он не является.
func TestInstanceHandler_List_CLL02_SystemPrincipalIsRefused(t *testing.T) {
	cli := &mockAuthCli{}
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	seedInstances(t, insRepo, "proj", "a", "b")

	ctx := operations.WithPrincipal(context.Background(), operations.SystemPrincipal())
	resp, err := h.List(ctx, &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Empty(t, resp.GetInstances(), "LEAK: system principal must NOT bypass to all instances")
	require.Zero(t, cli.calls)
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
		insSvc := instance.NewInstanceService(
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

// read==enforce: действие, которое публичный List шлёт в iam, обязано нести verb
// "list" (его kaname валидирует и записывает в аудит), а РЕШАЮЩЕЕ отношение,
// пинуемое на проверке, — быть тем же, которым per-RPC Check гейтит Get
// (`InstanceService/Get` → `v_get`, permission_map.go).
//
// Прежняя редакция ждала здесь `viewer` и объясняла это словами «та же relation,
// что per-RPC Check для Get». Утверждение было неверным: Get гейтится `v_get`, а
// ярусные и глагольные отношения в модели развязаны. Тест закреплял расхождение,
// из-за которого страница List оказывалась шире читаемого. Пиним значение, а не
// пересказ.
func TestListHandlers_PinTheReadRelationOnPerObjectCheck(t *testing.T) {
	t.Run("instance", func(t *testing.T) {
		cli := &mockAuthCli{allowedByKey: map[string][]string{}}
		ops := portmock.NewOpsRepo()
		insRepo := portmock.NewInstanceRepo()
		insRepo.Seed(&domain.Instance{ID: "epd-ins-1", ProjectID: "proj", Name: "vm", ZoneID: "ru-central1-a"})
		svc := instance.NewInstanceService(
			insRepo, portmock.NewMachineTypeRepo(), portmock.NewZoneRegistry(), portmock.NewSubnetRegistry(),
			&portmock.ProjectClient{OK: true}, portmock.NewNicClient(), portmock.NewStorageClient(), ops,
		)
		h := NewInstanceHandler(svc, newFilter(t, cli))
		cli.allowedByKey["user:usr_alice|compute_instance|compute.instances.list"] = []string{"epd-ins-1"}
		_, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
		require.NoError(t, err)
		require.Equal(t, "compute_instance", cli.lastResType)
		require.Equal(t, "list", verbOf(cli.lastAction),
			"instance List must send a list verb kaname resolves (read==enforce); got action %q", cli.lastAction)
		require.Equal(t, "v_get", cli.lastRelation,
			"пообъектная проверка страницы обязана пинить ТО ЖЕ отношение, которым гейтится Get")
	})
}
