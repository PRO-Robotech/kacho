// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package limits_test

// source_test.go — ведро и пауза источника, классы `Claim` и инвариант сетки
// `security` (полоса N7, kacho#2915; приёмка NTF1-H04…H07; замысел З24). Базы
// эти пробы не просят: ведро живёт в памяти реплики, пауза — значение
// установки, инвариант — страж старта.

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/notify/feed"

	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
)

func newGate(t *testing.T, module string, sl limits.SourceLimits, clk *clock, reg prometheus.Registerer) *limits.SourceGate {
	t.Helper()
	g, err := limits.NewSourceGate(module, sl, clk.Now, reg)
	if err != nil {
		t.Fatalf("ворота источника %s не собраны на годных ручках %+v: %v", module, sl, err)
	}
	return g
}

// NTF1-H04 — ведро источника задерживает, но не теряет: rate = 5, burst = 5,
// 50 строк — все выданы за конечное число тактов управляемых часов, ни одна не
// потеряна, `notify_source_throttled_total{source="probe"}` > 0. Близнец — 5
// строк: ведро не срабатывает.
func TestLimits_NTF1H04_SourceBucketDelaysButDoesNotLose(t *testing.T) {
	for _, rows := range []int{50, 5} {
		t.Run(map[int]string{50: "50 строк", 5: "близнец: 5 строк"}[rows], func(t *testing.T) {
			clk := newClock(noon)
			reg := prometheus.NewRegistry()
			g := newGate(t, "probe", limits.SourceLimits{Rate: 5, Burst: 5}, clk, reg)
			left, ticks := rows, 0
			for left > 0 {
				got := g.Take(left)
				if got < 0 || got > left {
					t.Fatalf("Take(%d) = %d — вне [0..%d]", left, got, left)
				}
				left -= got
				if left > 0 {
					clk.Advance(time.Second)
					ticks++
				}
				if ticks > rows {
					t.Fatalf("за %d тактов выдано %d из %d — ведро теряет строки", ticks, rows-left, rows)
				}
			}
			throttled := counter(t, reg, "notify_source_throttled_total", map[string]string{"source": "probe"})
			if rows == 50 {
				if throttled <= 0 {
					t.Fatalf("50 строк при rate=5, burst=5 выданы без сдерживания (throttled=%v)", throttled)
				}
				// 5 сразу и по 5 за секунду: 9 тактов, а не 49.
				if ticks > 10 {
					t.Fatalf("50 строк выданы за %d тактов по 1 с при rate=5 — ведро держит сильнее темпа", ticks)
				}
			} else if throttled != 0 || ticks != 0 {
				t.Fatalf("близнец: 5 строк при burst=5 сдержаны (throttled=%v, тактов %d)", throttled, ticks)
			}
		})
	}
}

// NTF1-H05 (классы) и NTF1-H06 — какие классы забирает `Claim`: потолок потока
// достигнут или источник на паузе — только `security`; иначе — оба. Пауза
// одного источника не трогает другого. `notify_source_paused{source}` = 1 у
// приостановленного и 0 у прочих.
func TestLimits_NTF1H06_PausedSourceAndCeilingClaimOnlySecurity(t *testing.T) {
	clk := newClock(noon)
	reg := prometheus.NewRegistry()
	probe := newGate(t, "probe", limits.SourceLimits{Rate: 5, Burst: 5, Paused: true}, clk, reg)
	kaname := newGate(t, "kaname", limits.SourceLimits{Rate: 5, Burst: 5}, clk, reg)
	only := []feed.Class{feed.ClassSecurity}
	both := []feed.Class{feed.ClassSecurity, feed.ClassNotice}
	same := func(got, want []feed.Class) bool {
		g, w := slices.Clone(got), slices.Clone(want)
		slices.Sort(g)
		slices.Sort(w)
		return slices.Equal(g, w)
	}
	if got := probe.Classes(false); !same(got, only) {
		t.Fatalf("probe на паузе: Claim(classes=%v), ожидалось %v", got, only)
	}
	if got := kaname.Classes(false); !same(got, both) {
		t.Fatalf("kaname не на паузе (близнец паузы): Claim(classes=%v), ожидалось %v", got, both)
	}
	if got := kaname.Classes(true); !same(got, only) {
		t.Fatalf("потолок достигнут: kaname Claim(classes=%v), ожидалось %v", got, only)
	}
	if got := counter(t, reg, "notify_source_paused", map[string]string{"source": "probe"}); got != 1 {
		t.Fatalf("notify_source_paused{source=probe} = %v, ожидалось 1", got)
	}
	if got := counter(t, reg, "notify_source_paused", map[string]string{"source": "kaname"}); got != 0 {
		t.Fatalf("notify_source_paused{source=kaname} = %v, ожидалось 0", got)
	}
}

// Ручки на источник вне границы §8 ворота не собирают: rate [1..1000], burst
// [1..10000]. Близнец — обе границы включены.
func TestLimits_NTF1H08_SourceGateRefusesOutOfBoundKnobs(t *testing.T) {
	clk := newClock(noon)
	for _, sl := range []limits.SourceLimits{{Rate: 0, Burst: 5}, {Rate: 1001, Burst: 5}, {Rate: 5, Burst: 0}, {Rate: 5, Burst: 10001}} {
		if _, err := limits.NewSourceGate("probe", sl, clk.Now, prometheus.NewRegistry()); err == nil {
			t.Errorf("ручки %+v вне границы приняты", sl)
		}
	}
	for _, sl := range []limits.SourceLimits{{Rate: 1, Burst: 1}, {Rate: 1000, Burst: 10000}} {
		newGate(t, "probe", sl, clk, prometheus.NewRegistry())
	}
}

// NTF1-H07 — инвариант «сетка security ≥ 1,25 × сумма суточных limits шаблонов
// security сборки»: сумма 27, сетка 33 — отказ с именем ручки и текстом
// «сетка security 33 меньше 1,25 × 27»; близнец — сетка 34 принята.
func TestLimits_NTF1H07_SecurityNetMustCoverTheBundle(t *testing.T) {
	err := limits.SecurityNetCovers(33, 27)
	if err == nil {
		t.Fatal("сетка security 33 при сумме limits 27 принята (1,25 × 27 = 33,75)")
	}
	for _, want := range []string{"сетка security 33 меньше 1,25 × 27", "notify.limits.recipient.security.perDay"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("отказ инварианта не несёт %q: %v", want, err)
		}
	}
	if err := limits.SecurityNetCovers(34, 27); err != nil {
		t.Fatalf("близнец: сетка 34 при сумме 27 отвергнута: %v", err)
	}
}
