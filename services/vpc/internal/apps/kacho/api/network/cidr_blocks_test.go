// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

func seedNetwork(t *testing.T, kr *kachomock.Repository, or *repomock.OpsRepo, v4 []string) string {
	t.Helper()
	create := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false)
	op, err := create.Execute(context.Background(), domain.Network{
		ProjectID:      "prj-b3n7k1x9q2m5t8",
		Name:           domain.RcNameVPC("core-prod"),
		IPv4CidrBlocks: v4,
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)
	var n vpcv1.Network
	require.NoError(t, op.Response.UnmarshalTo(&n))
	return n.Id
}

// VPC-1-08: :add-cidr-blocks grows the declared supernet, :remove-cidr-blocks
// shrinks it — both op-in-response with the full updated Network in .response.
func TestNetwork_VPC_1_08_AddRemoveCidrBlocks(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := seedNetwork(t, kr, or, []string{"10.20.0.0/16"})

	add := NewAddCidrBlocksUseCase(kr, or)
	aOp, err := add.Execute(context.Background(), netID, []string{"10.30.0.0/16"}, nil)
	require.NoError(t, err)
	require.True(t, aOp.Done, "AddCidrBlocks is op-in-response")
	require.Nil(t, aOp.Error)
	var afterAdd vpcv1.Network
	require.NoError(t, aOp.Response.UnmarshalTo(&afterAdd))
	assert.Equal(t, []string{"10.20.0.0/16", "10.30.0.0/16"}, afterAdd.Ipv4CidrBlocks,
		"response carries both supernet blocks")

	remove := NewRemoveCidrBlocksUseCase(kr, or)
	rOp, err := remove.Execute(context.Background(), netID, []string{"10.30.0.0/16"}, nil)
	require.NoError(t, err)
	require.True(t, rOp.Done)
	require.Nil(t, rOp.Error)
	var afterRem vpcv1.Network
	require.NoError(t, rOp.Response.UnmarshalTo(&afterRem))
	assert.Equal(t, []string{"10.20.0.0/16"}, afterRem.Ipv4CidrBlocks, "removed block gone")
}

// VPC-1-10: removing the last supernet block that still contains a live subnet
// (its ipv4CidrPrimary ⊆ the block) → FAILED_PRECONDITION, embedded in the op.
func TestNetwork_VPC_1_10_RemoveBlockWithLiveSubnet(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := seedNetwork(t, kr, or, []string{"10.20.0.0/16"})

	// Live subnet carved from 10.20.0.0/16 (primary = blocks[0]).
	kr.SeedSubnet(&kacho.SubnetRecord{
		Subnet: domain.Subnet{
			ID:           ids.NewID(ids.PrefixSubnet),
			ProjectID:    "prj-b3n7k1x9q2m5t8",
			NetworkID:    netID,
			V4CidrBlocks: []string{"10.20.0.0/24"},
		},
		CreatedAt: time.Now().UTC(),
	})

	remove := NewRemoveCidrBlocksUseCase(kr, or)
	rOp, err := remove.Execute(context.Background(), netID, []string{"10.20.0.0/16"}, nil)
	require.NoError(t, err) // op-in-response: reject embedded in op.Error, not returned
	require.True(t, rOp.Done)
	require.NotNil(t, rOp.Error, "block still contains a subnet → error in op")
	st := status.FromProto(rOp.Error)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Equal(t, "network CIDR block 10.20.0.0/16 still contains subnets", st.Message())
}

// VPC-1-09-adjacent: malformed CIDR block on :add-cidr-blocks → sync
// INVALID_ARGUMENT before the Operation is created.
func TestNetwork_AddCidrBlocks_MalformedCIDR(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := seedNetwork(t, kr, or, []string{"10.20.0.0/16"})

	add := NewAddCidrBlocksUseCase(kr, or)
	_, err := add.Execute(context.Background(), netID, []string{"10.30.0.0/33"}, nil)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "10.30.0.0/33")
}
