// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dnscheck

// querynames_census_test.go — перепись вызовов `LookupTXT` в прод-коде
// `services/notify` (CX1-138 (в), §12а «Имена запросов абсолютные»): имя
// запроса каждого вызова — вызов [queryName], а не литерал и не склейка.
// Имя без завершающей точки под `ndots: 5` пода сначала ушло бы в домены
// поиска кластера, и проверка судила бы чужую зону.
//
// Разбор — узлами синтаксиса: вызов — `*ast.CallExpr` с селектором
// `LookupTXT`, аргумент имени (второй) — `*ast.CallExpr` с именем `queryName`.

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

// lookupCensus — вызовы LookupTXT в srcs (имя → текст) и находки.
func lookupCensus(t *testing.T, srcs map[string]string) (int, []string) {
	t.Helper()
	fset := token.NewFileSet()
	names := make([]string, 0, len(srcs))
	for n := range srcs {
		names = append(names, n)
	}
	sort.Strings(names)
	calls := 0
	var findings []string
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, srcs[name], parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не разобран: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "LookupTXT" {
				return true
			}
			calls++
			pos := fset.Position(ce.Pos())
			if len(ce.Args) != 2 {
				findings = append(findings, fmt.Sprintf("%s:%d: LookupTXT не в форме (ctx, имя)", pos.Filename, pos.Line))
				return true
			}
			arg, ok := ce.Args[1].(*ast.CallExpr)
			id, isIdent := (*ast.Ident)(nil), false
			if ok {
				id, isIdent = arg.Fun.(*ast.Ident)
			}
			if !ok || !isIdent || id.Name != "queryName" {
				findings = append(findings, fmt.Sprintf("%s:%d: имя запроса LookupTXT не вызов queryName", pos.Filename, pos.Line))
			}
			return true
		})
	}
	return calls, findings
}

// notifyProdSources — прод-файлы дерева services/notify (вне _test.go).
func notifyProdSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(filepath.Join("..", ".."), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
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
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: обход services/notify: %v", err)
	}
	return out
}

func TestEveryLookupTXTAsksAnAbsoluteQueryName(t *testing.T) {
	srcs := notifyProdSources(t)
	calls, findings := lookupCensus(t, srcs)
	t.Logf("прод-файлов прочитано %d · вызовов LookupTXT %d", len(srcs), calls)
	if len(srcs) == 0 || calls == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файлов %d, вызовов LookupTXT %d — «все через queryName» на пустом обходе "+
			"не утверждает ничего", len(srcs), calls)
	}
	if len(findings) > 0 {
		t.Fatalf("имя запроса DNS не через queryName:\n  %s", strings.Join(findings, "\n  "))
	}
}

// Инъекция в обе стороны: литерал имени — находка с координатой; вызов
// queryName — тишина.
func TestLookupCensusCanFail(t *testing.T) {
	const head = "package x\n\nfunc f() {\n"
	bad := head + "\t_, _ = r.LookupTXT(ctx, \"_dmarc.example.test\")\n}\n"
	calls, f := lookupCensus(t, map[string]string{"x/a.go": bad})
	if calls != 1 || len(f) != 1 || !strings.Contains(f[0], "x/a.go:4") {
		t.Fatalf("литерал имени не стал находкой с координатой: вызовов %d, находки %v", calls, f)
	}
	good := head + "\t_, _ = r.LookupTXT(ctx, queryName(CheckDMARC, \"\", d))\n}\n"
	if calls, f := lookupCensus(t, map[string]string{"x/a.go": good}); calls != 1 || len(f) != 0 {
		t.Fatalf("близнец через queryName дал находку: вызовов %d, находки %v", calls, f)
	}
}
