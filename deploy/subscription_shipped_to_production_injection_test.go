// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

import "testing"

// subscription_shipped_to_production_injection_test.go — доказательство того, что
// наложение профилей вычисляется ВЕРНО и что гейт поставки способен упасть.
//
// Прогоняются ТЕ ЖЕ функции ([effectiveOwners], [countOwners]), а не их пересказ:
// инъекция, воспроизводящая логику своей копией, доказывает свойство копии.
// Дерево и профили при этом не трогаются вовсе.
func TestEffectiveOwnersFollowsTheOrderHelmApplies(t *testing.T) {
	// Умолчание чарта края непусто — то состояние, в котором возможность
	// поставлена. Различие между случаями обязано быть только в профилях.
	//
	// Имена слоёв СИНТЕТИЧЕСКИЕ и намеренно не совпадают с профилями умбреллы:
	// пара реальных имён рядом читается как вторая копия цепочки стенда, а
	// состав стенда объявляет только stacks.txt (TestNoSecondCopyOfAStackChain).
	// Предмет здесь — порядок наложения, и он от имён не зависит.
	const base = "compute,vpc"

	cases := []struct {
		name      string
		chain     []string
		declared  map[string]string // профиль → объявленное им значение
		wantCount int
	}{
		{
			name:      "профиль о ключе не высказался — действует умолчание",
			chain:     []string{"values.prod.yaml"},
			declared:  map[string]string{},
			wantCount: 2,
		},
		{
			name:      "профиль ВЫКЛЮЧИЛ возможность пустым значением",
			chain:     []string{"values.prod.yaml"},
			declared:  map[string]string{"values.prod.yaml": ""},
			wantCount: 0,
		},
		{
			name:      "последний слой выигрывает у предыдущего",
			chain:     []string{"layer-one.yaml", "layer-two.yaml"},
			declared:  map[string]string{"layer-one.yaml": "", "layer-two.yaml": "vpc"},
			wantCount: 1,
		},
		{
			name:      "вырожденное значение непусто строкой и НОЛЬ имён",
			chain:     []string{"values.prod.yaml"},
			declared:  map[string]string{"values.prod.yaml": " , , "},
			wantCount: 0,
		},
		{
			name:      "необъявившийся хвост не отменяет объявившую голову",
			chain:     []string{"layer-one.yaml", "layer-two.yaml"},
			declared:  map[string]string{"layer-one.yaml": "vpc,compute,storage"},
			wantCount: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			declaredBy := func(profile string) (string, bool) {
				value, ok := tc.declared[profile]
				return value, ok
			}
			got := countOwners(effectiveOwners(base, tc.chain, declaredBy))
			t.Logf("перепись: слоёв %d · объявили %d · владельцев на выходе %d",
				len(tc.chain), len(tc.declared), got)
			if got != tc.wantCount {
				t.Fatalf("владельцев %d, ожидалось %d (цепочка %v, объявления %v)",
					got, tc.wantCount, tc.chain, tc.declared)
			}
		})
	}
}

// TestShippingGateSeesTheDarkProductionStack — суждение «включено на стенде,
// выключено в бою» находится, а его законный близнец — нет.
//
// Свести оба случая в один прогон обязательно: гейт, который краснеет всегда,
// снимут первым, а гейт, который молчит всегда, не заметят вовсе.
//
// # Прогоняется САМО суждение, а не его пересказ
//
// Здесь зовётся [takeShippingCensus] — та же функция, которой судит гейт.
// Прежняя редакция этой пробы воспроизводила цикл суждения СВОЕЙ копией: она
// оставалась зелёной, когда судящую ветку настоящего гейта снимали, потому что
// доказывала свойство копии. Опыт, которым это найдено (приёмка 2026-08-29):
// заменить в гейте `if owners == 0` на `if owners < 0` и прогнать эту пробу —
// она обязана покраснеть.
func TestShippingGateSeesTheDarkProductionStack(t *testing.T) {
	const base = "compute,vpc"

	// Боевым стек делает ПЕРВЫЙ профиль цепочки — то же правило, по которому
	// судит гейт, и берётся оно из той же константы, а не из второго написания.
	// Имя стенда синтетическое: пара реальных имён профилей рядом читается как
	// вторая копия цепочки стенда, а состав стенда объявляет только stacks.txt
	// (TestNoSecondCopyOfAStackChain).
	const standProfile = "layer-stand.yaml"
	stacks := map[string][]string{
		"stand": {standProfile},
		"live":  {productionBaseProfile},
	}

	// Инъекция: боевой профиль ВЫКЛЮЧАЕТ то, что стенд оставляет включённым.
	darkened := map[string]string{productionBaseProfile: ""}
	// Законный близнец: выключено на СТЕНДЕ, бой наследует умолчание.
	twin := map[string]string{standProfile: ""}

	for _, tc := range []struct {
		name           string
		declared       map[string]string
		wantEnabled    []string
		wantProduction []string
		wantDark       []string
	}{
		{
			name:           "боевой стек затемнён",
			declared:       darkened,
			wantEnabled:    []string{"stand"},
			wantProduction: []string{"live"},
			wantDark:       []string{"live"},
		},
		{
			name:           "законный близнец: затемнён стенд",
			declared:       twin,
			wantEnabled:    []string{"live"},
			wantProduction: []string{"live"},
			wantDark:       []string{},
		},
		{
			name:           "никто не выключал — включены оба",
			declared:       map[string]string{},
			wantEnabled:    []string{"live", "stand"},
			wantProduction: []string{"live"},
			wantDark:       []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			declaredBy := func(profile string) (string, bool) {
				value, ok := tc.declared[profile]
				return value, ok
			}
			census := takeShippingCensus(stacks, base, declaredBy)
			t.Logf("перепись: стеков %d %v · включено %v · боевых %v · затемнённых боевых %v",
				len(census.stacks), census.stacks, census.enabled, census.production, census.dark)

			if !sameNames(census.enabled, tc.wantEnabled) {
				t.Fatalf("включено %v, ожидалось %v", census.enabled, tc.wantEnabled)
			}
			if !sameNames(census.production, tc.wantProduction) {
				t.Fatalf("боевых %v, ожидалось %v", census.production, tc.wantProduction)
			}
			if !sameNames(census.dark, tc.wantDark) {
				t.Fatalf("затемнённых боевых %v, ожидалось %v — именно это суждение "+
					"гейт и выносит; проба, воспроизводящая его своей копией, "+
					"осталась бы зелёной при снятой судящей ветке", census.dark, tc.wantDark)
			}
		})
	}
}

// sameNames — совпадение двух перечней имён поэлементно.
func sameNames(got, want []string) bool {
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
