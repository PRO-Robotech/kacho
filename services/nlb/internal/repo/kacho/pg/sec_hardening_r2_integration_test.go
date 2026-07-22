// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/H-BF/corlib/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// instTarget — helper: target с уникальным instance-id (для cap-тестов).
func instTarget(idx int) domain.Target {
	return domain.Target{
		InstanceID: option.MustNewOption(domain.InstanceID(fmt.Sprintf("inst%07d", idx))),
		Weight:     100,
	}
}

// --- Move ↔ wired-listener TOCTOU (cross-project wiring) ----------------------

// TestMoveProject_BlockedByWiredListener_Atomic — MoveProject должен атомарно
// отказывать, если у LB есть листенер, привязанный к TG (default_target_group_id
// set) — иначе LB проекта B унёс бы листенер, ссылающийся на TG проекта A. NLB
// CONTRACT: M:N pivot удалён; DB-level guard теперь `WHERE NOT EXISTS wired listener`.
func TestMoveProject_BlockedByWiredListener_Atomic(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01MOVE1234567890ll", "move-lb")
	tg := newTG("prj01MOVE1234567890ll", "move-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
		l := newListener(lb.ID, string(lb.ProjectID), "move-lst", 80)
		l.DefaultTargetGroupID = option.MustNewOption(tg.ID)
		_, err = w.Listeners().Insert(ctx, l)
		require.NoError(t, err)
	})

	// Move to a different project must be refused while a listener is wired to a TG.
	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.LoadBalancers().MoveProject(ctx, string(lb.ID), "prj02OTHER234567890ll")
	require.Error(t, err)
	assert.True(t, errors.Is(err, kacho.ErrFailedPrecondition),
		"wired-listener LB move must be FailedPrecondition, got %v", err)

	// Project unchanged.
	rd, _ := repo.Reader(ctx)
	defer func() { _ = rd.Close() }()
	got, err := rd.LoadBalancers().Get(ctx, string(lb.ID))
	require.NoError(t, err)
	assert.Equal(t, domain.ProjectID("prj01MOVE1234567890ll"), got.ProjectID,
		"project must be unchanged after refused move")
}

// TestMoveProject_Allowed_NoAttach — без attach'ей move проходит (regression).
func TestMoveProject_Allowed_NoAttach(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01MOVEOK234567890l", "move-ok-lb")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
	})

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		moved, err := w.LoadBalancers().MoveProject(ctx, string(lb.ID), "prj02MOVEOK234567890l")
		require.NoError(t, err)
		assert.Equal(t, domain.ProjectID("prj02MOVEOK234567890l"), moved.ProjectID)
	})
}

// TestMoveProject_NotFound — несуществующий LB → NotFound (не FailedPrecondition).
func TestMoveProject_NotFound(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.LoadBalancers().MoveProject(ctx, "nlbMISSING1234567890", "prj02OTHER234567890ll")
	require.Error(t, err)
	assert.True(t, errors.Is(err, kacho.ErrNotFound), "missing LB → NotFound, got %v", err)
}

// --- cumulative per-group target cap -----------------------------------------

// TestAddTargets_CumulativeCap — серия AddTargets не должна пробить
// MaxTargetsPerGroup (=100); превышающий вызов → FailedPrecondition, count не растёт.
func TestAddTargets_CumulativeCap(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	tg := newTG("prj0CAP01234567890lll", "cap-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
	})

	mkBatch := func(from, n int) []domain.Target {
		out := make([]domain.Target, 0, n)
		for i := from; i < from+n; i++ {
			out = append(out, instTarget(i))
		}
		return out
	}

	// 60 → ok.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		n, err := w.TargetGroups().AddTargets(ctx, string(tg.ID), mkBatch(0, 60))
		require.NoError(t, err)
		assert.Equal(t, 60, n)
	})
	// +60 → would be 120 > 100 → FailedPrecondition, nothing inserted.
	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	n, err := w.TargetGroups().AddTargets(ctx, string(tg.ID), mkBatch(60, 60))
	require.Error(t, err)
	assert.True(t, errors.Is(err, kacho.ErrFailedPrecondition), "cap breach → FailedPrecondition, got %v", err)
	assert.Equal(t, 0, n)
	w.Abort()

	// +40 → exactly 100 → ok.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		n, err := w.TargetGroups().AddTargets(ctx, string(tg.ID), mkBatch(60, 40))
		require.NoError(t, err)
		assert.Equal(t, 40, n)
	})
	// +1 → 101 > 100 → FailedPrecondition.
	w2, err := repo.Writer(ctx)
	require.NoError(t, err)
	_, err = w2.TargetGroups().AddTargets(ctx, string(tg.ID), mkBatch(100, 1))
	require.Error(t, err)
	assert.True(t, errors.Is(err, kacho.ErrFailedPrecondition), "over-cap → FailedPrecondition, got %v", err)
	w2.Abort()

	rd, _ := repo.Reader(ctx)
	defer func() { _ = rd.Close() }()
	targets, err := rd.TargetGroups().ListTargets(ctx, string(tg.ID))
	require.NoError(t, err)
	assert.Len(t, targets, 100, "group must be capped at 100")
}

// TestAddTargets_CumulativeCap_Concurrent — конкурентные AddTargets не должны
// суммарно пробить cap (FOR UPDATE на parent сериализует).
func TestAddTargets_CumulativeCap_Concurrent(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	tg := newTG("prj0CAPCONC34567890ll", "cap-conc-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
	})

	mkBatch := func(from, n int) []domain.Target {
		out := make([]domain.Target, 0, n)
		for i := from; i < from+n; i++ {
			out = append(out, instTarget(i))
		}
		return out
	}

	// Two goroutines each try to add 70 distinct targets. Cap is 100 → exactly
	// one full batch must win; the other must be rejected (FailedPrecondition).
	// Asserting only "<= cap" is a false-negative trap: a both-writers-fail
	// regression (final count 0) also satisfies it. We assert exactly-one-winner:
	// one Commit succeeds, the other observes the cap rejection, final count == 70.
	var (
		mu        sync.Mutex
		wins      int
		capReject int
	)
	// Start-барьер (зеркало runConcurrentAttachVIP): каждая goroutine открывает
	// СВОЙ writer-TX ДО сигнала старта, чтобы обе транзакции были провабельно
	// открыты и конкурировали за parent-row FOR UPDATE lock в один момент. Без
	// него планировщик может дать goroutine A закоммититься раньше, чем B откроет
	// TX — гонка вырождается в последовательность, и регрессия, снявшая row-lock
	// (software check-then-act), не была бы поймана детерминированно.
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(2)
	done.Add(2)
	for g := 0; g < 2; g++ {
		go func() {
			defer done.Done()
			w, err := repo.Writer(ctx)
			if err != nil {
				ready.Done()
				return
			}
			defer w.Abort()

			ready.Done()
			<-start // старт-барьер: обе writer-TX уже открыты (BeginTx eager)

			_, aerr := w.TargetGroups().AddTargets(ctx, string(tg.ID), mkBatch(g*1000, 70))
			if aerr == nil {
				if cerr := w.Commit(); cerr == nil {
					mu.Lock()
					wins++
					mu.Unlock()
				}
				return
			}
			if errors.Is(aerr, kacho.ErrFailedPrecondition) {
				mu.Lock()
				capReject++
				mu.Unlock()
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()

	assert.Equal(t, 1, wins, "exactly one concurrent AddTargets batch must win")
	assert.Equal(t, 1, capReject, "the losing batch must be rejected with FailedPrecondition (cap)")

	rd, _ := repo.Reader(ctx)
	defer func() { _ = rd.Close() }()
	targets, err := rd.TargetGroups().ListTargets(ctx, string(tg.ID))
	require.NoError(t, err)
	assert.Len(t, targets, 70, "exactly one full batch (70) must have committed — not 0, not >cap")
}

// --- deletion_protection atomic guard ----------------------------------------

// TestDeleteIfUnprotected_Guard — защищённый LB не удаляется атомарным guard'ом;
// снятие защиты открывает удаление; отсутствующий id → NotFound.
func TestDeleteIfUnprotected_Guard(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj0DELPRO34567890lll", "del-pro-lb")
	lb.DeletionProtection = true
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
	})

	// Protected → guard blocks.
	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	err = w.LoadBalancers().DeleteIfUnprotected(ctx, string(lb.ID))
	require.Error(t, err)
	assert.True(t, errors.Is(err, kacho.ErrFailedPrecondition),
		"protected LB delete → FailedPrecondition, got %v", err)
	w.Abort()

	// Row still there.
	rd, _ := repo.Reader(ctx)
	_, err = rd.LoadBalancers().Get(ctx, string(lb.ID))
	require.NoError(t, err)
	_ = rd.Close()

	// Missing id → NotFound.
	w2, err := repo.Writer(ctx)
	require.NoError(t, err)
	err = w2.LoadBalancers().DeleteIfUnprotected(ctx, "nlbMISSING1234567890")
	require.Error(t, err)
	assert.True(t, errors.Is(err, kacho.ErrNotFound), "missing LB → NotFound, got %v", err)
	w2.Abort()

	// Clear protection → delete succeeds.
	lb.DeletionProtection = false
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		cur, gerr := w.LoadBalancers().Get(ctx, string(lb.ID))
		require.NoError(t, gerr)
		_, err := w.LoadBalancers().Update(ctx, lb, cur.Xmin)
		require.NoError(t, err)
	})
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		require.NoError(t, w.LoadBalancers().DeleteIfUnprotected(ctx, string(lb.ID)))
	})
	rd2, _ := repo.Reader(ctx)
	defer func() { _ = rd2.Close() }()
	_, err = rd2.LoadBalancers().Get(ctx, string(lb.ID))
	assert.True(t, errors.Is(err, kacho.ErrNotFound), "LB must be gone after unprotected delete")
}

// --- 23505 constraint-specific messages --------------------------------------

// TestUnique_PortProto_Message — коллизия (lb, port, protocol) отдаёт сообщение
// про port/protocol, а НЕ про «name already exists».
func TestUnique_PortProto_Message(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj0PPMSG34567890llll", "ppmsg-lb")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.Listeners().Insert(ctx, newListener(lb.ID, string(lb.ProjectID), "lst-1", 80))
		require.NoError(t, err)
	})

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	dup := newListener(lb.ID, string(lb.ProjectID), "lst-2", 80) // same port+proto, different name
	dup.AllocatedAddress = "203.0.113.55"
	_, err = w.Listeners().Insert(ctx, dup)
	require.Error(t, err)
	require.True(t, errors.Is(err, kacho.ErrAlreadyExists), "got %v", err)
	assert.Contains(t, strings.ToLower(err.Error()), "port",
		"message must name the port/protocol conflict, not 'name': %v", err)
	assert.NotContains(t, strings.ToLower(err.Error()), "with name already exists",
		"must not mislabel port/protocol collision as name conflict: %v", err)
}
