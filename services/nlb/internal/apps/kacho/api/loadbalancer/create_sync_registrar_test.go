// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// fakeSyncRegistrar — двойник Registrar-порта: пишет каждый переданный intent,
// возвращает scripted err (для backstop-теста).
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

func internalZonalReq() *lbv1.CreateNetworkLoadBalancerRequest {
	req := baseCreateReq()
	req.Type = lbv1.NetworkLoadBalancer_INTERNAL
	req.PlacementType = lbv1.NetworkLoadBalancer_ZONAL
	req.V4Source = vipSubnet(lbTestSubnetZonal)
	return req
}

// TestCreate_SyncRegistrar_CalledPostCommit — после durable commit LB
// sync-registrar вызывается РОВНО с тем же owner-intent, что эмитится в
// `fga_register_outbox` (project-tuple первым). Durable outbox-intent ТОЖЕ
// присутствует (at-least-once backstop). Закрывает async-only окно видимости.
func TestCreate_SyncRegistrar_CalledPostCommit(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	reg := &fakeSyncRegistrar{}
	uc := newCreateUC(repo, opsRepo, createDeps{subnet: &fakeSubnetClient{placement: "ZONAL"}}).
		WithRegistrar(reg)

	op, err := uc.Execute(context.Background(), internalZonalReq())
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	rec := lbByName(t, repo, "lb-1")

	calls := reg.calls()
	require.Len(t, calls, 1, "sync-registrar called once post-commit")
	require.Equal(t, string(rec.ID), calls[0].ResourceID)
	require.NotEmpty(t, calls[0].Tuples)
	require.Equal(t, domain.FGARelationProject, calls[0].Tuples[0].Relation, "project (containment) tuple first")
	require.Equal(t, "project:prj-a", calls[0].Tuples[0].SubjectID)

	// durable outbox-intent тоже на месте (backstop drainer).
	require.Len(t, repo.fga, 1, "fga_register_outbox intent durable (at-least-once backstop)")
	require.Equal(t, domain.FGAEventRegister, repo.fga[0].EventType)
}

// TestCreate_SyncRegistrar_FailureIsBestEffort — сбой sync-Register НЕ фейлит
// Operation: op.done без error, ресурс durable, `fga_register_outbox`-строка на
// месте (drainer досведёт). НЕ phantom (ban #9).
func TestCreate_SyncRegistrar_FailureIsBestEffort(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	reg := &fakeSyncRegistrar{err: errors.New("iam unavailable")}
	uc := newCreateUC(repo, opsRepo, createDeps{subnet: &fakeSubnetClient{placement: "ZONAL"}}).
		WithRegistrar(reg)

	op, err := uc.Execute(context.Background(), internalZonalReq())
	require.NoError(t, err, "Execute returns Operation; sync-register failure is not a sync error")

	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, final.Error, "op done WITHOUT error — sync-register failure is best-effort (ban #9)")

	rec := lbByName(t, repo, "lb-1")
	require.Equal(t, domain.LBStatusInactive, rec.Status, "resource durable despite sync-register failure")

	require.Len(t, reg.calls(), 1, "sync-register was attempted")
	require.Len(t, repo.fga, 1, "durable fga_register_outbox intent present → drainer reconciles")
}

// TestCreate_NoRegistrar_AsyncOnly — без registrar (dev/no-iam) Create проходит:
// op.done, ресурс durable, только `fga_register_outbox`-intent (async drainer).
func TestCreate_NoRegistrar_AsyncOnly(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := newCreateUC(repo, opsRepo, createDeps{subnet: &fakeSubnetClient{placement: "ZONAL"}})

	op, err := uc.Execute(context.Background(), internalZonalReq())
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
	require.Len(t, repo.fga, 1)
}
