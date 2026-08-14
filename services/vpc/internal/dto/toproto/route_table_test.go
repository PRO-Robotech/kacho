// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"

	// blank-import регистрирует трансферы RouteTable + time.
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// TestDTO_TransferRouteTable_NextHopBranches — обе ветви `oneof next_hop`
// доезжают до ответа.
//
// Ветвь шлюза приземлена вместе с якорем размещения шлюза: запись её сохраняет
// (`static_routes` JSONB + `route_table_gateway_refs`), а платформа по ней
// ЭНФОРСИТ — шлюз, названный живым маршрутом, не удаляется. Чтение же её
// не возвращало: маппер `toPb` знал только `next_hop_address`. Наблюдаемо это
// было так — арендатор создаёт маршрут через шлюз, получает 200, читает
// таблицу и видит маршрут БЕЗ следующего узла, то есть контракт `oneof`
// нарушен ответом самого сервера; при этом шлюз перестаёт удаляться по
// причине, которой в ответе не видно. Это «принято-и-проигнорировано» в самой
// тихой форме: поле принято, применено и невидимо
// (`api-conventions.md` §«Принято-и-проигнорировано»).
//
// Утверждение парное: ветвь адреса — положительный контроль. Без него
// «gatewayId пуст» было бы неотличимо от «маппер вообще не заполняет
// next_hop», и проба зеленела бы на мапперe, потерявшем обе ветви.
func TestDTO_TransferRouteTable_NextHopBranches(t *testing.T) {
	at := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	rec := kachorepo.RouteTableRecord{
		RouteTable: domain.RouteTable{
			ID:        "rtb1",
			ProjectID: "prj1",
			Name:      domain.RcNameVPC("rt-with-both-branches"),
			NetworkID: "net1",
			StaticRoutes: []domain.StaticRoute{
				{DestinationPrefix: "0.0.0.0/0", GatewayID: "gtw1"},
				{DestinationPrefix: "10.0.0.0/8", NextHopAddress: "10.0.0.1"},
			},
		},
		CreatedAt: at,
	}

	var dst *vpcv1.RouteTable
	require.NoError(t, dto.Transfer(dto.FromTo(rec, &dst)))
	require.NotNil(t, dst)
	require.Len(t, dst.StaticRoutes, 2)

	gwRoute := dst.StaticRoutes[0]
	assert.Equal(t, "0.0.0.0/0", gwRoute.GetDestinationPrefix())
	assert.Equal(t, "gtw1", gwRoute.GetGatewayId(),
		"ветвь шлюза обязана доехать до ответа: запись её хранит и энфорсит")
	assert.Empty(t, gwRoute.GetNextHopAddress(), "ветвь ровно одна")

	addrRoute := dst.StaticRoutes[1]
	assert.Equal(t, "10.0.0.0/8", addrRoute.GetDestinationPrefix())
	assert.Equal(t, "10.0.0.1", addrRoute.GetNextHopAddress(),
		"положительный контроль: ветвь адреса доезжала и продолжает доезжать")
	assert.Empty(t, addrRoute.GetGatewayId(), "ветвь ровно одна")
}
