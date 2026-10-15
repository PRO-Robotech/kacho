// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// console_ingress_without_tls_refuses_render_test.go — консоль, выставленная
// входом контроллера без TLS, НЕ рендерится, и отказ называет причину
// (kacho#3183).
//
// Служба доступа выдаёт печенье контекста формы с `Secure`, и браузер не хранит
// его на происхождении `http`. Консоль за входом без TLS поднималась, а законная
// регистрация получала 403 FORM_TOKEN_REJECTED: искали неисправную форму, а
// неисправен транспорт. Причину называл лишь комментарий в values.
//
// Условие отказа — вход консоли включён и его перечень `tls` пуст; собственная
// TLS-раздача консоли (`publicFront`) от отказа не освобождает. Законные
// близнецы — вход с TLS и раздача при выключенном входе.
package deploy_test

import (
	"path/filepath"
	"strings"
	"testing"
)

const consoleNoTLSRefusal = "консоль выставлена входом без TLS"

func renderProdConsole(t *testing.T, setJSON ...string) (string, error) {
	t.Helper()
	requireHelmForRender(t)
	return helmRenderFiles([]string{filepath.Join(umbrellaDir, "values.prod.yaml")},
		append([]string{`uif.enabled=true`}, setJSON...)...)
}

func TestConsoleIngressWithoutTLSRefusesRenderNamingTheCause(t *testing.T) {
	out, err := renderProdConsole(t, `uif.ingress={"enabled":true,"tls":[]}`)
	if err == nil {
		t.Fatal("консоль за входом без TLS рендерится — регистрация на ней получит отказ формы, " +
			"а причину назовёт только комментарий в values")
	}
	if !strings.Contains(out, consoleNoTLSRefusal) || !strings.Contains(out, "Secure") {
		t.Fatalf("рендер отказал, но не назвал причину (ждали %q и Secure-печенье):\n%s",
			consoleNoTLSRefusal, out)
	}
}

func TestConsoleIngressWithTLSRenders(t *testing.T) {
	out, err := renderProdConsole(t,
		`uif.ingress={"enabled":true,"host":"console.example.test","tls":[{"hosts":["console.example.test"],"secretName":"console-tls"}]}`)
	if err != nil {
		t.Fatalf("законный близнец (вход с TLS) не рендерится: %v\n%s", err, out)
	}
}

func TestConsoleOwnTLSFrontWithoutIngressRenders(t *testing.T) {
	out, err := renderProdConsole(t,
		`uif.ingress={"enabled":false,"tls":[]}`,
		`uif.publicFront={"enabled":true,"tls":{"secretName":"console-tls","certificate":{"create":false}}}`)
	if err != nil && strings.Contains(out, consoleNoTLSRefusal) {
		t.Fatalf("собственная TLS-раздача консоли названа «без TLS»:\n%s", out)
	}
}

// Собственная TLS-раздача консоли не освобождает ВХОД консоли: вход подчарта
// рендерится по `ingress.enabled` независимо от publicFront, и при пустом `tls`
// наружу выходит обычный http (ревью security-auditor волны 3245, находка 1).
// Близнец — TestConsoleOwnTLSFrontWithoutIngressRenders (вход выключен).
func TestConsoleOwnTLSFrontDoesNotExemptAPlaintextIngress(t *testing.T) {
	out, err := renderProdConsole(t,
		`uif.ingress={"enabled":true,"tls":[]}`,
		`global.kacho.identity.appBaseURL="https://console.example.test"`,
		`uif.publicFront={"enabled":true,"tls":{"secretName":"console-tls","certificate":{"create":false}},"clientAddress":{"proxyProtocol":{"enabled":false,"disabledBecause":"probe"}}}`)
	if err == nil {
		t.Fatal("вход консоли без TLS рендерится, потому что включена собственная раздача — " +
			"вход от неё не зависит и выходит наружу обычным http")
	}
	if !strings.Contains(out, consoleNoTLSRefusal) {
		t.Fatalf("рендер отказал не по причине входа без TLS (ждали %q):\n%s", consoleNoTLSRefusal, out)
	}
}
