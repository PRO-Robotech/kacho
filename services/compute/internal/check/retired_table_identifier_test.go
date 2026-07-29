// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The retired-table gate next door searches for the table name adjacent to a SQL
// keyword — `FROM disks`, `INTO images`. That finds a query written out in full and
// misses the one that matters more: a query whose table name arrives through a
// variable.
//
//	fmt.Sprintf(`SELECT id, project_id FROM %s`, rt.table)
//
// Here the only place the name appears is a Go string in a table of names, several
// lines away from any SQL keyword, and the text search walks straight past it. That
// is not a hypothetical — it is how three of the four retired tables kept a live
// reader after the migration that dropped them, and how the gate written to prevent
// exactly that stayed green.
//
// So this reads the syntax tree instead of the text, and asks a different question:
// does any non-test compute source contain a string that IS the name of a dropped
// table? A name is in a program for one reason. It cannot be reached through a
// variable if it is not there.
//
// Parsing rather than grepping also settles the comment problem for free: a
// paragraph explaining what was dropped is not a *ast.BasicLit, so prose stays
// allowed, which it must be — the migrations and their reasons have to remain
// readable.

// TestNoRetiredTableNameSurvivesAsAGoIdentifier — the substance half of "no live
// path reaches the dropped tables".
func TestNoRetiredTableNameSurvivesAsAGoIdentifier(t *testing.T) {
	root := moduleRootFrom(t, ".")

	retired := map[string]bool{}
	for _, r := range retiredBlockStorage {
		retired[r.table] = true
	}
	if len(retired) == 0 {
		t.Fatal("no retired tables — this gate would assert nothing")
	}

	parsed := 0
	forEachComputeGoFile(t, root, func(rel string, file *ast.File) {
		parsed++
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if retired[strings.ToLower(strings.TrimSpace(val))] {
				t.Errorf("%s names dropped table %q as a string — a table name in a program is there to be queried, and this one no longer exists after the retire migration",
					rel, val)
			}
			return true
		})
	})
	if parsed == 0 {
		t.Fatal("no compute Go files were parsed — this gate asserted nothing")
	}
	t.Logf("census: %d non-test compute Go file(s) parsed for %d retired table name(s)", parsed, len(retired))
}

// forEachComputeGoFile parses every non-test compute Go file that can run in
// production. The migration directories are excluded for the same reason as in the
// text gate: an applied migration is never edited, so its CREATE and its DROP both
// stay on the record.
func forEachComputeGoFile(t *testing.T, root string, visit func(rel string, file *ast.File)) {
	t.Helper()
	computeDir := filepath.Join(root, "services", "compute")
	skipDirs := map[string]bool{
		filepath.Join(computeDir, "internal", "migrations"): true,
		filepath.Join(computeDir, "migrations"):             true,
	}
	fset := token.NewFileSet()

	err := filepath.WalkDir(computeDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[p] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(p) // #nosec G304 -- fixed in-repo paths under this module's own root
		if rerr != nil {
			return rerr
		}
		// Comments are not parsed: prose about a retired table is allowed, and must
		// be, or the gate would forbid explaining the drop it is enforcing.
		f, perr := parser.ParseFile(fset, p, src, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", p, perr)
		}
		visit(mustRel(t, root, p), f)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", computeDir, err)
	}
}
