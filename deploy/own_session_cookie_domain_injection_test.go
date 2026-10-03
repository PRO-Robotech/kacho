// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_session_cookie_domain_injection_test.go — доказательство того, что сверка
// домена печенья нашей сессии на посадке `own` СПОСОБНА упасть и способна
// смолчать (kacho#2810, п. 1; kacho#2818).
//
// Две оси: судья на синтетике (каждый случай меняет РОВНО ОДИН факт против
// законного близнеца) и НАСТОЯЩИЙ вход — разобранные значения цепочек дерева,
// в которые вносится одна правка.
package deploy_test

import (
	"strings"
	"testing"
)

func TestOwnCookieDomainJudgement_CanFailAndStaysSilent(t *testing.T) {
	legal := func() cookieDomainFacts {
		return cookieDomainFacts{Stack: "own", CookieDomain: "none", Declared: true}
	}
	cases := []struct {
		name    string
		mutate  func(f *cookieDomainFacts)
		want    int
		mustSay string
	}{
		{name: "законный близнец: own и `none` — молчит", mutate: func(*cookieDomainFacts) {}},
		{
			name:    "own и доменное имя — находка",
			mutate:  func(f *cookieDomainFacts) { f.CookieDomain = "console.example" },
			want:    1,
			mustSay: "выход ответит успехом и сессию НЕ закроет",
		},
		{
			name:    "own и ручка не объявлена — находка",
			mutate:  func(f *cookieDomainFacts) { f.CookieDomain, f.Declared = "", false },
			want:    1,
			mustSay: "не объявлен",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := legal()
			c.mutate(&f)
			findings, census := judgeOwnCookieDomain([]cookieDomainFacts{f})
			if len(findings) != c.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(findings), c.want, findings)
			}
			if c.mustSay != "" && !strings.Contains(strings.Join(findings, "\n"), c.mustSay) {
				t.Errorf("ни одна находка не называет %q: %v", c.mustSay, findings)
			}
			if census.Stacks != 1 {
				t.Errorf("перепись осмотренного %d, подана 1", census.Stacks)
			}
		})
	}
}

// TestOwnCookieDomain_TreeInjectionNamesTheChainAndTheKey — НАСТОЯЩИЙ вход:
// цепочки дерева, в одну из которых внесён `cookieDomain` ≠ `none`. Инъекция
// краснеет и называет цепочку и ключ; законный близнец — то же дерево без
// правки — молчит. Прежде близнецом служила та же цепочка, переведённая в
// памяти на посадку `external`; второй посадки у службы нет (kaname#363), и
// близнец отличается от инъекции ровно внесённой величиной.
func TestOwnCookieDomain_TreeInjectionNamesTheChainAndTheKey(t *testing.T) {
	base := readCookieDomainFacts(t, nil)
	var target string
	for _, f := range base {
		if f.Declared && target == "" {
			target = f.Stack
		}
	}
	if target == "" {
		t.Fatalf("в дереве нет цепочки с объявленной ручкой — инъекции некуда попасть")
	}
	inject := func(stack string, declared map[string]any) {
		if stack != target {
			return
		}
		login, ok := lookup(declared, cookieDomainKey[:len(cookieDomainKey)-1]...)
		m, isMap := login.(map[string]any)
		if !ok || !isMap {
			t.Fatalf("цепочка %s: блока `kaname.config.authn.login` нет — инъекция не внесена", stack)
		}
		m["cookieDomain"] = "console.example"
	}

	red, census := judgeOwnCookieDomain(readCookieDomainFacts(t, inject))
	t.Logf("инъекция в цепочку %s: находок %d · %s", target, len(red), census)
	if len(red) != 1 {
		t.Fatalf("инъекция дала %d находок, ожидалась 1: %v", len(red), red)
	}
	for _, must := range []string{"цепочка " + target, "kaname.config.authn.login.cookieDomain", `"console.example"`} {
		if !strings.Contains(red[0], must) {
			t.Errorf("находка не называет %q: %s", must, red[0])
		}
	}

	silent, census := judgeOwnCookieDomain(base)
	t.Logf("близнец — дерево без правки: находок %d · %s", len(silent), census)
	if len(silent) != 0 {
		t.Errorf("дерево без правки дало находки: %v", silent)
	}
}
