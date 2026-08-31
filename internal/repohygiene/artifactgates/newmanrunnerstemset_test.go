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

// stemLane — полоса сравнения: какую функцию общего слоя исполняем, из какого
// подкаталога набора произведён стем и как называется вторая сторона в тексте
// находки.
//
// Полоса выделена типом, а не двумя копиями кода, ровно потому, что обе стороны
// сравнения у них РАЗНЫЕ по происхождению: слева ответ слоя (он спрашивает
// ДИСК), справа — правило, применённое к ИНДЕКСУ git. Именно этот шов и породил
// находку #1780; держать его в двух местах значило бы чинить одно из них.
type stemLane struct {
	fn     string // функция общего слоя, чей ОТВЕТ сверяется
	dir    string // подкаталог набора, в котором лежит файл-производитель стема
	suffix string // расширение файла-производителя
	other  string // как называется вторая сторона сравнения
}

var (
	stemLaneExpected = stemLane{
		fn: "newman_expected_stems", dir: "cases", suffix: ".py",
		other: "правило генератора",
	}
	stemLanePresent = stemLane{
		fn: "newman_present_stems", dir: "collections", suffix: ".postman_collection.json",
		other: "состав дерева",
	}
)

// rel — путь файла, из которого стем произведён.
func (l stemLane) rel(suite, stem string) string {
	return suite + "/" + l.dir + "/" + stem + l.suffix
}

// stemDiffVerdict — расхождения, РАЗВЕДЁННЫЕ по причине.
//
// Две причины, и смешивать их нельзя: первая говорит о ПРАВИЛАХ отбора (предмет
// гейта), вторая — о том, что рабочее дерево разошлось с индексом git (предмет
// вообще не гейта). Одна общая корзина посылала читателя искать дефект в
// правилах отбора там, где причина «файла нет в индексе».
type stemDiffVerdict struct {
	rule  []string // расхождение ПРАВИЛ отбора
	index []string // рабочее дерево разошлось с индексом git
}

// classifySuiteStemDiff — судящая функция: почему имя разошлось.
//
// ПОЧЕМУ РАЗЛИЧЕНИЕ ВООБЩЕ ВОЗМОЖНО. Стороны сравнения спрашивают РАЗНЫЕ
// авторитеты: слой отбора обходит диск глобом оболочки, правило генератора
// применяется к индексу git. Значит у каждого расхождения есть ровно два
// объяснения, и они отличимы вопросом к самому файлу-производителю:
//
//	есть на диске  · нет в индексе  → ИНДЕКС: файл не добавлен (`git add`)
//	нет на диске   · есть в индексе → ИНДЕКС: файл удалён из рабочего дерева
//	сходятся оба                    → ПРАВИЛА: слой и генератор судят по-разному
//
// Третья строка — единственная, о которой гейт заведён. Первые две означают, что
// сравнение отборов на этом наборе БЕСПРЕДМЕТНО, и текст обязан называть индекс,
// а не отбор: иначе находка посылает искать дефект в правилах, которых он не
// касается. Стоило полного локального прогона (#1780).
//
// Гейт при этом остаётся КРАСНЫМ в обоих случаях: молчание на расхождении дерева
// с индексом было бы ослаблением, а предмет находки — диагностика, а не вердикт.
func classifySuiteStemDiff(lane stemLane, suite string, got, want []string,
	inIndex, onDisk func(rel string) bool) stemDiffVerdict {

	gotSet := map[string]bool{}
	for _, s := range got {
		gotSet[s] = true
	}
	wantSet := map[string]bool{}
	for _, s := range want {
		wantSet[s] = true
	}

	var v stemDiffVerdict
	var ruleOnly []string
	for _, s := range got {
		if wantSet[s] {
			continue
		}
		rel := lane.rel(suite, s)
		if !inIndex(rel) && onDisk(rel) {
			v.index = append(v.index, fmt.Sprintf(
				"%s — имя %q НЕ ПРО ПРАВИЛА ОТБОРА: файл %s лежит на диске и в ИНДЕКСЕ git ОТСУТСТВУЕТ. "+
					"%s спрашивает диск, %s применяется к индексу — оттого имя есть у одной стороны и нет у другой. "+
					"Сведите рабочее дерево с индексом (`git add %s` либо уберите файл): до этого сравнение "+
					"отборов на этом наборе беспредметно",
				suite, s, rel, lane.fn, lane.other, rel))
			continue
		}
		ruleOnly = append(ruleOnly, s)
	}
	for _, s := range want {
		if gotSet[s] {
			continue
		}
		rel := lane.rel(suite, s)
		if inIndex(rel) && !onDisk(rel) {
			v.index = append(v.index, fmt.Sprintf(
				"%s — имя %q НЕ ПРО ПРАВИЛА ОТБОРА: файл %s числится в ИНДЕКСЕ git и на диске ОТСУТСТВУЕТ. "+
					"%s спрашивает диск, %s применяется к индексу — оттого имя есть у одной стороны и нет у другой. "+
					"Сведите рабочее дерево с индексом (`git checkout -- %s` либо `git rm`): до этого сравнение "+
					"отборов на этом наборе беспредметно",
				suite, s, rel, lane.fn, lane.other, rel))
			continue
		}
		ruleOnly = append(ruleOnly, s)
	}
	if len(ruleOnly) > 0 {
		sort.Strings(ruleOnly)
		v.rule = append(v.rule, fmt.Sprintf(
			"%s — %s дал [%s], %s даёт [%s]; расходятся имена: %s",
			suite, lane.fn, strings.Join(got, " "), lane.other, strings.Join(want, " "),
			strings.Join(ruleOnly, ", ")))
	}
	return v
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
//
// ДИАГНОСТИКА ЕСТЬ ЧАСТЬ СВОЙСТВА (#1780, `testing.md` §«Гейт на класс» п.8).
// Стороны сравнения спрашивают разные авторитеты — диск и индекс git, — поэтому
// НЕОТСЛЕЖИВАЕМЫЙ файл кейса даёт расхождение, к правилам отбора отношения не
// имеющее. Прежняя редакция называла его «расхождением отбора» и печатала два
// списка имён: находка читалась как настоящая, а причина была «файл не в
// индексе». Причины разведены `classifySuiteStemDiff`, и перепись печатает их
// ПОРОЗНЬ — одно суммарное число вернуло бы ту же неразличимость.
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

	inIndex := func(rel string) bool { return tt.files[rel] }
	onDisk := func(rel string) bool {
		_, err := os.Stat(filepath.Join(root, rel))
		return err == nil
	}

	var checked int
	var ruleFindings, indexFindings []string
	for _, suite := range names {
		wantExpected := generatorCaseStems(tt, suite)
		wantPresent := collectionStemsForRunner(tt, suite+"/scripts/run.sh")
		gotExpected := runSharedSelector(t, root, stemLaneExpected.fn, suite)
		gotPresent := runSharedSelector(t, root, stemLanePresent.fn, suite)
		checked++
		for _, v := range []stemDiffVerdict{
			classifySuiteStemDiff(stemLaneExpected, suite, gotExpected, wantExpected, inIndex, onDisk),
			classifySuiteStemDiff(stemLanePresent, suite, gotPresent, wantPresent, inIndex, onDisk),
		} {
			ruleFindings = append(ruleFindings, v.rule...)
			indexFindings = append(indexFindings, v.index...)
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
	// Расхождения печатаются ПОРОЗНЬ по причине: сумма вернула бы ту самую
	// неразличимость, ради устранения которой заведено `classifySuiteStemDiff`.
	// Охват (наборов · наборов с помощником) при этом не изменился — расширение
	// текста не есть расширение обхода.
	t.Logf("наборов newman осмотрено: %d, из них с модулем-помощником в cases/: %d; "+
		"расхождений ПРАВИЛ отбора: %d, имён вне индекса git: %d",
		checked, helperSuites, len(ruleFindings), len(indexFindings))
	if checked == 0 {
		t.Fatal("предпосылка пробы не выполняется: наборов newman не найдено — сверять нечего")
	}
	if helperSuites == 0 {
		t.Fatal("предпосылка пробы не выполняется: ни в одном наборе нет модуля с ведущим подчёркиванием,\n" +
			"поэтому узкое и широкое правила отбора отвечали бы одинаково и проба была бы вакуумна")
	}
	// ПОРЯДОК НАЗЫВАНИЯ. Первым идёт предмет гейта (правила отбора), вторым —
	// расхождение дерева с индексом. Обратный порядок вернул бы прежнее: читатель
	// принимает первую строку за диагноз и идёт по ней.
	var msg []string
	if len(ruleFindings) > 0 {
		msg = append(msg, "отбор общего слоя разошёлся с правилом генератора:\n  "+
			strings.Join(ruleFindings, "\n  "))
	}
	if len(indexFindings) > 0 {
		msg = append(msg, "рабочее дерево разошлось с ИНДЕКСОМ git — это НЕ расхождение правил отбора:\n  "+
			strings.Join(indexFindings, "\n  "))
	}
	if len(msg) > 0 {
		t.Fatal(strings.Join(msg, "\n"))
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

// Здесь стоял `equalStrings` — он снят ВМЕСТЕ со своим предметом (#1780).
// Сравнение «равны или нет» отвечало на вопрос, который больше не задаётся:
// `classifySuiteStemDiff` спрашивает не РАВНЫ ли перечни, а ПОЧЕМУ они
// разошлись, и различает имена поимённо. Мёртвый близнец, оставленный «на
// всякий случай», к следующей правке разошёлся бы с живым разбором молча.
