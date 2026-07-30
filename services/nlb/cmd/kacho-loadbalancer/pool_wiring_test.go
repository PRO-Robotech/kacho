// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// The pool-size setting reaches the pool only if the composition root builds the
// pool from Config.DSN(). Handing coredb.NewPool the raw URL instead leaves
// max-conns declared, validated, documented and shipped — and read by nobody, which
// is how it was found: a service whose List handlers borrow a connection per call
// ran on the pgxpool default of max(4, NumCPU) with no way to raise it.
//
// This is asserted against the source of the composition root because there is
// nothing else to assert it against: runServe dials peers and opens listeners, so
// it cannot be exercised in a unit test, and a pool built from the wrong string
// looks exactly like a pool built from the right one until production saturates.
//
// The gate carries its own premise: it fails if it cannot find the call at all, so
// "the wiring is right" can never be confused with "the call moved and I looked at
// nothing".
func TestPoolIsBuiltFromConfigDSN(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	require.NoError(t, err)

	found := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "NewPool" {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "coredb" {
			return true
		}
		found++
		require.Len(t, call.Args, 2, "coredb.NewPool(ctx, dsn)")

		arg, ok := call.Args[1].(*ast.CallExpr)
		require.True(t, ok,
			"the pool must be built from cfg.DSN() at %s — a bare field carries no pool_* parameter, "+
				"so repository.postgres.max-conns would be a setting with no reader",
			fset.Position(call.Args[1].Pos()))
		argSel, ok := arg.Fun.(*ast.SelectorExpr)
		require.True(t, ok && argSel.Sel.Name == "DSN",
			"the pool must be built from cfg.DSN() at %s", fset.Position(call.Args[1].Pos()))
		return true
	})
	require.Equal(t, 1, found,
		"expected exactly one coredb.NewPool call in the composition root; "+
			"zero means this gate read nothing and proved nothing")
}
