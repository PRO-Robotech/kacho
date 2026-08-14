// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

// Magic-numbers и enum-константы для domain-слоя (запрет inline-status и
// inline-magic-numbers — выносим в именованные константы).

// ShortIDLen — длина prefix-а ресурс-id, используемого при построении
// derived-имен (например default-sg-<8chars>).
const ShortIDLen = 8

// TruncateID возвращает первые ShortIDLen символов id (или весь id если он
// короче). Используется builder'ами имен вида "default-sg-<short>".
func TruncateID(id string) string {
	if len(id) > ShortIDLen {
		return id[:ShortIDLen]
	}
	return id
}

// SecurityGroupRuleDirection — направление SG-правила (INGRESS/EGRESS).
// Используется builder'ом NewDefaultSecurityGroupRules + sync-валидацией в
// service-слое (validateSGRule).
type SecurityGroupRuleDirection string

const (
	SecurityGroupRuleDirectionIngress SecurityGroupRuleDirection = "INGRESS"
	SecurityGroupRuleDirectionEgress  SecurityGroupRuleDirection = "EGRESS"
)

// ---- GatewayType -------------------------------------------------------------

// GatewayType — выбранная ветвь `Gateway.gateway` (oneof). Не голая строка, а
// enum-константа: значение выбирается на Create и НЕИЗМЕНЯЕМО (смена вида шлюза
// означает другой шлюз).
//
// Значения — те же, что хранит столбец `gateways.gateway_type`, и набор закреплён
// DB-CHECK'ом `gateways_type_chk` (миграция 0030). Расширяется в lockstep: новая
// ветвь oneof — новая константа здесь И новая миграция, меняющая CHECK.
type GatewayType string

const (
	// GatewayTypeNat — публичная трансляция исходящего IPv4
	// (`Gateway.nat_gateway`).
	GatewayTypeNat GatewayType = "NAT"

	// GatewayTypeEgressOnly — «только исход» для IPv6
	// (`Gateway.egress_only_gateway`): наружу можно, входящие соединения не
	// устанавливаются.
	GatewayTypeEgressOnly GatewayType = "EGRESS_ONLY"
)

// IPFamily — семейство адресов, которое обслуживает вид шлюза: 4 либо 6.
// Ноль означает «вид не назван» — вызывающий обязан отличать этот случай сам,
// потому что required-проверку ветви oneof делает use-case, а не эта функция.
//
// Функция — ЕДИНЫЙ источник соответствия «вид шлюза → семейство» для кода
// сервиса. Зеркало предиката живёт в SQL (миграция 0030 и оператор записи ссылок
// маршрутов); расхождение между ними ловит интеграционная проба
// TestGatewayFamilyRuleMatchesDBPredicate.
func (t GatewayType) IPFamily() int {
	switch t {
	case GatewayTypeNat:
		return 4
	case GatewayTypeEgressOnly:
		return 6
	default:
		return 0
	}
}

// ---- NetworkInterfaceStatus --------------------------------------------------

// NetworkInterfaceStatus — грубый статус NIC (зеркалит vpcv1.NetworkInterface_Status).
type NetworkInterfaceStatus int

// Значения NetworkInterfaceStatus. STATUS_UNSPECIFIED — для legacy rows (DB-layer
// возвращает его если status-колонка пустая или содержит неизвестное значение).
const (
	NIStatusUnspecified NetworkInterfaceStatus = iota
	NIStatusProvisioning
	NIStatusActive
	NIStatusAvailable
	NIStatusFailed
	NIStatusDeleting
)

// String-значения NetworkInterfaceStatus для DB-CHECK constraint и DB-маппинга
// (network_interfaces.status TEXT). Используется в маппинге
// internal/repo/helpers/nic.go (NIStatusName / NIStatusFromName), в DTO
// toproto/network_interface.go и в CHECK-constraint
// network_interfaces_status_check (0001_initial.sql).
const (
	NIStatusStrProvisioning = "PROVISIONING"
	NIStatusStrActive       = "ACTIVE"
	NIStatusStrAvailable    = "AVAILABLE"
	NIStatusStrFailed       = "FAILED"
	NIStatusStrDeleting     = "DELETING"
	NIStatusStrUnspecified  = "STATUS_UNSPECIFIED"
)
