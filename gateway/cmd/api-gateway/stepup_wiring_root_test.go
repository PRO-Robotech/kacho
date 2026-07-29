// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// stepup_wiring_root_test.go — the guard next door decides correctly; this
// asserts it is asked, and asked about the real thing.
//
// A start-up guard fed a constant is worse than no guard: it reports the healthy
// answer forever, and the report is what everyone reads. So the assertion is not
// "the guard appears in the composition root" — it is that the value it judges
// comes from the interceptor being asked whether the floor is mounted.
//
// main() cannot be exercised from a test (it dials backends and binds listeners),
// so this reads the composition root's SYNTAX TREE rather than its text: a regexp
// over source matches the word in a comment explaining the very thing it is
// checking, which is how a gate of this shape stays green over a removed control.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// rootFile parses the composition root.
func rootFile(t *testing.T) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("composition root must parse: %v", err)
	}
	return f
}

// calledFunctions collects the names of every function called in the file, by
// plain identifier (`f(...)`) and by selector (`x.M(...)`).
func calledFunctions(f *ast.File) map[string]int {
	out := map[string]int{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			out[fn.Name]++
		case *ast.SelectorExpr:
			if fn.Sel != nil {
				out[fn.Sel.Name]++
			}
		}
		return true
	})
	return out
}

// The floor has to be mounted by the composition root, on the layer that always
// runs. Nothing else in the process can put it there.
func TestCompositionRoot_MountsTheAuthenticationFloor(t *testing.T) {
	if calledFunctions(rootFile(t))["WithStepUp"] == 0 {
		t.Error("the composition root never mounts the authentication floor on the authN " +
			"interceptor — the catalog would declare a per-RPC assurance demand that this " +
			"process applies nowhere, which is the state this work removes")
	}
}

// And it has to ask the guard before serving anything.
func TestCompositionRoot_AsksTheStartupGuard(t *testing.T) {
	if calledFunctions(rootFile(t))["validateProductionStepUpConfig"] == 0 {
		t.Error("the composition root never asks the step-up start-up guard — a production stand " +
			"could then come up applying no floor at all, and nothing about it would say so")
	}
}

// The guard must judge what the interceptor REPORTS, not a value written next to
// the question. A literal here would answer "enforced" on a process that enforces
// nothing, and the refusal would never fire.
func TestCompositionRoot_GuardJudgesTheMountedInterceptor(t *testing.T) {
	f := rootFile(t)

	// 1. Find the identifier passed as StepUpConfig.Enforced.
	var enforcedBy string
	var literalUsed bool
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		name, ok := lit.Type.(*ast.Ident)
		if !ok || name.Name != "StepUpConfig" {
			return true
		}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Enforced" {
				continue
			}
			switch v := kv.Value.(type) {
			case *ast.Ident:
				// `true` and `false` are predeclared identifiers, not literals.
				if v.Name == "true" || v.Name == "false" {
					literalUsed = true
					continue
				}
				enforcedBy = v.Name
			default:
				literalUsed = true
			}
		}
		return true
	})

	if literalUsed || enforcedBy == "" {
		t.Fatal("StepUpConfig.Enforced is not read from a variable — a constant makes the guard " +
			"answer the same way whatever the process does, and a guard that cannot refuse is " +
			"the defect it was written against")
	}

	// 2. That variable must be assigned from the interceptor's own report.
	fromInterceptor := false
	ast.Inspect(f, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != enforcedBy || i >= len(assign.Rhs) {
				continue
			}
			ast.Inspect(assign.Rhs[i], func(m ast.Node) bool {
				call, ok := m.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel != nil &&
					sel.Sel.Name == "StepUpMounted" {
					fromInterceptor = true
				}
				return true
			})
		}
		return true
	})

	if !fromInterceptor {
		t.Errorf("%q is never assigned from the interceptor's StepUpMounted() report, so the "+
			"guard judges something other than whether the floor is actually applied",
			enforcedBy)
	}
}

// Premise of all three: this file is the composition root and it still builds the
// authN interceptor. If that moved elsewhere, the cases above would keep passing
// while asserting nothing about where the floor now lives.
func TestCompositionRoot_PremiseHolds(t *testing.T) {
	calls := calledFunctions(rootFile(t))
	if calls["NewAuthInterceptor"] == 0 {
		t.Fatal("main.go no longer builds the authN interceptor — the wiring cases in this file " +
			"have lost their subject; point them at wherever the interceptor is now assembled " +
			"rather than deleting them")
	}
}
