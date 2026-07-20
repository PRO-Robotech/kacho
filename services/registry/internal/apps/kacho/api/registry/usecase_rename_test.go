// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package registry_test — RenameNamespace (F2 :rename-verb) behavioural locks:
// happy re-name + default-slug re-derive, opt-in slug preservation, sync verb-guards
// (no-op/malformed/malformed-id), async collision. Трассируются к REG-1-06/07.
package registry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

func nsGetFn(ns *domain.Namespace) func(context.Context, string) (*domain.Namespace, error) {
	return func(context.Context, string) (*domain.Namespace, error) { return ns, nil }
}

// REG-1-06 — happy RenameNamespace: name сменён; id стабилен; default-derived
// globalSlug пересчитан (<projectId>-<newName> — accountSlug отложен).
func TestNamespace_REG_1_06_Rename_HappyPath(t *testing.T) {
	cur := &domain.Namespace{ID: validRegID, ProjectID: "prj-7h3n", Name: "payments",
		Status: domain.NamespaceStatusActive, RegionID: "eu-north-1", GlobalSlug: "prj-7h3n-payments"}
	repo := &mockRepo{getFn: nsGetFn(cur)}
	ops := newMemOps()
	uc := newUC(repo, &mockZot{}, &mockIAM{}, ops)

	op, err := uc.RenameNamespace(aliceCtx(), validRegID, "billing")
	require.NoError(t, err)

	var meta registryv1.RenameNamespaceMetadata
	require.NoError(t, op.Metadata.UnmarshalTo(&meta))
	require.Equal(t, validRegID, meta.GetNamespaceId())
	require.Equal(t, "billing", meta.GetNewName())

	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	require.True(t, repo.updateSpec.ApplyName)
	require.Equal(t, "billing", repo.updateSpec.Name)
	require.True(t, repo.updateSpec.ApplyGlobalSlug, "default-derived slug re-derived")
	require.Equal(t, "prj-7h3n-billing", repo.updateSpec.GlobalSlug)
}

// REG-1-06 — opt-in bare-global globalSlug (не равен default) НЕ пересчитывается.
func TestNamespace_REG_1_06_Rename_OptInSlug_NotRederived(t *testing.T) {
	cur := &domain.Namespace{ID: validRegID, ProjectID: "prj-7h3n", Name: "payments",
		Status: domain.NamespaceStatusActive, GlobalSlug: "team-payments"}
	repo := &mockRepo{getFn: nsGetFn(cur)}
	ops := newMemOps()
	uc := newUC(repo, &mockZot{}, &mockIAM{}, ops)

	op, err := uc.RenameNamespace(aliceCtx(), validRegID, "billing")
	require.NoError(t, err)
	awaitOpDone(t, ops, op.ID)
	require.True(t, repo.updateSpec.ApplyName)
	require.False(t, repo.updateSpec.ApplyGlobalSlug, "opt-in bare-global slug не трогается")
}

// REG-1-07 — verb-guards первым стейтментом (синхронно): no-op / malformed newName /
// malformed id → INVALID_ARGUMENT; Operation не создаётся.
func TestNamespace_REG_1_07_Rename_SyncGuards(t *testing.T) {
	cur := &domain.Namespace{ID: validRegID, ProjectID: "prj-7h3n", Name: "payments", Status: domain.NamespaceStatusActive}
	t.Run("no_op_same_name", func(t *testing.T) {
		uc := newUC(&mockRepo{getFn: nsGetFn(cur)}, &mockZot{}, &mockIAM{}, newMemOps())
		_, err := uc.RenameNamespace(aliceCtx(), validRegID, "payments")
		require.Equal(t, codes.InvalidArgument, codeOf(t, err))
	})
	t.Run("malformed_new_name", func(t *testing.T) {
		uc := newUC(&mockRepo{getFn: nsGetFn(cur)}, &mockZot{}, &mockIAM{}, newMemOps())
		_, err := uc.RenameNamespace(aliceCtx(), validRegID, "Bad Name!")
		require.Equal(t, codes.InvalidArgument, codeOf(t, err))
	})
	t.Run("malformed_id", func(t *testing.T) {
		uc := newUC(&mockRepo{}, &mockZot{}, &mockIAM{}, newMemOps())
		_, err := uc.RenameNamespace(aliceCtx(), "bad-id!!", "billing")
		require.Equal(t, codes.InvalidArgument, codeOf(t, err))
	})
}

// REG-1-07 — целевое имя занято → Operation{done:true} с result.error ALREADY_EXISTS
// (async — partial UNIQUE(project,name) арбитр в repo.Update), НЕ sync-ошибка.
func TestNamespace_REG_1_07_Rename_Collision_AlreadyExists(t *testing.T) {
	cur := &domain.Namespace{ID: validRegID, ProjectID: "prj-7h3n", Name: "payments",
		Status: domain.NamespaceStatusActive, GlobalSlug: "prj-7h3n-payments"}
	repo := &mockRepo{
		getFn: nsGetFn(cur),
		updateFn: func(context.Context, registry.UpdateSpec, func(*domain.Namespace) domain.RegisterIntent) (*domain.Namespace, error) {
			return nil, regerrors.ErrAlreadyExists
		},
	}
	ops := newMemOps()
	uc := newUC(repo, &mockZot{}, &mockIAM{}, ops)

	op, err := uc.RenameNamespace(aliceCtx(), validRegID, "billing")
	require.NoError(t, err, "collision — async Operation error, не sync reject")
	done := awaitOpDone(t, ops, op.ID)
	require.NotNil(t, done.Error)
	require.Equal(t, int32(codes.AlreadyExists), done.Error.GetCode())
}
