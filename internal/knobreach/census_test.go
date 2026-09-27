// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package knobreach

// census_test.go — гейт ЗОВУТ все пакеты процессов, чьи пробы задают имена
// ручек (kacho#2737).
//
// Гейт живёт в каждом пакете процесса, а не здесь: загрузчик службы лежит под
// её `internal/`, и правило языка не пускает общий пакет его импортировать.
// Значит пакет, забывший позвать гейт, выпадает из наблюдения МОЛЧА — и эта
// перепись та проверка, что выпадения нет. Состав дерева берётся из индекса
// git, а не с диска: рядом с деревом лежат чужие копии и распаковки.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// treeRoot — корень дерева продукта относительно каталога пакета.
const treeRoot = "../.."

// cmdProbePatterns — где живут пакеты процессов: службы и край.
var cmdProbePatterns = []string{
	"services/*/cmd/*/*_test.go",
	"gateway/cmd/*/*_test.go",
}

// gateCall — как пакет зовёт гейт: зовёт ли вовсе и передаёт ли каталоги текстов.
type gateCall struct {
	calls     bool
	withProse bool
}

// findGateCall ищет в пробах каталога вызов `knobreach.Gate(…, knobreach.Package{…})`
// узлом разбора, а не текстом: имя в комментарии вызовом не является.
func findGateCall(dir string) (gateCall, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return gateCall{}, err
	}
	var g gateCall
	for _, p := range paths {
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			return gateCall{}, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Gate" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "knobreach" {
				return true
			}
			g.calls = true
			for _, a := range call.Args {
				cl, ok := a.(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, e := range cl.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "ProseDirs" {
							g.withProse = true
						}
					}
				}
			}
			return true
		})
	}
	return g, nil
}

type gateCensus struct {
	packages, withSettings, calling, withProse int
	missing, noProse                           []string
}

// censusOf — перепись по данным каталогам пакетов.
func censusOf(dirs []string) (gateCensus, error) {
	var c gateCensus
	for _, d := range dirs {
		c.packages++
		pc, err := ProbeSettings(d)
		if err != nil {
			return c, err
		}
		if len(pc.Settings) == 0 {
			continue
		}
		c.withSettings++
		g, err := findGateCall(d)
		if err != nil {
			return c, err
		}
		switch {
		case !g.calls:
			c.missing = append(c.missing, d)
		case !g.withProse:
			c.calling++
			c.noProse = append(c.noProse, d)
		default:
			c.calling++
			c.withProse++
		}
	}
	return c, nil
}

// cmdProbeDirs — каталоги пакетов процессов с пробами, из индекса git.
func cmdProbeDirs(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var dirs []string
	for _, pat := range cmdProbePatterns {
		files, err := treecorpus.Glob(filepath.Join(treeRoot, pat))
		if err != nil {
			t.Fatalf("состав дерева по образцу %s не прочитан: %v", pat, err)
		}
		for _, f := range files {
			d := filepath.Dir(f)
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	sort.Strings(dirs)
	return dirs
}

func TestEveryCmdPackageWhoseProbesSetKnobsCallsTheGate(t *testing.T) {
	dirs := cmdProbeDirs(t)
	c, err := censusOf(dirs)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("пакетов процессов с пробами %d · задают имена ручек %d · зовут гейт %d (из них с текстами %d)",
		c.packages, c.withSettings, c.calling, c.withProse)
	if c.withSettings == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ни один из %d пакетов процессов не задаёт имён ручек — предикат "+
			"поиска перестал узнавать дерево, а не дерево стало чистым", c.packages)
	}
	root, _ := filepath.Abs(treeRoot)
	rel := func(d string) string { r, _ := filepath.Rel(root, d); return r }
	for _, d := range c.missing {
		t.Errorf("%s: пробы пакета задают имена ручек, а гейт knobreach.Gate не зовётся — имя, "+
			"которого загрузчик не читает, здесь не краснеет ничем", rel(d))
	}
}

// Самопроверка переписи: пакет, задающий имена без вызова гейта, — находка;
// тот же пакет с вызовом — молчание; вызов без каталогов текстов — находка.
func TestGateCensusSelfCheck(t *testing.T) {
	setting := "package x\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { t.Setenv(\"KACHO_KNOBPROBE_GRPC_PORT\", \"0\") }\n"
	gateWith := func(fields string) string {
		return "package x\n\nimport (\n\t\"testing\"\n\n\t\"github.com/PRO-Robotech/kacho/internal/knobreach\"\n)\n\n" +
			"func TestGate(t *testing.T) { knobreach.Gate(t, knobreach.Package{" + fields + "}) }\n"
	}
	mk := func(files map[string]string) string {
		d := t.TempDir()
		for n, b := range files {
			if err := os.WriteFile(filepath.Join(d, n), []byte(b), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return d
	}
	missing := mk(map[string]string{"a_test.go": setting})
	noProse := mk(map[string]string{"a_test.go": setting, "g_test.go": gateWith(`Dir: "."`)})
	full := mk(map[string]string{"a_test.go": setting, "g_test.go": gateWith(`Dir: ".", ProseDirs: []string{"."}`)})
	commentOnly := mk(map[string]string{"a_test.go": setting + "\n// knobreach.Gate(t, knobreach.Package{ProseDirs: nil})\n"})
	quiet := mk(map[string]string{"a_test.go": "package x\n"})

	c, err := censusOf([]string{missing, noProse, full, commentOnly, quiet})
	if err != nil {
		t.Fatal(err)
	}
	if c.packages != 5 || c.withSettings != 4 || c.calling != 2 || c.withProse != 1 {
		t.Fatalf("перепись не та: %+v", c)
	}
	if strings.Join(c.missing, ",") != missing+","+commentOnly {
		t.Fatalf("пакеты без гейта названы не те (вызов в комментарии — не вызов): %q", c.missing)
	}
	if len(c.noProse) != 1 || c.noProse[0] != noProse {
		t.Fatalf("пакет без каталогов текстов не назван: %q", c.noProse)
	}
}
