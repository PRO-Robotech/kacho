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

// NLB-1-06 / NLB-1-07 (F2): placement is the authoritative Create input; the derived
// (type°, placement_type°) read projections persist consistent with it. The placement°
// round-trip itself is verified in type2pb.
func TestLoadBalancer_NLB_1_06_07_PlacementDerivedPersisted(t *testing.T) {
	t.Parallel()

	t.Run("NLB-1-06 EXTERNAL → EXTERNAL_REGIONAL", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{addr: &fakeAddressClient{}})
		req := baseCreateReq()
		req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
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
		req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_ZONAL
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
		req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
		req.V4Source = vipSubnet(lbTestSubnetRegional)
		op, err := uc.Execute(context.Background(), req)
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		require.Equal(t, domain.PlacementInternalRegional, lbByName(t, repo, "lb-1").Placement)
	})
}

// NLB-1-06 (F2, MIGRATE): placement is the AUTHORITATIVE Create input. With NO
// legacy type/placement_type supplied, EXTERNAL_REGIONAL drives type°=EXTERNAL,
// placementType°=unspecified (anycast).
func TestLoadBalancer_NLB_1_06_PlacementAuthoritative_External(t *testing.T) {
	t.Parallel()
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := newCreateUC(repo, opsRepo, createDeps{addr: &fakeAddressClient{}})
	req := baseCreateReq()
	// No req.Type / req.PlacementType — placement alone drives derivation.
	req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
	req.V4Source = vipPublic()
	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
	rec := lbByName(t, repo, "lb-1")
	require.Equal(t, domain.PlacementExternalRegional, rec.Placement)
	require.Equal(t, domain.LBTypeExternal, rec.Type)
	require.Equal(t, domain.PlacementUnspecified, rec.PlacementType)
}

// NLB-1-07 (F2, MIGRATE): placement authoritative — INTERNAL_ZONAL / INTERNAL_REGIONAL
// drive type°=INTERNAL + placementType° WITHOUT any legacy input.
func TestLoadBalancer_NLB_1_07_PlacementAuthoritative_Internal(t *testing.T) {
	t.Parallel()

	t.Run("INTERNAL_ZONAL", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{subnet: &fakeSubnetClient{placement: "ZONAL"}})
		req := baseCreateReq()
		req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_ZONAL
		req.V4Source = vipSubnet(lbTestSubnetZonal)
		op, err := uc.Execute(context.Background(), req)
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		rec := lbByName(t, repo, "lb-1")
		require.Equal(t, domain.LBTypeInternal, rec.Type)
		require.Equal(t, domain.PlacementZonal, rec.PlacementType)
		require.Equal(t, domain.PlacementInternalZonal, rec.Placement)
	})

	t.Run("INTERNAL_REGIONAL", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{})
		req := baseCreateReq()
		req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
		req.V4Source = vipSubnet(lbTestSubnetRegional)
		op, err := uc.Execute(context.Background(), req)
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		rec := lbByName(t, repo, "lb-1")
		require.Equal(t, domain.LBTypeInternal, rec.Type)
		require.Equal(t, domain.PlacementRegional, rec.PlacementType)
		require.Equal(t, domain.PlacementInternalRegional, rec.Placement)
	})
}

// NLB-1-08 (F2, CONTRACT): type/placement_type are derived output-only — writing
// either in Create is an EXPLICIT reject (not silent-ignore). placement is the sole
// authoritative mode input; even a legacy value CONSISTENT with placement is rejected.
func TestLoadBalancer_NLB_1_08_LegacyModeInputRejected(t *testing.T) {
	t.Parallel()

	t.Run("type set rejected (even consistent with placement)", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{addr: &fakeAddressClient{}})
		req := baseCreateReq()
		req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
		req.Type = lbv1.NetworkLoadBalancer_EXTERNAL // consistent, but still forbidden input
		req.V4Source = vipPublic()
		_, err := uc.Execute(context.Background(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Contains(t, status.Convert(err).Message(), "placement")
	})

	t.Run("placement_type set rejected", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{subnet: &fakeSubnetClient{placement: "ZONAL"}})
		req := baseCreateReq()
		req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_ZONAL
		req.PlacementType = lbv1.NetworkLoadBalancer_ZONAL // forbidden input
		req.V4Source = vipSubnet(lbTestSubnetZonal)
		_, err := uc.Execute(context.Background(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Contains(t, status.Convert(err).Message(), "placement")
	})

	t.Run("placement unset rejected (required)", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{addr: &fakeAddressClient{}})
		req := baseCreateReq()
		req.V4Source = vipPublic()
		// no placement, no type
		_, err := uc.Execute(context.Background(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Contains(t, status.Convert(err).Message(), "placement")
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
