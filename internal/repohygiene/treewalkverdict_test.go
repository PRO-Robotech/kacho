// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// Разбор класса и устройство судьи — в шапке `treewalkverdict.go`. Здесь добыча
// знаменателя, помощник гейта и МЕТА-ГЕЙТ: он считает гейты дерева, ещё не
// спросившие судью, и краснеет при РОСТЕ этого числа.

// treeWalkEdgeRe — внутреннее ребро сборочного графа в файле модуля.
var treeWalkEdgeRe = regexp.MustCompile(`(?m)^\s*(github\.com/PRO-Robotech/[a-z0-9-]+)\s+v`)

// TreeWalkBuildGraphEdges — ВТОРОЕ выражение вопроса «что это за дерево»:
// внутренних рёбер сборочного графа в файле модуля КОММИТА.
//
// Читается у коммита, а не с диска: правка файла модуля в рабочем каталоге не
// обязана делать чужое дерево продуктом.
func TreeWalkBuildGraphEdges(t *testing.T, root string) int {
	t.Helper()
	out, err := gitenv.Command(root, "show", "HEAD:go.mod").Output()
	if err != nil {
		// Файла модуля в коммите нет — рёбер ноль, и это законный ответ:
		// судья назовёт дерево не-продуктом. Отказывать здесь нельзя, иначе
		// предпосылка судилась бы ошибкой инструмента, а не деревом.
		return 0
	}
	seen := map[string]struct{}{}
	for _, m := range treeWalkEdgeRe.FindAllStringSubmatch(string(out), -1) {
		seen[m[1]] = struct{}{}
	}
	return len(seen)
}

// TreeWalkFloor — знаменатель обхода, снятый ВТОРЫМ выражением.
//
// keep — тот же отбор, каким гейт отбирает свои пути. exact говорит, совпадает
// ли отбор точно: ложь означает, что отбор коммита ШИРЕ, и тогда требуется
// вложение, а не равенство.
func TreeWalkFloor(
	t *testing.T, root string, exact bool, expression string, keep func(rel string) bool,
) TreeWalkDenominator {
	t.Helper()
	pkgPath := reflect.TypeOf(TreeWalkCensus{}).PkgPath()
	module := treeWalkModulePath(t, root)
	d := TreeWalkDenominator{
		Exact:           exact,
		Expression:      expression,
		BuildGraphEdges: TreeWalkBuildGraphEdges(t, root),
		ModulePath:      module,
		PkgPath:         pkgPath,
	}
	// Каталог собственного пакета ВЫВОДИТСЯ из пары «путь пакета — модуль
	// дерева», а не выписывается: выписанный, он разъехался бы с переездом
	// пакета молча.
	if module != "" && strings.HasPrefix(pkgPath, module+"/") {
		selfDir := strings.TrimPrefix(pkgPath, module+"/")
		d.SelfFilesInCommit = treeWalkCountCommitPaths(t, root, func(rel string) bool {
			return strings.HasPrefix(rel, selfDir+"/")
		})
	}
	d.CommitPaths = treeWalkCountCommitPaths(t, root, keep)
	return d
}

// treeWalkModulePath — модуль, объявленный судимым деревом в КОММИТЕ.
//
// У коммита, а не с диска: правка файла модуля в рабочем каталоге не обязана
// делать чужое дерево нашим.
func treeWalkModulePath(t *testing.T, root string) string {
	t.Helper()
	out, err := gitenv.Command(root, "show", "HEAD:go.mod").Output()
	if err != nil {
		return ""
	}
	m := regexp.MustCompile(`(?m)^module\s+(\S+)`).FindSubmatch(out)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// treeWalkCountCommitPaths — путей КОММИТА, отобранных предикатом.
func treeWalkCountCommitPaths(t *testing.T, root string, keep func(rel string) bool) int {
	t.Helper()
	out, err := gitenv.Command(root, "ls-tree", "-r", "-z", "--name-only", "HEAD").Output()
	if err != nil {
		// Коммита нет — путей коммита ноль. Судья назовёт это несостоявшимся
		// обходом; отказывать здесь значит судить инструмент вместо дерева.
		return 0
	}
	n := 0
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		if keep == nil || keep(rel) {
			n++
		}
	}
	return n
}

// TrackedPaths — ПЕРВОЕ выражение состава дерева: пути ИНДЕКСА git, отобранные
// предикатом самого гейта.
//
// Индекс, а не диск: посторонний каталог рядом с репозиторием не имеет права
// влиять на вердикт. Пара к нему — `TreeWalkFloor`, спрашивающая КОММИТ.
func TrackedPaths(t *testing.T, root string, keep func(rel string) bool) []string {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("состав дерева не установлен: %v — «ноль находок» означало бы "+
			"«ноль прочитанного»", err)
	}
	var kept []string
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		if keep == nil || keep(rel) {
			kept = append(kept, rel)
		}
	}
	return kept
}

// Общие отборы путей. Отбор ОДИН на оба выражения — индекс и коммит: разные
// отборы сравнивали бы разное и расходились бы молча.
func treeWalkKeepAll(rel string) bool { return rel != "" }

func treeWalkKeepGo(rel string) bool { return strings.HasSuffix(rel, ".go") }

func treeWalkKeepProdGo(rel string) bool {
	return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") && !skipPath(rel)
}

func treeWalkKeepChart(rel string) bool {
	if !strings.HasPrefix(rel, "deploy/") {
		return false
	}
	return strings.HasSuffix(rel, ".yaml") || strings.HasSuffix(rel, ".yml") ||
		strings.HasSuffix(rel, ".tpl")
}

// requireTreeWalkOver — общий зов судьи: первое выражение состава берётся у
// индекса, второе — у коммита, ТЕМ ЖЕ отбором.
func requireTreeWalkOver(
	t *testing.T, root string, keep func(string) bool, expression, unit string, subjects int,
) {
	t.Helper()
	paths := TrackedPaths(t, root, keep)
	RequireTreeWalk(t, TreeWalkCensus{
		Gate: t.Name(), Walked: len(paths), Judged: len(paths),
		Subjects: subjects, Unit: unit,
	}, TreeWalkFloor(t, root, true, expression, keep))
}

// requireTreeWalkOverCorpus — зов судьи для гейта, чей ПРЕДМЕТ и есть корпус:
// там «предмета нет» означало бы пустое дерево, и рулит предпосылка продукта.
func requireTreeWalkOverCorpus(
	t *testing.T, root string, keep func(string) bool, expression, unit string,
) {
	t.Helper()
	paths := TrackedPaths(t, root, keep)
	RequireTreeWalk(t, TreeWalkCensus{
		Gate: t.Name(), Walked: len(paths), Judged: len(paths),
		Subjects: len(paths), Unit: unit,
	}, TreeWalkFloor(t, root, true, expression, keep))
}

// ЗДЕСЬ СТОЯЛ СЧЁТЧИК ЗОВОВ, И ОН СНЯТ ЦЕЛИКОМ
//
// Это была общая карта пакета без охраны, а пишут сюда 28 проб, каждая после
// `t.Parallel()`. Под детектором гонок — гонка; без него — паника «concurrent
// map writes», которая убивает ВЕСЬ бинарь проб, то есть уносит вердикт всех
// гейтов дерева, а не одного. Локальным прогоном это не ловилось и не могло:
// на машине замера нет компилятора C, и `-race` там не собирается вовсе.
//
// Читателя у карты не было ни одного: три вхождения на пакет — объявление,
// инкремент и комментарий, назвавший читателем мета-гейт. Мета-гейт считает
// РАЗБОРОМ исходников, а не счётчиком прогона, так что комментарий был вторым
// ложным утверждением о поведении в этом же файле. Риск был куплен ни за что.
//
// Если счётчик понадобится, он заводится с настоящим читателем и под охраной —
// и называется в переписи мета-гейта, а не рядом с ней.

// RequireTreeWalk — ТРЁХИСХОДНЫЙ вердикт обхода. Зовётся каждым гейтом дерева.
//
// Печатает перепись и знаменатель ВСЕГДА: «находок ноль» без знаменателя
// неотличимо от «ноль прочитанного». Красит гейт, если обход не состоялся, и
// называет ВСЕ причины, а не первую.
func RequireTreeWalk(t *testing.T, c TreeWalkCensus, d TreeWalkDenominator) TreeWalkVerdict {
	t.Helper()
	if c.Gate == "" {
		c.Gate = t.Name()
	}
	v := JudgeTreeWalk(c, d)
	unit := c.Unit
	if unit == "" {
		unit = "предметов"
	}
	t.Logf("ОБХОД %s: путей обойдено %d · судимо %d · %s %d || ЗНАМЕНАТЕЛЬ (второе "+
		"выражение): путей в коммите %d (%s, отбор %s) · внутренних рёбер сборочного "+
		"графа %d || ИСХОД: %s",
		c.Gate, c.Walked, c.Judged, unit, c.Subjects,
		d.CommitPaths, d.Expression, treeWalkExactWord(d.Exact), d.BuildGraphEdges, v.Outcome)
	t.Logf("ЯКОРЬ %s: дерево объявляет модуль %q · гейт собран из пакета %q · файлов "+
		"собственного пакета в коммите %d", c.Gate, d.ModulePath, d.PkgPath, d.SelfFilesInCommit)

	if v.Outcome == TreeWalkFailed {
		for _, reason := range v.Blind {
			t.Errorf("обход не состоялся: %s", reason)
		}
		t.Fatalf("гейт %s: исход %q — вердикта о предмете НЕТ, и молчание гейта ничего "+
			"не утверждает. Причин названо: %d", c.Gate, v.Outcome, len(v.Blind))
	}
	return v
}

func treeWalkExactWord(exact bool) string {
	if exact {
		return "точный"
	}
	return "шире обхода"
}

// ─────────────────────────────────────────────────────────────────────────────
// МЕТА-ГЕЙТ: гейт, забывший спросить судью, неотличим от гейта, которому нечего
// сказать.

// treeWalkSilentCeiling — УБЫВАЮЩИЙ потолок: проб о дереве, ещё НЕ спросивших
// судью.
//
// Единица — проба `Test…`, доходящая до корня продукта (транзитивно по вызовам
// внутри пакета) и не зовущая `RequireTreeWalk` ни сама, ни через помощника.
//
// Число получено прогоном этого же мета-гейта на ревизии, которой он заведён.
// Запись меняется ТОЛЬКО ВНИЗ и тем же изменением, которым число снижено: рост
// означает новый гейт дерева, у которого «ничего не нашёл» неотличимо от «не
// искал», и ловиться это обязано в тот же день, а не через полгода.
const treeWalkSilentCeiling = 649

// treeWalkUnknownFormCeiling — проб, читающих дерево корнем, которого обход не
// производит. Держится потолком по той же причине: форма, не сводимая к
// примитиву среды, обязана быть заметна числом, а не молчать.
const treeWalkUnknownFormCeiling = 0

// ─────────────────────────────────────────────────────────────────────────────
// ОТОЗВАННОЕ УТВЕРЖДЕНИЕ
//
// Сообщение коммита, которым заведён этот мета-гейт, называло `os.Getwd`
// частью предиката: «проба, не доходящая до repoRoot / repoRootFromWD /
// repoRootForCoverage / os.Getwd, дерева продукта не читает». КОД ЭТОГО НЕ
// ДЕЛАЛ: разбор читал только `*ast.Ident`, а `os.Getwd` — вызов через
// селектор, и до сверки он не доходил ни разу. Утверждение было ложным с
// рождения — ровно тот класс, который этот пакет ловит по всему дереву.
//
// Записано здесь, а не в истории: история не переписывается, а читатель
// предиката обязан узнать, что о нём однажды сказали неправду.

// treeWalkRootPrimitives — ПЕРВИЧНЫЕ источники корня: те, что берут его у СРЕДЫ,
// а не получают аргументом.
//
// Это перечень ПРИМИТИВОВ, а не перечень форм. Формы — функции вроде `repoRoot`
// или `repoRootForCoverage` — здесь НЕ выписаны: они ПРОИЗВОДЯТСЯ обходом графа
// вызовов пакета от этих примитивов, и потому пятая форма появляется в переписи
// сама, без правки словаря.
//
// ЦЕНА НАЗВАНА: способ, построенный не на этих примитивах — вшитый абсолютный
// путь, каталог из ответа внешней программы, значение из окружения, — обходом
// не производится. Он не молчит: проба, читающая дерево и не доходящая ни до
// одного примитива, считается отдельным числом «форма неизвестна» и держится
// своим потолком.
//
// Прежняя редакция выписывала ТРИ имени форм памятью и сверяла их по простому
// имени вызова. Из-за этого `os.Getwd` и `runtime.Caller` — вызовы через
// селектор — до сверки не доходили НИКОГДА по построению разбора, и настоящий
// гейт дерева, добывающий корень четвёртым способом, проходил мимо мета-гейта
// молча. Разбор ниже читает и `Ident`, и `SelectorExpr`.
var treeWalkRootPrimitives = map[string]bool{
	"os.Getwd":       true,
	"runtime.Caller": true,
	"os.Executable":  true,
	// `filepath.Abs` от ОТНОСИТЕЛЬНОГО пути — тоже рабочий каталог, просто
	// спрошенный не по имени. Внесён не по памяти: счётчик «форма неизвестна»
	// назвал восемь проб, и все восемь свелись к `repoRootFor`, который
	// добывает корень именно так. Ровно ради этого счётчик и заведён: форма,
	// которой обход не знает, обязана звучать числом, а не молчать.
	"filepath.Abs": true,
	// Пятая форма — не вызов, а ЗАПИСЬ: относительный путь литералом.
	// Опознаётся отдельной веткой разбора, см. treeWalkCarriesRelativeRoot.
	treeWalkRelativeRootMark: true,
}

// treeWalkCarriesRelativeRoot — функция берёт корень ОТНОСИТЕЛЬНЫМ ПУТЁМ: в её
// теле стоит строковый литерал, начинающийся с `..`.
//
// Это пятая форма добычи корня, и она не имя, а ФОРМА ЗАПИСИ: `auditX(t, "../..")`
// доходит до рабочего каталога, не позвав ни одного примитива среды. Признак
// выведен разбором узла, а не перечислен, поэтому шестое такое место найдётся
// само.
func treeWalkCarriesRelativeRoot(fd *ast.FuncDecl, relConsts map[string]bool) bool {
	found := false
	ast.Inspect(fd, func(n ast.Node) bool {
		if found {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && relConsts[id.Name] {
			found = true
			return false
		}
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if val == ".." || strings.HasPrefix(val, "../") {
			found = true
			return false
		}
		return true
	})
	return found
}

// collectRelativeRootConsts — объявления пакета, чьё значение есть
// относительный путь. Читается значение, а не имя: имя может быть любым.
func collectRelativeRootConsts(gd *ast.GenDecl, into map[string]bool) {
	if gd.Tok != token.CONST && gd.Tok != token.VAR {
		return
	}
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range vs.Names {
			if i >= len(vs.Values) {
				continue
			}
			lit, ok := vs.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				continue
			}
			if val == ".." || strings.HasPrefix(val, "../") {
				into[name.Name] = true
			}
		}
	}
}

// treeWalkTreePrimitives — чем читают дерево. Нужны, чтобы отличить пробу,
// которая дерево ЧИТАЕТ, но корень добыла неизвестным способом, от синтетики,
// которой корень передали.
var treeWalkTreePrimitives = map[string]bool{
	"gitenv.Command":     true,
	"treecorpus.NewTree": true,
	"filepath.WalkDir":   true,
	"filepath.Walk":      true,
	"os.ReadDir":         true,
}

// treeWalkGivenRoot — корень ПЕРЕДАН, а не добыт: временный каталог пробы.
var treeWalkGivenRoot = map[string]bool{"t.TempDir": true}

// treeWalkPkgFuncs — объявления пакета проб, разобранные из исходников индекса.
type treeWalkPkgFuncs struct {
	decls map[string]*ast.FuncDecl
	file  map[string]string
	// relConsts — имена объявлений пакета, чьё значение есть ОТНОСИТЕЛЬНЫЙ путь.
	// Собираются обходом объявлений, а не выписываются: шестая форма добычи
	// корня — константа с путём — иначе была бы невидима так же, как была
	// невидима четвёртая.
	relConsts map[string]bool
}

// treeWalkParsePackage — разбирает `_test.go` пакета гигиены ПО ИНДЕКСУ git.
func treeWalkParsePackage(t *testing.T, root string) (treeWalkPkgFuncs, int) {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "internal/repohygiene/*_test.go").Output()
	if err != nil {
		t.Fatalf("состав пакета не установлен: %v — «проб ноль» означало бы "+
			"«ноль прочитанного»", err)
	}
	p := treeWalkPkgFuncs{
		decls: map[string]*ast.FuncDecl{}, file: map[string]string{},
		relConsts: map[string]bool{},
	}
	files := 0
	fset := token.NewFileSet()
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) // #nosec G304 -- путь из индекса своего дерева
		if rerr != nil {
			t.Fatalf("чтение %s: %v", rel, rerr)
		}
		f, perr := parser.ParseFile(fset, rel, raw, 0)
		if perr != nil {
			t.Fatalf("разбор %s: %v — молчаливый пропуск превратил бы «не прочитали» "+
				"в «нарушений нет»", rel, perr)
		}
		files++
		for _, d := range f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok {
				collectRelativeRootConsts(gd, p.relConsts)
				continue
			}
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Recv != nil {
				continue
			}
			p.decls[fd.Name.Name] = fd
			p.file[fd.Name.Name] = rel
		}
	}
	return p, files
}

// treeWalkCallName — имя вызываемого, КАК ОНО НАПИСАНО: `Ident` даёт простое
// имя, `SelectorExpr` — `пакет.Имя`.
//
// Прежняя редакция читала только `Ident`, и потому целый род вызовов —
// `os.Getwd`, `runtime.Caller` — не доходил до сверки ни разу.
func treeWalkCallName(ce *ast.CallExpr) string {
	switch fn := ce.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return "." + fn.Sel.Name
	}
	return ""
}

// reaches — транзитивно ли имя доходит до целевого вызова. Через `SelectorExpr`
// не спускается: чужой пакет здесь не разобран, и это названо границей.
func (p treeWalkPkgFuncs) reaches(name string, target func(string) bool, seen map[string]bool) bool {
	if seen[name] {
		return false
	}
	seen[name] = true
	fd, ok := p.decls[name]
	if !ok {
		return false
	}
	if target(treeWalkRelativeRootMark) && treeWalkCarriesRelativeRoot(fd, p.relConsts) {
		return true
	}
	hit := false
	ast.Inspect(fd, func(n ast.Node) bool {
		if hit {
			return false
		}
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		called := treeWalkCallName(ce)
		if called == "" {
			return true
		}
		if target(called) || p.reaches(called, target, seen) {
			hit = true
			return false
		}
		return true
	})
	return hit
}

// treeWalkRelativeRootMark — имя пятой формы в наборе примитивов. Вызовом не
// является и потому опознаётся отдельной веткой разбора.
const treeWalkRelativeRootMark = "<относительный путь к корню>"

// treeWalkRootProducers — формы добычи корня, ПРОИЗВЕДЁННЫЕ обходом: всякая
// функция пакета, транзитивно доходящая до примитива среды.
func (p treeWalkPkgFuncs) treeWalkRootProducers() []string {
	var out []string
	for name := range p.decls {
		if strings.HasPrefix(name, "Test") {
			continue
		}
		if p.reaches(name, func(n string) bool { return treeWalkRootPrimitives[n] },
			map[string]bool{}) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// TestEveryTreeGateAsksTheWalkJudge — гейт, забывший спросить судью, ловится в
// тот же день.
//
// СЧИТАЕТ, а не перечисляет: ведомость исключений здесь слепла бы ровно тогда,
// когда опустеет. Число проб дерева, не спросивших судью, печатается и
// сравнивается с УБЫВАЮЩИМ потолком: рост — находка, убывание — повод
// переписать запись тем же изменением.
func TestEveryTreeGateAsksTheWalkJudge(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	pkg, files := treeWalkParsePackage(t, root)

	isRoot := func(n string) bool { return treeWalkRootPrimitives[n] }
	isJudge := func(n string) bool { return n == "RequireTreeWalk" }
	isTree := func(n string) bool { return treeWalkTreePrimitives[n] }
	isGiven := func(n string) bool { return treeWalkGivenRoot[n] }

	producers := pkg.treeWalkRootProducers()

	var silent, unknownForm []string
	treeGates := 0
	for name := range pkg.decls {
		if !strings.HasPrefix(name, "Test") {
			continue
		}
		if !pkg.reaches(name, isRoot, map[string]bool{}) {
			// Корень до примитива среды не доходит. Два разных случая, и
			// смешивать их нельзя: корень ПЕРЕДАН (синтетика) либо добыт
			// СПОСОБОМ, которого обход не производит, — и второй молчать не
			// имеет права.
			if pkg.reaches(name, isTree, map[string]bool{}) &&
				!pkg.reaches(name, isGiven, map[string]bool{}) {
				unknownForm = append(unknownForm, name+" ("+pkg.file[name]+")")
			}
			continue
		}
		treeGates++
		if pkg.reaches(name, isJudge, map[string]bool{}) {
			continue
		}
		silent = append(silent, name+" ("+pkg.file[name]+")")
	}
	sort.Strings(silent)
	sort.Strings(unknownForm)

	RequireTreeWalk(t, TreeWalkCensus{
		Walked: files, Judged: files, Subjects: len(silent),
		Unit: "проб дерева без судьи",
	}, TreeWalkFloor(t, root, true, "git ls-tree -r HEAD -- internal/repohygiene/*_test.go",
		func(rel string) bool {
			return strings.HasPrefix(rel, "internal/repohygiene/") &&
				strings.HasSuffix(rel, "_test.go")
		}))

	t.Logf("формы добычи корня ПРОИЗВЕДЕНЫ обходом от %d примитивов среды: "+
		"производителей %d (%s)", len(treeWalkRootPrimitives), len(producers),
		strings.Join(producers, ", "))
	t.Logf("проб дерева всего %d · спросили судью %d · НЕ спросили %d при потолке %d · "+
		"форма корня неизвестна у %d при потолке %d",
		treeGates, treeGates-len(silent), len(silent), treeWalkSilentCeiling,
		len(unknownForm), treeWalkUnknownFormCeiling)
	if len(producers) == 0 {
		t.Fatal("производителей корня ноль — перечень форм не произведён, и «все спросили " +
			"судью» было бы вердиктом о непрочитанном")
	}
	if len(unknownForm) > treeWalkUnknownFormCeiling {
		var head []string
		for i, u := range unknownForm {
			if i >= 15 {
				break
			}
			head = append(head, u)
		}
		t.Errorf("проб, читающих дерево корнем НЕИЗВЕСТНОЙ формы, %d при потолке %d (+%d): "+
			"обход таких форм не производит, и их молчание неотличимо от «не искал». "+
			"Либо сведите форму к примитиву среды, либо внесите примитив в "+
			"treeWalkRootPrimitives вместе с инъекцией.\n  %s",
			len(unknownForm), treeWalkUnknownFormCeiling,
			len(unknownForm)-treeWalkUnknownFormCeiling, strings.Join(head, "\n  "))
	}

	switch {
	case len(silent) > treeWalkSilentCeiling:
		var grew []string
		for i, s := range silent {
			if i >= 20 {
				break
			}
			grew = append(grew, s)
		}
		t.Errorf("проб дерева без судьи %d при потолке %d (+%d): у каждой из них "+
			"«ничего не нашёл» неотличимо от «не искал». Позовите RequireTreeWalk либо "+
			"снимите пробу.\nпервые из перечня:\n  %s",
			len(silent), treeWalkSilentCeiling, len(silent)-treeWalkSilentCeiling,
			strings.Join(grew, "\n  "))
	case len(silent) < treeWalkSilentCeiling:
		t.Errorf("проб дерева без судьи %d при потолке %d (−%d): перепишите "+
			"treeWalkSilentCeiling на %d ТЕМ ЖЕ изменением — потолок, переживший своё "+
			"число, прощает возврат ровно настолько, насколько успели починить",
			len(silent), treeWalkSilentCeiling, treeWalkSilentCeiling-len(silent), len(silent))
	}
	if treeGates == 0 {
		t.Fatal("проб дерева ноль — предикат меряет не то, и «все спросили судью» " +
			"было бы вердиктом о непрочитанном")
	}
}
