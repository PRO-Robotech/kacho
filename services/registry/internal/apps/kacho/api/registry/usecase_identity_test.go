// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// REG-1-04 (F1) — id immutable: в update_mask → sync INVALID_ARGUMENT
// "id is immutable after Registry.Create"; registry не изменён (writer.Update не вызван).
func TestRegistry_REG_1_04_IDImmutableInMask(t *testing.T) {
	repo := &mockRepo{}
	uc := newUC(repo, &mockZot{}, &mockIAM{}, newMemOps())

	_, err := uc.Update(aliceCtx(), registry.UpdateSpec{RegistryID: validRegID, Mask: []string{"id"}})
	require.Equal(t, codes.InvalidArgument, codeOf(t, err))
	require.Equal(t, "id is immutable after Registry.Create", status.Convert(err).Message())
	require.Nil(t, repo.updateSpec.Mask, "immutable-reject до writer.Update — registry не тронут")
}

// REG-1-07 (F2, ключевой) — rename name НЕ меняет id/endpoint (URL по immutable id):
// Update(mask=[name], name=billing) → response.name=billing, но id и endpoint стабильны.
func TestRegistry_REG_1_07_RenameNameKeepsIDAndEndpoint(t *testing.T) {
	repo := &mockRepo{updateFn: func(_ context.Context, spec registry.UpdateSpec, _ func(*domain.Registry) domain.RegisterIntent) (*domain.Registry, error) {
		// writer возвращает переименованную строку: НОВОЕ имя, ТОТ ЖЕ id (стабильный якорь).
		return &domain.Registry{
			ID: spec.RegistryID, Name: "billing", ProjectID: "prj-P",
			RegionID: "eu-north-1", PlacementType: domain.PlacementTypeRegional,
			Status: domain.RegistryStatusActive,
		}, nil
	}}
	ops := newMemOps()
	uc := newUC(repo, &mockZot{}, &mockIAM{}, ops)

	op, err := uc.Update(aliceCtx(), registry.UpdateSpec{RegistryID: validRegID, Mask: []string{"name"}, Name: "billing"})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)

	var reg registryv1.Registry
	require.NoError(t, done.Response.UnmarshalTo(&reg))
	require.Equal(t, "billing", reg.GetName(), "name mutable — сменилось")
	require.Equal(t, validRegID, reg.GetId(), "id НЕ изменился (стабильный якорь)")
	require.Equal(t, "registry.kacho.local/"+validRegID, reg.GetEndpoint(),
		"endpoint derived по id — не по name → pull-URL стабилен (REG-1-07)")
}

// REG-1-28 (F8) — пустой update_mask → full-object PATCH mutable-полей; immutable
// (id/regionId/placementType) структурно вне UpdateSpec → silently игнорируются.
func TestRegistry_REG_1_28_EmptyMaskFullPatchIgnoresImmutable(t *testing.T) {
	repo := &mockRepo{}
	ops := newMemOps()
	uc := newUC(repo, &mockZot{}, &mockIAM{}, ops)

	op, err := uc.Update(aliceCtx(), registry.UpdateSpec{
		RegistryID: validRegID, Name: "billing", Description: "new",
		Labels: map[string]string{"team": "pay"},
	})
	require.NoError(t, err)
	awaitOpDone(t, ops, op.ID)
	require.True(t, repo.updateSpec.ApplyName, "empty-mask применяет name (задан)")
	require.True(t, repo.updateSpec.ApplyDescription, "empty-mask применяет description")
	require.True(t, repo.updateSpec.ApplyLabels, "empty-mask применяет labels")
	// immutable-поля не выразимы в UpdateSpec → silently ignored by construction (нет ошибки).
}

// REG-1-30 (F8) — INTERNAL никогда не эхает pgx/SQL-текст: некатегоризированная
// DB-ошибка на write-пути → фикс. "internal database error"; NotContains driver/creds.
func TestRegistry_REG_1_30_InternalNoLeak(t *testing.T) {
	const secret = "pq: password=hunter2 host=db.internal user=registry dbname=kacho_registry"
	repo := &mockRepo{insertFn: func(context.Context, *domain.Registry, domain.RegisterIntent) (*domain.Registry, error) {
		return nil, errors.New(secret)
	}}
	uc := newUC(repo, &mockZot{}, &mockIAM{}, newMemOps())

	_, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-P", Name: "payments", RegionID: "eu-north-1"})
	st := status.Convert(err)
	require.Equal(t, codes.Internal, st.Code())
	require.Equal(t, "internal database error", st.Message(), "фикс. opaque-текст (behaviour-level lock)")
	require.NotContains(t, st.Message(), "password")
	require.NotContains(t, st.Message(), "hunter2")
	require.NotContains(t, st.Message(), "db.internal")
}
