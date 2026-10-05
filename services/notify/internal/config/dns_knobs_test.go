// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// dns_knobs_test.go — ручки DNS установки в загрузчике notify (полоса N14;
// приёмка NTF-1 NTF1-P13; замысел §8 строки `KACHO_NOTIFY_DNS_BOOT_DEADLINE`,
// `KACHO_NOTIFY_DNS_RECHECK_INTERVAL`, `KACHO_NOTIFY_STAND_DNS`; §12а «Ручки и
// признак», «Закрытый отказ старта» (Д104)).
//
// Ручки судятся по ИМЕНИ ПЕРЕМЕННОЙ: имя ручки в values замысел для них не
// вводит, а переменная — то, что видит оператор в манифесте пода.

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

const (
	envBootDeadline    = "KACHO_NOTIFY_DNS_BOOT_DEADLINE"
	envRecheckInterval = "KACHO_NOTIFY_DNS_RECHECK_INTERVAL"
	envStandDNS        = "KACHO_NOTIFY_STAND_DNS"
)

// requireOnlyRefusalEnv — как requireOnlyRefusal, но ручка опознаётся по
// переменной окружения: находка ровно одна, её ручка несёт эту переменную, и
// текст отказа называет переменную и каждое слово why.
func requireOnlyRefusalEnv(t *testing.T, err error, env string, why ...string) *config.RefusalError {
	t.Helper()
	if err == nil {
		t.Fatalf("старт принят, ожидался отказ по %s", env)
	}
	var r *config.RefusalError
	if !errors.As(err, &r) {
		t.Fatalf("отказ не в форме RefusalError (%T): %v", err, err)
	}
	if len(r.Findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка по %s, получено %d:\n%v", env, len(r.Findings), err)
	}
	if r.Findings[0].Knob.Env != env {
		t.Fatalf("находка называет ручку %s, ожидалась ручка с переменной %s: %v", r.Findings[0].Knob, env, err)
	}
	msg := err.Error()
	if !strings.Contains(msg, env) {
		t.Fatalf("текст отказа не называет переменную %s: %s", env, msg)
	}
	for _, w := range why {
		if !strings.Contains(msg, w) {
			t.Fatalf("текст отказа не несёт %q: %s", w, msg)
		}
	}
	return r
}

// loadedValueOf — значение поля загруженной конфигурации, которое загрузчик
// читает из переменной env. Поле ищется по тегу `envconfig` — единственному
// перечню ручек (config.go); поля нет — ручки в перечне нет.
func loadedValueOf(t *testing.T, cfg config.Config, env string) reflect.Value {
	t.Helper()
	v := reflect.ValueOf(cfg)
	ty := v.Type()
	for i := range ty.NumField() {
		if ty.Field(i).Tag.Get("envconfig") == env {
			return v.Field(i)
		}
	}
	t.Fatalf("в перечне ручек Config нет поля с переменной %s — загрузчик эту ручку не читает", env)
	return reflect.Value{}
}

// NTF1-P13 — ручки DNS без значения или вне границы: отказ старта, текст
// называет ручку и нарушенную границу; незаданная названа незаданной.
// Близнец — значения ровно на границах приняты и загружены.
func TestNTF1P13DNSKnobsWithoutValueOrOutOfBoundRefuseStart(t *testing.T) {
	cases := []struct {
		name string
		env  string
		edit *string
		why  []string
	}{
		{"срок старта не задан", envBootDeadline, nil, []string{"не задана"}},
		{"срок старта пуст", envBootDeadline, str(""), nil},
		{"срок старта 9s — ниже нижней", envBootDeadline, str("9s"), []string{"10s"}},
		{"срок старта 10m1s — выше верхней", envBootDeadline, str("10m1s"), []string{"10m0s"}},
		{"интервал не задан", envRecheckInterval, nil, []string{"не задана"}},
		{"интервал пуст", envRecheckInterval, str(""), nil},
		{"интервал 59s — ниже нижней", envRecheckInterval, str("59s"), []string{"1m0s"}},
		{"интервал 24h1s — выше верхней", envRecheckInterval, str("24h0m1s"), []string{"24h0m0s"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useFixture(t, map[string]*string{c.env: c.edit})
			requireOnlyRefusalEnv(t, start(t), c.env, c.why...)
		})
	}

	twins := []struct {
		env  string
		v    string
		want time.Duration
	}{
		{envBootDeadline, "10s", 10 * time.Second},
		{envBootDeadline, "10m", 10 * time.Minute},
		{envRecheckInterval, "1m", time.Minute},
		{envRecheckInterval, "24h", 24 * time.Hour},
	}
	for _, c := range twins {
		t.Run("близнец "+c.env+"="+c.v, func(t *testing.T) {
			useFixture(t, map[string]*string{c.env: str(c.v)})
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("загрузка отвергла значение на границе %s=%s: %v", c.env, c.v, err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("страж отверг значение на границе %s=%s: %v", c.env, c.v, err)
			}
			got := loadedValueOf(t, cfg, c.env)
			d, ok := got.Interface().(time.Duration)
			if !ok {
				t.Fatalf("поле ручки %s не длительность: %s", c.env, got.Type())
			}
			if d != c.want {
				t.Fatalf("загруженная конфигурация несёт %s=%v, ожидалось %v", c.env, d, c.want)
			}
		})
	}
}

// Д104 — `KACHO_NOTIFY_STAND_DNS`: ручка без умолчания; `off` либо имя хоста
// узла почты. Иное — отказ с именем ручки и хостом узла без учётной части.
func TestStandDNSKnobAgreesWithTheRelayHost(t *testing.T) {
	t.Run("ручка не задана", func(t *testing.T) {
		useFixture(t, map[string]*string{envStandDNS: nil})
		requireOnlyRefusalEnv(t, start(t), envStandDNS, "не задана")
	})
	t.Run("ручка пуста", func(t *testing.T) {
		useFixture(t, map[string]*string{envStandDNS: str("")})
		requireOnlyRefusalEnv(t, start(t), envStandDNS)
	})
	t.Run("хост ручки не равен хосту узла", func(t *testing.T) {
		useFixture(t, map[string]*string{
			envStandDNS:                        str("other-mailpit"),
			"KACHO_NOTIFY_SMTP_CONNECTION_URI": str("smtp://relay.example:587/"),
		})
		requireOnlyRefusalEnv(t, start(t), envStandDNS, "relay.example")
	})
	t.Run("хост узла назван без учётной части", func(t *testing.T) {
		useFixture(t, map[string]*string{
			envStandDNS:                        str("other-mailpit"),
			"KACHO_NOTIFY_SMTP_CONNECTION_URI": str("smtp://relay-user-marker@relay.example:587/"),
			"KACHO_NOTIFY_SMTP_CREDENTIAL":     str("relay-credential"),
		})
		r := requireOnlyRefusalEnv(t, start(t), envStandDNS, "relay.example")
		if strings.Contains(r.Error(), "relay-user-marker") {
			t.Fatalf("текст отказа несёт учётную часть адреса узла: %v", r)
		}
	})
	t.Run("близнец: off при внешнем узле", func(t *testing.T) {
		useFixture(t, map[string]*string{
			envStandDNS:                        str("off"),
			"KACHO_NOTIFY_SMTP_CONNECTION_URI": str("smtp://relay.example:587/"),
		})
		if err := start(t); err != nil {
			t.Fatalf("off отвергнут: %v", err)
		}
	})
	t.Run("близнец: хост ручки равен хосту узла", func(t *testing.T) {
		useFixture(t, map[string]*string{
			envStandDNS:                        str("kacho-mailpit"),
			"KACHO_NOTIFY_SMTP_CONNECTION_URI": str("smtp://kacho-mailpit:1025/"),
		})
		if err := start(t); err != nil {
			t.Fatalf("зона стенда при узле — приёмнике стенда отвергнута: %v", err)
		}
	})
}

// containsAny — текст несёт хотя бы одну из строк.
func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if x != "" && strings.Contains(s, x) {
			return true
		}
	}
	return false
}
