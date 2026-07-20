// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// REG-1-11 (F4) — regionId обязателен на Create: омитнут → sync INVALID_ARGUMENT
// "regionId is required" первым стейтментом; geo НЕ вызывается, Insert НЕ вызывается.
func TestRegistry_REG_1_11_Create_RegionRequired(t *testing.T) {
	repo := &mockRepo{}
	geo := &mockGeo{}
	uc := newUCWithGeo(repo, &mockZot{}, &mockIAM{}, geo, newMemOps())

	_, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-P", Name: "payments"})
	require.Equal(t, codes.InvalidArgument, codeOf(t, err))
	require.Contains(t, status.Convert(err).Message(), "regionId is required")
	require.False(t, geo.called, "geo НЕ вызывается при отсутствующем regionId (own-field first)")
	require.Nil(t, repo.insertReg, "operation не создаётся, реестр не вставлен")
}

// REG-1-12 (F4, peer-validate lane) — несуществующий regionId → geo miss →
// FAILED_PRECONDITION (PEER_RESOURCE_MISSING; НЕ NOT_FOUND — чужой ресурс); Insert НЕ вызван.
func TestRegistry_REG_1_12_Create_RegionMissing_FailedPrecondition(t *testing.T) {
	repo := &mockRepo{}
	geo := &mockGeo{regionFn: func(context.Context, string) error { return regerrors.ErrFailedPrecondition }}
	uc := newUCWithGeo(repo, &mockZot{}, &mockIAM{}, geo, newMemOps())

	_, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-P", Name: "payments", RegionID: "eu-west-9"})
	require.Equal(t, codes.FailedPrecondition, codeOf(t, err))
	require.Contains(t, status.Convert(err).Message(), "eu-west-9")
	require.True(t, geo.called, "geo RegionService.Get вызван на request-path")
	require.Equal(t, "eu-west-9", geo.lastArg)
	require.Nil(t, repo.insertReg, "registry НЕ создаётся с висячим regionId")
}

// REG-1-13 (F4, edge) — geo недоступен на Create → UNAVAILABLE (PEER_UNAVAILABLE,
// fail-closed для мутации); registry НЕ создаётся.
func TestRegistry_REG_1_13_Create_GeoUnavailable_FailClosed(t *testing.T) {
	repo := &mockRepo{}
	geo := &mockGeo{regionFn: func(context.Context, string) error { return regerrors.ErrUnavailable }}
	uc := newUCWithGeo(repo, &mockZot{}, &mockIAM{}, geo, newMemOps())

	_, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-P", Name: "payments", RegionID: "eu-north-1"})
	require.Equal(t, codes.Unavailable, codeOf(t, err))
	require.Nil(t, repo.insertReg, "fail-closed: реестр не вставлен при недоступном geo")
}

// REG-1-14 (F4) — regionId / placementType immutable после Create: в update_mask →
// sync INVALID_ARGUMENT с каноничным immutable-текстом (до применения).
func TestRegistry_REG_1_14_Update_RegionPlacementImmutable(t *testing.T) {
	cases := map[string]struct {
		field string
		want  string
	}{
		"region_id_snake":      {"region_id", "regionId is immutable after Registry.Create"},
		"regionId_camel":       {"regionId", "regionId is immutable after Registry.Create"},
		"placement_type_snake": {"placement_type", "placementType is immutable after Registry.Create"},
		"placementType_camel":  {"placementType", "placementType is immutable after Registry.Create"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &mockRepo{}
			uc := newUC(repo, &mockZot{}, &mockIAM{}, newMemOps())
			_, err := uc.Update(aliceCtx(), registry.UpdateSpec{RegistryID: validRegID, Mask: []string{tc.field}})
			require.Equal(t, codes.InvalidArgument, codeOf(t, err))
			require.Equal(t, tc.want, status.Convert(err).Message())
			require.Nil(t, repo.updateSpec.Mask, "immutable reject — до writer.Update")
		})
	}
}
