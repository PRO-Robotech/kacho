// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

// Builder inline-собираемой системной RouteTable сети (VPC-1 F3). Симметричен
// security_group_builders.go: держит имя/описание системного ресурса в domain,
// а не magic-литералами в service-слое.

// DefaultRTName возвращает имя системной default-RouteTable сети по формуле
// `default-rt-<first 8 chars of network id>` (симметрия с DefaultSGName).
func DefaultRTName(networkID string) string {
	return "default-rt-" + TruncateID(networkID)
}

// DefaultRTDescription — описание автосоздаваемой default-RouteTable.
const DefaultRTDescription = "Default route table (auto-created by kacho-vpc)"

// NewDefaultRouteTable собирает domain.RouteTable для системной default-RT сети.
// Чистый value-builder: `id` минтит use-case-слой (ids.NewID(PrefixRouteTable)) —
// domain не тянет infra-утилиту. `CreatedAt` сюда не входит (DB-managed).
//
// StaticRoutes пуст: системная RT существует как явный, стабильный якорь
// «дефолтная RT сети» (Network.defaultRouteTableId°, auto-assoc новым подсетям),
// маршруты в неё добавляет тенант.
func NewDefaultRouteTable(id string, net Network) RouteTable {
	return RouteTable{
		ID:          id,
		ProjectID:   net.ProjectID,
		NetworkID:   net.ID,
		Name:        RcNameVPC(DefaultRTName(net.ID)),
		Description: RcDescription(DefaultRTDescription),
	}
}
