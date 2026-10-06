// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notifyspecsingular_corpus_test.go — корпус деревьев НА ПИНАХ для гейтов
// дерева NTF-1, судящих «одна функция на всю платформу» (NTF1-A09 — валидатор
// формата шаблона; NTF1-M08 и УК1 — субъект служебного вызывающего).
//
// # Что читается и почему так
//
// Предмет этих гейтов живёт не только в kacho: валидатор формата и функция
// субъекта — в corelib, а пины задают, КАКОЙ corelib (и kaname) дерево kacho
// собирает. Поэтому корпус — три дерева: kacho по индексу git (вердикт —
// свойство коммита, а не рабочего каталога), corelib и kaname — каталоги кэша
// модулей ровно тех версий, что закреплены в go.mod этого дерева. Только
// не-тестовые `.go` вне `testdata`: проба, читающая шаблон своей фикстурой, —
// не реализация формата.
//
// # Как опознаётся цель обращения
//
// Узлом разбора, а не словом: селектор `x.Name` засчитывается за объект пакета
// P, только если `x` — имя, которым ЭТОТ файл импортирует P (явное или
// известное имя пакета). Для функций и констант уровня пакета это и есть
// идентичность объекта: полное имя `путь.Имя` единственно. Точечный импорт
// пакета-цели делает селектор невидимым — он сам находка гейта, а не молчание.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// Пути модулей деревьев корпуса.
const (
	kachoModulePath  = "github.com/PRO-Robotech/kacho"
	kanameModulePath = "github.com/PRO-Robotech/kaname"
)

// pinnedGoFile — один разобранный не-тестовый Go-файл дерева на пине.
type pinnedGoFile struct {
	tree    string // kacho | corelib | kaname
	rel     string // путь от корня дерева (через «/»)
	pkgPath string // импортный путь пакета файла
	file    *ast.File
}

// pinnedCorpus — корпус деревьев и объём осмотренного.
type pinnedCorpus struct {
	fset     *token.FileSet
	files    []pinnedGoFile
	versions map[string]string // дерево → версия (kacho — «коммит дерева»)
	perTree  map[string]int    // дерево → файлов
	broken   []string          // файлы, не прошедшие разбор
}

// pos — координата узла в форме «дерево:путь:строка».
func (c *pinnedCorpus) pos(f pinnedGoFile, p token.Pos) string {
	return f.tree + ":" + f.rel + ":" + strconv.Itoa(c.fset.Position(p).Line)
}

// census — строка объёма осмотренного.
func (c *pinnedCorpus) census() string {
	trees := make([]string, 0, len(c.perTree))
	for tr := range c.perTree {
		trees = append(trees, tr)
	}
	sort.Strings(trees)
	parts := make([]string, 0, len(trees))
	for _, tr := range trees {
		parts = append(parts, tr+"@"+c.versions[tr]+" файлов "+strconv.Itoa(c.perTree[tr]))
	}
	return strings.Join(parts, " · ")
}

var (
	pinnedCorpusMu    sync.Mutex
	pinnedCorpusCache = map[string]*pinnedCorpus{}
)

// pinnedTreesCorpus — корпус деревьев trees («kacho», «corelib», «kaname») для
// корня root. Пустое дерево — отказ прогона, а не пустой корпус.
func pinnedTreesCorpus(t *testing.T, root string, trees ...string) *pinnedCorpus {
	t.Helper()
	key := root + "|" + strings.Join(trees, ",")
	pinnedCorpusMu.Lock()
	defer pinnedCorpusMu.Unlock()
	if c, ok := pinnedCorpusCache[key]; ok {
		return c
	}
	c := &pinnedCorpus{fset: token.NewFileSet(), versions: map[string]string{}, perTree: map[string]int{}}
	for _, tree := range trees {
		var srcs map[string][]byte
		var module string
		switch tree {
		case "kacho":
			srcs, module = kachoTrackedGoFiles(t, root), kachoModulePath
			c.versions[tree] = "дерево"
		case "corelib":
			dir, version := corelibModuleDir(t, root)
			srcs, module = moduleDirGoFiles(t, dir), corelibModulePath
			c.versions[tree] = version
		case "kaname":
			dir, version := pinnedModuleDir(t, root, kanameModulePath)
			srcs, module = moduleDirGoFiles(t, dir), kanameModulePath
			c.versions[tree] = version
		default:
			t.Fatalf("дерево корпуса %q неизвестно", tree)
		}
		if len(srcs) == 0 {
			t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: дерево %s не дало ни одного Go-файла", tree)
		}
		rels := make([]string, 0, len(srcs))
		for rel := range srcs {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		for _, rel := range rels {
			f, err := parser.ParseFile(c.fset, tree+":"+rel, srcs[rel], parser.SkipObjectResolution)
			if err != nil {
				c.broken = append(c.broken, tree+":"+rel+": "+err.Error())
				continue
			}
			pkgPath := module
			if d := path.Dir(rel); d != "." {
				pkgPath += "/" + d
			}
			c.files = append(c.files, pinnedGoFile{tree: tree, rel: rel, pkgPath: pkgPath, file: f})
			c.perTree[tree]++
		}
	}
	pinnedCorpusCache[key] = c
	return c
}

// kachoTrackedGoFiles — не-тестовые Go-файлы индекса kacho вне testdata.
func kachoTrackedGoFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	tracked, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: состав дерева %s: %v", root, err)
	}
	out := map[string][]byte{}
	for _, p := range tracked {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatalf("путь %s вне корня %s", p, root)
		}
		rel = filepath.ToSlash(rel)
		if skipCorpusPath(rel) {
			continue
		}
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("чтение %s: %v", p, err)
		}
		out[rel] = body
	}
	return out
}

// moduleDirGoFiles — не-тестовые Go-файлы каталога модуля из кэша.
func moduleDirGoFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_") ||
				d.Name() == "testdata" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || skipCorpusPath(rel) {
			return nil
		}
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		out[rel] = body
		return nil
	})
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: обход модуля %s: %v", dir, err)
	}
	return out
}

// skipCorpusPath — тестовый файл либо фикстура: предметом корпуса не являются.
func skipCorpusPath(rel string) bool {
	if strings.HasSuffix(rel, "_test.go") {
		return true
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "testdata" {
			return true
		}
	}
	return false
}

// pinnedModuleDir — каталог кэша модуля module той версии, что закреплена в
// go.mod дерева root.
func pinnedModuleDir(t *testing.T, root, module string) (dir, version string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("чтение go.mod: %v", err)
	}
	for _, dep := range ParseGoModRequires(string(body)) {
		if dep.Path == module {
			return ModuleCacheDir(moduleCacheDir(t), dep), dep.Version
		}
	}
	t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: go.mod не закрепляет %s", module)
	return "", ""
}

// pinnedImportNames — локальные имена импортов файла: имя → путь; точечные
// импорты — отдельным перечнем путей.
func pinnedImportNames(f *ast.File, known map[string]string) (names map[string]string, dot []string) {
	names = map[string]string{}
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		switch {
		case spec.Name != nil && spec.Name.Name == ".":
			dot = append(dot, p)
		case spec.Name != nil && spec.Name.Name == "_":
		case spec.Name != nil:
			names[spec.Name.Name] = p
		case known[p] != "":
			names[known[p]] = p
		default:
			names[path.Base(p)] = p
		}
	}
	return names, dot
}

// selectorTarget — путь пакета и имя объекта, если e — селектор `x.Name`, где
// x — имя импорта файла; иначе пусто.
func selectorTarget(e ast.Expr, names map[string]string) (pkg, name string) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", ""
	}
	p, ok := names[x.Name]
	if !ok {
		return "", ""
	}
	return p, sel.Sel.Name
}

// funcOwner — полное имя объявления функции: `путь.Имя` либо `путь.(Тип).Имя`.
func funcOwner(pkgPath string, fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return pkgPath + "." + fd.Name.Name
	}
	typ := fd.Recv.List[0].Type
	for {
		switch x := typ.(type) {
		case *ast.StarExpr:
			typ = x.X
			continue
		case *ast.IndexExpr:
			typ = x.X
			continue
		case *ast.IndexListExpr:
			typ = x.X
			continue
		}
		break
	}
	recv := "?"
	if id, ok := typ.(*ast.Ident); ok {
		recv = id.Name
	}
	return pkgPath + ".(" + recv + ")." + fd.Name.Name
}

// packageStringConsts — строковые константы уровня пакета (литерал значения)
// по пакетам корпуса: путь → имя → значение.
func packageStringConsts(c *pinnedCorpus) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, f := range c.files {
		for _, decl := range f.file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, n := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					if out[f.pkgPath] == nil {
						out[f.pkgPath] = map[string]string{}
					}
					out[f.pkgPath][n.Name] = v
				}
			}
		}
	}
	return out
}

// foldString — значение строкового константного выражения: литерал, константа
// своего пакета, константа импортированного пакета корпуса, сложение таких.
func foldString(e ast.Expr, pkgPath string, names map[string]string, consts map[string]map[string]string) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(x.Value)
		return v, err == nil
	case *ast.ParenExpr:
		return foldString(x.X, pkgPath, names, consts)
	case *ast.Ident:
		v, ok := consts[pkgPath][x.Name]
		return v, ok
	case *ast.SelectorExpr:
		p, n := selectorTarget(x, names)
		if p == "" {
			return "", false
		}
		v, ok := consts[p][n]
		return v, ok
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, ok := foldString(x.X, pkgPath, names, consts)
		if !ok {
			return "", false
		}
		r, ok := foldString(x.Y, pkgPath, names, consts)
		if !ok {
			return "", false
		}
		return l + r, true
	}
	return "", false
}
