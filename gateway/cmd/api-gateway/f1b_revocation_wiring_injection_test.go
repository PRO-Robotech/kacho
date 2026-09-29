// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_revocation_wiring_injection_test.go — инъекции в обе стороны для судьи
// безусловной провязки полосы записи отзыва (kacho#2890).
//
// Вход каждой инъекции — НАСТОЯЩИЙ корень, изменённый по позициям его разбора
// ровно в одном факте: оператор провязки поставлен под одну форму, которая
// отделяет его от прямолинейного исполнения main. Законный близнец — корень как
// он есть. Каждая форма обязана дать находку, называющую её тип и координату
// провязки: полоса, в соседней стороне формы не заведённая, безусловной не
// считается.
package main

import (
	"go/ast"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordLaneStmt — оператор верхнего уровня main, несущий провязку полосы
// записи, и сам вызов; судья на корне как он есть обязан молчать.
func recordLaneStmt(t *testing.T) ([]byte, *token.FileSet, ast.Stmt, *ast.CallExpr) {
	t.Helper()
	src, fset, f := rootSource(t)
	finding, census := recordLaneFinding(fset, f)
	require.Empty(t, finding, "законный близнец — корень как он есть — обязан молчать")
	t.Log("близнец: " + census)
	fn := mainFunc(f)
	require.NotNil(t, fn, "предпосылка инъекции: в корне есть main")
	var call *ast.CallExpr
	ast.Inspect(fn, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "WithRevocationCheck" {
				call = c
			}
		}
		return true
	})
	require.NotNil(t, call, "предпосылка инъекции: провязка полосы записи стоит в main")
	for _, st := range fn.Body.List {
		if st.Pos() <= call.Pos() && call.End() <= st.End() {
			return src, fset, st, call
		}
	}
	t.Fatal("предпосылка инъекции: провязка не лежит в операторе верхнего уровня main")
	return nil, nil, nil, nil
}

func TestRecordLaneInjection_EveryFormThatMayNotRunIsNamed(t *testing.T) {
	src, fset, st, call := recordLaneStmt(t)
	from, to := offset(fset, st.Pos()), offset(fset, st.End())
	callFrom, callTo := offset(fset, call.Pos()), offset(fset, call.End())
	stmtText := string(src[from:to])
	wrap := func(before, after string) []rootEdit {
		return []rootEdit{{from, from, before}, {to, to, after}}
	}

	injected, named := 0, 0
	defer func() {
		t.Logf("перепись: форм внесено %d · названо находкой %d", injected, named)
	}()
	for _, tc := range []struct {
		name  string
		edits []rootEdit
		form  string
	}{
		{"ветка then", wrap("if cfg.AppEnv != \"dev\" {\n\t", "\n\t}"), "*ast.IfStmt"},
		{"ветка else", wrap("if cfg.AppEnv == \"dev\" {\n\t} else {\n\t", "\n\t}"), "*ast.IfStmt"},
		{"замыкание, позванное под классом окружения",
			wrap("wireRecordLane := func() {\n\t", "\n\t}\n\tif cfg.AppEnv != \"dev\" {\n\t\twireRecordLane()\n\t}"),
			"*ast.FuncLit"},
		{"ветка switch", wrap("switch cfg.AppEnv {\n\tcase \"dev\":\n\tdefault:\n\t", "\n\t}"), "*ast.CaseClause"},
		{"ветка type switch", wrap("switch any(cfg).(type) {\n\tcase int:\n\tdefault:\n\t", "\n\t}"), "*ast.CaseClause"},
		{"ветка select", wrap("select {\n\tcase <-ctx.Done():\n\tdefault:\n\t", "\n\t}"), "*ast.CommClause"},
		{"тело for", wrap("for i := 0; i < len(cfg.TokenIssuers); i++ {\n\t", "\n\t}"), "*ast.ForStmt"},
		{"тело range", wrap("for range cfg.TokenIssuers {\n\t", "\n\t}"), "*ast.RangeStmt"},
		{"оператор go", []rootEdit{{from, callFrom, "go "}}, "*ast.GoStmt"},
		{"оператор defer", []rootEdit{{from, callFrom, "defer "}}, "*ast.DeferStmt"},
		{"правый операнд ||",
			[]rootEdit{{from, callFrom, "_ = cfg.AppEnv == \"dev\" || "}, {callTo, callTo, " != nil"}},
			"*ast.BinaryExpr"},
		{"переход goto через провязку", wrap("goto recordLaneWired\n\t", "\nrecordLaneWired:"), "*ast.BranchStmt"},
		{"функция-помощник вместо main",
			[]rootEdit{{from, to, "wireRecordLane()"}, {len(src), len(src), "\nfunc wireRecordLane() {\n\t" + stmtText + "\n}\n"}},
			"wireRecordLane"},
	} {
		injected++
		t.Run(tc.name, func(t *testing.T) {
			mfset, mf := mutateRoot(t, src, tc.edits...)
			calls := f1bFindCall(mf, "WithRevocationCheck")
			require.Len(t, calls, 1, "инъекция не того предмета: провязка полосы записи обязана остаться одна")
			finding, _ := recordLaneFinding(mfset, mf)
			requireNamedFinding(t, finding, tc.form, mfset.Position(calls[0]).String())
			named++
		})
	}
}
