// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package knownfailingsubject

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Пометка `# verifies <тикет>` на кейсе — третья половина гейта. Каждая проба ниже
// поставлена парой: НАСТОЯЩИЙ вход (пометка, которую надо поймать) и ЗАКОННЫЙ БЛИЗНЕЦ той
// же формы (текст, на котором гейт обязан молчать). Одиночное отрицание здесь зеленело бы
// сильнее всего именно тогда, когда половина сломана целиком.

// annSuite собирает минимальную сюиту: коллекцию с одним кейсом-папкой, файл кейсов и
// (по желанию) отчёт прогона.
type annSuite struct {
	root  string
	suite string
}

func newAnnSuite(t *testing.T, caseIDs ...string) annSuite {
	t.Helper()
	root := t.TempDir()
	suite := filepath.Join("services", "fix", "tests", "newman")
	base := filepath.Join(root, suite)
	for _, d := range []string{"cases", "collections", "docs"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	// Документ отчёта без объявляющего раздела: первая половина гейта на нём молчит,
	// поэтому находки в пробах принадлежат ровно третьей.
	writeAnn(t, filepath.Join(base, "docs", "RESULTS.md"), "# Итоги\n\nОбъявлений нет.\n")

	items := make([]map[string]any, 0, len(caseIDs))
	for _, id := range caseIDs {
		items = append(items, map[string]any{
			"name": id + " — заголовок кейса",
			"item": []map[string]any{{"name": "step-1", "request": map[string]any{"method": "GET"}}},
		})
	}
	raw, err := json.Marshal(map[string]any{"item": items})
	if err != nil {
		t.Fatal(err)
	}
	writeAnn(t, filepath.Join(base, "collections", "coll.postman_collection.json"), string(raw))
	return annSuite{root: root, suite: suite}
}

func (f annSuite) cases(t *testing.T, body string) {
	t.Helper()
	writeAnn(t, filepath.Join(f.root, f.suite, "cases", "coll.py"), body)
}

// report кладёт отчёт прогона: executed — позиции курсора, реально исполнившиеся;
// failedNames — имена, под которыми newman записал упавшие утверждения.
func (f annSuite) report(t *testing.T, length int, executed []int, failedNames []string) {
	t.Helper()
	base := filepath.Join(f.root, f.suite, "out")
	if err := os.MkdirAll(base, 0o750); err != nil {
		t.Fatal(err)
	}
	type exec struct {
		Cursor   map[string]any `json:"cursor"`
		Response map[string]any `json:"response"`
	}
	execs := make([]exec, 0, len(executed))
	for _, p := range executed {
		execs = append(execs, exec{
			Cursor:   map[string]any{"position": p, "length": length},
			Response: map[string]any{"code": 200},
		})
	}
	type failure struct {
		Source map[string]any `json:"source"`
		Parent map[string]any `json:"parent"`
	}
	fails := make([]failure, 0, len(failedNames))
	for _, n := range failedNames {
		fails = append(fails, failure{
			Source: map[string]any{"name": "assert"},
			Parent: map[string]any{"name": n},
		})
	}
	raw, err := json.Marshal(map[string]any{
		"run": map[string]any{"executions": execs, "failures": fails},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeAnn(t, filepath.Join(base, "coll.json"), string(raw))
}

func writeAnn(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// scan прогоняет гейт с ПОДСТАВНЫМ трекером: состояние тикета задаётся картой, поэтому
// проба детерминирована и сети не требует. Неназванный тикет отвечает StateUnknown —
// «не спрашивали» отличимо от «открыт».
func scanAnn(t *testing.T, f annSuite, state map[int]IssueState) (Report, error) {
	t.Helper()
	return Scan(Options{
		Root: f.root,
		IssueState: func(_ string, n int) (IssueState, error) {
			if s, ok := state[n]; ok {
				return s, nil
			}
			return StateUnknown, nil
		},
	})
}

func findingsWith(rep Report, needle string) []string {
	var out []string
	for _, f := range rep.Findings {
		if strings.Contains(f, needle) {
			out = append(out, f)
		}
	}
	return out
}

// ── (5) сторона ТРЕКЕРА: закрытый тикет ──────────────────────────────────────────

func TestAnnotationOnClosedIssueIsAFinding(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, "# verifies https://github.com/PRO-Robotech/kacho/issues/8\n"+
		mkCase("FIX-CR-CRUD-OK"))

	rep, err := scanAnn(t, f, map[int]IssueState{8: StateClosed})
	if err == nil {
		t.Fatalf("гейт обязан упасть на пометке, ссылающейся на ЗАКРЫТЫЙ тикет; census=%+v", rep.Census)
	}
	got := findingsWith(rep, "kacho#8")
	if len(got) == 0 {
		t.Fatalf("находка обязана называть тикет; findings=%v", rep.Findings)
	}
	if !strings.Contains(got[0], "cases/coll.py:1") {
		t.Fatalf("находка обязана называть КООРДИНАТУ пометки, а не только тикет: %q", got[0])
	}
	if rep.Census.CaseAnnotations != 1 {
		t.Fatalf("перепись обязана насчитать одну пометку со ссылкой на тикет, насчитала %d",
			rep.Census.CaseAnnotations)
	}
}

// Законный близнец: тот же тикет ОТКРЫТ. Гейт молчит — иначе он ловил бы форму пометки, а
// не её просроченность, и был бы снят при первом же ложном срабатывании.
func TestAnnotationOnOpenIssueIsSilent(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, "# verifies https://github.com/PRO-Robotech/kacho/issues/8\n"+
		mkCase("FIX-CR-CRUD-OK"))

	rep, err := scanAnn(t, f, map[int]IssueState{8: StateOpen})
	if err != nil {
		t.Fatalf("пометка на ОТКРЫТЫЙ тикет находкой не является: %v (findings=%v)", err, rep.Findings)
	}
	if rep.Census.CaseAnnotations != 1 {
		t.Fatalf("пометка обязана быть ПРОЧИТАНА и посчитана, даже когда находки нет: %+v", rep.Census)
	}
	if len(rep.Unverified) == 0 {
		t.Fatalf("«всё ещё красный» без отчёта прогона обязано печататься НЕПРОВЕРЕННЫМ, " +
			"иначе не выполненная проверка читается как пройденная")
	}
}

// ── (1) ссылка без репозитория ───────────────────────────────────────────────────

func TestBareIssueNumberIsAFinding(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, mkCaseWithInnerMarker("FIX-CR-CRUD-OK", "    # verifies #71 (FGA types)"))

	rep, err := scanAnn(t, f, map[int]IssueState{71: StateOpen})
	if err == nil {
		t.Fatal("голый номер тикета нельзя разрешить — у пометки нет срока, это находка")
	}
	if len(findingsWith(rep, "БЕЗ репозитория (#71)")) == 0 {
		t.Fatalf("находка обязана называть номер и причину; findings=%v", rep.Findings)
	}
	// Пометка стоит ВНУТРИ конструктора — привязка к кейсу обязана найтись назад.
	if rep.Census.CaseSubjectsResolved != 1 {
		t.Fatalf("пометка внутри конструктора обязана привязаться к своему кейсу: %+v", rep.Census)
	}
}

// Законный близнец: тот же номер, но с репозиторием — разрешимо, значит не находка.
func TestQualifiedIssueReferenceIsSilent(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, mkCaseWithInnerMarker("FIX-CR-CRUD-OK", "    # verifies kacho#71 (FGA types)"))

	rep, err := scanAnn(t, f, map[int]IssueState{71: StateOpen})
	if err != nil {
		t.Fatalf("ссылка с репозиторием разрешима и находкой не является: %v (findings=%v)",
			err, rep.Findings)
	}
	if rep.Census.CaseAnnotations != 1 {
		t.Fatalf("пометка обязана быть посчитана: %+v", rep.Census)
	}
}

// ── (4) сторона ДЕФЕКТА: кейс исполнился и зелен. Сети не требует ────────────────

func TestAnnotationOnCaseThatRanGreenIsAFinding(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, "# verifies kacho#8\n"+mkCase("FIX-CR-CRUD-OK"))
	f.report(t, 1, []int{0}, nil)

	// Трекер НЕ спрашивается вовсе (карта пуста ⇒ StateUnknown): измерение стороны
	// дефекта обязано ловить это без единого сетевого вызова.
	rep, err := scanAnn(t, f, nil)
	if err == nil {
		t.Fatal("кейс исполнился и не дал упавших утверждений — пометка пережила предмет")
	}
	if len(findingsWith(rep, "ИСПОЛНИЛСЯ и не дал ни одного упавшего утверждения")) == 0 {
		t.Fatalf("находка обязана называть исход прогона; findings=%v", rep.Findings)
	}
}

// Законный близнец: тот же отчёт, но кейс КРАСНЫЙ — предмет пометки живой.
func TestAnnotationOnCaseStillRedIsSilent(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, "# verifies kacho#8\n"+mkCase("FIX-CR-CRUD-OK"))
	f.report(t, 1, []int{0}, []string{"FIX-CR-CRUD-OK — заголовок кейса"})

	rep, err := scanAnn(t, f, map[int]IssueState{8: StateOpen})
	if err != nil {
		t.Fatalf("пометка на всё ещё красном кейсе законна: %v (findings=%v)", err, rep.Findings)
	}
	if rep.Census.CaseSubjectsResolved != 1 {
		t.Fatalf("предмет пометки обязан разрешиться: %+v", rep.Census)
	}
}

// ── (3) кейса нет в наборе ───────────────────────────────────────────────────────

func TestAnnotationOnCaseTheSuiteDoesNotGenerateIsAFinding(t *testing.T) {
	f := newAnnSuite(t, "FIX-OTHER-CASE-OK")
	f.cases(t, "# verifies kacho#8\n"+mkCase("FIX-RENAMED-AWAY-OK"))

	rep, err := scanAnn(t, f, map[int]IssueState{8: StateOpen})
	if err == nil {
		t.Fatal("пометка на кейсе, которого коллекции не содержат, — находка")
	}
	if len(findingsWith(rep, "FIX-RENAMED-AWAY-OK")) == 0 {
		t.Fatalf("находка обязана называть кейс; findings=%v", rep.Findings)
	}
}

// ── (2) пометка без кейса ────────────────────────────────────────────────────────

func TestAnnotationWithNoCaseIsAFinding(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, "# verifies kacho#8\n# кейсов в файле нет вовсе\n")

	rep, err := scanAnn(t, f, map[int]IssueState{8: StateOpen})
	if err == nil {
		t.Fatal("пометка, не привязанная ни к одному кейсу, ничего не выкупает — находка")
	}
	if len(findingsWith(rep, "не привязана ни к одному кейсу")) == 0 {
		t.Fatalf("findings=%v", rep.Findings)
	}
}

// ── премиса: гейт читает КОММЕНТАРИЙ, а не текст ─────────────────────────────────

// Законный близнец I: разбор УЖЕ СНЯТОЙ пометки. Цитата стоит в обратных кавычках посреди
// строки; половина этих файлов существует ради такого разбора, и краснеть на нём значило
// бы учить писать вокруг мерки.
func TestQuotedMarkerInProseIsSilent(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, "# ЗДЕСЬ СТОЯЛА ПОМЕТКА `# verifies https://github.com/PRO-Robotech/kacho/issues/8`,\n"+
		"# И ОНА ПЕРЕЖИЛА СВОЙ ПРЕДМЕТ: тикет закрыт, кейс утверждает исправленный контракт.\n"+
		mkCase("FIX-CR-CRUD-OK"))

	rep, err := scanAnn(t, f, map[int]IssueState{8: StateClosed})
	if err != nil {
		t.Fatalf("разбор снятой пометки — не объявление: %v (findings=%v)", err, rep.Findings)
	}
	if rep.Census.CaseMarkers != 0 {
		t.Fatalf("цитата в прозе не является пометкой и не должна попадать в перепись как она: %+v",
			rep.Census)
	}
	if rep.Census.CaseFiles != 1 {
		t.Fatalf("файл обязан быть ПРОЧИТАН — «ноль пометок» и «ноль прочитанного» разные исходы: %+v",
			rep.Census)
	}
}

// Законный близнец II: пометка внутри строкового литерала — данные, а не объявление.
func TestMarkerInsideStringLiteralIsSilent(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, "DOC = \"\"\"\nПишите так: # verifies https://github.com/PRO-Robotech/kacho/issues/8\n\"\"\"\n"+
		mkCase("FIX-CR-CRUD-OK"))

	rep, err := scanAnn(t, f, map[int]IssueState{8: StateClosed})
	if err != nil {
		t.Fatalf("литерал — данные фикстуры, не объявление: %v (findings=%v)", err, rep.Findings)
	}
	if rep.Census.CaseMarkers != 0 {
		t.Fatalf("перепись не должна считать литерал пометкой: %+v", rep.Census)
	}
}

// Законный близнец III: пометка `# closes <тикет>` — ДРУГОЙ маркер. Она не выкупает кейс
// из зелёного, а называет артефакт закрытия, поэтому от закрытия тикета становится
// подтверждённой, а не ложной.
func TestClosesMarkerIsSilent(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, "# closes https://github.com/PRO-Robotech/kacho/issues/8\n"+mkCase("FIX-CR-CRUD-OK"))

	rep, err := scanAnn(t, f, map[int]IssueState{8: StateClosed})
	if err != nil {
		t.Fatalf("`# closes` — не объявление «ожидаемо красного»: %v (findings=%v)", err, rep.Findings)
	}
	if rep.Census.CaseMarkers != 0 {
		t.Fatalf("`# closes` не является пометкой `# verifies`: %+v", rep.Census)
	}
}

// Законный близнец IV: пометка на ПУНКТ ТРЕБОВАНИЙ тикета не называет. Она читается,
// считается пометкой — и НЕ судится; иначе гейт краснел бы на 222 законных строках дерева.
func TestRequirementIDAnnotationIsCountedButNotJudged(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, "# verifies GEO-1-21\n"+mkCase("FIX-CR-CRUD-OK"))

	rep, err := scanAnn(t, f, nil)
	if err != nil {
		t.Fatalf("пометка на пункт требований находкой не является: %v (findings=%v)", err, rep.Findings)
	}
	if rep.Census.CaseMarkers != 1 || rep.Census.CaseAnnotations != 0 {
		t.Fatalf("такая пометка обязана быть посчитана как маркер и НЕ как объявление о дефекте: %+v",
			rep.Census)
	}
}

// Трейлинг-пометка на той же строке, что идентификатор кейса (так пишет registry), обязана
// читаться: иначе у половины гейта осталась бы слепая зона в целом сервисе.
func TestTrailingMarkerOnTheIDLineIsRead(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, "CASES.append(Case(\n"+
		"    id=\"FIX-CR-CRUD-OK\",  # verifies kacho#8\n"+
		"    title=\"заголовок\",\n))\n")

	rep, err := scanAnn(t, f, map[int]IssueState{8: StateClosed})
	if err == nil {
		t.Fatal("трейлинг-пометка — такая же пометка; закрытый тикет обязан быть находкой")
	}
	if len(findingsWith(rep, "kacho#8 is no longer open")) == 0 {
		t.Fatalf("находка обязана быть про ЗАКРЫТЫЙ тикет; findings=%v", rep.Findings)
	}
	if len(findingsWith(rep, "cases/coll.py:2")) == 0 {
		t.Fatalf("находка обязана называть строку трейлинг-пометки; findings=%v", rep.Findings)
	}
	// Привязка обязана состояться: иначе проба зеленела бы на находке «пометка ни к чему
	// не привязана», которая тоже называет ту же координату, — то есть по другой причине.
	if rep.Census.CaseSubjectsResolved != 1 {
		t.Fatalf("трейлинг-пометка обязана привязаться к кейсу на СВОЕЙ строке: %+v", rep.Census)
	}
}

// ── премиса самой половины: ноль прочитанного ≠ ноль находок ─────────────────────

func TestZeroCaseFilesIsAFinding(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	if err := os.RemoveAll(filepath.Join(f.root, f.suite, "cases")); err != nil {
		t.Fatal(err)
	}
	rep, err := scanAnn(t, f, nil)
	if err == nil {
		t.Fatal("не прочитав ни одного файла кейсов, половина ничего не доказала — это находка")
	}
	if len(findingsWith(rep, "read 0 case file(s)")) == 0 {
		t.Fatalf("находка обязана называть объём осмотренного; findings=%v", rep.Findings)
	}
}

// ── привязка пометки к кейсу: комментарий МЕЖДУ двумя кейсами ────────────────────

// Пометка, стоящая в комментарии между кейсами, принадлежит СЛЕДУЮЩЕМУ. Обратное
// наблюдалось вживую: инъекция настоящей пометки в дерево привязала её к ПРЕДЫДУЩЕМУ
// кейсу, и находка назвала бы чужую координату. Гейт, называющий не тот предмет, чинят
// удалением, поэтому промах закрыт пробой, а не одной правкой.
func TestMarkerBetweenTwoCasesBindsToTheFollowingOne(t *testing.T) {
	f := newAnnSuite(t, "FIX-FIRST-CASE-OK", "FIX-SECOND-CASE-OK")
	f.cases(t, mkCase("FIX-FIRST-CASE-OK")+
		"\n# пояснение к следующему кейсу\n# verifies kacho#8\n"+
		mkCase("FIX-SECOND-CASE-OK"))
	// Отчёт: ЗЕЛЁН второй кейс, КРАСЕН первый. Привязка к первому дала бы «всё ещё
	// красный» и молчание — то есть промах был бы неотличим от исправности.
	f.report(t, 2, []int{0, 1}, []string{"FIX-FIRST-CASE-OK — заголовок кейса"})

	rep, err := scanAnn(t, f, map[int]IssueState{8: StateOpen})
	if err == nil {
		t.Fatalf("пометка привязана к первому кейсу вместо второго — промах невидим; census=%+v",
			rep.Census)
	}
	if len(findingsWith(rep, "FIX-SECOND-CASE-OK")) == 0 {
		t.Fatalf("находка обязана называть СЛЕДУЮЩИЙ кейс; findings=%v", rep.Findings)
	}
}

// ── слаг репозитория, который принимает трекер ──────────────────────────────────

// Ссылки в этих документах нормализуются к ИМЕНИ репозитория (`kacho`), а `gh` требует
// `OWNER/REPO` и на голом имени отказывает СВОИМ разбором аргументов. Пока владелец не
// подставлялся, измерение «тикет всё ещё открыт» не могло сработать ни разу: любой тикет
// давал StateUnknown, вердикт печатался как НЕПРОВЕРЕНО и читался как «всё чисто».
// Утверждается СТРОКА ЗАПРОСА к трекеру, а не поведение вспомогательной функции: проба
// на один `trackerSlug` оставалась зелёной при снятом вызове — заголовок шире тела.
func TestTrackerQueryCarriesTheOwner(t *testing.T) {
	cases := map[string]string{
		"kacho":                  "PRO-Robotech/kacho",
		"kacho-vpc":              "PRO-Robotech/kacho-vpc",
		"PRO-Robotech/kacho":     "PRO-Robotech/kacho",
		"github.com/other/kacho": "github.com/other/kacho",
	}
	for in, want := range cases {
		args := ghArgs(in, 8)
		var got string
		for i, a := range args {
			if a == "--repo" && i+1 < len(args) {
				got = args[i+1]
			}
		}
		if got != want {
			t.Fatalf("`gh %s` спрашивает про %q, ожидалось %q — на форме без владельца `gh` "+
				"отвергает запрос СВОИМ разбором аргументов, поэтому вопрос не задаётся вовсе, "+
				"а вердикт печатается как НЕПРОВЕРЕНО и читается как «чисто»",
				strings.Join(args, " "), got, want)
		}
	}
}

// ── перепись напечатана ─────────────────────────────────────────────────────────

func TestCensusNamesTheVerifiesVolume(t *testing.T) {
	f := newAnnSuite(t, "FIX-CR-CRUD-OK")
	f.cases(t, "# verifies kacho#8\n"+mkCase("FIX-CR-CRUD-OK"))

	rep, _ := scanAnn(t, f, map[int]IssueState{8: StateOpen})
	var sb strings.Builder
	Print(rep, &sb)
	for _, want := range []string{"case file(s) read for `# verifies`", "naming a tracker issue"} {
		if !strings.Contains(sb.String(), want) {
			t.Fatalf("перепись обязана называть объём третьей половины (%q): %s", want, sb.String())
		}
	}
}

func mkCase(id string) string {
	return "CASES.append(Case(\n    id=\"" + id + "\",\n    title=\"заголовок\",\n))\n"
}

func mkCaseWithInnerMarker(id, marker string) string {
	return "CASES.append(Case(\n    id=\"" + id + "\",\n" + marker + "\n    title=\"заголовок\",\n))\n"
}
