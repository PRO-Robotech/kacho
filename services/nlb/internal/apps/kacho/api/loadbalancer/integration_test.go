// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/loadbalancer"
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	_ "github.com/PRO-Robotech/kacho/services/nlb/internal/dto/type2pb"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

// setupDB выдаёт тесту СОБСТВЕННУЮ базу на одном контейнере пакета — клон
// шаблона, в который миграции накатаны один раз (см. TestMain и
// pkg/pgtest). Зеркало pg/setup_integration_test.go (внутренний helper).
func setupDB(t *testing.T) (*pgxpool.Pool, *kachopg.Repository) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}

	dsn := pgtest.NewDB(t)

	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })

	// Учёт числа ресурсов: вставка строки ресурса СПИСЫВАЕТ место, и списать его
	// не с чего, пока у проекта нет строки учёта. Разбор и перечень идентичностей
	// — `quota_fixture_test.go`.
	seedQuotaFixture(t, pool)
	return pool, kachopg.New(pool, nil)
}

// newOpsRepo создаёт реальную operations-таблицу repo на тестовом пуле.
func newOpsRepo(t *testing.T, pool *pgxpool.Pool) operations.Repo {
	t.Helper()
	return operations.NewRepo(pool, "kacho_nlb")
}

// pollOpDone — детерминированно ждёт op.Done в реальной БД (60s).
func pollOpDone(t *testing.T, opsRepo operations.Repo, opID string) *operations.Operation {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		op, err := opsRepo.Get(context.Background(), opID)
		if err == nil && op.Done {
			return op
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("operation %s did not finish within 60s", opID)
	return nil
}

func makeHandler(t *testing.T, repo *kachopg.Repository, opsRepo operations.Repo) *loadbalancer.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	// vpc/geo недоступны в testcontainers-стенде — заглушаем их ЯВНЫМИ двойниками
	// (VIP-аллокация + subnet-резолв), а не nil-клиентами: nil означает
	// «ребро не сконфигурировано» и теперь fail-close'ит мутацию (несконфигурированный
	// peer — неверная конфигурация, а не режим работы). DB-сторона саги — реальная.
	return loadbalancer.NewHandler(repo, opsRepo, nil, stubCheckClient{}, nil, nil, nil,
		&stubSubnetClient{region: "ru-central1"}, nil, &stubAddressClient{}, nil, logger)
}

// stubCheckClient — явный двойник решателя доступа для integration-стенда: iam
// здесь не поднят, а nil означает «звена решения нет» и с некоторых пор роняет
// вызов отказом (`shared.AuthorizeObject`), а не пропускает его. Двойник отвечает
// «разрешено» — эти сценарии проверяют сторону БД, а не выдачу прав; сама
// fail-closed посадка закреплена отдельно (objectauthz_failclosed_test.go).
type stubCheckClient struct{}

func (stubCheckClient) Check(_ context.Context, _, _, _ string) (bool, error) { return true, nil }

// ctxNamedCaller — контекст с НАЗВАННЫМ вызывающим. Пообъектные решения о доступе
// отвергают вызывающего, которого нельзя назвать субъектом модели прав
// (`shared.AuthorizeObject`), поэтому сценарий, доходящий до такого решения,
// обязан кого-то назвать — иначе он проверяет отказ, а не свой предмет.
func ctxNamedCaller() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr_integration"})
}

// stubSubnetClient — заглушка vpc SubnetClient для integration-стенда: REGIONAL
// подсеть в заданном регионе, одна сеть на все семейства (dualstack same-network
// инвариант выполняется). Заменяет прежний nil-клиент, который молча снимал
// placement/region-precheck.
type stubSubnetClient struct{ region string }

func (s *stubSubnetClient) Get(_ context.Context, subnetID string) (*vpcclient.Subnet, error) {
	return &vpcclient.Subnet{
		ID:            subnetID,
		NetworkID:     "net-integration",
		PlacementType: vpcclient.SubnetPlacementRegional,
		RegionID:      s.region,
	}, nil
}

// stubAddressClient — заглушка vpc InternalAddressClient для integration-стенда
// (без реального vpc). AllocateInternalIP/IPv6 возвращают уникальный адрес/id на
// вызов; AttachExisting эхо-адрес; release — no-op.
type stubAddressClient struct{ seq int64 }

func (s *stubAddressClient) AllocateInternalIP(_ context.Context, _ vpcclient.AllocateInternalIPRequest) (*vpcclient.AllocateResponse, error) {
	n := atomic.AddInt64(&s.seq, 1)
	return &vpcclient.AllocateResponse{
		AddressID: fmt.Sprintf("adr%017d", n),
		Value:     fmt.Sprintf("100.64.0.%d", n),
	}, nil
}

func (s *stubAddressClient) AllocateInternalIPv6(_ context.Context, _ vpcclient.AllocateInternalIPRequest) (*vpcclient.AllocateResponse, error) {
	n := atomic.AddInt64(&s.seq, 1)
	return &vpcclient.AllocateResponse{
		AddressID: fmt.Sprintf("adr%017d", n),
		Value:     fmt.Sprintf("fd00::%d", n),
	}, nil
}

func (s *stubAddressClient) AllocateExternalIP(_ context.Context, _ vpcclient.AllocateExternalIPRequest) (*vpcclient.AllocateResponse, error) {
	n := atomic.AddInt64(&s.seq, 1)
	return &vpcclient.AllocateResponse{AddressID: fmt.Sprintf("adr%017d", n), Value: fmt.Sprintf("203.0.113.%d", n)}, nil
}

func (s *stubAddressClient) AllocateExternalIPv6(_ context.Context, _ vpcclient.AllocateExternalIPRequest) (*vpcclient.AllocateResponse, error) {
	n := atomic.AddInt64(&s.seq, 1)
	return &vpcclient.AllocateResponse{AddressID: fmt.Sprintf("adr%017d", n), Value: fmt.Sprintf("2001:db8::%d", n)}, nil
}

func (s *stubAddressClient) AttachExisting(_ context.Context, req vpcclient.AttachExistingRequest) (*vpcclient.AllocateResponse, error) {
	n := atomic.AddInt64(&s.seq, 1)
	return &vpcclient.AllocateResponse{AddressID: req.AddressID, Value: fmt.Sprintf("100.64.9.%d", n)}, nil
}

func (s *stubAddressClient) ReleaseLease(
	context.Context, vpcclient.ReleaseLeaseRequest,
) (vpcclient.LeaseOutcome, error) {
	return vpcclient.LeaseReleased, nil
}

// internalAutoReq — INTERNAL REGIONAL Create-request (subnet-auto v4) для e2e.
func internalAutoReq(projectID, name string) *lbv1.CreateNetworkLoadBalancerRequest {
	return &lbv1.CreateNetworkLoadBalancerRequest{
		ProjectId: projectID, RegionId: "ru-central1", Name: name,
		Placement: lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL,
		V4Source:  &lbv1.VipSource{Source: &lbv1.VipSource_SubnetId{SubnetId: "sub-1"}},
	}
}

// ---- Tests -----------------------------------------------------------------

func TestIntegration_CreateLoadBalancer_EndToEnd(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := makeHandler(t, repo, opsRepo)

	op, err := h.Create(context.Background(), internalAutoReq("prj-acme-test", "edge-public"))
	require.NoError(t, err)
	require.False(t, op.GetDone())
	require.NotEmpty(t, op.GetId())

	final := pollOpDone(t, opsRepo, op.GetId())
	require.Nilf(t, final.Error, "operation error: %v", final.Error)
	require.NotNil(t, final.Response)

	// Inspect outbox: exactly one CREATED row.
	rows, err := pool.Query(context.Background(),
		`SELECT resource_type, action FROM kacho_nlb.nlb_outbox ORDER BY sequence_no ASC`)
	require.NoError(t, err)
	defer rows.Close()
	var events []string
	for rows.Next() {
		var rt, action string
		require.NoError(t, rows.Scan(&rt, &action))
		events = append(events, rt+":"+action)
	}
	require.Contains(t, events, "nlb_load_balancer:CREATED")
}

func TestIntegration_DeleteLoadBalancer_BlocksOnListener(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)

	// Insert LB via repo directly.
	w, err := repo.Writer(context.Background())
	require.NoError(t, err)
	lb := &domain.LoadBalancer{
		ID:        domain.ResourceID(ids.NewID(ids.PrefixLoadBalancer)),
		ProjectID: "prj-x", RegionID: "ru-central1",
		Name: "edge", Type: domain.LBTypeExternal, Status: domain.LBStatusInactive,
		SessionAffinity: domain.SessionAffinity5Tuple,
	}
	_, err = w.LoadBalancers().Insert(context.Background(), lb)
	require.NoError(t, err)
	require.NoError(t, w.Commit())
	// Insert listener (via raw SQL — no listener handler yet). Must run after LB
	// TX is committed because the pool sees a different snapshot.
	_, err = pool.Exec(context.Background(), `
		INSERT INTO kacho_nlb.listeners (id, project_id, load_balancer_id, region_id, name,
			description, labels, protocol, port,
			default_target_group_id, status)
		VALUES ($1, $2, $3, $4, 'lst-1', '', '{}', 'TCP', 8080, '', 'ACTIVE')`,
		ids.NewID(ids.PrefixListener), "prj-x", string(lb.ID), "ru-central1",
	)
	require.NoError(t, err)

	h := makeHandler(t, repo, opsRepo)
	_, err = h.Delete(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: string(lb.ID),
	})
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "listener")
}

// TestIntegration_Move_Blocked_ListenerWiredToTG — NLB CONTRACT replacement for
// the removed attach-pivot Move guard: a LB with a listener wired to a target
// group (default_target_group_id set, direct FK) cannot be moved cross-project.
func TestIntegration_Move_Blocked_ListenerWiredToTG(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := makeHandler(t, repo, opsRepo)

	w, err := repo.Writer(context.Background())
	require.NoError(t, err)
	lbID := ids.NewID(ids.PrefixLoadBalancer)
	tgID := ids.NewID(ids.PrefixTargetGroup)
	_, err = w.LoadBalancers().Insert(context.Background(), &domain.LoadBalancer{
		ID: domain.ResourceID(lbID), ProjectID: "prj-src", RegionID: "ru-central1",
		Name: "edge", Type: domain.LBTypeExternal, Status: domain.LBStatusInactive,
		SessionAffinity: domain.SessionAffinity5Tuple,
	})
	require.NoError(t, err)
	_, err = w.TargetGroups().Insert(context.Background(), &domain.TargetGroup{
		ID: domain.ResourceID(tgID), ProjectID: "prj-src", RegionID: "ru-central1",
		Name: "tg-1", DeregistrationDelay: domain.LbDuration(300 * time.Second), Status: domain.TargetGroupStatusActive, Port: 8080,
		HealthCheck: domain.HealthCheck{
			Interval: domain.DefaultHealthInterval, Timeout: domain.DefaultHealthTimeout,
			UnhealthyThreshold: domain.DefaultUnhealthyThreshold, HealthyThreshold: domain.DefaultHealthyThreshold,
			TCP: &domain.HealthCheckTCP{Port: 80},
		},
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
	// Wire a listener to the TG (default_target_group_id set) — the NLB CONTRACT
	// replacement for the removed attach pivot. Raw SQL after the LB+TG TX is
	// committed (pool sees a different snapshot); the direct FK RESTRICT to
	// target_groups(id) is satisfied because the TG exists.
	_, err = pool.Exec(context.Background(), `
		INSERT INTO kacho_nlb.listeners (id, project_id, load_balancer_id, region_id, name,
			description, labels, protocol, port,
			default_target_group_id, status)
		VALUES ($1, $2, $3, $4, 'lst-1', '', '{}', 'TCP', 8080, $5, 'ACTIVE')`,
		ids.NewID(ids.PrefixListener), "prj-src", lbID, "ru-central1", tgID,
	)
	require.NoError(t, err)

	_, err = h.Move(ctxNamedCaller(), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-dst",
	})
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "wired to a target group")
}

func TestIntegration_GetTargetStates_HappyPath(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := makeHandler(t, repo, opsRepo)
	_ = pool

	w, err := repo.Writer(context.Background())
	require.NoError(t, err)
	lbID := ids.NewID(ids.PrefixLoadBalancer)
	tgID := ids.NewID(ids.PrefixTargetGroup)
	_, err = w.LoadBalancers().Insert(context.Background(), &domain.LoadBalancer{
		ID: domain.ResourceID(lbID), ProjectID: "prj-q", RegionID: "ru-central1",
		Name: "edge", Type: domain.LBTypeExternal, Status: domain.LBStatusActive,
		SessionAffinity: domain.SessionAffinity5Tuple,
	})
	require.NoError(t, err)
	_, err = w.TargetGroups().Insert(context.Background(), &domain.TargetGroup{
		ID: domain.ResourceID(tgID), ProjectID: "prj-q", RegionID: "ru-central1",
		Name: "tg-1", DeregistrationDelay: domain.LbDuration(300 * time.Second), Status: domain.TargetGroupStatusActive, Port: 8080,
		HealthCheck: domain.HealthCheck{
			Interval: domain.DefaultHealthInterval, Timeout: domain.DefaultHealthTimeout,
			UnhealthyThreshold: 2, HealthyThreshold: 2,
			TCP: &domain.HealthCheckTCP{Port: 80},
		},
		Targets: []domain.Target{
			{ExternalIP: &domain.TargetExternalIP{Address: "1.1.1.1"}, Weight: 100},
		},
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	resp, err := h.GetTargetStates(ctxNamedCaller(), &lbv1.GetTargetStatesRequest{
		NetworkLoadBalancerId: lbID, TargetGroupId: tgID,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetTargetStates(), 1)
}

func TestIntegration_ListOperations_FilterByResourceID(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := makeHandler(t, repo, opsRepo)

	// The operations page is creator-scoped, and an absent principal owns nothing —
	// a bare context made this list legitimately empty, so the case measured an
	// anonymous read rather than its own subject. Both calls now run as one named
	// caller, which is what the case means: the creator lists its own operations.
	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr-ops-filter"})

	op, err := h.Create(ctx, internalAutoReq("prj-ops", "edge"))
	require.NoError(t, err)
	final := pollOpDone(t, opsRepo, op.GetId())
	require.Nilf(t, final.Error, "op err: %v", final.Error)

	// Find LB id from outbox payload.
	var lbID string
	row := pool.QueryRow(context.Background(),
		`SELECT resource_id FROM kacho_nlb.nlb_outbox WHERE action='CREATED' LIMIT 1`)
	require.NoError(t, row.Scan(&lbID))
	require.NotEmpty(t, lbID)

	resp, err := h.ListOperations(ctx, &lbv1.ListNetworkLoadBalancerOperationsRequest{
		NetworkLoadBalancerId: lbID,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetOperations())
}

func TestIntegration_Update_PathUpdatesPersisted(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := makeHandler(t, repo, opsRepo)
	_ = pool

	w, err := repo.Writer(context.Background())
	require.NoError(t, err)
	lbID := ids.NewID(ids.PrefixLoadBalancer)
	_, err = w.LoadBalancers().Insert(context.Background(), &domain.LoadBalancer{
		ID: domain.ResourceID(lbID), ProjectID: "prj-u", RegionID: "ru-central1",
		Name: "edge-old", Description: "old", Type: domain.LBTypeExternal,
		Status: domain.LBStatusInactive, SessionAffinity: domain.SessionAffinity5Tuple,
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	op, err := h.Update(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		Name:                  "edge-new",
		UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"name"}},
	})
	require.NoError(t, err)
	final := pollOpDone(t, opsRepo, op.GetId())
	require.Nil(t, final.Error)

	rd, err := repo.Reader(context.Background())
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.LoadBalancers().Get(context.Background(), lbID)
	require.NoError(t, err)
	require.Equal(t, domain.LbName("edge-new"), got.Name)
}

// TestIntegration_SessionAffinity_RoundTrip — Create persists an explicit
// session_affinity (CLIENT_IP_ONLY, accepted by the DB CHECK); Update flips it
// back via update_mask.
func TestIntegration_SessionAffinity_RoundTrip(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := makeHandler(t, repo, opsRepo)
	ctx := context.Background()

	saReq := internalAutoReq("prj-sa", "edge-sa")
	saReq.SessionAffinity = lbv1.NetworkLoadBalancer_CLIENT_IP_ONLY
	op, err := h.Create(ctx, saReq)
	require.NoError(t, err)
	final := pollOpDone(t, opsRepo, op.GetId())
	require.Nilf(t, final.Error, "create error: %v", final.Error)

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	lbs, _, err := rd.LoadBalancers().List(ctx, kachorepo.LoadBalancerFilter{ProjectID: "prj-sa"}, kachorepo.Pagination{})
	require.NoError(t, err)
	_ = rd.Close()
	require.Len(t, lbs, 1)
	lbID := string(lbs[0].ID)
	require.Equal(t, domain.SessionAffinityClientIPOnly, lbs[0].SessionAffinity)

	// Update flips session_affinity back via mask.
	opU, err := h.Update(ctx, &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		SessionAffinity:       lbv1.NetworkLoadBalancer_FIVE_TUPLE,
		UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"session_affinity"}},
	})
	require.NoError(t, err)
	require.Nil(t, pollOpDone(t, opsRepo, opU.GetId()).Error)
	rd3, err := repo.Reader(ctx)
	require.NoError(t, err)
	got, err := rd3.LoadBalancers().Get(ctx, lbID)
	require.NoError(t, err)
	_ = rd3.Close()
	require.Equal(t, domain.SessionAffinity5Tuple, got.SessionAffinity)
}

// ---- Compile guard ----

var _ kachorepo.Repository = (*kachopg.Repository)(nil)
