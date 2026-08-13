// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// v4Blocks генерирует n различных canonical /24-блоков начиная со смещения off:
// 10.<(off+i)/256>.<(off+i)%256>.0/24.
func v4Blocks(off, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		v := off + i
		out = append(out, fmt.Sprintf("10.%d.%d.0/24", v/256, v%256))
	}
	return out
}

// Declared-супернет — tenant-управляемый набор, который парсится на КАЖДОМ
// Subnet.Create/AddCidrBlocks и целиком сериализуется в каждом Network.Get/List.
// Без потолка он растёт неограниченно (AddCidrBlocks аддитивен и идемпотентен →
// размер накапливается между вызовами). Потолок — domain.MaxNetworkCidrBlocks.

// Create с числом блоков выше потолка → sync InvalidArgument ДО Operation.
func TestNetwork_SupernetCap_Create_TooManyBlocks(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	create := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or)

	_, err := create.Execute(context.Background(), domain.Network{
		ProjectID:      "prj-b3n7k1x9q2m5t8",
		Name:           domain.RcNameVPC("too-wide"),
		IPv4CidrBlocks: v4Blocks(0, domain.MaxNetworkCidrBlocks+1),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, fmt.Sprintf("too many CIDR blocks (max %d)", domain.MaxNetworkCidrBlocks), st.Message())
}

// Create ровно на потолке — проходит (граница включительна, BVA).
func TestNetwork_SupernetCap_Create_AtCapAccepted(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	create := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or)

	op, err := create.Execute(context.Background(), domain.Network{
		ProjectID:      "prj-b3n7k1x9q2m5t8",
		Name:           domain.RcNameVPC("at-cap"),
		IPv4CidrBlocks: v4Blocks(0, domain.MaxNetworkCidrBlocks),
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)
}

// AddCidrBlocks: одиночный запрос ниже потолка, но НАКОПЛЕННЫЙ (post-merge)
// размер выше — обязан быть отвергнут. Это и есть настоящая дыра: pre-merge
// проверка входа сама по себе такой вызов пропускает.
func TestNetwork_SupernetCap_Add_PostMergeOverflowRejected(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	half := domain.MaxNetworkCidrBlocks/2 + 1
	netID := seedNetwork(t, kr, or, v4Blocks(0, half))

	add := NewAddCidrBlocksUseCase(kr, or)
	op, err := add.Execute(context.Background(), netID, v4Blocks(1000, half), nil)
	// op-in-response: reject приходит embedded в Operation.error.
	require.NoError(t, err)
	require.True(t, op.Done)
	require.NotNil(t, op.Error, "накопленный супернет выше потолка → error в op")
	st := status.FromProto(op.Error)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, fmt.Sprintf("too many CIDR blocks (max %d)", domain.MaxNetworkCidrBlocks), st.Message())

	// Состояние сети не изменилось — отказ атомарен (writer-TX abort).
	get := NewGetNetworkUseCase(kr)
	got, gerr := get.Execute(context.Background(), netID)
	require.NoError(t, gerr)
	assert.Len(t, got.IPv4CidrBlocks, half, "отвергнутый merge не должен частично примениться")
}

// AddCidrBlocks ровно на потолке — идемпотентный re-add тех же блоков остаётся
// разрешённым (дедуп не увеличивает размер), потолок не ломает идемпотентность.
func TestNetwork_SupernetCap_Add_IdempotentReAddAtCap(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	blocks := v4Blocks(0, domain.MaxNetworkCidrBlocks)
	netID := seedNetwork(t, kr, or, blocks)

	add := NewAddCidrBlocksUseCase(kr, or)
	op, err := add.Execute(context.Background(), netID, blocks, nil)
	require.NoError(t, err)
	require.True(t, op.Done)
	require.Nil(t, op.Error, "re-add уже объявленных блоков идемпотентен, не overflow")
	var n vpcv1.Network
	require.NoError(t, op.Response.UnmarshalTo(&n))
	assert.Len(t, n.Ipv4CidrBlocks, domain.MaxNetworkCidrBlocks)
}

// Одиночный AddCidrBlocks-запрос выше потолка → sync InvalidArgument ДО
// Operation (bounded parse-cost на request-path).
func TestNetwork_SupernetCap_Add_InputOverCapSyncRejected(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := seedNetwork(t, kr, or, []string{"10.20.0.0/16"})

	add := NewAddCidrBlocksUseCase(kr, or)
	_, err := add.Execute(context.Background(), netID, v4Blocks(0, domain.MaxNetworkCidrBlocks+1), nil)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, fmt.Sprintf("too many CIDR blocks (max %d)", domain.MaxNetworkCidrBlocks), st.Message())
}

// Сужение (RemoveCidrBlocks) никогда не блокируется потолком — даже если вход
// формально длиннее потолка, он лишь уменьшает набор.
func TestNetwork_SupernetCap_RemoveNotBlocked(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	blocks := v4Blocks(0, domain.MaxNetworkCidrBlocks)
	netID := seedNetwork(t, kr, or, blocks)

	remove := NewRemoveCidrBlocksUseCase(kr, or)
	op, err := remove.Execute(context.Background(), netID, blocks[:10], nil)
	require.NoError(t, err)
	require.True(t, op.Done)
	require.Nil(t, op.Error)
	var n vpcv1.Network
	require.NoError(t, op.Response.UnmarshalTo(&n))
	assert.Len(t, n.Ipv4CidrBlocks, domain.MaxNetworkCidrBlocks-10)
}
