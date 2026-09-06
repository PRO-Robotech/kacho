// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenclaimsinglesource_injection_test.go — доказательство того, что
// TestTokenClaimsAreAssembledInOnePlace СПОСОБЕН упасть, и падает он на
// существе.
//
// Инъекция гоняет ТЕ ЖЕ функции разбора (ScanClaimAssemblies,
// ScanClaimBuilderCalls), что и гейт.
//
// Вторая сторона пары здесь тяжелее первой и без неё гейт был бы вреден:
// потребителей у единственной сборки должно быть МНОГО — второй способ дойти до
// того же состава есть цель, ради которой сборка вынесена. Гейт, спутавший
// потребителя со сборкой, запретил бы ровно это.
package repohygiene

import (
	"strings"
	"testing"
)

// claimInjectedSecondAssembly — ВТОРАЯ сборка состава.
//
// Она намеренно НЕПОЛНАЯ относительно первой: два состава расходятся не сразу,
// а при первой правке одной стороны, и расходятся молча — обе стороны по
// отдельности выглядят исправными, потому что каждая отдаёт непустой состав.
const claimInjectedSecondAssembly = `package iamhooks

func (h *RefreshHookHandler) refreshClaims(u user) map[string]any {
	claims := map[string]any{
		"kacho_external_id":    u.ExternalID,
		"kacho_user_id":        u.ID,
		"kacho_principal_type": "user",
	}
	return claims
}
`

// claimInjectedLawfulConsumers — законные потребители единственной сборки.
//
// Соседи, каждый со своим способом обмануть разбор:
//
//   - ТРИ вызова сборщиков из ДВУХ разных функций — это и есть «обе стороны»;
//   - `map[string]any{}` без ключей — состав, собираемый присваиваниями: слепая
//     зона, названная в шапке разбора, и она обязана быть видна счётчиком;
//   - `map[string]any{"kacho_route": …}` с ОДНИМ ключом — чтение одного
//     значения, а не объявление состава.
const claimInjectedLawfulConsumers = `package service

func (s *TokenEnrichmentService) ClaimsForAssertionClient(c client) map[string]any {
	if c.Kind == kindUser {
		return s.userTokenClaims(c.Row, c.User, c.Subject, c.Hook)
	}
	return s.saClaims(c.Row, c.SA, c.Subject, c.Hook)
}

func (s *TokenEnrichmentService) EnrichClaims(subject string) map[string]any {
	acc := map[string]any{}
	acc["kacho_user_id"] = subject
	_ = map[string]any{"kacho_route": subject}
	return s.userClaims(subject)
}
`

// TestClaimScannerFindsASecondAssembly — сторона (а): вторая сборка становится
// находкой, и находка несёт координату и функцию.
func TestClaimScannerFindsASecondAssembly(t *testing.T) {
	as, census, err := ScanClaimAssemblies(
		"synthetic/iamhooks/refresh.go", []byte(claimInjectedSecondAssembly),
		claimKeyPrefix, claimMinKeys)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.MapLiterals == 0 {
		t.Fatalf("осмотрено ноль литералов отображения — разбирается не то дерево")
	}
	if len(as) != 1 {
		t.Fatalf("сборок найдено %d, ожидалась 1: %+v", len(as), as)
	}
	a := as[0]
	if a.File == "" || a.Line == 0 {
		t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", a)
	}
	if a.Func != "RefreshHookHandler.refreshClaims" {
		t.Errorf("находка не называет функцию сборки: %+v", a)
	}
	if len(a.Keys) != claimMinKeys {
		t.Errorf("ключей насчитано %d, ожидалось %d: %+v", len(a.Keys), claimMinKeys, a)
	}

	// Порог обязан РАБОТАТЬ: та же сборка на ключ меньше сборкой не считается.
	// Без этой половины порог был бы объявлением, а не предикатом.
	trimmed := strings.Replace(claimInjectedSecondAssembly,
		"\t\t\"kacho_principal_type\": \"user\",\n", "", 1)
	as2, _, err := ScanClaimAssemblies("synthetic/iamhooks/refresh.go", []byte(trimmed),
		claimKeyPrefix, claimMinKeys)
	if err != nil {
		t.Fatalf("разбор усечённой синтетики: %v", err)
	}
	if len(as2) != 0 {
		t.Fatalf("место с %d ключами признано сборкой при пороге %d: %+v",
			claimMinKeys-1, claimMinKeys, as2)
	}

	// Сборка не прячется за ДРУГИМ типом значения. Разбор судит по ИМЕНАМ
	// ключей, а не по типу отображения: словарь утверждений и есть предмет, и
	// перенос состава в map[string]string сменил бы форму записи, а не место.
	const otherValueType = `package iamhooks

func claims() map[string]string {
	return map[string]string{
		"kacho_external_id":    "a",
		"kacho_user_id":        "b",
		"kacho_principal_type": "user",
	}
}
`
	as3, _, err := ScanClaimAssemblies("synthetic/iamhooks/other.go", []byte(otherValueType),
		claimKeyPrefix, claimMinKeys)
	if err != nil {
		t.Fatalf("разбор синтетики с другим типом значения: %v", err)
	}
	if len(as3) != 1 {
		t.Fatalf("сборка, переписанная на другой тип значения, найдена %d раз(а), ожидался 1: "+
			"%+v.\n\nРазбор, судящий по типу отображения, а не по именам ключей, пропустил бы "+
			"вторую сборку, переписанную одной сменой типа.", len(as3), as3)
	}
}

// TestClaimScannerIsSilentOnConsumers — сторона (б): потребители единственной
// сборки, сколько бы их ни было, находкой не становятся.
func TestClaimScannerIsSilentOnConsumers(t *testing.T) {
	as, census, err := ScanClaimAssemblies(
		"synthetic/service/own_lane.go", []byte(claimInjectedLawfulConsumers),
		claimKeyPrefix, claimMinKeys)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(as) != 0 {
		t.Fatalf("разбор объявил сборкой законное место (%+v).\n\nЛибо потребитель спутан "+
			"со сборкой — тогда гейт запрещает второй способ дойти до единственного "+
			"объявления, ради которого оно и вынесено; либо отображение другого типа "+
			"значений принято за состав утверждений.", as)
	}
	// Слепая зона названа, а не спрятана: состав, собираемый присваиваниями,
	// даёт ПУСТОЙ литерал, и счётчик обязан это показывать.
	if census.EmptyMapLiterals == 0 {
		t.Fatalf("пустых литералов насчитано 0 — перепись не показывает той формы, которой " +
			"разбор не видит, и его молчание нечем взвесить")
	}
	// Одноключевое место в перепись попадает, сборкой не становясь: молчание
	// взвешиваемо.
	if census.KeyedLiterals == 0 {
		t.Fatalf("литералов с ключами состава насчитано 0 — тогда молчание сказано о " +
			"слепоте, а не о различении")
	}

	calls, c2, err := ScanClaimBuilderCalls(
		"synthetic/service/own_lane.go", []byte(claimInjectedLawfulConsumers), claimBuilders)
	if err != nil {
		t.Fatalf("разбор вызовов синтетики: %v", err)
	}
	if c2.Calls == 0 {
		t.Fatalf("осмотрено ноль вызовов — разбирается не то дерево")
	}
	if len(calls) != 3 {
		t.Fatalf("вызовов сборщиков найдено %d, ожидалось 3: %+v", len(calls), calls)
	}
	lanes := map[string]bool{}
	for _, c := range calls {
		lanes[c.Func] = true
	}
	if len(lanes) < claimLanesFloor {
		t.Fatalf("полос насчитано %d при пороге %d — гейт считает ВЫЗОВЫ вместо точек входа, "+
			"и «обе стороны» у него выполняется двумя вызовами из одной", len(lanes), claimLanesFloor)
	}
}

// TestClaimDebtRulesCatchABareEntry — правила ведомости обязаны краснеть на
// голой записи и МОЛЧАТЬ на полной.
func TestClaimDebtRulesCatchABareEntry(t *testing.T) {
	lawful := claimDebtEntry{
		File:  "services/iam/internal/handler/x/handler.go",
		Func:  "H.claims",
		Why:   "лежит в другом пакете и позвать неэкспортируемый сборщик не может",
		Until: "файл зовёт сборщик единственного объявления",
	}
	if bad := claimDebtDefects([]claimDebtEntry{lawful}); len(bad) != 0 {
		t.Fatalf("полная запись объявлена дефектной: %v", bad)
	}
	noWhy, noUntil, noCoord := lawful, lawful, lawful
	noWhy.Why, noUntil.Until, noCoord.Func = " ", "", ""
	for name, e := range map[string]claimDebtEntry{
		"без обоснования": noWhy,
		"без предиката":   noUntil,
		"без координаты":  noCoord,
	} {
		if bad := claimDebtDefects([]claimDebtEntry{e}); len(bad) == 0 {
			t.Errorf("запись %s принята правилами ведомости — тогда ведомость перестаёт "+
				"отличаться от списка прощённых", name)
		}
	}
	if bad := claimDebtDefects([]claimDebtEntry{lawful, lawful}); len(bad) == 0 {
		t.Error("дубль в ведомости не найден — записей на одну меньше, чем кажется")
	}
}

// TestClaimDebtStaleRuleCatchesAnEntryWithoutSubject — запись без предмета
// роняет прогон, живая молчит, пустая ведомость молчит.
func TestClaimDebtStaleRuleCatchesAnEntryWithoutSubject(t *testing.T) {
	entries := []claimDebtEntry{
		{File: "a.go", Func: "A.claims", Why: "w", Until: "u"},
		{File: "b.go", Func: "B.claims", Why: "w", Until: "u"},
	}
	live := map[string]bool{"a.go\x00A.claims": true}
	stale := claimDebtStale(entries, live)
	if len(stale) != 1 || !strings.Contains(stale[0], "b.go") {
		t.Fatalf("просроченные записи определены неверно: %v", stale)
	}
	if got := claimDebtStale(entries[:1], live); len(got) != 0 {
		t.Fatalf("живая запись объявлена просроченной: %v", got)
	}
	if got := claimDebtStale(nil, live); len(got) != 0 {
		t.Fatalf("пустая ведомость объявлена просроченной: %v", got)
	}
}
