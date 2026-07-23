// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// fakeSyncRegistrar — тестовая реализация порта registry.SyncRegistrar: записывает
// поданные register-intents и опц. возвращает инъектированную ошибку (проверка
// best-effort non-fatal контракта).
type fakeSyncRegistrar struct {
	mu    sync.Mutex
	calls [][]domain.RegisterIntent
	err   error
}

func (f *fakeSyncRegistrar) Register(_ context.Context, intents []domain.RegisterIntent) error {
	f.mu.Lock()
	cp := make([]domain.RegisterIntent, len(intents))
	copy(cp, intents)
	f.calls = append(f.calls, cp)
	f.mu.Unlock()
	return f.err
}

func (f *fakeSyncRegistrar) allIntents() []domain.RegisterIntent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.RegisterIntent
	for _, c := range f.calls {
		out = append(out, c...)
	}
	return out
}

// compile-time: fake удовлетворяет порту (ловит drift сигнатуры порта).
var _ registry.SyncRegistrar = (*fakeSyncRegistrar)(nil)

// TestCreateRepository_InvokesSyncRegistrar — CreateRepository после durable InsertConfig
// СИНХРОННО регистрирует register-type owner/parent tuple'ы через sync-registrar (immediate
// materialization), теми же intents, что эмитятся в outbox.
func TestCreateRepository_InvokesSyncRegistrar(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)
	sr := &fakeSyncRegistrar{}
	uc.WithSyncRegistrar(sr)

	op, err := uc.CreateRepository(aliceCtx(), registry.CreateRepositorySpec{RegistryID: regID, Repository: "team/app"})
	require.NoError(t, err)
	awaitOpDone(t, ops, op.ID)

	got := sr.allIntents()
	require.NotEmpty(t, got, "sync-registrar вызван register-intents CreateRepository")
	var hasRepoPush bool
	for _, in := range got {
		if in.Kind == "Repository" {
			hasRepoPush = true
		}
	}
	require.True(t, hasRepoPush, "adopt-owner (RepoPush) tuple зарегистрирован синхронно")

	// outbox-intents эмитированы в той же tx (drainer backstop цел — не подменён sync-путём).
	require.NotEmpty(t, cfg.allIntents(), "durable outbox register-intents эмитированы (at-least-once backstop)")
}

// TestCreateRepository_SyncRegistrarError_NonFatal — ошибка sync-registrar'а НЕ валит
// Create (best-effort): Operation успешна, а durable outbox-intents всё равно эмитированы
// (register-drainer подхватит at-least-once).
func TestCreateRepository_SyncRegistrarError_NonFatal(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)
	sr := &fakeSyncRegistrar{err: errors.New("iam down")}
	uc.WithSyncRegistrar(sr)

	op, err := uc.CreateRepository(aliceCtx(), registry.CreateRepositorySpec{RegistryID: regID, Repository: "team/app"})
	require.NoError(t, err, "sync-registrar error НЕ валит Create (best-effort non-fatal)")
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error, "Operation успешна несмотря на sync-registrar error")

	require.NotEmpty(t, sr.allIntents(), "sync-registrar был вызван")
	require.NotEmpty(t, cfg.allIntents(), "durable outbox-intents эмитированы (drainer backstop не тронут)")
}

// TestCreateRepository_NoSyncRegistrar_StillCreates — без sync-registrar'а (dev/no-iam)
// Create работает как раньше (только async outbox+drainer).
func TestCreateRepository_NoSyncRegistrar_StillCreates(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.CreateRepository(aliceCtx(), registry.CreateRepositorySpec{RegistryID: regID, Repository: "team/app"})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	require.NotEmpty(t, cfg.allIntents(), "outbox register-intents эмитированы")
}

// TestCreateRegistry_InvokesSyncRegistrar — Create (registry) после durable Insert
// синхронно регистрирует project-tuple + owner-tuple тем же intent'ом, что эмитится в outbox.
func TestCreateRegistry_InvokesSyncRegistrar(t *testing.T) {
	repo, zot, iam, ops := &mockRepo{}, &mockZot{}, &mockIAM{}, newMemOps()
	uc := newUC(repo, zot, iam, ops)
	sr := &fakeSyncRegistrar{}
	uc.WithSyncRegistrar(sr)

	op, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-P", Name: "team-images", RegionID: "eu-north-1"})
	require.NoError(t, err)
	awaitOpDone(t, ops, op.ID)

	got := sr.allIntents()
	require.NotEmpty(t, got, "sync-registrar вызван на Create registry")
	var hasProject, hasOwner bool
	for _, in := range got {
		for _, tup := range in.Tuples {
			switch tup.Relation {
			case domain.FGARelationProject:
				hasProject = true
			case domain.FGARelationOwner:
				hasOwner = true
			}
		}
	}
	require.True(t, hasProject, "project-tuple зарегистрирован синхронно")
	require.True(t, hasOwner, "owner-tuple зарегистрирован синхронно")
}

// TestCreateRegistry_SyncRegistrarError_NonFatal — ошибка sync-registrar'а на Create
// registry НЕ валит операцию (best-effort); outbox-intent эмитирован (drainer backstop).
func TestCreateRegistry_SyncRegistrarError_NonFatal(t *testing.T) {
	repo, zot, iam, ops := &mockRepo{}, &mockZot{}, &mockIAM{}, newMemOps()
	uc := newUC(repo, zot, iam, ops)
	sr := &fakeSyncRegistrar{err: errors.New("iam down")}
	uc.WithSyncRegistrar(sr)

	op, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-P", Name: "team-images", RegionID: "eu-north-1"})
	require.NoError(t, err, "sync-registrar error НЕ валит Create registry")
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)

	// writer.Insert получил durable register-intent (drainer backstop цел).
	repo.mu.Lock()
	gotIntent := repo.insertIntent
	repo.mu.Unlock()
	require.NotEmpty(t, gotIntent.Tuples, "durable register-intent эмитирован в writer-tx (backstop)")
}
