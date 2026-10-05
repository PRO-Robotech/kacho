// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// starts_census_test.go — перепись in-process стартов notify (замысел §12а
// «Процессные пробы и integration-старты», CX1-131 (в), (г)): каждый вызов
// runServe в пробах дерева `services/notify` стоит на зоне испытания
// `dnscheck/dnstest`. Старт без зоны судил бы резолвер машины прогона — его
// исход зависел бы от сети, а не от предмета пробы.
//
// Разбор — узлами синтаксиса, не текстом: вызов — `*ast.CallExpr` с именем
// `runServe`; «со зоной» — резолвер вызова (третий аргумент) несёт обращение
// к пакету `dnstest` сам, через переменную, присвоенную из него в той же
// функции, либо через вызов функции пакета пробы, тело которой к `dnstest`
// обращается. Иная форма — находка с координатой.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// usesDNSTest — поддерево обращается к пакету dnstest (селектор `dnstest.X`).
func usesDNSTest(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "dnstest" {
				found = true
			}
		}
		return !found
	})
	return found
}

// startsCensus — вызовы runServe в исходниках srcs (имя → текст), из них со
// зоной испытания, и находки.
func startsCensus(t *testing.T, srcs map[string]string) (starts, withZone int, findings []string) {
	t.Helper()
	fset := token.NewFileSet()
	type pkgFiles struct{ funcs map[string]*ast.FuncDecl }
	pkgs := map[string]*pkgFiles{} // каталог → функции пакета пробы
	type call struct {
		pkg  string
		fn   *ast.FuncDecl
		expr *ast.CallExpr
	}
	var calls []call
	names := make([]string, 0, len(srcs))
	for n := range srcs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, srcs[name], parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не разобран: %v", name, err)
		}
		dir := filepath.Dir(name)
		if pkgs[dir] == nil {
			pkgs[dir] = &pkgFiles{funcs: map[string]*ast.FuncDecl{}}
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if fd.Recv == nil {
				pkgs[dir].funcs[fd.Name.Name] = fd
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := ce.Fun.(*ast.Ident); ok && id.Name == "runServe" {
					calls = append(calls, call{pkg: dir, fn: fd, expr: ce})
				}
				return true
			})
		}
	}
	for _, c := range calls {
		starts++
		pos := fset.Position(c.expr.Pos())
		where := fmt.Sprintf("%s:%d (%s)", pos.Filename, pos.Line, c.fn.Name.Name)
		if len(c.expr.Args) != 3 {
			findings = append(findings, where+": вызов runServe не в форме (cfg, logger, resolver)")
			continue
		}
		if resolverFromZone(c.expr.Args[2], c.fn, pkgs[c.pkg].funcs) {
			withZone++
			continue
		}
		findings = append(findings, where+": резолвер старта не из зоны испытания dnstest")
	}
	return starts, withZone, findings
}

// resolverFromZone — резолвер arg получен из dnstest: прямо, через
// переменную функции fn, присвоенную выражением с dnstest, либо вызовом
// функции пакета, тело которой обращается к dnstest.
func resolverFromZone(arg ast.Expr, fn *ast.FuncDecl, funcs map[string]*ast.FuncDecl) bool {
	if usesDNSTest(arg) {
		return true
	}
	zoneVars := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, l := range as.Lhs {
			if id, ok := l.(*ast.Ident); ok && usesDNSTest(as.Rhs[i]) {
				zoneVars[id.Name] = true
			}
		}
		return true
	})
	hit := false
	ast.Inspect(arg, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			if zoneVars[x.Name] {
				hit = true
			}
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok {
				if helper := funcs[id.Name]; helper != nil && helper != fn && usesDNSTest(helper.Body) {
					hit = true
				}
			}
		}
		return !hit
	})
	return hit
}

// notifyTestSources — пробы дерева services/notify (каталог службы — два
// уровня выше корня процесса).
func notifyTestSources(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[p] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: обход проб services/notify: %v", err)
	}
	return out
}

func TestEveryInProcessStartStandsOnTheTestZone(t *testing.T) {
	srcs := notifyTestSources(t)
	starts, withZone, findings := startsCensus(t, srcs)
	t.Logf("проб прочитано %d · стартов %d, со зоной %d", len(srcs), starts, withZone)
	if len(srcs) == 0 || starts == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: проб %d, стартов runServe %d — переписывать нечего, «все со зоной» на пустом "+
			"обходе не утверждает ничего", len(srcs), starts)
	}
	if len(findings) > 0 {
		t.Fatalf("старт notify без зоны испытания:\n  %s", strings.Join(findings, "\n  "))
	}
}

// Инъекция в обе стороны: старт на резолвере машины — находка с координатой;
// законные формы (прямо, через переменную, через помощника пакета) — тишина.
func TestStartsCensusCanFail(t *testing.T) {
	const head = "package main\n\nimport (\n\t\"net\"\n\t\"testing\"\n)\n\n"
	t.Run("старт на net.DefaultResolver — находка", func(t *testing.T) {
		src := head + "func TestX(t *testing.T) { _ = runServe(cfg, nil, net.DefaultResolver) }\n"
		starts, with, f := startsCensus(t, map[string]string{"p/x_test.go": src})
		if starts != 1 || with != 0 || len(f) != 1 || !strings.Contains(f[0], "p/x_test.go:8") {
			t.Fatalf("инъекция не стала находкой с координатой: стартов %d, со зоной %d, находки %v", starts, with, f)
		}
	})
	t.Run("помощник без dnstest — находка", func(t *testing.T) {
		src := head + "func zone(t *testing.T) *net.Resolver { return &net.Resolver{} }\n" +
			"func TestX(t *testing.T) { _ = runServe(cfg, nil, zone(t)) }\n"
		if _, _, f := startsCensus(t, map[string]string{"p/x_test.go": src}); len(f) != 1 {
			t.Fatalf("помощник без зоны не стал находкой: %v", f)
		}
	})
	for name, body := range map[string]string{
		"прямо":               "func TestX(t *testing.T) { _ = runServe(cfg, nil, dnstest.Start(t).Resolver()) }\n",
		"через переменную":    "func TestX(t *testing.T) { z := dnstest.Start(t); _ = runServe(cfg, nil, z.Resolver()) }\n",
		"через помощника":     "func zone(t *testing.T) *net.Resolver { return dnstest.Start(t).Resolver() }\nfunc TestX(t *testing.T) { _ = runServe(cfg, nil, zone(t)) }\n",
		"в литерале горутины": "func TestX(t *testing.T) { z := dnstest.Start(t); go func() { _ = runServe(cfg, nil, z.Resolver()) }() }\n",
	} {
		t.Run("близнец "+name, func(t *testing.T) {
			starts, with, f := startsCensus(t, map[string]string{"p/x_test.go": head + body})
			if starts != 1 || with != 1 || len(f) != 0 {
				t.Fatalf("законная форма дала находку: стартов %d, со зоной %d, находки %v", starts, with, f)
			}
		})
	}
}
