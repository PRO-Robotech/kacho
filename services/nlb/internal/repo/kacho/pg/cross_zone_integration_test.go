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

// TestLB_NLB_1_16_CrossZoneEnabled_RoundTrip — a REGIONAL LB persists + reads back
// cross_zone_enabled (revived column, migration 0019).
func TestLB_NLB_1_16_CrossZoneEnabled_RoundTrip(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01CROSSZONE000001", "cz-regional")
	lb.Type = domain.LBTypeInternal
	lb.PlacementType = domain.PlacementRegional
	lb.Placement = domain.PlacementInternalRegional
	lb.CrossZoneEnabled = true

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		rec, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		assert.True(t, rec.CrossZoneEnabled, "cross_zone_enabled round-trips on INSERT RETURNING")
	})

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.LoadBalancers().Get(ctx, string(lb.ID))
	require.NoError(t, err)
	assert.True(t, got.CrossZoneEnabled, "cross_zone_enabled round-trips on Get")
}

// TestLB_NLB_1_16_CrossZoneEnabled_ZonalCheck — the DB CHECK
// load_balancers_cross_zone_placement_check rejects cross_zone_enabled=true on a
// ZONAL LB (23514 → ErrInvalidArg) as defense-in-depth behind the use-case guard.
func TestLB_NLB_1_16_CrossZoneEnabled_ZonalCheck(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01CROSSZONE000002", "cz-zonal")
	lb.Type = domain.LBTypeInternal
	lb.PlacementType = domain.PlacementZonal
	lb.Placement = domain.PlacementInternalZonal
	lb.CrossZoneEnabled = true // illegal on ZONAL

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.LoadBalancers().Insert(ctx, lb)
	require.Error(t, err)
	assert.True(t, errors.Is(err, kacho.ErrInvalidArg), "CHECK 23514 → ErrInvalidArg, got %v", err)
	assert.NotContains(t, err.Error(), "SQLSTATE", "must not leak raw pgx text")
}
