// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_lane_validation_test.go — сценарий F4d-06 приёмки Ф4д: страж края
// разводит требования ПО ПОСАДКЕ, а не предъявляет их всегда.
//
// Утверждается ТЕКСТ отказа, а не только исход: тон отказа при старте — часть
// контракта оператора и одно из трёх мест, выведенных из-под запрета на
// публичный разбор.
//
// ОСЬ ПОСТАВЩИКА СНЯТА С ПРЕДМЕТОМ (#2734). Здесь стояли случаи «под external
// незаданный административный адрес поставщика отвергает старт» и «заданный
// административный адрес судится одинаково на обеих посадках». Край больше не
// читает ни одного адреса поставщика, и требовать их нечем: оставленные случаи
// судили бы ручку, которой у процесса нет. Остались случаи о том, что посадка
// по-прежнему РАЗВОДИТ: наш авторитет отзыва под `own` и отказ на незаданной
// посадке раньше любых требований.
package main

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ снятия: под `external` край стартует, не требуя ни
// одного адреса поставщика, — снятая ось не оставила требования.
func TestF4d06_ExternalLaneDemandsNoProviderAddress(t *testing.T) {
	if err := validateProductionRevocationConfig("production", RevocationConfig{
		IdentityProvider: identityposture.External,
	}); err != nil {
		t.Fatalf("под external край не обязан объявлять адресов поставщика — их больше не читают, получено: %v", err)
	}
}

// Под `own` край по-прежнему требует своего — авторитета отзыва: снятие оси
// поставщика не сняло защиту заодно.
func TestF4d06_OwnLaneDoesNotStopDemandingItsOwn(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IdentityProvider: identityposture.Own,
	})
	if err == nil {
		t.Fatal("под own авторитет отзыва обязан оставаться обязательным")
	}
	if !strings.Contains(err.Error(), "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL is empty") {
		t.Fatalf("отказ обязан называть оставшееся требование, получено: %q", err.Error())
	}
}

// F4d-01 у края: посадка обязана быть ОБЪЯВЛЕНА, и отказ производится ДО любых
// посадочных требований — по неизвестной посадке требовать нечего.
func TestF4d06_UnsetLaneRefusesBeforeAnyLaneScopedDemand(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IdentityProvider: identityposture.Unset,
	})
	if err == nil {
		t.Fatal("незаданная посадка обязана отвергать старт края")
	}
	msg := err.Error()
	if !strings.Contains(msg, config.IdentityProviderKnob) {
		t.Fatalf("отказ обязан называть ручку посадки, получено: %q", msg)
	}
	if strings.Contains(msg, "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL") {
		t.Fatalf("при незаданной посадке посадочное требование предъявляться не должно: %q", msg)
	}
}
