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

// NLB-1-16 (F3, MIGRATE): cross_zone_enabled is REGIONAL-only. Setting it true on a
// ZONAL placement is rejected synchronously with the verbatim contract message.
func TestLoadBalancer_NLB_1_16_CrossZoneEnabled_ZonalGuard(t *testing.T) {
	t.Parallel()

	t.Run("Create ZONAL + crossZoneEnabled=true → InvalidArgument", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{subnet: &fakeSubnetClient{placement: "ZONAL"}})
		req := baseCreateReq()
		req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_ZONAL
		req.V4Source = vipSubnet(lbTestSubnetZonal)
		req.CrossZoneEnabled = true
		_, err := uc.Execute(context.Background(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Equal(t, "crossZoneEnabled is not applicable to ZONAL placement", status.Convert(err).Message())
	})

	t.Run("Create REGIONAL + crossZoneEnabled=true → accepted + echoed", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{})
		req := baseCreateReq()
		req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
		req.V4Source = vipSubnet(lbTestSubnetRegional)
		req.CrossZoneEnabled = true
		op, err := uc.Execute(context.Background(), req)
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		require.True(t, lbByName(t, repo, "lb-1").CrossZoneEnabled)
	})
}

// NLB-1-16 (F3, MIGRATE): cross_zone_enabled is LIVE-mutable; Update true on a ZONAL
// LB is rejected before the async worker (placement_type immutable).
func TestLoadBalancer_NLB_1_16_CrossZoneEnabled_Update_ZonalGuard(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	// Force the seeded LB to INTERNAL_ZONAL so the ZONAL-guard applies.
	rec := repo.lbs[lbID]
	rec.Type = domain.LBTypeInternal
	rec.PlacementType = domain.PlacementZonal
	rec.Placement = domain.PlacementInternalZonal
	uc := NewUpdateLoadBalancerUseCase(repo, newFakeOpsRepo(), nil, nil)
	_, err := uc.Execute(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"cross_zone_enabled"}},
		CrossZoneEnabled:      true,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "crossZoneEnabled is not applicable to ZONAL placement", status.Convert(err).Message())
}
