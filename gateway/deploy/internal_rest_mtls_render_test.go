// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

package deploy_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Внутренний REST-слушатель края по каждой цепочке стенда (kacho#3131).
//
// Слушатель отдаёт Internal* REST, и его транспорт — mTLS. Рендер цепочки,
// объявившей слушатель, обязан нести:
//   - P6: свой серверный лист слушателя (Certificate api-gateway-internal-rest-tls),
//     и это НЕ секрет внешнего слушателя;
//   - P7: операторскую личность (Certificate умбреллы kacho-internal-rest-operator-client-tls)
//     с URI-именем ИЗ круга, который край получает в KACHO_API_GATEWAY_INTERNAL_REST_CLIENT_SANS:
//     две записи одного имени (шаблон умбреллы и умолчание чарта края) иначе
//     разошлись бы молча — и оператор получил бы отказ рукопожатия;
//   - боевые цепочки (prod, fe3455) — без имён проброса (IP-SAN 127.0.0.1,
//     DNS localhost) на листе слушателя: адрес проброса — удобство стенда, а не
//     имя сервиса.
//
// Что край с материалом рендера СТАРТУЕТ, держит соседняя проба
// TestEveryStackEdgeStartsOnItsRenderedEnvironment: её judgeEdgeStart зовёт
// тот же config.InternalRESTListenerTLS, что корень.
//
// Чего проба НЕ держит: что IP-SAN ЕСТЬ у цепочек стенда — ключ включения
// (`api-gateway.internalRest.loopbackSAN`) живёт в профилях умбреллы; проба
// печатает его состояние в переписи.

const (
	irServerCert   = "api-gateway-internal-rest-tls"
	irOperatorCert = "kacho-internal-rest-operator-client-tls"
	irCircleKnob   = "KACHO_API_GATEWAY_INTERNAL_REST_CLIENT_SANS"
)

// irProductionChains — цепочки, чей профиль боевой: IP-SAN 127.0.0.1 на листе
// слушателя им запрещён (решение по kacho#3131).
var irProductionChains = map[string]bool{"prod": true, "fe3455": true}

type irChainFacts struct {
	listener     bool
	circle       []string
	serverCert   map[string]any
	operatorURIs []string
	tlsSecret    string
}

func irFacts(docs []map[string]any) irChainFacts {
	var f irChainFacts
	for _, d := range docs {
		kind, name := ka1Str(d, "kind"), ka1Str(ka1Sub(d, "metadata"), "name")
		switch {
		case kind == "Certificate" && name == irServerCert:
			f.serverCert = ka1Sub(d, "spec")
		case kind == "Certificate" && name == irOperatorCert:
			for _, u := range ka1Slice(ka1Sub(d, "spec"), "uris") {
				if s, ok := u.(string); ok {
					f.operatorURIs = append(f.operatorURIs, s)
				}
			}
		}
	}
	if pod, ok := edgePod(docs); ok {
		for _, c := range ka1Slice(pod, "containers") {
			cm, _ := c.(map[string]any)
			for _, e := range ka1Slice(cm, "env") {
				em, _ := e.(map[string]any)
				switch ka1Str(em, "name") {
				case "KACHO_API_GATEWAY_INTERNAL_REST_ADDR":
					f.listener = ka1Str(em, "value") != ""
				case irCircleKnob:
					for _, s := range strings.Split(ka1Str(em, "value"), ",") {
						if s = strings.TrimSpace(s); s != "" {
							f.circle = append(f.circle, s)
						}
					}
				}
			}
		}
		for _, v := range ka1Slice(pod, "volumes") {
			vm, _ := v.(map[string]any)
			if ka1Str(vm, "name") == "tls" {
				f.tlsSecret = ka1Str(ka1Sub(vm, "secret"), "secretName")
			}
		}
	}
	return f
}

// irHasLoopback — несёт ли лист имя проброса: IP 127.0.0.1 либо DNS localhost.
func irHasLoopback(spec map[string]any) bool {
	for _, ip := range ka1Slice(spec, "ipAddresses") {
		if s, _ := ip.(string); s == "127.0.0.1" {
			return true
		}
	}
	for _, n := range ka1Slice(spec, "dnsNames") {
		if s, _ := n.(string); s == "localhost" {
			return true
		}
	}
	return false
}

// irJudge — находки одной цепочки; пустой срез — цепочка годна.
func irJudge(name string, f irChainFacts) []string {
	if !f.listener {
		return nil
	}
	var out []string
	if f.serverCert == nil {
		out = append(out, fmt.Sprintf("%s: слушатель объявлен, а Certificate %s в рендере нет", name, irServerCert))
	} else {
		if s := ka1Str(f.serverCert, "secretName"); s == f.tlsSecret && s != "" {
			out = append(out, fmt.Sprintf("%s: лист слушателя — секрет внешнего слушателя %q", name, s))
		}
		if irProductionChains[name] && irHasLoopback(f.serverCert) {
			out = append(out, fmt.Sprintf("%s: боевая цепочка несёт имя проброса (IP-SAN 127.0.0.1 / localhost) на листе слушателя", name))
		}
	}
	if len(f.circle) == 0 {
		out = append(out, fmt.Sprintf("%s: %s пуст — край не стартует", name, irCircleKnob))
	}
	if len(f.operatorURIs) == 0 {
		out = append(out, fmt.Sprintf("%s: Certificate %s в рендере нет — оператору нечем войти", name, irOperatorCert))
	}
	for _, u := range f.operatorURIs {
		if len(f.circle) == 0 {
			break // пустой круг уже назван выше
		}
		found := false
		for _, c := range f.circle {
			found = found || c == u
		}
		if !found {
			out = append(out, fmt.Sprintf("%s: имя оператора %s вне круга %v", name, u, f.circle))
		}
	}
	return out
}

func TestEveryStackInternalRESTListenerIsMutualTLSWithItsOperatorInTheCircle(t *testing.T) {
	stacks := ka1Stacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	var listeners, loopback int
	var findings, census []string
	for _, name := range names {
		r := ka1Render(t, stacks[name])
		if r.err != nil {
			t.Fatalf("%s: отрисовка отказала: %v\n%s", name, r.err, ka1LastLines(r.out, 15))
		}
		f := irFacts(r.docs)
		if f.listener {
			listeners++
		}
		lb := f.serverCert != nil && irHasLoopback(f.serverCert)
		if lb {
			loopback++
		}
		census = append(census, fmt.Sprintf("%s(слушатель=%t, IP-SAN=%t, круг=%d)", name, f.listener, lb, len(f.circle)))
		findings = append(findings, irJudge(name, f)...)
	}
	t.Logf("перепись: цепочек %d, со слушателем %d, с IP-SAN 127.0.0.1 %d — %s", len(names), listeners, loopback, strings.Join(census, " · "))
	if listeners == 0 {
		t.Fatalf("знаменатель пуст: ни одна из %d цепочек не объявила слушатель — проба не исполнилась", len(names))
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// Инъекции судьи: каждая форма дефекта — своя находка, законный близнец молчит.
func TestInternalRESTRenderJudgeInjection(t *testing.T) {
	op := "spiffe://kacho.cloud/ns/kacho/sa/kacho-internal-rest-operator"
	good := func() irChainFacts {
		return irChainFacts{
			listener:     true,
			circle:       []string{op},
			serverCert:   map[string]any{"secretName": irServerCert},
			operatorURIs: []string{op},
			tlsSecret:    "api-gateway-tls",
		}
	}
	if got := irJudge("prod", good()); len(got) != 0 {
		t.Fatalf("законный близнец дал находки: %v", got)
	}
	lbDev := good()
	lbDev.serverCert = map[string]any{"secretName": irServerCert, "ipAddresses": []any{"127.0.0.1"}}
	if got := irJudge("dev", lbDev); len(got) != 0 {
		t.Fatalf("IP-SAN на цепочке стенда — не находка, а дано: %v", got)
	}
	off := irChainFacts{}
	if got := irJudge("dev", off); len(got) != 0 {
		t.Fatalf("цепочка без слушателя — судить нечего: %v", got)
	}
	cases := []struct {
		name string
		mut  func(*irChainFacts)
		want string
	}{
		{"нет листа слушателя", func(f *irChainFacts) { f.serverCert = nil }, "Certificate " + irServerCert},
		{"лист слушателя — секрет внешнего", func(f *irChainFacts) { f.serverCert = map[string]any{"secretName": "api-gateway-tls"} }, "секрет внешнего слушателя"},
		{"IP-SAN на боевой", func(f *irChainFacts) {
			f.serverCert = map[string]any{"secretName": irServerCert, "ipAddresses": []any{"127.0.0.1"}}
		}, "IP-SAN 127.0.0.1"},
		{"localhost на боевой", func(f *irChainFacts) {
			f.serverCert = map[string]any{"secretName": irServerCert, "dnsNames": []any{"localhost"}}
		}, "localhost"},
		{"пустой круг", func(f *irChainFacts) { f.circle = nil }, irCircleKnob + " пуст"},
		{"нет операторской личности", func(f *irChainFacts) { f.operatorURIs = nil }, "оператору нечем войти"},
		{"оператор вне круга", func(f *irChainFacts) { f.circle = []string{"spiffe://kacho.cloud/ns/kacho/sa/other"} }, "вне круга"},
	}
	for _, c := range cases {
		f := good()
		c.mut(&f)
		got := irJudge("prod", f)
		if len(got) != 1 || !strings.Contains(got[0], c.want) {
			t.Errorf("%s: ждали одну находку с %q, получили %v", c.name, c.want, got)
		}
	}
}
