// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_lane_validation_test.go — сценарий F4d-06 приёмки Ф4д: страж края
// требует адресов внешнего поставщика ПО ПОЛОСЕ, а не всегда.
//
// Утверждается ТЕКСТ отказа, а не только исход: тон отказа при старте — часть
// контракта оператора и одно из трёх мест, выведенных из-под запрета на
// публичный разбор.
//
// ГРАНИЦА ЗАДАЧИ НАЗВАНА ОТДЕЛЬНЫМ СЛУЧАЕМ. Из края переезжает ТОЛЬКО
// административный адрес: он нужен ровно затем, чтобы выход человека снял
// сессию на стороне поставщика, а под `own` такой сессии не существует.
//
// ЗДЕСЬ СТОЯЛО «полоса интроспекции остаётся обязательной на ОБЕИХ полосах —
// её предмет принадлежит соседней задаче». Соседняя задача исполнена: под `own`
// адреса интроспекции поставщика не существует ни одного законного (пусто —
// отказ, наш собственный авторитет — отказ по пути), то есть возможность была
// объявлена и неисполнима. Требование не СНЯТО, а ЗАМЕЩЕНО нашим авторитетом
// отзыва — `own_lane_revocation_authority_test.go`.
package main

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/pkg/identityposture"
)

// testAdminCA — якорь доверия административного контура. Часть фикстуры, не
// предмет случая: страж отвергает TLS без якоря на любой полосе, и опустив его
// здесь, каждый случай падал бы не о том.
const testAdminCA = "/etc/api-gateway/hydra-admin-ca/ca.crt"

// F4d-06 — под `external` незаданный административный адрес старт не проходит,
// и текст называет ручку и предмет.
func TestF4d06_ExternalLaneStillDemandsTheProviderAdminAddress(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IdentityProvider: identityposture.External,
		IntrospectionURL: tlsIntrospectURL,
		AdminCAFile:      testAdminCA,
		AdminURL:         "",
	})
	if err == nil {
		t.Fatal("под external незаданный административный адрес обязан отвергать старт")
	}
	msg := err.Error()
	if !strings.Contains(msg, "KACHO_HYDRA_ADMIN_URL is empty") {
		t.Fatalf("отказ обязан называть ручку и предмет, получено: %q", msg)
	}
	if !strings.Contains(msg, config.IdentityProviderKnob+"=own and this requirement is lifted") {
		t.Fatalf("отказ обязан назвать значение, которым требование снимается, получено: %q", msg)
	}
}

// F4d-06 — под `own` край стартует БЕЗ административного адреса поставщика.
func TestF4d06_OwnLaneStartsWithoutTheProviderAdminAddress(t *testing.T) {
	cfg := ownLane()
	cfg.IntrospectionURL = tlsIntrospectURL
	cfg.AdminCAFile = testAdminCA
	cfg.AdminURL = ""
	if err := validateProductionRevocationConfig("production", cfg); err != nil {
		t.Fatalf("под own административный адрес поставщика не требуется, получено: %v", err)
	}
}

// F4d-06 — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ того, что полосность не сняла защиту заодно:
// под `own` край по-прежнему требует своего — авторитета отзыва.
//
// НАМЕРЕНИЕ СЛУЧАЯ СОХРАНЕНО ДОСЛОВНО, изменилось имя ручки: авторитет отзыва
// под `own` — НАШ, и требование к нему теперь называет его собственную ручку, а
// не ручку поставщика, которой под этой посадкой нечего адресовать.
func TestF4d06_OwnLaneDoesNotStopDemandingItsOwn(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IdentityProvider: identityposture.Own,
		IntrospectionURL: "",
		AdminURL:         "",
	})
	if err == nil {
		t.Fatal("под own авторитет отзыва обязан оставаться обязательным")
	}
	if !strings.Contains(err.Error(), "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL is empty") {
		t.Fatalf("отказ обязан называть оставшееся требование, получено: %q", err.Error())
	}
}

// Заданный административный адрес проверяется ОДИНАКОВО на обеих полосах:
// полосность снимает требование НАЛИЧИЯ, а не правила транспорта.
func TestF4d06_ADeclaredAdminAddressIsJudgedTheSameOnBothLanes(t *testing.T) {
	for _, lane := range identityposture.Values() {
		t.Run(lane.String(), func(t *testing.T) {
			err := validateProductionRevocationConfig("production", RevocationConfig{
				IdentityProvider: lane,
				IntrospectionURL: tlsIntrospectURL,
				AdminCAFile:      testAdminCA,
				AdminURL:         "http://provider-admin.kacho.svc:4445",
			})
			if err == nil {
				t.Fatal("незашифрованный административный контур обязан отвергаться на любой полосе")
			}
			if !strings.Contains(err.Error(), "KACHO_HYDRA_ADMIN_URL") {
				t.Fatalf("отказ обязан называть ручку, получено: %q", err.Error())
			}
		})
	}
}

// F4d-01 у края: посадка обязана быть ОБЪЯВЛЕНА, и отказ производится ДО любых
// полосных требований — по неизвестной посадке требовать нечего.
func TestF4d06_UnsetLaneRefusesBeforeAnyLaneScopedDemand(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IdentityProvider: identityposture.Unset,
		IntrospectionURL: "",
		AdminURL:         "",
	})
	if err == nil {
		t.Fatal("незаданная посадка обязана отвергать старт края")
	}
	msg := err.Error()
	if !strings.Contains(msg, config.IdentityProviderKnob) {
		t.Fatalf("отказ обязан называть ручку посадки, получено: %q", msg)
	}
	for _, lanescoped := range []string{"KACHO_HYDRA_INTROSPECTION_URL", "KACHO_HYDRA_ADMIN_URL"} {
		if strings.Contains(msg, lanescoped) {
			t.Fatalf("при незаданной посадке полосное требование %q предъявляться не должно: %q",
				lanescoped, msg)
		}
	}
}

// Разбор ручки края идёт ОБЩИМ словарём, и отказ называет ЕЁ ручку, а не чужую.
func TestF4d06_TheEdgeRefusalNamesTheEdgeKnob(t *testing.T) {
	cfg := config.Config{IdentityProvider: "Own"} // соседняя раскладка регистра
	_, err := cfg.ResolvedIdentityProvider()
	if err == nil {
		t.Fatal("значение вне словаря обязано быть отвергнуто")
	}
	if !strings.Contains(err.Error(), config.IdentityProviderKnob) {
		t.Fatalf("отказ края обязан называть ЕГО ручку, получено: %q", err.Error())
	}
	if strings.Contains(err.Error(), "authn.identity-provider") {
		t.Fatalf("отказ края назвал ручку службы прав — оператор пойдёт править не тот профиль: %q", err.Error())
	}

	// Положительный контроль: каноническое значение принимается.
	ok := config.Config{IdentityProvider: "own"}
	got, err := ok.ResolvedIdentityProvider()
	if err != nil || got != identityposture.Own {
		t.Fatalf("каноническое значение обязано приниматься, получено %v / %v", got, err)
	}
}
