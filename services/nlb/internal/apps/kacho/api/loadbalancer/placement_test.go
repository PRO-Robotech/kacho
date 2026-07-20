// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// NLB-1-06 / NLB-1-07 (F2, EXPAND): merged placement is persisted derived-consistent
// with the legacy (type, placement_type). In EXPAND type/placement_type stay the
// authoritative inputs; the placement° derived read is verified in type2pb.
func TestLoadBalancer_NLB_1_06_07_PlacementDerivedPersisted(t *testing.T) {
	t.Parallel()

	t.Run("NLB-1-06 EXTERNAL → EXTERNAL_REGIONAL", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{addr: &fakeAddressClient{}})
		req := baseCreateReq()
		req.Type = lbv1.NetworkLoadBalancer_EXTERNAL
		req.V4Source = vipPublic()
		op, err := uc.Execute(context.Background(), req)
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		rec := lbByName(t, repo, "lb-1")
		require.Equal(t, domain.PlacementExternalRegional, rec.Placement)
		require.Equal(t, domain.LBTypeExternal, rec.Type)
	})

	t.Run("NLB-1-07 INTERNAL_ZONAL → INTERNAL_ZONAL", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{subnet: &fakeSubnetClient{placement: "ZONAL"}})
		req := baseCreateReq()
		req.Type = lbv1.NetworkLoadBalancer_INTERNAL
		req.PlacementType = lbv1.NetworkLoadBalancer_ZONAL
		req.V4Source = vipSubnet(lbTestSubnetZonal)
		op, err := uc.Execute(context.Background(), req)
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		rec := lbByName(t, repo, "lb-1")
		require.Equal(t, domain.PlacementInternalZonal, rec.Placement)
		require.Equal(t, domain.PlacementZonal, rec.PlacementType)
	})

	t.Run("INTERNAL_REGIONAL → INTERNAL_REGIONAL", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{})
		req := baseCreateReq()
		req.Type = lbv1.NetworkLoadBalancer_INTERNAL
		req.PlacementType = lbv1.NetworkLoadBalancer_REGIONAL
		req.V4Source = vipSubnet(lbTestSubnetRegional)
		op, err := uc.Execute(context.Background(), req)
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		require.Equal(t, domain.PlacementInternalRegional, lbByName(t, repo, "lb-1").Placement)
	})
}

// EXPAND: an explicit placement input consistent with type/placement_type is
// accepted; an inconsistent one is rejected (InvalidArgument). Full authority
// (placement drives, legacy inputs rejected) is NLB-1c/MIGRATE.
func TestLoadBalancer_Placement_InputConsistency(t *testing.T) {
	t.Parallel()

	t.Run("consistent input accepted", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{addr: &fakeAddressClient{}})
		req := baseCreateReq()
		req.Type = lbv1.NetworkLoadBalancer_EXTERNAL
		req.V4Source = vipPublic()
		req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
		op, err := uc.Execute(context.Background(), req)
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		require.Equal(t, domain.PlacementExternalRegional, lbByName(t, repo, "lb-1").Placement)
	})

	t.Run("inconsistent input rejected", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{addr: &fakeAddressClient{}})
		req := baseCreateReq()
		req.Type = lbv1.NetworkLoadBalancer_EXTERNAL
		req.V4Source = vipPublic()
		req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_ZONAL // contradicts EXTERNAL
		_, err := uc.Execute(context.Background(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Contains(t, status.Convert(err).Message(), "inconsistent")
	})
}

// NLB-1-10 (F2, EXPAND): placement is immutable — addressing it in update_mask is
// rejected before UpdateMask processing (immutable-switch first).
func TestLoadBalancer_NLB_1_10_PlacementImmutable(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	uc := NewUpdateLoadBalancerUseCase(repo, newFakeOpsRepo(), nil, nil)
	_, err := uc.Execute(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"placement"}},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "placement is immutable after NetworkLoadBalancer.Create", status.Convert(err).Message())
}
