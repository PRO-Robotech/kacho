// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

// owner-tuple opgate — Listener.Create confirm-gate: OTG-03/04/05/05b. nlb
// owner-ресурсы НЕ были opgated до этого коммита. Контракт confirm-gate идентичен
// vpc P3. (seedParentLB / newCreateUC / contextWithSubject / awaitOpDone —
// из create_test.go / fakes_test.go этого же пакета.)

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/anypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

type fakeConfirmer struct {
	allow atomic.Bool
	mu    sync.Mutex
	calls int
}

func (f *fakeConfirmer) Confirm(_ context.Context, _ operations.Principal, _ string) (bool, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.allow.Load(), nil
}

func (f *fakeConfirmer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func shortDeadlineDispatch(t *testing.T, deadline time.Duration) confirmDispatcher {
	t.Helper()
	w := operations.NewWorker(
		operations.WithConfirmationDeadline(deadline),
		operations.WithTerminalWriteConfig(operations.TerminalWriteConfig{
			InitialInterval: 5 * time.Millisecond,
			MaxInterval:     20 * time.Millisecond,
			MaxElapsed:      200 * time.Millisecond,
		}),
	)
	w.Start()
	t.Cleanup(w.Stop)
	return func(ctx context.Context, or operations.Repo, opID string,
		fn func(context.Context) (*anypb.Any, error), confirm operations.ConfirmFunc) {
		operations.RunWithWorkerConfirm(w, ctx, or, opID, fn, confirm)
	}
}

func opgateListenerReq(lbID string) *lbv1.CreateListenerRequest {
	return &lbv1.CreateListenerRequest{
		LoadBalancerId: lbID,
		Name:           "otg",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetPort:     8080,
	}
}

// OTG-03 — op done только после confirm ALLOW (ordering).
func TestCreateListener_OTG03_DoneOnlyAfterConfirm(t *testing.T) {
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	uc := newCreateUC(repo, ops)
	fc := &fakeConfirmer{}
	uc.WithConfirmer(fc)

	op, err := uc.Run(contextWithSubject("user:test-actor"), opgateListenerReq(string(lb.ID)))
	require.NoError(t, err)

	end := time.Now().Add(120 * time.Millisecond)
	for time.Now().Before(end) {
		cur, _ := ops.Get(context.Background(), op.ID)
		require.False(t, cur.Done, "Listener op done пока confirmer DENY — окно 403 не закрыто")
		time.Sleep(5 * time.Millisecond)
	}
	require.GreaterOrEqual(t, fc.callCount(), 1)

	fc.allow.Store(true)
	saved := awaitOpDone(t, ops, op.ID, testTimeout)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error)
}

// OTG-04 — gate ON: op не done пока DENY (нет окна). N итераций.
func TestCreateListener_OTG04_NoWindowGateON(t *testing.T) {
	for i := 0; i < 5; i++ {
		repo := newFakeRepo()
		ops := newFakeOpsRepo()
		lb := seedParentLB(t, repo)
		uc := newCreateUC(repo, ops)
		fc := &fakeConfirmer{}
		uc.WithConfirmer(fc)
		op, err := uc.Run(contextWithSubject("user:test-actor"), opgateListenerReq(string(lb.ID)))
		require.NoError(t, err)

		end := time.Now().Add(50 * time.Millisecond)
		for time.Now().Before(end) {
			cur, _ := ops.Get(context.Background(), op.ID)
			require.False(t, cur.Done, "iteration %d: Listener op done пока DENY", i)
			time.Sleep(5 * time.Millisecond)
		}
		fc.allow.Store(true)
		saved := awaitOpDone(t, ops, op.ID, testTimeout)
		require.True(t, saved.Done)
		require.Nil(t, saved.Error)
	}
}

// OTG-05 / 05b — confirm timeout → op.error Unavailable, точный текст; resource-ref
// в op.metadata; ресурс durable (commit до confirm-timeout).
func TestCreateListener_OTG05_ConfirmTimeout_FailClosed(t *testing.T) {
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	uc := newCreateUC(repo, ops)
	fc := &fakeConfirmer{} // DENY forever
	uc.WithConfirmer(fc)
	uc.dispatch = shortDeadlineDispatch(t, 300*time.Millisecond)

	op, err := uc.Run(contextWithSubject("user:test-actor"), opgateListenerReq(string(lb.ID)))
	require.NoError(t, err)

	saved := awaitOpDone(t, ops, op.ID, testTimeout)
	require.NotNil(t, saved.Error, "confirm timeout → op.error")
	assert.Equal(t, int32(codes.Unavailable), saved.Error.Code)
	assert.NotEqual(t, int32(codes.DeadlineExceeded), saved.Error.Code)
	assert.Equal(t, "owner-tuple registration not confirmed", saved.Error.Message)
	assert.Nil(t, saved.Response)

	meta, merr := operations.MetadataFor[*lbv1.CreateListenerMetadata](saved)
	require.NoError(t, merr)
	require.NotEmpty(t, meta.GetListenerId(), "op.metadata несёт listener_id на error-терминале")
	require.Len(t, listenerByLB(repo, string(lb.ID)), 1, "Listener durable на timeout-ветке (commit до confirm)")
}
