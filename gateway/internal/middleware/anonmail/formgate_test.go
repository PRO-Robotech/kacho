// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// formReport — что гейт формы пакета прочёл и нашёл.
type formReport struct {
	files          int
	advisoryCalls  int
	begins         int
	poolOperators  int
	limitLiterals  int
	findings       []string
	firstAfterOpen string
}

var (
	reAdvisory = regexp.MustCompile(`pg_advisory\w*\(`)
	// Значения пределов звена — anonMailDecisionBudget (1,5 с) и
	// anonMailStoreWait (250 мс) — в любой записи литерала.
	forbiddenLimitLiterals = map[string]bool{"1500": true, "250": true, "1.5": true}
	reLimitInString        = regexp.MustCompile(`\b(1500|250)\b|\b1\.5s\b|\b250ms\b`)
	// Константы, внутри объявления которых значения законны.
	limitConstNames = map[string]bool{"anonMailStoreWait": true, "anonMailDecisionBudget": true}
	poolOperatorSel = map[string]bool{"Exec": true, "Query": true, "QueryRow": true, "SendBatch": true, "CopyFrom": true}
)

// auditLimiterForm — гейт формы пакета anonmail (И26, УК53, «одна дверь»):
//
//   - вызовы pg_advisory* — только формы с ДВУМЯ аргументами (int4, int4);
//   - вызовов Begin/BeginTx — ровно один, в beginDecision, и первое обращение
//     транзакции в нём — строка пределов (`limitsSQL`);
//   - операторов на пуле ограничителя вне транзакции — ровно два, в методах
//     pgResolveProbe (опрос pg_xact_status и чтение строки момента);
//   - литералов значений пределов вне объявления их констант — ноль.
//
// Ядро отделено от каталога пакета: инъекции гонят его на синтетической копии.
func auditLimiterForm(dir string) (formReport, error) {
	var rep formReport
	fset := token.NewFileSet()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return rep, err
	}
	sort.Strings(paths)
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			return rep, err
		}
		rep.files++
		auditFile(fset, f, &rep)
	}
	return rep, nil
}

func auditFile(fset *token.FileSet, f *ast.File, rep *formReport) {
	pos := func(n ast.Node) string {
		p := fset.Position(n.Pos())
		return filepath.Base(p.Filename) + ":" + strconv.Itoa(p.Line)
	}
	// Позиции внутри объявлений констант пределов — законное место значений.
	inLimitConst := map[ast.Node]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for _, name := range vs.Names {
			if limitConstNames[name.Name] {
				for _, v := range vs.Values {
					ast.Inspect(v, func(m ast.Node) bool { inLimitConst[m] = true; return true })
				}
			}
		}
		return true
	})
	for _, decl := range f.Decls {
		fn, _ := decl.(*ast.FuncDecl)
		recv := ""
		if fn != nil && fn.Recv != nil && len(fn.Recv.List) == 1 {
			switch rt := fn.Recv.List[0].Type.(type) {
			case *ast.Ident:
				recv = rt.Name
			case *ast.StarExpr:
				if id, ok := rt.X.(*ast.Ident); ok {
					recv = id.Name
				}
			}
		}
		fnName := ""
		if fn != nil {
			fnName = fn.Name.Name
		}
		sawBegin := false
		ast.Inspect(decl, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BasicLit:
				switch x.Kind {
				case token.STRING:
					for _, m := range reAdvisory.FindAllStringIndex(x.Value, -1) {
						rep.advisoryCalls++
						if args := advisoryArgs(x.Value[m[1]:]); args != 2 {
							rep.findings = append(rep.findings, fmt.Sprintf("%s: вызов pg_advisory* с %d аргументами — в звене разрешена только форма с двумя int4", pos(x), args))
						}
					}
					if reLimitInString.MatchString(x.Value) && !inLimitConst[x] {
						rep.limitLiterals++
						rep.findings = append(rep.findings, fmt.Sprintf("%s: литерал значения предела %s вне объявления константы", pos(x), x.Value))
					}
				case token.INT, token.FLOAT:
					if forbiddenLimitLiterals[x.Value] && !inLimitConst[x] {
						rep.limitLiterals++
						rep.findings = append(rep.findings, fmt.Sprintf("%s: литерал значения предела %s вне объявления константы", pos(x), x.Value))
					}
				}
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "Begin", "BeginTx":
					rep.begins++
					sawBegin = true
					if fnName != "beginDecision" {
						rep.findings = append(rep.findings, fmt.Sprintf("%s: транзакция открыта в %s — дверь в транзакцию одна, beginDecision", pos(x), fnName))
					}
					return true
				}
				if sawBegin && rep.firstAfterOpen == "" && fnName == "beginDecision" &&
					(sel.Sel.Name == "Exec" || sel.Sel.Name == "Query" || sel.Sel.Name == "QueryRow") && len(x.Args) >= 2 {
					if s, ok := x.Args[1].(*ast.SelectorExpr); ok {
						rep.firstAfterOpen = s.Sel.Name
					} else {
						rep.firstAfterOpen = "?"
					}
				}
				if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "pool" && poolOperatorSel[sel.Sel.Name] {
					rep.poolOperators++
					if recv != "pgResolveProbe" {
						rep.findings = append(rep.findings, fmt.Sprintf("%s: оператор %s на пуле ограничителя вне транзакции в %s — их ровно два, в разрешении исхода фиксации", pos(x), sel.Sel.Name, fnName))
					}
				}
			}
			return true
		})
	}
}

// advisoryArgs — число аргументов вызова по тексту после открывающей скобки.
func advisoryArgs(rest string) int {
	depth, args, any := 0, 1, false
	for _, r := range rest {
		switch r {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				if !any {
					return 0
				}
				return args
			}
			depth--
		case ',':
			if depth == 0 {
				args++
			}
		default:
			if r != ' ' {
				any = true
			}
		}
	}
	return -1
}

func judgeForm(rep formReport) []string {
	out := append([]string(nil), rep.findings...)
	if rep.files == 0 {
		out = append(out, "обход пуст: не прочитано ни одного файла пакета")
	}
	if rep.advisoryCalls == 0 {
		out = append(out, "ни одного вызова pg_advisory* — гейт не опознал форму блокировки")
	}
	if rep.begins != 1 {
		out = append(out, fmt.Sprintf("вызовов Begin/BeginTx %d, ожидался ровно 1 (beginDecision)", rep.begins))
	}
	if rep.firstAfterOpen != "limitsSQL" {
		out = append(out, fmt.Sprintf("первое обращение транзакции в beginDecision — %q, ожидалась строка пределов limitsSQL", rep.firstAfterOpen))
	}
	if rep.poolOperators != 2 {
		out = append(out, fmt.Sprintf("операторов вне транзакции на пуле ограничителя %d, ожидалось 2 (resolveCommit)", rep.poolOperators))
	}
	return out
}

// TestLimiterForm_LockFormOneDoorAndLimitLiterals — гейт формы пакета на
// настоящем дереве: печатает объём осмотренного и числа, находок ноль.
func TestLimiterForm_LockFormOneDoorAndLimitLiterals(t *testing.T) {
	rep, err := auditLimiterForm(".")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("осмотрено файлов %d · вызовов pg_advisory* %d · Begin %d (первое обращение — %s) · операторов вне транзакции на пуле %d · литералов пределов вне констант %d",
		rep.files, rep.advisoryCalls, rep.begins, rep.firstAfterOpen, rep.poolOperators, rep.limitLiterals)
	for _, f := range judgeForm(rep) {
		t.Error(f)
	}
}

// TestLimiterForm_InjectionsAreFound — инъекции настоящим входом: копия пакета
// с одной правкой каждой — красный с координатой; нетронутая копия — зелёная.
func TestLimiterForm_InjectionsAreFound(t *testing.T) {
	src, err := os.ReadFile("store_pg.go")
	if err != nil {
		t.Fatal(err)
	}
	others, _ := filepath.Glob("*.go")
	copyPkg := func(t *testing.T, patched string) string {
		dir := t.TempDir()
		for _, p := range others {
			if strings.HasSuffix(p, "_test.go") || p == "store_pg.go" {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, p), b, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "store_pg.go"), []byte(patched), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	clean, _ := auditLimiterForm(copyPkg(t, string(src)))
	if f := judgeForm(clean); len(f) != 0 {
		t.Fatalf("нетронутая копия красна: %v", f)
	}
	cases := []struct {
		name, from, to, want string
	}{
		{"форма с одним ключом", "pg_advisory_xact_lock($1::int4, $2::int4)", "pg_advisory_xact_lock($1::bigint)", "с 1 аргументами"},
		{"литерал 1500", "const limiterWarmUpBudget = 30 * time.Second", "const limiterWarmUpBudget = 30 * time.Second\n\nvar injected = 1500", "литерал значения предела 1500"},
		{"второй Begin", "func (b *pgBackend) close() error {", "func (b *pgBackend) close() error {\n\t_, _ = b.pool.Begin(context.Background())", "транзакция открыта в close"},
		{"третий оператор вне транзакции", "func (b *pgBackend) close() error {", "func (b *pgBackend) close() error {\n\t_, _ = b.pool.Exec(context.Background(), xactStatusSQL)", "оператор Exec на пуле ограничителя вне транзакции в close"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(string(src), c.from) {
				t.Fatalf("точка инъекции %q не найдена в store_pg.go — проба устарела", c.from)
			}
			rep, err := auditLimiterForm(copyPkg(t, strings.Replace(string(src), c.from, c.to, 1)))
			if err != nil {
				t.Fatal(err)
			}
			f := judgeForm(rep)
			joined := strings.Join(f, "\n")
			if !strings.Contains(joined, c.want) || !strings.Contains(joined, "store_pg.go:") {
				t.Fatalf("инъекция не найдена с координатой: %v", f)
			}
			t.Logf("инъекция найдена: %s", f[0])
		})
	}
}
