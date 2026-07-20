// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package registry_test — F4/F3 behavioural locks: regionId (required + peer-validate
// geo), placementType (always-REGIONAL const), globalSlug (derived echo / opt-in /
// collision), immutability. Трассируются к REG-1-08..18. Use-case через mock-порты.
package registry_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// REG-1-15 — regionId обязателен на Create → омитнут → синхронный INVALID_ARGUMENT
// "regionId is required"; namespace НЕ создаётся, geo НЕ вызывается.
func TestNamespace_REG_1_15_RegionIdRequired(t *testing.T) {
	repo := &mockRepo{}
	geo := &mockGeo{}
	uc := newUCWithGeo(repo, &mockZot{}, &mockIAM{}, geo, newMemOps())
	_, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-P", Name: "payments"})
	require.Equal(t, codes.InvalidArgument, codeOf(t, err))
	require.Equal(t, "regionId is required", status.Convert(err).Message())
	require.Nil(t, repo.insertReg, "namespace НЕ создаётся без regionId")
	require.False(t, geo.called, "geo НЕ вызывается — regionId reject раньше")
}

// REG-1-16 — явный несуществующий regionId → peer-validate geo reject → InvalidArgument
// (текст несёт regionId); namespace НЕ создаётся.
func TestNamespace_REG_1_16_RegionNotFound(t *testing.T) {
	repo := &mockRepo{}
	geo := &mockGeo{regionFn: func(context.Context, string) error { return regerrors.ErrInvalidArg }}
	uc := newUCWithGeo(repo, &mockZot{}, &mockIAM{}, geo, newMemOps())
	_, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-P", Name: "payments", RegionID: "eu-west-9"})
	require.Equal(t, codes.InvalidArgument, codeOf(t, err))
	require.Contains(t, status.Convert(err).Message(), "eu-west-9")
	require.True(t, geo.called, "geo peer-validate вызван на request-path")
	require.Nil(t, repo.insertReg)
}

// REG-1-17 — geo недоступен на Create → UNAVAILABLE fail-closed; namespace НЕ создаётся.
func TestNamespace_REG_1_17_GeoUnavailable_FailClosed(t *testing.T) {
	repo := &mockRepo{}
	geo := &mockGeo{regionFn: func(context.Context, string) error { return regerrors.ErrUnavailable }}
	uc := newUCWithGeo(repo, &mockZot{}, &mockIAM{}, geo, newMemOps())
	_, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-P", Name: "payments", RegionID: "eu-north-1"})
	require.Equal(t, codes.Unavailable, codeOf(t, err))
	require.Nil(t, repo.insertReg, "мутация fail-closed — namespace НЕ создаётся при недоступном geo")
}

// REG-1-14/08 — placementType always-REGIONAL const на проекции + globalSlug derived
// echo при омитнутом входе. NB: default-derive временно projectId-based (accountSlug
// addendum отложен) — locka projectId-форму "<projectId>-<name>".
func TestNamespace_REG_1_14_08_PlacementRegional_GlobalSlugDerived(t *testing.T) {
	repo := &mockRepo{}
	ops := newMemOps()
	uc := newUC(repo, &mockZot{}, &mockIAM{}, ops)
	op, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-7h3n", Name: "payments", RegionID: "eu-north-1"})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)

	var ns registryv1.Namespace
	require.NoError(t, done.Response.UnmarshalTo(&ns))
	require.Equal(t, registryv1.PlacementType_REGIONAL, ns.GetPlacementType(), "always-REGIONAL («not a choice»)")
	require.Equal(t, "eu-north-1", ns.GetRegionId())
	// globalSlug derived echo — клиент scope-строку руками не собирает.
	require.Equal(t, "prj-7h3n-payments", ns.GetGlobalSlug(), "default derive <projectId>-<name> (accountSlug отложен)")
	require.True(t, strings.HasSuffix(ns.GetGlobalSlug(), "-payments"))
}

// REG-1-09 — явный bare-global globalSlug (свободен) → принят/echoed как есть.
func TestNamespace_REG_1_09_OptInGlobalSlug(t *testing.T) {
	repo := &mockRepo{}
	ops := newMemOps()
	uc := newUC(repo, &mockZot{}, &mockIAM{}, ops)
	op, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-P", Name: "payments", RegionID: "eu-north-1", GlobalSlug: "team-payments"})
	require.NoError(t, err)
	done := awaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)

	var ns registryv1.Namespace
	require.NoError(t, done.Response.UnmarshalTo(&ns))
	require.Equal(t, "team-payments", ns.GetGlobalSlug())
}

// REG-1-10 — bare-global globalSlug занят → ALREADY_EXISTS + tenant-prefix hint (тон-
// контракт эмитится на каждом Create-пути; DB partial UNIQUE(global_slug) — арбитр).
func TestNamespace_REG_1_10_BareGlobalCollision_Hint(t *testing.T) {
	repo := &mockRepo{insertFn: func(context.Context, *domain.Namespace, domain.RegisterIntent) (*domain.Namespace, error) {
		return nil, regerrors.ErrAlreadyExists
	}}
	uc := newUC(repo, &mockZot{}, &mockIAM{}, newMemOps())
	_, err := uc.Create(aliceCtx(), registry.CreateSpec{ProjectID: "prj-P", Name: "payments", RegionID: "eu-north-1", GlobalSlug: "payments"})
	require.Equal(t, codes.AlreadyExists, codeOf(t, err))
	msg := status.Convert(err).Message()
	require.Contains(t, msg, "explicit globalSlug 'payments'")
	require.Contains(t, msg, "already taken")
}

// REG-1-18/13 — regionId/placementType/globalSlug immutable через Update: каноничный
// immutable-текст (reject в mask-discipline, не generic unknown-field).
func TestNamespace_REG_1_18_13_ImmutableFields(t *testing.T) {
	cases := []struct {
		name string
		mask []string
		msg  string
	}{
		{"region_id", []string{"region_id"}, "regionId is immutable after Namespace.Create"},
		{"placement_type", []string{"placement_type"}, "placementType is immutable after Namespace.Create"},
		{"global_slug", []string{"global_slug"}, "globalSlug is immutable after Namespace.Create"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := newUC(&mockRepo{}, &mockZot{}, &mockIAM{}, newMemOps())
			_, err := uc.Update(aliceCtx(), registry.UpdateSpec{NamespaceID: validRegID, Mask: tc.mask})
			require.Equal(t, codes.InvalidArgument, codeOf(t, err))
			require.Equal(t, tc.msg, status.Convert(err).Message())
		})
	}
}
