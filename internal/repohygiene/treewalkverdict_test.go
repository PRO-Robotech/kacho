// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	d := TreeWalkDenominator{
		Exact:           exact,
		Expression:      expression,
		BuildGraphEdges: TreeWalkBuildGraphEdges(t, root),
	}
	out, err := gitenv.Command(root, "ls-tree", "-r", "-z", "--name-only", "HEAD").Output()
	if err != nil {
		// Коммита нет — путей коммита ноль. Судья назовёт это несостоявшимся
		// обходом; отказывать здесь значит судить инструмент вместо дерева.
		return d
	}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		if keep == nil || keep(rel) {
			d.CommitPaths++
		}
	}
	return d
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

// treeWalkAsked — сколько раз судью позвали за прогон. Счётчик не для вердикта,
// а для МЕТА-ГЕЙТА: «ноль обращений за всю жизнь» разбирается как находка, а не
// как успех.
var treeWalkAsked = map[string]int{}

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
	treeWalkAsked[c.Gate]++

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
const treeWalkSilentCeiling = 603

// treeWalkRootFns — где обход берёт КОРЕНЬ ПРОДУКТА. Проба, не доходящая сюда,
// дерева продукта не читает: корень ей обязаны передать, а передают только
// временный каталог.
var treeWalkRootFns = map[string]bool{
	"repoRoot": true, "repoRootFromWD": true, "repoRootForCoverage": true,
}

// treeWalkPkgFuncs — объявления пакета проб, разобранные из исходников индекса.
type treeWalkPkgFuncs struct {
	decls map[string]*ast.FuncDecl
	file  map[string]string
}

// treeWalkParsePackage — разбирает `_test.go` пакета гигиены ПО ИНДЕКСУ git.
func treeWalkParsePackage(t *testing.T, root string) (treeWalkPkgFuncs, int) {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "internal/repohygiene/*_test.go").Output()
	if err != nil {
		t.Fatalf("состав пакета не установлен: %v — «проб ноль» означало бы "+
			"«ноль прочитанного»", err)
	}
	p := treeWalkPkgFuncs{decls: map[string]*ast.FuncDecl{}, file: map[string]string{}}
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

// reaches — транзитивно ли имя доходит до одной из целевых функций.
func (p treeWalkPkgFuncs) reaches(name string, target func(string) bool, seen map[string]bool) bool {
	if seen[name] {
		return false
	}
	seen[name] = true
	fd, ok := p.decls[name]
	if !ok {
		return false
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
		id, ok := ce.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if target(id.Name) || p.reaches(id.Name, target, seen) {
			hit = true
			return false
		}
		return true
	})
	return hit
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

	isRoot := func(n string) bool { return treeWalkRootFns[n] }
	isJudge := func(n string) bool { return n == "RequireTreeWalk" }

	var silent []string
	treeGates := 0
	for name := range pkg.decls {
		if !strings.HasPrefix(name, "Test") {
			continue
		}
		if !pkg.reaches(name, isRoot, map[string]bool{}) {
			continue // дерева продукта не читает: корень ему передают
		}
		treeGates++
		if pkg.reaches(name, isJudge, map[string]bool{}) {
			continue
		}
		silent = append(silent, name+" ("+pkg.file[name]+")")
	}
	sort.Strings(silent)

	RequireTreeWalk(t, TreeWalkCensus{
		Walked: files, Judged: files, Subjects: len(silent),
		Unit: "проб дерева без судьи",
	}, TreeWalkFloor(t, root, true, "git ls-tree -r HEAD -- internal/repohygiene/*_test.go",
		func(rel string) bool {
			return strings.HasPrefix(rel, "internal/repohygiene/") &&
				strings.HasSuffix(rel, "_test.go")
		}))

	t.Logf("проб дерева всего %d · спросили судью %d · НЕ спросили %d при потолке %d",
		treeGates, treeGates-len(silent), len(silent), treeWalkSilentCeiling)

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
