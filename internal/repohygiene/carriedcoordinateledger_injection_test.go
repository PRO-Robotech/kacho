// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// carriedcoordinateledger_injection_test.go — доказательство того, что
// TestCarriedCoordinateLedgerExpiresOnItsOwn СПОСОБЕН упасть, и падает он на
// существе.
//
// Инъекция подаёт синтетику в ТЕ ЖЕ функции (ParseCarriedCoordinateLedger и
// AdjudicateCarriedLedger), которые судят дерево.
//
// Инъекция здесь НЕ УКРАШЕНИЕ, и причина названа приёмкой прямо: на ПУСТОЙ
// ведомости гейт обязан проходить, потому что пустая ведомость есть его цель.
// Значит «зелено» о его способности упасть не говорит НИЧЕГО, и показать её
// можно только синтетикой.
package repohygiene

import (
	"strings"
	"testing"
)

// carriedInjectedNoPredicate — раздел с исходом «оставлено» и БЕЗ предиката
// снятия.
//
// Ровно то послабление, которое не умеет истечь: снимать его будет некому, и
// следующий читатель примет закрытый долг за живой.
const carriedInjectedNoPredicate = `# Ведомость

## Ведомость: зеркальная колонка

| # | координата | что делает | исход |
|---:|---|---|---|
| 1 | ` + "`services/iam/internal/live.go`" + ` | читает зеркало | **оставлено** как путь к прежнему эндпоинту |
`

// carriedInjectedLawful — законный близнец: тот же исход, но раздел объявляет
// предикат снятия, и координате ещё есть что переносить.
//
// Соседи, каждый со своим способом обмануть разбор:
//
//   - строка с исходом **снято**: предиката не требует, потому что снимать в
//     ней нечего;
//   - строка с исходом **не предмет**: то же;
//   - огороженный блок с командой, содержащей вертикальные черты, — таблицей
//     он не является, и разбор, читающий всякую строку с чертой, взял бы его
//     строкой ведомости.
const carriedInjectedLawful = `# Ведомость

## Ведомость: зеркальная колонка

**Предикат снятия — один на все:** внешний сервер выведен из контура.

` + "```sh" + `
git grep -l 'hydra_client_id' -- 'services/iam' | grep -v test
` + "```" + `

| # | координата | что делает | исход |
|---:|---|---|---|
| 1 | ` + "`services/iam/internal/live.go`" + ` | читает зеркало | **оставлено** как путь к прежнему эндпоинту |
| 2 | ` + "`services/iam/internal/gone.go`" + ` | читал зеркало | **снято** |
| 3 | ` + "`gateway/internal/other.go`" + ` | обслуживает путь библиотеки | **не предмет** |
`

// carriedInjectedStale — запись, которой больше нечего переносить: координаты в
// дереве нет.
const carriedInjectedStale = `# Ведомость

## Ведомость: зеркальная колонка

**Предикат снятия:** внешний сервер выведен из контура.

| # | координата | что делает | исход |
|---:|---|---|---|
| 1 | ` + "`services/iam/internal/live.go`" + ` | читает зеркало | **оставлено** |
| 2 | ` + "`services/iam/internal/vanished.go`" + ` | читал зеркало | **оставлено** |
`

// carriedInjectedEmpty — ПУСТАЯ ведомость: все записи разрешились.
//
// Это ЦЕЛЬ, ради которой ведомость заведена, и гейт на ней обязан молчать.
// Отказ здесь подталкивал бы держать запись ради зелёного — то есть ровно к
// тому, что гейт и ловит.
const carriedInjectedEmpty = `# Ведомость

## Ведомость: зеркальная колонка

**Предикат снятия:** внешний сервер выведен из контура.

| # | координата | что делает | исход |
|---:|---|---|---|
| 1 | ` + "`services/iam/internal/gone.go`" + ` | читал зеркало | **снято** |
`

// carriedInjectedUnknownOutcome — исход вне закрытого словаря.
const carriedInjectedUnknownOutcome = `# Ведомость

## Ведомость: зеркальная колонка

**Предикат снятия:** внешний сервер выведен из контура.

| # | координата | что делает | исход |
|---:|---|---|---|
| 1 | ` + "`services/iam/internal/live.go`" + ` | читает зеркало | пока оставим на всякий случай |
`

// carriedSyntheticTree — состав синтетического дерева.
func carriedSyntheticTree(paths ...string) func(string) bool {
	have := map[string]bool{}
	for _, p := range paths {
		have[p] = true
	}
	return func(p string) bool { return have[p] }
}

// TestCarriedLedgerRulesCatchAnEntryThatCannotExpire — сторона (а): запись без
// предиката снятия становится находкой, и находка несёт координату.
func TestCarriedLedgerRulesCatchAnEntryThatCannotExpire(t *testing.T) {
	rows, sections, census := ParseCarriedCoordinateLedger(carriedInjectedNoPredicate)
	if census.Tables != 1 || census.Rows != 1 {
		t.Fatalf("разбор синтетики прочитал таблиц %d, строк %d — ожидалось 1 и 1: %+v",
			census.Tables, census.Rows, rows)
	}
	if rows[0].Outcome != CarriedOutcomeKept {
		t.Fatalf("исход строки опознан как %q, ожидалось %q", rows[0].Outcome, CarriedOutcomeKept)
	}
	if sections[rows[0].Section].HasRemovalPredicate {
		t.Fatalf("раздел без предиката снятия объявлен несущим его — тогда гейт зелен на "+
			"послаблении, которое не умеет истечь: %+v", sections)
	}

	findings := AdjudicateCarriedLedger("ledger.md", rows, sections,
		carriedSyntheticTree("services/iam/internal/live.go"),
		[]string{"services/iam/internal/live.go"})
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "services/iam/internal/live.go") {
		t.Errorf("находка без координаты — по такому отказу нечего чинить: %q", findings[0])
	}
	if !strings.Contains(findings[0], "ledger.md:") {
		t.Errorf("находка не называет строку ведомости: %q", findings[0])
	}
	if !strings.Contains(findings[0], "предиката снятия") {
		t.Errorf("находка не называет, ЧЕГО не хватает: %q", findings[0])
	}
}

// TestCarriedLedgerRulesAreSilentOnALawfulEntry — сторона (б): запись с
// предикатом снятия, которой ещё есть что переносить, находкой не является.
func TestCarriedLedgerRulesAreSilentOnALawfulEntry(t *testing.T) {
	rows, sections, census := ParseCarriedCoordinateLedger(carriedInjectedLawful)
	if census.Tables != 1 {
		t.Fatalf("таблиц прочитано %d, ожидалась 1 — огороженный блок с вертикальными "+
			"чертами принят за таблицу: %+v", census.Tables, rows)
	}
	if census.Rows != 3 {
		t.Fatalf("строк таблицы прочитано %d, ожидалось 3: %+v", census.Rows, rows)
	}
	if !sections["Ведомость: зеркальная колонка"].HasRemovalPredicate {
		t.Fatalf("раздел с объявленным предикатом снятия признан не несущим его — тогда " +
			"гейт краснеет на исправной ведомости")
	}

	findings := AdjudicateCarriedLedger("ledger.md", rows, sections,
		carriedSyntheticTree("services/iam/internal/live.go", "gateway/internal/other.go"),
		[]string{"services/iam/internal/live.go"})
	if len(findings) != 0 {
		t.Fatalf("законная ведомость объявлена находкой (%v).\n\nЛибо предикат снятия не "+
			"опознан, либо исходы «снято» и «не предмет» ошибочно требуют предиката — "+
			"снимать в них нечего, потому что они уже разрешились.", findings)
	}

	// Молчание обязано быть взвешиваемым: разбор увидел все три исхода.
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Outcome] = true
	}
	for _, want := range []string{CarriedOutcomeKept, CarriedOutcomeRemoved, CarriedOutcomeNotSubject} {
		if !got[want] {
			t.Fatalf("исход %q не опознан разбором (опознаны %v) — тогда молчание сказано "+
				"о слепоте, а не о различении", want, got)
		}
	}
}

// TestCarriedLedgerRulesCatchAnEntryWithNothingLeftToCarry — самоистечение:
// запись, чьей координаты в дереве нет.
func TestCarriedLedgerRulesCatchAnEntryWithNothingLeftToCarry(t *testing.T) {
	rows, sections, _ := ParseCarriedCoordinateLedger(carriedInjectedStale)
	findings := AdjudicateCarriedLedger("ledger.md", rows, sections,
		carriedSyntheticTree("services/iam/internal/live.go"),
		[]string{"services/iam/internal/live.go"})
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1 (запись без предмета): %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "vanished.go") {
		t.Fatalf("находка называет не ту запись: %q", findings[0])
	}
	if !strings.Contains(findings[0], "координаты в дереве НЕТ") {
		t.Errorf("находка не называет, ЧЕМ запись просрочена: %q", findings[0])
	}
}

// TestCarriedLedgerRulesCatchAnUnnamedCoordinate — полнота: координата, живущая
// в дереве и ведомостью не названная.
//
// Без этой стороны ведомость молчала бы ровно о том, что забыли, — то есть о
// четвёртом исходе, которого не существует.
func TestCarriedLedgerRulesCatchAnUnnamedCoordinate(t *testing.T) {
	rows, sections, _ := ParseCarriedCoordinateLedger(carriedInjectedLawful)
	findings := AdjudicateCarriedLedger("ledger.md", rows, sections,
		carriedSyntheticTree("services/iam/internal/live.go", "services/iam/internal/forgotten.go"),
		[]string{"services/iam/internal/live.go", "services/iam/internal/forgotten.go"})
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1 (координата без записи): %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "forgotten.go") {
		t.Fatalf("находка называет не ту координату: %q", findings[0])
	}
}

// TestCarriedLedgerRulesPassOnAnEmptyLedger — ПУСТАЯ ведомость проходит.
//
// Пустая ведомость есть ЦЕЛЬ, ради которой ведомость заведена. Отказ на ней
// подталкивал бы держать запись ради зелёного — ровно к тому, что гейт и ловит
// (`testing.md` §«Гейт на класс» п. 5).
func TestCarriedLedgerRulesPassOnAnEmptyLedger(t *testing.T) {
	rows, sections, census := ParseCarriedCoordinateLedger(carriedInjectedEmpty)
	if census.Rows != 1 {
		t.Fatalf("строк прочитано %d, ожидалась 1: %+v", census.Rows, rows)
	}
	kept := 0
	for _, r := range rows {
		if r.Outcome == CarriedOutcomeKept {
			kept++
		}
	}
	if kept != 0 {
		t.Fatalf("оставленных записей %d, ожидалось 0 — синтетика не является пустой "+
			"ведомостью, и утверждение о ней сказано не о том", kept)
	}

	findings := AdjudicateCarriedLedger("ledger.md", rows, sections,
		carriedSyntheticTree(), nil)
	if len(findings) != 0 {
		t.Fatalf("ПУСТАЯ ведомость объявлена находкой (%v).\n\nПустая ведомость есть цель, "+
			"ради которой она заведена: отказ на ней подталкивает держать запись ради "+
			"зелёного — ровно то, что гейт и ловит.", findings)
	}

	// И перепись обязана остаться непустой: «ноль находок» обязано быть отличимо
	// от «ноль прочитанного».
	if census.Tables == 0 || census.Lines == 0 {
		t.Fatalf("на пустой ведомости перепись обнулилась (таблиц %d, строк документа %d) — "+
			"тогда молчание нечем взвесить", census.Tables, census.Lines)
	}
}

// TestCarriedLedgerRulesCatchAnOutcomeOutsideTheVocabulary — словарь исходов
// закрыт: «прочее» не является корзиной приёма.
func TestCarriedLedgerRulesCatchAnOutcomeOutsideTheVocabulary(t *testing.T) {
	rows, sections, _ := ParseCarriedCoordinateLedger(carriedInjectedUnknownOutcome)
	if len(rows) != 1 || rows[0].Outcome != "" {
		t.Fatalf("исход вне словаря опознан как %q — тогда «пока оставим на всякий случай» "+
			"читается решением и решением не является: %+v", rows[0].Outcome, rows)
	}
	findings := AdjudicateCarriedLedger("ledger.md", rows, sections,
		carriedSyntheticTree("services/iam/internal/live.go"), nil)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "вне закрытого словаря") {
		t.Fatalf("находка не называет, чем исход негоден: %q", findings[0])
	}
}
