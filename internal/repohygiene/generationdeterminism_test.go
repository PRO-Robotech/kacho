// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// Разбор класса «имя шага зависит от того, сколько модулей обработал
// генератор» — и гейт по дереву, который его держит.
//
// # Предмет
//
// Генераторы сквозных проб (`*/tests/newman/scripts/gen.py`) объявляют режим
// «один модуль» — `python3 scripts/gen.py <модуль>` — и печатают его в
// собственной шапке. Режим осмыслен ровно при одном условии: результат обоих
// режимов побайтово совпадает. Иначе правка одного набора выглядит завершённой
// и оставляет дерево в состоянии, которого полный прогон не производит, —
// следующий полный прогон переписывает соседние коллекции целиком.
//
// Величина, попадающая в имя шага, обязана быть функцией КЕЙСА. Счётчик,
// объявленный на уровне модуля Python и не сброшенный при загрузке следующего
// набора, делает её функцией ОКРУЖЕНИЯ: имя зависит от того, сколько наборов
// загрузилось раньше. Такое имя нестабильно между прогонами, а именно по именам
// шагов ставят диагноз при разборе красного.
//
// # Что ловится
//
// Расхождение байтов коллекции между двумя режимами генерации одного и того же
// набора. Гейт не смотрит НА КОД счётчика вовсе — он исполняет генератор и
// сравнивает то, что тот произвёл. Поэтому счётчик любой формы (целое на уровне
// модуля, список-ячейка, `itertools.count`, длина накопителя) ловится
// одинаково, а законная величина, выведенная из кейса, не ловится никогда.
//
// # Почему исполнением, а не разбором исходника
//
// Синтаксический предикат пришлось бы писать под известные формы счётчика и
// сверять их с известным местом сброса. Обе половины — перечни, а перечень
// стареет молча: следующая форма счётчика в него не попадёт, и гейт останется
// зелёным ровно там, где заводится новый экземпляр класса. Исполнение
// генератора этой слепой зоны не имеет by construction: расхождение байтов
// наблюдаемо независимо от того, чем оно вызвано.
//
// # Цена
//
// Гейт запускает интерпретатор один раз на полный прогон набора и ещё по разу
// на каждый его модуль. Замер 2026-08-14 на восьми наборах дерева: полный
// прогон всех — 3.4 с, помодульный — 7.8 с. Наборы осматриваются параллельно,
// поэтому стена определяется самым крупным из них, а не суммой.

// collectionsSubdir — каталог, в который пишет генератор. Одинаков у всех восьми
// наборов дерева (замер: `grep -n '^OUT_DIR\|^COLLECTIONS_DIR' */tests/newman/scripts/gen.py`).
const collectionsSubdir = "collections"

// generatorRelPath — путь генератора внутри каталога набора.
const generatorRelPath = "scripts/gen.py"

// suiteVerdict — исход осмотра одного набора: перепись осмотренного плюс
// находки. Перепись отдельна от находок намеренно: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
type suiteVerdict struct {
	// Suite — путь каталога набора относительно корня дерева.
	Suite string
	// Modules — сколько модулей генератор произвёл на полном прогоне.
	Modules int
	// BytesCompared — сколько байт коллекций сверено между режимами.
	BytesCompared int
	// Findings — по одной записи на модуль, чей одиночный прогон разошёлся с
	// полным. Каждая запись называет координату и первое расхождение.
	Findings []string
}

// compareGenerationModes исполняет генератор набора в двух режимах и сверяет
// произведённое побайтово.
//
// suiteDir — каталог набора (тот, что содержит `scripts/gen.py` и `cases/`);
// он НЕ изменяется: работа идёт в копии под tmpDir. Копия нужна не ради
// аккуратности, а ради вердикта — гейт, мутирующий дерево, которое он судит,
// сообщает о состоянии, созданном им самим.
//
// python — путь интерпретатора. Пустая строка означает «не найден», и это
// ОТКАЗ, а не пропуск: набор, который не смогли исполнить, нельзя ни засчитать
// в перепись, ни молча пропустить.
//
// listFiles — источник СОСТАВА копируемого дерева, и он передаётся явно, а не
// выбирается внутри. У настоящего набора авторитет — индекс git; у
// синтетического, который проба инъекции собирает во временном каталоге,
// репозитория нет вовсе, и авторитет — обход диска. Молчаливый откат «нет
// индекса → иду по диску» вернул бы ровно тот дефект, ради которого состав
// берётся у индекса, и сделал бы это невидимо: см. godoc treecorpus.SyntheticTree.
func compareGenerationModes(suiteDir, tmpDir, python string, listFiles fileLister) (suiteVerdict, error) {
	v := suiteVerdict{Suite: suiteDir}
	if python == "" {
		return v, errors.New("интерпретатор python3 не найден: набор не исполнен, " +
			"и его вердикт не существует — это не «ноль находок»")
	}

	work := filepath.Join(tmpDir, "suite")
	if err := copyTreeExcept(suiteDir, work, listFiles, collectionsSubdir); err != nil {
		return v, fmt.Errorf("копия набора %s: %w", suiteDir, err)
	}
	// Копия обязана нести ЗАВИСИМОСТЬ генератора, а не только его самого.
	// Общий слой (`tests/newman/kacholib/gen_shared.py`, задача #1367) лежит вне
	// каталога набора, и генератор ищет его ВВЕРХ ОТ СЕБЯ. Без этого шага копия
	// исполняется в дереве, где зависимости нет, и гейт сообщал бы о состоянии,
	// созданном им самим, — том самом, ради которого копия и делается.
	if err := materializeSharedGenLayer(suiteDir, tmpDir); err != nil {
		return v, fmt.Errorf("общий слой генератора для копии %s: %w", suiteDir, err)
	}
	outDir := filepath.Join(work, collectionsSubdir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return v, fmt.Errorf("каталог вывода: %w", err)
	}

	// Режим 1 — полный прогон. Он же задаёт перечень модулей: имя коллекции
	// и есть имя модуля, поэтому перечень не выписывается вторым способом и
	// не может разойтись с тем, что генератор считает своим набором.
	if out, err := runGenerator(work, python); err != nil {
		return v, fmt.Errorf("полный прогон генератора %s: %w\n%s", suiteDir, err, out)
	}
	full, err := readCollections(outDir)
	if err != nil {
		return v, err
	}
	if len(full) == 0 {
		return v, fmt.Errorf("%s: полный прогон не произвёл ни одной коллекции — "+
			"сверять нечего, и зелёный здесь означал бы «ничего не прочитано»", suiteDir)
	}

	modules := make([]string, 0, len(full))
	for name := range full {
		modules = append(modules, name)
	}
	sort.Strings(modules)
	v.Modules = len(modules)

	// Режим 2 — по одному модулю за отдельный запуск интерпретатора. Именно
	// отдельный процесс и есть предмет: состояние, накопленное предыдущими
	// модулями, существует только внутри одного прогона.
	for _, m := range modules {
		if out, err := runGenerator(work, python, m); err != nil {
			return v, fmt.Errorf("одиночный прогон %s модуля %s: %w\n%s", suiteDir, m, err, out)
		}
		got, err := os.ReadFile(filepath.Join(outDir, collectionFileName(m)))
		if err != nil {
			return v, fmt.Errorf("%s: одиночный прогон модуля %s не произвёл своей коллекции: %w",
				suiteDir, m, err)
		}
		want := full[m]
		v.BytesCompared += len(want)
		if bytes.Equal(got, want) {
			continue
		}
		v.Findings = append(v.Findings, fmt.Sprintf(
			"%s: модуль %q, сгенерированный в одиночку, расходится с ним же из полного прогона.\n"+
				"    %s",
			filepath.Join(suiteDir, generatorRelPath), m, firstDifference(want, got)))
	}
	return v, nil
}

// collectionFileName — имя файла коллекции для модуля.
func collectionFileName(module string) string { return module + ".postman_collection.json" }

// runGenerator запускает генератор из каталога набора ровно так, как его
// запускает человек: `cd <набор> && python3 scripts/gen.py [модуль]`.
func runGenerator(suiteDir, python string, args ...string) (string, error) {
	cmd := exec.Command(python, append([]string{generatorRelPath}, args...)...)
	cmd.Dir = suiteDir
	// Байт-компиляция кладёт `__pycache__` рядом с исходником; в копии это
	// безвредно, но детерминизм вывода не должен зависеть от её наличия.
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// readCollections читает произведённые коллекции: имя модуля → содержимое.
func readCollections(outDir string) (map[string][]byte, error) {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return nil, fmt.Errorf("чтение %s: %w", outDir, err)
	}
	const suffix = ".postman_collection.json"
	got := make(map[string][]byte, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(outDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("чтение %s: %w", filepath.Join(outDir, e.Name()), err)
		}
		got[strings.TrimSuffix(e.Name(), suffix)] = body
	}
	return got, nil
}

// firstDifference называет первую разошедшуюся строку обеих версий. Отказ
// обязан показывать РАСХОЖДЕНИЕ, а не факт неравенства: по нему ставят диагноз,
// и «файлы различаются» диагнозом не является.
func firstDifference(want, got []byte) string {
	wl := strings.Split(string(want), "\n")
	gl := strings.Split(string(got), "\n")
	n := min(len(wl), len(gl))
	for i := range n {
		if wl[i] == gl[i] {
			continue
		}
		return fmt.Sprintf("строка %d: полный прогон дал %s, одиночный — %s",
			i+1, strings.TrimSpace(wl[i]), strings.TrimSpace(gl[i]))
	}
	return fmt.Sprintf("строк в полном прогоне %d, в одиночном %d", len(wl), len(gl))
}

// copyTreeExcept копирует дерево src в dst, пропуская каталоги верхнего уровня
// из skip. Пропускается только вывод генератора: всё остальное копируется, даже
// если сегодня не читается, — перечень «что генератору нужно» стал бы вторым
// местом об одном предмете и разошёлся бы с генератором молча.
//
// СОСТАВ БЕРЁТСЯ У ИНДЕКСА, А НЕ У ДИСКА, и это требование гейта
// TestTreeWalkersAskTheIndex, а не вкус. Обход диска под каталогом набора
// подхватил бы всё, что там лежит на конкретной машине: кэш байт-компиляции,
// распакованные чарты, каталоги сборки фронтенда, отчёты прошлых прогонов.
// Тогда вердикт «генерация детерминирована» стал бы свойством машины: на чистом
// клоне зелено, у того, кто вчера поднимал стенд, — красное или, хуже, зелёное
// по другой причине. Индекс отвечает одинаково везде.
//
// Пропуск `__pycache__` при этом сохранён отдельной ветвью и остаётся нужным:
// каталог не отслеживается, но интерпретатор создаёт его ВНУТРИ копии во время
// самого прогона, а сверяется он с исходником по времени правки и размеру —
// копия времени не сохраняет.
// fileLister — источник состава дерева: возвращает пути файлов под указанным
// каталогом. Реализации: trackedFiles (индекс git) и syntheticFiles (обход
// диска для дерева, собранного самой пробой).
type fileLister func(dir string) ([]string, error)

// trackedFiles — состав по индексу git. Авторитет для настоящего дерева.
func trackedFiles(dir string) ([]string, error) { return treecorpus.Under(dir) }

// syntheticFiles — состав по диску. Законен ТОЛЬКО для дерева, собранного самой
// проверкой во временном каталоге: репозиторием оно не является, спрашивать у
// него индекс нечего.
func syntheticFiles(dir string) ([]string, error) {
	t, err := treecorpus.SyntheticTree(dir)
	if err != nil {
		return nil, err
	}
	rels := t.SortedFiles()
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		out = append(out, filepath.Join(dir, rel))
	}
	return out, nil
}

func copyTreeExcept(src, dst string, listFiles fileLister, skip ...string) error {
	skipped := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipped[s] = true
	}
	tracked, err := listFiles(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, path := range tracked {
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(rel)
		if skipped[strings.SplitN(slash, "/", 2)[0]] ||
			strings.Contains("/"+slash+"/", "/__pycache__/") {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			// Индекс называет файл, которого на диске нет — это состояние
			// рабочей копии, а не дефект: копируем то, что есть.
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, body, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// TestGeneratedStepNamesDoNotDependOnHowManyModulesRan — гейт по дереву.
//
// Предмет и его цена — в шапке `generationdeterminism.go`. Здесь — обход:
// каждый набор сквозных проб исполняется в обоих объявленных режимах, и
// произведённое сверяется побайтово.
//
// Перепись печатается всегда: наборов осмотрено, модулей сверено, байт сверено.
// Без неё зелёный вердикт этого гейта неотличим от вердикта, при котором обход
// не нашёл ни одного генератора.
func TestGeneratedStepNamesDoNotDependOnHowManyModulesRan(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	suites := newmanSuites(t, root)
	if len(suites) == 0 {
		t.Fatal("генераторов сквозных проб в дереве не найдено — обход сломан, " +
			"а не дерево чисто")
	}

	python := pythonInterpreter(t)

	type result struct {
		v   suiteVerdict
		err error
	}
	// Временные каталоги раздаются ДО запуска горутин: предмет гейта —
	// детерминизм генератора, и вносить в него собственную конкуренцию нельзя.
	tmp := make([]string, len(suites))
	for i := range suites {
		tmp[i] = t.TempDir()
	}

	results := make(chan result, len(suites))
	for i, s := range suites {
		go func(suite, dir string) {
			v, err := compareGenerationModes(filepath.Join(root, suite), dir, python, trackedFiles)
			v.Suite = suite
			results <- result{v, err}
		}(s, tmp[i])
	}

	var (
		findings []string
		modules  int
		bytesCmp int
	)
	for range suites {
		r := <-results
		if r.err != nil {
			t.Fatalf("%s: %v", r.v.Suite, r.err)
		}
		modules += r.v.Modules
		bytesCmp += r.v.BytesCompared
		findings = append(findings, r.v.Findings...)
	}
	sort.Strings(findings)

	t.Logf("осмотрено: наборов %d, модулей сверено %d, байт коллекций сверено %d",
		len(suites), modules, bytesCmp)

	if modules == 0 {
		t.Fatal("ни один генератор не произвёл модулей — сверять было нечего, " +
			"и зелёный означал бы «ноль прочитанного»")
	}
	if len(findings) == 0 {
		return
	}
	t.Fatalf("имя шага зависит от того, сколько модулей обработал генератор, — %d "+
		"расхождений:\n%s\n\n"+
		"Величина, попадающая в имя шага, обязана быть функцией КЕЙСА. Счётчик на "+
		"уровне модуля Python, не сброшенный при загрузке следующего набора, делает "+
		"её функцией окружения: имя зависит от того, сколько наборов загрузилось "+
		"раньше. Следствие — объявленный режим `gen.py <модуль>` даёт дерево, "+
		"которого полный прогон не производит, а имена шагов, по которым ставят "+
		"диагноз при разборе красного, между прогонами не совпадают.\n"+
		"Исход: сбросить счётчик в `load_cases_module` — там же, где сбрасывается "+
		"счётчик имён опроса, и по той же причине.",
		len(findings), strings.Join(findings, "\n"))
}

// newmanSuites — каталоги наборов сквозных проб, выведенные из индекса git.
// Перечень не выписывается: выписанный разошёлся бы с деревом молча, и первым
// незамеченным стал бы новый набор — ровно тот, у которого счётчик скопировали
// у соседа вместе с дефектом.
func newmanSuites(t *testing.T, root string) []string {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files", "-z",
		"*/tests/newman/scripts/gen.py").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — без переписи «ноль находок» неотличимо от "+
			"«ноль прочитанного»", err)
	}
	var suites []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p == "" {
			continue
		}
		suites = append(suites, strings.TrimSuffix(p, "/"+generatorRelPath))
	}
	sort.Strings(suites)
	return suites
}

// pythonInterpreter — интерпретатор, которым исполняются генераторы.
//
// Отсутствие — ОТКАЗ, а не пропуск. Генерация предшествует каждому сквозному
// прогону, поэтому дерево без python3 не собирает сквозные пробы вовсе; гейт,
// молча пропустившийся в такой среде, отчитался бы зелёным о свойстве, которого
// не проверял.
func pythonInterpreter(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 не найден (%v): генераторы сквозных проб не исполнимы, "+
			"а значит их вердикт не существует — это не «ноль находок»", err)
	}
	return p
}

// sharedGenLayerRel — путь общего слоя генератора ОТ КОРНЯ дерева. Тот же, что
// ищет загрузчик в `scripts/gen.py` каждого набора; расхождение этих двух мест
// сделало бы копию неисполнимой, поэтому правятся они вместе.
const sharedGenLayerRel = "tests/newman/kacholib"

// materializeSharedGenLayer кладёт общий слой генератора рядом с копией набора —
// по ТОМУ ЖЕ правилу, по которому его ищет генератор: вверх от себя до каталога
// `tests/newman/kacholib`.
//
// Набор без общего слоя над собой (синтетический, собранный пробой инъекции во
// временном каталоге) — законный случай: его генератор общего слоя не
// импортирует. Поэтому ненайденный слой здесь НЕ отказ; отказ наступит там, где
// он и должен, — в самом генераторе, если тот попробует импортировать.
func materializeSharedGenLayer(suiteDir, tmpDir string) error {
	src := ""
	for dir := filepath.Clean(suiteDir); ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, sharedGenLayerRel)
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			src = candidate
			break
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	if src == "" {
		return nil // синтетический набор — общего слоя над ним нет by construction
	}

	dst := filepath.Join(tmpDir, sharedGenLayerRel)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	copied := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".py") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name())) // #nosec G304 -- путь получен обходом каталога дерева
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o600); err != nil {
			return err
		}
		copied++
	}
	if copied == 0 {
		return fmt.Errorf("каталог общего слоя %s найден, но модулей .py в нём нет: "+
			"копия набора была бы неисполнима, и вердикт стал бы свойством фикстуры", src)
	}
	return nil
}
