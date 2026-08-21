// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// idempotency_validation.go — хранилище однократности и объявленный размер
// флота перестали быть двумя местами об одном предмете (#694).
//
// # Предмет
//
// Край принимает `Idempotency-Key` и обещает по нему однократность: тот же ключ
// — тот же ответ, обработчик ниже вызывается один раз. Обещание верно ровно до
// второй реплики, если записи живут в памяти процесса: повтор, попавший в
// соседний под, записи не находит и уходит к downstream. Отказа при этом не
// происходит — мутация просто исполняется второй раз, и наблюдаемого признака
// у этого нет.
//
// Условие, при котором обещание верно, было НАЗВАНО — комментарием в самой
// середине, — и не было связано ни с чем: посадка объявляла автомасштабирование
// до десяти реплик, и два объявления об одном предмете расходились молча.
//
// # Что делает этот отказ
//
// Он делает пару проверяемой при СТАРТЕ. Чарт рендерит размер флота из того же
// значения, что питает автомасштабирование, поэтому «сколько реплик объявлено»
// и «что об этом думает процесс» — одна величина, а не две. Хранилище в памяти
// процесса законно ровно для флота из одной реплики; для большего требуется
// общее (`gateway/internal/idempotencypg`).
//
// Отказ, а не предупреждение: край, поднявшийся с непригодной парой, продолжает
// ОБЕЩАТЬ однократность и не исполнять её — то есть отвечает успехом на просьбу,
// которую не выполнил.
package main

import (
	"fmt"
	"strings"
)

// Значения ручки KACHO_IDEMPOTENCY_STORE. Список закрыт: неизвестное значение
// отвергается, а не сводится к умолчанию — иначе опечатка тихо переключала бы
// хранилище на то, которого оператор не выбирал.
const (
	idempotencyStoreMemory   = "memory"
	idempotencyStorePostgres = "postgres"
)

// IdempotencyPairing — то, что сверяется: вид хранилища, его адрес и объявленный
// посадкой размер флота.
type IdempotencyPairing struct {
	// StoreKind — KACHO_IDEMPOTENCY_STORE.
	StoreKind string
	// DSN — KACHO_IDEMPOTENCY_DSN.
	DSN string
	// FleetSize — KACHO_GATEWAY_FLEET_SIZE: верхняя граница числа реплик,
	// объявленная профилем посадки (при включённом автомасштабировании — его
	// максимум, иначе — число реплик развёртывания).
	FleetSize int
}

// validateIdempotencyFleetPairing отказывает в старте, когда гарантия
// однократности не может быть исполнена при объявленной посадке.
//
// Сообщение НАЗЫВАЕТ ручки и причину: это рантайм-диагностика оператору, а не
// публичный артефакт, и без имени ручки стенд не поднять.
func validateIdempotencyFleetPairing(p IdempotencyPairing) error {
	kind := strings.ToLower(strings.TrimSpace(p.StoreKind))
	switch kind {
	case idempotencyStoreMemory, idempotencyStorePostgres:
	default:
		return fmt.Errorf(
			"KACHO_IDEMPOTENCY_STORE=%q is not a store this build knows; "+
				"expected %q (in-process, single replica only) or %q (shared, any fleet size) "+
				"(refuse to start)",
			p.StoreKind, idempotencyStoreMemory, idempotencyStorePostgres)
	}

	if p.FleetSize < 1 {
		return fmt.Errorf(
			"KACHO_GATEWAY_FLEET_SIZE=%d declares a fleet smaller than one replica, "+
				"which no deployment can satisfy; render it from the same value that drives "+
				"autoscaling (refuse to start)", p.FleetSize)
	}

	if kind == idempotencyStorePostgres && strings.TrimSpace(p.DSN) == "" {
		return fmt.Errorf(
			"KACHO_IDEMPOTENCY_STORE=%q but KACHO_IDEMPOTENCY_DSN is empty — the shared "+
				"idempotency store has no address, and deriving one from a neighbour's address "+
				"would look configured while leading nowhere (refuse to start)",
			idempotencyStorePostgres)
	}

	if kind == idempotencyStoreMemory && p.FleetSize > 1 {
		return fmt.Errorf(
			"KACHO_IDEMPOTENCY_STORE=%q keeps Idempotency-Key records in this process, "+
				"but the deployment declares up to %d replicas (KACHO_GATEWAY_FLEET_SIZE). "+
				"A repeat that lands on another replica finds no record and the mutation runs "+
				"a second time, silently — the header promises exactly-once and would not "+
				"deliver it. Either declare one replica, or point "+
				"KACHO_IDEMPOTENCY_STORE=%q at a shared store via KACHO_IDEMPOTENCY_DSN "+
				"(refuse to start)",
			idempotencyStoreMemory, p.FleetSize, idempotencyStorePostgres)
	}
	return nil
}
