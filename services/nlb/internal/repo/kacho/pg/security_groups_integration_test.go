// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestLB_NLB_1_51_SecurityGroupIds_RoundTrip — an INTERNAL LB persists + reads back
// security_group_ids (revived text[] column, wired into the repo). Update
// replace-whole is reflected.
func TestLB_NLB_1_51_SecurityGroupIds_RoundTrip(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01SG00000000000001", "sg-lb")
	lb.Type = domain.LBTypeInternal
	lb.PlacementType = domain.PlacementRegional
	lb.Placement = domain.PlacementInternalRegional
	lb.SecurityGroupIDs = []string{"sg-aaa", "sg-bbb"}

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		rec, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		assert.Equal(t, []string{"sg-aaa", "sg-bbb"}, rec.SecurityGroupIDs)
	})

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	got, err := rd.LoadBalancers().Get(ctx, string(lb.ID))
	require.NoError(t, err)
	assert.Equal(t, []string{"sg-aaa", "sg-bbb"}, got.SecurityGroupIDs)
	xmin := got.Xmin
	_ = rd.Close()

	// Update replace-whole.
	lb.SecurityGroupIDs = []string{"sg-ccc"}
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		rec, err := w.LoadBalancers().Update(ctx, lb, xmin)
		require.NoError(t, err)
		assert.Equal(t, []string{"sg-ccc"}, rec.SecurityGroupIDs)
	})
}

// TestLB_NLB_1_52_SecurityGroupIds_InternalCheck — the DB CHECK
// load_balancers_sg_internal_check rejects a non-empty security_group_ids set on a
// non-INTERNAL LB (23514 → ErrInvalidArg), backing the use-case INTERNAL-only guard.
func TestLB_NLB_1_52_SecurityGroupIds_InternalCheck(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01SG00000000000002", "sg-ext-lb") // newLB default Type = EXTERNAL
	lb.SecurityGroupIDs = []string{"sg-aaa"}

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.LoadBalancers().Insert(ctx, lb)
	require.Error(t, err)
	assert.True(t, errors.Is(err, kacho.ErrInvalidArg), "CHECK 23514 → ErrInvalidArg, got %v", err)
	assert.NotContains(t, err.Error(), "SQLSTATE", "must not leak raw pgx text")
}
