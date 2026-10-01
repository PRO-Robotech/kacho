// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retired_probe_successors_injection_test.go — судья преемников
// (retired_probe_successors_test.go) краснеет на каждом нарушении и молчит на
// законном близнеце. Вход — синтетическая ведомость и синтетический ответ
// «где объявлена проба»: каждая инъекция меняет ровно один факт против близнеца.
package deploy_test

import (
	"errors"
	"strings"
	"testing"
)

// fixtureProbes — пробы, лежащие в синтетическом дереве.
var fixtureProbes = map[string]bool{"identity_a_test.go": true, "identity_b_test.go": true, "identity_c_test.go": true}

func fixturePresent(file string) bool { return fixtureProbes[file] }

func successorFixture() (map[int]fateRow, []ledgerSuccession, successorLocator) {
	ledger := map[int]fateRow{
		1: {Number: 1, File: "identity_a_test.go", Outcome: "снять"},
		2: {Number: 2, File: "identity_b_test.go", Outcome: "переписать"},
		3: {Number: 3, File: "identity_c_test.go", Outcome: "оставить"},
	}
	table := []ledgerSuccession{{
		Rows:  []fateRowRef{{1, "identity_a_test.go"}, {2, "identity_b_test.go"}},
		What:  "свойство",
		Heirs: []probeSuccessor{{successorTreePlatform, "deploy/heir_test.go", "TestHeir"}, {successorTreeAccess, "internal/x/heir_test.go", "TestPinHeir"}},
	}}
	locate := func(tree, file, fn string) (int, error) {
		if fn == "TestHeir" || fn == "TestPinHeir" {
			return 7, nil
		}
		return 0, errors.New("не объявлена")
	}
	return ledger, table, locate
}

func TestSuccessorJudge_TwinIsSilentAndEachDefectIsFound(t *testing.T) {
	ledger, table, locate := successorFixture()
	lines, findings := judgeSuccessions(ledger, table, locate, fixturePresent)
	if len(findings) != 0 || len(lines) != 1 ||
		!strings.Contains(lines[0], "TestHeir") || !strings.Contains(lines[0], "TestPinHeir") {
		t.Fatalf("законный близнец: строки %v, находки %v — ждали одну строку свойства с обоими "+
			"преемниками и молчание", lines, findings)
	}

	cases := []struct {
		name, want string
		mutate     func(map[int]fateRow, []ledgerSuccession) successorLocator
	}{
		{"строки нет в ведомости, а проба в дереве есть", "строки 2 в ведомости нет", func(l map[int]fateRow, _ []ledgerSuccession) successorLocator {
			delete(l, 2)
			return locate
		}},
		{"ведомость перенумерована", "ведомость называет", func(l map[int]fateRow, _ []ledgerSuccession) successorLocator {
			l[1] = fateRow{Number: 1, File: "identity_other_test.go", Outcome: "снять"}
			return locate
		}},
		{"проба остаётся", "исход «оставить»", func(_ map[int]fateRow, tb []ledgerSuccession) successorLocator {
			tb[0].Rows = append(tb[0].Rows, fateRowRef{3, "identity_c_test.go"})
			return locate
		}},
		{"преемник не объявлен", "TestGone", func(_ map[int]fateRow, tb []ledgerSuccession) successorLocator {
			tb[0].Heirs = append(tb[0].Heirs, probeSuccessor{successorTreePlatform, "deploy/heir_test.go", "TestGone"})
			return locate
		}},
		{"преемник сам снимается", "сам снимается", func(_ map[int]fateRow, tb []ledgerSuccession) successorLocator {
			tb[0].Heirs = append(tb[0].Heirs, probeSuccessor{successorTreePlatform, "deploy/identity_a_test.go", "TestHeir"})
			return locate
		}},
		{"преемников нет", "преемник не назван", func(_ map[int]fateRow, tb []ledgerSuccession) successorLocator {
			tb[0].Heirs = nil
			return locate
		}},
	}
	for _, c := range cases {
		l, tb, _ := successorFixture()
		loc := c.mutate(l, tb)
		_, got := judgeSuccessions(l, tb, loc, fixturePresent)
		if !strings.Contains(strings.Join(got, "\n"), c.want) {
			t.Errorf("%s: находка %q не выдана; находки: %v", c.name, c.want, got)
		}
	}
	t.Logf("перепись: инъекций %d · близнец 1", len(cases))
}

// Проба, снятая вместе со своей строкой (правило ведомости: «снята проба — тем
// же изменением снимаются её строка»), остаётся в таблице преемников: её класс
// обязан держать объявленный преемник. Близнец меняет один факт — файл пробы в
// дереве остался, и тогда отсутствие строки — находка.
func TestSuccessorJudge_ProbeRemovedWithItsRowStillNeedsItsHeirs(t *testing.T) {
	ledger, table, locate := successorFixture()
	delete(ledger, 2)
	gone := func(f string) bool { return f != "identity_b_test.go" && fixturePresent(f) }

	lines, findings := judgeSuccessions(ledger, table, locate, gone)
	if len(findings) != 0 || len(lines) != 1 || !strings.Contains(lines[0], "снята вместе со строкой") {
		t.Fatalf("снятая вместе со строкой проба: строки %v, находки %v — ждали строку свойства со снятой "+
			"пробой и молчание", lines, findings)
	}

	_, findings = judgeSuccessions(ledger, table, locate, fixturePresent)
	if !strings.Contains(strings.Join(findings, "\n"), "строки 2 в ведомости нет") {
		t.Errorf("строки нет, а файл пробы в дереве есть — находки нет: %v", findings)
	}

	table[0].Heirs = []probeSuccessor{{successorTreeAccess, "internal/x/heir_test.go", "TestGone"}}
	_, findings = judgeSuccessions(ledger, table, locate, gone)
	if !strings.Contains(strings.Join(findings, "\n"), "TestGone") {
		t.Errorf("у снятой пробы преемник не объявлен — находки нет: %v", findings)
	}
}

// ledgerHeaderFixture — заголовок ведомости и разделитель под ним, как их пишет
// документ.
const ledgerHeaderFixture = "| # | файл | что утверждает | предпосылка | исход | основание (координата) |\n" +
	"|---:|---|---|---|---|---|\n"

func TestLedgerRows_RefusesAnUnknownRowForm(t *testing.T) {
	good := ledgerHeaderFixture + "| 1 | `identity_a_test.go` | x | y | снять | z |\n"
	rows, err := parseLedgerRows(good)
	if err != nil || rows[1].File != "identity_a_test.go" || rows[1].Outcome != "снять" {
		t.Fatalf("строка ведомости прочитана как %+v (ошибка %v)", rows, err)
	}
	if _, err := parseLedgerRows(ledgerHeaderFixture); err == nil {
		t.Error("ведомость без строк принята — обход пуст")
	}
	if _, err := parseLedgerRows(good + "| 2 | `identity_b_test.go` | лишний | столбец | x | снять | z |\n"); err == nil {
		t.Error("строка с лишним столбцом принята молча — исход читался бы не из своей ячейки")
	}
}

// Документ ведомости несёт и ДРУГИЕ таблицы с номером в первой ячейке — решение
// по гейтам признака (семь ячеек) и судьбу проб вне ведомости (шесть ячеек, но
// исход — в третьей, и номера свои: kacho#1276). Строкой ведомости они не
// являются: номер 34 записи о пробах вне ведомости — не прежняя строка 34
// ведомости, и прочитанный как она он объявил бы ведомость перенумерованной.
// Ведомость узнаётся по своему заголовку — тому же, по которому её находит гейт
// документа (internal/repohygiene/identityprobefate.go), — и кончается первой
// строкой, таблицей не являющейся. Близнец — та же ведомость без чужих таблиц.
func TestLedgerRows_ReadsOnlyTheLedgerTable(t *testing.T) {
	ledger := ledgerHeaderFixture +
		"| 1 | `identity_a_test.go` | x | y | снять | z |\n" +
		"| 2 | `identity_b_test.go` | x | y | переписать | z |\n"
	gates := "\n### А. Решение по гейтам признака\n\n" +
		"| # | гейт | исход | снят | класс | судьба класса | основание |\n" +
		"|---:|---|---|---|---|---|---|\n" +
		"| 1 | `identity_other_test.go` | снять | #1 | к | невоспроизводим | о |\n"
	beyond := "\n### Б. Пробы вне ведомости\n\n" +
		"| # | проба | исход | исполнено | фрагмент имени | довод |\n" +
		"|---:|---|---|---|---|---|\n" +
		"| 34 | `deploy/tests/helm/x-inject.sh` | переписать | да | — | д |\n"
	foreign := gates + beyond

	twin, err := parseLedgerRows(ledger)
	if err != nil || len(twin) != 2 {
		t.Fatalf("законный близнец: строки %+v, ошибка %v — ждали две строки ведомости", twin, err)
	}
	for name, doc := range map[string]string{"чужие таблицы после ведомости": ledger + foreign, "чужие таблицы до ведомости": foreign + "\n" + ledger} {
		got, err := parseLedgerRows(doc)
		if err != nil {
			t.Errorf("%s: %v — чужая таблица прочитана как ведомость", name, err)
			continue
		}
		if len(got) != 2 || got[1].File != "identity_a_test.go" || got[1].Outcome != "снять" {
			t.Errorf("%s: строки %+v — ждали ровно две строки ведомости, строку 1 — её собственную", name, got)
		}
		if _, ok := got[34]; ok {
			t.Errorf("%s: строка 34 чужой таблицы прочитана как строка ведомости", name)
		}
	}

	// Таблица той же ширины без заголовка ведомости: ширина строки её не
	// отличает, отличает только заголовок.
	if rows, err := parseLedgerRows(beyond); err == nil {
		t.Errorf("документ без заголовка ведомости принят, строки %+v — они прочитаны из чужой таблицы", rows)
	}
	if _, err := parseLedgerRows(ledger + "\n" + ledger); err == nil {
		t.Error("два заголовка ведомости приняты — какая из таблиц судит, не установлено")
	}
}

func TestDeclaredTestFunc_ReadsTheDeclarationNotTheText(t *testing.T) {
	src := "package x\n\n// func TestInComment(t *testing.T) {}\n" +
		"var s = \"func TestInString(t *testing.T)\"\n\n" +
		"func TestReal(t *testing.T) {}\n\n" +
		"type r struct{}\n\nfunc (r) TestMethod(t *testing.T) {}\n"
	if line, err := declaredFuncLine("x_test.go", []byte(src), "TestReal"); err != nil || line != 6 {
		t.Errorf("объявленная проба: строка %d, ошибка %v — ждали 6", line, err)
	}
	for _, fn := range []string{"TestInComment", "TestInString", "TestMethod"} {
		if _, err := declaredFuncLine("x_test.go", []byte(src), fn); err == nil {
			t.Errorf("%s принята за объявленную пробу — судится текст, а не объявление", fn)
		}
	}
}
