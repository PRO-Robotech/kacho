// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identityprobefate_injection_test.go — гейт ведомости судьбы проб СПОСОБЕН
// упасть и СПОСОБЕН смолчать (задача #2731).
//
// Вход — СИНТЕТИКА: документ и состав дерева собираются здесь же. Живая
// ведомость для самопроверки не годится — она убывает до нуля по ходу снятия, и
// самопроверка, привязанная к её строкам, истекла бы вместе с ними.
//
// Каждая инъекция меняет РОВНО ОДИН факт против законного близнеца, и близнец
// прогоняется первым. Находка проверяется по ТЕКСТУ: гейт обязан назвать
// причину и координату, а не «что-то не так».
package repohygiene

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// probeFateRow — строка синтетической ведомости.
type probeFateRow struct{ file, fate, coords string }

// probeFateDoc — синтетический документ: ведомость и разбивка, посчитанная по
// строкам (законный близнец), либо переданная явно.
func probeFateDoc(rows []probeFateRow, totals map[string]string, total string) string {
	var b strings.Builder
	b.WriteString("# Ведомость\n\nПроза: `:1` в прозе координатой не является.\n\n")
	b.WriteString("| " + strings.Join(identityProbeFateHeader, " | ") + " |\n")
	b.WriteString("|---:|---|---|---|---|---|\n")
	for i, r := range rows {
		fmt.Fprintf(&b, "| %d | `%s` | утверждение | наша полоса | %s | %s |\n", i+1, r.file, r.fate, r.coords)
	}
	b.WriteString("\n## Разбивка\n\n| исход | файлов |\n|---|---:|\n")
	if totals == nil {
		counts := map[string]int{}
		for _, r := range rows {
			counts[r.fate]++
		}
		totals = map[string]string{}
		for _, f := range identityProbeFates {
			totals[f] = fmt.Sprint(counts[f])
		}
		total = fmt.Sprint(len(rows))
	}
	for _, f := range identityProbeFates {
		if v, ok := totals[f]; ok {
			fmt.Fprintf(&b, "| %s | %s |\n", f, v)
		}
	}
	for k, v := range totals {
		if !knownFate(k) {
			fmt.Fprintf(&b, "| %s | %s |\n", k, v)
		}
	}
	if total != "" {
		fmt.Fprintf(&b, "| **итого** | **%s** |\n", total)
	}
	return b.String()
}

// probeFateTree — синтетический индекс: пробы и число строк каждого файла.
func probeFateTree(probes ...string) identityProbeFateFacts {
	f := identityProbeFateFacts{Lines: map[string]int{"deploy/helm/umbrella/values.yaml": 900}}
	for _, p := range probes {
		f.Probes = append(f.Probes, p)
		f.Lines["deploy/"+p] = 100
	}
	return f
}

// probeFateLawfulRows — законный близнец: три пробы, три исхода, у каждой довод в себе.
func probeFateLawfulRows() []probeFateRow {
	return []probeFateRow{
		{"identity_alpha_test.go", identityFateRemove, "`:10-20`"},
		{"identity_beta_test.go", identityFateKeep, "`deploy/identity_beta_test.go:5` · `deploy/helm/umbrella/values.yaml:800`"},
		{"identity_gamma_test.go", identityFateRewrite, "`:1` и `:100`"},
	}
}

func probeFateLawfulTree() identityProbeFateFacts {
	return probeFateTree("identity_alpha_test.go", "identity_beta_test.go", "identity_gamma_test.go")
}

// probeFateJudge — разбор и суд одним вызовом; отказ разбора — фатален для пробы.
func probeFateJudge(t *testing.T, doc string, f identityProbeFateFacts) ([]string, identityProbeFateCensus) {
	t.Helper()
	l, err := parseIdentityProbeFateLedger(doc, "deploy")
	if err != nil {
		t.Fatalf("разбор синтетики отказал: %v", err)
	}
	return judgeIdentityProbeFate(l, f, "deploy", "ведомость.md")
}

// probeFateOneFinding — ровно одна находка, и она несёт каждый из фрагментов.
func probeFateOneFinding(t *testing.T, found []string, want ...string) {
	t.Helper()
	if len(found) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n%s", len(found), strings.Join(found, "\n"))
	}
	for _, w := range want {
		if !strings.Contains(found[0], w) {
			t.Fatalf("находка не называет %q:\n%s", w, found[0])
		}
	}
}

func TestIdentityProbeFateInjection_LawfulLedgerIsSilent(t *testing.T) {
	t.Parallel()
	found, c := probeFateJudge(t, probeFateDoc(probeFateLawfulRows(), nil, ""), probeFateLawfulTree())
	if len(found) != 0 {
		t.Fatalf("законная ведомость дала находки:\n%s", strings.Join(found, "\n"))
	}
	if c.Rows != 3 || c.Probes != 3 || c.Coords != 5 || c.OwnCoords != 4 {
		t.Fatalf("перепись законной ведомости неверна: %s", c)
	}
	for _, f := range identityProbeFates {
		if c.ByFate[f] != 1 {
			t.Fatalf("исход %q посчитан %d раз, а не 1: %s", f, c.ByFate[f], c)
		}
	}
}

func TestIdentityProbeFateInjection_ProbeWithoutARowIsAFinding(t *testing.T) {
	t.Parallel()
	rows := probeFateLawfulRows()
	tree := probeFateTree("identity_alpha_test.go", "identity_beta_test.go", "identity_gamma_test.go", "identity_delta_test.go")
	// Разбивка верна для трёх строк, итог — четыре файла: меняется ровно один
	// факт — строки о четвёртой пробе нет.
	doc := probeFateDoc(rows, map[string]string{identityFateRemove: "1", identityFateKeep: "1", identityFateRewrite: "1"}, "4")
	found, _ := probeFateJudge(t, doc, tree)
	probeFateOneFinding(t, found, "deploy/identity_delta_test.go", "строки о нём", "не названа")
}

func TestIdentityProbeFateInjection_RowWithoutAProbeIsAFinding(t *testing.T) {
	t.Parallel()
	rows := append(probeFateLawfulRows(), probeFateRow{"identity_retired_test.go", identityFateRemove, "`:1`"})
	doc := probeFateDoc(rows, map[string]string{identityFateRemove: "2", identityFateKeep: "1", identityFateRewrite: "1"}, "3")
	found, _ := probeFateJudge(t, doc, probeFateLawfulTree())
	// Файла нет — значит и координата в нём не резолвится: находок две, и обе
	// называют ту же строку ведомости. Проверяется, что первая — о составе.
	var bySubject []string
	for _, f := range found {
		if strings.Contains(f, "пережила свою пробу") {
			bySubject = append(bySubject, f)
		}
	}
	if len(bySubject) != 1 || !strings.Contains(bySubject[0], "identity_retired_test.go") {
		t.Fatalf("строка без пробы не названа находкой о составе:\n%s", strings.Join(found, "\n"))
	}
	for _, f := range found {
		if !strings.Contains(f, "identity_retired_test.go") && !strings.Contains(f, "разбивка") {
			t.Fatalf("находка не о внесённом дефекте: %s", f)
		}
	}
}

func TestIdentityProbeFateInjection_GroupRowIsAFinding(t *testing.T) {
	t.Parallel()
	for _, group := range []string{"identity_*_test.go", "identity_alpha_test.go, identity_beta_test.go", "deploy/identity_alpha_test.go"} {
		rows := probeFateLawfulRows()
		rows[0].file = group
		found, _ := probeFateJudge(t, probeFateDoc(rows, map[string]string{identityFateRemove: "0", identityFateKeep: "1", identityFateRewrite: "1"}, "3"), probeFateLawfulTree())
		var group1 bool
		for _, f := range found {
			if strings.Contains(f, "Групповая запись") && strings.Contains(f, group) {
				group1 = true
			}
		}
		if !group1 {
			t.Fatalf("групповая запись %q не названа находкой:\n%s", group, strings.Join(found, "\n"))
		}
	}
}

func TestIdentityProbeFateInjection_DuplicateRowIsAFinding(t *testing.T) {
	t.Parallel()
	rows := append(probeFateLawfulRows(), probeFateRow{"identity_alpha_test.go", identityFateKeep, "`:2`"})
	doc := probeFateDoc(rows, map[string]string{identityFateRemove: "1", identityFateKeep: "1", identityFateRewrite: "1"}, "3")
	found, _ := probeFateJudge(t, doc, probeFateLawfulTree())
	probeFateOneFinding(t, found, "identity_alpha_test.go", "второй раз")
}

func TestIdentityProbeFateInjection_FateOutsideTheDictionaryIsAFinding(t *testing.T) {
	t.Parallel()
	for _, fate := range []string{"перевести", "прочее", "снять вместе с предметом", ""} {
		rows := probeFateLawfulRows()
		rows[0].fate = fate
		doc := probeFateDoc(rows, map[string]string{identityFateRemove: "0", identityFateKeep: "1", identityFateRewrite: "1"}, "3")
		found, _ := probeFateJudge(t, doc, probeFateLawfulTree())
		probeFateOneFinding(t, found, "identity_alpha_test.go", "вне закрытого словаря")
	}
}

func TestIdentityProbeFateInjection_RowWithoutACoordinateIsAFinding(t *testing.T) {
	t.Parallel()
	rows := probeFateLawfulRows()
	rows[0].coords = "довод словами, без координаты"
	found, _ := probeFateJudge(t, probeFateDoc(rows, nil, ""), probeFateLawfulTree())
	probeFateOneFinding(t, found, "identity_alpha_test.go", "в самой пробе")
}

func TestIdentityProbeFateInjection_CoordinateOnlyElsewhereIsAFinding(t *testing.T) {
	t.Parallel()
	rows := probeFateLawfulRows()
	rows[0].coords = "`deploy/helm/umbrella/values.yaml:12`"
	found, _ := probeFateJudge(t, probeFateDoc(rows, nil, ""), probeFateLawfulTree())
	probeFateOneFinding(t, found, "identity_alpha_test.go", "в самой пробе")
}

func TestIdentityProbeFateInjection_CoordinatePastTheEndIsAFinding(t *testing.T) {
	t.Parallel()
	for _, coord := range []string{"`:101`", "`:90-101`", "`:0`", "`:20-10`"} {
		rows := probeFateLawfulRows()
		rows[0].coords = "`:10` · " + coord
		found, _ := probeFateJudge(t, probeFateDoc(rows, nil, ""), probeFateLawfulTree())
		probeFateOneFinding(t, found, "identity_alpha_test.go", coord, "вне файла")
	}
}

func TestIdentityProbeFateInjection_CoordinateIntoAnUntrackedFileIsAFinding(t *testing.T) {
	t.Parallel()
	// Путь, записанный от каталога проб, а не от корня: та же форма, другой
	// файл. Разбор обязан назвать его, а не молча отбросить.
	rows := probeFateLawfulRows()
	rows[1].coords = "`:5` · `helm/umbrella/values.yaml:800`"
	found, _ := probeFateJudge(t, probeFateDoc(rows, nil, ""), probeFateLawfulTree())
	probeFateOneFinding(t, found, "identity_beta_test.go", "helm/umbrella/values.yaml:800", "в индексе нет")
}

func TestIdentityProbeFateInjection_TotalsThatDisagreeAreAFinding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		totals map[string]string
		total  string
		want   []string
	}{
		{"число исхода", map[string]string{identityFateRemove: "2", identityFateKeep: "1", identityFateRewrite: "1"}, "3",
			[]string{"«снять 2»", "строк ведомости с этим исходом 1"}},
		{"итог", map[string]string{identityFateRemove: "1", identityFateKeep: "1", identityFateRewrite: "1"}, "4",
			[]string{"итог разбивки 4", "в индексе 3"}},
		{"исход без строки", map[string]string{identityFateRemove: "1", identityFateKeep: "1"}, "3",
			[]string{"нет строки исхода", identityFateRewrite}},
		{"итога нет", map[string]string{identityFateRemove: "1", identityFateKeep: "1", identityFateRewrite: "1"}, "",
			[]string{"нет итоговой строки"}},
		{"исход вне словаря", map[string]string{identityFateRemove: "1", identityFateKeep: "1", identityFateRewrite: "1", "перевести": "0"}, "3",
			[]string{"в разбивке исход", "перевести"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, _ := probeFateJudge(t, probeFateDoc(probeFateLawfulRows(), tc.totals, tc.total), probeFateLawfulTree())
			probeFateOneFinding(t, found, tc.want...)
		})
	}
}

func TestIdentityProbeFateInjection_UnrecognisedFormIsARefusal(t *testing.T) {
	t.Parallel()
	lawful := probeFateDoc(probeFateLawfulRows(), nil, "")
	header := "| " + strings.Join(identityProbeFateHeader, " | ") + " |"
	cases := []struct {
		name string
		doc  string
		want error
	}{
		{"заголовок переименован", strings.Replace(lawful, header, strings.Replace(header, "исход", "судьба", 1), 1), errIdentityProbeFateNoLedger},
		{"ведомость дважды", lawful + "\n" + header + "\n", errIdentityProbeFateTwoLedgers},
		{"разбивки нет", strings.Replace(lawful, "| исход | файлов |", "| исходы | файлов |", 1), errIdentityProbeFateNoTotals},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseIdentityProbeFateLedger(tc.doc, "deploy"); !errors.Is(err, tc.want) {
				t.Fatalf("ожидался отказ %v, получено %v", tc.want, err)
			}
		})
	}
}

func TestIdentityProbeFateInjection_MalformedRowIsAFinding(t *testing.T) {
	t.Parallel()
	doc := strings.Replace(probeFateDoc(probeFateLawfulRows(), nil, ""), "| 1 | `identity_alpha_test.go` | утверждение | наша полоса |",
		"| 1 | `identity_alpha_test.go` | утверждение |", 1)
	found, _ := probeFateJudge(t, doc, probeFateLawfulTree())
	var malformed, missing bool
	for _, f := range found {
		malformed = malformed || strings.Contains(f, "не 6 ячеек")
		missing = missing || strings.Contains(f, "deploy/identity_alpha_test.go в индексе есть")
	}
	if !malformed || !missing {
		t.Fatalf("строка с недостающей ячейкой не названа (и её проба — не названа):\n%s", strings.Join(found, "\n"))
	}
}

// Пустая ведомость при пустом каталоге — цель снятия, а не поломка.
func TestIdentityProbeFateInjection_EmptyLedgerIsTheGoalNotAFailure(t *testing.T) {
	t.Parallel()
	found, c := probeFateJudge(t, probeFateDoc(nil, nil, ""), probeFateTree())
	if len(found) != 0 {
		t.Fatalf("пустая ведомость при нуле проб дала находки:\n%s", strings.Join(found, "\n"))
	}
	if c.Rows != 0 || c.Probes != 0 || !strings.Contains(c.String(), "строк ведомости 0 · файлов в дереве 0") {
		t.Fatalf("перепись пустой ведомости не говорит о нуле: %s", c)
	}
	// Та же пустая ведомость при одной пробе — уже находка.
	found, _ = probeFateJudge(t, probeFateDoc(nil, nil, ""), probeFateTree("identity_alpha_test.go"))
	var named bool
	for _, f := range found {
		named = named || strings.Contains(f, "deploy/identity_alpha_test.go в индексе есть")
	}
	if !named {
		t.Fatalf("пустая ведомость при живой пробе промолчала о ней:\n%s", strings.Join(found, "\n"))
	}
}
