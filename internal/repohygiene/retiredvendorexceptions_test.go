// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retiredvendorexceptions_test.go — перечень исключений имени снятого
// поставщика судится по индексу git (задача #1276). Устройство и требования —
// шапка retiredvendorexceptions.go; способность упасть и смолчать — соседний
// retiredvendorexceptions_injection_test.go.
package repohygiene

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"

	"github.com/PRO-Robotech/kacho/internal/identityvendor"
)

// retiredVendorExceptionTreeOf — состав индекса и содержимое каждого файла
// области и каждого стража, названного пробой. Состав — из индекса, а не обходом
// диска: вердикт — свойство коммита.
func retiredVendorExceptionTreeOf(t *testing.T, root string) retiredVendorExceptionTree {
	t.Helper()
	tt := newTrackedTree(t, root)
	tree := retiredVendorExceptionTree{Tracked: tt.files, Text: map[string][]byte{}}
	need := map[string]bool{}
	for rel := range tt.files {
		if retiredVendorExceptionInScope(rel) {
			need[rel] = true
		}
	}
	for _, e := range retiredVendorExceptions {
		need[e.Path] = true
	}
	for rel := range need {
		if !tt.files[rel] {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s в индексе есть, а не читается: %v — судить его строки нечем", rel, err)
		}
		tree.Text[rel] = raw
	}
	return tree
}

// retiredVendorGrepCount — число строк вывода `git grep` теми же отметками по
// той же области: второе выражение того же предиката.
func retiredVendorGrepCount(t *testing.T, root string, marks []string) (lines int, command string) {
	t.Helper()
	args := []string{"grep", "-n", "-i", "-F"}
	shown := []string{"git", "grep", "-n", "-i", "-F"}
	for _, m := range marks {
		args = append(args, "-e", m)
		shown = append(shown, "-e", m)
	}
	args = append(args, "--", "*.go", "deploy/")
	shown = append(shown, "--", "'*.go'", "'deploy/'")
	out, err := gitenv.Command(root, args...).Output()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1 && len(out) == 0) {
		t.Fatalf("%s: %v — второе выражение предиката не исполнилось, и сверять обход не с чем",
			strings.Join(shown, " "), err)
	}
	for _, l := range strings.Split(string(out), "\n") {
		if l != "" {
			lines++
		}
	}
	return lines, strings.Join(shown, " ")
}

// TestRetiredVendorMentionsStayInsideTheNamedExceptionList — сам гейт: вне
// перечня — 0, каждая запись точна и не пуста.
func TestRetiredVendorMentionsStayInsideTheNamedExceptionList(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	marks := identityvendor.Marks()
	tree := retiredVendorExceptionTreeOf(t, root)

	found, c, err := judgeRetiredVendorExceptions(retiredVendorExceptions, marks, tree)
	if err != nil {
		t.Fatalf("перечень исключений: %v", err)
	}
	t.Log(c.String())

	// Предпосылка: обход читал область. Ноль файлов снаружи неотличим от «имени нет».
	if c.ScopeFiles == 0 {
		t.Fatalf("файлов в области ноль (%s) — гейт не читал дерева, и «вне перечня 0» "+
			"ничего не значит", retiredVendorExceptionScopeText)
	}
	// Предпосылка: обход видит то же, что команда тела задачи. Два выражения
	// одного предиката обязаны дать одно число; разошлись — один из двух слеп.
	grep, command := retiredVendorGrepCount(t, root, marks)
	t.Logf("второе выражение: %s → %d строк; обход индекса → %d строк", command, grep, c.MarkedLines)
	if grep != c.MarkedLines {
		t.Errorf("обход индекса насчитал %d строк с отметкой, а %s — %d: распознаватель гейта "+
			"видит не ту популяцию, что команда тела задачи, и его «вне перечня 0» о другом предмете",
			c.MarkedLines, command, grep)
	}
	for _, f := range found {
		t.Errorf("%s", f)
	}
}
