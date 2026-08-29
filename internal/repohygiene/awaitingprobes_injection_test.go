// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import "testing"

// awaitingprobes_injection_test.go — доказательство того, что судья
// [judgeAwaitingProbes] СПОСОБЕН упасть и способен смолчать.
//
// Прогоняется ТА ЖЕ функция суждения, которой судит гейт, а не её пересказ:
// проверка, воспроизводящая цикл своей копией, доказывает свойство копии и
// остаётся зелёной при снятой судящей ветке (класс найден приёмкой 2026-08-29 на
// гейте поставки). Дерево при этом не трогается вовсе.
//
// Каждый случай снимает у входа РОВНО ОДНО свойство; остальные на месте.
func TestAwaitingProbeJudgeExpiresOnlyWhenBothHalvesAreCreated(t *testing.T) {
	// Словарь писанных видов синтетический и одинаков во всех случаях:
	// различие между ними обязано быть только во входе.
	served := map[string]struct{}{"compute_instance": {}, "vpc_network": {}}

	const askServed = "const u = `/subscription/v1/events?owner=${OWNER}&kinds=compute_instance`;"
	const askUnserved = "const u = `/subscription/v1/events?owner=${OWNER}&kinds=compute.placement_group`;"
	const askNothing = "test('поток', async () => { await open(); });"

	cases := []struct {
		name             string
		bodies           map[string]string
		ownerDeclared    bool
		wantExpired      []string
		wantUnserved     map[string][]string
		wantUnrecognized []string
	}{
		{
			name:          "инъекция: обе половины созданы — долг истёк",
			bodies:        map[string]string{"a.spec.ts": askServed},
			ownerDeclared: true,
			wantExpired:   []string{"a.spec.ts"},
		},
		{
			name:          "законный близнец: вид не пишет НИКТО — проба остаётся",
			bodies:        map[string]string{"a.spec.ts": askUnserved},
			ownerDeclared: true,
			wantExpired:   []string{},
			wantUnserved:  map[string][]string{"a.spec.ts": {"compute.placement_group"}},
		},
		{
			name:          "законный близнец: владелец не объявлен — проба остаётся",
			bodies:        map[string]string{"a.spec.ts": askServed},
			ownerDeclared: false,
			wantExpired:   []string{},
		},
		{
			name:             "форма не разобрана — это находка, а не «условие не создано»",
			bodies:           map[string]string{"a.spec.ts": askNothing},
			ownerDeclared:    true,
			wantExpired:      []string{},
			wantUnrecognized: []string{"a.spec.ts"},
		},
		{
			name:          "перечень пуст — это ЦЕЛЬ долга, а не поломка",
			bodies:        map[string]string{},
			ownerDeclared: true,
			wantExpired:   []string{},
		},
		{
			name: "одна проба истекла, соседняя ждёт — судятся порознь",
			bodies: map[string]string{
				"a.spec.ts": askServed,
				"b.spec.ts": askUnserved,
			},
			ownerDeclared: true,
			wantExpired:   []string{"a.spec.ts"},
			wantUnserved:  map[string][]string{"b.spec.ts": {"compute.placement_group"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := judgeAwaitingProbes(tc.bodies, tc.ownerDeclared, served)
			t.Logf("перепись: проб подано %d · владелец объявлен %v · писанных видов %d · "+
				"истекло %v · ждут вида %v · не разобрано %v",
				len(tc.bodies), tc.ownerDeclared, len(served),
				got.expired, got.unserved, got.unrecognized)

			if !sameStringSlice(got.expired, tc.wantExpired) {
				t.Fatalf("истекло %v, ожидалось %v — именно это суждение гейт и выносит",
					got.expired, tc.wantExpired)
			}
			if !sameStringSlice(got.unrecognized, tc.wantUnrecognized) {
				t.Fatalf("не разобрано %v, ожидалось %v", got.unrecognized, tc.wantUnrecognized)
			}
			if len(got.unserved) != len(tc.wantUnserved) {
				t.Fatalf("ждут вида %v, ожидалось %v", got.unserved, tc.wantUnserved)
			}
			for name, want := range tc.wantUnserved {
				if !sameStringSlice(got.unserved[name], want) {
					t.Fatalf("у пробы %s не пишутся виды %v, ожидалось %v",
						name, got.unserved[name], want)
				}
			}
		})
	}
}

// TestAwaitingProbeJudgeRefusesToBlessAnEmptyDictionary — предпосылка судьи.
//
// Пустой словарь писанных видов означает «сверять было не с чем»: если бы судья
// при нём объявлял долг истёкшим, гейт вытолкнул бы пробы в исполняемый набор от
// того, что перестал читать страницу.
func TestAwaitingProbeJudgeRefusesToBlessAnEmptyDictionary(t *testing.T) {
	got := judgeAwaitingProbes(
		map[string]string{"a.spec.ts": "kinds=compute_instance"}, true, map[string]struct{}{})
	t.Logf("перепись: писанных видов 0 · проб 1 · истекло %v · ждут вида %v",
		got.expired, got.unserved)
	if len(got.expired) != 0 {
		t.Fatalf("судья объявил долг истёкшим при ПУСТОМ словаре видов (%v): тогда "+
			"«условие создано» означало бы «страница не прочитана»", got.expired)
	}
}

// sameStringSlice — поэлементное совпадение двух перечней имён.
func sameStringSlice(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
