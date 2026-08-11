// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

// helpers_test.go — общие помощники обхода.
//
// Лежат в ТЕСТОВОМ файле намеренно: они нужны только гейтам, и в не-тестовом
// файле поехали бы в сборку продукта — вместе с запуском подпроцесса, который
// статический анализ справедливо считает поверхностью.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// repoRoot — поднимаемся от каталога теста до каталога с go.mod (корень репо).
//
// Копия соседского помощника — намеренная и дешёвая: связать пакеты ради шести
// строк значило бы завести между ними зависимость, которой нет по существу.
// Разойтись они не могут: предмет у обхода один — каталог с go.mod, и любое
// расхождение немедленно роняет оба пакета.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("не найден корень репозитория (каталог с go.mod)")
		}
		dir = parent
	}
}

// synthTrack делает синтетическое дерево видимым для git-индекса.
//
// Копия по той же причине, что и repoRoot: две строки git-команд, связывать
// пакеты ради которых дороже, чем повторить.
func synthTrack(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v в синтетическом дереве: %v\n%s", args, err, out)
		}
	}
}
