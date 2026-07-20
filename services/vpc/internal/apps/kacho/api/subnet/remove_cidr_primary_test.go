// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

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

// seedSubnetWithBlocks — committed Subnet c заданными v4/v6-блоками (blocks[0] —
// immutable primary anchor ipv4CidrPrimary/ipv6CidrPrimary).
func seedSubnetWithBlocks(t *testing.T, kr *kachomock.Repository, projectID, subID string, v4, v6 []string) {
	t.Helper()
	kr.SeedSubnet(&kacho.SubnetRecord{
		Subnet: domain.Subnet{
			ID:            subID,
			ProjectID:     projectID,
			NetworkID:     ids.NewID(ids.PrefixNetwork),
			PlacementType: domain.PlacementZonal,
			ZoneID:        testZone,
			V4CidrBlocks:  v4,
			V6CidrBlocks:  v6,
		},
		CreatedAt: time.Now().UTC(),
	})
}

// TestSubnet_RemoveCidrBlocks_PrimaryV4_Rejected — Finding 5 (F7 immutable anchor):
// RemoveCidrBlocks НЕ должен удалять blocks[0] — редизайн-immutable ipv4CidrPrimary.
// Тихое промотирование следующего блока в primary недопустимо → INVALID_ARGUMENT
// (op-in-response), конвенционный immutable-тон. RED: текущий код молча удаляет
// primary и промотит 10.20.8.0/24.
func TestSubnet_RemoveCidrBlocks_PrimaryV4_Rejected(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	subID := ids.NewID(ids.PrefixSubnet)
	seedSubnetWithBlocks(t, kr, "f1", subID, []string{"10.20.0.0/24", "10.20.8.0/24"}, nil)

	remove := NewRemoveCidrBlocksUseCase(kr, or)
	rOp, err := remove.Execute(context.Background(), subID, []string{"10.20.0.0/24"}, nil)
	require.NoError(t, err)
	require.True(t, rOp.Done)
	require.NotNil(t, rOp.Error, "removing the primary CIDR anchor (blocks[0]) must be rejected")
	st := status.FromProto(rOp.Error)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "ipv4_cidr_primary is immutable after Subnet.Create", st.Message())
}

// TestSubnet_RemoveCidrBlocks_PrimaryV6_Rejected — то же для v6 primary anchor.
func TestSubnet_RemoveCidrBlocks_PrimaryV6_Rejected(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	subID := ids.NewID(ids.PrefixSubnet)
	seedSubnetWithBlocks(t, kr, "f1", subID, nil, []string{"fd00:20::/64", "fd00:21::/64"})

	remove := NewRemoveCidrBlocksUseCase(kr, or)
	rOp, err := remove.Execute(context.Background(), subID, nil, []string{"fd00:20::/64"})
	require.NoError(t, err)
	require.True(t, rOp.Done)
	require.NotNil(t, rOp.Error, "removing the v6 primary CIDR anchor (blocks[0]) must be rejected")
	st := status.FromProto(rOp.Error)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "ipv6_cidr_primary is immutable after Subnet.Create", st.Message())
}

// TestSubnet_RemoveCidrBlocks_AdditionalV4_OK — negative-control: удаление НЕ
// primary (additional blocks[1:]) остаётся успешным (guard не over-reject'ит).
func TestSubnet_RemoveCidrBlocks_AdditionalV4_OK(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	subID := ids.NewID(ids.PrefixSubnet)
	seedSubnetWithBlocks(t, kr, "f1", subID, []string{"10.20.0.0/24", "10.20.8.0/24"}, nil)

	remove := NewRemoveCidrBlocksUseCase(kr, or)
	rOp, err := remove.Execute(context.Background(), subID, []string{"10.20.8.0/24"}, nil)
	require.NoError(t, err)
	require.True(t, rOp.Done)
	require.Nil(t, rOp.Error, "removing a non-primary additional block is allowed")
	var got vpcv1.Subnet
	require.NoError(t, rOp.Response.UnmarshalTo(&got))
	assert.Equal(t, "10.20.0.0/24", got.Ipv4CidrPrimary, "primary anchor retained")
	assert.Empty(t, got.Ipv4CidrBlocks, "additional block removed")
}
