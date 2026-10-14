// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"testing"
)

// recordedBearerStates — пары (источник, исход), которые код полосы записывает
// вызовом `<…>.bearerLane.record(<константа>, <константа>)`, — разбором дерева
// синтаксиса, а не поиском по тексту.
func recordedBearerStates(t *testing.T, src string) map[BearerLaneState]int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "auth_revocation.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	consts := map[string]string{
		"presentedSourceAuthority": presentedSourceAuthority, "presentedSourceRecord": presentedSourceRecord,
		"presentedOutcomeLive": presentedOutcomeLive, "presentedOutcomeRevoked": presentedOutcomeRevoked,
		"presentedOutcomeUnanswered": presentedOutcomeUnanswered, "presentedOutcomeMisconfigured": presentedOutcomeMisconfigured,
		"presentedOutcomeNoIdentifier": presentedOutcomeNoIdentifier, "presentedOutcomeNotWired": presentedOutcomeNotWired,
	}
	val := func(e ast.Expr) string {
		switch v := e.(type) {
		case *ast.Ident:
			return consts[v.Name]
		case *ast.BasicLit:
			s, _ := strconv.Unquote(v.Value)
			return s
		}
		return ""
	}
	out := map[BearerLaneState]int{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "record" {
			return true
		}
		if inner, ok := sel.X.(*ast.SelectorExpr); !ok || inner.Sel.Name != "bearerLane" {
			return true
		}
		out[BearerLaneState{Source: val(call.Args[0]), Outcome: val(call.Args[1])}]++
		return true
	})
	return out
}

// TestBearerLaneRecordsOnlyDeclaredStates — перечень состояний на приборе и
// ветки кода полосы сходятся в обе стороны: каждая записываемая пара объявлена
// (иначе клетки нет и запись теряется), и каждое объявленное состояние где-то
// записывается (иначе клетка вечно стоит нулём и её ноль ничего не говорит).
func TestBearerLaneRecordsOnlyDeclaredStates(t *testing.T) {
	raw, err := os.ReadFile("auth_revocation.go")
	if err != nil {
		t.Fatalf("носитель ветвей полосы не читается: %v", err)
	}
	recorded := recordedBearerStates(t, string(raw))
	declared := map[BearerLaneState]bool{}
	for _, s := range BearerLaneStates() {
		declared[s] = true
	}
	t.Logf("перепись: состояний объявлено %d · мест записи %d · различных записываемых %d",
		len(declared), sumCounts(recorded), len(recorded))
	if len(recorded) == 0 {
		t.Fatal("мест записи ноль — обход пуст либо полоса клеток не пишет")
	}
	for s := range recorded {
		if !declared[s] {
			t.Errorf("записывается необъявленное состояние %+v — клетки нет, событие теряется", s)
		}
	}
	for s := range declared {
		if recorded[s] == 0 {
			t.Errorf("объявленное состояние %+v не записывается ни одной веткой — его ноль ничего не утверждает", s)
		}
	}
}

// Инъекция в обе стороны: запись необъявленного состояния — находка; запись
// объявленного — молчит.
func TestBearerLaneStatesInjection(t *testing.T) {
	bad := recordedBearerStates(t, `package p
func f(a *A) { a.bearerLane.record(presentedSourceRecord, "rumour") }`)
	if bad[BearerLaneState{Source: presentedSourceRecord, Outcome: "rumour"}] != 1 {
		t.Fatalf("запись необъявленного состояния не распознана: %v", bad)
	}
	good := recordedBearerStates(t, `package p
func f(a *A) { a.bearerLane.record(presentedSourceAuthority, presentedOutcomeLive) }`)
	if good[BearerLaneState{Source: presentedSourceAuthority, Outcome: presentedOutcomeLive}] != 1 || len(good) != 1 {
		t.Fatalf("запись объявленного состояния распознана неверно: %v", good)
	}
}

func sumCounts(m map[BearerLaneState]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}
