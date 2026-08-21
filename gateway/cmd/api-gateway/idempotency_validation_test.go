// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// idempotency_validation_test.go — отказ в старте на неисполнимой паре
// «хранилище ↔ объявленный флот» (#694).
//
// Каждая ось утверждается С ОБЕИХ СТОРОН: отрицание без положительного близнеца
// зелено и у проверки, отвергающей всё, а положительное без отрицания — у
// проверки, не отвергающей ничего.
package main

import (
	"strings"
	"testing"
)

func TestValidateIdempotencyFleetPairing(t *testing.T) {
	cases := []struct {
		name string
		in   IdempotencyPairing
		// wantKnobs — ручки, которые сообщение обязано назвать. Пусто ⇒ пара
		// исполнима и отказа быть не должно.
		wantKnobs []string
	}{
		{
			name: "память + одна реплика — законно",
			in:   IdempotencyPairing{StoreKind: "memory", FleetSize: 1},
		},
		{
			name: "память + две реплики — отказ, названы обе ручки",
			in:   IdempotencyPairing{StoreKind: "memory", FleetSize: 2},
			wantKnobs: []string{
				"KACHO_IDEMPOTENCY_STORE", "KACHO_GATEWAY_FLEET_SIZE", "KACHO_IDEMPOTENCY_DSN",
			},
		},
		{
			name: "память + десять реплик — тот же отказ (это и была посадка по умолчанию)",
			in:   IdempotencyPairing{StoreKind: "memory", FleetSize: 10},
			wantKnobs: []string{
				"KACHO_IDEMPOTENCY_STORE", "KACHO_GATEWAY_FLEET_SIZE",
			},
		},
		{
			name: "общее хранилище + десять реплик — законно",
			in: IdempotencyPairing{
				StoreKind: "postgres", DSN: "postgres://u@h/kacho_gateway?sslmode=require", FleetSize: 10,
			},
		},
		{
			name:      "общее хранилище без адреса — отказ, названа ручка адреса",
			in:        IdempotencyPairing{StoreKind: "postgres", FleetSize: 1},
			wantKnobs: []string{"KACHO_IDEMPOTENCY_DSN"},
		},
		{
			name:      "адрес из одних пробелов — тот же отказ (значение есть, содержания нет)",
			in:        IdempotencyPairing{StoreKind: "postgres", DSN: "   ", FleetSize: 1},
			wantKnobs: []string{"KACHO_IDEMPOTENCY_DSN"},
		},
		{
			name:      "неизвестный вид хранилища — отказ, а не молчаливый откат на память",
			in:        IdempotencyPairing{StoreKind: "redis", FleetSize: 1},
			wantKnobs: []string{"KACHO_IDEMPOTENCY_STORE"},
		},
		{
			name:      "флот меньше одной реплики — отказ: такого не бывает",
			in:        IdempotencyPairing{StoreKind: "memory", FleetSize: 0},
			wantKnobs: []string{"KACHO_GATEWAY_FLEET_SIZE"},
		},
		{
			name: "регистр и пробелы в виде хранилища не меняют решения",
			in:   IdempotencyPairing{StoreKind: "  Memory ", FleetSize: 1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateIdempotencyFleetPairing(tc.in)
			if len(tc.wantKnobs) == 0 {
				if err != nil {
					t.Fatalf("пара исполнима, но старт отвергнут: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("пара неисполнима (%+v), но старт разрешён — край поднялся бы, "+
					"продолжая обещать однократность и не исполняя её", tc.in)
			}
			msg := err.Error()
			for _, knob := range tc.wantKnobs {
				if !strings.Contains(msg, knob) {
					t.Errorf("сообщение оператору не называет ручку %q: %s\n"+
						"Без имени ручки стенд не поднять — это рантайм-диагностика, "+
						"а не публичный артефакт.", knob, msg)
				}
			}
			if !strings.Contains(msg, "refuse to start") {
				t.Errorf("сообщение не говорит, что процесс отказывается стартовать: %s", msg)
			}
		})
	}
}

// TestIdempotencyPairing_MemoryIsRejectedExactlyAboveOneReplica — граница
// проверена в обе стороны на самой оси: единица проходит, двойка нет.
//
// Односторонняя проба зеленела бы и у проверки, отвергающей любой флот.
func TestIdempotencyPairing_MemoryIsRejectedExactlyAboveOneReplica(t *testing.T) {
	if err := validateIdempotencyFleetPairing(
		IdempotencyPairing{StoreKind: idempotencyStoreMemory, FleetSize: 1}); err != nil {
		t.Fatalf("флот из одной реплики отвергнут: %v", err)
	}
	if err := validateIdempotencyFleetPairing(
		IdempotencyPairing{StoreKind: idempotencyStoreMemory, FleetSize: 2}); err == nil {
		t.Fatal("флот из двух реплик с хранилищем в памяти процесса принят — " +
			"повтор, попавший в соседнюю реплику, исполнил бы мутацию второй раз")
	}
}
