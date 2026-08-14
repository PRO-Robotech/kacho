// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto

import (
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// gateway — receiver-объект под трансфер kacho.GatewayRecord → *vpcv1.Gateway.
// Parity с network.go.
type gateway struct{}

// toPb формирует *vpcv1.Gateway из repo-entity. CreatedAt — truncate до секунд
// через inline вызов time-трансфера.
//
// Проекция — только НАМЕРЕНИЕ и РЕЗУЛЬТАТ: идентификатор, проект, имя, описание,
// метки, привязка (`subnet_id`) и выбранный вид. Ничего о том, как исход разложен
// по железу, здесь нет и появиться не может: у ресурса нет ни одного такого поля
// ни в контракте, ни в столбцах.
//
// Ветвь oneof выводится ИЗ ЗАПИСИ, а не выставляется безусловно. Прежде она
// ставилась всегда одной и той же — тогда это было верно (вид был один), теперь
// стало бы ложью. Неизвестное значение вида оставляет ветвь НЕЗАПОЛНЕННОЙ, и это
// намеренно: набор закрыт CHECK'ом базы (миграция 0030) и проверкой use-case, а
// выдать чужому виду ветвь «наугад» значило бы соврать о том, что шлюз делает.
func (gateway) toPb(rec kacho.GatewayRecord) (*vpcv1.Gateway, error) {
	ts, err := (timeObj{}).toPb(rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	out := &vpcv1.Gateway{
		Id:          rec.ID,
		ProjectId:   rec.ProjectID,
		CreatedAt:   ts,
		Name:        string(rec.Name),
		Description: string(rec.Description),
		Labels:      domain.LabelsToMap(rec.Labels),
		SubnetId:    rec.SubnetID,
	}
	switch rec.GatewayType {
	case domain.GatewayTypeNat:
		// Адрес едет ИДЕНТИФИКАТОРОМ, а не значением: сам IP живёт у ресурса
		// адреса, и зеркало здесь было бы вторым местом об одном предмете,
		// которое расходится молча. Пустым он у этой ветви не бывает — биусловие
		// базы (0038) не даёт записать шлюз трансляции без адреса.
		out.Gateway = &vpcv1.Gateway_NatGateway{
			NatGateway: &vpcv1.NatGateway{AddressId: rec.ExternalAddressID},
		}
	case domain.GatewayTypeEgressOnly:
		out.Gateway = &vpcv1.Gateway_EgressOnlyGateway{EgressOnlyGateway: &vpcv1.EgressOnlyGateway{}}
	}
	return out, nil
}

func init() {
	dto.RegTransfer(dto.Fn2Face(gateway{}.toPb))
}
