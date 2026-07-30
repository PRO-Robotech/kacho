// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer_test

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/loadbalancer"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

// The unit-level order assertion (list_reader_release_test.go) says the reader is
// closed before iam is asked. This one says what that BUYS, against the thing that
// actually runs out: a real pgxpool. The pool is capped at one connection — the
// smallest faithful model of "as many Lists in flight as the pool is wide" — and
// the visibility filter, standing where iam stands, tries to take a second
// connection while List is between its two jobs.
//
// While the read-TX is held that acquire cannot succeed: it waits out its whole
// budget and fails. In production the same wait is paid by every other reader and
// writer of this service (slave == master — one pool), and it lasts as long as iam
// takes to answer, so a healthy database answers DEADLINE_EXCEEDED to Create,
// Update, Delete and Get.

// setupCappedPoolDB brings up a Postgres container, applies the migrations, and
// returns a repository over a pool of EXACTLY maxConns connections.
//
// It does not reuse setupDB from integration_test.go on purpose: that one hands
// back a pool of the default width (max(4, NumCPU)), and the width is the whole
// point here — the property under test is about exhausting it.
func setupCappedPoolDB(t *testing.T, maxConns int) *kachopg.Repository {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pgc, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("kacho_nlb_test"),
		postgres.WithUsername("nlb"),
		postgres.WithPassword("nlb"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgc.Terminate(context.Background()) })

	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	gooseMu.Lock()
	goose.SetBaseFS(migrations.FS)
	err = goose.SetDialect("postgres")
	if err == nil {
		err = goose.Up(db, ".")
	}
	gooseMu.Unlock()
	require.NoError(t, err)

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "options=-c%20search_path%3Dkacho_nlb%2Cpublic"
	dsn += "&pool_max_conns=" + strconv.Itoa(maxConns)

	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	require.Equal(t, int32(maxConns), pool.Config().MaxConns,
		"the cap must actually reach the pool, otherwise this test models nothing")
	t.Cleanup(pool.Close)
	return kachopg.New(pool, nil)
}

// secondReaderProbe stands where iam stands: it is called with the page in hand and
// tries, within a bounded budget, to borrow another connection from the same pool.
type secondReaderProbe struct {
	repo       *kachopg.Repository
	budget     time.Duration
	calls      int
	waited     time.Duration
	acquireErr error
}

var _ authzfilter.Filter = (*secondReaderProbe)(nil)

func (p *secondReaderProbe) FilterVisibleIDs(_ context.Context, _, _, _ string, ids []string) ([]string, error) {
	p.calls++
	ctx, cancel := context.WithTimeout(context.Background(), p.budget)
	defer cancel()
	start := time.Now()
	rd, err := p.repo.Reader(ctx)
	p.waited = time.Since(start)
	if err != nil {
		// Reported through the probe rather than as a filter error, so the failure
		// is attributed to the acquire and not to some mapping of it.
		p.acquireErr = err
		return ids, nil
	}
	_ = rd.Close()
	return ids, nil
}

// TestListLoadBalancers_DoesNotHoldPooledConnectionAcrossAuthz — with a pool of one,
// somebody else must still be able to reach the database while List is asking iam.
func TestListLoadBalancers_DoesNotHoldPooledConnectionAcrossAuthz(t *testing.T) {
	repo := setupCappedPoolDB(t, 1)

	ctx := context.Background()
	lb := &domain.LoadBalancer{
		ID:              domain.ResourceID(ids.NewID(ids.PrefixLoadBalancer)),
		ProjectID:       "prj-pool-release",
		RegionID:        "ru-central1",
		Name:            "lb-pool-release",
		Description:     "pool-release fixture",
		Type:            domain.LBTypeExternal,
		Status:          domain.LBStatusInactive,
		SessionAffinity: domain.SessionAffinity5Tuple,
	}
	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	_, err = w.LoadBalancers().Insert(ctx, lb)
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	probe := &secondReaderProbe{repo: repo, budget: 3 * time.Second}
	uc := loadbalancer.NewListLoadBalancersUseCase(repo, probe)

	callerCtx := operations.WithPrincipal(ctx,
		operations.Principal{Type: "user", ID: "usr_pool_probe"})
	resp, err := uc.Execute(callerCtx, &lbv1.ListNetworkLoadBalancersRequest{
		ProjectId: string(lb.ProjectID),
	})
	require.NoError(t, err)
	require.Len(t, resp.GetNetworkLoadBalancers(), 1)

	require.Equal(t, 1, probe.calls,
		"the page must actually be run through the visibility filter")
	require.NoError(t, probe.acquireErr,
		"List held its pooled connection while the caller's rights were being asked about "+
			"(waited %s of a %s budget): for the whole authz round-trip no other reader or "+
			"writer of this service can reach a healthy database", probe.waited, probe.budget)
}
