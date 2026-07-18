// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

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

// TestCreateTG_SyncRegistrar_CalledPostCommit — sync-registrar вызывается после
// durable commit TG с owner-intent (project-tuple первым). Durable outbox-intent
// тоже на месте (at-least-once backstop).
func TestCreateTG_SyncRegistrar_CalledPostCommit(t *testing.T) {
	repo := newFakeRepo()
	opsRepo := newFakeOpsRepo()
	reg := &fakeSyncRegistrar{}
	uc := mkUC(repo, opsRepo).WithRegistrar(reg)

	ctx := contextWithUser("alice")
	op, err := uc.Execute(ctx, mkCreateReq("prj-fga", "ru-central1", "tg-fga"))
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	calls := reg.calls()
	require.Len(t, calls, 1, "sync-registrar called once post-commit")
	require.Equal(t, "TargetGroup", calls[0].Kind)
	require.NotEmpty(t, calls[0].Tuples)
	require.Equal(t, domain.FGARelationProject, calls[0].Tuples[0].Relation, "project (containment) tuple first")
	require.Equal(t, "project:prj-fga", calls[0].Tuples[0].SubjectID)

	require.Len(t, repo.fga, 1, "durable fga_register_outbox intent present")
}

// TestCreateTG_SyncRegistrar_FailureIsBestEffort — сбой sync-Register НЕ фейлит
// Operation: op.done без error, TG durable, outbox-intent на месте (drainer досведёт).
func TestCreateTG_SyncRegistrar_FailureIsBestEffort(t *testing.T) {
	repo := newFakeRepo()
	opsRepo := newFakeOpsRepo()
	reg := &fakeSyncRegistrar{err: errors.New("iam unavailable")}
	uc := mkUC(repo, opsRepo).WithRegistrar(reg)

	ctx := contextWithUser("alice")
	op, err := uc.Execute(ctx, mkCreateReq("prj-fga", "ru-central1", "tg-fga"))
	require.NoError(t, err)

	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, final.Error, "op done WITHOUT error — best-effort (ban #9)")

	require.Len(t, reg.calls(), 1, "sync-register attempted")
	require.Len(t, repo.fga, 1, "durable fga_register_outbox intent → drainer reconciles")
}
