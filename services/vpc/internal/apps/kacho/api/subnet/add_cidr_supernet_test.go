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

// seedNetworkWithSupernet — committed Network c объявленным супернетом (F2/F7).
func seedNetworkWithSupernet(t *testing.T, kr *kachomock.Repository, projectID, networkID string, v4 []string) {
	t.Helper()
	ctx := context.Background()
	w, err := kr.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Networks().Insert(ctx, &domain.Network{
		ID:             networkID,
		ProjectID:      projectID,
		Name:           domain.RcNameVPC("net-supernet"),
		IPv4CidrBlocks: v4,
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
}

// createSubnetForTest — happy Create подсети c primary-anchor ⊆ супернета сети.
func createSubnetForTest(t *testing.T, kr *kachomock.Repository, or *repomock.OpsRepo, projectID, networkID, name, primary string) *vpcv1.Subnet {
	t.Helper()
	create := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or)
	cOp, err := create.Execute(context.Background(), domain.Subnet{
		ProjectID:    projectID,
		NetworkID:    networkID,
		Name:         domain.RcNameVPC(name),
		ZoneID:       testZone,
		V4CidrBlocks: []string{primary},
	})
	require.NoError(t, err)
	require.True(t, cOp.Done)
	require.Nil(t, cOp.Error)
	var got vpcv1.Subnet
	require.NoError(t, cOp.Response.UnmarshalTo(&got))
	return &got
}

// TestSubnet_VPC_1_34_AddCidrBlocks_OutsideSupernet_Rejected — Finding 4 (VPC-1-34 F7):
// Subnet.AddCidrBlocks обязан валидировать, что добавляемый диапазон лежит ВНУТРИ
// супернета родительской сети. Добавление блока вне супернета → INVALID_ARGUMENT
// (op-in-response), точный контракт-текст. RED: текущий код фетчит только сам
// сабнет (не сеть) → блок вне супернета проходит.
func TestSubnet_VPC_1_34_AddCidrBlocks_OutsideSupernet_Rejected(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetworkWithSupernet(t, kr, "f1", netID, []string{"10.20.0.0/16"})
	sub := createSubnetForTest(t, kr, or, "f1", netID, "s1", "10.20.0.0/24")

	add := NewAddCidrBlocksUseCase(kr, or)
	aOp, err := add.Execute(context.Background(), sub.Id, []string{"192.168.0.0/24"}, nil)
	require.NoError(t, err) // op-in-response: reject embedded в op.Error, не return-ошибка
	require.True(t, aOp.Done)
	require.NotNil(t, aOp.Error, "adding a block outside the network supernet must be rejected")
	st := status.FromProto(aOp.Error)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "subnet CIDR 192.168.0.0/24 is not within any network CIDR block", st.Message())
}

// TestSubnet_VPC_1_34_AddCidrBlocks_WithinSupernet_OK — VPC-1-34 happy: добавление
// диапазона ⊆ супернета (и не пересекающегося) остаётся успешным после фикса
// (guard не должен over-reject легитимный блок).
func TestSubnet_VPC_1_34_AddCidrBlocks_WithinSupernet_OK(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetworkWithSupernet(t, kr, "f1", netID, []string{"10.20.0.0/16"})
	sub := createSubnetForTest(t, kr, or, "f1", netID, "s1", "10.20.0.0/24")

	add := NewAddCidrBlocksUseCase(kr, or)
	aOp, err := add.Execute(context.Background(), sub.Id, []string{"10.20.8.0/24"}, nil)
	require.NoError(t, err)
	require.True(t, aOp.Done)
	require.Nil(t, aOp.Error, "block within supernet, non-overlapping → accepted")
	var got vpcv1.Subnet
	require.NoError(t, aOp.Response.UnmarshalTo(&got))
	assert.Equal(t, []string{"10.20.8.0/24"}, got.Ipv4CidrBlocks, "additional range added")
	assert.Equal(t, "10.20.0.0/24", got.Ipv4CidrPrimary, "primary anchor unchanged")
}
