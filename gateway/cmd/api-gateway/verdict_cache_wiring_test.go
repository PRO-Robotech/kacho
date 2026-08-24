// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verdict_cache_wiring_test.go — заполнение кэша вердиктов базовой полосы
// доходит до диагностической поверхности (#1221).
//
// # Почему разбор исходника, а не вызов
//
// Внешний слушатель собирается в `main()`, вызвать которую случай не может.
// Провязка же наблюдаемости nil-безопасна ПО ПОСТРОЕНИЮ — так и задумано, сбор
// величин не должен ронять подъём, — поэтому её пропажу не поймает ни
// компилятор, ни проба самого накопителя: она останется зелёной, а серия просто
// исчезнет с поверхности. Место провязки есть свойство исходника, и закрепляется
// оно разбором исходника.
//
// # Почему ДВА утверждения, а не одно
//
// «Читатель зарегистрирован» и «читатель читает полосу» — разные факты.
// Регистрация, отдающая постоянные числа, объявляет серию, ничего о процессе не
// сообщая: заполнение стояло бы нулём при переполненном кэше, и это выглядело бы
// исправнее исправного.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// parseMainFile — синтаксическое дерево композиционного корня.
func parseMainFile(t *testing.T) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("разбор main.go: %v", err)
	}
	return fset, f
}

// TestRootWiresTheVerdictCacheFillToTheDiagnosticSurface — корень регистрирует
// читателя заполнения, и этот читатель спрашивает ПОЛОСУ.
func TestRootWiresTheVerdictCacheFillToTheDiagnosticSurface(t *testing.T) {
	_, f := parseMainFile(t)

	registered, readsLane := false, false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "RegisterBasicCredentialCache" {
			return true
		}
		registered = true
		// Читатель обязан спрашивать ПОЛОСУ, а не отдавать постоянные числа.
		//
		// Годятся обе формы — значение метода (`basicLane.CacheStats`) и вызов
		// внутри замыкания (`basicLane.CacheStats()`): предмет утверждения в
		// том, ОТКУДА берутся числа, а не в том, сколько скобок по дороге.
		// Требовать одну форму значило бы требовать ритуала.
		ast.Inspect(call, func(inner ast.Node) bool {
			s, ok := inner.(*ast.SelectorExpr)
			if !ok || s.Sel.Name != "CacheStats" {
				return true
			}
			if recv, ok := s.X.(*ast.Ident); ok && strings.Contains(recv.Name, "asicLane") {
				readsLane = true
			}
			return true
		})
		return true
	})

	if !registered {
		t.Fatal("корень не регистрирует читателя заполнения кэша вердиктов: " +
			"величина считается в никуда, и её ноль ничего не утверждает")
	}
	if !readsLane {
		t.Error("читатель заполнения не спрашивает полосу — серия объявлена, но " +
			"о процессе не сообщает ничего")
	}
}
