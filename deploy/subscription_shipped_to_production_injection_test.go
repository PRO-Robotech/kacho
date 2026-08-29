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
func TestShippingGateSeesTheDarkProductionStack(t *testing.T) {
	const base = "compute,vpc"
	stacks := map[string][]string{
		"dev":  {"values.dev.yaml"},
		"prod": {"values.prod.yaml"},
	}

	// Инъекция: боевой профиль ВЫКЛЮЧАЕТ то, что стенд оставляет включённым.
	darkened := map[string]string{"values.prod.yaml": ""}
	// Законный близнец: выключено на СТЕНДЕ, бой наследует умолчание.
	twin := map[string]string{"values.dev.yaml": ""}

	for _, tc := range []struct {
		name      string
		declared  map[string]string
		wantDark  bool
		wantAnyOn bool
	}{
		{name: "боевой стек затемнён", declared: darkened, wantDark: true, wantAnyOn: true},
		{name: "законный близнец: затемнён стенд", declared: twin, wantDark: false, wantAnyOn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			declaredBy := func(profile string) (string, bool) {
				value, ok := tc.declared[profile]
				return value, ok
			}
			anyOn, dark := false, false
			for name, chain := range stacks {
				owners := countOwners(effectiveOwners(base, chain, declaredBy))
				if owners > 0 {
					anyOn = true
				}
				if name == "prod" && owners == 0 {
					dark = true
				}
			}
			t.Logf("перепись: стеков %d · включено где-либо %v · боевой затемнён %v",
				len(stacks), anyOn, dark)
			if anyOn != tc.wantAnyOn || dark != tc.wantDark {
				t.Fatalf("включено=%v затемнён=%v, ожидалось включено=%v затемнён=%v",
					anyOn, dark, tc.wantAnyOn, tc.wantDark)
			}
		})
	}
}
