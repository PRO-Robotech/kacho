// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package registry_test — F7 Repository lifecycle behavioural locks (REG-1-26..29):
// explicit Create=DURABLE default, opt-in EPHEMERAL, overlay-set auto-promote
// EPHEMERAL→DURABLE, lifecycle output-only (reject в UpdateMask). Use-case через mock-порты.
package registry_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// REG-1-26 — явный CreateRepository (без lifecycle-входа) → lifecycle=DURABLE by default
// (explicit intent = сохранить каркас); survives-empty (tagCount=0).
func TestRepository_REG_1_26_Create_DefaultDurable(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.CreateRepository(aliceCtx(), registry.CreateRepositorySpec{NamespaceID: regID, Repository: "backend/api"})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	repo := opResponseRepository(t, done.Response)
	require.Equal(t, registryv1.RepositoryLifecycle_DURABLE, repo.GetLifecycle(), "explicit Create → DURABLE by default")
	require.Equal(t, int32(0), repo.GetTagCount(), "durable survives-empty")
}

// REG-1-27 — CreateRepository(lifecycle=EPHEMERAL) → lifecycle=EPHEMERAL (явный опц.
// вход перекрывает дефолт; register-on-first-push семантика как эксплицитный рычаг).
func TestRepository_REG_1_27_Create_ExplicitEphemeral(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.CreateRepository(aliceCtx(), registry.CreateRepositorySpec{
		NamespaceID: regID, Repository: "scratch/tmp", Lifecycle: domain.LifecycleEphemeral,
	})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	repo := opResponseRepository(t, done.Response)
	require.Equal(t, registryv1.RepositoryLifecycle_EPHEMERAL, repo.GetLifecycle())
}

// REG-1-28 — overlay-set на EPHEMERAL push-repo (register-on-first-push, overlay-строки
// нет) → UpdateRepository(description) → auto-promote → lifecycle=DURABLE (наблюдаемо).
func TestRepository_REG_1_28_Update_AutoPromoteEphemeralToDurable(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	// ephemeral push-repo: проекция из zot без overlay-строки (cfg.byName пуст).
	zot := &mockZot{projByName: map[string]*domain.Repository{
		"pushed/img": {NamespaceID: regID, Name: "pushed/img", TagCount: 2},
	}}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.UpdateRepository(aliceCtx(), registry.UpdateRepositorySpec{
		NamespaceID: regID, Repository: "pushed/img", Mask: []string{"description"}, Description: "now configured",
	})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	repo := opResponseRepository(t, done.Response)
	require.Equal(t, registryv1.RepositoryLifecycle_DURABLE, repo.GetLifecycle(), "overlay-set auto-promotes EPHEMERAL→DURABLE")
}

// REG-1-29 — lifecycle output-only: в UpdateRepository.update_mask → INVALID_ARGUMENT
// (авторитетно управляется системой; тот же класс, что tagCount/fgaObject).
func TestRepository_REG_1_29_Update_LifecycleOutputOnly_Rejected(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	_, err := uc.UpdateRepository(aliceCtx(), registry.UpdateRepositorySpec{
		NamespaceID: regID, Repository: "backend/api", Mask: []string{"lifecycle"},
	})
	require.Equal(t, codes.InvalidArgument, codeOf(t, err))
}

// REG-1-26/28 (List) — lifecycle-проекция в ListRepositories: durable-overlay строка
// несёт DURABLE, ephemeral push-repo (проекция без overlay) — EPHEMERAL.
func TestRepository_REG_1_26_28_List_LifecycleProjection(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["backend/api"] = &domain.RepositoryConfig{
		NamespaceID: regID, Name: "backend/api", Visibility: domain.VisibilityPrivate, Lifecycle: domain.LifecycleDurable,
	}
	zot := &mockZot{listReposResult: []*domain.Repository{
		{NamespaceID: regID, Name: "backend/api", TagCount: 5}, // durable (есть overlay)
		{NamespaceID: regID, Name: "pushed/img", TagCount: 2},  // ephemeral (нет overlay)
	}}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	repos, _, err := uc.ListRepositories(aliceCtx(), registry.RepoListQuery{NamespaceID: regID})
	require.NoError(t, err)
	byName := map[string]domain.RepositoryLifecycle{}
	for _, r := range repos {
		byName[r.Name] = r.Lifecycle
	}
	require.Equal(t, domain.LifecycleDurable, byName["backend/api"], "durable overlay → DURABLE")
	require.Equal(t, domain.LifecycleEphemeral, byName["pushed/img"], "ephemeral проекция → EPHEMERAL")
}
