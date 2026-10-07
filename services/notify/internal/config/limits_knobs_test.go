// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"testing"
)

// NTF1-H08 — ручки лимитов Р10 без значения или вне границы: отказ старта с
// именем ручки и границей; со всеми ручками в границах — старт (§8 замысла):
//
//	notify.limits.recipient.security.perDay   [1..1000]
//	notify.limits.recipient.notice.perHour    [1..10000]
//	notify.limits.recipient.notice.perDay     [1..10000]
//	notify.limits.global.perDay               [1..10000000]
//	notify.sourceLimits.<модуль>.rate / burst / paused   [1..1000] / [1..10000] / bool
//
// Ручки на источник приходят процессу одной переменной `KACHO_NOTIFY_SOURCE_LIMITS`
// — JSON-объект «модуль → {rate, burst, paused}», параметризующий уже выведенный
// перечень `KACHO_NOTIFY_SOURCES` (замысел З24, приёмка Р10 «где живут ручки на
// источник»): у модуля перечня без своей записи умолчания нет — отказ с именем
// модуля.
//
// Близнец каждого отрицания — фикстура `testdata/boot.env` целиком
// (TestBootFixtureStarts): каждое отрицание меняет в ней одну строку.

const (
	securityPerDayEnv = "KACHO_NOTIFY_LIMITS_RECIPIENT_SECURITY_PER_DAY"
	noticePerHourEnv  = "KACHO_NOTIFY_LIMITS_RECIPIENT_NOTICE_PER_HOUR"
	noticePerDayEnv   = "KACHO_NOTIFY_LIMITS_RECIPIENT_NOTICE_PER_DAY"
	globalPerDayEnv   = "KACHO_NOTIFY_LIMITS_GLOBAL_PER_DAY"
	sourceLimitsEnv   = "KACHO_NOTIFY_SOURCE_LIMITS"
	sourceLimitsKnob  = "notify.sourceLimits"
)

// gridKnobs — ручки сетки и потолка: переменная, имя ручки, граница в тексте
// отказа, значения за обеими сторонами границы и на обеих границах.
var gridKnobs = []struct {
	env, knob, bound  string
	below, above      string
	lowEdge, highEdge string
}{
	{securityPerDayEnv, "notify.limits.recipient.security.perDay", "[1..1000]", "0", "1001", "", "1000"},
	{noticePerHourEnv, "notify.limits.recipient.notice.perHour", "[1..10000]", "0", "10001", "1", "10000"},
	{noticePerDayEnv, "notify.limits.recipient.notice.perDay", "[1..10000]", "0", "10001", "1", "10000"},
	{globalPerDayEnv, "notify.limits.global.perDay", "[1..10000000]", "0", "10000001", "1", "10000000"},
}

// Нижняя граница сетки `security` судится вдобавок инвариантом H07 (сетка ≥
// 1,25 × сумма суточных limits шаблонов `security` сборки), поэтому у неё
// близнец только верхней границы: единица могла бы краснеть по H07, а не по
// границе.

func TestConfig_NTF1H08_GridKnobWithoutValueRefusesStart(t *testing.T) {
	for _, k := range gridKnobs {
		t.Run(k.knob+"/не задана", func(t *testing.T) {
			useFixture(t, map[string]*string{k.env: nil})
			requireOnlyRefusal(t, start(t), k.knob, "не задана")
		})
		t.Run(k.knob+"/пустая строка", func(t *testing.T) {
			useFixture(t, map[string]*string{k.env: str("")})
			requireOnlyRefusal(t, start(t), k.knob)
		})
		t.Run(k.knob+"/ниже границы", func(t *testing.T) {
			useFixture(t, map[string]*string{k.env: str(k.below)})
			requireOnlyRefusal(t, start(t), k.knob, k.bound)
		})
		t.Run(k.knob+"/выше границы", func(t *testing.T) {
			useFixture(t, map[string]*string{k.env: str(k.above)})
			requireOnlyRefusal(t, start(t), k.knob, k.bound)
		})
		for _, v := range []string{k.lowEdge, k.highEdge} {
			if v == "" {
				continue
			}
			t.Run(k.knob+"/близнец "+v, func(t *testing.T) {
				useFixture(t, map[string]*string{k.env: str(v)})
				if err := start(t); err != nil {
					t.Fatalf("%s = %s на границе отвергнута: %v", k.knob, v, err)
				}
			})
		}
	}
}

// Ручки на источник: значение вне границы, лишнее или недостающее поле, модуль
// перечня без записи и запись модуля вне перечня — отказ старта с именем ручки
// и предметом (модуль, поле, граница).
func TestConfig_NTF1H08_SourceLimitsOutOfBoundRefusesStart(t *testing.T) {
	const kaname = `"kaname":{"rate":10,"burst":20,"paused":false}`
	rec := func(probe string) *string { return str("{" + kaname + `,"probe":` + probe + "}") }
	cases := []struct {
		name string
		edit *string
		why  []string
	}{
		{"не задана", nil, []string{"не задана"}},
		{"пустая строка", str(""), nil},
		{"не JSON-объект", str(`[]`), nil},
		{"probe.rate = 0", rec(`{"rate":0,"burst":5,"paused":false}`), []string{"probe", "rate", "[1..1000]"}},
		{"probe.rate = 1001", rec(`{"rate":1001,"burst":5,"paused":false}`), []string{"probe", "rate", "[1..1000]"}},
		{"probe.burst = 0", rec(`{"rate":5,"burst":0,"paused":false}`), []string{"probe", "burst", "[1..10000]"}},
		{"probe.burst = 10001", rec(`{"rate":5,"burst":10001,"paused":false}`), []string{"probe", "burst", "[1..10000]"}},
		{"probe.paused не bool", rec(`{"rate":5,"burst":5,"paused":"yes"}`), []string{"probe", "paused"}},
		{"probe без rate", rec(`{"burst":5,"paused":false}`), []string{"probe", "rate"}},
		{"probe без paused", rec(`{"rate":5,"burst":5}`), []string{"probe", "paused"}},
		{"probe с лишним полем", rec(`{"rate":5,"burst":5,"paused":false,"ceiling":1}`), []string{"probe", "ceiling"}},
		{"модуль перечня без записи", str("{" + kaname + "}"), []string{"probe"}},
		{"запись модуля вне перечня", str("{" + kaname + `,"probe":{"rate":5,"burst":5,"paused":false},"vpc":{"rate":5,"burst":5,"paused":false}}`), []string{"vpc"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useFixture(t, map[string]*string{sourceLimitsEnv: c.edit})
			requireOnlyRefusal(t, start(t), sourceLimitsKnob, c.why...)
		})
	}
	for name, v := range map[string]*string{
		"близнец: нижние границы и пауза": rec(`{"rate":1,"burst":1,"paused":true}`),
		"близнец: верхние границы":        rec(`{"rate":1000,"burst":10000,"paused":false}`),
	} {
		t.Run(name, func(t *testing.T) {
			useFixture(t, map[string]*string{sourceLimitsEnv: v})
			if err := start(t); err != nil {
				t.Fatalf("ручки на источник в границах отвергнуты: %v", err)
			}
		})
	}
}
