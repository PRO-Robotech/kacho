// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestRefillLoopReportsItsScan — цикл добора страницы обязан снимать стоимость
// этого добора (#653).
//
// # Предмет
//
// Список с пообъектной проверкой прав набирает страницу ДОБОРОМ: строки читаются
// порциями, каждая порция судится, и обход повторяется, пока видимых не наберётся
// на страницу. Сколько строк рассмотрено ради одной отданной — величина, которой
// негде взяться ни из журнала, ни из счётчиков хранилища: обход внутренний.
//
// # Почему гейт, а не проба одного списка
//
// Класс — на семь списков (#645), и восьмой заведут завтра. Проба у одного из них
// закрепляет свойство ровно у него; свойство ДЕРЕВА держит только обход дерева.
//
// # Что именно требуется
//
// Функция, несущая цикл добора, обязана:
//
//	AddBatch — внутри цикла, на каждой порции. Съём последней порции дал бы
//	           константу при любом числе доборов;
//	Report   — ПОСЛЕ цикла, ровно один раз за запрос.
//
// Признак цикла — условие вида `len(<accum>) < <need>` И чтение страницы внутри
// (вызов `.List(`): это и есть форма добора, а не любой `for`. Без второй половины
// гейт краснел бы на всяком накопителе — то есть ловил бы форму, а не существо. Разбор идёт по AST, поэтому упоминание в комментарии (в том
// числе в этом файле) находкой не является.
func TestRefillLoopReportsItsScan(t *testing.T) {
	// treecorpus отдаёт АБСОЛЮТНЫЕ пути отслеживаемых файлов — состав берётся у
	// git, а не с диска, поэтому неотслеживаемый файл в корпус не попадает.
	files, err := treecorpus.UnderWithSuffix("../../services", ".go")
	if err != nil {
		t.Fatalf("перечислить исходники сервисов: %v", err)
	}

	var seenFiles, withLoop int
	var findings []string

	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		seenFiles++

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			continue // непарсящийся файл — предмет другого гейта
		}

		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			loop := findRefillLoop(fn.Body)
			if loop == nil {
				return true
			}
			// Сужение: добором является цикл, который ЧИТАЕТ СТРАНИЦУ. Накопитель
			// вида `for len(buf) < n` без обращения к хранилищу — законный близнец,
			// и требовать от него съёма стоимости не за что.
			if !callsSelector(loop.Body, "List") {
				return true
			}
			withLoop++

			if !callsSelector(loop.Body, "AddBatch") {
				findings = append(findings, short(path)+": "+fn.Name.Name+
					" — цикл добора не считает порции (нет AddBatch внутри цикла)")
			}
			if !callsSelectorAfter(fn.Body, loop, "Report") {
				findings = append(findings, short(path)+": "+fn.Name.Name+
					" — стоимость страницы не снимается после цикла (нет Report)")
			}
			return true
		})
	}

	t.Logf("осмотрено прод-файлов %d; из них с циклом добора %d", seenFiles, withLoop)

	if seenFiles == 0 {
		t.Fatal("корпус пуст — гейт ничего не прочитал; «ноль находок» здесь " +
			"означало бы «ноль прочитанного»")
	}
	if withLoop == 0 {
		t.Fatal("ни одного цикла добора не распознано: признак разошёлся с деревом, " +
			"и гейт стал тождественно-зелёным")
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// findRefillLoop возвращает первый цикл вида `for len(x) < y {` в теле функции.
func findRefillLoop(body *ast.BlockStmt) *ast.ForStmt {
	var found *ast.ForStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		fs, ok := n.(*ast.ForStmt)
		if !ok || fs.Cond == nil {
			return true
		}
		bin, ok := fs.Cond.(*ast.BinaryExpr)
		if !ok || bin.Op != token.LSS {
			return true
		}
		call, ok := bin.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "len" {
			found = fs
		}
		return true
	})
	return found
}

// callsSelector — есть ли в поддереве вызов метода с таким именем.
func callsSelector(n ast.Node, name string) bool {
	got := false
	ast.Inspect(n, func(x ast.Node) bool {
		if got {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
			got = true
		}
		return true
	})
	return got
}

// callsSelectorAfter — есть ли вызов метода name в теле функции ПОСЛЕ цикла.
// Позиция сравнивается по смещению: съём внутри цикла считался бы за съём после
// него, а это ровно тот дефект, который здесь запрещён.
func callsSelectorAfter(body *ast.BlockStmt, loop *ast.ForStmt, name string) bool {
	got := false
	ast.Inspect(body, func(x ast.Node) bool {
		if got {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return true
		}
		if call.Pos() > loop.End() {
			got = true
		}
		return true
	})
	return got
}

// short — путь от каталога сервисов, чтобы координата в отказе читалась.
func short(abs string) string {
	if i := strings.Index(abs, "/services/"); i >= 0 {
		return abs[i+1:]
	}
	return abs
}
