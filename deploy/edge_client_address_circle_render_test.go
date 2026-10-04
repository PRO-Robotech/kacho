// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// edge_client_address_circle_render_test.go — КАЖДАЯ ЦЕПОЧКА ОБЪЯВЛЯЕТ КРАЮ
// КРУГ ДОВЕРЕННЫХ ЗВЕНЬЕВ АДРЕСА КЛИЕНТА, И КРУГ ЭТОТ НЕ ПУСКАЕТ ПУБЛИЧНЫЙ
// АДРЕС (kacho#3028).
//
// Край принимает `X-Forwarded-For` только от пира из круга
// (KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS). Судится рендер каждой цепочки
// deploy/stacks.txt:
//
//  1. ручка есть у Deployment края и непуста — пустая при включённом доверии
//     заголовкам роняет старт края, и стенд это узнаёт подом в CrashLoop, а не
//     здесь;
//  2. каждая запись разбирается как сеть;
//  3. ни одна сеть не покрывает публичного адреса: звено перед краем — под
//     кластера, а сеть вида 0.0.0.0/0 вернула бы доверие заголовку от любого.
//
// ЗНАМЕНАТЕЛЬ — число цепочек с Deployment края: ноль значил бы, что судить
// было нечего.
package deploy_test

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"
)

const edgeCircleKnob = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS"

// nonPublicRanges — диапазоны, в которых живёт сеть подов: RFC 1918, RFC 6598,
// уникальные локальные IPv6 (RFC 4193).
var nonPublicRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fc00::/7"),
}

// edgeEnvValue — значение переменной окружения контейнера края; found — есть ли
// Deployment края в рендере.
func edgeEnvValue(t *testing.T, rendered, name string) (value string, declared, found bool) {
	t.Helper()
	for _, d := range decodeRender(t, rendered) {
		if str(d, "kind") != "Deployment" || str(submap(d, "metadata"), "name") != edgeDeploymentName {
			continue
		}
		found = true
		spec := submap(submap(submap(d, "spec"), "template"), "spec")
		for _, c := range slice(spec, "containers") {
			cm, _ := c.(map[string]any)
			for _, e := range slice(cm, "env") {
				em, _ := e.(map[string]any)
				if str(em, "name") == name {
					return str(em, "value"), true, true
				}
			}
		}
	}
	return "", false, found
}

// judgeEdgeCircle — находки по значению ручки одной цепочки. Чистая функция.
func judgeEdgeCircle(stack, value string, declared bool) []string {
	if !declared {
		return []string{fmt.Sprintf("цепочка %s: у края нет %s", stack, edgeCircleKnob)}
	}
	var out []string
	entries := 0
	for _, raw := range strings.Split(value, ",") {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		entries++
		p, err := netip.ParsePrefix(e)
		if err != nil {
			out = append(out, fmt.Sprintf("цепочка %s: запись %q не разбирается как сеть", stack, e))
			continue
		}
		inside := false
		for _, r := range nonPublicRanges {
			if r.Bits() <= p.Bits() && r.Contains(p.Masked().Addr()) {
				inside = true
			}
		}
		if !inside {
			out = append(out, fmt.Sprintf("цепочка %s: сеть %s покрывает публичные адреса — заголовок адреса "+
				"принимался бы от пира вне кластера", stack, p))
		}
	}
	if entries == 0 {
		out = append(out, fmt.Sprintf("цепочка %s: %s пуст — край откажет в старте", stack, edgeCircleKnob))
	}
	return out
}

func TestEveryStackDeclaresTheEdgeClientAddressCircle(t *testing.T) {
	stacks := deployStacks(t)
	edges := 0
	for _, n := range sortedStackNames(stacks) {
		value, declared, found := edgeEnvValue(t, renderChainCached(t, stacks[n]), edgeCircleKnob)
		if !found {
			continue
		}
		edges++
		t.Logf("цепочка %s: %s=%q", n, edgeCircleKnob, value)
		for _, f := range judgeEdgeCircle(n, value, declared) {
			t.Error(f)
		}
	}
	t.Logf("цепочек %d · с краем %d", len(stacks), edges)
	if edges == 0 {
		t.Fatal("ни одна цепочка не рендерит края — судить нечего")
	}
}

// Способность упасть: каждая инъекция меняет один факт, близнец молчит.
func TestEdgeClientAddressCircleJudgement_CanFailAndStaysSilent(t *testing.T) {
	for _, c := range []struct {
		name, value string
		declared    bool
		mustSay     string
	}{
		{name: "законный близнец", value: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,100.64.0.0/10,fc00::/7", declared: true},
		{name: "суженный близнец", value: "10.244.0.0/16", declared: true},
		{name: "ручки нет", declared: false, mustSay: "нет " + edgeCircleKnob},
		{name: "пусто", value: " , ", declared: true, mustSay: "пуст"},
		{name: "неразборная запись", value: "10.0.0.300/8", declared: true, mustSay: "не разбирается"},
		{name: "весь адресный простор", value: "0.0.0.0/0", declared: true, mustSay: "покрывает публичные"},
		{name: "частная сеть, расширенная за свою границу", value: "10.0.0.0/7", declared: true, mustSay: "покрывает публичные"},
		{name: "публичная сеть", value: "198.51.100.0/24", declared: true, mustSay: "покрывает публичные"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := judgeEdgeCircle("инъекция", c.value, c.declared)
			if c.mustSay == "" {
				if len(got) != 0 {
					t.Fatalf("близнец не молчит: %v", got)
				}
				return
			}
			if !strings.Contains(strings.Join(got, "\n"), c.mustSay) {
				t.Fatalf("ни одна находка не называет %q: %v", c.mustSay, got)
			}
		})
	}
}
