// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"google.golang.org/protobuf/proto"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// Шлюз общего исхода — ресурс БЕЗ собственных настроек.
//
// Его спецификация в контракте пуста: `SharedEgressGatewaySpec` не несёт ни одного поля.
// Это не заготовка под будущее, а форма ресурса: шлюз здесь — сам факт наличия исхода у
// проекта, а не набор ручек. Поэтому он ложится на общий каркас плоских ресурсов целиком,
// а спецификация подставляется конструктором запроса.
//
// Вынести её в конфигурацию значило бы просить у пользователя пустой блок — форму без
// содержания, которую он всё равно обязан написать, чтобы ничего не задать.
var vpcGatewaySpec = flatSpec{
	tfName: "kacho_vpc_gateway", human: "Шлюз", pathCol: "/vpc/v1/gateways",
	idField: "gatewayId", prefix: ids.PrefixGateway, scopeParam: "projectId", scopeAttr: "project_id",
	descr: "Шлюз общего исхода — выход трафика проекта наружу.\n\n" +
		"Собственных настроек у него нет: контракт объявляет спецификацию пустой. Шлюз " +
		"здесь — сам факт наличия исхода, а не набор ручек.",
	deleteHint: "Удалению мешают таблицы маршрутизации, ссылающиеся на шлюз следующим узлом.",
	newCreate: func() proto.Message {
		// Вид шлюза — ВАРИАНТ oneof, а не поле: у контракта их может стать больше одного.
		// Пустая спецификация здесь — не заглушка, а выбор вида: без неё край не знает,
		// шлюз чего заводят.
		return &vpcv1.CreateGatewayRequest{
			Gateway: &vpcv1.CreateGatewayRequest_SharedEgressGatewaySpec{
				SharedEgressGatewaySpec: &vpcv1.SharedEgressGatewaySpec{},
			},
		}
	},
	newUpdate:     func() proto.Message { return &vpcv1.UpdateGatewayRequest{} },
	updateIDField: "gateway_id",
	fields:        commonFields("project_id", "Проект-владелец."),
}
