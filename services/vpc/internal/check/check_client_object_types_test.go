// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVPCObjectTypesCoverEveryResourceScope — набор типов, по которым отказ
// скрывает существование, обязан совпадать с ресурсными типами домена.
//
// Перечень БОЛЬШЕ не выписан: он выводится из карты. Проба закрепляет состав,
// потому что вывод может ошибиться в другую сторону — затянуть в набор
// иерархический якорь (`project`/`cluster`), и тогда отказ на списке начал бы
// отвечать «не найдено» вместо «нет доступа», сообщая о коллекции то, что о ней
// и не скрывают.
//
// Числа названы, чтобы «ноль лишнего» было отличимо от «ничего не прочитано».
func TestVPCObjectTypesCoverEveryResourceScope(t *testing.T) {
	want := []string{
		"vpc_address",
		"vpc_cidr_group",
		"vpc_gateway",
		"vpc_network",
		"vpc_network_interface",
		"vpc_route_table",
		"vpc_security_group",
		"vpc_subnet",
	}

	got := make([]string, 0, len(vpcObjectTypes))
	for k := range vpcObjectTypes {
		got = append(got, k)
	}
	sort.Strings(got)

	require.Equal(t, want, got,
		"состав типов, скрывающих существование, разошёлся с ресурсными типами домена")
	t.Logf("перепись: записей карты %d, ресурсных типов %d", len(PermissionMap()), len(got))
}
