// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_session_cookie_domain_injection_test.go — доказательство того, что сверка
// домена печенья нашей сессии на посадке `own` СПОСОБНА упасть и способна
// смолчать (kacho#2810, п. 1).
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
		return cookieDomainFacts{Stack: "own", Posture: "own", CookieDomain: "none", Declared: true}
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
		{
			// Законный близнец случая с доменным именем: та же величина на
			// цепочке, где наше печенье не выдаётся.
			name:   "external и доменное имя — молчит: наше печенье там не выдаётся",
			mutate: func(f *cookieDomainFacts) { f.Posture, f.CookieDomain = "external", "console.example" },
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
// цепочки дерева, в одну из которых внесён `cookieDomain` ≠ `none`.
// Инъекция в цепочку на `own` краснеет и называет цепочку и ключ; та же правка
// в той же цепочке, переведённой в памяти на посадку `external`, молчит.
func TestOwnCookieDomain_TreeInjectionNamesTheChainAndTheKey(t *testing.T) {
	// Близнец — ТА ЖЕ цепочка с той же правкой, у которой в памяти сменена
	// ровно посадка. Прежде близнецом служила другая цепочка дерева не на
	// `own`, но таких больше нет: посадку `own` объявляют все стенды (#2735).
	// Близнец на той же цепочке строже прежнего — он отличается от инъекции
	// одним фактом, а не всем составом другой цепочки.
	base := readCookieDomainFacts(t, nil)
	var own string
	for _, f := range base {
		if f.Posture == "own" && f.Declared && own == "" {
			own = f.Stack
		}
	}
	if own == "" {
		t.Fatalf("в дереве нет цепочки на own с объявленной ручкой — инъекции некуда попасть")
	}
	other := own
	inject := func(target string, posture string) func(string, map[string]any) {
		return func(stack string, declared map[string]any) {
			if stack != target {
				return
			}
			login, ok := lookup(declared, cookieDomainKey[:len(cookieDomainKey)-1]...)
			m, isMap := login.(map[string]any)
			if !ok || !isMap {
				t.Fatalf("цепочка %s: блока `kaname.config.authn.login` нет — инъекция не внесена", stack)
			}
			m["cookieDomain"] = "console.example"
			if posture == "" {
				return
			}
			authn, ok := lookup(declared, "kaname", "config", "authn")
			am, isMap := authn.(map[string]any)
			if !ok || !isMap {
				t.Fatalf("цепочка %s: блока `kaname.config.authn` нет — посадку сменить нечем", stack)
			}
			am["identityProvider"] = posture
		}
	}

	red, census := judgeOwnCookieDomain(readCookieDomainFacts(t, inject(own, "")))
	t.Logf("инъекция в цепочку %s (own): находок %d · %s", own, len(red), census)
	if len(red) != 1 {
		t.Fatalf("инъекция в цепочку на own дала %d находок, ожидалась 1: %v", len(red), red)
	}
	for _, must := range []string{"цепочка " + own, "kaname.config.authn.login.cookieDomain", `"console.example"`} {
		if !strings.Contains(red[0], must) {
			t.Errorf("находка не называет %q: %s", must, red[0])
		}
	}

	silent, census := judgeOwnCookieDomain(readCookieDomainFacts(t, inject(other, "external")))
	t.Logf("близнец — та же правка в цепочке %s, посадка в памяти external: находок %d · %s",
		other, len(silent), census)
	if len(silent) != 0 {
		t.Errorf("правка в цепочке без посадки own дала находки: %v", silent)
	}
}
