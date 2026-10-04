// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_origin_is_secure_test.go — ПРОИСХОЖДЕНИЕ КОНСОЛИ НА КАЖДОЙ ЦЕПОЧКЕ
// ЗАЩИЩЁННОЕ: СХЕМА `https` (kacho#3024).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Служба доступа выдаёт печенье контекста формы и носитель сессии с атрибутом
// `Secure` — это боевая форма, и она не ослабляется. Браузер Secure-печенье с
// происхождения `http`, отличного от `localhost`, не хранит. Значит на цепочке,
// объявившей происхождение консоли схемой `http`, законный человек получает на
// первой же форме, меняющей состояние, `403 FORM_TOKEN_REJECTED`: признак формы
// пришёл, контекста нет. Ровно так отказывала регистрация на управляемом
// стенде, отдававшем консоль по открытому http (kacho#3024).
//
// Происхождение здесь — то, что служба доступа получает как адрес консоли:
// `global.kacho.identity.appBaseURL`, а пусто — `https://<appSubdomain>.<domain>`
// (помощник `kaname.identity.consoleOrigin` подчарта службы). Читаются
// разобранные значения цепочки поверх умолчаний умбреллы, не рендер.
//
// ─────────────────────────────────────────────────────────────────────────────
// ИСКЛЮЧЕНИЕ — ЗАПИСЬ С ЗАДАЧЕЙ, И ОНО САМОИСТЕКАЕТ
//
// Стенд kind сегодня отдаёт консоль по http (`plainHTTPOriginDebts`): это тот же
// дефект, у него своя задача, и запись его НАЗЫВАЕТ, а не прощает. Запись,
// которой ни одна цепочка не объявляет, — находка: исключению нечего
// исключать, и оно обязано быть снято вместе с предметом.
//
// Способность упасть и смолчать — console_origin_is_secure_injection_test.go.
package deploy_test

import (
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// plainHTTPOriginDebt — происхождение по http, известное и заведённое задачей.
type plainHTTPOriginDebt struct {
	Origin string // дословно, как объявлено
	Issue  string // задача, снимающая запись
	Reason string
}

// plainHTTPOriginDebts — единственный перечень. Пополнять его — значит заводить
// задачу на тот же дефект, а не объявлять http допустимым.
var plainHTTPOriginDebts = []plainHTTPOriginDebt{{
	Origin: "http://console.kacho.local:28080",
	Issue:  "PRO-Robotech/kacho#3025",
	Reason: "стенд kind отображает только 80 → 28080, слушателя TLS у него нет; " +
		"сквозные пробы объявляют это происхождение защищённым своим клиентам (#1274), человеку такой обход недоступен",
}}

var (
	consoleOriginKey       = []string{"global", "kacho", "identity", "appBaseURL"}
	consoleDomainKey       = []string{"global", "kacho", "identity", "domain"}
	consoleAppSubdomainKey = []string{"global", "kacho", "identity", "appSubdomain"}
)

// consoleOriginFacts — что объявила цепочка одного стенда.
type consoleOriginFacts struct {
	Stack    string
	Origin   string // действующее происхождение консоли
	Declared bool   // объявлено ли `appBaseURL` явно (иначе — выведено)
}

type consoleOriginCensus struct {
	Stacks, Secure, Debts int
}

func (c consoleOriginCensus) String() string {
	return fmt.Sprintf("цепочек осмотрено %d · защищённых %d · по записи-исключению %d", c.Stacks, c.Secure, c.Debts)
}

// effectiveConsoleOrigin — тот же вывод, что у `kaname.identity.consoleOrigin`.
func effectiveConsoleOrigin(declared map[string]any) (origin string, explicit bool) {
	v, ok := lookup(declared, consoleOriginKey...)
	if s := declaredString(v, ok); s != "" {
		return s, true
	}
	d, dok := lookup(declared, consoleDomainKey...)
	a, aok := lookup(declared, consoleAppSubdomainKey...)
	return fmt.Sprintf("https://%s.%s", declaredString(a, aok), declaredString(d, dok)), false
}

// judgeConsoleOrigins — НАХОДКИ по цепочкам. Чистая функция: инъекция подаёт ей
// синтетический вход.
func judgeConsoleOrigins(facts []consoleOriginFacts, debts []plainHTTPOriginDebt) ([]string, consoleOriginCensus) {
	var findings []string
	var census consoleOriginCensus
	used := map[string]bool{}
	debtOf := map[string]plainHTTPOriginDebt{}
	for _, d := range debts {
		debtOf[d.Origin] = d
	}
	for _, f := range facts {
		census.Stacks++
		u, err := url.Parse(f.Origin)
		if err != nil || u.Host == "" {
			findings = append(findings, fmt.Sprintf(
				"цепочка %s: происхождение консоли %q не разбирается как адрес — что выдаст браузер, не сказать", f.Stack, f.Origin))
			continue
		}
		switch {
		case u.Scheme == "https":
			census.Secure++
		case u.Scheme == "http":
			if _, ok := debtOf[f.Origin]; ok {
				census.Debts++
				used[f.Origin] = true
				continue
			}
			findings = append(findings, fmt.Sprintf(
				"цепочка %s: происхождение консоли %q — открытый http. Печенье формы и сессии выдаётся с `Secure`, "+
					"браузер его с такого происхождения не хранит, и законная регистрация получает "+
					"`403 FORM_TOKEN_REJECTED` (kacho#3024). Завершите TLS на пользовательском крае "+
					"(`uif.publicFront`) и объявите `https`; защиту не снимать", f.Stack, f.Origin))
		default:
			findings = append(findings, fmt.Sprintf(
				"цепочка %s: происхождение консоли %q — схема %q, ни http, ни https", f.Stack, f.Origin, u.Scheme))
		}
	}
	for _, d := range debts {
		if !used[d.Origin] {
			findings = append(findings, fmt.Sprintf(
				"запись-исключение %q (%s) не используется ни одной цепочкой — исключению нечего исключать: "+
					"снимите запись вместе с задачей", d.Origin, d.Issue))
		}
	}
	return findings, census
}

// TestConsoleOriginIsSecureOnEveryChain — сверка по дереву.
func TestConsoleOriginIsSecureOnEveryChain(t *testing.T) {
	facts := readConsoleOriginFacts(t, nil)
	findings, census := judgeConsoleOrigins(facts, plainHTTPOriginDebts)
	t.Logf("перепись: %s", census)
	for _, f := range facts {
		how := "выведено"
		if f.Declared {
			how = "объявлено"
		}
		t.Logf("  %-11s %s (%s)", f.Stack, f.Origin, how)
	}
	if census.Stacks == 0 {
		t.Fatal("таблица стеков пуста: «находок нет» здесь означало бы «сверять было не с чем»")
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// readConsoleOriginFacts — факты из дерева; mutate — правка разобранных значений
// цепочки ДО чтения (инъекция настоящим входом), nil — без правки.
func readConsoleOriginFacts(t *testing.T, mutate func(stack string, declared map[string]any)) []consoleOriginFacts {
	t.Helper()
	stacksTbl := deployStacks(t)
	names := make([]string, 0, len(stacksTbl))
	for n := range stacksTbl {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]consoleOriginFacts, 0, len(names))
	for _, name := range names {
		// Умолчания умбреллы читаются ЗАНОВО на каждый стенд: `mergeValues`
		// правит карту на месте.
		declared := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))
		for _, p := range stacksTbl[name] {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		if mutate != nil {
			mutate(name, declared)
		}
		origin, explicit := effectiveConsoleOrigin(declared)
		out = append(out, consoleOriginFacts{Stack: name, Origin: strings.TrimRight(origin, "/"), Declared: explicit})
	}
	return out
}
