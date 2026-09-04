// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// =============================================================================
// MoveProject ↔ listener-wire race — DB-level, both orders.
// =============================================================================
//
// The NLB CONTRACT dropped the `attached_target_groups` pivot; wiring now happens
// through a plain `UPDATE listeners SET default_target_group_id = …` (OCC on
// xmin), which takes NO lock on `target_groups` / `load_balancers` of its own.
// The `NOT EXISTS (referencing listener)` guard inside MoveProject is therefore a
// SNAPSHOT check — on its own it cannot see a wire that commits in the window
// between the guard and the commit (data-integrity.md ban #10: a within-service
// referential invariant must be a DB construction, not a check-then-act).
//
// What actually closes the race is the composite FK of migration 0023:
//
//	listeners (default_tg_fk, project_id) → target_groups (id, project_id)
//
// It makes `project_id` a KEY column of the referenced side
// (target_groups_id_project_uniq), so:
//   - move-first — the key-update takes an exclusive tuple lock; the concurrent
//     wire's RI probe (`… FOR KEY SHARE`) conflicts, blocks, and after the commit
//     re-reads through EvalPlanQual, no longer matching the old project → 23503;
//   - wire-first — the wire's KEY SHARE lock makes the key-update wait, and the
//     referenced-side ON UPDATE NO ACTION trigger (which probes with a fresh
//     snapshot) then sees the committed reference → 23503.
//
// These tests lock the OBSERVABLE both ways: the loser gets FailedPrecondition
// with the contract tone (no pgx leak), and — the real invariant — the database
// NEVER ends up with a listener referencing a TargetGroup of another project.
// They are the regression backstop for the whole construction: dropping the
// composite FK (or the UNIQUE (id, project_id) that makes project_id a key
// column, which silently downgrades the tuple lock to "no key update") turns
// both tests red.

// assertNoCrossProjectWiring — the durable invariant. Fails if ANY listener row
// references a TargetGroup owned by a different project.
func assertNoCrossProjectWiring(t *testing.T, ctx context.Context, repo kacho.Repository, lstID, tgID string) {
	t.Helper()
	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	lst, err := rd.Listeners().Get(ctx, lstID)
	require.NoError(t, err)
	wired, ok := lst.DefaultTargetGroupID.Maybe()
	if !ok || string(wired) == "" {
		return // unwired listener cannot be a cross-project reference
	}
	tg, err := rd.TargetGroups().Get(ctx, tgID)
	require.NoError(t, err)
	assert.Equal(t, string(tg.ProjectID), string(lst.ProjectID),
		"a listener must never end up referencing a TargetGroup of another project "+
			"(listener %s in project %s → TargetGroup %s in project %s)",
		lstID, lst.ProjectID, tgID, tg.ProjectID)
}

// TestTGMove_vs_ListenerRepoint_MoveFirst_NoCrossProject — TargetGroup.MoveProject
// wins the race to the row lock; the concurrent listener repoint must NOT be able
// to durably wire the TG that has just left the project.
func TestTGMove_vs_ListenerRepoint_MoveFirst_NoCrossProject(t *testing.T) {
	dsn := setupTestDB(t)
	repo, cleanup := newRepo(t, dsn)
	defer cleanup()
	observer := newObserverPool(t, dsn)
	ctx := context.Background()

	const (
		projA = "prj0TGMVRACE1A000001"
		projB = "prj0TGMVRACE1B000001"
	)
	lb := newLB(projA, "")
	tg := newTG(projA, "")
	lst := newListener(lb.ID, projA, "", 8443)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
		_, err = w.Listeners().Insert(ctx, lst) // unwired — nothing blocks the move yet
		require.NoError(t, err)
	})

	// TX1 — move the TG out of project A. The guard sees no referencing listener
	// on its snapshot, so the UPDATE succeeds; the TX stays open.
	mover, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer mover.Abort()
	moved, err := mover.TargetGroups().MoveProject(ctx, string(tg.ID), projB)
	require.NoError(t, err, "no listener references the TG yet — the move must pass the guard")
	require.Equal(t, domain.ProjectID(projB), moved.ProjectID)

	// TX2 (concurrent) — wire the very TG that is being moved away.
	wireErrCh := make(chan error, 1)
	go func() {
		w, werr := repo.Writer(ctx)
		if werr != nil {
			wireErrCh <- werr
			return
		}
		defer w.Abort()
		wire := *lst
		wire.DefaultTargetGroupID = option.MustNewOption(tg.ID)
		if _, uerr := updateListenerOCC(ctx, w, &wire); uerr != nil {
			wireErrCh <- uerr
			return
		}
		wireErrCh <- w.Commit()
	}()

	// Deterministic proof that the two transactions really overlap: TX2 must be
	// parked on a lock before TX1 commits (otherwise the window never opened and
	// a green result would be vacuous — CWE-367).
	waitForLockWaiter(t, ctx, observer, 15*time.Second)
	require.NoError(t, mover.Commit())

	wireErr := <-wireErrCh
	require.Error(t, wireErr,
		"wiring a TargetGroup that concurrently left the project must be rejected")
	assert.True(t, errors.Is(wireErr, kacho.ErrFailedPrecondition),
		"cross-project wire → FailedPrecondition, got %v", wireErr)
	assert.NotContains(t, wireErr.Error(), "SQLSTATE", "must not leak raw pgx text")

	assertNoCrossProjectWiring(t, ctx, repo, string(lst.ID), string(tg.ID))
}

// TestTGMove_vs_ListenerRepoint_WireFirst_MoveRejected — the mirror order: the
// listener wires the TG first, so the move must lose (FailedPrecondition) and the
// TargetGroup must stay in its original project.
func TestTGMove_vs_ListenerRepoint_WireFirst_MoveRejected(t *testing.T) {
	dsn := setupTestDB(t)
	repo, cleanup := newRepo(t, dsn)
	defer cleanup()
	observer := newObserverPool(t, dsn)
	ctx := context.Background()

	const (
		projA = "prj0TGMVRACE2A000001"
		projB = "prj0TGMVRACE2B000001"
	)
	lb := newLB(projA, "")
	tg := newTG(projA, "")
	lst := newListener(lb.ID, projA, "", 8444)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
		_, err = w.Listeners().Insert(ctx, lst)
		require.NoError(t, err)
	})

	// TX1 — wire the listener to the TG; keep the TX open (holds the referenced
	// row under KEY SHARE via the RI probe).
	wirer, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer wirer.Abort()
	wire := *lst
	wire.DefaultTargetGroupID = option.MustNewOption(tg.ID)
	_, err = updateListenerOCC(ctx, wirer, &wire)
	require.NoError(t, err, "same-project wire must be accepted")

	// TX2 (concurrent) — move the freshly referenced TG to another project.
	moveErrCh := make(chan error, 1)
	go func() {
		w, werr := repo.Writer(ctx)
		if werr != nil {
			moveErrCh <- werr
			return
		}
		defer w.Abort()
		if _, merr := w.TargetGroups().MoveProject(ctx, string(tg.ID), projB); merr != nil {
			moveErrCh <- merr
			return
		}
		moveErrCh <- w.Commit()
	}()

	waitForLockWaiter(t, ctx, observer, 15*time.Second)
	require.NoError(t, wirer.Commit())

	moveErr := <-moveErrCh
	require.Error(t, moveErr,
		"moving a TargetGroup that a listener concurrently wired must be rejected")
	assert.True(t, errors.Is(moveErr, kacho.ErrFailedPrecondition),
		"referenced TG move → FailedPrecondition, got %v", moveErr)
	assert.NotContains(t, moveErr.Error(), "SQLSTATE", "must not leak raw pgx text")

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	after, err := rd.TargetGroups().Get(ctx, string(tg.ID))
	require.NoError(t, err)
	assert.Equal(t, domain.ProjectID(projA), after.ProjectID,
		"the rejected move must not have shifted the TargetGroup")
	assertNoCrossProjectWiring(t, ctx, repo, string(lst.ID), string(tg.ID))
}

// TestLBMove_vs_ListenerWire_CascadeBlocked — the symmetric hole on the
// LoadBalancer side. `loadBalancerWriter.MoveProject` guards on
// `NOT EXISTS (listener wired to a TG)` and then CASCADES `listeners.project_id`
// to the new project. That cascade is what would create the cross-project
// reference: the listener rows move, the TargetGroup they point at does not.
// The composite FK re-checks every touched listener row, so the cascade is the
// statement that fails — the guard alone (a snapshot read) cannot see a wire that
// commits after it.
func TestLBMove_vs_ListenerWire_CascadeBlocked(t *testing.T) {
	dsn := setupTestDB(t)
	repo, cleanup := newRepo(t, dsn)
	defer cleanup()
	observer := newObserverPool(t, dsn)
	ctx := context.Background()

	const (
		projA = "prj0LBMVRACE1A000001"
		projB = "prj0LBMVRACE1B000001"
	)
	lb := newLB(projA, "")
	tg := newTG(projA, "")
	lst := newListener(lb.ID, projA, "", 8445)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
		_, err = w.Listeners().Insert(ctx, lst)
		require.NoError(t, err)
	})

	// TX1 — wire the listener; keep open.
	wirer, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer wirer.Abort()
	wire := *lst
	wire.DefaultTargetGroupID = option.MustNewOption(tg.ID)
	_, err = updateListenerOCC(ctx, wirer, &wire)
	require.NoError(t, err)

	// TX2 (concurrent) — move the LB (and, by cascade, its listeners) to project B.
	moveErrCh := make(chan error, 1)
	go func() {
		w, werr := repo.Writer(ctx)
		if werr != nil {
			moveErrCh <- werr
			return
		}
		defer w.Abort()
		if _, _, merr := w.LoadBalancers().MoveProject(ctx, string(lb.ID), projB); merr != nil {
			moveErrCh <- merr
			return
		}
		moveErrCh <- w.Commit()
	}()

	waitForLockWaiter(t, ctx, observer, 15*time.Second)
	require.NoError(t, wirer.Commit())

	moveErr := <-moveErrCh
	require.Error(t, moveErr,
		"moving an LB whose listener was concurrently wired must be rejected")
	assert.True(t, errors.Is(moveErr, kacho.ErrFailedPrecondition),
		"wired-listener LB move → FailedPrecondition, got %v", moveErr)
	assert.NotContains(t, moveErr.Error(), "SQLSTATE", "must not leak raw pgx text")

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	afterLB, err := rd.LoadBalancers().Get(ctx, string(lb.ID))
	require.NoError(t, err)
	assert.Equal(t, domain.ProjectID(projA), afterLB.ProjectID,
		"the rejected move must not have shifted the LoadBalancer")
	assertNoCrossProjectWiring(t, ctx, repo, string(lst.ID), string(tg.ID))
}
