// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// TestSubnet_VPC_1_40_OpInResponse — F9/F4: Subnet.Create — statusless op-in-response.
// В ТОМ ЖЕ ответе Operation.done == true; metadata → CreateSubnetMetadata{subnetId};
// result.response — полный public Subnet (id, networkId, placementType°, zoneId°,
// ipv4CidrPrimary, createdAt°). Follow-up GET не нужен.
func TestSubnet_VPC_1_40_OpInResponse(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetwork(t, kr, "f1", netID)

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or)

	op, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID:    "f1",
		NetworkID:    netID,
		Name:         domain.RcNameVPC("app-tier-a"),
		ZoneID:       testZone,
		V4CidrBlocks: []string{"10.20.0.0/24"},
	})
	require.NoError(t, err)
	require.True(t, op.Done, "Create must return an already-completed Operation (op-in-response)")
	require.Nil(t, op.Error)
	require.NotNil(t, op.Response)

	var meta vpcv1.CreateSubnetMetadata
	require.NoError(t, op.Metadata.UnmarshalTo(&meta))
	require.NotEmpty(t, meta.SubnetId)

	var got vpcv1.Subnet
	require.NoError(t, op.Response.UnmarshalTo(&got))
	assert.Equal(t, meta.SubnetId, got.Id)
	assert.Equal(t, netID, got.NetworkId)
	assert.Equal(t, vpcv1.SubnetPlacementType_ZONAL, got.PlacementType)
	assert.Equal(t, testZone, got.ZoneId)
}

// TestSubnet_OpInResponse_AddCidr — verb-pair AddCidrBlocks тоже op-in-response.
func TestSubnet_OpInResponse_AddCidr(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetwork(t, kr, "f1", netID)

	create := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or)
	add := NewAddCidrBlocksUseCase(kr, or)

	cOp, err := create.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("s1"),
		ZoneID:       testZone,
		V4CidrBlocks: []string{"10.20.0.0/24"},
	})
	require.NoError(t, err)
	require.True(t, cOp.Done)
	var created vpcv1.Subnet
	require.NoError(t, cOp.Response.UnmarshalTo(&created))

	aOp, err := add.Execute(context.Background(), created.Id, []string{"10.20.8.0/24"}, nil)
	require.NoError(t, err)
	require.True(t, aOp.Done, "AddCidrBlocks must return an already-completed Operation")
	require.Nil(t, aOp.Error)
	require.NotNil(t, aOp.Response)
}
