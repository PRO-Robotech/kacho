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
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// TestStaticRouteGatewayNextHopRejectedByName — ветка `gateway_id` следующего
// перехода отвергается ЯВНО и СВОИМ именем.
//
// ПРЕДМЕТ (api-conventions.md, «Принято-и-проигнорировано — ЗАПРЕЩЕНО»). Ветка
// `next_hop` разбиралась одним геттером — адресом; выбранный вызывающим
// `gateway_id` не читал никто, и маршрут доезжал до валидации с ПУСТЫМ
// следующим переходом. Отказ приходил, но назывался `next_hop_address`: клиент,
// приславший `gatewayId`, читал про поле, которого не посылал, и не мог понять,
// что именно не поддержано. Из трёх законных исходов выбран второй — явный
// синхронный отказ с именем поля.
//
// Пара обязательна: без положительного контроля (`next_hop_address` проходит
// проверку) отрицание зеленело бы и на обработчике, отвергающем любой маршрут.
func TestStaticRouteGatewayNextHopRejectedByName(t *testing.T) {
	ctx := context.Background()

	// Интерфейс ветки oneof не экспортируется, поэтому маршрут собирается целиком.
	viaGateway := func() []*vpcv1.StaticRoute {
		return []*vpcv1.StaticRoute{{
			Destination: &vpcv1.StaticRoute_DestinationPrefix{DestinationPrefix: "10.0.0.0/24"},
			NextHop:     &vpcv1.StaticRoute_GatewayId{GatewayId: "gw000000000000000000"},
		}}
	}
	viaAddress := func() []*vpcv1.StaticRoute {
		return []*vpcv1.StaticRoute{{
			Destination: &vpcv1.StaticRoute_DestinationPrefix{DestinationPrefix: "10.0.0.0/24"},
			NextHop:     &vpcv1.StaticRoute_NextHopAddress{NextHopAddress: "10.0.0.1"},
		}}
	}

	t.Run("Create: gateway_id отвергнут своим именем", func(t *testing.T) {
		h, _, kr := minimalHandler(t, true)
		net := makeNetwork(t, kr)

		_, err := h.Create(ctx, &vpcv1.CreateRouteTableRequest{
			ProjectId:    "f1",
			NetworkId:    net.ID,
			Name:         "rt",
			StaticRoutes: viaGateway(),
		})

		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, status.Convert(err).Message(), "static_routes[0].gateway_id",
			"отказ обязан называть ПРИСЛАННОЕ поле, а не соседнюю ветку oneof")
	})

	t.Run("Create: next_hop_address проходит (положительный контроль)", func(t *testing.T) {
		h, _, kr := minimalHandler(t, true)
		net := makeNetwork(t, kr)

		_, err := h.Create(ctx, &vpcv1.CreateRouteTableRequest{
			ProjectId:    "f1",
			NetworkId:    net.ID,
			Name:         "rt",
			StaticRoutes: viaAddress(),
		})

		require.NoError(t, err, "реализованная ветка next_hop обязана проходить")
	})

	t.Run("Update: gateway_id отвергнут своим именем", func(t *testing.T) {
		h, or, kr := minimalHandler(t, true)
		net := makeNetwork(t, kr)

		createOp, err := h.Create(ctx, &vpcv1.CreateRouteTableRequest{
			ProjectId:    "f1",
			NetworkId:    net.ID,
			Name:         "rt",
			StaticRoutes: viaAddress(),
		})
		require.NoError(t, err)
		repomock.AwaitOpDone(t, or, createOp.Id)
		listed, err := h.List(narrowtest.Caller(), &vpcv1.ListRouteTablesRequest{ProjectId: "f1"})
		require.NoError(t, err)
		require.Len(t, listed.RouteTables, 1)
		rtID := listed.RouteTables[0].Id

		_, err = h.Update(ctx, &vpcv1.UpdateRouteTableRequest{
			RouteTableId: rtID,
			StaticRoutes: viaGateway(),
		})

		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, status.Convert(err).Message(), "static_routes[0].gateway_id",
			"на пути Update действует то же правило, что на Create")
	})
}
