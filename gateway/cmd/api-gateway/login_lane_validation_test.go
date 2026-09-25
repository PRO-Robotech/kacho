// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_validation_test.go — страж старта адреса полосы формы (приёмка
// Ф3 Р2, Ф3-45): под посадкой `own` ручка обязательна, под `external` — не
// читается. Каждый отрицательный случай стоит рядом с положительным близнецом,
// и различие одно.
package main

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// loginLaneWired — годная полоса формы целиком: положительный близнец.
func loginLaneWired() LoginLaneConfig {
	return LoginLaneConfig{
		Target:         mustRelayTargetDecl(middleware.RelayTargetForm),
		URL:            "https://kaname-internal.kacho.svc:9100",
		ClientCertFile: "/etc/api-gateway/mtls/tls.crt",
		ClientKeyFile:  "/etc/api-gateway/mtls/tls.key",
		CAFile:         "/etc/api-gateway/mtls/ca.crt",
	}
}

func TestLoginLaneGuard_F3_45_OwnLaneStartsWithTheLaneWired(t *testing.T) {
	if err := validateLoginLaneConfig(identityposture.Own, loginLaneWired()); err != nil {
		t.Fatalf("годная полоса формы под own обязана проходить: %v", err)
	}
}

func TestLoginLaneGuard_F3_45_OwnLaneRefusesToStartWithoutTheLaneAddress(t *testing.T) {
	cfg := loginLaneWired()
	cfg.URL = ""
	err := validateLoginLaneConfig(identityposture.Own, cfg)
	if err == nil {
		t.Fatal("под own незаданный адрес полосы формы обязан отвергать старт: ретрансляция без цели отвечала бы 503 на каждом запросе всю жизнь")
	}
	if !strings.Contains(err.Error(), config.LoginLaneURLKnob) {
		t.Fatalf("отказ обязан называть ручку %s, получено: %q", config.LoginLaneURLKnob, err.Error())
	}
}

// Под `external` ручка не читается: полосы формы там нет, и требовать адрес
// значило бы не пускать в старт край, которому он не нужен ни для чего.
func TestLoginLaneGuard_F3_45_ExternalLaneDoesNotReadTheKnob(t *testing.T) {
	for _, cfg := range []LoginLaneConfig{{}, {URL: "http://plaintext.invalid"}, {Target: mustRelayTargetDecl(middleware.RelayTargetIssuance)}} {
		if err := validateLoginLaneConfig(identityposture.External, cfg); err != nil {
			t.Fatalf("под external ручка не читается, получено: %v", err)
		}
	}
}

// Адрес судится теми же правилами, что хопы к нашему авторитету: не
// незашифрованный (по ретрансляции едет носитель сессии), и абсолютный.
func TestLoginLaneGuard_F3_45_PlaintextAndMalformedAddressesAreRefused(t *testing.T) {
	for _, raw := range []string{"http://kaname-internal.kacho.svc:9098", "kaname-internal:9098", "://x"} {
		cfg := loginLaneWired()
		cfg.URL = raw
		err := validateLoginLaneConfig(identityposture.Own, cfg)
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
		err := validateLoginLaneConfig(identityposture.Own, cfg)
		if err == nil {
			t.Fatalf("%s: обязан отвергаться", name)
		}
		if !strings.Contains(err.Error(), "KACHO_API_GATEWAY_MTLS_") {
			t.Fatalf("%s: отказ обязан называть ручку пары, получено %q", name, err.Error())
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Вторая цель ретрансляции — слушатель выдачи службы, на который край уводит
// обе координаты церемонии авторизации (замысел LINE-A-1 §5.1б п. 2, §7 инв.
// 36; полоса L13). Набор осей стража выводится из РЕЖИМА ПРЕДЪЯВЛЕНИЯ цели, а не
// из дословного паритета со стражем полосы формы.

// issuanceWired — годная цель выдачи целиком: положительный близнец. Пары
// клиента нет — цель её не спрашивает.
func issuanceWired() LoginLaneConfig {
	return LoginLaneConfig{
		Target: mustRelayTargetDecl(middleware.RelayTargetIssuance),
		URL:    "https://kaname.kacho.svc:9096",
		CAFile: "/etc/api-gateway/mtls/ca.crt",
	}
}

func TestIssuanceRelayGuard_L13_OwnStartsWithTheIssuanceTargetWired(t *testing.T) {
	if err := validateLoginLaneConfig(identityposture.Own, issuanceWired()); err != nil {
		t.Fatalf("годная цель выдачи под own обязана проходить: %v", err)
	}
}

// Три оси при ЛЮБОМ режиме: адрес непуст · абсолютный https · корень
// пришпилен. Отказ называет ручку ЭТОЙ цели, а не соседней.
func TestIssuanceRelayGuard_L13_TheThreeModeIndependentAxesRefuseAndNameTheTargetsKnob(t *testing.T) {
	cases := map[string]func(*LoginLaneConfig){
		"адрес пуст":            func(c *LoginLaneConfig) { c.URL = "" },
		"адрес не https":        func(c *LoginLaneConfig) { c.URL = "http://kaname.kacho.svc:9096" },
		"адрес не абсолютный":   func(c *LoginLaneConfig) { c.URL = "kaname.kacho.svc:9096" },
		"корень не пришпилен":   func(c *LoginLaneConfig) { c.CAFile = "" },
		"цель не объявлена":     func(c *LoginLaneConfig) { c.Target = relayTargetDecl{} },
		"режим цели неизвестен": func(c *LoginLaneConfig) { c.Target.Mode = "optional" },
		"ручка цели не названа": func(c *LoginLaneConfig) { c.Target.URLKnob = "" },
	}
	for name, mutate := range cases {
		cfg := issuanceWired()
		mutate(&cfg)
		err := validateLoginLaneConfig(identityposture.Own, cfg)
		if err == nil {
			t.Fatalf("%s: обязан отвергать старт", name)
		}
		if strings.Contains(err.Error(), config.LoginLaneURLKnob) {
			t.Fatalf("%s: отказ цели выдачи называет ручку полосы формы: %q", name, err.Error())
		}
	}
	for _, name := range []string{"адрес пуст", "адрес не https", "адрес не абсолютный"} {
		cfg := issuanceWired()
		cases[name](&cfg)
		if err := validateLoginLaneConfig(identityposture.Own, cfg); !strings.Contains(err.Error(), config.IssuanceURLKnob) {
			t.Fatalf("%s: отказ обязан называть ручку %s: %q", name, config.IssuanceURLKnob, err.Error())
		}
	}
}

// Четвёртая ось — пара «сертификат и ключ» — при `server-tls-only` объявлена
// НЕПРИМЕНИМОЙ явно, с названным режимом, а не опущена молча. Инъекция в обе
// стороны: половина пары у цели выдачи НЕ краснит (судить нечего — ось, судящая
// её, судила бы несуществующее); та же половина у цели формы (`mutual`) краснит.
func TestIssuanceRelayGuard_L13_ClientPairAxisIsDeclaredInapplicableUnderServerTLSOnly(t *testing.T) {
	for name, mutate := range map[string]func(*LoginLaneConfig){
		"пары нет":          func(c *LoginLaneConfig) { c.ClientCertFile, c.ClientKeyFile = "", "" },
		"только сертификат": func(c *LoginLaneConfig) { c.ClientCertFile, c.ClientKeyFile = "/x/tls.crt", "" },
		"только ключ":       func(c *LoginLaneConfig) { c.ClientCertFile, c.ClientKeyFile = "", "/x/tls.key" },
	} {
		issuance := issuanceWired()
		mutate(&issuance)
		if err := validateLoginLaneConfig(identityposture.Own, issuance); err != nil {
			t.Errorf("%s у цели выдачи (server-tls-only): страж судит пару, которой цель не спрашивает: %v", name, err)
		}
		form := loginLaneWired()
		mutate(&form)
		if err := validateLoginLaneConfig(identityposture.Own, form); err == nil {
			t.Errorf("%s у цели формы (mutual): обязан отвергать старт", name)
		}
	}
	axes := relayGuardAxes(mustRelayTargetDecl(middleware.RelayTargetIssuance))
	pair, found := relayGuardAxis{}, false
	for _, a := range axes {
		if a.Name == relayAxisClientPair {
			pair, found = a, true
		}
	}
	if !found {
		t.Fatal("ось пары клиента у цели выдачи ОПУЩЕНА — неприменимость обязана быть названа, а не выпасть из перечня")
	}
	if pair.Applies || !strings.Contains(pair.Reason, string(relayClientAuthServerTLSOnly)) {
		t.Fatalf("ось пары клиента у цели выдачи: применима=%v, причина %q — обязана быть неприменимой с названным режимом", pair.Applies, pair.Reason)
	}
	for _, a := range relayGuardAxes(mustRelayTargetDecl(middleware.RelayTargetForm)) {
		if !a.Applies {
			t.Errorf("у цели формы (mutual) ось %q объявлена неприменимой: %q", a.Name, a.Reason)
		}
	}
	t.Logf("перепись: осей у цели выдачи %d (применимых %d) · у цели формы %d",
		len(axes), countApplying(axes), len(relayGuardAxes(mustRelayTargetDecl(middleware.RelayTargetForm))))
}

// Страж судит КАЖДУЮ пару «адрес плюс удостоверение», а не первую: перечень
// целей стража и закрытый перечень целей объявления — один предмет (инв. 36).
func TestRelayTargetDecls_L13_OneDeclarationPerDeclaredTarget(t *testing.T) {
	decls := relayTargetDecls()
	targets := middleware.RelayTargets()
	if len(decls) != len(targets) {
		t.Fatalf("объявлений целей у стража %d, целей у объявления путей %d", len(decls), len(targets))
	}
	knobs := map[string]bool{}
	for _, tg := range targets {
		d, ok := relayTargetDeclFor(tg)
		if !ok {
			t.Fatalf("у цели %q нет объявления стража — её пара ехала бы без стража", tg)
		}
		if d.URLKnob == "" || knobs[d.URLKnob] {
			t.Fatalf("цель %q: ручка адреса %q пуста либо уже принадлежит другой цели — ось различения «цель» обязана быть ручкой адреса", tg, d.URLKnob)
		}
		knobs[d.URLKnob] = true
		if _, err := d.Mode.presentsClientPair(); err != nil {
			t.Fatalf("цель %q: режим предъявления %q вне закрытого перечня: %v", tg, d.Mode, err)
		}
	}
	if _, ok := relayTargetDeclFor("foreign"); ok {
		t.Fatal("объявление стража нашлось для цели вне перечня")
	}
	form, _ := relayTargetDeclFor(middleware.RelayTargetForm)
	issuance, _ := relayTargetDeclFor(middleware.RelayTargetIssuance)
	if form.Mode != relayClientAuthMutual || issuance.Mode != relayClientAuthServerTLSOnly {
		t.Fatalf("режимы целей: форма %q (ожидалось mutual, Ф3 Р16), выдача %q (ожидалось server-tls-only, registryTokenClientAuthMode)", form.Mode, issuance.Mode)
	}
	if form.URLKnob != config.LoginLaneURLKnob || issuance.URLKnob != config.IssuanceURLKnob {
		t.Fatalf("ручки адреса целей: форма %q, выдача %q", form.URLKnob, issuance.URLKnob)
	}
}

func countApplying(axes []relayGuardAxis) int {
	n := 0
	for _, a := range axes {
		if a.Applies {
			n++
		}
	}
	return n
}
