// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package helpers

import "github.com/PRO-Robotech/kacho/services/vpc/internal/domain"

// OrEmptyStrSlice: nil → empty slice (для JSONB-сериализации; иначе `null`
// вместо `[]` в БД-колонке). Используется в NIC-сценариях для
// security_group_ids/v4_address_ids/v6_address_ids.
func OrEmptyStrSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// NIStatusName — domain enum → DB column text (status TEXT в network_interfaces).
//
// Ветвей столько же, сколько значений у перечисления: снятые значения статуса не
// производит ни один путь записи, а ограничение столбца их больше не принимает —
// ветвь под них была бы кодом, который не исполнится, и одновременно заявкой на
// значение, которое база отвергнет.
func NIStatusName(s domain.NetworkInterfaceStatus) string {
	switch s {
	case domain.NIStatusActive:
		return domain.NIStatusStrActive
	case domain.NIStatusAvailable:
		return domain.NIStatusStrAvailable
	default:
		return domain.NIStatusStrUnspecified
	}
}

// NIStatusFromName — DB column text → domain enum.
//
// Значение вне словаря приезжает как `STATUS_UNSPECIFIED`, а не роняет чтение:
// столбец закрыт CHECK-ограничением, поэтому попасть сюда чужая строка может
// только при расхождении ограничения с кодом, и это расхождение ловит
// интеграционная проба ограничения, а не путь чтения арендатора.
func NIStatusFromName(s string) domain.NetworkInterfaceStatus {
	switch s {
	case domain.NIStatusStrActive:
		return domain.NIStatusActive
	case domain.NIStatusStrAvailable:
		return domain.NIStatusAvailable
	default:
		return domain.NIStatusUnspecified
	}
}
