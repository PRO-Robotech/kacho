// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

// Gateway — точка исхода трафика подсетей проекта наружу.
//
// Семантически-нагруженные поля (Name/Description/Labels) — newtypes из
// `domain/types.go` со встроенным Validate(). `CreatedAt` сюда НЕ входит —
// DB-managed, живет в `GatewayRecord` (см. `internal/repo/kacho/entity_gateway.go`).
//
// `GatewayType` — выбранная ветвь oneof (`NAT` либо `EGRESS_ONLY`), обязательна на
// Create и неизменяема. `SubnetID` — привязка шлюза И его якорь размещения: своей
// зоны/региона шлюз НЕ несёт, он наследует размещение подсети — так же, как
// сетевой интерфейс и адрес. Отсюда следует когерентность ссылки из статического
// маршрута: маршрут вправе назвать шлюз только из таблицы той же сети и только
// при совпадении зоны (региональная anycast-подсеть зоны не несёт и из зональной
// сверки исключена by construction).
type Gateway struct {
	ID          string
	ProjectID   string
	Name        RcNameVPC
	Description RcDescription
	Labels      RcLabels
	GatewayType GatewayType
	SubnetID    string
}

// Validate проверяет name/description/labels по domain-контракту. Вызывается
// use-case-слоем ПЕРЕД repo.Insert / repo.Update.
//
// Замечание: Gateway.Name держится здесь как `RcNameVPC` (permissive) — единый
// newtype-набор для всех ресурсов. Strict-name regex (`corevalidate.NameGateway`
// — lowercase, без uppercase/underscore) применяется дополнительно в service-слое
// после `g.Validate()` (см.
// internal/apps/kacho/api/gateway/update.go::validateGatewayUpdate).
func (g Gateway) Validate() error {
	return combineValidation(
		g.Name.Validate(),
		g.Description.Validate(),
		ValidateLabels(g.Labels),
	)
}

// Equal — deep equality по domain-полям. `CreatedAt` не входит.
func (g Gateway) Equal(other Gateway) bool {
	return g.ID == other.ID &&
		g.ProjectID == other.ProjectID &&
		g.Name == other.Name &&
		g.Description == other.Description &&
		LabelsEqual(g.Labels, other.Labels) &&
		g.GatewayType == other.GatewayType &&
		g.SubnetID == other.SubnetID
}
