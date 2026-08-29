// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// sharedStemsRel — общий слой ОТБОРА коллекций для прогонщиков newman.
//
// Рядом с общим слоем генератора (`tests/newman/kacholib/gen_shared.py`) и по
// той же причине: перечень коллекций набора — один предмет, и мест у него
// должно быть одно.
const sharedStemsRel = "tests/newman/kacholib/stems.sh"

// runnerStemCensus — объём осмотренного. Печатается ВСЕГДА: без него «находок
// нет» неотличимо от «ничего не прочитано».
//
// Половины названы порознь намеренно. Одно суммарное число («прогонщиков 9»)
// скрыло бы ровно тот случай, ради которого гейт заведён: перечень, выписанный
// массивом, и перечень, взятый из общего слоя, — разные состояния, и переход
// между ними обязан быть виден числом.
type runnerStemCensus struct {
	runners         int // прогонщиков осмотрено
	takenFromShared int // берут отбор из общего слоя
	ownSelection    int // отбирают сами, обходя дерево
	noSelection     int // не отбирают вовсе (обёртка · одна названная коллекция · чужое объявление)
	written         int // выписывают перечень массивом
	writtenStems    int // сколько имён выписано суммарно
	sharedDeclare   bool
	suites          int // наборов, чей состав коллекций сопоставлен
	stems           int // стемов коллекций во всех наборах
}

// arrayAssignRe — присваивание массива в bash: `ИМЯ=(a b c)`.
//
// Читается ИСПОЛНЯЕМАЯ строка, а не текст файла: строка, начинающаяся с `#`,
// отбрасывается до сверки. Иначе гейт краснел бы на СОБСТВЕННОМ объяснении —
// шапка прогонщика называет предмет («COLLECTIONS — the suite's own expected
// set»), и распознаватель по подстроке считал бы её нарушением.
var arrayAssignRe = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)=\(([^)]*)\)`)

// selectsFromTree — ДЕЛАЕТ ЛИ прогонщик отбор сам, обходя дерево набора.
//
// Антецедент требования «взять отбор из общего слоя». Без него гейт краснел бы
// на прогонщиках, которые ничего не отбирают, — а такие в дереве есть и они
// законны: обёртка над пошаговым прогоном (`exec node run-incremental.js`),
// прогон ОДНОЙ названной коллекции (`run-failclosed.sh`) и волна, чей перечень
// выводится не из каталога, а из объявления (`run-ceremony.sh`). Требовать от
// них общего отбора значило бы требовать отбора там, где его нет.
func selectsFromTree(src string) bool {
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.Contains(t, "collections/*.postman_collection.json") ||
			strings.Contains(t, "cases/*.py") {
			return true
		}
	}
	return false
}

// sourcesSharedStems — берёт ли прогонщик отбор из общего слоя.
//
// Признак — упоминание ФАЙЛА общего слоя в исполняемой строке. Имя функции для
// этого не годится: прогонщик, объявивший функцию с тем же именем у себя, дал
// бы тот же признак — то есть форк читался бы как сведение.
func sourcesSharedStems(src string) bool {
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.Contains(t, "kacholib/stems.sh") {
			return true
		}
	}
	return false
}

// writtenStemList — перечень коллекций, ВЫПИСАННЫЙ массивом.
//
// Возвращает имя переменной и попавшие в неё стемы. Порог — ДВА стема: одиночное
// присваивание (`stems=("$SERVICE")`, `EXTRA=()`) перечнем не является, и гейт,
// считающий его перечнем, краснел бы на разборе аргументов — то есть на коде,
// который к предмету отношения не имеет.
func writtenStemList(src string, stems map[string]bool) (string, []string) {
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		m := arrayAssignRe.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		var hit []string
		for _, w := range strings.Fields(m[2]) {
			w = strings.Trim(w, `"'`)
			if stems[w] {
				hit = append(hit, w)
			}
		}
		if len(hit) >= 2 {
			sort.Strings(hit)
			return m[1], hit
		}
	}
	return "", nil
}

// auditRunnerStemSets — судящая функция гейта.
//
// Выделена, чтобы инъекция гоняла ЕЁ, а не свою копию: проба, повторяющая логику
// гейта, доказывала бы свойство копии.
func auditRunnerStemSets(sharedSrc string, runners map[string]string, stemsOfSuite map[string][]string) ([]string, runnerStemCensus) {
	cen := runnerStemCensus{runners: len(runners)}
	cen.sharedDeclare = strings.Contains(sharedSrc, "newman_expected_stems") &&
		strings.Contains(sharedSrc, "newman_present_stems")

	rels := make([]string, 0, len(runners))
	for rel := range runners {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var findings []string
	for _, rel := range rels {
		src := runners[rel]
		stemList := stemsOfSuite[rel]
		if len(stemList) > 0 {
			cen.suites++
			cen.stems += len(stemList)
		}
		stems := map[string]bool{}
		for _, s := range stemList {
			stems[s] = true
		}
		switch {
		case sourcesSharedStems(src):
			cen.takenFromShared++
		case selectsFromTree(src):
			cen.ownSelection++
			findings = append(findings, fmt.Sprintf(
				"%s — отбирает коллекции САМ (обходит cases/ либо collections/), а не берёт отбор "+
					"из общего слоя %s", rel, sharedStemsRel))
		default:
			// Прогонщик, который ничего не отбирает, — законный близнец, и он
			// считается отдельно: «молчит» обязано быть отличимо от «не осмотрен».
			cen.noSelection++
		}
		if name, hit := writtenStemList(src, stems); name != "" {
			cen.written++
			cen.writtenStems += len(hit)
			findings = append(findings, fmt.Sprintf(
				"%s — перечень коллекций ВЫПИСАН массивом %s (%s): второе место об одном предмете, "+
					"и оно уже разошлось с деревом", rel, name, strings.Join(hit, ", ")))
		}
	}
	return findings, cen
}

// Перечень коллекций прогонщика ВЫВОДИТСЯ из дерева, а не выписывается.
//
// ПРЕДМЕТ. Что именно гонит суита, объявлено в дереве дважды: генератор эмитит
// коллекцию на каждый `cases/<имя>.py`, а прогонщик нёс рукописный массив тех же
// имён. Два места об одном предмете расходятся молча и расходятся РОВНО в одну
// сторону: новый набор кейсов появляется, коллекция генерируется, а массив о ней
// не знает.
//
// ЦЕНА ИЗМЕРЕНА, А НЕ ПРЕДПОЛОЖЕНА (задача #1524). На `release/deforking-2`
// рукописных перечней было ПЯТЬ (compute · storage · nlb · nlb-incremental ·
// registry), и один из них уже разошёлся: `registry-docker-facade-lane` в массиве
// не значилась при пяти модулях кейсов и пяти сгенерированных коллекциях.
//
// ЧТО ЗДЕСЬ ОПРОВЕРГНУТО, ЧТОБЫ СЛЕДУЮЩИЙ НЕ ИСКАЛ ЗАНОВО. Задача утверждала, что
// разошедшаяся коллекция «не гоняется никем», и опиралась на поиск её ИМЕНИ по
// скриптам. Признак мерил соглашение об именовании, а не вызывающего: прогонщик
// подбирал её ветвью-подхватом (`[drift] … running it anyway`), заведённой
// 16d8c3436 — то есть ЗАДОЛГО до замера задачи. Коллекция исполнялась и попадала
// в вердикт. Настоящий остаток — сам рукописный перечень: он делает подхват
// ПОСТОЯННЫМ, и предупреждение, печатающееся на штатном состоянии, перестают
// читать.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ДОГОВОРЁННОСТЬ. Массив заводится не злым умыслом, а
// копированием соседнего прогонщика при заведении набора — тем самым действием,
// которым набор и заводят. Пять копий накопились так.
//
// ЧЕГО ГЕЙТ НЕ СУДИТ. Он не запрещает называть коллекцию по имени вообще: у iam
// ПОРЯДОК вызовов рукописный осознанно (посев и зависимость между коллекциями),
// и это законно, пока сам НАБОР выводится из дерева, а остаток подбирается и
// называется. Предмет гейта — множество, а не порядок.
func TestNewmanRunnerDerivesItsCollectionSet(t *testing.T) {
	root := repoRoot(t)

	// Состав берётся из ИНДЕКСА git, а не обходом диска: под корнем лежат
	// каталоги, которых в репозитории нет (рабочие копии агентов, отчёты
	// прогонов), и обход по диску сделал бы вердикт свойством чужого рабочего
	// каталога.
	tt := newTrackedTree(t, root)
	if !tt.files[sharedStemsRel] {
		t.Fatalf("предпосылка гейта не выполняется: общего слоя отбора %s в индексе git нет.\n"+
			"Отбор коллекций живёт там, и без него гейт судил бы пустоту.", sharedStemsRel)
	}
	sharedSrc, err := os.ReadFile(filepath.Join(root, sharedStemsRel)) // #nosec G304 -- путь из индекса git этого модуля
	if err != nil {
		t.Fatalf("чтение %s: %v", sharedStemsRel, err)
	}

	runners := map[string]string{}
	stemsOfSuite := map[string][]string{}
	for rel := range tt.files {
		if !strings.Contains(rel, "tests/newman/scripts/") || !strings.HasSuffix(rel, ".sh") {
			continue
		}
		base := filepath.Base(rel)
		if base != "run.sh" && !strings.HasPrefix(base, "run-") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		runners[rel] = string(b)
		stemsOfSuite[rel] = collectionStemsForRunner(tt, rel)
	}

	findings, cen := auditRunnerStemSets(string(sharedSrc), runners, stemsOfSuite)

	// Проверка предпосылки: обходчик обязан заявить объём осмотренного, иначе
	// «находок нет» неотличимо от «ничего не прочитано».
	t.Logf("осмотрено прогонщиков newman: %d — берут отбор из общего слоя %d, отбирают сами %d, "+
		"не отбирают вовсе %d; выписывают перечень массивом: %d (имён выписано %d); "+
		"общий слой объявляет отбор: %s; наборов сопоставлено: %d, стемов коллекций: %d",
		cen.runners, cen.takenFromShared, cen.ownSelection, cen.noSelection,
		cen.written, cen.writtenStems, yesNo(cen.sharedDeclare), cen.suites, cen.stems)
	if cen.runners == 0 {
		t.Fatalf("предпосылка гейта не выполняется: прогонщиков newman в индексе git не найдено — " +
			"либо раскладка сменилась, либо обход смотрит не туда; чинить надо гейт, а не молча выходить успехом")
	}
	if !cen.sharedDeclare {
		t.Fatalf("предпосылка гейта не выполняется: общий слой %s не объявляет обоих отборов "+
			"(newman_expected_stems / newman_present_stems) — тогда «взят оттуда» вычислялось бы "+
			"по имени файла, а не по предмету", sharedStemsRel)
	}
	if cen.suites == 0 || cen.stems == 0 {
		t.Fatalf("предпосылка гейта не выполняется: ни у одного из %d прогонщиков не нашлось "+
			"сгенерированных коллекций — распознаватель выписанного перечня был бы вакуумен "+
			"by construction (сверять не с чем)", cen.runners)
	}

	if len(findings) > 0 {
		t.Fatalf("прогонщики, чей набор коллекций объявлен вторым местом (перечень расходится с деревом молча "+
			"и ровно в одну сторону — новая коллекция генерируется и в перечень не попадает):\n  %s",
			strings.Join(findings, "\n  "))
	}
}

// collectionStemsForRunner — стемы коллекций набора, которому принадлежит
// прогонщик. Состав берётся из индекса git по той же причине, что и сам обход.
func collectionStemsForRunner(tt *trackedTree, runnerRel string) []string {
	dir := strings.TrimSuffix(filepath.Dir(runnerRel), "/scripts") + "/collections/"
	var out []string
	for rel := range tt.files {
		if !strings.HasPrefix(rel, dir) {
			continue
		}
		if !strings.HasSuffix(rel, ".postman_collection.json") {
			continue
		}
		out = append(out, strings.TrimSuffix(filepath.Base(rel), ".postman_collection.json"))
	}
	sort.Strings(out)
	return out
}

// Отбор общего слоя ИСПОЛНЯЕТСЯ и сходится с правилом генератора.
//
// ПОЧЕМУ ИСПОЛНЕНИЕМ, А НЕ ЧТЕНИЕМ. Предмет здесь — не текст функции, а её
// ОТВЕТ: правило отбора уже расходилось с генератором молча. Прогонщики
// пропускали `__init__`/`__main__`, генератор — ЛЮБОЕ имя с ведущим
// подчёркиванием, и в двух наборах из восьми (`nlb`, `registry`) в `cases/`
// лежит `_helpers.py`. По прежнему правилу он стал бы ожидаемой коллекцией,
// суита доложила бы MISSING и покраснела бы по причине, которой нет.
//
// Расхождение было НЕВИДИМО там, где его писали: у vpc/geo/gateway модулей с
// подчёркиванием нет вовсе, поэтому узкое правило выглядело решением, не будучи
// им, — тот же класс, который общий слой генератора называет у себя.
func TestNewmanSharedStemSelectorMatchesTheGenerator(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	if !tt.files[sharedStemsRel] {
		t.Fatalf("предпосылка гейта не выполняется: общего слоя отбора %s в индексе git нет", sharedStemsRel)
	}

	// Наборы newman — по каталогу cases/ в индексе.
	suites := map[string]bool{}
	for rel := range tt.files {
		i := strings.Index(rel, "/tests/newman/cases/")
		if i < 0 {
			continue
		}
		suites[rel[:i+len("/tests/newman")]] = true
	}
	names := make([]string, 0, len(suites))
	for s := range suites {
		names = append(names, s)
	}
	sort.Strings(names)

	var checked, helpers, mismatched int
	var findings []string
	for _, suite := range names {
		wantExpected := generatorCaseStems(tt, suite)
		wantPresent := collectionStemsForRunner(tt, suite+"/scripts/run.sh")
		for _, s := range wantExpected {
			if strings.HasPrefix(s, "_") {
				helpers++
			}
		}
		gotExpected := runSharedSelector(t, root, "newman_expected_stems", suite)
		gotPresent := runSharedSelector(t, root, "newman_present_stems", suite)
		checked++
		if !equalStrings(gotExpected, wantExpected) {
			mismatched++
			findings = append(findings, fmt.Sprintf(
				"%s — newman_expected_stems дал [%s], правило генератора даёт [%s]",
				suite, strings.Join(gotExpected, " "), strings.Join(wantExpected, " ")))
		}
		if !equalStrings(gotPresent, wantPresent) {
			mismatched++
			findings = append(findings, fmt.Sprintf(
				"%s — newman_present_stems дал [%s], в дереве лежит [%s]",
				suite, strings.Join(gotPresent, " "), strings.Join(wantPresent, " ")))
		}
	}

	// Перепись обеих величин: наборов осмотрено И сколько из них НЕСУТ модуль
	// с ведущим подчёркиванием. Одно первое число скрыло бы ровно тот случай,
	// ради которого проба заведена: на наборах без таких модулей узкое и широкое
	// правила отвечают одинаково.
	helperSuites := 0
	for _, suite := range names {
		for _, f := range caseFileStems(tt, suite) {
			if strings.HasPrefix(f, "_") {
				helperSuites++
				break
			}
		}
	}
	t.Logf("наборов newman осмотрено: %d, из них с модулем-помощником в cases/: %d; расхождений отбора: %d",
		checked, helperSuites, mismatched)
	if checked == 0 {
		t.Fatal("предпосылка пробы не выполняется: наборов newman не найдено — сверять нечего")
	}
	if helperSuites == 0 {
		t.Fatal("предпосылка пробы не выполняется: ни в одном наборе нет модуля с ведущим подчёркиванием,\n" +
			"поэтому узкое и широкое правила отбора отвечали бы одинаково и проба была бы вакуумна")
	}
	if len(findings) > 0 {
		t.Fatalf("отбор общего слоя разошёлся с правилом генератора:\n  %s", strings.Join(findings, "\n  "))
	}
}

// caseFileStems — все модули cases/ набора, включая помощников.
func caseFileStems(tt *trackedTree, suite string) []string {
	dir := suite + "/cases/"
	var out []string
	for rel := range tt.files {
		if !strings.HasPrefix(rel, dir) || !strings.HasSuffix(rel, ".py") {
			continue
		}
		if strings.Contains(strings.TrimPrefix(rel, dir), "/") {
			continue
		}
		out = append(out, strings.TrimSuffix(filepath.Base(rel), ".py"))
	}
	sort.Strings(out)
	return out
}

// generatorCaseStems — модули кейсов ПО ПРАВИЛУ ГЕНЕРАТОРА: имя с ведущим
// подчёркиванием — помощник, а не набор кейсов (`gen_shared._case_modules`).
func generatorCaseStems(tt *trackedTree, suite string) []string {
	var out []string
	for _, s := range caseFileStems(tt, suite) {
		if strings.HasPrefix(s, "_") {
			continue
		}
		out = append(out, s)
	}
	return out
}

// runSharedSelector — ОТВЕТ общего слоя, полученный его исполнением.
//
// Путь к слою — ПАРАМЕТР, а не константа: инъекция обязана навести ту же функцию
// на изменённую копию слоя, иначе она доказывала бы свойство своей копии.
func runSharedSelector(t *testing.T, root, fn, suite string) []string {
	t.Helper()
	return runSelectorFrom(t, filepath.Join(root, sharedStemsRel), fn, filepath.Join(root, suite))
}

// runSelectorFrom — то же, но слой и каталог набора названы явно.
func runSelectorFrom(t *testing.T, lib, fn, dir string) []string {
	t.Helper()
	script := fmt.Sprintf(". %q && %s %q", lib, fn, dir)
	cmd := exec.Command("bash", "-c", script) // #nosec G204 -- путь из индекса git этого модуля
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("исполнение %s для %s: %v\n%s", fn, dir, err, out)
	}
	var res []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			res = append(res, s)
		}
	}
	sort.Strings(res)
	return res
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
