// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package protofieldreaders

import (
	"os"
	"path/filepath"
	"testing"
)

// ownermodule_test.go — TestOwnerModuleRejectsAnAbsoluteDir locks the fix for
// PRO-Robotech/kacho#2211 (census row 3): ownerModule's loop terminates on
// `cur == "."`, and that guard never fires for an absolute dir — filepath.Dir
// never turns an absolute path into ".". Confirmed one-fact, by direct call:
// an absolute dir sitting inside a REAL nested module returned (".", rootPath,
// nil) — the root module, silently, though the correct owner was the nested
// one and its go.mod was walked straight past.
//
// Reachability, named honestly: planWalks (ownerModule's only caller) filters
// out any pattern that does not start with "./" BEFORE it ever reaches
// ownerModule (see the `!strings.HasPrefix(pat, "./")` branch there), so this
// input is not reachable through Build()/the CLI today — a caller-side filter,
// not a property ownerModule enforces itself. #2211 asks for the same remedy
// already applied at the sibling coordinates of this class: check the premise
// and name it, rather than let the caller's incidental filtering be the only
// thing standing between a silent wrong answer and the truth.
func TestOwnerModuleRejectsAnAbsoluteDir(t *testing.T) {
	root := t.TempDir()
	writeGoModForTest(t, root, "example.com/outer")
	nested := filepath.Join(root, "nested")
	mustMkdirAll(t, nested)
	writeGoModForTest(t, nested, "example.com/nested")

	abs := filepath.Join(nested, "pkg")
	mustMkdirAll(t, abs)

	_, _, err := ownerModule(root, abs, "example.com/outer")
	if err == nil {
		t.Fatal("absolute dir was accepted silently — the walk-up climbs cwd-relative " +
			"segments and cannot resolve an absolute path; it must refuse, not guess")
	}
}

// Законный близнец: тот же вложенный модуль, найденный ОТНОСИТЕЛЬНЫМ dir,
// резолвится верно — иначе отказ на абсолютном входе значил бы «отказ на
// всём», а не «отказ на неверной форме входа».
func TestOwnerModuleFindsANestedModuleByRelativeDir(t *testing.T) {
	root := t.TempDir()
	writeGoModForTest(t, root, "example.com/outer")
	nested := filepath.Join(root, "nested")
	mustMkdirAll(t, nested)
	writeGoModForTest(t, nested, "example.com/nested")
	pkgDir := filepath.Join(nested, "pkg")
	mustMkdirAll(t, pkgDir)

	owner, ownerPath, err := ownerModule(root, "nested/pkg", "example.com/outer")
	if err != nil {
		t.Fatalf("relative dir into a real nested module must resolve, got error: %v", err)
	}
	if owner != "nested" || ownerPath != "example.com/nested" {
		t.Fatalf("owner=%q ownerPath=%q, want owner=\"nested\" ownerPath=\"example.com/nested\"", owner, ownerPath)
	}
}

func writeGoModForTest(t *testing.T, dir, module string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}
