// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"errors"
	"testing"

	"github.com/H-BF/corlib/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// NLB-1b MIGRATE (F4 / grind-note #3): the listener wires to a TargetGroup DIRECTLY
// via listeners.default_target_group_id → target_groups(id) FK ON DELETE RESTRICT
// (migration 0018). The M:N attached_target_groups pivot has been DROPPED (migration
// 0022, NLB CONTRACT) — the direct FK is now the only wiring path. These tests lock
// the DIRECT-FK contract; the legacy pivot-composite-FK tests were replaced
// (межфазовая эволюция, grind-note #2 EVOLUTION).

// tgReferencedByListenerMsg — verbatim contract text of the direct FK RESTRICT
// (TG.Delete while referenced by a listener). Part of the API contract.
const tgReferencedByListenerMsg = "target group is referenced by one or more listeners"

// seedLBTGWiredListener — LB + TG (same region) + listener WIRED directly to the
// TG with NO pivot attach. Returns the three domain objects.
func seedLBTGWiredListener(t testing.TB, repo kacho.Repository, projectID string) (*domain.LoadBalancer, *domain.TargetGroup, *domain.Listener) {
	t.Helper()
	ctx := context.Background()
	lb := newLB(projectID, "")
	tg := newTG(projectID, "")
	lst := newListener(lb.ID, projectID, "", 443)
	lst.DefaultTargetGroupID = option.MustNewOption(tg.ID)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
		_, err = w.Listeners().Insert(ctx, lst)
		require.NoError(t, err)
	})
	return lb, tg, lst
}

// TestListener_NLB_1_19_WireExistingTG_NoPivot_Happy — a listener may wire an
// EXISTING TargetGroup with NO pivot attachment: the direct FK requires only that
// the TG exists. resolved_backend_port° echoes the wired TargetGroup.port.
func TestListener_NLB_1_19_WireExistingTG_NoPivot_Happy(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	_, tg, lst := seedLBTGWiredListener(t, repo, "prj01WIRE0000000001")

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.Listeners().Get(ctx, string(lst.ID))
	require.NoError(t, err)
	val, ok := got.DefaultTargetGroupID.Maybe()
	require.True(t, ok, "listener must be wired to the TG without any pivot attach")
	assert.Equal(t, tg.ID, val)
	require.NotNil(t, got.ResolvedBackendPort, "resolved_backend_port° must echo TargetGroup.port")
	assert.Equal(t, int32(8080), *got.ResolvedBackendPort)
}

// TestListener_NLB_1_23_WireNonexistentTG_FailedPrecondition — wiring a
// well-formed-but-NONEXISTENT TargetGroup is rejected by the direct FK (23503 →
// FailedPrecondition, fixed contract tone, no pgx leak).
func TestListener_NLB_1_23_WireNonexistentTG_FailedPrecondition(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01NOEXISTTG000001", "")
	lst := newListener(lb.ID, "prj01NOEXISTTG000001", "", 443)
	lst.DefaultTargetGroupID = option.MustNewOption(domain.ResourceID("tgr00000000000000xx"))

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.LoadBalancers().Insert(ctx, lb)
	require.NoError(t, err)
	_, err = w.Listeners().Insert(ctx, lst)
	require.Error(t, err)
	assert.True(t, errors.Is(err, kacho.ErrFailedPrecondition), "direct FK 23503 → ErrFailedPrecondition, got %v", err)
	assert.NotContains(t, err.Error(), "SQLSTATE", "must not leak raw pgx text")
}

// TestTargetGroup_NLB_1_23_DeleteReferencedByListener_RESTRICT — deleting a
// TargetGroup that a listener references is blocked by ON DELETE RESTRICT (grind-note
// #3: fixed contract tone, no leak). After the listener clears its reference, the
// delete succeeds.
func TestTargetGroup_NLB_1_23_DeleteReferencedByListener_RESTRICT(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	_, tg, lst := seedLBTGWiredListener(t, repo, "prj01TGDELRESTR00001")

	// Delete blocked — the listener references the TG.
	func() {
		w, err := repo.Writer(ctx)
		require.NoError(t, err)
		defer w.Abort()
		derr := w.TargetGroups().Delete(ctx, string(tg.ID))
		require.Error(t, derr, "TG.Delete must be blocked by FK RESTRICT while a listener references it")
		assert.True(t, errors.Is(derr, kacho.ErrFailedPrecondition), "got %v", derr)
		assert.Contains(t, derr.Error(), tgReferencedByListenerMsg, "verbatim contract text required")
		assert.NotContains(t, derr.Error(), "SQLSTATE", "must not leak raw pgx text")
	}()

	// TG row survives the blocked delete.
	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	_, err = rd.TargetGroups().Get(ctx, string(tg.ID))
	require.NoError(t, err, "TG must survive blocked delete")
	_ = rd.Close()

	// Clear the listener reference → delete now succeeds.
	lst.DefaultTargetGroupID = option.ValueOf[domain.ResourceID]{}
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := updateListenerOCC(ctx, w, lst)
		require.NoError(t, err)
	})
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		require.NoError(t, w.TargetGroups().Delete(ctx, string(tg.ID)),
			"TG.Delete must succeed after the listener reference is cleared")
	})
}

// TestListener_NLB_1_22_RepointTargetGroup — repoint a listener onto a second
// region-coherent TargetGroup (LIVE-mutable); resolved_backend_port° echoes the new
// TargetGroup.port.
func TestListener_NLB_1_22_RepointTargetGroup(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	_, _, lst := seedLBTGWiredListener(t, repo, "prj01REPOINT00000001")

	tg2 := newTG("prj01REPOINT00000001", "")
	tg2.Port = 9090
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.TargetGroups().Insert(ctx, tg2)
		require.NoError(t, err)
	})

	lst.DefaultTargetGroupID = option.MustNewOption(tg2.ID)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		rec, err := updateListenerOCC(ctx, w, lst)
		require.NoError(t, err, "repoint to another existing TG must succeed")
		got, ok := rec.DefaultTargetGroupID.Maybe()
		require.True(t, ok)
		assert.Equal(t, tg2.ID, got)
	})

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.Listeners().Get(ctx, string(lst.ID))
	require.NoError(t, err)
	require.NotNil(t, got.ResolvedBackendPort)
	assert.Equal(t, int32(9090), *got.ResolvedBackendPort, "resolved_backend_port° echoes the repointed TG.port")
}

// updateListenerOCC — Get current xmin (OCC snapshot) then Update, mirroring the
// use-case read-modify-write flow (listenerWriter.Update enforces `WHERE
// xmin::text=$exp`). Plain error return so it is safe from goroutines.
func updateListenerOCC(ctx context.Context, w kacho.RepositoryWriter, l *domain.Listener) (*kacho.ListenerRecord, error) {
	cur, err := w.Listeners().Get(ctx, string(l.ID))
	if err != nil {
		return nil, err
	}
	return w.Listeners().Update(ctx, l, cur.Xmin)
}
