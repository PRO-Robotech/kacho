// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestLB_NLB_1b_AdminStatePlacement_RoundTrip — NLB-1b EXPAND additive columns
// (admin_state, placement) persist through Insert → Get (migrations 0016/0017).
func TestLB_NLB_1b_AdminStatePlacement_RoundTrip(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01EXPAND123456ll1", "expand-lb")
	lb.AdminState = domain.AdminStateDisabled
	lb.Placement = domain.PlacementExternalRegional // consistent with EXTERNAL
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
	})

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.LoadBalancers().Get(ctx, string(lb.ID))
	require.NoError(t, err)
	assert.Equal(t, domain.AdminStateDisabled, got.AdminState)
	assert.Equal(t, domain.PlacementExternalRegional, got.Placement)
}

// TestLB_NLB_1b_AdminState_EmptyCoercedEnabled — empty AdminState (thin builders /
// legacy) is normalised to ENABLED at Insert (adminStateParam + DB DEFAULT).
func TestLB_NLB_1b_AdminState_EmptyCoercedEnabled(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01EXPAND123456ll2", "coerce-lb") // AdminState unset
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		rec, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		assert.Equal(t, domain.AdminStateEnabled, rec.AdminState)
	})
}

// TestLB_NLB_1b_AdminState_UpdateRoundTrip — admin_state is LIVE-mutable through
// the repo Update (OCC on xmin).
func TestLB_NLB_1b_AdminState_UpdateRoundTrip(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01EXPAND123456ll3", "mut-lb")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
	})

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	cur, err := rd.LoadBalancers().Get(ctx, string(lb.ID))
	_ = rd.Close()
	require.NoError(t, err)
	require.Equal(t, domain.AdminStateEnabled, cur.AdminState)

	next := cur.LoadBalancer
	next.AdminState = domain.AdminStateDisabled
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		rec, err := w.LoadBalancers().Update(ctx, &next, cur.Xmin)
		require.NoError(t, err)
		assert.Equal(t, domain.AdminStateDisabled, rec.AdminState)
	})

	rd2, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd2.Close() }()
	got, err := rd2.LoadBalancers().Get(ctx, string(lb.ID))
	require.NoError(t, err)
	assert.Equal(t, domain.AdminStateDisabled, got.AdminState)
}

// TestLB_NLB_1b_CheckConstraints — DB-level invariants (data-integrity.md ban #10):
// an out-of-set admin_state / placement is rejected by the CHECK even if a caller
// bypasses domain.Validate (defense-in-depth, SQLSTATE 23514).
func TestLB_NLB_1b_CheckConstraints(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	t.Run("admin_state out of set rejected", func(t *testing.T) {
		lb := newLB("prj01EXPAND123456ll4", "bad-admin")
		lb.AdminState = domain.AdminState("PAUSED")
		w, err := repo.Writer(ctx)
		require.NoError(t, err)
		_, insErr := w.LoadBalancers().Insert(ctx, lb)
		require.Error(t, insErr, "admin_state=PAUSED must be rejected by DB CHECK")
		w.Abort()
	})

	t.Run("placement out of set rejected", func(t *testing.T) {
		lb := newLB("prj01EXPAND123456ll5", "bad-plc")
		lb.Placement = domain.Placement("EXTERNAL_ZONAL") // inexpressible by construction
		w, err := repo.Writer(ctx)
		require.NoError(t, err)
		_, insErr := w.LoadBalancers().Insert(ctx, lb)
		require.Error(t, insErr, "placement=EXTERNAL_ZONAL must be rejected by DB CHECK")
		w.Abort()
	})
}
