// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// ka1_port_race_test.go — исход пробы старта края не зависит от того, свободен
// ли заранее какой-либо порт.
//
// Пробы старта (KA1-20/21, домен печенья, круг звеньев, издатели) поднимают
// настоящий процесс края с четырьмя слушателями. Адрес, выбранный пробой ДО
// старта («открыть :0, прочитать порт, закрыть»), к моменту подъёма процесса
// может занять кто угодно: соседний процесс пакетов, идущих параллельно, или
// сам край — ядро вправе вернуть тот же порт двум подряд таким выборам, и тогда
// второй слушатель процесса падает на «bind: address already in use» (так упал
// близнец домена печенья на хуке отправки kacho#3324).
//
// Предмет пробы — ровно это свойство: соперник, держащий любой адрес, который
// окружение пробы называет, старт не срывает. Близнец — тот же старт без
// соперника.

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
)

// ka1AddrKnobs — ручки адресов слушателей края в окружении пробы старта.
var ka1AddrKnobs = []string{
	"KACHO_API_GATEWAY_LISTEN_ADDR",
	"KACHO_API_GATEWAY_INTERNAL_REST_ADDR",
	"KACHO_API_GATEWAY_METRICS_ADDR",
	"KACHO_API_GATEWAY_TLS_LISTEN_ADDR",
}

// ka1Rival занимает адрес и сбрасывает каждое соединение: запрос к нему не
// повисает и ответа не получает.
func ka1Rival(t *testing.T, addr string) {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("соперник не занял адрес %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if err == nil {
				_ = c.Close()
			}
		}
	}()
}

func TestEdgeStartDoesNotDependOnAPreChosenPortBeingFree(t *testing.T) {
	// Соперник — на одном адресе за раз: каждый слушатель края свидетельствует
	// сам, и отказ одного не прячет отказ другого (первым поднимается
	// диагностическая поверхность).
	for _, knob := range ka1AddrKnobs {
		t.Run("соперник на "+knob, func(t *testing.T) {
			env := ka1EdgeEnv(t, "production")
			if env[knob] == "" {
				t.Fatalf("предпосылка: окружение пробы не называет %s", knob)
			}
			ka1Rival(t, env[knob])
			got := ka1RunEdge(t, env)
			if got.exited || got.healthz != http.StatusOK {
				t.Fatalf("старт сорван соперником на %s=%s: завершился=%v код=%d /healthz %d\n%s",
					knob, env[knob], got.exited, got.code, got.healthz, ka1Tail(got.journal))
			}
			if strings.Contains(got.journal, "address already in use") {
				t.Errorf("журнал старта называет занятый адрес:\n%s", ka1Tail(got.journal))
			}
		})
	}
	t.Run("близнец без соперника", func(t *testing.T) {
		got := ka1RunEdge(t, ka1EdgeEnv(t, "production"))
		if got.exited || got.healthz != http.StatusOK {
			t.Fatalf("старт без соперника обязан ответить /healthz 200: завершился=%v код=%d\n%s",
				got.exited, got.code, ka1Tail(got.journal))
		}
	})
}
