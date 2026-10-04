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
//	отказ   — доверие заголовкам включено (умолчание), круг пуст;
//	отказ   — запись круга не разбирается как сеть;
//	близнец — тот же процесс с объявленной сетью стартует и отвечает /healthz.

import (
	"net/http"
	"strings"
	"testing"
)

func TestTrustedProxyCircleIsJudgedAtStart(t *testing.T) {
	const knob = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS"
	for _, c := range []struct{ name, value, mustSay string }{
		{name: "пустой круг при включённом доверии", value: "", mustSay: knob},
		{name: "неразборная запись", value: "10.0.0.0/8,10.0.0.300/8", mustSay: "10.0.0.300/8"},
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
	t.Run("близнец: объявленная сеть — старт", func(t *testing.T) {
		env, listen := ka1EdgeEnv(t, "production")
		env[knob] = "10.244.0.0/16"
		got := ka1RunEdge(t, env, listen)
		if got.exited || got.healthz != http.StatusOK {
			t.Fatalf("процесс обязан стартовать; завершился=%v код=%d\n%s", got.exited, got.code, ka1Tail(got.journal))
		}
	})
}
