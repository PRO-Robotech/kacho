// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/H-BF/corlib/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// getLBStatus — читает текущий status LB (после срабатывания триггера recompute).
func getLBStatus(t *testing.T, repo kacho.Repository, id string) domain.LBStatus {
	t.Helper()
	ctx := context.Background()
	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.LoadBalancers().Get(ctx, id)
	require.NoError(t, err)
	return got.Status
}

// wiredListener — listener с резолвящейся target group (default_target_group_id set).
func wiredListener(lbID domain.ResourceID, projectID, name string, port int32, tgID domain.ResourceID) *domain.Listener {
	l := newListener(lbID, projectID, name, port)
	l.DefaultTargetGroupID = option.MustNewOption(tgID)
	return l
}

// TestLB_NLB_1b_StatusRecompute_InactiveToActive — NLB-1-17: enabled LB без
// listener'ов → INACTIVE; после wired-listener'а (резолвящийся targetGroupId) →
// ACTIVE. ACTIVE гейтится listener-TG-resolution, НЕ pivot-attach (F3/F4 rewrite).
func TestLB_NLB_1b_StatusRecompute_InactiveToActive(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const prj = "prj0GLOSS1ACT34567890"
	lb := newLB(prj, "gloss-active-lb") // Status=INACTIVE, AdminState unset→ENABLED
	tg := newTG(prj, "gloss-active-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
	})

	// No listeners yet → INACTIVE (config incomplete).
	assert.Equal(t, domain.LBStatusInactive, getLBStatus(t, repo, string(lb.ID)),
		"enabled LB without listeners must be INACTIVE")

	// Wired listener (resolving targetGroupId) → ACTIVE.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.Listeners().Insert(ctx, wiredListener(lb.ID, prj, "gloss-lst", 443, tg.ID))
		require.NoError(t, err)
	})
	assert.Equal(t, domain.LBStatusActive, getLBStatus(t, repo, string(lb.ID)),
		"enabled LB with a wired listener (targetGroupId resolves) must be ACTIVE")
}

// TestLB_NLB_1b_StatusRecompute_Degraded — NLB-1-18: enabled LB с listener'ом БЕЗ
// резолвящейся target group (пустой default_target_group_id) → DEGRADED
// (misconfigured, silent-blackhole не маскируется под ACTIVE).
func TestLB_NLB_1b_StatusRecompute_Degraded(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const prj = "prj0GLOSS2DEG34567890"
	lb := newLB(prj, "gloss-degraded-lb")
	tg := newTG(prj, "gloss-degraded-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
	})

	// Listener WITHOUT a target group (empty default_target_group_id) → DEGRADED.
	unwired := newListener(lb.ID, prj, "gloss-unwired", 80) // no DefaultTargetGroupID
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.Listeners().Insert(ctx, unwired)
		require.NoError(t, err)
	})
	assert.Equal(t, domain.LBStatusDegraded, getLBStatus(t, repo, string(lb.ID)),
		"a listener with no resolvable target group must degrade the LB")

	// A second, wired listener does NOT lift DEGRADED — EVERY listener must resolve.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.Listeners().Insert(ctx, wiredListener(lb.ID, prj, "gloss-wired2", 443, tg.ID))
		require.NoError(t, err)
	})
	assert.Equal(t, domain.LBStatusDegraded, getLBStatus(t, repo, string(lb.ID)),
		"one unresolved listener keeps the LB DEGRADED even with another wired one")

	// Removing the unresolved listener → all remaining resolve → ACTIVE.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		require.NoError(t, w.Listeners().Delete(ctx, string(unwired.ID)))
	})
	assert.Equal(t, domain.LBStatusActive, getLBStatus(t, repo, string(lb.ID)),
		"once every listener resolves its target group the LB is ACTIVE")
}

// TestLB_NLB_1b_StatusRecompute_DisabledFeed — NLB-1-13: admin_state=DISABLED feeds
// status → DISABLED (config intact); back to ENABLED re-derives from listeners.
func TestLB_NLB_1b_StatusRecompute_DisabledFeed(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const prj = "prj0GLOSS3DIS34567890"
	lb := newLB(prj, "gloss-disabled-lb")
	tg := newTG(prj, "gloss-disabled-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
		_, err = w.Listeners().Insert(ctx, wiredListener(lb.ID, prj, "gloss-lst3", 443, tg.ID))
		require.NoError(t, err)
	})
	require.Equal(t, domain.LBStatusActive, getLBStatus(t, repo, string(lb.ID)),
		"precondition: wired listener → ACTIVE")

	// Flip admin_state=DISABLED via repo Update → status feed → DISABLED.
	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	cur, err := rd.LoadBalancers().Get(ctx, string(lb.ID))
	_ = rd.Close()
	require.NoError(t, err)
	off := cur.LoadBalancer
	off.AdminState = domain.AdminStateDisabled
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Update(ctx, &off, cur.Xmin)
		require.NoError(t, err)
	})
	assert.Equal(t, domain.LBStatusDisabled, getLBStatus(t, repo, string(lb.ID)),
		"admin_state=DISABLED must drive status→DISABLED (config intact)")

	// Flip back to ENABLED → status re-derives from listeners → ACTIVE.
	rd2, err := repo.Reader(ctx)
	require.NoError(t, err)
	cur2, err := rd2.LoadBalancers().Get(ctx, string(lb.ID))
	_ = rd2.Close()
	require.NoError(t, err)
	on := cur2.LoadBalancer
	on.AdminState = domain.AdminStateEnabled
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Update(ctx, &on, cur2.Xmin)
		require.NoError(t, err)
	})
	assert.Equal(t, domain.LBStatusActive, getLBStatus(t, repo, string(lb.ID)),
		"admin_state back to ENABLED re-derives status from listener wiring → ACTIVE")
}
