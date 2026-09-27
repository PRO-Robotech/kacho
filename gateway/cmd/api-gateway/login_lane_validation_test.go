// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_validation_test.go — страж старта адреса полосы формы (приёмка
// Ф3 Р2, Ф3-45): под посадкой `own` ручка обязательна. Страж зовётся из ветки
// посадки `own` корня, её держит own_lane_readers_wiring_test.go. Каждый
// отрицательный случай стоит рядом с положительным близнецом, и различие одно.
package main

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// loginLaneWired — годная полоса формы целиком: положительный близнец.
func loginLaneWired() LoginLaneConfig {
	return LoginLaneConfig{
		URL:            "https://kacho-umbrella-kaname-internal.kacho.svc:9098",
		ClientCertFile: "/etc/api-gateway/mtls/tls.crt",
		ClientKeyFile:  "/etc/api-gateway/mtls/tls.key",
		CAFile:         "/etc/api-gateway/mtls/ca.crt",
	}
}

func TestLoginLaneGuard_F3_45_OwnLaneStartsWithTheLaneWired(t *testing.T) {
	if err := validateLoginLaneConfig(loginLaneWired()); err != nil {
		t.Fatalf("годная полоса формы под own обязана проходить: %v", err)
	}
}

func TestLoginLaneGuard_F3_45_OwnLaneRefusesToStartWithoutTheLaneAddress(t *testing.T) {
	cfg := loginLaneWired()
	cfg.URL = ""
	err := validateLoginLaneConfig(cfg)
	if err == nil {
		t.Fatal("под own незаданный адрес полосы формы обязан отвергать старт: ретрансляция без цели отвечала бы 503 на каждом запросе всю жизнь")
	}
	if !strings.Contains(err.Error(), config.LoginLaneURLKnob) {
		t.Fatalf("отказ обязан называть ручку %s, получено: %q", config.LoginLaneURLKnob, err.Error())
	}
}

// Адрес судится теми же правилами, что хопы к нашему авторитету: не
// незашифрованный (по ретрансляции едет носитель сессии), и абсолютный.
func TestLoginLaneGuard_F3_45_PlaintextAndMalformedAddressesAreRefused(t *testing.T) {
	for _, raw := range []string{"http://kaname-internal.kacho.svc:9098", "kaname-internal:9098", "://x"} {
		cfg := loginLaneWired()
		cfg.URL = raw
		err := validateLoginLaneConfig(cfg)
		if err == nil {
			t.Fatalf("адрес %q обязан отвергаться", raw)
		}
		if !strings.Contains(err.Error(), config.LoginLaneURLKnob) {
			t.Fatalf("отказ на %q обязан называть ручку: %q", raw, err.Error())
		}
	}
}

// Слушатель формы взаимный по TLS (Р16): без клиентской пары край отвергается
// на каждом рукопожатии, и это контроль без механизма исполниться. Половина
// пары хуже отсутствия обеих — она выглядит настроенной.
func TestLoginLaneGuard_F3_45_ClientCertificatePairIsRequiredWithTheAddress(t *testing.T) {
	cases := map[string]func(*LoginLaneConfig){
		"пары нет":          func(c *LoginLaneConfig) { c.ClientCertFile, c.ClientKeyFile = "", "" },
		"только сертификат": func(c *LoginLaneConfig) { c.ClientKeyFile = "" },
		"только ключ":       func(c *LoginLaneConfig) { c.ClientCertFile = "" },
		"якоря нет":         func(c *LoginLaneConfig) { c.CAFile = "" },
	}
	for name, mutate := range cases {
		cfg := loginLaneWired()
		mutate(&cfg)
		err := validateLoginLaneConfig(cfg)
		if err == nil {
			t.Fatalf("%s: обязан отвергаться", name)
		}
		if !strings.Contains(err.Error(), "KACHO_API_GATEWAY_MTLS_") {
			t.Fatalf("%s: отказ обязан называть ручку пары, получено %q", name, err.Error())
		}
	}
}
