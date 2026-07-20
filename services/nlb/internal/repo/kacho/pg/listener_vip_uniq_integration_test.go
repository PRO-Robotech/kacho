// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// NLB-1b MIGRATE F5 (NLB-1-30/31/55): VIP-anchor relocation LoadBalancer→Listener.
// The authoritative VIP uniqueness invariant returns to the Listener level as the
// partial-UNIQUE `listeners_region_vip_uniq (region_id, allocated_address, port,
// protocol) WHERE status<>'DELETING' AND allocated_address<>”` (baseline 0001,
// dropped in 0009 when VIP was consolidated on the LB, re-created in migration
// 0021). These tests lock the DB-enforced invariant at the repo layer under real
// goroutine concurrency (data-integrity.md: «concurrent-goroutine integration-тест
// на спорный путь обязателен — race не ловится unit-тестом»):
//
//   - NLB-1-30: two listeners on the same (region, ip, port, protocol) → the second
//     is rejected ALREADY_EXISTS; the same ip on a different (port,protocol) is fine.
//   - NLB-1-31: concurrent Create on one VIP → exactly one winner, one ALREADY_EXISTS.
//   - NLB-1-55: recycle-on-delete — a VIP held by a DELETING (or deleted) listener is
//     re-claimable (partial index excludes DELETING; create→delete→re-create passes).
//   - scope proof: the same VIP in DIFFERENT regions does not conflict (key = region).
package pg_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// vipListener builds a fresh domain.Listener wired to lb, carrying a resolved VIP
// (allocated_address) — the shape a VIP-anchored Listener.Create worker persists.
func vipListener(lbID domain.ResourceID, projectID, name, ip string, port int32) *domain.Listener {
	l := newListener(lbID, projectID, name, port)
	l.AllocatedAddress = domain.IPAddress(ip)
	return l
}

// TestListener_NLB_1_30_VIPConflict_SameRegionIPPortProto — partial-UNIQUE
// (region_id, allocated_address, port, protocol): two listeners on DIFFERENT LBs in
// the SAME region binding the SAME VIP:port/proto → the second is ALREADY_EXISTS
// («address already in use»). Two listeners on the same VIP but a DIFFERENT port are
// allowed (443/TCP and 8443/TCP coexist on one address).
func TestListener_NLB_1_30_VIPConflict_SameRegionIPPortProto(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const region = "ru-central1"
	const vip = "203.0.113.40"

	lbA := newLB("prj01VIPUNIQ00000001", "vip-uniq-a")
	lbB := newLB("prj01VIPUNIQ00000001", "vip-uniq-b")
	lbA.RegionID = region
	lbB.RegionID = region
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lbA)
		require.NoError(t, err)
		_, err = w.LoadBalancers().Insert(ctx, lbB)
		require.NoError(t, err)
		// Listener A holds VIP 203.0.113.40:443/TCP in the region.
		_, err = w.Listeners().Insert(ctx, vipListener(lbA.ID, string(lbA.ProjectID), "lst-a", vip, 443))
		require.NoError(t, err)
	})

	// Listener B on the SAME (region, ip, port=443, proto=TCP) → ALREADY_EXISTS.
	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Listeners().Insert(ctx, vipListener(lbB.ID, string(lbB.ProjectID), "lst-b", vip, 443))
	w.Abort()
	require.Error(t, err)
	require.ErrorIs(t, err, kacho.ErrAlreadyExists, "same VIP:port/proto in region must collide")
	assert.Contains(t, err.Error(), "address already in use")

	// Same VIP but a DIFFERENT port (8443/TCP) is allowed — two listeners can share
	// one address on distinct (port,protocol) pairs.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.Listeners().Insert(ctx, vipListener(lbB.ID, string(lbB.ProjectID), "lst-b2", vip, 8443))
		require.NoError(t, err, "same VIP on a different port must NOT collide")
	})
}

// TestListener_NLB_1_55_VIPRecycleOnDelete — recycle-on-delete (data-integrity.md
// B17). A VIP held by a DELETING listener is excluded from the partial index, so it
// is immediately re-claimable; and create→delete→re-create of the same VIP passes
// (the lease is not orphaned). Without recycle the partial-UNIQUE would keep the
// slot occupied and exhaust the pool under parallel e2e.
func TestListener_NLB_1_55_VIPRecycleOnDelete(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const region = "ru-central1"
	const vip = "203.0.113.41"

	lbA := newLB("prj01VIPRECYC0000001", "vip-recyc-a")
	lbB := newLB("prj01VIPRECYC0000001", "vip-recyc-b")
	lbA.RegionID = region
	lbB.RegionID = region
	lstA := vipListener(lbA.ID, string(lbA.ProjectID), "lst-a", vip, 443)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lbA)
		require.NoError(t, err)
		_, err = w.LoadBalancers().Insert(ctx, lbB)
		require.NoError(t, err)
		_, err = w.Listeners().Insert(ctx, lstA)
		require.NoError(t, err)
	})

	// While A is ACTIVE the VIP is taken.
	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Listeners().Insert(ctx, vipListener(lbB.ID, string(lbB.ProjectID), "lst-b", vip, 443))
	w.Abort()
	require.ErrorIs(t, err, kacho.ErrAlreadyExists, "VIP held by an ACTIVE listener is taken")

	// Move A to DELETING — the partial index (WHERE status<>'DELETING') excludes it,
	// so the VIP is immediately re-claimable by B.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.Listeners().SetStatusCAS(ctx, string(lstA.ID),
			domain.ListenerStatusActive, domain.ListenerStatusDeleting)
		require.NoError(t, err)
	})
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.Listeners().Insert(ctx, vipListener(lbB.ID, string(lbB.ProjectID), "lst-b", vip, 443))
		require.NoError(t, err, "VIP of a DELETING listener must be re-claimable (recycle)")
	})

	// create→delete→re-create of the same VIP passes: fully delete A's row, then a
	// fresh listener on the same VIP (a third LB, distinct port to avoid B's slot).
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		require.NoError(t, w.Listeners().Delete(ctx, string(lstA.ID)))
		_, err := w.Listeners().Insert(ctx, vipListener(lbA.ID, string(lbA.ProjectID), "lst-a2", vip, 8443))
		require.NoError(t, err, "create→delete→re-create of the same VIP must pass")
	})
}

// TestListener_VIPUniq_CrossRegionScope — the partial-UNIQUE is keyed on region_id:
// the same VIP:port/proto in DIFFERENT regions does NOT conflict (proves scope).
func TestListener_VIPUniq_CrossRegionScope(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const vip = "203.0.113.42"
	lbA := newLB("prj01VIPXREG00000001", "vip-xreg-a")
	lbB := newLB("prj01VIPXREG00000001", "vip-xreg-b")
	lbA.RegionID = "region-1"
	lbB.RegionID = "region-2"
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lbA)
		require.NoError(t, err)
		_, err = w.LoadBalancers().Insert(ctx, lbB)
		require.NoError(t, err)
		_, err = w.Listeners().Insert(ctx, vipListener(lbA.ID, string(lbA.ProjectID), "lst-a", vip, 443))
		require.NoError(t, err)
		_, err = w.Listeners().Insert(ctx, vipListener(lbB.ID, string(lbB.ProjectID), "lst-b", vip, 443))
		require.NoError(t, err, "same VIP in a different region must NOT conflict")
	})
}

// runConcurrentInsertListener runs one goroutine per candidate listener. Each opens
// its OWN writer-tx BEFORE the start barrier (real transaction concurrency, not a
// sequence), then on the barrier inserts its listener: the winner commits, the loser
// records the rejection and rolls back. The ready barrier guarantees every tx is
// open before any touches the index — else the race degenerates to a sequence.
func runConcurrentInsertListener(t *testing.T, repo kacho.Repository, cands []*domain.Listener) (winners int, conflicts []error, other []error) {
	t.Helper()
	ctx := context.Background()
	var mu sync.Mutex

	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(len(cands))
	done.Add(len(cands))

	for _, c := range cands {
		go func() {
			defer done.Done()
			w, err := repo.Writer(ctx)
			if err != nil {
				ready.Done()
				mu.Lock()
				other = append(other, err)
				mu.Unlock()
				return
			}
			committed := false
			defer func() {
				if !committed {
					w.Abort()
				}
			}()
			ready.Done()
			<-start // barrier: all writer-TX open (BeginTx eager)

			if _, err := w.Listeners().Insert(ctx, c); err != nil {
				mu.Lock()
				conflicts = append(conflicts, err)
				mu.Unlock()
				return
			}
			if err := w.Commit(); err != nil {
				mu.Lock()
				other = append(other, err)
				mu.Unlock()
				return
			}
			committed = true
			mu.Lock()
			winners++
			mu.Unlock()
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return winners, conflicts, other
}

// TestListener_NLB_1_31_VIPConcurrentRace — two concurrent Create on the SAME
// (region, ip, port=443, proto=TCP) (BYO on two different LBs) → EXACTLY ONE commits
// (partial-UNIQUE holds), the other gets ALREADY_EXISTS «address already in use».
// Not second-writer-wins, not both. Deterministic under -race (the index tuple lock
// serialises the writers; no time.Sleep).
func TestListener_NLB_1_31_VIPConcurrentRace(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const region = "ru-central1"
	const vip = "203.0.113.43"
	lbA := newLB("prj01VIPRACE00000001", "vip-race-a")
	lbB := newLB("prj01VIPRACE00000001", "vip-race-b")
	lbA.RegionID = region
	lbB.RegionID = region
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lbA)
		require.NoError(t, err)
		_, err = w.LoadBalancers().Insert(ctx, lbB)
		require.NoError(t, err)
	})

	winners, conflicts, other := runConcurrentInsertListener(t, repo, []*domain.Listener{
		vipListener(lbA.ID, string(lbA.ProjectID), "lst-race-a", vip, 443),
		vipListener(lbB.ID, string(lbB.ProjectID), "lst-race-b", vip, 443),
	})

	require.Empty(t, other, "no unexpected writer/commit errors: %v", other)
	require.Equal(t, 1, winners, "exactly one concurrent Create binds the VIP (partial-UNIQUE atomic on DB)")
	require.Len(t, conflicts, 1, "the loser gets exactly one rejection")
	require.ErrorIs(t, conflicts[0], kacho.ErrAlreadyExists)
	msg := conflicts[0].Error()
	assert.Contains(t, msg, "address already in use")
	// Anti-oracle: the generic conflict must not leak the winner's identity.
	for _, leak := range []string{string(lbA.ID), string(lbB.ID)} {
		assert.NotContains(t, msg, leak)
	}
}
