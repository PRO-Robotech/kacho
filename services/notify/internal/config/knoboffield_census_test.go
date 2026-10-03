// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// GS-I4 — KnobOfField паникует на имени поля, которого в [config.Config] нет.
// Паника вне cmd/ недостижима, пока имя каждого вызова известно разбору и
// называет существующее поле. Перепись судит прод-файлы каталога службы
// (состав — у индекса git) и знает три законные формы аргумента:
//
//  1. строковый литерал;
//  2. переменная цикла `for _, f := range []string{…}` той же функции —
//     судится каждый литерал перечня;
//  3. параметр объемлющей функции — судится аргумент на его месте у КАЖДОГО
//     вызова этой функции в переписи (литералом); вызовов ноль — находка.
//
// Любая иная форма — находка: значение её разбор не видит, и недостижимость
// паники перестала бы быть доказанной.

type knobCall struct {
	pos   string
	fn    *ast.FuncDecl // объемлющая функция
	arg   ast.Expr
	inLit []string // форма 2: литералы перечня цикла
}

// knobOfFieldCensus — вызовы KnobOfField в исходниках srcs (имя → текст):
// число вызовов и находки.
func knobOfFieldCensus(t *testing.T, srcs map[string]string) (int, []string) {
	t.Helper()
	fset := token.NewFileSet()
	cfg := reflect.TypeFor[config.Config]()
	var calls []knobCall
	// callsites[имя функции] — аргументы всех вызовов функции с этим именем.
	callsites := map[string][][]ast.Expr{}
	for name, src := range srcs {
		f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не разобран: %v", name, err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			var ranges []*ast.RangeStmt
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.RangeStmt:
					ranges = append(ranges, x)
				case *ast.CallExpr:
					var fn string
					switch f := x.Fun.(type) {
					case *ast.Ident:
						fn = f.Name
					case *ast.SelectorExpr:
						fn = f.Sel.Name
					}
					callsites[fn] = append(callsites[fn], x.Args)
					if fn == "KnobOfField" && len(x.Args) == 1 {
						kc := knobCall{pos: fset.Position(x.Pos()).String(), fn: fd, arg: x.Args[0]}
						if id, ok := x.Args[0].(*ast.Ident); ok {
							for _, r := range ranges {
								v, ok := r.Value.(*ast.Ident)
								if !ok || v.Name != id.Name || x.Pos() < r.Body.Pos() || x.Pos() > r.Body.End() {
									continue
								}
								if cl, ok := r.X.(*ast.CompositeLit); ok {
									kc.inLit = []string{}
									for _, e := range cl.Elts {
										s, ok := stringLit(e)
										if !ok {
											kc.inLit = nil
											break
										}
										kc.inLit = append(kc.inLit, s)
									}
								}
							}
						}
						calls = append(calls, kc)
					}
				}
				return true
			})
		}
	}
	var out []string
	check := func(pos, field string) {
		if _, ok := cfg.FieldByName(field); !ok {
			out = append(out, pos+": поля "+field+" в config.Config нет — KnobOfField запаникует")
		}
	}
	for _, c := range calls {
		if s, ok := stringLit(c.arg); ok {
			check(c.pos, s)
			continue
		}
		if c.inLit != nil {
			for _, s := range c.inLit {
				check(c.pos, s)
			}
			continue
		}
		id, ok := c.arg.(*ast.Ident)
		idx := paramIndex(c.fn, idxName(id, ok))
		if idx < 0 {
			out = append(out, c.pos+": аргумент KnobOfField — не литерал, не перечень цикла и не параметр: имя поля переписи не видно")
			continue
		}
		sites := callsites[c.fn.Name.Name]
		if len(sites) == 0 {
			out = append(out, fmt.Sprintf("%s: параметр %s функции %s — вызовов функции в переписи 0", c.pos, id.Name, c.fn.Name.Name))
			continue
		}
		for _, args := range sites {
			if idx >= len(args) {
				out = append(out, c.pos+": вызов "+c.fn.Name.Name+" без аргумента на месте параметра")
				continue
			}
			s, ok := stringLit(args[idx])
			if !ok {
				out = append(out, c.pos+": вызов "+c.fn.Name.Name+" передаёт имя поля не литералом")
				continue
			}
			check(c.pos+" (через "+c.fn.Name.Name+")", s)
		}
	}
	return len(calls), out
}

func idxName(id *ast.Ident, ok bool) string {
	if !ok {
		return ""
	}
	return id.Name
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// paramIndex — место параметра name в списке параметров fd (без приёмника);
// -1 — такого параметра нет.
func paramIndex(fd *ast.FuncDecl, name string) int {
	if name == "" || fd.Type.Params == nil {
		return -1
	}
	i := 0
	for _, f := range fd.Type.Params.List {
		if len(f.Names) == 0 {
			i++
			continue
		}
		for _, n := range f.Names {
			if n.Name == name {
				return i
			}
			i++
		}
	}
	return -1
}

func TestKnobOfFieldIsCalledOnlyWithExistingFields(t *testing.T) {
	files, err := treecorpus.UnderWithSuffix("../..", ".go")
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: состав services/notify не взят у индекса: %v", err)
	}
	srcs := map[string]string{}
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не прочитан: %v", p, err)
		}
		srcs[p] = string(b)
	}
	calls, findings := knobOfFieldCensus(t, srcs)
	t.Logf("перепись KnobOfField: прод-файлов .go осмотрено %d, вызовов %d, находок %d", len(srcs), calls, len(findings))
	if calls == 0 {
		t.Fatalf("вызовов KnobOfField 0 при %d осмотренных файлах — обход не видит предмета", len(srcs))
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// Инъекция по каждой форме: имя несуществующего поля — находка, существующего
// — молчание; форма, которой разбор не знает, — находка.
func TestKnobOfFieldCensusCanFail(t *testing.T) {
	cases := []struct {
		name, body string
		want       int
	}{
		{"литерал, поле есть", `func f() { _ = config.KnobOfField("AuthMode") }`, 0},
		{"литерал, поля нет", `func f() { _ = config.KnobOfField("NoSuchField") }`, 1},
		{"перечень цикла, поля есть", `func f() { for _, x := range []string{"DBHost", "DBName"} { _ = config.KnobOfField(x) } }`, 0},
		{"перечень цикла, одного поля нет", `func f() { for _, x := range []string{"DBHost", "NoSuch"} { _ = config.KnobOfField(x) } }`, 1},
		{"параметр, вызовы литералом", `func g(x string) { _ = config.KnobOfField(x) }; func h() { g("DBPort") }`, 0},
		{"параметр, вызов с чужим полем", `func g(x string) { _ = config.KnobOfField(x) }; func h() { g("DBPort"); g("NoSuch") }`, 1},
		{"параметр без вызовов", `func g(x string) { _ = config.KnobOfField(x) }`, 1},
		{"переменная иной формы", `func f() { x := "DBHost"; _ = config.KnobOfField(x) }`, 1},
	}
	for _, c := range cases {
		n, f := knobOfFieldCensus(t, map[string]string{"inj.go": "package p\n" + c.body + "\n"})
		if n != 1 || len(f) != c.want {
			t.Errorf("%s: вызовов %d, находок %d (%v); ожидалось вызовов 1, находок %d", c.name, n, len(f), f, c.want)
		}
	}
}
