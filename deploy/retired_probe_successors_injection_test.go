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
	lines, findings := judgeSuccessions(ledger, table, locate)
	if len(findings) != 0 || len(lines) != 1 ||
		!strings.Contains(lines[0], "TestHeir") || !strings.Contains(lines[0], "TestPinHeir") {
		t.Fatalf("законный близнец: строки %v, находки %v — ждали одну строку свойства с обоими "+
			"преемниками и молчание", lines, findings)
	}

	cases := []struct {
		name, want string
		mutate     func(map[int]fateRow, []ledgerSuccession) successorLocator
	}{
		{"строки нет в ведомости", "строки 2 в ведомости нет", func(l map[int]fateRow, _ []ledgerSuccession) successorLocator {
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
		_, got := judgeSuccessions(l, tb, loc)
		if !strings.Contains(strings.Join(got, "\n"), c.want) {
			t.Errorf("%s: находка %q не выдана; находки: %v", c.name, c.want, got)
		}
	}
	t.Logf("перепись: инъекций %d · близнец 1", len(cases))
}

func TestLedgerRows_RefusesAnUnknownRowForm(t *testing.T) {
	good := "| # | файл |\n|---:|---|\n| 1 | `identity_a_test.go` | x | y | снять | z |\n"
	rows, err := parseLedgerRows(good)
	if err != nil || rows[1].File != "identity_a_test.go" || rows[1].Outcome != "снять" {
		t.Fatalf("строка ведомости прочитана как %+v (ошибка %v)", rows, err)
	}
	if _, err := parseLedgerRows("| # | файл |\n"); err == nil {
		t.Error("ведомость без строк принята — обход пуст")
	}
	if _, err := parseLedgerRows(good + "| 2 | `identity_b_test.go` | лишний | столбец | x | снять | z |\n"); err == nil {
		t.Error("строка с лишним столбцом принята молча — исход читался бы не из своей ячейки")
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
