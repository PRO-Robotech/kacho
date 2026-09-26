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
// поэтому провязываться БЕЗУСЛОВНО: ни одна ветка `if` её вызов не охватывает.
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
	"go/ast"
	"go/parser"
	"go/token"
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

// enclosingIfBodies — ветки `if`, чьё тело охватывает позицию.
func enclosingIfBodies(f *ast.File, pos token.Pos) []*ast.IfStmt {
	var out []*ast.IfStmt
	ast.Inspect(f, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if pos > ifs.Body.Lbrace && pos < ifs.Body.Rbrace {
			out = append(out, ifs)
		}
		if ifs.Else != nil && pos > ifs.Else.Pos() && pos < ifs.Else.End() {
			out = append(out, ifs)
		}
		return true
	})
	return out
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

// TestRecordLaneOfRevocationIsWiredUnconditionally — полоса записи отзыва
// провязана ровно один раз и вне всякой ветки.
func TestRecordLaneOfRevocationIsWiredUnconditionally(t *testing.T) {
	fset, f := parseCompositionRoot(t)
	calls := f1bFindCall(f, "WithRevocationCheck")
	if len(calls) != 1 {
		t.Fatalf("полоса записи отзыва провязана %d раз, ожидался ровно 1 — ни одного значит "+
			"«токен проходит, ни о чём не спросив», два — два решения об одном предмете", len(calls))
	}
	if outer := enclosingIfBodies(f, calls[0]); len(outer) != 0 {
		t.Fatalf("полоса записи отзыва провязана внутри ветки (%s, ветка у %s): в ветке «не "+
			"заведена» край пропускал бы токен, ни о чём не спросив",
			fset.Position(calls[0]), fset.Position(outer[0].Pos()))
	}
	// Положительный контроль разбора веток: вызов, заведомо стоящий в ветке
	// (читатель отзыва НАШИХ токенов — под «наш издатель принимается»), обязан
	// находиться внутри неё. Иначе «вне ветки» выше верно про что угодно.
	platform := f1bFindCall(f, "WithPlatformRevocationCheck")
	if len(platform) == 0 {
		t.Fatal("читатель отзыва НАШИХ токенов не провязывается вовсе — объявленный контроль " +
			"без читателя не отказал бы ни разу за свою жизнь")
	}
	if len(enclosingIfBodies(f, platform[0])) == 0 {
		t.Fatalf("положительный контроль не сработал: условный вызов %s не найден внутри ветки — "+
			"разбор веток слеп, и утверждение о безусловности ничего не значит",
			fset.Position(platform[0]))
	}
	t.Logf("перепись: полоса записи — вызовов 1, охватывающих веток 0 (%s); полоса нашей "+
		"чеканки — вызовов %d, в ветке", fset.Position(calls[0]), len(platform))
}
