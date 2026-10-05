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
//	близнец — тот же процесс с объявленной сетью стартует и отвечает /healthz;
//	близнец — круг не объявлен: умолчание «никому», процесс стартует.

import (
	"net/http"
	"strings"
	"testing"
)

func TestTrustedProxyCircleIsJudgedAtStart(t *testing.T) {
	const knob = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS"
	for _, c := range []struct{ name, value, mustSay string }{
		{name: "неразборная запись", value: "10.0.0.0/8,10.0.0.300/8", mustSay: "10.0.0.300/8"},
		{name: "весь адресный простор", value: "10.0.0.0/8,0.0.0.0/0", mustSay: "0.0.0.0/0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, listen := ka1EdgeEnv(t, "production")
			env[knob] = c.value
			got := ka1RunEdge(t, env, listen)
			if !got.exited || got.code == 0 {
				t.Fatalf("процесс обязан отказать в старте; завершился=%v код=%d healthz=%d\n%s",
					got.exited, got.code, got.healthz, ka1Tail(got.journal))
			}
			if !strings.Contains(got.journal, c.mustSay) || !strings.Contains(got.journal, knob) {
				t.Fatalf("отказ не называет %q и ручку %s:\n%s", c.mustSay, knob, ka1Tail(got.journal))
			}
		})
	}
	for name, value := range map[string]string{
		"близнец: объявленная сеть — старт":          "10.244.0.0/16",
		"близнец: круг не объявлен — «никому», старт": "",
	} {
		t.Run(name, func(t *testing.T) {
			env, listen := ka1EdgeEnv(t, "production")
			env[knob] = value
			got := ka1RunEdge(t, env, listen)
			if got.exited || got.healthz != http.StatusOK {
				t.Fatalf("процесс обязан стартовать; завершился=%v код=%d\n%s", got.exited, got.code, ka1Tail(got.journal))
			}
		})
	}
}
