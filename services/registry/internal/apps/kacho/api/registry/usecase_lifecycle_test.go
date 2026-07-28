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

// EPHEMERAL on create promises "register-on-first-push / auto-removed-when-empty".
// No branch in the service reads the stored value — the row it produces survives an
// empty repository, is listed, and is fetched exactly like a DURABLE one; only the
// echoed enum differs. Accepting it therefore promised a capability that does not
// exist, so the input is refused until the behaviour is built (api-conventions.md
// §«Принято-и-проигнорировано»). The refused field is named in the details, where
// the contract carries field identity — the message stays the generic one.
func TestRepository_CreateEphemeralLifecycleRejected(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	_, err := uc.CreateRepository(aliceCtx(), registry.CreateRepositorySpec{
		RegistryID: regID, Repository: "scratch/tmp", Lifecycle: domain.LifecycleEphemeral,
	})
	require.Error(t, err, "an input nothing acts on must not be accepted")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	requireFieldViolation(t, err, "lifecycle")
	require.Empty(t, cfg.byName, "nothing may be written for a refused create")
}

// An out-of-range enum was silently coerced to DURABLE — the same silent acceptance,
// one step further from what the caller asked for. It is refused by the same lane.
func TestRepository_CreateUnknownLifecycleRejected(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	_, err := uc.CreateRepository(aliceCtx(), registry.CreateRepositorySpec{
		RegistryID: regID, Repository: "scratch/oob", Lifecycle: domain.Lifecycle(7),
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	requireFieldViolation(t, err, "lifecycle")
	require.Empty(t, cfg.byName, "nothing may be written for a refused create")
}

// Explicit DURABLE keeps working: it names the behaviour the service actually has,
// so it is honoured rather than promised. Omitting the field is the same thing.
func TestRepository_CreateExplicitDurableAccepted(t *testing.T) {
	cfg, zot, ops := newMockCfg(), &mockZot{}, newMemOps()
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.CreateRepository(aliceCtx(), registry.CreateRepositorySpec{
		RegistryID: regID, Repository: "scratch/durable", Lifecycle: domain.LifecycleDurable,
	})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	require.Equal(t, registryv1.RepositoryLifecycle_DURABLE, opResponseRepository(t, done.Response).GetLifecycle())
}

// Refusing the input must not touch reading. A row stored before the refusal (an
// un-migrated deployment, or a write by a pod of the previous version) still parses
// and is still reported for what it says it is.
func TestRepository_StoredEphemeralOverlayStillReads(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["legacy/ephemeral"] = &domain.RepositoryConfig{
		RegistryID: regID, Name: "legacy/ephemeral",
		Visibility: domain.VisibilityPrivate, Lifecycle: domain.LifecycleEphemeral,
	}
	uc := ucWithRegistry(cfg, &mockZot{}, ops, domain.VisibilityPrivate)

	repo, err := uc.GetRepository(aliceCtx(), regID, "legacy/ephemeral")
	require.NoError(t, err, "a stored row must stay readable after the input is refused")
	require.Equal(t, domain.LifecycleEphemeral, repo.Lifecycle)
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
