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
//	отказ   — сеть и звенья без имён звеньев в сертификате (C4): доверие адресу;
//	отказ   — боевой профиль, ни круга, ни звеньев, ни имён (C6): за звеном
//	          фронта все клиенты были бы одним адресом;
//	близнец — тот же процесс с объявленными сетью, звеньями и именами стартует и
//	          отвечает /healthz (имя службы фронта не разрешается — звеньев пока
//	          нет, старт этим не роняется);
//	близнец — профиль разработки, ничего не объявлено: «никому», процесс стартует.

import (
	"net/http"
	"strings"
	"testing"
)

func TestTrustedProxyCircleIsJudgedAtStart(t *testing.T) {
	const knob = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS"
	const peersKnob = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_PEERS"
	const sansKnob = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_SANS"
	const peers = "api-gateway-front-console"
	const sans = "api-gateway-front-console.front-link.kacho.internal"
	const anchorKnob = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CA_FILE"
	for _, c := range []struct{ name, value, peers, sans, mustSay, mustName string }{
		{name: "неразборная запись", value: "10.0.0.0/8,10.0.0.300/8", peers: peers, sans: sans, mustSay: "10.0.0.300/8", mustName: knob},
		{name: "весь адресный простор", value: "10.0.0.0/8,0.0.0.0/0", peers: peers, sans: sans, mustSay: "0.0.0.0/0", mustName: knob},
		{name: "сеть без звеньев поимённо", value: "10.244.0.0/16", peers: "", sans: sans, mustSay: "10.244.0.0/16", mustName: peersKnob},
		{name: "адрес вместо имени звена", value: "10.244.0.0/16", peers: "10.244.1.17", sans: sans, mustSay: "10.244.1.17", mustName: peersKnob},
		{name: "сеть и звенья без имён в сертификате", value: "10.244.0.0/16", peers: peers, sans: "", mustSay: "10.244.0.0/16", mustName: sansKnob},
		{name: "боевой профиль не доверяет никому", value: "", peers: "", sans: "", mustSay: knob, mustName: sansKnob},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, listen := ka1EdgeEnv(t, "production")
			env[knob] = c.value
			env[peersKnob] = c.peers
			env[sansKnob] = c.sans
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
	// Якорь звеньев (круг 5): имена без него и якорь, равный якорю установки, —
	// отказ старта процесса, называющий ручку якоря.
	for name, mutate := range map[string]func(map[string]string){
		"имена звеньев без якоря звеньев": func(e map[string]string) { delete(e, anchorKnob) },
		"якорь звеньев = якорь установки": func(e map[string]string) { e[anchorKnob] = e["KACHO_API_GATEWAY_MTLS_CA_FILE"] },
	} {
		t.Run(name, func(t *testing.T) {
			env, listen := ka1EdgeEnv(t, "production")
			mutate(env)
			got := ka1RunEdge(t, env, listen)
			if !got.exited || got.code == 0 {
				t.Fatalf("процесс обязан отказать в старте; завершился=%v код=%d healthz=%d\n%s",
					got.exited, got.code, got.healthz, ka1Tail(got.journal))
			}
			if !strings.Contains(got.journal, anchorKnob) {
				t.Fatalf("отказ не называет ручку %s:\n%s", anchorKnob, ka1Tail(got.journal))
			}
		})
	}
	for name, v := range map[string][4]string{
		"близнец: объявленные сеть, звенья и имена — старт": {"production", "10.244.0.0/16", peers, sans},
		"близнец: разработка, ничего не объявлено — старт":  {"dev", "", "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			env, listen := ka1EdgeEnv(t, v[0])
			env[knob] = v[1]
			env[peersKnob] = v[2]
			env[sansKnob] = v[3]
			got := ka1RunEdge(t, env, listen)
			if got.exited || got.healthz != http.StatusOK {
				t.Fatalf("процесс обязан стартовать; завершился=%v код=%d\n%s", got.exited, got.code, ka1Tail(got.journal))
			}
		})
	}
}
