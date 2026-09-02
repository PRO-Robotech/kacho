// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

// module_set.go — RBAC rules-model 2026 platform module-set ownership.
//
// The domain OWNS the closed platform module-set. A Rule.module must be a member
// of this set (besides being grammar-valid and non-empty); Rule.Validate consults
// IsKnownModule to reject an unknown module on the request-path (INVALID_ARGUMENT)
// — WITHOUT the domain importing authzmap (clean-arch: pure Go,
// stdlib only).
//
// The set MUST stay in lockstep with the module-prefixes of authzmap.objectTypes
// (the FGA object-type catalog) — authzmap CONSUMES this set (or is held lockstep
// via the authzmap↔domain drift-test). geo is intentionally absent (Geography is
// its own service, not in objectTypes); the load-balancer module token is
// `loadbalancer` (NOT `nlb`).
//
// # ЗДЕСЬ ДВА ИСТОЧНИКА ОДНОВРЕМЕННО, И ЭТО ПЕРЕХОДНОЕ СОСТОЯНИЕ (#1927)
//
// Литерал ниже — прежний источник, а [ModuleSet] — новый ПОРТ, через который
// набор приходит извне. Оба стоят рядом ОДНИМ изменением намеренно: снятие
// литерала прежде появления читателя оставило бы дерево без обоих, а членство
// модуля читается на ПУТИ ЗАПРОСА — то есть отказом на живом трафике. Читателей
// переводит следующее изменение, литерал снимается им же.

// knownModules — the closed set of platform modules a rule may grant over. Order
// is the canonical platform order (iam first, then resource domains).
var knownModules = []string{"iam", "vpc", "compute", "loadbalancer", "registry", "storage"}

// knownModuleSet — membership index built once from knownModules.
var knownModuleSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(knownModules))
	for _, k := range knownModules {
		m[k] = struct{}{}
	}
	return m
}()

// IsKnownModule reports whether m is a member of the closed platform module-set
// declared by knownModules above — today {iam, vpc, compute, loadbalancer, registry,
// storage}. The wildcard `*` is NOT a known module (it is a system-only marker
// handled separately by Rule.Validate).
//
// Перечень здесь НАЗЫВАЕТ набор, а не задаёт его: единственный источник — литерал
// knownModules. Эта строка уже переживала свой предмет — она осталась при пяти
// именах, когда шестое (storage) было добавлено, и разошлась молча: проба набора
// утверждала ЧЛЕНСТВО, а не равенство, поэтому росту набора не сопротивлялась.
func IsKnownModule(m string) bool {
	_, ok := knownModuleSet[m]
	return ok
}

// KnownModules returns a copy of the closed platform module-set in canonical
// order. Used by the authzmap↔domain drift-test to assert lockstep.
func KnownModules() []string {
	return append([]string(nil), knownModules...)
}

// ModuleSet — членство в наборе модулей платформы, каким его знает ВЫЗЫВАЮЩИЙ.
//
// Подстановочный знак `*` членом набора не является ни в одной реализации: он не
// имя модуля, а маркер политики, и разбирается [Rule.Validate] отдельно.
type ModuleSet interface {
	// IsKnownModule — состоит ли модуль в наборе.
	IsKnownModule(module string) bool
}

// moduleSetOf — набор, собранный из перечня имён.
type moduleSetOf map[string]struct{}

// IsKnownModule реализует [ModuleSet].
func (s moduleSetOf) IsKnownModule(module string) bool {
	_, ok := s[module]
	return ok
}

// ModuleSetOf — набор из перечня имён, для вызывающего, у которого перечень уже
// в руках: канон дерева, фикстура пробы, применитель ролей модуля.
//
// Пустой перечень даёт набор, не признающий НИЧЕГО, и это не вырожденный случай,
// а тот же fail-closed, что у отсутствующего набора ниже: «перечень пуст» не есть
// «принимаем любой». Разница лишь в том, что здесь отказ приходит с именем
// модуля, а там — с указанием на непровязанный источник.
func ModuleSetOf(names ...string) ModuleSet {
	s := make(moduleSetOf, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		s[n] = struct{}{}
	}
	return s
}
