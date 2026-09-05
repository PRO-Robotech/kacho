// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// authzwrapperoutcomelanes_test.go — булева ОБЁРТКА вопроса о правах не
// вызывается там, где у неё есть парная форма с исходом (задача #1045).
//
// # Почему соседний гейт этот класс не видит — by construction, а не по недосмотру
//
// `TestRelationCheckOutcomeLanesAreNotCollapsed` судит УПОТРЕБЛЕНИЕ ошибки:
// он находит `…Check(ctx, s, r, o)` и спрашивает, можно ли из связанной ошибки
// получить исход. Здесь ошибки в точке вызова НЕТ ВОВСЕ — она потеряна ВНУТРИ
// обёртки, на которую вызывающий смотрит как на `bool`. Смотреть тому гейту не
// на что: вопроса к модели в этом файле не написано.
//
// Итог для обоих гейтов, и он важнее каждого из них: «класс закрыт» верно ровно
// в пределах формы, которую гейт умеет прочесть. Два гейта здесь делят один
// класс по РАЗНЫМ признакам — употребление ошибки и выбор формы вызова, — и ни
// один не является частным случаем другого.
//
// # Что ищется — СВОЙСТВО, и оно ВЫВОДИТСЯ ИЗ ДЕРЕВА
//
// Перечня имён у этого гейта нет и быть не может: выписанный перечень разошёлся
// бы с деревом молча, и восьмая обёртка приехала бы под наблюдение, которого
// нет. Поэтому пары выводятся:
//
//	F  — экспортированная функция пакета, возвращающая РОВНО `bool`;
//	FE — функция ТОГО ЖЕ пакета с именем `F`+`E` либо `F`+`PlainE`,
//	     возвращающая `(bool, error)`.
//
// Есть пара ⇒ автор пакета УЖЕ объявил, что у этого вопроса три исхода, а не
// два. Значит вызов булевой половины из ЧУЖОГО пакета — выбор, а не
// необходимость, и выбран он в пользу формы, из которой «хранилище не ответило»
// достать нельзя.
//
// Самоистечение встроено: снимут E-форму — пара исчезнет, и гейт перестанет
// обвинять; заведут новую пару в другом пакете — она попадёт под наблюдение
// сама, без правки этого файла.
//
// # Граница названа честно
//
// Вызовы ВНУТРИ пакета, объявившего пару, под гейт не подпадают: там булева
// половина и есть тело обёртки (`IsClusterAdmin` зовёт `SubjectIsClusterAdmin`),
// и обвинять её в собственной реализации бессмысленно. Следствие: перенеся
// решение о доступе внутрь того же пакета, гейт можно обойти. Это граница
// синтаксического признака, а не послабление, и она печатается переписью —
// «пакетов-владельцев» видно, и разрастание такого пакета заметно.
//
// Второе: узнавание идёт по ИМЕНИ ИМПОРТА файла, а не по типам — разрешать типы
// значило бы грузить пакеты ради всего дерева. Дословный импорт под алиасом
// распознаётся, импорт через точку — нет; в дереве таких нет, и перепись это
// показывает числом.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// modulePath — путь модуля, чтобы импорт файла сопоставлялся с каталогом дерева.
// Читается из go.mod, а не выписывается: разъедется — перепись найдёт ноль пар.
func treeModulePath(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("go.mod не читается: %w", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("в go.mod нет строки module")
}

// wrapperPair — вопрос, у которого автор пакета объявил ОБЕ формы.
type wrapperPair struct {
	pkgDir   string   // каталог пакета относительно корня дерева
	boolName string   // булева половина
	eNames   []string // ВСЕ половины с исходом, а не первая найденная
}

// outcomeForms — половины с исходом, перечисленные для сообщения.
//
// Их бывает больше одной, и это не избыточность: `SubjectIsClusterAdmin` имеет
// и `…E`, и `…PlainE` — один вопрос, два ПОРТА. Назвав одну, гейт послал бы
// правящего провязывать второй порт там, где нужный у него уже есть.
func (p wrapperPair) outcomeForms() string { return strings.Join(p.eNames, " либо ") }

// boolWrapperCall — одно употребление булевой половины из чужого пакета.
type boolWrapperCall struct {
	file string
	line int
	pair wrapperPair
}

// wrapperScanReport — что именно осмотрено. Печатается ВСЕГДА.
type wrapperScanReport struct {
	roots      []string
	files      int
	generated  int
	dotImports int
	pairs      []wrapperPair
	found      []boolWrapperCall
}

// TestAuthzBoolWrapperIsNotCalledWhereAnOutcomeFormExists — по всему прод-дереву:
// булева обёртка, у которой есть парная форма с исходом, из чужого пакета не
// зовётся.
func TestAuthzBoolWrapperIsNotCalledWhereAnOutcomeFormExists(t *testing.T) {
	root := repoRoot(t)

	roots, err := prodGoRoots(root)
	if err != nil {
		t.Fatalf("%v", err)
	}
	rep, err := scanBoolWrapperCalls(root, roots)
	if err != nil {
		t.Fatalf("%v", err)
	}

	owners := map[string]struct{}{}
	names := make([]string, 0, len(rep.pairs))
	for _, p := range rep.pairs {
		owners[p.pkgDir] = struct{}{}
		names = append(names, p.pkgDir+"."+p.boolName+"→"+p.outcomeForms())
	}
	sort.Strings(names)
	t.Logf("осмотрено: каталогов=%d (%s), файлов Go прочитано=%d (сгенерённых пропущено=%d), "+
		"импортов через точку=%d, пар «булева↔с исходом» выведено=%d в %d пакетах-владельцах [%s], "+
		"употреблений булевой половины из чужого пакета=%d",
		len(rep.roots), strings.Join(rep.roots, ", "), rep.files, rep.generated,
		rep.dotImports, len(rep.pairs), len(owners), strings.Join(names, "; "), len(rep.found))

	if len(rep.roots) == 0 {
		t.Fatalf("предпосылка нарушена: каталогов с не-тестовым кодом Go не найдено — "+
			"состав дерева не прочитан, зелёное здесь не значит ничего (файлов %d)", rep.files)
	}
	if rep.files == 0 {
		t.Fatal("предпосылка нарушена: не прочитано ни одного файла Go")
	}
	// Предпосылка ГЕЙТА: пары в дереве есть. Ноль означает, что соглашение об
	// именовании парной формы сменилось (`…E` / `…PlainE`), — и тогда гейт судит
	// пустоту, а зелёное на нём ничего не значит.
	if len(rep.pairs) == 0 {
		t.Fatalf("предпосылка нарушена: ни одной пары «булева↔с исходом» в дереве не найдено — "+
			"соглашение об именовании (суффикс E / PlainE при возврате (bool, error)) сменилось; "+
			"пока это не выяснено, гейт не судит ничего (файлов прочитано %d)", rep.files)
	}

	found := rep.found
	sort.Slice(found, func(i, j int) bool {
		if found[i].file != found[j].file {
			return found[i].file < found[j].file
		}
		return found[i].line < found[j].line
	})
	for _, c := range found {
		t.Errorf("%s:%d — зовётся булева половина %s.%s, у которой В ТОМ ЖЕ ПАКЕТЕ есть парная "+
			"форма %s. Из булевой «хранилище прав не ответило» достать нельзя: отказ теряется "+
			"ВНУТРИ обёртки, и вызывающий читает недоступность как «не положено». На списочном "+
			"пути это well-formed `200` с молча суженной страницей, неотличимый от отзыва прав. "+
			"Замена drop-in: тот же порт, тот же вопрос, третий исход",
			c.file, c.line, c.pair.pkgDir, c.pair.boolName, c.pair.outcomeForms())
	}
}

// scanBoolWrapperCalls — два прохода по одному корпусу: сперва вывести пары,
// затем найти их употребления. Порядок обязателен — искать нечего, пока не
// известно, что искать.
func scanBoolWrapperCalls(root string, roots []string) (wrapperScanReport, error) {
	var rep wrapperScanReport

	mod, err := treeModulePath(root)
	if err != nil {
		return rep, err
	}

	var corpus []parsedProdFile
	fset := token.NewFileSet()

	for _, dir := range roots {
		abs := filepath.Join(root, dir)
		if st, serr := os.Stat(abs); serr != nil || !st.IsDir() {
			continue
		}
		rep.roots = append(rep.roots, dir)
		tracked, terr := treecorpus.Under(abs)
		if terr != nil {
			return wrapperScanReport{}, fmt.Errorf("состав дерева под %s не читается: %w", dir, terr)
		}
		for _, file := range tracked {
			if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
				continue
			}
			rel, rerr := filepath.Rel(root, file)
			if rerr != nil {
				return wrapperScanReport{}, fmt.Errorf("относительный путь для %s: %w", file, rerr)
			}
			rel = filepath.ToSlash(rel)
			f, perr := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution|parser.ParseComments)
			if perr != nil {
				return wrapperScanReport{}, fmt.Errorf("разбор %s: %w", rel, perr)
			}
			// Сгенерённое не осматривается: у порождённого кода нет автора,
			// которому адресован упрёк.
			if isGeneratedFile(f) {
				rep.generated++
				continue
			}
			rep.files++
			corpus = append(corpus, parsedProdFile{rel: rel, dir: pathDirOf(rel), file: f})
		}
	}

	rep.pairs = derivePairs(exportedFuncsByDir(corpus))
	byDir := map[string][]wrapperPair{}
	for _, p := range rep.pairs {
		byDir[p.pkgDir] = append(byDir[p.pkgDir], p)
	}

	for _, p := range corpus {
		dots, calls := boolWrapperCallsInFile(fset, p.file, p.rel, p.dir, mod, byDir)
		rep.dotImports += dots
		rep.found = append(rep.found, calls...)
	}
	return rep, nil
}

// ── вывод пар ───────────────────────────────────────────────────────────────

// parsedProdFile — один прочитанный не-тестовый файл дерева.
type parsedProdFile struct {
	rel  string
	dir  string
	file *ast.File
}

// pathDirOf — каталог пути в слэш-форме («.» для файла в корне).
func pathDirOf(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return "."
}

// pkgFuncs — сигнатуры функций верхнего уровня одного каталога-пакета.
type pkgFuncs map[string]map[string]*ast.FuncDecl // dir → name → decl

func exportedFuncsByDir(corpus []parsedProdFile) pkgFuncs {
	out := pkgFuncs{}
	for _, p := range corpus {
		if out[p.dir] == nil {
			out[p.dir] = map[string]*ast.FuncDecl{}
		}
		for _, d := range p.file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue // методы и неэкспортированное чужим пакетом не зовутся
			}
			out[p.dir][fn.Name.Name] = fn
		}
	}
	return out
}

// derivePairs — из сигнатур каталога выводит пары «булева ↔ с исходом».
func derivePairs(byDir pkgFuncs) []wrapperPair {
	var out []wrapperPair
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		funcs := byDir[dir]
		names := make([]string, 0, len(funcs))
		for n := range funcs {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			if !returnsExactlyBool(funcs[name]) {
				continue
			}
			var eNames []string
			for _, suffix := range []string{"E", "PlainE"} {
				if cand, ok := funcs[name+suffix]; ok && returnsBoolAndError(cand) {
					eNames = append(eNames, name+suffix)
				}
			}
			if len(eNames) > 0 {
				out = append(out, wrapperPair{pkgDir: dir, boolName: name, eNames: eNames})
			}
		}
	}
	return out
}

// returnsExactlyBool — ровно один результат, и он `bool`.
func returnsExactlyBool(fn *ast.FuncDecl) bool {
	res := fn.Type.Results
	if res == nil || len(res.List) != 1 || len(res.List[0].Names) > 1 {
		return false
	}
	return identNamed(res.List[0].Type, "bool")
}

// returnsBoolAndError — ровно `(bool, error)`, в этом порядке.
func returnsBoolAndError(fn *ast.FuncDecl) bool {
	res := fn.Type.Results
	if res == nil {
		return false
	}
	var types []ast.Expr
	for _, f := range res.List {
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			types = append(types, f.Type)
		}
	}
	return len(types) == 2 && identNamed(types[0], "bool") && identNamed(types[1], "error")
}

func identNamed(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// ── поиск употреблений ──────────────────────────────────────────────────────

// boolWrapperCallsInFile — вызовы булевых половин ЧУЖИХ пакетов в одном файле.
func boolWrapperCallsInFile(
	fset *token.FileSet, f *ast.File, rel, dir, mod string, byDir map[string][]wrapperPair,
) (dotImports int, found []boolWrapperCall) {
	// alias → каталог пакета в дереве. Импорты чужих модулей отбрасываются: их
	// путь не начинается с пути нашего модуля.
	local := map[string]string{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if imp.Name != nil && imp.Name.Name == "." {
			dotImports++
			continue
		}
		pkgDir, ok := strings.CutPrefix(path, mod+"/")
		if !ok {
			continue
		}
		alias := ""
		switch {
		case imp.Name != nil && imp.Name.Name != "_":
			alias = imp.Name.Name
		default:
			alias = path[strings.LastIndex(path, "/")+1:]
		}
		if alias != "" {
			local[alias] = pkgDir
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true // метод значения, а не вызов функции пакета
		}
		pkgDir, ok := local[pkgIdent.Name]
		if !ok {
			return true
		}
		if pkgDir == dir {
			return true // свой же пакет: булева половина здесь и реализуется
		}
		for _, p := range byDir[pkgDir] {
			if p.boolName == sel.Sel.Name {
				found = append(found, boolWrapperCall{
					file: rel, line: fset.Position(call.Pos()).Line, pair: p,
				})
				break
			}
		}
		return true
	})
	return dotImports, found
}
