// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package smtp_test

// classify_nodefault_test.go — УК17 (CX1-17): у классификатора нет ветки `default`.
// Поведенческие пробы (classify_test.go) видят, что каждый названный ответ лёг в
// свою клетку; они не видят, что НОВЫЙ ответ, которого нет в таблице, молча
// уйдёт в «прочее». Это держит разбор: в Classify и в функциях пакета, которые она
// зовёт (замыкание по вызовам), нет `default` ни в одном `switch`.
//
// Судит узел синтаксиса (ast.CaseClause с пустым списком), а не текст: слово
// `default` в комментарии или строке находкой не является. Объём осмотренного
// печатается; пустой обход (Classify не найдена) — красный, а не «ноль находок».
// Способность упасть доказана на синтетике в t.TempDir(): дефект — находка с
// координатой, законный близнец той же формы — молчание.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// classifyDefaults разбирает не-тестовые файлы пакета в dir и возвращает число
// осмотренных функций замыкания Classify и координаты веток `default`.
func classifyDefaults(t *testing.T, dir string) (inspected int, findings []string) {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог пакета %s не читается: %v", dir, err)
	}
	funcs := map[string]*ast.FuncDecl{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("файл %s не разбирается: %v", name, err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Body != nil {
				funcs[fd.Name.Name] = fd
			}
		}
	}
	root, ok := funcs["Classify"]
	if !ok {
		return 0, nil
	}
	seen := map[string]bool{}
	queue := []*ast.FuncDecl{root}
	for len(queue) > 0 {
		fd := queue[0]
		queue = queue[1:]
		if seen[fd.Name.Name] {
			continue
		}
		seen[fd.Name.Name] = true
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok {
					if callee, ok := funcs[id.Name]; ok && !seen[id.Name] {
						queue = append(queue, callee)
					}
				}
			case *ast.SwitchStmt:
				findings = append(findings, defaultsIn(fset, fd.Name.Name, x.Body)...)
			case *ast.TypeSwitchStmt:
				findings = append(findings, defaultsIn(fset, fd.Name.Name, x.Body)...)
			}
			return true
		})
	}
	sort.Strings(findings)
	return len(seen), findings
}

func defaultsIn(fset *token.FileSet, fn string, body *ast.BlockStmt) []string {
	var out []string
	for _, s := range body.List {
		if cc, ok := s.(*ast.CaseClause); ok && cc.List == nil {
			p := fset.Position(cc.Pos())
			out = append(out, filepath.Base(p.Filename)+":"+strconv.Itoa(p.Line)+" "+fn)
		}
	}
	return out
}

func TestClassify_UK17_NoDefaultBranch(t *testing.T) {
	inspected, findings := classifyDefaults(t, ".")
	t.Logf("функций замыкания Classify осмотрено: %d; веток default: %d", inspected, len(findings))
	if inspected == 0 {
		t.Fatal("Classify в пакете smtp не найдена: осмотрено 0 функций — вердикта нет, это не «ноль находок»")
	}
	for _, f := range findings {
		t.Errorf("ветка default в классификаторе (УК17): %s", f)
	}
}

// Самопроверка распознавателя на синтетике: дефект в вызываемой функции —
// находка; законный близнец (тот же switch без default, default в функции вне
// замыкания, слово default в комментарии и строке) — молчание; пакет без Classify —
// ноль осмотренных.
func TestClassify_UK17_RecognizerProvenOnSynthetic(t *testing.T) {
	write := func(t *testing.T, src string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "c.go"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	const head = "package smtp\n\nfunc Classify(code int) string { return row(code) }\n\n"
	defect := write(t, head+"func row(code int) string {\n\tswitch code {\n\tcase 250:\n\t\treturn \"sent\"\n\tdefault:\n\t\treturn \"other\"\n\t}\n}\n")
	twin := write(t, head+"// default: в комментарии\nfunc row(code int) string {\n\tswitch code {\n\tcase 250:\n\t\treturn \"sent\"\n\t}\n\treturn \"default\"\n}\n\n"+
		"func unrelated(x int) int {\n\tswitch x {\n\tdefault:\n\t\treturn 0\n\t}\n}\n")
	empty := write(t, "package smtp\n\nfunc other() {}\n")

	if n, f := classifyDefaults(t, defect); n != 2 || len(f) != 1 || !strings.Contains(f[0], "c.go:9 row") {
		t.Fatalf("дефект: осмотрено %d, находки %v; ожидалось 2 и одна находка c.go:9 row", n, f)
	}
	if n, f := classifyDefaults(t, twin); n != 2 || len(f) != 0 {
		t.Fatalf("близнец: осмотрено %d, находки %v; ожидалось 2 и молчание", n, f)
	}
	if n, f := classifyDefaults(t, empty); n != 0 || len(f) != 0 {
		t.Fatalf("пакет без Classify: осмотрено %d, находки %v; ожидалось 0", n, f)
	}
}
