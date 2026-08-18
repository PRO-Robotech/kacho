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

	// ExternalAddressID — внешний адрес, через который шлюз транслирует.
	//
	// Непуст РОВНО у вида `NAT` и пуст у `EGRESS_ONLY`; оба направления держит
	// база (`gateways_nat_has_address_chk` / `gateways_external_address_kind_chk`,
	// миграция 0038), поэтому «шлюз трансляции без адреса» — состояние
	// незаписываемое, а не то, которого код старается не создавать. У вида
	// «только исход» публичного адреса нет by design: отсутствие входящей
	// достижимости и есть смысл этого вида.
	//
	// Адрес выделяется на Create из пула зоны ЯКОРЯ (подсети) и возвращается в
	// пул на Delete. Обратную сторону привязки — ту, которую видит владелец
	// адреса, — несёт строка `address_references` с `referrer_type` =
	// `GatewayReferrerType`, поэтому `Address.used_by` называет шлюз.
	ExternalAddressID string
}

// GatewayReferrerType — `ReferrerType` в `address_references` для адреса,
// привязанного к шлюзу. Тот же словарь, что у интерфейса
// (`network_interface`) и у балансировщика; имя ресурса — то же, каким шлюз
// зовётся в модели прав (`vpc_gateway`), чтобы у одного предмета не завелось
// двух написаний.
const GatewayReferrerType = "vpc_gateway"

// Validate проверяет name/description/labels по domain-контракту. Вызывается
// use-case-слоем ПЕРЕД repo.Insert / repo.Update.
//
// Gateway.Name — тот же `RcNameVPC`, что и у остальных ресурсов, и это больше
// не «единый newtype при разных формах»: форма у всех ОДНА
// (`validate.NameForm`). Второй, более строгой проверки в service-слое у шлюза
// нет — она была следствием собственной, более широкой формы сервиса, снятой
// вместе с ней (#715).
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
		g.SubnetID == other.SubnetID &&
		g.ExternalAddressID == other.ExternalAddressID
}
