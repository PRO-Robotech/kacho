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

// seedRouteTable — committed RouteTable сети (ссылка на неё обязана резолвиться:
// подсеть принимает только таблицу СВОЕЙ сети).
func seedRouteTable(t *testing.T, kr *kachomock.Repository, projectID, networkID, rtID string) {
	t.Helper()
	ctx := context.Background()
	w, err := kr.Writer(ctx)
	require.NoError(t, err)
	_, err = w.RouteTables().Insert(ctx, &domain.RouteTable{
		ID: rtID, ProjectID: projectID, NetworkID: networkID,
		Name: domain.RcNameVPC("rt-" + rtID[len(rtID)-6:]),
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
}

// seedNetworkWithDefaultRT — committed Network, несущая явный
// defaultRouteTableId° (то, что провижнит Network.Create).
func seedNetworkWithDefaultRT(t *testing.T, kr *kachomock.Repository, projectID, networkID, rtID string) {
	t.Helper()
	ctx := context.Background()
	w, err := kr.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Networks().Insert(ctx, &domain.Network{
		ID: networkID, ProjectID: projectID, Name: domain.RcNameVPC("net-default-rt"),
		DefaultRouteTableID: rtID,
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
}

// VPC-1-37 / F8: Subnet.Create без явного routeTableId ассоциируется с
// `network.defaultRouteTableId°` — ЯВНЫМ дефолтом сети, а не «самой ранней RT»
// (легаси-триггер subnet_auto_pick_rt). До фикса поле сети никто не заполнял,
// поэтому подсеть уходила в БД с NULL и RT ей назначал недетерминированный
// триггер — ровно то поведение, которое редизайн объявил замещённым.
func TestSubnet_VPC_1_37_AutoAssociatesNetworkDefaultRouteTable(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	rtID := ids.NewID(ids.PrefixRouteTable)
	seedNetworkWithDefaultRT(t, kr, "f1", netID, rtID)

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or)
	op, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("s-auto-rt"),
		ZoneID: testZone, V4CidrBlocks: []string{"10.20.1.0/24"},
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)
	var s vpcv1.Subnet
	require.NoError(t, op.Response.UnmarshalTo(&s))
	assert.Equal(t, rtID, s.RouteTableId, "routeTableId должен унаследоваться от network.defaultRouteTableId°")
}

// Явный routeTableId тенанта не перетирается дефолтом сети.
func TestSubnet_VPC_1_37_ExplicitRouteTableWins(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	defaultRT := ids.NewID(ids.PrefixRouteTable)
	explicitRT := ids.NewID(ids.PrefixRouteTable)
	seedNetworkWithDefaultRT(t, kr, "f1", netID, defaultRT)
	// Явная таблица обязана существовать и лежать в той же сети — иначе это не
	// «явный выбор тенанта», а висячая ссылка.
	seedRouteTable(t, kr, "f1", netID, explicitRT)

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or)
	op, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("s-explicit-rt"),
		ZoneID: testZone, V4CidrBlocks: []string{"10.20.2.0/24"}, RouteTableID: explicitRT,
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)
	var s vpcv1.Subnet
	require.NoError(t, op.Response.UnmarshalTo(&s))
	assert.Equal(t, explicitRT, s.RouteTableId)
}

// Legacy-сеть без defaultRouteTableId° — подсеть создаётся без RT (пустое поле
// остаётся легальным состоянием, back-compat не ломается).
func TestSubnet_VPC_1_37_LegacyNetworkWithoutDefaultRT(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetwork(t, kr, "f1", netID)

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or)
	op, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("s-legacy-rt"),
		ZoneID: testZone, V4CidrBlocks: []string{"10.20.3.0/24"},
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)
	var s vpcv1.Subnet
	require.NoError(t, op.Response.UnmarshalTo(&s))
	assert.Empty(t, s.RouteTableId)
}
