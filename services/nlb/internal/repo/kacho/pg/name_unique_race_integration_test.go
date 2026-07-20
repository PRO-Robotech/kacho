// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// NLB-1b MIGRATE (grind-note #1): the name-UNIQUE invariant is asserted in a
// dedicated concurrent-race case for EACH of the three resources — LoadBalancer,
// Listener, TargetGroup — so the TG/Listener assertions do not fall through the
// carve seam between 1b/1c. LB name-race lives in TestLB_ConcurrentInsertSameName;
// these lock TG (target_groups_project_name_uniq) and Listener (listeners_lb_name_uniq).

// TestTargetGroup_NLB_1_49_ConcurrentInsertSameName — two concurrent TargetGroup
// inserts with the same (project, name): exactly one commits, the other gets
// ErrAlreadyExists (partial UNIQUE target_groups_project_name_uniq). Deterministic
// under -race (partial-UNIQUE serialises; not second-writer-wins).
func TestTargetGroup_NLB_1_49_ConcurrentInsertSameName(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const project = "prj01TGNAMERACE00001"
	const name = "race-tg"

	var wg sync.WaitGroup
	var successes, conflicts int
	var mu sync.Mutex
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tg := newTG(project, name)
			w, err := repo.Writer(ctx)
			if err != nil {
				return
			}
			defer w.Abort()
			if _, err := w.TargetGroups().Insert(ctx, tg); err != nil {
				recordRaceOutcome(&mu, err, &successes, &conflicts, false)
				return
			}
			recordRaceOutcome(&mu, w.Commit(), &successes, &conflicts, true)
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, successes, "exactly one TargetGroup Insert succeeds")
	assert.Equal(t, 1, conflicts, "the other gets ErrAlreadyExists")
}

// TestListener_NLB_1_49_ConcurrentInsertSameName — two concurrent Listener inserts
// with the same (load_balancer_id, name) but DISTINCT ports (so the sole conflicting
// index is the name-UNIQUE one): exactly one commits, the other gets
// ErrAlreadyExists (partial UNIQUE listeners_lb_name_uniq).
func TestListener_NLB_1_49_ConcurrentInsertSameName(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const project = "prj01LSTNAMERACE0001"
	lb := newLB(project, "")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
	})

	var wg sync.WaitGroup
	var successes, conflicts int
	var mu sync.Mutex
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Same name, distinct port so the ONLY conflicting index is the
			// name-UNIQUE one (isolates the invariant under test from listeners_lb_port_proto_uniq).
			lst := newListener(lb.ID, project, "race-lst", int32(443+i))
			w, err := repo.Writer(ctx)
			if err != nil {
				return
			}
			defer w.Abort()
			if _, err := w.Listeners().Insert(ctx, lst); err != nil {
				recordRaceOutcome(&mu, err, &successes, &conflicts, false)
				return
			}
			recordRaceOutcome(&mu, w.Commit(), &successes, &conflicts, true)
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, successes, "exactly one Listener Insert succeeds")
	assert.Equal(t, 1, conflicts, "the other gets ErrAlreadyExists")
}

// recordRaceOutcome — classify one goroutine's outcome under the shared mutex:
// committed with nil err → success; ErrAlreadyExists → conflict. Other errors are
// ignored (transient pool/setup), matching TestLB_ConcurrentInsertSameName.
func recordRaceOutcome(mu *sync.Mutex, err error, successes, conflicts *int, committed bool) {
	mu.Lock()
	defer mu.Unlock()
	switch {
	case err == nil && committed:
		*successes++
	case err != nil && errors.Is(err, kacho.ErrAlreadyExists):
		*conflicts++
	}
}
