// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// REG-1-21 (F7) — явный CreateRepository без lifecycle → lifecycle°=DURABLE
// (survives-empty; durable-empty не исчезает), tagCount=0.
func TestRepository_REG_1_21_CreateDefaultDurable(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.CreateRepository(aliceCtx(), registry.CreateRepositorySpec{
		RegistryID: regID, Repository: "backend/api",
	})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	repo := opResponseRepository(t, done.Response)
	require.Equal(t, registryv1.RepositoryLifecycle_DURABLE, repo.GetLifecycle(), "явный intent-create → DURABLE")
	require.Equal(t, int32(0), repo.GetTagCount(), "survives-empty")
}

// REG-1-22 (F7) — явный вход lifecycle=EPHEMERAL перекрывает дефолт → lifecycle°=EPHEMERAL.
func TestRepository_REG_1_22_CreateExplicitEphemeral(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.CreateRepository(aliceCtx(), registry.CreateRepositorySpec{
		RegistryID: regID, Repository: "scratch/tmp", Lifecycle: domain.LifecycleEphemeral,
	})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	repo := opResponseRepository(t, done.Response)
	require.Equal(t, registryv1.RepositoryLifecycle_EPHEMERAL, repo.GetLifecycle(), "явный вход EPHEMERAL")
}

// REG-1-22 (F7, edge) — UNSPECIFIED явно → трактуется как омит (DURABLE by default).
func TestRepository_REG_1_22_UnspecifiedDefaultsDurable(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.CreateRepository(aliceCtx(), registry.CreateRepositorySpec{
		RegistryID: regID, Repository: "u/spec", Lifecycle: domain.LifecycleUnspecified,
	})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	require.Equal(t, registryv1.RepositoryLifecycle_DURABLE, opResponseRepository(t, done.Response).GetLifecycle())
}

// REG-1-23 (F7) — overlay-set на EPHEMERAL push-repo → auto-promote → DURABLE.
// Scenario A: overlay-строка есть c lifecycle=EPHEMERAL (создан явно EPHEMERAL) →
// UpdateRepository(description) поднимает до DURABLE.
func TestRepository_REG_1_23_AutoPromoteExistingEphemeral(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	cfg.byName["pushed/img"] = &domain.RepositoryConfig{
		RegistryID: regID, Name: "pushed/img", Visibility: domain.VisibilityPrivate, Lifecycle: domain.LifecycleEphemeral,
	}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.UpdateRepository(aliceCtx(), registry.UpdateRepositorySpec{
		RegistryID: regID, Repository: "pushed/img", Mask: []string{"description"}, Description: "now configured",
	})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	require.Equal(t, registryv1.RepositoryLifecycle_DURABLE, opResponseRepository(t, done.Response).GetLifecycle(),
		"overlay-set auto-promote EPHEMERAL→DURABLE")
}

// REG-1-23 (F7) — Scenario B: register-on-first-push (overlay-строки нет, есть проекция
// с тегами) → UpdateRepository промоутит через INSERT durable overlay (Lifecycle=DURABLE).
func TestRepository_REG_1_23_AutoPromoteFromProjection(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	zot := &mockZot{projByName: map[string]*domain.Repository{
		"pushed/img": {RegistryID: regID, Name: "pushed/img", TagCount: 2},
	}}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.UpdateRepository(aliceCtx(), registry.UpdateRepositorySpec{
		RegistryID: regID, Repository: "pushed/img", Mask: []string{"description"}, Description: "now configured",
	})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	require.Equal(t, registryv1.RepositoryLifecycle_DURABLE, opResponseRepository(t, done.Response).GetLifecycle(),
		"ephemeral projection promote → DURABLE overlay")
}

// REG-1-24 (F7) — lifecycle output-only: в UpdateRepository.update_mask → sync
// INVALID_ARGUMENT (system-managed; понижение DURABLE→EPHEMERAL не выразимо).
func TestRepository_REG_1_24_LifecycleInMaskRejected(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	_, err := uc.UpdateRepository(aliceCtx(), registry.UpdateRepositorySpec{
		RegistryID: regID, Repository: "backend/api", Mask: []string{"lifecycle"},
	})
	st := status.Convert(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
	require.Equal(t, "lifecycle is read-only (system-managed)", st.Message())
}
