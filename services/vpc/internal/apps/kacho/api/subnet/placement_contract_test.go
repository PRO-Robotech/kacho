// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// placementFixture — Create-use-case c seed-network и geo-mock'ами.
func placementFixture(t *testing.T) (*CreateSubnetUseCase, string) {
	t.Helper()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetwork(t, kr, "f1", netID)
	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or)
	return uc, netID
}

// TestSubnet_VPC_1_23_DeriveZonal — F6: placementType° server-derived из непустого
// zoneId (клиент НЕ шлёт placementType). response.placementType° == ZONAL, regionId "".
func TestSubnet_VPC_1_23_DeriveZonal(t *testing.T) {
	uc, netID := placementFixture(t)
	op, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("app-tier-a"),
		ZoneID:       testZone,
		V4CidrBlocks: []string{"10.20.0.0/24"},
		// placementType НЕ задан — сервер выводит.
	})
	require.NoError(t, err)
	require.True(t, op.Done)
	require.Nil(t, op.Error)
	var got vpcv1.Subnet
	require.NoError(t, op.Response.UnmarshalTo(&got))
	assert.Equal(t, vpcv1.SubnetPlacementType_ZONAL, got.PlacementType, "derived ZONAL")
	assert.Equal(t, testZone, got.ZoneId)
	assert.Equal(t, "", got.RegionId)
}

// TestSubnet_VPC_1_24_DeriveRegional — F6: placementType° derived REGIONAL из regionId.
func TestSubnet_VPC_1_24_DeriveRegional(t *testing.T) {
	uc, netID := placementFixture(t)
	op, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("anycast"),
		RegionID:     testRegion,
		V4CidrBlocks: []string{"10.99.0.0/24"},
	})
	require.NoError(t, err)
	require.True(t, op.Done)
	require.Nil(t, op.Error)
	var got vpcv1.Subnet
	require.NoError(t, op.Response.UnmarshalTo(&got))
	assert.Equal(t, vpcv1.SubnetPlacementType_REGIONAL, got.PlacementType)
	assert.Equal(t, "", got.ZoneId)
	assert.Equal(t, testRegion, got.RegionId)
}

// TestSubnet_VPC_1_25_BothZoneRegion_Reject — оба заданы → sync InvalidArgument.
func TestSubnet_VPC_1_25_BothZoneRegion_Reject(t *testing.T) {
	uc, netID := placementFixture(t)
	_, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("both"),
		ZoneID: testZone, RegionID: testRegion,
		V4CidrBlocks: []string{"10.20.0.0/24"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "exactly one of zone_id, region_id must be set", status.Convert(err).Message())
}

// TestSubnet_VPC_1_26_NeitherZoneRegion_Reject — ни одного → sync InvalidArgument.
func TestSubnet_VPC_1_26_NeitherZoneRegion_Reject(t *testing.T) {
	uc, netID := placementFixture(t)
	_, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("neither"),
		V4CidrBlocks: []string{"10.20.0.0/24"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "exactly one of zone_id, region_id must be set", status.Convert(err).Message())
}

// TestSubnet_VPC_1_27_PlacementTypeInBody_Reject — placementType в теле → explicit
// reject (не silent), даже если значение «совпало бы» с derived.
func TestSubnet_VPC_1_27_PlacementTypeInBody_Reject(t *testing.T) {
	uc, netID := placementFixture(t)
	_, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("with-pt"),
		PlacementType: domain.PlacementZonal, // клиент явно задал — reject
		ZoneID:        testZone,
		V4CidrBlocks:  []string{"10.20.0.0/24"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "placement_type is server-derived; set zone_id or region_id instead", status.Convert(err).Message())
}

// TestSubnet_VPC_1_28_ImmutablePlacement — Update mask с placement-полями → immutable.
func TestSubnet_VPC_1_28_ImmutablePlacement(t *testing.T) {
	uc := NewUpdateSubnetUseCase(kachomock.NewRepository(), repomock.NewOpsRepo())
	sid := ids.NewID(ids.PrefixSubnet)
	cases := []struct {
		field   string
		wantMsg string
	}{
		{"zone_id", "zone_id is immutable after Subnet.Create"},
		{"region_id", "region_id is immutable after Subnet.Create"},
		{"network_id", "network_id is immutable after Subnet.Create"},
		{"placement_type", "placement_type is server-derived; set zone_id or region_id instead"},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			_, err := uc.Execute(context.Background(), UpdateInput{
				SubnetID:   sid,
				UpdateMask: []string{tc.field},
			})
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.Equal(t, tc.wantMsg, status.Convert(err).Message())
		})
	}
}
