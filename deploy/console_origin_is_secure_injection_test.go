// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_origin_is_secure_injection_test.go — доказательство того, что сверка
// происхождения консоли СПОСОБНА упасть и способна смолчать (kacho#3024).
//
// Две оси: судья на синтетике (каждый случай меняет РОВНО ОДИН факт против
// законного близнеца) и НАСТОЯЩИЙ вход — разобранные значения цепочек дерева,
// в одну из которых вносится одна правка.
package deploy_test

import (
	"strings"
	"testing"
)

func TestConsoleOriginJudgement_CanFailAndStaysSilent(t *testing.T) {
	debt := plainHTTPOriginDebt{Origin: "http://stand.example:28080", Issue: "#0", Reason: "проба"}
	legal := func() []consoleOriginFacts {
		return []consoleOriginFacts{
			{Stack: "edge", Origin: "https://console.example", Declared: true},
			{Stack: "kind", Origin: debt.Origin, Declared: true},
		}
	}
	cases := []struct {
		name    string
		mutate  func(f []consoleOriginFacts) []consoleOriginFacts
		debts   []plainHTTPOriginDebt
		want    int
		mustSay string
	}{
		{name: "законный близнец: https и http по записи — молчит", mutate: func(f []consoleOriginFacts) []consoleOriginFacts { return f }},
		{
			name: "http на IP-литерале без записи — находка",
			mutate: func(f []consoleOriginFacts) []consoleOriginFacts {
				f[0].Origin = "http://192.0.2.10"
				return f
			},
			want:    1,
			mustSay: "FORM_TOKEN_REJECTED",
		},
		{
			name: "http того же имени, другой порт — находка: запись судит происхождение дословно",
			mutate: func(f []consoleOriginFacts) []consoleOriginFacts {
				f[1].Origin = "http://stand.example:28081"
				return f
			},
			// находка о самой цепочке и о записи, ставшей ничьей
			want:    2,
			mustSay: "нечего исключать",
		},
		{
			name: "запись, которой не пользуется ни одна цепочка, — находка",
			mutate: func(f []consoleOriginFacts) []consoleOriginFacts {
				f[1].Origin = "https://stand.example"
				return f
			},
			want:    1,
			mustSay: "нечего исключать",
		},
		{
			name:    "без записи http стенда — находка",
			mutate:  func(f []consoleOriginFacts) []consoleOriginFacts { return f },
			debts:   []plainHTTPOriginDebt{},
			want:    1,
			mustSay: "открытый http",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			debts := c.debts
			if debts == nil {
				debts = []plainHTTPOriginDebt{debt}
			}
			findings, census := judgeConsoleOrigins(c.mutate(legal()), debts)
			if len(findings) != c.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(findings), c.want, findings)
			}
			if c.mustSay != "" && !strings.Contains(strings.Join(findings, "\n"), c.mustSay) {
				t.Errorf("ни одна находка не называет %q: %v", c.mustSay, findings)
			}
			if census.Stacks != 2 {
				t.Errorf("перепись осмотренного %d, подано 2", census.Stacks)
			}
		})
	}
}

// TestConsoleOrigin_TreeInjectionNamesTheChain — НАСТОЯЩИЙ вход: цепочки
// дерева, в одну из которых внесено происхождение по открытому http. Инъекция
// краснеет и называет цепочку; законный близнец — то же дерево без правки —
// молчит.
func TestConsoleOrigin_TreeInjectionNamesTheChain(t *testing.T) {
	base := readConsoleOriginFacts(t, nil)
	if f, _ := judgeConsoleOrigins(base, plainHTTPOriginDebts); len(f) != 0 {
		t.Fatalf("законный близнец (дерево без правки) не молчит — инъекции не с чем сравнить: %v", f)
	}
	var target string
	for _, f := range base {
		if strings.HasPrefix(f.Origin, "https://") {
			target = f.Stack
			break
		}
	}
	if target == "" {
		t.Fatal("в дереве нет цепочки с https-происхождением — инъекции некуда попасть")
	}
	inject := func(stack string, declared map[string]any) {
		if stack != target {
			return
		}
		id := declared["global"].(map[string]any)["kacho"].(map[string]any)["identity"].(map[string]any)
		id["appBaseURL"] = "http://192.0.2.10"
	}
	findings, _ := judgeConsoleOrigins(readConsoleOriginFacts(t, inject), plainHTTPOriginDebts)
	if len(findings) != 1 {
		t.Fatalf("инъекция в цепочку %s дала %d находок, ожидалась одна: %v", target, len(findings), findings)
	}
	if !strings.Contains(findings[0], "цепочка "+target+":") {
		t.Errorf("находка не называет цепочку %s: %s", target, findings[0])
	}
}

// TestConsoleOriginJudgement_StackKeyedDebtCoversOnlyItsChain — запись по
// имени цепочки (её адрес в сверку не копируется) прощает http РОВНО этой
// цепочки. Каждый случай меняет один факт против законного близнеца.
func TestConsoleOriginJudgement_StackKeyedDebtCoversOnlyItsChain(t *testing.T) {
	debt := plainHTTPOriginDebt{Stack: "managed", Issue: "#0", Reason: "проба"}
	legal := func() []consoleOriginFacts {
		return []consoleOriginFacts{
			{Stack: "edge", Origin: "https://console.example", Declared: true},
			{Stack: "managed", Origin: "http://192.0.2.20", Declared: true},
		}
	}
	cases := []struct {
		name    string
		mutate  func(f []consoleOriginFacts) []consoleOriginFacts
		debts   []plainHTTPOriginDebt
		want    int
		mustSay string
	}{
		{name: "законный близнец: http своей цепочки по записи — молчит", mutate: func(f []consoleOriginFacts) []consoleOriginFacts { return f }},
		{
			name: "та же цепочка, другой адрес по http — молчит: адрес живёт в профиле, не в записи",
			mutate: func(f []consoleOriginFacts) []consoleOriginFacts {
				f[1].Origin = "http://192.0.2.21"
				return f
			},
		},
		{
			name: "другая цепочка по http — находка: запись не переносится на соседа",
			mutate: func(f []consoleOriginFacts) []consoleOriginFacts {
				f[0].Origin = "http://192.0.2.20"
				return f
			},
			want:    1,
			mustSay: "цепочка edge:",
		},
		{
			name: "цепочка записи перешла на https — запись ничья, находка",
			mutate: func(f []consoleOriginFacts) []consoleOriginFacts {
				f[1].Origin = "https://console.managed.example"
				return f
			},
			want:    1,
			mustSay: "нечего исключать",
		},
		{
			name:    "запись с обоими ключами — находка формы, цепочка не прощена",
			mutate:  func(f []consoleOriginFacts) []consoleOriginFacts { return f },
			debts:   []plainHTTPOriginDebt{{Stack: "managed", Origin: "http://192.0.2.20", Issue: "#0"}},
			want:    2,
			mustSay: "ровно одним ключом",
		},
		{
			name:    "запись без задачи — находка, цепочка не прощена",
			mutate:  func(f []consoleOriginFacts) []consoleOriginFacts { return f },
			debts:   []plainHTTPOriginDebt{{Stack: "managed"}},
			want:    2,
			mustSay: "не называет задачу",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			debts := c.debts
			if debts == nil {
				debts = []plainHTTPOriginDebt{debt}
			}
			findings, census := judgeConsoleOrigins(c.mutate(legal()), debts)
			if len(findings) != c.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(findings), c.want, findings)
			}
			if c.mustSay != "" && !strings.Contains(strings.Join(findings, "\n"), c.mustSay) {
				t.Errorf("ни одна находка не называет %q: %v", c.mustSay, findings)
			}
			if census.Stacks != 2 {
				t.Errorf("перепись осмотренного %d, подано 2", census.Stacks)
			}
		})
	}
}
