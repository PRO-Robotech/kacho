// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// VPC-1-11 / F3: Network.Create система провижнит default RouteTable и
// проставляет её id в `Network.defaultRouteTableId°`. До фикса колонка и
// публичное поле существовали, но НИ ОДИН прод-путь их не писал (writer'а
// SetDefaultRouteTableID не было) — тенант всегда получал "" и не мог отличить
// «RT ещё нет» от «поле не реализовано», а миграция 0015 утверждала обратное.
func TestNetwork_VPC_1_11_CreateProvisionsDefaultRouteTable(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	create := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, true)

	op, err := create.Execute(context.Background(), domain.Network{
		ProjectID:      "prj-b3n7k1x9q2m5t8",
		Name:           domain.RcNameVPC("core-prod"),
		IPv4CidrBlocks: []string{"10.20.0.0/16"},
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)
	var n vpcv1.Network
	require.NoError(t, op.Response.UnmarshalTo(&n))

	require.NotEmpty(t, n.DefaultRouteTableId, "Create обязан вернуть непустой defaultRouteTableId°")
	assert.True(t, strings.HasPrefix(n.DefaultRouteTableId, ids.PrefixRouteTable),
		"default RT адресуется id с rtb-префиксом, получено %q", n.DefaultRouteTableId)
	assert.NotEmpty(t, n.DefaultSecurityGroupId, "default-SG остаётся на месте (F4)")

	// Get эхает то же значение (durable, а не только в op-response).
	get := NewGetNetworkUseCase(kr)
	got, gerr := get.Execute(context.Background(), n.Id)
	require.NoError(t, gerr)
	assert.Equal(t, n.DefaultRouteTableId, got.DefaultRouteTableID)

	// Провижн атомарен с Insert(Network): RouteTable лежит в том же writer-TX,
	// то есть видна сразу после единственного Commit.
	rd, rerr := kr.Reader(context.Background())
	require.NoError(t, rerr)
	defer func() { _ = rd.Close() }()
	rt, rtErr := rd.RouteTables().Get(context.Background(), n.DefaultRouteTableId)
	require.NoError(t, rtErr, "default RT обязана существовать как ресурс, а не быть висячим id")
	assert.Equal(t, n.Id, rt.NetworkID)
	assert.Equal(t, "prj-b3n7k1x9q2m5t8", rt.ProjectID)
}

// F3: default RT — часть того же FGA register-intent, что Network и default-SG.
// Без owner-tuple gateway scope_extractor не резолвит vpc_route_table→project и
// тенант получает 403 на собственной системной RT.
func TestNetwork_VPC_1_11_DefaultRouteTableRegisteredInFGA(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	create := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, true)

	op, err := create.Execute(context.Background(), domain.Network{
		ProjectID: "prj-b3n7k1x9q2m5t8",
		Name:      domain.RcNameVPC("core-fga"),
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)
	var n vpcv1.Network
	require.NoError(t, op.Response.UnmarshalTo(&n))
	require.NotEmpty(t, n.DefaultRouteTableId)

	want := "vpc_route_table:" + n.DefaultRouteTableId
	var found bool
	for _, ev := range kr.FGARegisterEvents() {
		if ev.Tuple.Object == want {
			found = true
		}
	}
	assert.True(t, found, "owner-tuple intent на системную RT обязан лежать в том же writer-TX")
}
