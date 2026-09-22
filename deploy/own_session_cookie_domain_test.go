// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_session_cookie_domain_test.go — НА КАЖДОЙ ЦЕПОЧКЕ ПОСАДКИ `own` НАША
// СЕССИЯ ВЫДАЁТСЯ БЕЗ `Domain` (kacho#2810, п. 1).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Выход края гасит печенье нашей сессии БЕЗ `Domain`
// (gateway/internal/middleware, `SessionCarrierEndings`), а браузер сопоставляет
// печенье по имени, пути и домену: гашение без `Domain` закрывает только
// печенье, выданное тоже без него. Шапка
// gateway/internal/middleware/session_carrier_names.go (ПРЕДУСЛОВИЕ ОБЪЯВЛЕНИЯ)
// опирается на то, что так объявлено на каждой цепочке, где наше печенье
// выдаётся, — `kaname.config.authn.login.cookieDomain: none`. Выдаётся оно
// полосой входа, а полоса поднимается посадкой `own`.
//
// Пробы этого условия в дереве не было (kacho `origin/2795` @ `c5a1e7cf`):
// условие держалось текстом шапки и тем, что сегодня его никто не нарушил.
// Профиль, объявивший на `own` доменное имя, прошёл бы все пробы, а выход края
// перестал бы закрывать нашу сессию — молча, потому что гашение уходит, и
// ответ выхода выглядит успехом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ
//
// По каждой цепочке deploy/stacks.txt, наложенной слева направо поверх
// умолчаний подчарта (разобранные значения, не рендер): посадка `own` ⇒
// `cookieDomain` объявлен и равен `none`. Цепочка не на `own` предметом не
// является — наше печенье там не выдаётся. Перепись печатает число осмотренных
// цепочек и число цепочек на `own`; ноль цепочек на `own` — отказ, а не
// зелёное.
//
// Вторая половина предусловия той же шапки — переходное состояние
// `own,external` не объявляется там, где поставщик ставит `Domain`, — здесь НЕ
// судится: её предмет снимается вместе с переходным состоянием (kacho#2815,
// kacho#2792), и проба, заведённая на снимаемое, истекла бы вместе с ним.
//
// Способность упасть и смолчать — own_session_cookie_domain_injection_test.go.
package deploy_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"
)

// ownCookieDomainWanted — единственное значение ручки, при котором печенье
// нашей сессии выдаётся без `Domain` и гашение края его закрывает.
const ownCookieDomainWanted = "none"

// cookieDomainKey — ручка, которую судит проба (путь в значениях умбреллы).
var cookieDomainKey = []string{"kaname", "config", "authn", "login", "cookieDomain"}

// cookieDomainFacts — что цепочка одного стенда объявила.
type cookieDomainFacts struct {
	Stack        string
	Posture      string // kaname.config.authn.identityProvider
	CookieDomain string // kaname.config.authn.login.cookieDomain
	Declared     bool   // объявлена ли ручка вовсе
}

// cookieDomainCensus — объём осмотренного.
type cookieDomainCensus struct {
	Stacks int
	Own    int
}

func (c cookieDomainCensus) String() string {
	return fmt.Sprintf("цепочек осмотрено %d · из них на посадке own %d", c.Stacks, c.Own)
}

// judgeOwnCookieDomain — НАХОДКИ по цепочкам. Чистая функция: инъекция подаёт
// ей синтетический вход.
func judgeOwnCookieDomain(facts []cookieDomainFacts) ([]string, cookieDomainCensus) {
	var findings []string
	var census cookieDomainCensus
	for _, f := range facts {
		census.Stacks++
		if f.Posture != "own" {
			continue
		}
		census.Own++
		switch {
		case !f.Declared || f.CookieDomain == "":
			findings = append(findings, fmt.Sprintf(
				"цепочка %s на посадке own: `kaname.config.authn.login.cookieDomain` не объявлен — "+
					"домен печенья нашей сессии не назван, и гашение края без `Domain` закрывает его "+
					"только случайно. Объявите `%s`", f.Stack, ownCookieDomainWanted))
		case f.CookieDomain != ownCookieDomainWanted:
			findings = append(findings, fmt.Sprintf(
				"цепочка %s на посадке own: `kaname.config.authn.login.cookieDomain` = %q, а не %q — "+
					"наше печенье выдаётся с `Domain`, а край гасит его без `Domain`: выход ответит "+
					"успехом и сессию НЕ закроет. Нарушено предусловие шапки "+
					"gateway/internal/middleware/session_carrier_names.go",
				f.Stack, f.CookieDomain, ownCookieDomainWanted))
		}
	}
	return findings, census
}

// TestOwnSessionCookieDomain_IsNoneOnEveryOwnChain — сверка по дереву.
func TestOwnSessionCookieDomain_IsNoneOnEveryOwnChain(t *testing.T) {
	facts := readCookieDomainFacts(t, nil)
	findings, census := judgeOwnCookieDomain(facts)
	t.Logf("перепись: %s", census)
	for _, f := range facts {
		if f.Posture == "own" {
			t.Logf("  %s: посадка %s · cookieDomain %q", f.Stack, f.Posture, f.CookieDomain)
		}
	}
	if census.Stacks == 0 {
		t.Fatal("таблица стеков пуста: «находок нет» здесь означало бы «сверять было не с чем»")
	}
	if census.Own == 0 {
		t.Fatalf("ни одна из %d цепочек не стоит на посадке own — наше печенье нигде не выдаётся, и "+
			"судить его домен не на чем", census.Stacks)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// readCookieDomainFacts — факты из дерева; mutate — правка разобранных
// значений цепочки ДО чтения (инъекция настоящим входом), nil — без правки.
func readCookieDomainFacts(t *testing.T, mutate func(stack string, declared map[string]any)) []cookieDomainFacts {
	t.Helper()
	stacksTbl := deployStacks(t)
	names := make([]string, 0, len(stacksTbl))
	for n := range stacksTbl {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]cookieDomainFacts, 0, len(names))
	for _, name := range names {
		// Умолчания подчарта читаются ЗАНОВО на каждый стенд: `mergeValues`
		// правит карту на месте.
		declared := map[string]any{"kaname": readYAML(t, filepath.Join(kanameSubchart(t), "values.yaml"))}
		for _, p := range stacksTbl[name] {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		if mutate != nil {
			mutate(name, declared)
		}
		f := cookieDomainFacts{Stack: name}
		f.Posture = declaredString(lookup(declared, "kaname", "config", "authn", "identityProvider"))
		v, ok := lookup(declared, cookieDomainKey...)
		f.Declared = ok && v != nil
		f.CookieDomain = declaredString(v, ok)
		out = append(out, f)
	}
	return out
}
