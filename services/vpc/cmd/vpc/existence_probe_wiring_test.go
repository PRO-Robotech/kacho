// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Probe существования читает МАСТЕР, и это утверждение обязано проверяться, а не
// стоять комментарием.
//
// Probe отвечает «объекта нет», и этот ответ превращает отказ в правах в 404.
// С реплики он на ограниченное, но никак не измеряемое отсюда время отвечал бы
// «нет» про уже созданный объект. Переключение пула — одно слово в проводке, и
// оно тихое: тестов, которые бы упали, до сих пор не было, а godoc конструктора
// как раз предписывал реплику. Отсюда проверка именно проводки.
//
// Разбор — по AST, а не по тексту: слово `pool` встречается в этом файле
// многократно, в том числе в комментариях и в соседних вызовах, поэтому поиск
// подстрокой прошёл бы и на `slavePool`.
func TestExistenceProbeIsWiredToTheMasterPool(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse composition root: %v", err)
	}

	var (
		calls   int
		wrongAt []string
	)
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "NewExistenceProbe" {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "kachopg" {
			return true
		}
		calls++
		if len(call.Args) != 1 {
			wrongAt = append(wrongAt, fset.Position(call.Pos()).String()+": expected exactly one pool argument")
			return true
		}
		arg, ok := call.Args[0].(*ast.Ident)
		if !ok || arg.Name != "pool" {
			wrongAt = append(wrongAt, fset.Position(call.Pos()).String()+": probe must be built on `pool` (master)")
		}
		return true
	})

	// «Ноль находок» обязано отличаться от «ничего не осмотрено»: без этого
	// утверждения переименование конструктора превратило бы проверку в тихий no-op.
	if calls == 0 {
		t.Fatal("no kachopg.NewExistenceProbe call found in the composition root — the check inspected nothing")
	}
	if len(wrongAt) > 0 {
		t.Fatalf("existence probe must read the master pool (a replica would answer \"absent\" about a just-created object): %v", wrongAt)
	}
}
