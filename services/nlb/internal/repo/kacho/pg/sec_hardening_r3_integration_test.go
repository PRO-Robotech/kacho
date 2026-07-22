// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// waitForLockWaiter deterministically blocks until at least one backend is
// waiting on a lock (pg_stat_activity.wait_event_type='Lock'), proving the
// concurrent goroutine has actually reached and is blocked on the contended row
// lock. Replaces a fixed `time.Sleep(600ms)` barrier that could pass vacuously
// under CI/host load if the goroutine had not yet issued its locking query
// before the main TX committed and released the lock — the intended race window
// then never opened and a genuine lost-update/cross-project-attach regression
// slipped through green (CWE-367). `observer` must be a pool
// distinct from the transactions under test. Fails the test if no waiter appears
// within deadline.
func waitForLockWaiter(t *testing.T, ctx context.Context, observer *pgxpool.Pool, deadline time.Duration) {
	t.Helper()
	stop := time.Now().Add(deadline)
	for time.Now().Before(stop) {
		var n int
		err := observer.QueryRow(ctx,
			`SELECT count(*) FROM pg_stat_activity
			  WHERE wait_event_type = 'Lock' AND state = 'active'`).Scan(&n)
		require.NoError(t, err)
		if n >= 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout: no backend became blocked on a lock within %v — the race window never opened", deadline)
}

// newObserverPool opens a standalone pool (separate from the tx-under-test pool)
// for pg_stat_activity introspection in the deterministic lock-wait helper.
func newObserverPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	p, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	return p
}

// =============================================================================
// TargetGroup.MoveProject — basic guard behavior. NLB CONTRACT removed the M:N
// attach pivot; the cross-project attach TOCTOU it created (Move ↔ Attach) no
// longer exists, so the pivot-race sub-tests were removed. The referenced-by-
// listener move guard is covered at the use-case level
// (targetgroup/move_test.go TestMove_ReferencedByListener).
// =============================================================================

// TestTGMoveProject_Allowed_NoAttach — without any blocker, move proceeds.
func TestTGMoveProject_Allowed_NoAttach(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	tg := newTG("prj0TGMVOK234567890ll", "tgmvok-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
	})
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		moved, err := w.TargetGroups().MoveProject(ctx, string(tg.ID), "prj0TGMVOK2234567890l")
		require.NoError(t, err)
		assert.Equal(t, domain.ProjectID("prj0TGMVOK2234567890l"), moved.ProjectID)
	})
}

// TestTGMoveProject_NotFound — missing TG → NotFound (not FailedPrecondition).
func TestTGMoveProject_NotFound(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.TargetGroups().MoveProject(ctx, "tgrMISSING1234567890", "prj0TGMVX2234567890ll")
	require.Error(t, err)
	assert.True(t, errors.Is(err, kacho.ErrNotFound), "missing TG → NotFound, got %v", err)
}

// NOTE on listener region-VIP uniqueness: the partial UNIQUE
// listeners_region_vip_uniq that existed in migration 0001 was DELIBERATELY
// dropped in migration 0009 ("VIP-уникальность переехала на LoadBalancer"). VIP
// uniqueness is now a LoadBalancer-level invariant enforced by
// load_balancers_region_v4_uniq / _v6_uniq, and is race-tested in
// load_balancer_vip_concurrent_integration_test.go. There is therefore no
// listener-level region-VIP invariant left to test; the corrected/dead comment
// in listener_integration_test.go and docs/architecture/known-divergences.md
// record this by-design move.
