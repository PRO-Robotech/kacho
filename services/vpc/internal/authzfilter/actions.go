// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

// Сквозного пропуска фильтра видимости здесь БОЛЬШЕ НЕТ, и это не упрощение.
//
// Раньше существовал sentinel «доверенный system-вызов», при котором страница
// отдавалась нефильтрованной. Производил его pbconv по ОДНОМУ признаку —
// `Principal.Type == "system"`, — то есть по значению, которое (а) вызывающий
// пишет себе сам заголовком и (б) платформа использует как ЯРЛЫК АНОНИМНОСТИ
// (`{system, anonymous}` край ставит запросу без удостоверения). Ни один законный
// путь его не производил: служебные вызовы несут `user:system.<сервис>-<роль>`, а
// межсервисные передают личность инициатора. Значит ветка была достижима
// практически только подделкой — и открывала ровно то, что фильтр закрывает.
//
// Теперь субъект либо назван (`type:id` → спрашиваем модель), либо не назван
// (пустая строка → пустой результат). Третьего значения нет, поэтому и пропускать
// нечего.

// FGA object types VPC-домена (передаются в AuthorizeService.ListObjects как
// resource_type). Должны совпадать с closed-table objectTypes в kacho-iam
// (например "vpc.subnet" → "vpc_subnet").
const (
	ResourceTypeNetwork          = "vpc_network"
	ResourceTypeSubnet           = "vpc_subnet"
	ResourceTypeSecurityGroup    = "vpc_security_group"
	ResourceTypeRouteTable       = "vpc_route_table"
	ResourceTypeAddress          = "vpc_address"
	ResourceTypeGateway          = "vpc_gateway"
	ResourceTypeNetworkInterface = "vpc_network_interface"
)

// Action-строки VPC-домена. На стороне kacho-iam последний `.`-сегмент (verb)
// резолвится в FGA relation: `list` → `viewer` — та же tier-relation, что
// энфорсит per-RPC Check для чтения (read==enforce). Формат —
// `<domain>.<resource>.<verb>` из IAM permission catalog.
const (
	ActionNetworkList          = "vpc.networks.list"
	ActionSubnetList           = "vpc.subnets.list"
	ActionSecurityGroupList    = "vpc.securityGroups.list"
	ActionRouteTableList       = "vpc.routeTables.list"
	ActionAddressList          = "vpc.addresses.list"
	ActionGatewayList          = "vpc.gateways.list"
	ActionNetworkInterfaceList = "vpc.networkInterfaces.list"
)
