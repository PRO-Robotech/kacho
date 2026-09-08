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
// resource_type). Должны совпадать с closed-table objectTypes в kaname
// (например "vpc.subnet" → "vpc_subnet").
const (
	ResourceTypeNetwork          = "vpc_network"
	ResourceTypeSubnet           = "vpc_subnet"
	ResourceTypeSecurityGroup    = "vpc_security_group"
	ResourceTypeRouteTable       = "vpc_route_table"
	ResourceTypeAddress          = "vpc_address"
	ResourceTypeGateway          = "vpc_gateway"
	ResourceTypeNetworkInterface = "vpc_network_interface"
	ResourceTypeCidrGroup        = "vpc_cidr_group"
)

// Action-строки VPC-домена. Формат `<domain>.<resource>.<verb>` из IAM permission
// catalog; action едет на каждом check'е для аудита/трассировки.
//
// РЕШЕНИЕ принимается по явному `required_relation`, который фильтр пинит на батч
// (`v_get` — см. filter.go visibilityRelations), а НЕ по server-side деривации
// verb→relation. `v_get` — то же отношение, которым per-RPC Check гейтит Get
// (`internal/check/permission_map.go`), поэтому предикат страницы равен
// отношению чтения (read==enforce).
//
// Прежняя редакция утверждала, что verb-деривация iam (`list` → `viewer`) даёт «ту
// же tier-relation, что энфорсит per-RPC Check для чтения». Деривация описана верно,
// а вывод из неё — нет: чтение гейтится `v_get`, а ярусные (`viewer`/`editor`/
// `admin`) и глагольные (`v_*`) отношения в модели РАЗВЯЗАНЫ. Значит опора на
// деривацию (снятие override) расхождение бы закрепила.
const (
	ActionNetworkList          = "vpc.networks.list"
	ActionSubnetList           = "vpc.subnets.list"
	ActionSecurityGroupList    = "vpc.securityGroups.list"
	ActionRouteTableList       = "vpc.routeTables.list"
	ActionAddressList          = "vpc.addresses.list"
	ActionGatewayList          = "vpc.gateways.list"
	ActionNetworkInterfaceList = "vpc.networkInterfaces.list"
	ActionCidrGroupList        = "vpc.cidrGroups.list"
)
