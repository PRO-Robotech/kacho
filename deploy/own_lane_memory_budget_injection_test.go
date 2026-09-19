// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_memory_budget_injection_test.go — доказательство того, что сверка
// бюджета полосы СПОСОБНА упасть и способна смолчать.
//
// Вход СИНТЕТИЧЕСКИЙ: подделка дерева трогает общий клон, а вердикт обязан
// доказываться на входе, построенном здесь и целиком видном читателю.
//
// Каждый случай меняет РОВНО ОДИН факт против законного близнеца. Величина
// памяти одной проверки подаётся аргументом, поэтому оси не зависят ни от пина,
// ни от кэша модулей: предмет здесь — АРИФМЕТИКА и ветвление, а не потолок.
package deploy_test

import (
	"strings"
	"testing"
)

// injPerCheck — память одной проверки в осях ниже. Круглое число выбрано
// намеренно: арифметику отказа должно быть видно глазом.
const injPerCheck uint64 = 100

// legalBudgetStack — законный близнец: предел ровно покрывает бюджет.
// 8 × 100 + 200 = 1000.
func legalBudgetStack() laneBudgetFacts {
	return laneBudgetFacts{
		Stack: "стенд", Posture: "external",
		Capacity: 8, Reserve: 200, Limit: 1000,
		Declared: true, HasLimit: true,
	}
}

func TestLaneMemoryBudgetJudgement_CanFailAndStaysSilent(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(f *laneBudgetFacts)
		want    int
		mustSay string
	}{
		{
			name:   "законный близнец: предел РОВНО покрывает бюджет — молчит",
			mutate: func(*laneBudgetFacts) {},
			want:   0,
		},
		{
			name:    "предел на байт меньше бюджета — находка",
			mutate:  func(f *laneBudgetFacts) { f.Limit = 999 },
			want:    1,
			mustSay: "ПРЕВЫШАЕТ предел памяти контейнера",
		},
		{
			name:    "предел не объявлен, числа полосы объявлены — находка",
			mutate:  func(f *laneBudgetFacts) { f.HasLimit = false },
			want:    1,
			mustSay: "resources.limits.memory",
		},
		{
			name:   "предел ВЫШЕ бюджета — НЕ находка: сверка судит достаточность, а не равенство",
			mutate: func(f *laneBudgetFacts) { f.Limit = 4096 },
			want:   0,
		},
		{
			// ЗАКОННЫЙ БЛИЗНЕЦ: стенд, не объявивший чисел полосы и стоящий не на
			// `own`, предметом не является — требовать от него предела значило бы
			// краснеть на исправном дереве.
			name: "ни чисел полосы, ни посадки own — НЕ находка",
			mutate: func(f *laneBudgetFacts) {
				f.Declared, f.HasLimit = false, false
			},
			want: 0,
		},
		{
			name: "посадка own БЕЗ чисел полосы — находка: стражу нечем сверять",
			mutate: func(f *laneBudgetFacts) {
				f.Posture, f.Declared = "own", false
			},
			want:    1,
			mustSay: "ёмкость и резерв полосы не объявлены",
		},
		{
			name: "посадка own без предела — находка, хотя на external тот же стенд молчал бы",
			mutate: func(f *laneBudgetFacts) {
				f.Posture, f.HasLimit = "own", false
			},
			want:    1,
			mustSay: "предел памяти средой не наложен",
		},
		{
			name:    "непозитивная ёмкость — находка ДО арифметики",
			mutate:  func(f *laneBudgetFacts) { f.Capacity = 0 },
			want:    1,
			mustSay: "непозитивной",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := legalBudgetStack()
			c.mutate(&f)
			findings, census := judgeLaneMemoryBudget([]laneBudgetFacts{f}, injPerCheck)
			if len(findings) != c.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(findings), c.want, findings)
			}
			if c.mustSay != "" {
				var said bool
				for _, got := range findings {
					if strings.Contains(got, c.mustSay) {
						said = true
					}
				}
				if !said {
					t.Errorf("ни одна находка не называет %q — оператор не узнает причину: %v",
						c.mustSay, findings)
				}
			}
			if census.Stacks != 1 {
				t.Errorf("перепись осмотренного %d, подан 1 — «находок ноль» стало бы "+
					"неотличимо от «прочитано ноль»", census.Stacks)
			}
		})
	}
}

// TestLaneMemoryBudgetJudgement_NamesTheNeededNumber — отказ обязан называть
// величину, до которой поднимать предел. Отказ без неё восстанавливает не
// следующий шаг, а лишь факт неудачи.
func TestLaneMemoryBudgetJudgement_NamesTheNeededNumber(t *testing.T) {
	f := legalBudgetStack()
	f.Limit = 1
	findings, _ := judgeLaneMemoryBudget([]laneBudgetFacts{f}, injPerCheck)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1", len(findings))
	}
	if !strings.Contains(findings[0], "Поднимите предел до 1000 байт") {
		t.Errorf("отказ не называет требуемой величины: %s", findings[0])
	}
}
