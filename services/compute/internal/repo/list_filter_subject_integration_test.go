// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_filter_subject_integration_test.go — end-to-end (testcontainers Postgres)
// guard for the public List label-scope over-show leak fix.
//
// Wires a REAL InstanceRepo (real SQL) into InstanceService → InstanceHandler →
// real FGAFilter (mock AuthorizeClient) and drives handler.List through the request
// Principal path — the SAME identity source per-RPC Check uses. Proves the fix
// against real rows.
//
// The guard was originally written over compute's own Disk resource. Disk is
// retired (kacho-storage owns block storage), but the property under test belongs
// to the shared list-filter, not to any one resource, so it moved to Instance
// rather than leaving with the resource it happened to be written against:
//   - CLL-02: no-principal / system principal → fail-closed (0 rows) DESPITE
//     seeded rows. Before the fix the handler short-circuited to bypass-all and
//     leaked every row.
//   - CLL-01: label-scoped principal → exactly the per-object-allowed subset of
//     the page reaches the response, not the whole project.
package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/instance"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/handler"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// batchCheckStub — minimal listnarrow.AuthorizeClient answering each per-object
// check from a fixed grant set keyed by subject (so subject-source mismatches stay
// observable: a "" subject never reaches here, and an unexpected subject is denied
// everything). Order-preserving, as the BatchCheck contract requires.
type batchCheckStub struct {
	allowBySubject map[string][]string
	calls          int
}

func (s *batchCheckStub) BatchCheck(_ context.Context, in *iamv1.BatchAuthorizeCheckRequest, _ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	s.calls++
	out := &iamv1.BatchAuthorizeCheckResponse{
		Responses: make([]*iamv1.AuthorizeCheckResponse, 0, len(in.GetChecks())),
	}
	for _, c := range in.GetChecks() {
		allowed := false
		for _, id := range s.allowBySubject[c.GetSubject()] {
			if id == c.GetResource().GetId() {
				allowed = true
				break
			}
		}
		out.Responses = append(out.Responses, &iamv1.AuthorizeCheckResponse{Allowed: allowed})
	}
	return out, nil
}

// newInstanceHandlerOnRealRepo — InstanceService over a real (testcontainers) repo
// + real FGAFilter (mock AuthorizeClient). Returns the handler, the repo (for
// deterministic seeding) and the pool (for cleanup).
func newInstanceHandlerOnRealRepo(t *testing.T, cli listnarrow.AuthorizeClient) (*handler.InstanceHandler, *repo.InstanceRepo, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)

	instanceRepo := repo.NewInstanceRepo(pool)
	svc := instance.NewInstanceService(
		instanceRepo,
		portmock.NewMachineTypeRepo(),
		portmock.NewZoneRegistry("ru-central1-a"),
		portmock.NewSubnetRegistry(),
		&portmock.ProjectClient{OK: true},
		portmock.NewNicClient(),
		portmock.NewStorageClient(),
		portmock.NewOpsRepo(),
	)
	cfg := listnarrow.Config{Relations: authzfilter.PageRelations}
	cfg.Timeout = 500 * time.Millisecond
	filter := listnarrow.New(cli, cfg)
	return handler.NewInstanceHandler(svc, filter), instanceRepo, pool
}

// seedInstances — insert N instances directly via the real repo for deterministic ids.
func seedInstances(t *testing.T, r *repo.InstanceRepo, projectID string, names ...string) []string {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	var out []string
	for i, n := range names {
		id := ids.NewID(ids.PrefixInstance)
		in := &domain.Instance{
			ID:        id,
			ProjectID: projectID,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
			Name:      n, ZoneID: "ru-central1-a",
			Status:             domain.InstanceStatusProvisioning,
			InstanceKind:       domain.InstanceKindVM,
			MachineTypeID:      "mt-std2",
			EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
			BootSource:         domain.BootSource{Type: "storage.image", ID: "img-9k2m4x7q1n8p:22.04-lts", ImageKind: domain.ImageKindStorageImage},
			FQDN:               id + ".auto.internal",
		}
		created, _, err := r.Insert(ctx, in)
		require.NoError(t, err)
		out = append(out, created.ID)
	}
	return out
}

// CLL-02 (integration): no-principal / system principal → fail-closed 0 rows
// despite real seeded rows. This is the production leak reproduced end-to-end:
// a List with no caller-identity must NOT return the project's instances.
func TestIntegration_InstanceHandler_NoPrincipal_FailClosed_NoLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	stub := &batchCheckStub{allowBySubject: map[string][]string{}}
	h, r, pool := newInstanceHandlerOnRealRepo(t, stub)
	defer pool.Close()

	ids := seedInstances(t, r, "proj-a", "a", "b", "c")
	require.Len(t, ids, 3)

	// Принципала в ctx нет → вызывающий не назван → ОТКАЗ.
	//
	// Полярность сменилась: прежде здесь была пустая страница. «Пусто» неотличимо от
	// «личность потеряна по дороге», и именно этим неразличением класс живёт годами.
	resp, err := h.List(context.Background(), &computev1.ListInstancesRequest{ProjectId: "proj-a"})
	require.Error(t, err, "no-principal List must be refused")
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Len(t, resp.GetInstances(), 0, "LEAK: no-principal List must NOT return any instance")
	require.Equal(t, 0, stub.calls, "fail-closed: filter must not be consulted for empty subject")

	// Явный служебный принципал → тот же отказ: служебный тип объявляет отправитель
	// заголовков, поэтому личностью он не является.
	sysCtx := operations.WithPrincipal(context.Background(), operations.SystemPrincipal())
	resp, err = h.List(sysCtx, &computev1.ListInstancesRequest{ProjectId: "proj-a"})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Len(t, resp.GetInstances(), 0, "LEAK: system principal List must NOT return any instance")
}

// CLL-01 (integration): the real (unfiltered, project-scoped) SQL page is narrowed
// per-object to exactly the FGA-allowed subset; not-granted → empty.
func TestIntegration_InstanceHandler_LabelScoped_SubsetOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	stub := &batchCheckStub{allowBySubject: map[string][]string{}}
	h, r, pool := newInstanceHandlerOnRealRepo(t, stub)
	defer pool.Close()

	ids := seedInstances(t, r, "proj-a", "a", "b", "c")
	require.Len(t, ids, 3)

	// alice's label-grant covers only 2 of 3 instances.
	stub.allowBySubject["user:usr_alice"] = []string{ids[0], ids[2]}
	ctx := operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: "usr_alice"})

	resp, err := h.List(ctx, &computev1.ListInstancesRequest{ProjectId: "proj-a"})
	require.NoError(t, err)
	require.Len(t, resp.Instances, 2, "label-scoped subject must see only the granted subset of the page")
	got := map[string]bool{}
	for _, d := range resp.Instances {
		got[d.Id] = true
	}
	require.True(t, got[ids[0]] && got[ids[2]])
	require.False(t, got[ids[1]], "LEAK: non-granted instance must not appear")

	// not-granted subject → empty (no existence leak).
	other := operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: "usr_nobody"})
	resp, err = h.List(other, &computev1.ListInstancesRequest{ProjectId: "proj-a"})
	require.NoError(t, err)
	require.Len(t, resp.Instances, 0, "LEAK: not-granted subject must see nothing")
}
