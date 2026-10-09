// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// edge_retired_knobs_layer_test.go — семейство «рендер чарта края в пробе
// снятых ручек» (`edge_retired_knobs_render_test.go`) получает число доверенных
// прыжков ТОЛЬКО слоем `deploy/testdata/notify-standalone/edge.yaml` (приёмка
// NTF-2 Р8, строка «рендер чарта края без зонтика», §6б (1); решения Д51, Д52;
// замысел issue-2917 З28 (а), М71 — база вызова этого семейства — `deploy/`).
//
// Судится по РАЗБОРУ исходника, а не по тексту: каждый вызов
// `exec.Command("helm", "template", …)`, среди аргументов которого есть имя
// `edgeChartDir`, обязан нести пару литералов `"-f"`, `"<путь>"`, и путь,
// разрешённый от каталога пакета (`deploy/`), тождествен файлу слоя дерева —
// по нормализованному абсолютному пути, не по подстроке. Аргумент `--set` в
// таком вызове — значение мимо каталога, находка. Ноль найденных вызовов —
// «не выполнилось»: смотреть было не на что.
package deploy_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	retiredKnobsRenderFile = "edge_retired_knobs_render_test.go"
	edgeLayerFromRoot      = "deploy/testdata/notify-standalone/edge.yaml"
)

// edgeLayerCall — найденный вызов helm на чарте края.
type edgeLayerCall struct {
	pos      string
	layers   []string // разрешённые абсолютные пути `-f`
	spelled  []string // написание `-f` в исходнике
	sets     int      // аргументов `--set`
	nonConst int      // не-литеральных аргументов после `-f`
}

// edgeLayerCalls — вызовы `exec.Command("helm", …)` с идентификатором
// chartIdent среди аргументов; base — каталог, от которого разрешается `-f`.
func edgeLayerCalls(fset *token.FileSet, f *ast.File, chartIdent, base string) []edgeLayerCall {
	var out []edgeLayerCall
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Command" {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "exec" || len(call.Args) == 0 {
			return true
		}
		if lit, ok := call.Args[0].(*ast.BasicLit); !ok || lit.Value != `"helm"` {
			return true
		}
		onEdge := false
		for _, a := range call.Args {
			if id, ok := a.(*ast.Ident); ok && id.Name == chartIdent {
				onEdge = true
			}
		}
		if !onEdge {
			return true
		}
		c := edgeLayerCall{pos: fset.Position(call.Pos()).String()}
		for i, a := range call.Args {
			lit, ok := a.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			v, _ := strconv.Unquote(lit.Value)
			switch v {
			case "--set", "--set-string", "--set-file", "--set-json":
				c.sets++
			case "-f", "--values":
				if i+1 >= len(call.Args) {
					c.nonConst++
					continue
				}
				nl, ok := call.Args[i+1].(*ast.BasicLit)
				if !ok || nl.Kind != token.STRING {
					c.nonConst++
					continue
				}
				p, _ := strconv.Unquote(nl.Value)
				c.spelled = append(c.spelled, p)
				c.layers = append(c.layers, filepath.Clean(filepath.Join(base, p)))
			}
		}
		out = append(out, c)
		return true
	})
	return out
}

// judgeEdgeLayerCalls — находки по найденным вызовам; want — абсолютный путь
// файла слоя дерева.
func judgeEdgeLayerCalls(calls []edgeLayerCall, want string) []string {
	var f []string
	for _, c := range calls {
		switch {
		case c.nonConst > 0:
			f = append(f, fmt.Sprintf("%s: `-f` не литералом — база вызова не устанавливается разбором", c.pos))
		case c.sets > 0:
			f = append(f, fmt.Sprintf("%s: `--set` в рендере чарта края — значение мимо каталога слоя (Д52)", c.pos))
		}
		hit := false
		for i, l := range c.layers {
			if l == want {
				hit = true
				continue
			}
			if _, err := os.Stat(l); err != nil {
				f = append(f, fmt.Sprintf("%s: слой %q разрешается в %s — такого файла нет", c.pos, c.spelled[i], l))
			} else {
				f = append(f, fmt.Sprintf("%s: слой %q разрешается в %s — файл значений ручки вне %s",
					c.pos, c.spelled[i], l, edgeLayerFromRoot))
			}
		}
		if !hit && c.nonConst == 0 {
			f = append(f, fmt.Sprintf("%s: слоя %s в вызове нет — рендер без зонтика отказывает (Д51)", c.pos, edgeLayerFromRoot))
		}
	}
	return f
}

func edgeLayerWant(t *testing.T) (pkgDir, want string) {
	t.Helper()
	pkgDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	want = filepath.Join(filepath.Dir(pkgDir), edgeLayerFromRoot)
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("слоя %s в дереве нет (%v) — после D6 файл — продукт полосы", edgeLayerFromRoot, err)
	}
	return pkgDir, want
}

func TestRetiredKnobsEdgeRenderCarriesTheLayer(t *testing.T) {
	pkgDir, want := edgeLayerWant(t)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, retiredKnobsRenderFile, nil, 0)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не разобрался: %v", retiredKnobsRenderFile, err)
	}
	calls := edgeLayerCalls(fset, f, "edgeChartDir", pkgDir)
	if len(calls) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в %s нет ни одного вызова helm на edgeChartDir — смотреть было не на что",
			retiredKnobsRenderFile)
	}
	for _, c := range calls {
		t.Logf("  %s: слой %v → %v; --set %d", c.pos, c.spelled, c.layers, c.sets)
	}
	for _, fd := range judgeEdgeLayerCalls(calls, want) {
		t.Error(fd)
	}
	t.Logf("перепись семейства %s: вызовов helm на чарте края %d; ожидаемый слой %s",
		retiredKnobsRenderFile, len(calls), want)
}

// Инъекции на синтетическом исходнике, близнец — литерал от базы `deploy/`.
func TestRetiredKnobsEdgeLayerJudgeFiresAndStaysSilent(t *testing.T) {
	pkgDir, want := edgeLayerWant(t)
	src := func(args string) string {
		return "package p\nimport \"os/exec\"\nfunc f() { exec.Command(\"helm\", \"template\", \"r\", edgeChartDir, " +
			args + ") }\n"
	}
	judge := func(args string) []string {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", src(args), 0)
		if err != nil {
			t.Fatalf("синтетика не разобралась: %v", err)
		}
		calls := edgeLayerCalls(fset, f, "edgeChartDir", pkgDir)
		if len(calls) != 1 {
			t.Fatalf("синтетика: вызовов %d, ожидался 1", len(calls))
		}
		return judgeEdgeLayerCalls(calls, want)
	}
	if f := judge(`"-f", "testdata/notify-standalone/edge.yaml"`); len(f) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %q", f)
	}
	cases := map[string]string{
		`"-f", "deploy/testdata/notify-standalone/edge.yaml"`: "такого файла нет",
		`"--set", "trustedHops=1"`:                            "--set",
		`"-f", "testdata/notify-standalone/values.yaml"`:      "вне",
		`"-n", "kacho"`: "слоя",
		`"-f", layer`:   "не литералом",
		`"-f", "testdata/notify-standalone/edge.yaml", "--set", "trustedHops=2"`: "--set",
	}
	for args, want := range cases {
		f := judge(args)
		if len(f) == 0 || !strings.Contains(strings.Join(f, "\n"), want) {
			t.Errorf("инъекция %s: ожидалась находка «%s», получено %q", args, want, f)
		}
	}
}
