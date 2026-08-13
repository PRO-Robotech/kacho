// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// TestStaticRouteNextHopIsExactlyOneBranch — ветвь следующего узла выбирается
// вызывающим, и требование «ровно одна» проверяется на СУММЕ ветвей, а не на
// одной из них.
//
// ПРЕДМЕТ. Здесь стоял тест, закреплявший ОТКАЗ по `gateway_id`: резолвить шлюз
// было нечем — у шлюза не было ни якоря размещения, ни вида, с которым можно
// сверить маршрут, поэтому из трёх законных исходов выбирался второй (явный
// отказ). Теперь у шлюза есть и то и другое, ветвь РЕАЛИЗОВАНА, и прежний тест
// закреплял бы снятое поведение. Он заменён, а не удалён: предмет остался тем же —
// вызывающий обязан получать отказ, называющий ЕГО поле.
//
// Что проверяется здесь (синхронный уровень, без БД):
//   - ни одной ветви — отказ, называющий обе возможности;
//   - обе ветви — отказ по `gateway_id` (взаимоисключение);
//   - мусор в `gateway_id` — терминальный отказ ФОРМАТА своего id, а не полоса
//     существования: «повтори позже» на вход, который валидным не станет никогда,
//     был бы ложью, а «Gateway  not found» — утверждением об отсутствии ресурса,
//     которого вызывающий не называл;
//   - положительные контроли на ОБЕ ветви — иначе отрицания зеленели бы на
//     обработчике, отвергающем любой маршрут.
//
// Существование шлюза и когерентность размещения здесь НЕ проверяются: их держит
// оператор записи, и утверждать о них можно только против настоящей БД
// (services/vpc/internal/repo/route_table_gateway_ref_integration_test.go).
func TestStaticRouteNextHopIsExactlyOneBranch(t *testing.T) {
	ctx := context.Background()
	gwID := ids.NewID(ids.PrefixGateway)

	route := func(mutate func(*vpcv1.StaticRoute)) []*vpcv1.StaticRoute {
		sr := &vpcv1.StaticRoute{
			Destination: &vpcv1.StaticRoute_DestinationPrefix{DestinationPrefix: "10.0.0.0/24"},
		}
		mutate(sr)
		return []*vpcv1.StaticRoute{sr}
	}
	viaAddress := route(func(sr *vpcv1.StaticRoute) {
		sr.NextHop = &vpcv1.StaticRoute_NextHopAddress{NextHopAddress: "10.0.0.1"}
	})
	viaGateway := route(func(sr *vpcv1.StaticRoute) {
		sr.NextHop = &vpcv1.StaticRoute_GatewayId{GatewayId: gwID}
	})
	viaGatewayGarbage := route(func(sr *vpcv1.StaticRoute) {
		sr.NextHop = &vpcv1.StaticRoute_GatewayId{GatewayId: "not-an-id"}
	})
	viaNothing := route(func(*vpcv1.StaticRoute) {})

	create := func(t *testing.T, routes []*vpcv1.StaticRoute) error {
		t.Helper()
		h, _, kr := minimalHandler(t, true)
		net := makeNetwork(t, kr)
		_, err := h.Create(ctx, &vpcv1.CreateRouteTableRequest{
			ProjectId:    "f1",
			NetworkId:    net.ID,
			Name:         "rt",
			StaticRoutes: routes,
		})
		return err
	}

	t.Run("положительный контроль: адрес проходит", func(t *testing.T) {
		require.NoError(t, create(t, viaAddress))
	})

	t.Run("положительный контроль: well-formed id шлюза проходит синхронный уровень", func(t *testing.T) {
		require.NoError(t, create(t, viaGateway),
			"синхронный уровень не резолвит шлюз — это делает оператор записи")
	})

	t.Run("ни одной ветви — отказ называет обе возможности", func(t *testing.T) {
		err := create(t, viaNothing)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, status.Convert(err).Message(),
			"static_routes[0]: next_hop_address or gateway_id is required")
	})

	t.Run("обе ветви — отказ по взаимоисключению", func(t *testing.T) {
		both := route(func(sr *vpcv1.StaticRoute) {
			sr.NextHop = &vpcv1.StaticRoute_GatewayId{GatewayId: gwID}
		})
		// Обе ветви одного oneof по проводу не выражаются, поэтому «оба» собирается
		// на доменном уровне — там, где взаимоисключение и проверяется. Иначе кейс
		// утверждал бы о состоянии, которого вход не может произвести.
		routes, err := staticRoutesFromProto(both)
		require.NoError(t, err)
		routes[0].NextHopAddress = "10.0.0.1"
		verr := validateStaticRoutes(routes)
		require.Error(t, verr)
		assert.Equal(t, codes.InvalidArgument, status.Code(verr))
		assert.Contains(t, status.Convert(verr).Message(),
			"static_routes[0]: next_hop_address and gateway_id are mutually exclusive")
	})

	t.Run("мусор в gateway_id — терминальный отказ формата", func(t *testing.T) {
		err := create(t, viaGatewayGarbage)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, status.Convert(err).Message(), "invalid gateway id 'not-an-id'")
	})

	t.Run("на пути Update действует то же правило", func(t *testing.T) {
		h, or, kr := minimalHandler(t, true)
		net := makeNetwork(t, kr)
		createOp, err := h.Create(ctx, &vpcv1.CreateRouteTableRequest{
			ProjectId: "f1", NetworkId: net.ID, Name: "rt", StaticRoutes: viaAddress,
		})
		require.NoError(t, err)
		repomock.AwaitOpDone(t, or, createOp.Id)
		listed, err := h.List(narrowtest.Caller(), &vpcv1.ListRouteTablesRequest{ProjectId: "f1"})
		require.NoError(t, err)
		require.Len(t, listed.RouteTables, 1)

		_, err = h.Update(ctx, &vpcv1.UpdateRouteTableRequest{
			RouteTableId: listed.RouteTables[0].Id,
			StaticRoutes: viaGatewayGarbage,
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, status.Convert(err).Message(), "invalid gateway id 'not-an-id'")
	})
}
