// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// trusted_forwarders_test.go — путь «переменная окружения → конфиг → значение,
// уезжающее в corelib».
//
// Стража старта и поведение цепочки проверяются в cmd/kacho-registry; здесь
// предмет другой и не менее важный: ручка обязана ДОЕХАТЬ. Объявленное, но не
// читаемое поле — это ровно тот класс, из-за которого дыра и жила: проводка
// выглядела правильной, а сузить круг было нечем.
//
// Тесты идут через LoadInto (реальный путь загрузки окружения), а не через литерал
// структуры.

import (
	"testing"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
)

// gatewaySAN — личность отправителя, законно передающего личность конечного
// пользователя в registry (значение из values.prod).
const gatewaySAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"

func loadEnv(t *testing.T, env map[string]string) config.Config {
	t.Helper()
	// Объявление домена величин — часть законной посадки: у ручки ровно два
	// законных значения, и незаданное среди них не значится. Отправная точка,
	// его не несущая, отличалась бы от законной ДВУМЯ фактами сразу, и красное
	// ниже перестало бы означать то, что объявлено. Случай, которому нужно
	// иное значение, задаёт его сам.
	if _, ok := env["KACHO_REGISTRY_QUOTA_AUTHORITY"]; !ok {
		env["KACHO_REGISTRY_QUOTA_AUTHORITY"] = "not-deployed"
	}
	var c config.Config
	if err := config.LoadInto(&c, env); err != nil {
		t.Fatalf("LoadInto err = %v", err)
	}
	return c
}

// TestTrustedForwarders_ArrivesFromTheEnvironment — ручка читается тем же
// загрузчиком, что и весь остальной конфиг, и список через запятую разбирается.
func TestTrustedForwarders_ArrivesFromTheEnvironment(t *testing.T) {
	c := loadEnv(t, map[string]string{
		"KACHO_REGISTRY_DB_PASSWORD":                  "secret",
		"KACHO_REGISTRY_AUTHZ_TRUSTED_FORWARDER_SANS": gatewaySAN,
	})
	got := c.TrustedForwarders().SANs()
	if len(got) != 1 || got[0] != gatewaySAN {
		t.Fatalf("TrustedForwarders() = %#v, want exactly [%q] — "+
			"the knob does not reach the value that goes into WithTrustedForwarders", got, gatewaySAN)
	}
}

// TestTrustedForwarders_MatchesTheCorelibFilter — значение, отдаваемое в проводку,
// отфильтровано так же, как фильтрует corelib (пустые записи отбрасываются),
// плюс срезаны окружающие пробелы: corelib сравнивает личность сертификата
// побайтово, поэтому запись " spiffe://…" там не совпала бы ни с одним
// сертификатом и обернулась бы отказом в обслуживании законному отправителю.
func TestTrustedForwarders_MatchesTheCorelibFilter(t *testing.T) {
	c := loadEnv(t, map[string]string{
		"KACHO_REGISTRY_DB_PASSWORD":                  "secret",
		"KACHO_REGISTRY_AUTHZ_TRUSTED_FORWARDER_SANS": " ," + gatewaySAN + " ,",
	})
	got := c.TrustedForwarders().SANs()
	if len(got) != 1 || got[0] != gatewaySAN {
		t.Fatalf("TrustedForwarders() = %#v, want exactly [%q]", got, gatewaySAN)
	}
}

// TestTrustedForwarders_UnsetIsEmpty — незаданная ручка даёт пустой список (а не,
// скажем, срез из одной пустой строки, который выглядел бы заполненным для
// проверки по длине). Именно на этом значении обязана отказать стража старта.
func TestTrustedForwarders_UnsetIsEmpty(t *testing.T) {
	c := loadEnv(t, map[string]string{"KACHO_REGISTRY_DB_PASSWORD": "secret"})
	if got := c.TrustedForwarders(); got.IsNarrowed() {
		t.Fatalf("TrustedForwarders() = %#v, want empty when the knob is unset", got)
	}
}
