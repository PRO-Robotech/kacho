// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto

import (
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// routeTable — receiver-объект под трансфер kacho.RouteTableRecord →
// *vpcv1.RouteTable.
type routeTable struct{}

// toPb формирует *vpcv1.RouteTable из repo-entity. CreatedAt — truncate до
// секунд через time-трансфер.
func (routeTable) toPb(rec kachorepo.RouteTableRecord) (*vpcv1.RouteTable, error) {
	ts, err := (timeObj{}).toPb(rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	p := &vpcv1.RouteTable{
		Id:          rec.ID,
		ProjectId:   rec.ProjectID,
		CreatedAt:   ts,
		Name:        string(rec.Name),
		Description: string(rec.Description),
		Labels:      domain.LabelsToMap(rec.Labels),
		NetworkId:   rec.NetworkID,
	}
	for _, sr := range rec.StaticRoutes {
		psr := &vpcv1.StaticRoute{Labels: sr.Labels}
		if sr.DestinationPrefix != "" {
			psr.Destination = &vpcv1.StaticRoute_DestinationPrefix{DestinationPrefix: sr.DestinationPrefix}
		}
		// `next_hop` — oneof из ДВУХ ветвей, и обе обязаны доезжать до ответа.
		// Ветвь шлюза приземлена вместе с якорем размещения шлюза: запись её
		// хранит (`static_routes` JSONB + `route_table_gateway_refs`) и по ней
		// же ЭНФОРСИТ — шлюз, названный живым маршрутом, не удаляется. Пока
		// маппер знал только адрес, арендатор получал на Create 200, а на
		// чтении — маршрут без следующего узла: сервер сам нарушал свой oneof,
		// и причина неудаляемости шлюза в ответе не показывалась. Взаимоисключение
		// обеспечено валидацией на записи (`validateStaticRoutes`), поэтому здесь
		// ветви разбираются в порядке «адрес, иначе шлюз» — двух непустых значений
		// в строке быть не может.
		switch {
		case sr.NextHopAddress != "":
			psr.NextHop = &vpcv1.StaticRoute_NextHopAddress{NextHopAddress: sr.NextHopAddress}
		case sr.GatewayID != "":
			psr.NextHop = &vpcv1.StaticRoute_GatewayId{GatewayId: sr.GatewayID}
		}
		p.StaticRoutes = append(p.StaticRoutes, psr)
	}
	return p, nil
}

func init() {
	dto.RegTransfer(dto.Fn2Face(routeTable{}.toPb))
}
