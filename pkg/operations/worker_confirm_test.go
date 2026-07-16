// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package operations_test

// Confirm-gate (owner-tuple opgate P1) — corelib-уровень OTG-03 / OTG-05.
//
// Гарантия: Create-Operation достигает success-`done` ТОЛЬКО после подтверждения
// (confirm) переданной пробой; иначе fail-closed по confirmation-deadline —
// MarkError(codes.Unavailable, "owner-tuple registration not confirmed"), НИКОГДА
// ложный success-done. nil confirm сохраняет сегодняшнее поведение (back-compat).
//
// Behavioral-lock (не только код): тесты проверяют НАБЛЮДАЕМОЕ — done-порядок,
// точный код+текст ошибки, отсутствие success-done без confirm, abandon на shutdown.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// confirmRepo — in-memory Repo для confirm-gate тестов. Терминальные записи
// (MarkDone / MarkError) только учитываются (worker-путь). onMarkDone — опциональный
// hook, дающий тесту засечь MarkDone «слишком рано» (до первого confirmed=true) —
// behavioral-lock ordering OTG-03.
type confirmRepo struct {
	mu         sync.Mutex
	done       map[string]*anypb.Any
	errored    map[string]*rpcstatus.Status
	onMarkDone func() // вызывается под mu ПЕРЕД записью done — ordering-guard
}

func newConfirmRepo() *confirmRepo {
	return &confirmRepo{
		done:    map[string]*anypb.Any{},
		errored: map[string]*rpcstatus.Status{},
	}
}

func (r *confirmRepo) Create(context.Context, operations.Operation) error { return nil }
func (r *confirmRepo) CreateWithPrincipal(context.Context, operations.Operation, operations.Principal) error {
	return nil
}
func (r *confirmRepo) Get(context.Context, string) (*operations.Operation, error) {
	return nil, operations.ErrNotFound
}
func (r *confirmRepo) List(context.Context, operations.ListFilter) ([]operations.Operation, string, error) {
	return nil, "", nil
}
func (r *confirmRepo) Cancel(context.Context, string) error { return nil }

func (r *confirmRepo) MarkDone(_ context.Context, id string, resp *anypb.Any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.onMarkDone != nil {
		r.onMarkDone()
	}
	r.done[id] = resp
	return nil
}

func (r *confirmRepo) MarkError(_ context.Context, id string, st *rpcstatus.Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errored[id] = st
	return nil
}

func (r *confirmRepo) doneCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.done)
}

func (r *confirmRepo) erroredCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.errored)
}

func (r *confirmRepo) getDone(id string) (*anypb.Any, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.done[id]
	return v, ok
}

func (r *confirmRepo) getError(id string) (*rpcstatus.Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.errored[id]
	return v, ok
}

// confirmWorker — Worker с override'нутыми confirmation-deadline (не 30s) и быстрым
// terminal-write budget (тестам не ждать секундами).
func confirmWorker(t *testing.T, confirmDeadline time.Duration, opts ...operations.WorkerOption) *operations.Worker {
	t.Helper()
	base := []operations.WorkerOption{
		operations.WithConfirmationDeadline(confirmDeadline),
		operations.WithTerminalWriteConfig(operations.TerminalWriteConfig{
			InitialInterval: 5 * time.Millisecond,
			MaxInterval:     20 * time.Millisecond,
			MaxElapsed:      200 * time.Millisecond,
		}),
	}
	w := operations.NewWorker(append(base, opts...)...)
	w.Start()
	t.Cleanup(w.Stop)
	return w
}

// (a) OTG-03 — op.done(success) наступает ТОЛЬКО после confirm (ordering).
// Пока confirm-проба DENY (pending) → операция остаётся done=false. Как только
// проба начинает ALLOW → MarkDone(resp). Момент MarkDone НЕ предшествует первому
// ALLOW (ordering-guard onMarkDone).
func TestWorker_Confirm_PendingThenAllow_MarkDoneOrdering(t *testing.T) {
	w := confirmWorker(t, 5*time.Second) // deadline большой — таймаут не должен сработать

	var allowed atomic.Bool
	var confirmCalls atomic.Int64
	var earlyDone atomic.Bool

	repo := newConfirmRepo()
	repo.onMarkDone = func() {
		// MarkDone до первого confirmed=true — нарушение confirm-gate.
		if !allowed.Load() {
			earlyDone.Store(true)
		}
	}

	resp := mustAny(t, wrapperspb.String("network-created"))
	confirm := func(context.Context) (bool, error) {
		confirmCalls.Add(1)
		return allowed.Load(), nil
	}

	operations.RunWithWorkerConfirm(w, context.Background(), repo, "op-otg03",
		func(context.Context) (*anypb.Any, error) { return resp, nil },
		confirm)

	// Confirm-loop крутится (≥2 пробы), но пока DENY — done НЕ выставлен.
	waitFor(t, 3*time.Second, func() bool { return confirmCalls.Load() >= 2 })
	assert.Equal(t, 0, repo.doneCount(), "op не должна быть done=true пока confirm DENY (pending)")
	assert.Equal(t, 0, repo.erroredCount(), "op не должна быть error пока в пределах deadline")

	// Регистрируем tuple → confirm начинает ALLOW.
	allowed.Store(true)

	waitFor(t, 3*time.Second, func() bool { return repo.doneCount() == 1 })
	assert.False(t, earlyDone.Load(), "MarkDone НЕ должен предшествовать первому confirmed=true (ordering OTG-03)")
	got, ok := repo.getDone("op-otg03")
	require.True(t, ok, "op-otg03 должна быть MarkDone после ALLOW")
	assert.True(t, proto2Equal(t, got, resp), "MarkDone должен нести response из fn")
	assert.Equal(t, 0, repo.erroredCount(), "success-путь не должен писать error")
}

// (b) OTG-05 — confirm никогда не подтверждён в deadline → op.error, code=Unavailable,
// точный текст, code != DeadlineExceeded, success-done НЕ выставлен.
func TestWorker_Confirm_NeverConfirmed_TimesOut_Unavailable(t *testing.T) {
	w := confirmWorker(t, 300*time.Millisecond)

	repo := newConfirmRepo()
	repo.onMarkDone = func() {
		t.Errorf("MarkDone(success) НЕ должен вызываться, когда confirm не достигнут за deadline")
	}

	resp := mustAny(t, wrapperspb.String("resource-durable-but-unconfirmed"))
	confirm := func(context.Context) (bool, error) {
		return false, nil // навсегда pending (моделирует FGA/IAM outage)
	}

	operations.RunWithWorkerConfirm(w, context.Background(), repo, "op-otg05",
		func(context.Context) (*anypb.Any, error) { return resp, nil },
		confirm)

	waitFor(t, 5*time.Second, func() bool { return repo.erroredCount() == 1 })

	st, ok := repo.getError("op-otg05")
	require.True(t, ok, "op-otg05 должна завершиться error по истечении confirmation-deadline")
	assert.Equal(t, codes.Unavailable, codes.Code(st.GetCode()),
		"timeout-код обязан быть Unavailable (retryable, fail-closed; FIX-1)")
	assert.NotEqual(t, codes.DeadlineExceeded, codes.Code(st.GetCode()),
		"код НЕ должен быть DeadlineExceeded (FIX-1: стабильный Unavailable)")
	assert.Equal(t, "owner-tuple registration not confirmed", st.GetMessage(),
		"стабильный текст — часть контракта (FIX-1)")
	assert.Equal(t, 0, repo.doneCount(), "invariant: success-done без confirm не выставляется НИКОГДА")
}

// (c) back-compat — nil confirm → MarkDone немедленно (сегодняшнее поведение
// Run / RunWithWorker сохранено; nil-safe вход RunWithConfirm).
func TestWorker_Confirm_Nil_MarkDoneImmediately_BackCompat(t *testing.T) {
	w := confirmWorker(t, 5*time.Second)

	repo := newConfirmRepo()
	resp := mustAny(t, wrapperspb.String("no-gate"))

	// nil confirm через явный RunWithWorkerConfirm.
	operations.RunWithWorkerConfirm(w, context.Background(), repo, "op-nilconfirm",
		func(context.Context) (*anypb.Any, error) { return resp, nil },
		nil)

	waitFor(t, 3*time.Second, func() bool { return repo.doneCount() == 1 })
	got, ok := repo.getDone("op-nilconfirm")
	require.True(t, ok)
	assert.True(t, proto2Equal(t, got, resp))
	assert.Equal(t, 0, repo.erroredCount(), "nil confirm не должен приводить к error")
}

// (c') back-compat — package-level RunWithConfirm с nil confirm тоже MarkDone.
func TestWorker_RunWithConfirm_Nil_BackCompat(t *testing.T) {
	repo := newConfirmRepo()
	resp := mustAny(t, wrapperspb.String("pkg-level-nil"))

	operations.RunWithConfirm(context.Background(), repo, "op-pkg-nil",
		func(context.Context) (*anypb.Any, error) { return resp, nil },
		nil)

	// default-registry: дожидаемся терминала через repo.
	waitFor(t, 3*time.Second, func() bool { return repo.doneCount() == 1 })
	assert.Equal(t, 0, repo.erroredCount())
}

// (d) -race — N независимо gated'ых dispatch'ей: пока флаг op'а не поднят —
// он pending; после подъёма всех — все done. Гонок нет (детектор -race).
func TestWorker_Confirm_ConcurrentIndependentGating(t *testing.T) {
	const n = 16
	w := confirmWorker(t, 10*time.Second, operations.WithMaxInflight(n))

	repo := newConfirmRepo()
	flags := make([]*atomic.Bool, n)
	for i := range flags {
		flags[i] = &atomic.Bool{}
	}

	for i := 0; i < n; i++ {
		resp := mustAny(t, wrapperspb.String(fmt.Sprintf("op-%d", i)))
		operations.RunWithWorkerConfirm(w, context.Background(), repo, fmt.Sprintf("op-race-%d", i),
			func(context.Context) (*anypb.Any, error) { return resp, nil },
			func(context.Context) (bool, error) { return flags[i].Load(), nil })
	}

	// Все gated → ни одна не должна быть done (даём confirm-loop'ам покрутиться).
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 0, repo.doneCount(), "gated op'ы не должны быть done до подъёма флага")

	// Поднимаем все флаги — каждая op независимо проходит confirm.
	for i := range flags {
		flags[i].Store(true)
	}

	waitFor(t, 5*time.Second, func() bool { return repo.doneCount() == n })
	assert.Equal(t, 0, repo.erroredCount(), "все op'ы должны пройти confirm без error")
}

// (e) shutdown mid-confirm → op остаётся done=false (не terminal): reconciler /
// next-start восстановит. Ни MarkDone, ни MarkError.
func TestWorker_Confirm_ShutdownMidConfirm_LeavesDoneFalse(t *testing.T) {
	// Большой deadline — таймаут не должен опередить Stop.
	w := operations.NewWorker(operations.WithConfirmationDeadline(30 * time.Second))
	w.Start()
	t.Cleanup(w.Stop)

	repo := newConfirmRepo()
	repo.onMarkDone = func() { t.Errorf("MarkDone не должен вызываться при abandon по shutdown") }

	var confirmCalls atomic.Int64
	resp := mustAny(t, wrapperspb.String("mid-confirm"))
	operations.RunWithWorkerConfirm(w, context.Background(), repo, "op-shutdown",
		func(context.Context) (*anypb.Any, error) { return resp, nil },
		func(context.Context) (bool, error) {
			confirmCalls.Add(1)
			return false, nil // pending, ждём shutdown
		})

	// Дожидаемся, что confirm-loop реально крутится.
	waitFor(t, 3*time.Second, func() bool { return confirmCalls.Load() >= 1 })

	// Shutdown mid-confirm.
	w.Stop()

	// Wait обязан завершиться (abandon освобождает wg-слот).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, w.Wait(ctx), "Wait должен завершиться — abandon по shutdown не должен ловить wg")

	assert.Equal(t, 0, repo.doneCount(), "abandon по shutdown → done=false (не MarkDone)")
	assert.Equal(t, 0, repo.erroredCount(), "abandon по shutdown → не terminal (не MarkError)")
}

// proto2Equal — anypb byte-equality helper (не тянуть protocmp в unit).
func proto2Equal(t *testing.T, a, b *anypb.Any) bool {
	t.Helper()
	if a == nil || b == nil {
		return a == b
	}
	return a.GetTypeUrl() == b.GetTypeUrl() && string(a.GetValue()) == string(b.GetValue())
}
