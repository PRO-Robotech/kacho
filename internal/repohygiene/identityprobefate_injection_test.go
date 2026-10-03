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
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// probeFateRow — строка синтетической ведомости.
type probeFateRow struct{ file, fate, coords string }

// probeFateDoc — синтетический документ: ведомость и разбивка, посчитанная по
// строкам (законный близнец), либо переданная явно.
func probeFateDoc(rows []probeFateRow, totals map[string]string, total string) string {
	var b strings.Builder
	b.WriteString("# Ведомость\n\nПроза: `:1` без пути координатой не является, а " + probeFateProse +
		" — является и судится так же, как координата строки.\n\n")
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
	b.WriteString("\n## Якоря\n\n| " + strings.Join(identityProbeFateAnchorsHeader, " | ") + " |\n|---|---|---|\n")
	for _, k := range probeFateKeys(rows) {
		b.WriteString(probeFateAnchorRow(k.Path, k.From, k.To))
	}
	return b.String()
}

// probeFateKeys — координаты синтетического документа в порядке записи, каждая
// один раз: строк ведомости и прозы. Путь, не выразимый координатой (ячейка
// групповой записи), якоря не получает — такую строку гейт отвергает раньше.
func probeFateKeys(rows []probeFateRow) []identityProbeCoordKey {
	var keys []identityProbeCoordKey
	seen := map[identityProbeCoordKey]bool{}
	add := func(k identityProbeCoordKey) {
		if !seen[k] && probeFatePathForm.MatchString(k.Path) {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, r := range rows {
		for _, m := range identityProbeCoord.FindAllStringSubmatch(r.coords, -1) {
			k := identityProbeCoordKey{Path: m[1]}
			if k.Path == "" {
				k.Path = "deploy/" + r.file
			}
			k.From, _ = strconv.Atoi(m[2])
			k.To = k.From
			if m[3] != "" {
				k.To, _ = strconv.Atoi(m[3])
			}
			add(k)
		}
	}
	add(identityProbeCoordKey{Path: probeFateProseTarget, From: 7, To: 7})
	return keys
}

// probeFatePathForm — путь, выразимый координатой.
var probeFatePathForm = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// probeFateAnchorRow — строка таблицы якорей законного близнеца: якорь — текст
// строки предмета в синтетическом файле.
func probeFateAnchorRow(path string, from, to int) string {
	coord := fmt.Sprintf("%s:%d", path, from)
	last := identityProbeAnchorNone
	if to != from {
		coord += fmt.Sprintf("-%d", to)
		last = "`" + probeFateLine(path, to) + "`"
	}
	return fmt.Sprintf("| `%s` | `%s` | %s |\n", coord, probeFateLine(path, from), last)
}

// probeFateLine — строка n синтетического файла: единственная в файле, и ни
// одна другая её не содержит («строка 1.» — не часть «строка 10.»).
func probeFateLine(path string, n int) string { return fmt.Sprintf("%s — строка %d.", path, n) }

// probeFateFile — синтетический файл из n строк.
func probeFateFile(path string, n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = probeFateLine(path, i+1)
	}
	return lines
}

// probeFateProseTarget — файл, в который указывает координата прозы: вне
// ведомости, чтобы её сдвиг не задевал ни одной строки ведомости.
const probeFateProseTarget = "deploy/own_reader_test.go"

// probeFateProse — координата прозы синтетического документа.
const probeFateProse = "`" + probeFateProseTarget + ":7`"

// probeFateTree — синтетический индекс: пробы и число строк каждого файла.
func probeFateTree(probes ...string) identityProbeFateFacts {
	f := identityProbeFateFacts{Text: map[string][]string{
		"deploy/helm/umbrella/values.yaml": probeFateFile("deploy/helm/umbrella/values.yaml", 900),
		probeFateProseTarget:               probeFateFile(probeFateProseTarget, 50),
	}}
	for _, p := range probes {
		f.Probes = append(f.Probes, p)
		f.Text["deploy/"+p] = probeFateFile("deploy/"+p, 100)
	}
	return f
}

// probeFateInsert — в файл path перед строкой at вставлено n строк (at на
// единицу больше длины — вставка в конец). Меняет ровно один факт: где стоят
// строки файла ниже вставки.
func probeFateInsert(f identityProbeFateFacts, path string, at, n int) identityProbeFateFacts {
	text := f.Text[path]
	out := append([]string{}, text[:at-1]...)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("вставленная строка %d", i+1))
	}
	f.Text[path] = append(out, text[at-1:]...)
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
	// Координата прозы судится наравне со строками ведомости, и у каждой из
	// шести координат якорь стоит на её строке.
	if c.OtherCoords != 1 || c.Anchors != 6 || c.OnSubject != 6 || c.SharedAnchors != 0 {
		t.Fatalf("перепись якорей законной ведомости неверна: %s", c)
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
	anchors := "| " + strings.Join(identityProbeFateAnchorsHeader, " | ") + " |"
	cases := []struct {
		name string
		doc  string
		want error
	}{
		{"заголовок переименован", strings.Replace(lawful, header, strings.Replace(header, "исход", "судьба", 1), 1), errIdentityProbeFateNoLedger},
		{"ведомость дважды", lawful + "\n" + header + "\n", errIdentityProbeFateTwoLedgers},
		{"разбивки нет", strings.Replace(lawful, "| исход | файлов |", "| исходы | файлов |", 1), errIdentityProbeFateNoTotals},
		{"якорей нет", strings.Replace(lawful, anchors, strings.Replace(anchors, "первая строка", "якорь", 1), 1), errIdentityProbeFateNoAnchors},
		{"якоря дважды", lawful + "\n" + anchors + "\n", errIdentityProbeFateTwoAnchorTables},
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

// probeFateSomeFinding — среди находок есть одна, несущая каждый из фрагментов.
func probeFateSomeFinding(t *testing.T, found []string, want ...string) {
	t.Helper()
	for _, f := range found {
		all := true
		for _, w := range want {
			all = all && strings.Contains(f, w)
		}
		if all {
			return
		}
	}
	t.Fatalf("ни одна находка не несёт %q:\n%s", want, strings.Join(found, "\n"))
}

// Класс, ради которого у координаты есть якорь (#2731, возврат сборки 1 волны
// 4): файл под координатой сдвинулся, номер строки остался в пределах файла, и
// прежняя проверка «строка существует» молчала. Близнец — та же вставка ПОД
// диапазоном: предмет на месте, находок нет.
func TestIdentityProbeFateInjection_ShiftedCoordinateIsAFinding(t *testing.T) {
	t.Parallel()
	doc := probeFateDoc(probeFateLawfulRows(), nil, "")
	const alpha = "deploy/identity_alpha_test.go"
	if found, _ := probeFateJudge(t, doc, probeFateInsert(probeFateLawfulTree(), alpha, 21, 3)); len(found) != 0 {
		t.Fatalf("вставка под диапазоном дала находки — законный близнец не молчит:\n%s", strings.Join(found, "\n"))
	}
	found, _ := probeFateJudge(t, doc, probeFateInsert(probeFateLawfulTree(), alpha, 5, 3))
	probeFateOneFinding(t, found, "identity_alpha_test.go", "`:10-20`", "сошла со своего предмета", "стоит на :13")
}

// Предмет вырос: строки вставлены ВНУТРИ диапазона. Первая строка на месте,
// а конец диапазона предмет больше не накрывает.
func TestIdentityProbeFateInjection_RangeThatNoLongerCoversItsSubjectIsAFinding(t *testing.T) {
	t.Parallel()
	doc := probeFateDoc(probeFateLawfulRows(), nil, "")
	const alpha = "deploy/identity_alpha_test.go"
	if found, _ := probeFateJudge(t, doc, probeFateInsert(probeFateLawfulTree(), alpha, 21, 2)); len(found) != 0 {
		t.Fatalf("вставка сразу за концом диапазона дала находки — законный близнец не молчит:\n%s", strings.Join(found, "\n"))
	}
	found, _ := probeFateJudge(t, doc, probeFateInsert(probeFateLawfulTree(), alpha, 15, 2))
	probeFateOneFinding(t, found, "identity_alpha_test.go", "`:10-20`", "конец диапазона", "стоит на :22")
}

// Координата вне строк ведомости (проза, соседние таблицы) судится так же:
// сдвиг её файла — находка с номером строки документа.
func TestIdentityProbeFateInjection_ShiftedProseCoordinateIsAFinding(t *testing.T) {
	t.Parallel()
	doc := probeFateDoc(probeFateLawfulRows(), nil, "")
	if found, _ := probeFateJudge(t, doc, probeFateInsert(probeFateLawfulTree(), probeFateProseTarget, 8, 2)); len(found) != 0 {
		t.Fatalf("вставка под координатой прозы дала находки:\n%s", strings.Join(found, "\n"))
	}
	found, _ := probeFateJudge(t, doc, probeFateInsert(probeFateLawfulTree(), probeFateProseTarget, 1, 2))
	probeFateOneFinding(t, found, "ведомость.md:3:", probeFateProse, "сошла со своего предмета", "стоит на :9")
}

// Строка предмета переписана при прежнем числе строк: якоря в файле нет вовсе.
func TestIdentityProbeFateInjection_RewrittenSubjectIsAFinding(t *testing.T) {
	t.Parallel()
	tree := probeFateLawfulTree()
	tree.Text[probeFateProseTarget][6] = "строка переписана"
	found, _ := probeFateJudge(t, probeFateDoc(probeFateLawfulRows(), nil, ""), tree)
	probeFateOneFinding(t, found, probeFateProse, "в файле его нет")
}

// Якорь, стоящий в файле не на одной строке, всё равно ловит сдвиг, а перепись
// называет число таких якорей: точность проверки видна, а не подразумевается.
func TestIdentityProbeFateInjection_SharedAnchorStillCatchesTheShift(t *testing.T) {
	t.Parallel()
	doc := probeFateDoc(probeFateLawfulRows(), nil, "")
	tree := probeFateLawfulTree()
	tree.Text[probeFateProseTarget][29] = probeFateLine(probeFateProseTarget, 7)
	found, c := probeFateJudge(t, doc, tree)
	if len(found) != 0 || c.SharedAnchors != 1 || c.OnSubject != 6 {
		t.Fatalf("якорь с двойником на своей строке: находок %d, перепись %s\n%s", len(found), c, strings.Join(found, "\n"))
	}
	found, _ = probeFateJudge(t, doc, probeFateInsert(tree, probeFateProseTarget, 1, 2))
	probeFateOneFinding(t, found, probeFateProse, "стоит на :9, :32")
}

func TestIdentityProbeFateInjection_CoordinateWithoutAnAnchorIsAFinding(t *testing.T) {
	t.Parallel()
	doc := strings.Replace(probeFateDoc(probeFateLawfulRows(), nil, ""), probeFateAnchorRow(probeFateProseTarget, 7, 7), "", 1)
	found, _ := probeFateJudge(t, doc, probeFateLawfulTree())
	probeFateOneFinding(t, found, probeFateProse, "без якоря")
}

func TestIdentityProbeFateInjection_AnchorWithoutACoordinateIsAFinding(t *testing.T) {
	t.Parallel()
	row := probeFateAnchorRow(probeFateProseTarget, 7, 7)
	lawful := probeFateDoc(probeFateLawfulRows(), nil, "")
	found, _ := probeFateJudge(t, strings.Replace(lawful, row, row+probeFateAnchorRow(probeFateProseTarget, 30, 30), 1), probeFateLawfulTree())
	probeFateOneFinding(t, found, probeFateProseTarget+":30", "пережил свою координату")
	found, _ = probeFateJudge(t, strings.Replace(lawful, row, row+row, 1), probeFateLawfulTree())
	probeFateOneFinding(t, found, probeFateProseTarget+":7", "второй раз")
}

func TestIdentityProbeFateInjection_UnreadableAnchorIsAFinding(t *testing.T) {
	t.Parallel()
	row := probeFateAnchorRow(probeFateProseTarget, 7, 7)
	anchor := "`" + probeFateLine(probeFateProseTarget, 7) + "`"
	rangeRow := probeFateAnchorRow("deploy/identity_alpha_test.go", 10, 20)
	cases := []struct {
		name, from, to string
		want           []string
	}{
		{"координата без пути", row, "| `:7` | " + anchor + " | — |\n", []string{"не читается", "`:7`"}},
		{"якорь без обратных кавычек", row, "| " + probeFateProse + " | " + strings.Trim(anchor, "`") + " | — |\n", []string{"не читается", probeFateProseTarget + ":7"}},
		{"одиночной — якорь конца", row, strings.Replace(row, "| — |", "| "+anchor+" |", 1), []string{"одиночной координаты", probeFateProseTarget + ":7"}},
		{"диапазону — без якоря конца", rangeRow, rangeRow[:strings.LastIndex(rangeRow, "| `")] + "| — |\n", []string{"последней строки", "identity_alpha_test.go:10-20"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := strings.Replace(probeFateDoc(probeFateLawfulRows(), nil, ""), tc.from, tc.to, 1)
			found, _ := probeFateJudge(t, doc, probeFateLawfulTree())
			probeFateSomeFinding(t, found, tc.want...)
		})
	}
}
