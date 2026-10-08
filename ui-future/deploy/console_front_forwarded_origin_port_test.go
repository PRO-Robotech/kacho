// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_front_forwarded_origin_port_test.go — вход консоли, который площадка
// НЕ публикует (kacho#3102).
//
// Стенд проб в своём пространстве имён общего кластера не получает балансировщика:
// его вход пробрасывается к тому, кто им пользуется, и порт происхождения — порт
// проброса. Ручка `publicFront.service.type` разрешает такой порт РОВНО у входа
// типа ClusterIP; у балансировщика площадки происхождение по-прежнему обязано
// быть на 443. Судится исход рендера по обе стороны одного факта.
package deploy_test

import (
	"strings"
	"testing"
)

func forwardedFrontValues(t *testing.T, svcType, origin string) map[string]any {
	t.Helper()
	vals := consoleValuesOfChain(t, []string{"values.dev.yaml", "values.dev-prod.yaml"})
	pf := map[string]any{
		"enabled": true,
		"service": map[string]any{"type": svcType},
		"tls": map[string]any{
			"secretName": "console-public-tls",
			"certificate": map[string]any{
				"create": true, "namesFromOrigin": true,
				"issuerRef": map[string]any{"name": "kacho-internal-ca", "kind": "ClusterIssuer"},
			},
		},
		"acme": map[string]any{"enabled": false},
	}
	vals["publicFront"] = pf
	g, _ := vals["global"].(map[string]any)
	k, _ := g["kacho"].(map[string]any)
	id, _ := k["identity"].(map[string]any)
	id["appBaseURL"] = origin
	return vals
}

func TestForwardedConsoleFrontAcceptsTheForwardPortAndPublishesNoBalancer(t *testing.T) {
	out, err := renderConsoleChart(t, forwardedFrontValues(t, "ClusterIP", "https://t1-x.kacho.test:25058"))
	if err != nil {
		t.Fatalf("вход ClusterIP с портом проброса отвергнут: %v\n%s", err, out)
	}
	var svc map[string]any
	for _, d := range decodeDocs(t, out) {
		if str(d["kind"]) == "Service" && dig(d, "spec", "ports") != nil &&
			strings.HasSuffix(str(dig(d, "metadata", "name")), "-public") {
			svc = d
		}
	}
	if svc == nil {
		t.Fatal("Service внешнего входа не отрендерен")
	}
	if got := str(dig(svc, "spec", "type")); got != "ClusterIP" {
		t.Errorf("тип Service входа %q, ждали ClusterIP", got)
	}
	if dig(svc, "spec", "externalTrafficPolicy") != nil {
		t.Errorf("externalTrafficPolicy у ClusterIP — поле балансировщика, API его отвергнет")
	}
	if !strings.Contains(out, "t1-x.kacho.test") {
		t.Errorf("имя происхождения без порта не попало в сертификат")
	}
}

// Близнец: тот же порт у балансировщика площадки — по-прежнему отказ.
func TestPublishedConsoleFrontStillRefusesAnOriginOffPort443(t *testing.T) {
	out, err := renderConsoleChart(t, forwardedFrontValues(t, "LoadBalancer", "https://t1-x.kacho.test:25058"))
	if err == nil || !strings.Contains(out, "отличный от 443") {
		t.Fatalf("балансировщик с происхождением не на 443 принят:\n%s", out)
	}
	out, err = renderConsoleChart(t, forwardedFrontValues(t, "NodePort", "https://t1-x.kacho.test"))
	if err == nil || !strings.Contains(out, "publicFront.service.type") {
		t.Fatalf("тип входа вне перечня принят:\n%s", out)
	}
}
