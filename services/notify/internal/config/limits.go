// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
)

// Ручки лимитов Р10 (замысел З24, §8; приёмка NTF1-H08). Границы — пакета
// limits: одно место чисел на страж и на сетку.

// Grid — сетка на адресата и потолок потока; годна только после успешного
// [Config.Validate].
func (c Config) Grid() limits.Grid {
	return limits.Grid{
		SecurityPerDay: c.LimitSecurityPerDay,
		NoticePerHour:  c.LimitNoticePerHour,
		NoticePerDay:   c.LimitNoticePerDay,
		GlobalPerDay:   c.LimitGlobalPerDay,
	}
}

// validateGrid судит ручки сетки и потолка по границам §8.
func (c *Config) validateGrid(fs *findings) {
	c.checkInt(fs, "LimitSecurityPerDay", c.LimitSecurityPerDay, limits.SecurityPerDayMin, limits.SecurityPerDayMax)
	c.checkInt(fs, "LimitNoticePerHour", c.LimitNoticePerHour, limits.NoticePerHourMin, limits.NoticePerHourMax)
	c.checkInt(fs, "LimitNoticePerDay", c.LimitNoticePerDay, limits.NoticePerDayMin, limits.NoticePerDayMax)
	c.checkInt(fs, "LimitGlobalPerDay", c.LimitGlobalPerDay, limits.GlobalPerDayMin, limits.GlobalPerDayMax)
}

// checkInt судит целое по границе. Незаданная уже названа общим перебором.
func (c *Config) checkInt(fs *findings, field string, v, lo, hi int) {
	k := KnobOfField(field)
	if c.unset[k.Env] {
		return
	}
	if b := (limits.Bound{Knob: k.Name, Min: lo, Max: hi}); !b.Contains(v) {
		fs.add(k, "значение %d вне границы %s", v, b)
	}
}

// sourceLimitFields — поля записи ручек на источник, все обязательные.
var sourceLimitFields = []string{"rate", "burst", "paused"}

// validateSourceLimits разбирает `KACHO_NOTIFY_SOURCE_LIMITS` строго:
// JSON-объект «модуль → {rate, burst, paused}». Лишнее или недостающее поле,
// значение вне границы, модуль перечня источников без записи и запись модуля
// вне перечня — отказ старта; умолчания у модуля без записи нет. Находки
// ручки сводятся в одну — с модулем и полем каждой причины.
//
// Разобранные значения страж не хранит: читателя у них в этом процессе пока
// нет — ворота источника ([limits.NewSourceGate]) собирает цикл `Claim`, и
// аксессор заводится вместе с ним.
func (c *Config) validateSourceLimits(fs *findings) {
	k := KnobOfField("SourceLimits")
	if c.unset[k.Env] {
		return
	}
	if strings.TrimSpace(c.SourceLimits) == "" {
		fs.add(k, "значение пусто: ручки на источник — JSON-объект «модуль → {rate, burst, paused}»")
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(c.SourceLimits), &raw); err != nil || raw == nil {
		fs.add(k, "ручки на источник не разбираются как JSON-объект «модуль → {rate, burst, paused}»")
		return
	}
	modules := make([]string, 0, len(raw))
	for m := range raw {
		modules = append(modules, m)
	}
	sort.Strings(modules)

	var problems []string
	for _, m := range modules {
		for _, p := range parseSourceLimits(raw[m]) {
			problems = append(problems, fmt.Sprintf("модуль %q: %s", m, p))
		}
	}
	// Перечень источников разобран без находок — тогда и только тогда записи
	// сверяются с ним; иначе его находки уже названы своей ручкой.
	if roster := c.sources; len(roster) > 0 {
		inRoster := map[string]bool{}
		for _, s := range roster {
			inRoster[s.Module] = true
			if _, ok := raw[s.Module]; !ok {
				problems = append(problems, fmt.Sprintf("модуль %q перечня источников (%s) без записи — "+
					"умолчания у ручек на источник нет", s.Module, KnobOfField("Sources")))
			}
		}
		for _, m := range modules {
			if !inRoster[m] {
				problems = append(problems, fmt.Sprintf("запись модуля %q вне перечня источников (%s)",
					m, KnobOfField("Sources")))
			}
		}
	}
	if len(problems) > 0 {
		fs.add(k, "%s", strings.Join(problems, "; "))
	}
}

// parseSourceLimits судит запись одного модуля: находки с именем поля.
func parseSourceLimits(rec json.RawMessage) []string {
	var (
		problems []string
		fields   map[string]json.RawMessage
	)
	if err := json.Unmarshal(rec, &fields); err != nil || fields == nil {
		return []string{"запись не JSON-объект {rate, burst, paused}"}
	}
	extra := make([]string, 0)
	for f := range fields {
		if !slices.Contains(sourceLimitFields, f) {
			extra = append(extra, f)
		}
	}
	sort.Strings(extra)
	for _, f := range extra {
		problems = append(problems, fmt.Sprintf("лишнее поле %q — у записи только %s", f, strings.Join(sourceLimitFields, ", ")))
	}
	present := func(f string) (json.RawMessage, bool) {
		v, ok := fields[f]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			problems = append(problems, fmt.Sprintf("нет поля %q", f))
			return nil, false
		}
		return v, true
	}
	intIn := func(f string, b limits.Bound) {
		v, ok := present(f)
		if !ok {
			return
		}
		var n int
		if err := json.Unmarshal(v, &n); err != nil {
			problems = append(problems, fmt.Sprintf("поле %q не целое", f))
			return
		}
		if !b.Contains(n) {
			problems = append(problems, fmt.Sprintf("поле %q = %d вне границы %s", f, n, b))
		}
	}
	intIn("rate", limits.RateBound())
	intIn("burst", limits.BurstBound())
	if v, ok := present("paused"); ok {
		var paused bool
		if err := json.Unmarshal(v, &paused); err != nil {
			problems = append(problems, fmt.Sprintf("поле %q не bool", "paused"))
		}
	}
	return problems
}
