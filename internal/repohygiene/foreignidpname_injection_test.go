// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

// foreignidpname_injection_test.go — ИНЪЕКЦИЯ В ОБЕ СТОРОНЫ на синтетике.
//
// Прогон гейта по дереву доказывает «сегодня чисто». Он НЕ доказывает, что гейт
// СПОСОБЕН покраснеть: молчание над исправным деревом и молчание сломанного
// распознавателя выглядят одинаково. Здесь дефект вносится настоящей формой
// записи, а рядом ставится законный близнец той же формы.
//
// Корпус синтетический (`t.TempDir` не нужен: судятся тела, а не файлы) —
// самопроверка, построенная на живой записи ведомости, покраснела бы в день,
// когда ведомость опустеет, то есть на достижении собственной цели.

// injSources — корпус из одного файла.
func injSources(name, body string) map[string]string {
	return map[string]string{name: body}
}

// findingAt — есть ли находка названного вида с названным именем.
func findingAt(fs []ForeignIDPNameFinding, kind, name string) bool {
	for _, f := range fs {
		if f.Kind == kind && (name == "" || f.Name == name) {
			return true
		}
	}
	return false
}

// TestForeignIDPNameInjection_NameOutsideTheLedgerIsFound — дефект настоящей
// формой: константа фикстуры, носящая название поставщика, в области, которой
// ведомость не называет.
func TestForeignIDPNameInjection_NameOutsideTheLedgerIsFound(t *testing.T) {
	t.Parallel()
	const body = `package jwks

const testHydraIss = "https://hydra.api.kacho.cloud"
`
	findings, census, err := JudgeForeignIDPNames(
		injSources("services/registry/internal/clients/jwks/verifier_test.go", body), nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if census.Names != 1 {
		t.Fatalf("перепись назвала имён %d, внесено 1 — инъекция уронила не свой предмет", census.Names)
	}
	if !findingAt(findings, ForeignIDPNameUnledgered, "testHydraIss") {
		t.Fatalf("внесённое имя не найдено; находки: %+v", findings)
	}
	// Текст находки — часть свойства: он обязан называть ПРИЧИНУ, а не симптом.
	if !strings.Contains(findings[0].Detail, "hydra") {
		t.Errorf("находка не называет слова, по которому имя узнано: %q", findings[0].Detail)
	}
	if findings[0].Line != 3 {
		t.Errorf("координата находки %d, имя объявлено на строке 3", findings[0].Line)
	}
}

// TestForeignIDPNameInjection_LawfulTwinIsSilent — законный близнец ТОЙ ЖЕ
// формы: то же содержимое, где название поставщика стоит там, где оно законно —
// в строковом литерале (его адрес), в имени ручки посадки и в комментарии
// (разбор переезда). Имя — наше.
//
// Без этой половины гейт был бы гейтом ПО СЛОВУ: он краснел бы на исправном
// дереве и был бы снят первым же обходом.
func TestForeignIDPNameInjection_LawfulTwinIsSilent(t *testing.T) {
	t.Parallel()
	const body = `package jwks

// testLegacyIss — прежний издатель; его запись — зеркало, живёт до F4.
// Ручка посадки: KACHO_REGISTRY_HYDRA_ISSUER. Подчарт: kacho-umbrella-hydra-public.
// Разбор переезда: пока Hydra принимается краем, адрес обязан совпадать с профилем.
const (
	testLegacyIss = "https://hydra.api.kacho.cloud"
	legacyKnob    = "KACHO_REGISTRY_HYDRA_ISSUER"
	legacySvc     = "kacho-umbrella-hydra-public.kacho.svc:4444"
)

func legacyClaims(sub string) map[string]any { return map[string]any{"sub": sub} }
`
	findings, census, err := JudgeForeignIDPNames(
		injSources("services/registry/internal/clients/jwks/verifier_test.go", body), nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if census.Files != 1 || census.Idents == 0 {
		t.Fatalf("близнец не разобран: %s", census.String())
	}
	if len(findings) != 0 {
		t.Fatalf("гейт нашёл находку на законном близнеце — он судит СЛОВО, а не имя: %+v",
			findings)
	}
}

// TestForeignIDPNameInjection_EveryLawfulWritingFormIsFound — распознаватель
// знает КАЖДУЮ законную форму записи имени, и каждая проверена отдельно.
//
// Форма, о которой он не знает, даёт не красное и не зелёное, а молчание.
func TestForeignIDPNameInjection_EveryLawfulWritingFormIsFound(t *testing.T) {
	t.Parallel()
	forms := map[string]string{
		"camelCase (функция)": `package p

func hydraClaims() {}
`,
		"PascalCase (тип)": `package p

type KratosSubjectLookuper interface{ Lookup() }
`,
		"snake_case (переменная)": `package p

func f() { var saw_kratos bool; _ = saw_kratos }
`,
		"приставка ory (константа)": `package p

const oryFlowsAnchor = "x"
`,
		"сплошной заглавный прогон (поле)": `package p

type T struct{ HydraClientID string }
`,
		"параметр": `package p

func f(hydraMirroredKids []string) { _ = hydraMirroredKids }
`,
		"короткое объявление": `package p

func f() { hydra := 1; _ = hydra }
`,
		"SCREAMING_SNAKE (константа)": `package p

const KACHO_HYDRA_ADMIN_URL = "x"
`,
		"имя пробы": `package p

import "testing"

func TestLogout_HydraSessionKill(t *testing.T) { _ = t }
`,
		"псевдоним импорта": `package p

import hydraadmin "net/http"

var _ = hydraadmin.StatusOK
`,
		"переменная range": `package p

func f(m map[string]int) {
	for hydraKid := range m {
		_ = hydraKid
	}
}
`,
		"имя пакета": `package hydraclient
`,
		"сплошная строчная с hydrat": `package p

func hydratest() {}
`,
		"сплошная строчная с hydrat (константа)": `package p

const hydratokenTTL = 1
`,
	}
	for name, body := range forms {
		findings, census, err := JudgeForeignIDPNames(injSources("svc/a_test.go", body), nil)
		if err != nil {
			t.Fatalf("%s: разбор: %v", name, err)
		}
		if census.Names == 0 || !findingAt(findings, ForeignIDPNameUnledgered, "") {
			t.Errorf("форма %q не опознана: имён %d, находок %d — распознаватель о ней "+
				"не знает, и на ней он МОЛЧИТ", name, census.Names, len(findings))
		}
	}
}

// TestForeignIDPNameInjection_NeighbouringWordsStaySilent — отрицательный
// контроль распознавателя: имена, в которых подстрока «ory» есть, а слова «ory»
// нет, находкой не являются.
//
// Без него распознаватель по подстроке зеленел бы только потому, что таких имён
// в дереве не оказалось: `repository` встречается в нём тысячами.
func TestForeignIDPNameInjection_NeighbouringWordsStaySilent(t *testing.T) {
	t.Parallel()
	const body = `package p

type RepositoryWriter struct{ Factory string }

func NewRepositoryFactory(memoryRouter, directoryWalker, historyReader, categoryID string) {}

func hydrated() {}

func rehydrateCache() {}

const advisoryLock = 1
`
	findings, census, err := JudgeForeignIDPNames(injSources("svc/a.go", body), nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if census.Idents == 0 {
		t.Fatal("ноль осмотренных имён — разбор не дошёл до исполняемой части")
	}
	if len(findings) != 0 {
		t.Fatalf("соседние слова зачтены находкой: %+v", findings)
	}
}

// TestForeignIDPNameInjection_LedgeredAreaWithTheDeclaredCountIsSilent —
// вторая половина пары: область, названная ведомостью с ВЕРНЫМ числом, молчит.
func TestForeignIDPNameInjection_LedgeredAreaWithTheDeclaredCountIsSilent(t *testing.T) {
	t.Parallel()
	const body = `package p

func hydraClaims() {}

func kratosStub() {}
`
	ledger := []ForeignIDPNameLedgerEntry{{
		Area: "gateway/", Names: 2, Why: "край говорит с поставщиком", Until: "полоса края закончена",
	}}
	findings, census, err := JudgeForeignIDPNames(injSources("gateway/internal/a_test.go", body), ledger)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if census.Names != 2 {
		t.Fatalf("перепись назвала имён %d, внесено 2", census.Names)
	}
	if len(findings) != 0 {
		t.Fatalf("ведомость с верным числом не молчит: %+v", findings)
	}
}

// TestForeignIDPNameInjection_CountDriftIsFoundBothWays — расхождение числа
// ловится в ОБЕ стороны: вверх (поверхность выросла) и вниз (запись пережила
// часть своего предмета).
func TestForeignIDPNameInjection_CountDriftIsFoundBothWays(t *testing.T) {
	t.Parallel()
	const body = `package p

func hydraClaims() {}

func kratosStub() {}
`
	for _, tc := range []struct {
		name     string
		declared int
	}{
		{"вверх: объявлено меньше, чем в дереве", 1},
		{"вниз: объявлено больше, чем в дереве", 3},
	} {
		ledger := []ForeignIDPNameLedgerEntry{{
			Area: "gateway/", Names: tc.declared, Why: "w", Until: "u",
		}}
		findings, _, err := JudgeForeignIDPNames(injSources("gateway/internal/a_test.go", body), ledger)
		if err != nil {
			t.Fatalf("%s: разбор: %v", tc.name, err)
		}
		if !findingAt(findings, ForeignIDPNameCountDrift, "") {
			t.Errorf("%s: расхождение не найдено — потолок прощает рост до себя", tc.name)
		}
		if len(findings) == 1 && !strings.Contains(findings[0].Detail, "перепишите число на 2") {
			t.Errorf("%s: находка не называет числа, которое надо записать: %q",
				tc.name, findings[0].Detail)
		}
	}
}

// TestForeignIDPNameInjection_EntryWithNothingToNameIsFound — запись ведомости,
// которой больше нечего называть, — находка: послабление обязано истекать само.
func TestForeignIDPNameInjection_EntryWithNothingToNameIsFound(t *testing.T) {
	t.Parallel()
	const body = `package p

func legacyClaims() {}
`
	ledger := []ForeignIDPNameLedgerEntry{{
		Area: "gateway/", Names: 2, Why: "w", Until: "полоса края закончена",
	}}
	findings, _, err := JudgeForeignIDPNames(injSources("gateway/internal/a_test.go", body), ledger)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if !findingAt(findings, ForeignIDPNameStale, "") {
		t.Fatalf("запись, которой нечего называть, не найдена: %+v", findings)
	}
	if !strings.Contains(findings[0].Detail, "полоса края закончена") {
		t.Errorf("находка не называет предиката снятия записи: %q", findings[0].Detail)
	}
}

// TestForeignIDPNameInjection_EmptyCorpusIsRefused — пустой обход не вердикт:
// «ноль находок» обязано быть отличимо от «ноль прочитанного».
func TestForeignIDPNameInjection_EmptyCorpusIsRefused(t *testing.T) {
	t.Parallel()
	_, census, err := JudgeForeignIDPNames(map[string]string{}, nil)
	if err == nil {
		t.Fatal("пустой корпус принят за зелёное")
	}
	if census.Files != 0 {
		t.Errorf("перепись на пустом корпусе назвала файлов %d", census.Files)
	}
}

// TestForeignIDPNameInjection_UnparsableSourceIsRefused — нечитаемый исходник
// отказ, а не молчаливый пропуск: пропуск превратил бы «не прочитали» в «имён нет».
func TestForeignIDPNameInjection_UnparsableSourceIsRefused(t *testing.T) {
	t.Parallel()
	if _, _, err := JudgeForeignIDPNames(injSources("svc/a.go", "не Go вовсе"), nil); err == nil {
		t.Fatal("неразбираемый исходник принят за файл без имён")
	}
}

// TestForeignIDPNameSplitterKnowsTheWritingForms — разбор имени на слова
// проверяется прямо: от него зависит, что распознаватель вообще видит.
func TestForeignIDPNameSplitterKnowsTheWritingForms(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"testHydraIss":          {"test", "Hydra", "Iss"},
		"HydraClientID":         {"Hydra", "Client", "ID"},
		"saw_kratos":            {"saw", "kratos"},
		"oryFlowsAnchor":        {"ory", "Flows", "Anchor"},
		"KACHO_HYDRA_ADMIN_URL": {"KACHO", "HYDRA", "ADMIN", "URL"},
		"repository":            {"repository"},
		"kid2Rotation":          {"kid", "2", "Rotation"},
	}
	for in, want := range cases {
		got := SplitIdentifierWords(in)
		if strings.Join(got, "·") != strings.Join(want, "·") {
			t.Errorf("SplitIdentifierWords(%q) = %v, ожидалось %v", in, got, want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОВЕНАНС ЧИСЛА: ФОРМА И РАЗРЕШИМОСТЬ — ДВЕ ОСИ, И КАЖДАЯ ДОКАЗЫВАЕТСЯ
// ОДНО-ФАКТНОЙ ПАРОЙ
//
// Первая редакция судила строку НА НЕПУСТОТУ, а объявляла, что судит ревизию.
// Пара инъекции была двух-фактной: дефектом стояла пустая строка, близнецом —
// настоящая ревизия, и между ними менялось ДВА факта сразу — «непустая» и
// «является ревизией». Такая пара зелёная при любом из двух свойств, и предикат
// молчал на «a», «нет», заглушке, выдуманном наборе знаков и на ЧУЖОЙ ревизии.
//
// Цена последнего и есть та самая ложь, которой красное этой полосы началось:
// число, снятое с чужой головы, объявленное снятым здесь, — и гейт зелёный.
//
// Поэтому осей две, и у каждой пара меняет РОВНО ОДИН факт:
//   форма        — «a» против «5b20df5c638» при разрешителе, принимающем всё;
//   разрешимость — две строки ОДИНАКОВОЙ формы, из которых дерево знает одну.

// injResolveAll — разрешитель, принимающий всё: изолирует ось ФОРМЫ.
func injResolveAll(string) error { return nil }

// injResolveOnly — разрешитель, знающий ровно одну ревизию: изолирует ось
// РАЗРЕШИМОСТИ, оставляя форму обоих концов пары одинаковой.
func injResolveOnly(known string) ForeignIDPNameRevisionResolver {
	return func(rev string) error {
		if rev == known {
			return nil
		}
		return fmt.Errorf("объекта %s в этом дереве нет", rev)
	}
}

// injLedgerMeasured — ведомость из одной записи с названной ревизией.
func injLedgerMeasured(rev string) []ForeignIDPNameLedgerEntry {
	return []ForeignIDPNameLedgerEntry{{
		Area: "gateway/", Names: 2, Why: "w", Until: "полоса края закончена", Measured: rev,
	}}
}

// TestForeignIDPNameInjection_RevisionShapeIsJudgedNotEmptiness — ОСЬ ФОРМЫ,
// пара одно-фактная: обе строки непустые, разрешитель принимает обе, и
// различает их ровно одно — похоже ли значение на ревизию.
func TestForeignIDPNameInjection_RevisionShapeIsJudgedNotEmptiness(t *testing.T) {
	t.Parallel()
	if gaps := ForeignIDPNameProvenanceGaps(injLedgerMeasured("a"), injResolveAll); len(gaps) != 1 {
		t.Fatalf("непустое значение, не являющееся ревизией, принято: пропусков %d (%v)",
			len(gaps), gaps)
	}
	if gaps := ForeignIDPNameProvenanceGaps(injLedgerMeasured("5b20df5c638"), injResolveAll); len(gaps) != 0 {
		t.Fatalf("настоящая ревизия сочтена находкой: %v", gaps)
	}
}

// TestForeignIDPNameInjection_EveryNonRevisionStandInIsFound — тот перебор,
// которым предикат первой редакции и был опровергнут: ни одно из этих значений
// ревизией не является, и каждое обязано находиться.
//
// Здесь важен не объём, а то, что молчание хотя бы на одном возвращает ровно ту
// неразличимость, ради снятия которой ось и заведена.
func TestForeignIDPNameInjection_EveryNonRevisionStandInIsFound(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"", " ", "a", "нет", "TBD", "позже", "HEAD", "main", "0"} {
		if gaps := ForeignIDPNameProvenanceGaps(injLedgerMeasured(v), injResolveAll); len(gaps) != 1 {
			t.Errorf("значение %q принято за ревизию: пропусков %d", v, len(gaps))
		}
	}
}

// TestForeignIDPNameInjection_UnresolvableRevisionIsFound — ОСЬ РАЗРЕШИМОСТИ,
// пара одно-фактная: обе строки ОДНОЙ формы (одиннадцать шестнадцатеричных
// знаков), и различает их ровно одно — знает ли эту ревизию дерево.
//
// Дефект настоящий: так выглядит число, снятое обходом ЧУЖОГО дерева.
func TestForeignIDPNameInjection_UnresolvableRevisionIsFound(t *testing.T) {
	t.Parallel()
	const known, foreign = "5b20df5c638", "0123456789a"
	if len(known) != len(foreign) {
		t.Fatalf("концы пары разной формы (%d против %d) — менялся бы не один факт",
			len(known), len(foreign))
	}
	resolve := injResolveOnly(known)
	gaps := ForeignIDPNameProvenanceGaps(injLedgerMeasured(foreign), resolve)
	if len(gaps) != 1 {
		t.Fatalf("ревизия, которой в дереве нет, принята: пропусков %d (%v)", len(gaps), gaps)
	}
	if !strings.Contains(gaps[0], foreign) {
		t.Errorf("находка не называет неразрешённой ревизии: %q", gaps[0])
	}
	if gaps := ForeignIDPNameProvenanceGaps(injLedgerMeasured(known), resolve); len(gaps) != 0 {
		t.Fatalf("разрешимая ревизия сочтена находкой: %v", gaps)
	}
}

// TestForeignIDPNameInjection_ProvenanceGapNamesItsEntry — находка называет
// координату записи: пропуск без области чинить негде.
func TestForeignIDPNameInjection_ProvenanceGapNamesItsEntry(t *testing.T) {
	t.Parallel()
	gaps := ForeignIDPNameProvenanceGaps(injLedgerMeasured("TBD"), injResolveAll)
	if len(gaps) != 1 {
		t.Fatalf("пропусков %d, ожидался 1", len(gaps))
	}
	if !strings.Contains(gaps[0], "gateway/") {
		t.Errorf("находка не называет координаты записи: %q", gaps[0])
	}
}

// TestForeignIDPNameInjection_EmptyLedgerHasNoProvenanceGaps — пустая ведомость
// — ЦЕЛЬ гейта, а не поломка: пропусков в ней нет.
func TestForeignIDPNameInjection_EmptyLedgerHasNoProvenanceGaps(t *testing.T) {
	t.Parallel()
	if gaps := ForeignIDPNameProvenanceGaps(nil, injResolveAll); len(gaps) != 0 {
		t.Fatalf("пустая ведомость дала пропуски %v — проба падает на достижении своей цели", gaps)
	}
}

// TestForeignIDPNameInjection_DriftFindingCarriesTheEntryRevision — ТЕКСТ
// находки есть часть свойства.
//
// Находка дрейфа говорила «объявляет столько-то, в дереве столько-то» и не
// называла ревизии записи — то есть оставляла читающему ровно ту
// неразличимость, ради снятия которой ревизия и заводилась: «поверхность
// выросла» и «число снято с другой головы» выглядели одинаково.
//
// Пара одно-фактная: ведомости различаются РОВНО ревизией записи, и текст
// находки обязан различаться так же.
func TestForeignIDPNameInjection_DriftFindingCarriesTheEntryRevision(t *testing.T) {
	t.Parallel()
	const body = `package p

func hydraClaims() {}
`
	for _, rev := range []string{"bec320cf47d", "5b20df5c638"} {
		ledger := []ForeignIDPNameLedgerEntry{{
			Area: "gateway/", Names: 7, Why: "w", Until: "полоса края закончена", Measured: rev,
		}}
		findings, _, err := JudgeForeignIDPNames(injSources("gateway/internal/a_test.go", body), ledger)
		if err != nil {
			t.Fatalf("разбор: %v", err)
		}
		if !findingAt(findings, ForeignIDPNameCountDrift, "") {
			t.Fatalf("дрейф не найден: %+v", findings)
		}
		if !strings.Contains(findings[0].Detail, rev) {
			t.Errorf("текст находки дрейфа не называет ревизии записи %q: %q", rev, findings[0].Detail)
		}
	}
}

// TestForeignIDPNameInjection_StaleFindingCarriesTheEntryRevision — то же о
// записи, пережившей предмет: её число тоже снято на какой-то голове, и без неё
// «предмет ушёл» неотличимо от «число было не отсюда».
func TestForeignIDPNameInjection_StaleFindingCarriesTheEntryRevision(t *testing.T) {
	t.Parallel()
	const body = `package p

func ownClaims() {}
`
	ledger := []ForeignIDPNameLedgerEntry{{
		Area: "gateway/", Names: 2, Why: "w", Until: "полоса края закончена",
		Measured: "bec320cf47d",
	}}
	findings, _, err := JudgeForeignIDPNames(injSources("gateway/internal/a_test.go", body), ledger)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if !findingAt(findings, ForeignIDPNameStale, "") {
		t.Fatalf("пережившая запись не найдена: %+v", findings)
	}
	if !strings.Contains(findings[0].Detail, "bec320cf47d") {
		t.Errorf("текст находки не называет ревизии записи: %q", findings[0].Detail)
	}
}
