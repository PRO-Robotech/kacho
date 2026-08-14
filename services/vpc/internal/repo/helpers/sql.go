// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package helpers

import (
	"net/netip"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// PrefixFamily — семейство адресов CIDR-префикса: 4, 6 либо 0, если строка
// префиксом не является.
//
// Ноль — ОТДЕЛЬНЫЙ исход, а не «по умолчанию IPv4»: он означает «семейство не
// установлено», и вызывающий обязан на него отказать. Молчаливый выбор семейства
// за вызывающего превратил бы сверку «вид шлюза ↔ семейство назначения» в
// тождественно истинную для мусорного ввода.
func PrefixFamily(prefix string) int {
	p, err := netip.ParsePrefix(prefix)
	if err != nil {
		return 0
	}
	if p.Addr().Is4() {
		return 4
	}
	return 6
}

// JoinAnd соединяет conds через " AND " для построения композитного WHERE.
// Empty slice → "".
func JoinAnd(conds []string) string {
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}

// NullableStr: "" → nil (SQL NULL); non-empty → &s. Используется для optional
// колонок (например, SecurityGroup.network_id — SG может быть project-level).
func NullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// MarshalStaticRoutes — JSONB-сериализация RouteTable.static_routes. nil → "[]",
// non-nil → JSONB bytes (empty array вместо null — для deterministic UPSERT).
func MarshalStaticRoutes(routes []domain.StaticRoute) ([]byte, error) {
	if routes == nil {
		return []byte("[]"), nil
	}
	return MarshalJSONB(routes, "RouteTable.static_routes")
}
