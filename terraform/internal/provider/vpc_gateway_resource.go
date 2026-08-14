// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"google.golang.org/protobuf/proto"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// Шлюз — точка исхода трафика подсети наружу.
//
// Здесь стояло, что у шлюза нет собственных настроек, а вид один и подставляется
// конструктором запроса. Это перестало быть правдой: видов ДВА (публичная трансляция
// исходящего IPv4 и «только исход» для IPv6), и вид меняет поведение — он определяет,
// какое семейство назначения вправе маршрутизироваться через этот шлюз. Подставлять его
// за пользователя значило бы молча выбрать за него, что шлюз делает.
//
// Поэтому у ресурса два собственных аргумента, оба ОБЯЗАТЕЛЬНЫ и оба неизменяемы:
//   - `kind` — вид шлюза (`nat` | `egress_only`);
//   - `subnet_id` — привязка и якорь размещения; своей зоны шлюз не несёт, он наследует
//     размещение подсети.
//
// Смена любого из них — замена ресурса, а не правка: край объявляет их неизменяемыми
// после создания, поэтому план обязан показывать пересоздание, а не отказ на применении.
var vpcGatewaySpec = flatSpec{
	tfName: "kacho_vpc_gateway", human: "Шлюз", pathCol: "/vpc/v1/gateways",
	idField: "gatewayId", prefix: ids.PrefixGateway, scopeParam: "projectId", scopeAttr: "project_id",
	descr: "Шлюз — выход трафика подсети наружу.\n\n" +
		"Вид шлюза (`kind`) выбирается при создании и неизменяем: `nat` — публичная " +
		"трансляция исходящего IPv4, `egress_only` — «только исход» для IPv6 (входящие " +
		"соединения не устанавливаются). Подсеть привязки (`subnet_id`) обязана нести " +
		"CIDR-блок того семейства, которое обслуживает выбранный вид.",
	deleteHint:    "Удалению мешают таблицы маршрутизации, ссылающиеся на шлюз следующим узлом.",
	newCreate:     func() proto.Message { return &vpcv1.CreateGatewayRequest{} },
	newUpdate:     func() proto.Message { return &vpcv1.UpdateGatewayRequest{} },
	updateIDField: "gateway_id",
	kindAttr:      "kind",
	kindArms: map[string]func() (string, proto.Message){
		"nat": func() (string, proto.Message) {
			return "nat_gateway_spec", &vpcv1.NatGatewaySpec{}
		},
		"egress_only": func() (string, proto.Message) {
			return "egress_only_gateway_spec", &vpcv1.EgressOnlyGatewaySpec{}
		},
	},
	fields: withFields(commonFields("project_id", "Проект-владелец."),
		fieldSpec{name: "kind", kind: fString, required: true, immutable: true,
			doc: "Вид шлюза: `nat` — публичная трансляция исходящего IPv4; " +
				"`egress_only` — «только исход» для IPv6, входящие соединения не " +
				"устанавливаются. Выбирается при создании, смена — пересоздание."},
		fieldSpec{name: "subnet_id", kind: fString, required: true, immutable: true,
			doc: "Подсеть привязки. Она же якорь размещения: своей зоны шлюз не несёт. " +
				"Обязана нести CIDR-блок того семейства, которое обслуживает выбранный вид."},
	),
}
