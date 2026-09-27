// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package stackenv

// census_test.go — стражи старта каждого композиционного корня покрыты пробой
// стендов (kacho#941).
//
// Покрыт страж, если проба корня (`stackenv.Probe`) зовёт функцию, из которой он
// достижим по графу вызовов пакета. Страж — функция НЕ-пробного кода корня с
// именем `require…`/`validate…`: так их называет само дерево (перепись задачи
// #941 искала их этим же образцом). Корень выводится из дерева: пакет процесса,
// чей не-пробный код зовёт `servicehost.Serve`. Состав — из индекса git.

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

	"github.com/PRO-Robotech/corelib/treecorpus"
)

const treeRoot = "../.."

var guardName = regexp.MustCompile(`^(require|validate)[A-Z]`)

type rootCensus struct {
	dir       string
	guards    []string // стражи корня
	probed    bool     // проба корня зовёт stackenv.Probe
	uncovered []string // стражи, не достижимые из пробы
}

// callIdents — имена функций пакета, которые зовёт тело.
func callIdents(n ast.Node) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(n, func(x ast.Node) bool {
		if c, ok := x.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok {
				out[id.Name] = true
			}
		}
		return true
	})
	return out
}

func callsSel(n ast.Node, pkg, name string) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		c, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == pkg {
				found = true
			}
		}
		return true
	})
	return found
}

// censusOfRoot — перепись одного корня по файлам его каталога.
func censusOfRoot(dir string, files []string) (rootCensus, error) {
	c := rootCensus{dir: dir}
	graph := map[string]map[string]bool{}
	probeCalls := map[string]bool{}
	for _, p := range files {
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			return c, err
		}
		test := strings.HasSuffix(p, "_test.go")
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if !test {
				graph[fd.Name.Name] = callIdents(fd.Body)
				if fd.Recv == nil && guardName.MatchString(fd.Name.Name) {
					c.guards = append(c.guards, fd.Name.Name)
				}
				continue
			}
			if callsSel(fd.Body, "stackenv", "Probe") {
				c.probed = true
				for k := range callIdents(fd.Body) {
					probeCalls[k] = true
				}
			}
		}
	}
	reach := map[string]bool{}
	var walk func(string)
	walk = func(fn string) {
		if reach[fn] {
			return
		}
		reach[fn] = true
		for callee := range graph[fn] {
			walk(callee)
		}
	}
	for fn := range probeCalls {
		walk(fn)
	}
	sort.Strings(c.guards)
	for _, g := range c.guards {
		if !reach[g] {
			c.uncovered = append(c.uncovered, g)
		}
	}
	return c, nil
}

// rootsOf — корни среди данных файлов: каталоги под /cmd/, где НЕ-пробный код
// зовёт servicehost.Serve.
func rootsOf(files []string) (map[string][]string, error) {
	byDir := map[string][]string{}
	for _, p := range files {
		byDir[filepath.Dir(p)] = append(byDir[filepath.Dir(p)], p)
	}
	roots := map[string][]string{}
	for dir, fs := range byDir {
		if !strings.Contains(filepath.ToSlash(dir), "/cmd/") {
			continue
		}
		for _, p := range fs {
			if strings.HasSuffix(p, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
			if err != nil {
				return nil, err
			}
			if callsSel(f, "servicehost", "Serve") {
				roots[dir] = fs
				break
			}
		}
	}
	return roots, nil
}

func TestEveryCompositionRootsStartGuardsAreFedByEveryStack(t *testing.T) {
	root, err := filepath.Abs(treeRoot)
	if err != nil {
		t.Fatal(err)
	}
	files, err := treecorpus.Glob(filepath.Join(root, "services/*/cmd/*/*.go"))
	if err != nil {
		t.Fatalf("состав дерева не прочитан: %v", err)
	}
	roots, err := rootsOf(files)
	if err != nil {
		t.Fatal(err)
	}
	dirs := make([]string, 0, len(roots))
	for d := range roots {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	probed, guards, covered := 0, 0, 0
	for _, d := range dirs {
		c, err := censusOfRoot(d, roots[d])
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, d)
		guards += len(c.guards)
		covered += len(c.guards) - len(c.uncovered)
		if !c.probed {
			t.Errorf("%s: композиционный корень без пробы стендов stackenv.Probe — профиль, не объявивший "+
				"того, что требуют его стражи, узнается только подъёмом стенда", rel)
			continue
		}
		probed++
		for _, g := range c.uncovered {
			t.Errorf("%s: страж старта %s не достижим из пробы стендов — ни одна цепочка профилей ему не "+
				"подаётся. Позови его из функции стражей, которую зовут и процесс, и проба", rel, g)
		}
	}
	t.Logf("осмотрено файлов %d · композиционных корней %d · с пробой стендов %d · стражей старта (require…/validate…) %d · "+
		"достижимы из пробы %d", len(files), len(dirs), probed, guards, covered)
	if len(dirs) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: композиционных корней не найдено среди %d файлов", len(files))
	}
}

// Самопроверка переписи: страж, достижимый из пробы, покрыт; недостижимый — нет;
// корень без пробы назван.
func TestStackCensusSelfCheck(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	main := write("main.go", "package main\n\nfunc startGuards() error { return requireA() }\n"+
		"func requireA() error { return nil }\nfunc requireOrphan() error { return nil }\n"+
		"func serve() { servicehost.Serve(nil, nil, nil, nil) }\n")
	probe := write("stack_test.go", "package main\n\nfunc TestX() { stackenv.Probe(nil, stackenv.Service{Boot: func(stackenv.Env) error { return startGuards() }}) }\n")

	c, err := censusOfRoot(dir, []string{main, probe})
	if err != nil {
		t.Fatal(err)
	}
	if !c.probed || len(c.guards) != 2 || len(c.uncovered) != 1 || c.uncovered[0] != "requireOrphan" {
		t.Fatalf("перепись не та: %+v", c)
	}
	unprobed, err := censusOfRoot(dir, []string{main})
	if err != nil {
		t.Fatal(err)
	}
	if unprobed.probed || len(unprobed.uncovered) != 2 {
		t.Fatalf("корень без пробы не распознан: %+v", unprobed)
	}
	// Корень выводится по вызову носителя в не-пробном коде под /cmd/: пакет,
	// где вызов стоит только в пробе, корнем не является.
	mk := func(rel, body string) string {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	rootFile := mk("svc/cmd/a/main.go", "package main\n\nfunc run() { servicehost.Serve(nil, nil, nil, nil) }\n")
	testOnly := mk("svc/cmd/b/x_test.go", "package main\n\nfunc TestX() { servicehost.Serve(nil, nil, nil, nil) }\n")
	outside := mk("svc/internal/c/c.go", "package c\n\nfunc run() { servicehost.Serve(nil, nil, nil, nil) }\n")
	roots, err := rootsOf([]string{rootFile, testOnly, outside})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[filepath.Dir(rootFile)] == nil {
		t.Fatalf("корни выведены не те: %v", roots)
	}
}
