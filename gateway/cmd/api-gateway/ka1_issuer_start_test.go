// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// ka1_issuer_start_test.go — приёмка KA1, сценарии KA1-32 и KA1-33 (Р5): канон
// издателя на старте процесса. Уровень — старт процесса, запросов нет, кроме
// `/healthz`; окружение — полное во всём, как у близнеца KA1-20 (боевая
// посадка), кроме объявления издателя. Адреса набора и авторитета к сети на
// старте не нужны: конструктор проверяющего к сети не ходит.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// ka1IssuerEnv — окружение боевой посадки с объявлением издателя по строке.
func ka1IssuerEnv(t *testing.T, issuers, keySets, platform string) (map[string]string, string) {
	t.Helper()
	env, listen := ka1EdgeEnv(t, "production")
	env["KACHO_API_GATEWAY_TOKEN_ISSUERS"] = issuers
	env["KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS"] = keySets
	env["KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER"] = platform
	env["KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL"] = "https://revocation.issuer.example.test/introspect"
	return env, listen
}

// verifierWiredRecord — запись журнала старта `token verifier wired into principal path`.
func verifierWiredRecord(journal string) (map[string]any, bool) {
	for _, line := range strings.Split(journal, "\n") {
		if !strings.Contains(line, `"msg":"token verifier wired into principal path"`) {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) == nil {
			return rec, true
		}
	}
	return nil, false
}

// KA1-32 — объявление в разных формах одного издателя: край стартует, запись
// журнала старта несёт platform_issuer_accepted=true и ровно один принятый
// издатель.
func TestKA1_32_DeclarationFormsOfOneIssuerStart(t *testing.T) {
	env, listen := ka1IssuerEnv(t,
		"https://issuer.example.test/realm/",
		"HTTPS://issuer.example.test/realm=https://keys.issuer.example.test/jwks",
		"https://issuer.example.test:443/realm")
	got := ka1RunEdge(t, env, listen)
	if got.exited || got.healthz != http.StatusOK {
		t.Fatalf("KA1-32: процесс обязан стартовать и отвечать /healthz 200; завершился=%v код=%d\n%s",
			got.exited, got.code, ka1Tail(got.journal))
	}
	rec, ok := verifierWiredRecord(got.journal)
	if !ok {
		t.Fatalf("KA1-32: записи журнала старта «token verifier wired into principal path» нет:\n%s", ka1Tail(got.journal))
	}
	accepted, _ := rec["accepted_issuers"].([]any)
	if rec["platform_issuer_accepted"] != true || len(accepted) != 1 {
		t.Errorf("KA1-32: platform_issuer_accepted=%v, accepted_issuers=%v — ожидались true и ровно один элемент",
			rec["platform_issuer_accepted"], rec["accepted_issuers"])
	}
}

// KA1-33 — объявление, неразличимое каноном, либо не абсолютный http(s)-URL —
// отказ в старте до открытия слушателей.
func TestKA1_33_IndistinguishableOrNonURLDeclarationRefusesToStart(t *testing.T) {
	const knob = "KACHO_API_GATEWAY_TOKEN_ISSUERS"
	refuse := func(t *testing.T, env map[string]string, listen string, mustName ...string) {
		t.Helper()
		got := ka1RunEdge(t, env, listen)
		if !got.exited || got.code == 0 {
			t.Fatalf("процесс обязан завершиться с ненулевым кодом; стартовал (/healthz %d)", got.healthz)
		}
		if strings.Contains(got.journal, `"msg":"api-gateway started"`) {
			t.Errorf("отказ пришёл после открытия слушателя")
		}
		for _, w := range mustName {
			if !strings.Contains(got.journal, w) {
				t.Errorf("текст отказа не называет %q:\n%s", w, ka1Tail(got.journal))
			}
		}
	}
	start := func(t *testing.T, env map[string]string, listen string) {
		t.Helper()
		if got := ka1RunEdge(t, env, listen); got.exited || got.healthz != http.StatusOK {
			t.Fatalf("близнец обязан стартовать; завершился=%v код=%d\n%s", got.exited, got.code, ka1Tail(got.journal))
		}
	}

	t.Run("(а) две формы одного издателя", func(t *testing.T) {
		env, listen := ka1IssuerEnv(t,
			"https://issuer.example.test/realm,https://ISSUER.example.test/realm/",
			"https://issuer.example.test/realm=https://keys.issuer.example.test/a,https://ISSUER.example.test/realm/=https://keys.issuer.example.test/b",
			"https://issuer.example.test/realm")
		refuse(t, env, listen, knob, "https://issuer.example.test/realm", "https://ISSUER.example.test/realm/")
	})
	t.Run("(а) близнец: /realm и /realm2", func(t *testing.T) {
		env, listen := ka1IssuerEnv(t,
			"https://issuer.example.test/realm,https://issuer.example.test/realm2",
			"https://issuer.example.test/realm=https://keys.issuer.example.test/a,https://issuer.example.test/realm2=https://keys.issuer.example.test/b",
			"https://issuer.example.test/realm")
		start(t, env, listen)
	})
	t.Run("(б) не абсолютный URL", func(t *testing.T) {
		env, listen := ka1IssuerEnv(t, "issuer.example.test/realm",
			"issuer.example.test/realm=https://keys.issuer.example.test/a", "issuer.example.test/realm")
		refuse(t, env, listen, knob, "issuer.example.test/realm", "http(s)")
	})
	t.Run("(б) близнец: абсолютный https", func(t *testing.T) {
		env, listen := ka1IssuerEnv(t, "https://issuer.example.test/realm",
			"https://issuer.example.test/realm=https://keys.issuer.example.test/a", "https://issuer.example.test/realm")
		start(t, env, listen)
	})
}
