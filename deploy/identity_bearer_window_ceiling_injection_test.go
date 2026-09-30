// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_bearer_window_ceiling_injection_test.go — доказательство падучести
// MAIL-51 в обе стороны.
//
// Вход — НАСТОЯЩИЙ: рендер подчарта службы цепочкой боевого стека, в который
// точечной установкой вносится ровно один факт; законный близнец — тот же
// рендер без правки — прогоняется первым.
package deploy_test

import (
	"strings"
	"testing"
)

// bearerWindowOfProd — объявления срока полосы входа боевого стека плюс правка.
func bearerWindowOfProd(t *testing.T, sets ...string) []bearerWindowLifespan {
	t.Helper()
	out, err := renderStackSubchart(t, "prod", stackIdentityValues(t, chainOf(t, "prod")), sets...)
	if err != nil {
		t.Fatalf("рендер стека prod отказал: %v\n%s", err, out)
	}
	return bearerWindowLifespansOf(kanameServiceConfig(t, out))
}

// TestMAIL51Injection_LawfulTemplateIsSilent — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ.
func TestMAIL51Injection_LawfulTemplateIsSilent(t *testing.T) {
	findings, bounding := bearerWindowFindings("prod", bearerWindowOfProd(t))
	if len(findings) != 0 || bounding != 2 {
		t.Fatalf("законный близнец: находок %v, ограничивающих %d (ждали 0 и 2)", findings, bounding)
	}
}

// TestMAIL51Injection_RaisedCeilingIsAFinding — поднятый срок кода письма.
func TestMAIL51Injection_RaisedCeilingIsAFinding(t *testing.T) {
	for _, c := range []struct{ set, key string }{
		{"config.authn.login.recoveryCodeTtl=6m", "recovery-code-ttl"},
		{"config.authn.login.verificationCodeTtl=31m", "verification-code-ttl"},
	} {
		findings, _ := bearerWindowFindings("prod", bearerWindowOfProd(t, c.set))
		if len(findings) != 1 || !strings.Contains(findings[0], c.key) || !strings.Contains(findings[0], "превышает потолок") {
			t.Errorf("%s: ожидалась одна находка о потолке `%s`, получено %v", c.set, c.key, findings)
		}
	}
}

// TestMAIL51Injection_UnclassifiedLifespanIsAFinding — объявление срока, которого
// разбор не знает (например, полоса, сменившая код на ссылку новым ключом).
func TestMAIL51Injection_UnclassifiedLifespanIsAFinding(t *testing.T) {
	decls := append(bearerWindowOfProd(t), bearerWindowLifespan{Key: "recovery-link-ttl", Raw: "1h"})
	findings, _ := bearerWindowFindings("prod", decls)
	if len(findings) != 1 || !strings.Contains(findings[0], "recovery-link-ttl") || !strings.Contains(findings[0], "не отнесено") {
		t.Fatalf("неотнесённое объявление обязано быть находкой, получено %v", findings)
	}
}

// TestMAIL51Injection_UnparsableLifespanIsAFinding — величина, которую разбор
// не читает, — находка, а не пропуск.
func TestMAIL51Injection_UnparsableLifespanIsAFinding(t *testing.T) {
	findings, _ := bearerWindowFindings("prod", bearerWindowOfProd(t, "config.authn.login.recoveryCodeTtl=пять"))
	if len(findings) != 1 || !strings.Contains(findings[0], "не разбирается") {
		t.Fatalf("неразбираемая величина обязана быть находкой, получено %v", findings)
	}
}
