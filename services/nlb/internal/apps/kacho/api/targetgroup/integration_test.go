// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/targetgroup"
	// dto/type2pb init регистрирует TargetGroup transfer.
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	_ "github.com/PRO-Robotech/kacho/services/nlb/internal/dto/type2pb"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

// setupDB выдаёт тесту СОБСТВЕННУЮ базу на одном контейнере пакета — клон
// шаблона с уже накатанными migrations (см. TestMain и internal/pgtest).
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

func newOpsRepo(t *testing.T, pool *pgxpool.Pool) operations.Repo {
	t.Helper()
	return operations.NewRepo(pool, "kacho_nlb")
}

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

func mkHandler(t *testing.T, repo *kachopg.Repository, opsRepo operations.Repo) *targetgroup.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	// nil peer-clients — Create/AddTargets/Move skip peer-validate (acceptable
	// для integration сценариев DB happy-paths). Решатель доступа — ЯВНЫЙ двойник,
	// не nil: nil означает «звена решения нет» и роняет мутацию отказом.
	return targetgroup.NewHandler(repo, opsRepo, nil, stubCheckClient{}, nil, nil, nil, nil, nil, narrowtest.AllowingAll(), logger)
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

// ---- Integration tests -----------------------------------------------------

func TestIntegration_CreateTargetGroup_EndToEnd(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := mkHandler(t, repo, opsRepo)

	op, err := h.Create(context.Background(), &lbv1.CreateTargetGroupRequest{
		ProjectId: "prj-integ-create",
		RegionId:  "ru-central1",
		Name:      "tg-int-1",
		Labels:    map[string]string{"env": "prod"},
		Port:      8080,
		HealthCheck: &lbv1.HealthCheck{
			Interval:           durationpb.New(2 * time.Second),
			Timeout:            durationpb.New(1 * time.Second),
			UnhealthyThreshold: 2,
			HealthyThreshold:   2,
			Options: &lbv1.HealthCheck_Http{
				Http: &lbv1.HealthCheck_HttpOptions{Port: 8080, Path: "/healthz"},
			},
		},
		DeregistrationDelay: durationpb.New(300 * time.Second),
	})
	require.NoError(t, err)
	require.False(t, op.GetDone())

	final := pollOpDone(t, opsRepo, op.GetId())
	require.Nilf(t, final.Error, "operation error: %v", final.Error)
	require.NotNil(t, final.Response)

	// Outbox row present.
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
	require.Contains(t, events, "nlb_target_group:CREATED")
}

// integration: Delete TG blocked when a listener references it
// (NLB CONTRACT: real precheck via ReferencingListenerIDs, backed by the direct
// FK RESTRICT listeners.default_target_group_id → target_groups(id); the M:N
// pivot was dropped in migration 0022).
func TestIntegration_DeleteTG_BlocksOnReferencingListener(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)

	// Insert LB + TG + a listener wired to the TG via raw SQL (no handlers).
	lbID := ids.NewID(ids.PrefixLoadBalancer)
	tgID := ids.NewID(ids.PrefixTargetGroup)
	lstID := ids.NewID(ids.PrefixListener)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO kacho_nlb.load_balancers (id, project_id, region_id, name, description, labels,
			type, status, session_affinity, deletion_protection)
		VALUES ($1, 'prj-x', 'ru-central1', 'lb-int', '', '{}', 'EXTERNAL', 'ACTIVE',
		        'FIVE_TUPLE', false)`, lbID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO kacho_nlb.target_groups (id, project_id, region_id, name, description, labels,
			health_check, deregistration_delay_seconds, slow_start_seconds, status, port)
		VALUES ($1, 'prj-x', 'ru-central1', 'tg-int', '', '{}',
		        '{"name":"hc","interval":"2s","timeout":"1s","unhealthy_threshold":2,"healthy_threshold":2,"tcp":{"port":80}}'::jsonb,
		        300, 0, 'ACTIVE', 8080)`, tgID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO kacho_nlb.listeners (id, project_id, load_balancer_id, region_id, name,
			description, labels, protocol, port,
			default_target_group_id, status)
		VALUES ($1, 'prj-x', $2, 'ru-central1', 'lst-int', '', '{}', 'TCP', 8080,
		        $3, 'ACTIVE')`, lstID, lbID, tgID,
	)
	require.NoError(t, err)

	h := mkHandler(t, repo, opsRepo)
	_, err = h.Delete(ctx, &lbv1.DeleteTargetGroupRequest{TargetGroupId: tgID})
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "referenced by listeners")
}

// TestIntegration_AddRemoveTargets_Lifecycle — фаза B parity integration: full
// Add/Remove/Drain lifecycle через real Postgres.
func TestIntegration_AddRemoveTargets_Lifecycle(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := mkHandler(t, repo, opsRepo)
	// Список за ScopeFiltered-RPC отказывает вызывающему, которого никто не
	// назвал; реальный вызов всегда несёт личность. Этот файл — внешний
	// тест-пакет (targetgroup_test), поэтому принципал ставится напрямую.
	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr_lister"})

	// 1. Create TG.
	createOp, err := h.Create(ctx, &lbv1.CreateTargetGroupRequest{
		ProjectId: "prj-life", RegionId: "ru-central1", Name: "life-tg", Port: 8080,
		HealthCheck: &lbv1.HealthCheck{
			Interval: durationpb.New(2 * time.Second),
			Timeout:  durationpb.New(1 * time.Second), UnhealthyThreshold: 2, HealthyThreshold: 2,
			Options: &lbv1.HealthCheck_Tcp{Tcp: &lbv1.HealthCheck_TcpOptions{Port: 80}},
		},
		DeregistrationDelay: durationpb.New(300 * time.Second),
	})
	require.NoError(t, err)
	createFinal := pollOpDone(t, opsRepo, createOp.GetId())
	require.Nil(t, createFinal.Error)

	// Resolve TG.ID via List.
	listResp, err := h.List(ctx, &lbv1.ListTargetGroupsRequest{ProjectId: "prj-life"})
	require.NoError(t, err)
	require.Len(t, listResp.GetTargetGroups(), 1)
	tgID := listResp.GetTargetGroups()[0].GetId()

	// 2. AddTargets — 2 unique external_ip identities (peer-validate not needed:
	// external_ip path skips compute/vpc peer lookups; bogon-check done in
	// domain Validate).
	addOp, err := h.AddTargets(ctx, &lbv1.AddTargetsRequest{
		TargetGroupId: tgID,
		Targets: []*lbv1.Target{
			{Identity: &lbv1.Target_ExternalIp{ExternalIp: &lbv1.Target_ExternalIP{
				Address: "203.0.113.10",
			}}, Weight: 100},
			{Identity: &lbv1.Target_ExternalIp{ExternalIp: &lbv1.Target_ExternalIP{
				Address: "203.0.113.20",
			}}, Weight: 50},
		},
	})
	require.NoError(t, err)
	addFinal := pollOpDone(t, opsRepo, addOp.GetId())
	require.Nilf(t, addFinal.Error, "add op error: %v", addFinal.Error)

	// 3. Re-add same → idempotent (ON CONFLICT DO NOTHING on partial UNIQUE
	// per external_ip_address).
	reAddOp, err := h.AddTargets(ctx, &lbv1.AddTargetsRequest{
		TargetGroupId: tgID,
		Targets: []*lbv1.Target{
			{Identity: &lbv1.Target_ExternalIp{ExternalIp: &lbv1.Target_ExternalIP{
				Address: "203.0.113.10",
			}}, Weight: 100},
		},
	})
	require.NoError(t, err)
	reAddFinal := pollOpDone(t, opsRepo, reAddOp.GetId())
	require.Nil(t, reAddFinal.Error)

	// 4. Get TG inspects 2 targets (idempotent re-add не добавил третий).
	getResp, err := h.Get(ctx, &lbv1.GetTargetGroupRequest{TargetGroupId: tgID})
	require.NoError(t, err)
	require.Len(t, getResp.GetTargets(), 2, "duplicate identity should be no-op")

	// 5. RemoveTargets фаза A: mark one external_ip target as DRAINING.
	rmOp, err := h.RemoveTargets(ctx, &lbv1.RemoveTargetsRequest{
		TargetGroupId: tgID,
		Targets: []*lbv1.Target{
			{Identity: &lbv1.Target_ExternalIp{ExternalIp: &lbv1.Target_ExternalIP{
				Address: "203.0.113.10",
			}}, Weight: 100},
		},
	})
	require.NoError(t, err)
	rmFinal := pollOpDone(t, opsRepo, rmOp.GetId())
	require.Nil(t, rmFinal.Error)

	// 6. Verify SQL state: 1 row DRAINING + drain_started_at != NULL; 1 row ACTIVE.
	var drainingCount int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM kacho_nlb.targets
		  WHERE target_group_id = $1 AND status='DRAINING' AND drain_started_at IS NOT NULL`,
		tgID).Scan(&drainingCount)
	require.NoError(t, err)
	assert.Equal(t, 1, drainingCount)

	var activeCount int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM kacho_nlb.targets WHERE target_group_id = $1 AND status='ACTIVE'`,
		tgID).Scan(&activeCount)
	require.NoError(t, err)
	assert.Equal(t, 1, activeCount)
}
