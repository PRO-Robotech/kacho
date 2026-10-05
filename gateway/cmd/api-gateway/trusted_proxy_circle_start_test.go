// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// trusted_proxy_circle_start_test.go — КРУГ ДОВЕРЕННЫХ ЗВЕНЬЕВ АДРЕСА КЛИЕНТА
// СУДИТСЯ СТРАЖЕМ СТАРТА (kacho#3028).
//
// Уровень — старт настоящего процесса края (тот же запуск, что у приёмки KA1):
// окружение полное во всём, кроме испытуемой ручки. Утверждается исход старта
// и что отказ называет ручку.
//
//	отказ   — запись круга не разбирается как сеть;
//	отказ   — сеть круга выходит за частные диапазоны (весь простор);
//	отказ   — сеть круга без звеньев фронта поимённо (kacho#3028, круг 3):
//	          доверие всей сети подов;
//	близнец — тот же процесс с объявленной сетью и звеньями стартует и отвечает
//	          /healthz (имя службы фронта не разрешается — звеньев пока нет,
//	          старт этим не роняется);
//	близнец — ни круга, ни звеньев: умолчание «никому», процесс стартует.

import (
	"net/http"
	"strings"
	"testing"
)

func TestTrustedProxyCircleIsJudgedAtStart(t *testing.T) {
	const knob = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS"
	const peersKnob = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_PEERS"
	const peers = "api-gateway-front-console"
	for _, c := range []struct{ name, value, peers, mustSay, mustName string }{
		{name: "неразборная запись", value: "10.0.0.0/8,10.0.0.300/8", peers: peers, mustSay: "10.0.0.300/8", mustName: knob},
		{name: "весь адресный простор", value: "10.0.0.0/8,0.0.0.0/0", peers: peers, mustSay: "0.0.0.0/0", mustName: knob},
		{name: "сеть без звеньев поимённо", value: "10.244.0.0/16", peers: "", mustSay: "10.244.0.0/16", mustName: peersKnob},
		{name: "адрес вместо имени звена", value: "10.244.0.0/16", peers: "10.244.1.17", mustSay: "10.244.1.17", mustName: peersKnob},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, listen := ka1EdgeEnv(t, "production")
			env[knob] = c.value
			env[peersKnob] = c.peers
			got := ka1RunEdge(t, env, listen)
			if !got.exited || got.code == 0 {
				t.Fatalf("процесс обязан отказать в старте; завершился=%v код=%d healthz=%d\n%s",
					got.exited, got.code, got.healthz, ka1Tail(got.journal))
			}
			if !strings.Contains(got.journal, c.mustSay) || !strings.Contains(got.journal, c.mustName) {
				t.Fatalf("отказ не называет %q и ручку %s:\n%s", c.mustSay, c.mustName, ka1Tail(got.journal))
			}
		})
	}
	for name, v := range map[string][2]string{
		"близнец: объявленная сеть и звенья — старт":      {"10.244.0.0/16", peers},
		"близнец: ни круга, ни звеньев — «никому», старт": {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			env, listen := ka1EdgeEnv(t, "production")
			env[knob] = v[0]
			env[peersKnob] = v[1]
			got := ka1RunEdge(t, env, listen)
			if got.exited || got.healthz != http.StatusOK {
				t.Fatalf("процесс обязан стартовать; завершился=%v код=%d\n%s", got.exited, got.code, ka1Tail(got.journal))
			}
		})
	}
}
