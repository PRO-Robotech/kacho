// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package type2pb

// RED-фаза (ban #12) под задачу продукта #1473 — площадка ZONAL-балансировщика
// выходит НАРУЖУ.
//
// Строка её несла бы и без этой пробы, но невидимой отовсюду: столбец, который
// вставка заполняет, а проекция чтения не выбирает, не появляется ни в ответе,
// ни в объекте в памяти. Поэтому цепочка проверяется на обоих концах —
// у use-case (запись) и здесь (чтение).
//
// Отрицания — не косметика: непустая зона у REGIONAL/EXTERNAL была бы утечкой
// размещения, а не лишним полем.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

func lbRecWithPlacement(pt domain.PlacementType, typ domain.LBType, zone domain.ZoneID) kachorepo.LoadBalancerRecord {
	return kachorepo.LoadBalancerRecord{
		LoadBalancer: domain.LoadBalancer{
			ID: "nlb01ABCDEF1234567xx", ProjectID: "prj01ABCDEF1234567ll",
			RegionID: "ru-central1", Name: "demo-nlb",
			Type: typ, PlacementType: pt, ZoneID: zone,
			Status: domain.LBStatusActive, SessionAffinity: domain.SessionAffinity5Tuple,
		},
	}
}

func toPbLB(t *testing.T, rec kachorepo.LoadBalancerRecord) *lbv1.NetworkLoadBalancer {
	t.Helper()
	var pb *lbv1.NetworkLoadBalancer
	require.NoError(t, dto.Transfer(dto.FromTo(rec, &pb)))
	require.NotNil(t, pb)
	return pb
}

// TestNetworkLoadBalancer_ZonalCarriesItsZone — ZONAL называет площадку.
func TestNetworkLoadBalancer_ZonalCarriesItsZone(t *testing.T) {
	pb := toPbLB(t, lbRecWithPlacement(domain.PlacementZonal, domain.LBTypeInternal, "ru-central1-a"))
	assert.Equal(t, lbv1.NetworkLoadBalancer_ZONAL, pb.GetPlacementType())
	assert.Equal(t, "ru-central1-a", pb.GetZoneId(),
		"вид размещения без самой площадки не отвечает на вопрос, где стоит балансировщик")
}

// TestNetworkLoadBalancer_RegionalCarriesNoZone — REGIONAL/anycast зоны не имеет.
func TestNetworkLoadBalancer_RegionalCarriesNoZone(t *testing.T) {
	pb := toPbLB(t, lbRecWithPlacement(domain.PlacementRegional, domain.LBTypeInternal, ""))
	assert.Equal(t, lbv1.NetworkLoadBalancer_REGIONAL, pb.GetPlacementType())
	assert.Empty(t, pb.GetZoneId())
}

// TestNetworkLoadBalancer_ExternalCarriesNoZone — у внешнего зона подлежащего
// адреса деривится платформой и наружу не выходит.
func TestNetworkLoadBalancer_ExternalCarriesNoZone(t *testing.T) {
	pb := toPbLB(t, lbRecWithPlacement(domain.PlacementUnspecified, domain.LBTypeExternal, ""))
	assert.Empty(t, pb.GetZoneId())
}
