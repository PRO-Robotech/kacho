// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import "testing"

// TestSubjectUsersetKnowsTheSystemClass — распознаватель субъектной вершины гейта
// производителей знает системный класс и молчит на законных близнецах.
//
// Без класса в словаре субъектов отношение `public_reader: [system_class]`
// реестра (канон kaname v0.5.0) читается указателем принадлежности, и гейт
// требует от этого дерева производителя у выдачи, которую пишет служба доступа
// (kacho#3287). Близнецы: тип-контейнер остаётся структурным, и имя, лишь
// начинающееся с имени класса, классом не является — класс узнаётся ЭЛЕМЕНТОМ
// перечня, а не подстрокой.
func TestSubjectUsersetKnowsTheSystemClass(t *testing.T) {
	t.Parallel()
	for userset, want := range map[string]bool{
		"system_class":                           true,
		"system_class, user, service_account":    true,
		"user with mfa_fresh, system_class":      true,
		"registry_registry":                      false,
		"project":                                false,
		"system_class_reader":                    false,
		"registry_registry, system_class_reader": false,
	} {
		if got := hasSubjectUserset(userset); got != want {
			t.Errorf("hasSubjectUserset(%q) = %v, ожидалось %v", userset, got, want)
		}
	}
}
