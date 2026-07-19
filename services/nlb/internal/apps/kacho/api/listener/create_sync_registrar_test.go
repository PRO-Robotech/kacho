// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// fakeSyncRegistrar — двойник Registrar-порта.
type fakeSyncRegistrar struct {
	mu      sync.Mutex
	intents []domain.FGARegisterIntent
	err     error
}

func (f *fakeSyncRegistrar) Register(_ context.Context, intent domain.FGARegisterIntent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.intents = append(f.intents, intent)
	return f.err
}

func (f *fakeSyncRegistrar) calls() []domain.FGARegisterIntent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.FGARegisterIntent, len(f.intents))
	copy(out, f.intents)
	return out
}

func newCreateUCWithRegistrar(repo *fakeRepo, ops *fakeOpsRepo, reg Registrar) *CreateUseCase {
	return newCreateUC(repo, ops).WithRegistrar(reg)
}

func baseListenerReq(lbID string) *lbv1.CreateListenerRequest {
	return &lbv1.CreateListenerRequest{
		LoadBalancerId: lbID,
		Name:           "https",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetPort:     8080,
	}
}

// TestCreateListener_SyncRegistrar_CalledPostCommit — sync-registrar вызывается
// после durable commit листенера с owner-intent (project-tuple первым). Durable
// `fga_register_outbox`-intent тоже на месте (backstop).
func TestCreateListener_SyncRegistrar_CalledPostCommit(t *testing.T) {
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	reg := &fakeSyncRegistrar{}
	uc := newCreateUCWithRegistrar(repo, ops, reg)

	op, err := uc.Run(contextWithSubject("user:test-actor"), baseListenerReq(string(lb.ID)))
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID, testTimeout).Error)

	calls := reg.calls()
	require.Len(t, calls, 1, "sync-registrar called once post-commit")
	require.Equal(t, "Listener", calls[0].Kind)
	require.NotEmpty(t, calls[0].Tuples)
	require.Equal(t, domain.FGARelationProject, calls[0].Tuples[0].Relation, "project (containment) tuple first")

	require.Len(t, repo.committedFGA(), 1, "durable fga_register_outbox intent present")
}

// TestCreateListener_SyncRegistrar_FailureIsBestEffort — сбой sync-Register НЕ
// фейлит Operation: op.done без error, листенер durable, outbox-intent на месте.
func TestCreateListener_SyncRegistrar_FailureIsBestEffort(t *testing.T) {
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	reg := &fakeSyncRegistrar{err: errors.New("iam unavailable")}
	uc := newCreateUCWithRegistrar(repo, ops, reg)

	op, err := uc.Run(contextWithSubject("user:test-actor"), baseListenerReq(string(lb.ID)))
	require.NoError(t, err)

	final := awaitOpDone(t, ops, op.ID, testTimeout)
	require.Nil(t, final.Error, "op done WITHOUT error — best-effort (ban #9)")

	got := listenerByLB(repo, string(lb.ID))
	require.Len(t, got, 1, "listener durable despite sync-register failure")
	require.Equal(t, domain.ListenerStatusActive, got[0].Status)

	require.Len(t, reg.calls(), 1, "sync-register attempted")
	require.Len(t, repo.committedFGA(), 1, "durable fga_register_outbox intent → drainer reconciles")
}
