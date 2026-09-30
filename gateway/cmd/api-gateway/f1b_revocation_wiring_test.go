// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_revocation_wiring_test.go — провязка полос отзыва в композиционном
// корне: ни одна полоса не стоит в ветке, которая может её не завести.
//
// # Предмет
//
// Читатель отзыва НАШИХ токенов прежде стоял ВНУТРИ ветки «адрес прежнего
// провайдера задан», и это делало невыразимой посадку «принимаем ТОЛЬКО нашего
// издателя». Ветки прежнего провайдера больше нет вовсе (#2734) — её случай
// снят вместе с предметом, — но класс остался: полоса отзыва, провязанная
// условно, в ветке «не заведена» пропускает токен, ни о чём не спросив. Полоса
// ЗАПИСИ отзыва (токены записей, которые наша чеканка не пометила) обязана
// поэтому провязываться БЕЗУСЛОВНО: вызов стоит в main и исполняется на каждом
// его проходе.
//
// # Безусловность — отсутствие ЛЮБОЙ формы, а не только `if` (kacho#2890)
//
// Безусловность судится по пути от main до вызова (executionForms): ветка if,
// ветка case у switch и select, тело цикла, замыкание, оператор go и defer,
// правый операнд && и || — всякая форма, в соседней стороне которой вызов не
// исполняется, — находка с её типом узла и координатой. Находка и переход goto
// в main, и функция-помощник вместо main. Замыкание — находка всегда, где бы
// его ни позвали: вызовов замыкания судья не прослеживает. Каждую форму держит
// строка TestRecordLaneInjection_EveryFormThatMayNotRunIsNamed на настоящем
// корне.
//
// # Почему проверяется ИСХОДНИК, а не поведение
//
// `main()` из пробы не исполнить: он дозванивается до соседей и занимает
// слушатели. Чтение исходника СЛАБЕЕ исполнения, и здесь оно применяется ровно
// к тому свойству, которого «оно собирается» не показывает, — к ВЛОЖЕННОСТИ
// одного решения в другое.
//
// Разбор ведётся по дереву синтаксиса, а не по тексту: предмет здесь —
// вложенность узлов, и предикат по подстроке отвечал бы на другой вопрос.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// f1bFindCall возвращает позиции всех вызовов метода с названным именем.
func f1bFindCall(f *ast.File, name string) []token.Pos {
	var out []token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return true
		}
		out = append(out, call.Pos())
		return true
	})
	return out
}

// enclosingNodes — узлы разбора, охватывающие позицию, от файла вглубь.
func enclosingNodes(f *ast.File, pos token.Pos) []ast.Node {
	var path []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil || pos < n.Pos() || pos >= n.End() {
			return false
		}
		path = append(path, n)
		return true
	})
	return path
}

// within — лежит ли позиция в узле.
func within(n ast.Node, pos token.Pos) bool {
	return n != nil && n.Pos() <= pos && pos < n.End()
}

// executionForms — формы на пути к позиции, в соседней стороне которых она не
// исполняется: тело или else ветки if (условие и инициализация исполняются
// всегда), ветка case у switch и type switch, ветка select, цикл for (кроме
// инициализации), тело range, замыкание, правый операнд && и ||, вызов под go и
// defer (аргументы вычисляются сразу и формой не считаются). Каждая названа
// типом узла и координатой; функция, охватывающая позицию, формой не считается —
// её судит вызывающий.
func executionForms(fset *token.FileSet, path []ast.Node, pos token.Pos) []string {
	var forms []string
	for _, n := range path {
		conditional := false
		switch x := n.(type) {
		case *ast.IfStmt:
			conditional = within(x.Body, pos) || within(x.Else, pos)
		case *ast.CaseClause, *ast.CommClause, *ast.FuncLit:
			conditional = true
		case *ast.ForStmt:
			conditional = !within(x.Init, pos)
		case *ast.RangeStmt:
			conditional = within(x.Body, pos)
		case *ast.BinaryExpr:
			conditional = (x.Op == token.LAND || x.Op == token.LOR) && within(x.Y, pos)
		case *ast.GoStmt:
			conditional = !inArgs(x.Call, pos)
		case *ast.DeferStmt:
			conditional = !inArgs(x.Call, pos)
		}
		if conditional {
			forms = append(forms, fmt.Sprintf("%T у %s", n, fset.Position(n.Pos())))
		}
	}
	return forms
}

// inArgs — лежит ли позиция в аргументах вызова.
func inArgs(call *ast.CallExpr, pos token.Pos) bool {
	for _, a := range call.Args {
		if within(a, pos) {
			return true
		}
	}
	return false
}

// straightLineFinding — пусто, если позиция стоит в main и исполняется на
// каждом его проходе; иначе — что её отделяет. Переход goto где угодно в main
// — находка: путь, через который он прыгает, судья не строит.
func straightLineFinding(fset *token.FileSet, f *ast.File, pos token.Pos) string {
	at := fset.Position(pos)
	path := enclosingNodes(f, pos)
	var fn *ast.FuncDecl
	for _, n := range path {
		if fd, ok := n.(*ast.FuncDecl); ok {
			fn = fd
		}
	}
	switch {
	case fn == nil:
		return fmt.Sprintf("%s стоит вне объявления функции", at)
	case fn.Recv != nil || fn.Name.Name != "main":
		return fmt.Sprintf("%s стоит в функции %s, а не в main — её вызовы судья не прослеживает", at, fn.Name.Name)
	}
	if forms := executionForms(fset, path, pos); len(forms) > 0 {
		return fmt.Sprintf("%s исполняется не на каждом проходе main: охватывают %s", at, strings.Join(forms, " → "))
	}
	var jumps []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if br, ok := n.(*ast.BranchStmt); ok && br.Tok == token.GOTO {
			jumps = append(jumps, fmt.Sprintf("%T у %s", br, fset.Position(br.Pos())))
		}
		return true
	})
	if len(jumps) > 0 {
		return fmt.Sprintf("%s стоит в main с переходом goto (%s) — путь, через который он прыгает, судья не строит",
			at, strings.Join(jumps, ", "))
	}
	return ""
}

func parseCompositionRoot(t *testing.T) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("композиционный корень не разбирается: %v", err)
	}
	return fset, f
}

// recordLaneFinding — пусто, если полоса записи отзыва провязана ровно один раз
// и безусловно, а положительный контроль разбора форм найден условным; census
// называет осмотренное.
func recordLaneFinding(fset *token.FileSet, f *ast.File) (finding, census string) {
	calls := f1bFindCall(f, "WithRevocationCheck")
	if len(calls) != 1 {
		return fmt.Sprintf("полоса записи отзыва провязана %d раз, ожидался ровно 1 — ни одного значит "+
			"«токен проходит, ни о чём не спросив», два — два решения об одном предмете", len(calls)), ""
	}
	if why := straightLineFinding(fset, f, calls[0]); why != "" {
		return "полоса записи отзыва провязана условно: " + why + " — в соседней стороне формы край " +
			"пропускал бы токен, ни о чём не спросив", ""
	}
	// Положительный контроль разбора форм: вызов, заведомо стоящий в ветке
	// (читатель отзыва НАШИХ токенов — под «наш издатель принимается»), обязан
	// быть найден условным. Иначе «безусловно» выше верно про что угодно.
	platform := f1bFindCall(f, "WithPlatformRevocationCheck")
	if len(platform) == 0 {
		return "читатель отзыва НАШИХ токенов не провязывается вовсе — объявленный контроль " +
			"без читателя не отказал бы ни разу за свою жизнь", ""
	}
	control := straightLineFinding(fset, f, platform[0])
	if control == "" {
		return fmt.Sprintf("положительный контроль не сработал: условный вызов %s найден безусловным — "+
			"разбор форм слеп, и утверждение о безусловности ничего не значит",
			fset.Position(platform[0])), ""
	}
	return "", fmt.Sprintf("перепись: полоса записи — вызовов 1, форм на пути от main 0 (%s); полоса нашей "+
		"чеканки — вызовов %d, условна (%s)", fset.Position(calls[0]), len(platform), control)
}

// TestRecordLaneOfRevocationIsWiredUnconditionally — полоса записи отзыва
// провязана ровно один раз и безусловно.
func TestRecordLaneOfRevocationIsWiredUnconditionally(t *testing.T) {
	fset, f := parseCompositionRoot(t)
	finding, census := recordLaneFinding(fset, f)
	if finding != "" {
		t.Fatal(finding)
	}
	t.Log(census)
}
