// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// NLB-1b MIGRATE F5 (NLB-1-33): ZONAL LoadBalancer VIP zone-coherence anchor.
// A ZONAL LB serves a single zone; every ZONAL auto-VIP listener of the same LB
// MUST resolve to the SAME zone. This is a WITHIN-service invariant across the LB's
// listener rows → enforced at the DB level via the set-once `vip_zone_id` anchor +
// atomic CAS (migration 0022), NOT a software sibling scan (data-integrity.md ban
// #10 — no TOCTOU). These tests lock the DB invariant at the repo layer, including
// under real goroutine concurrency (data-integrity.md §5: «concurrent-goroutine
// integration-тест на спорный путь обязателен — race не ловится unit-тестом»).
package pg_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestLoadBalancer_NLB_1_33_VIPZonePin_SetOnceCAS — the anchor pins on the first
// bind and is idempotent for the same zone; a mismatching zone → FAILED_PRECONDITION
// «load balancer VIP must be in the same zone».
func TestLoadBalancer_NLB_1_33_VIPZonePin_SetOnceCAS(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01VIPZONE00000001", "vip-zone-a")
	lb.RegionID = "eu-north"
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
	})

	// First pin → zone eu-north-a.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		require.NoError(t, w.LoadBalancers().PinVIPZoneCAS(ctx, string(lb.ID), "eu-north-a"))
	})
	// Same zone again → idempotent no-op (still matches).
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		require.NoError(t, w.LoadBalancers().PinVIPZoneCAS(ctx, string(lb.ID), "eu-north-a"))
	})

	// Different zone → FAILED_PRECONDITION with the contract tone.
	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	err = w.LoadBalancers().PinVIPZoneCAS(ctx, string(lb.ID), "eu-north-b")
	w.Abort()
	require.ErrorIs(t, err, kacho.ErrFailedPrecondition)
	assert.Contains(t, err.Error(), "load balancer VIP must be in the same zone")
}

// TestLoadBalancer_NLB_1_33_VIPZonePin_ConcurrentRace — two concurrent binds pin the
// SAME LB to DIFFERENT zones. Each opens its own writer-tx, locks the LB row
// (SELECT ... FOR NO KEY UPDATE via a listener INSERT would; here we lock explicitly
// through the CAS UPDATE), and pins. EXACTLY ONE wins (pins its zone); the other sees
// the pinned value and is rejected FAILED_PRECONDITION — not both, not last-writer.
// Deterministic under -race (the row lock serialises; no time.Sleep).
func TestLoadBalancer_NLB_1_33_VIPZonePin_ConcurrentRace(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01VIPZONER0000001", "vip-zone-race")
	lb.RegionID = "eu-north"
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
	})

	zones := []string{"eu-north-a", "eu-north-b"}
	var (
		mu        sync.Mutex
		winners   int
		conflicts []error
		other     []error
	)
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(len(zones))
	done.Add(len(zones))

	for _, z := range zones {
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
			<-start // barrier: both writer-TX open before either pins

			if err := w.LoadBalancers().PinVIPZoneCAS(ctx, string(lb.ID), z); err != nil {
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

	require.Empty(t, other, "no unexpected writer/commit errors: %v", other)
	require.Equal(t, 1, winners, "exactly one concurrent bind pins the VIP zone (set-once CAS atomic on DB)")
	require.Len(t, conflicts, 1, "the loser gets exactly one rejection")
	require.ErrorIs(t, conflicts[0], kacho.ErrFailedPrecondition)
	assert.Contains(t, conflicts[0].Error(), "load balancer VIP must be in the same zone")
}
