// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// main_test.go — коды выхода шага сборки: 0 — записано либо свежо, 1 —
// устарело с именем шаблона, 2 — сборки нет либо вызов вне формы.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("ФИКСТУРА: %s без go.mod", root)
	}
	return root
}

func TestRun_ExitCodes(t *testing.T) {
	root, out := repoRoot(t), t.TempDir()
	call := func(args ...string) (int, string) {
		var o, e bytes.Buffer
		code := run(args, &o, &e)
		return code, o.String() + e.String()
	}
	steps := []struct {
		name string
		args []string
		code int
		text string
	}{
		{"сверка до записи", []string{"-root", root, "-out", out, "-check"}, 1, "notify-probe/probe-hello"},
		{"запись", []string{"-root", root, "-out", out}, 0, "файлов сборки записано"},
		{"сверка после записи", []string{"-root", root, "-out", out, "-check"}, 0, "расхождений 0"},
		{"дерево без каталога источника", []string{"-root", t.TempDir(), "-out", out, "-check"}, 2, "notify-probe"},
		{"без -out", []string{"-root", root}, 2, "вызов"},
		{"лишний аргумент", []string{"-root", root, "-out", out, "x"}, 2, "вызов"},
	}
	for _, s := range steps {
		code, text := call(s.args...)
		if code != s.code || !strings.Contains(text, s.text) {
			t.Errorf("%s: код %d (ждали %d), вывод без %q:\n%s", s.name, code, s.code, s.text, text)
		}
	}
}
