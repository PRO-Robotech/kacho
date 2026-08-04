// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package treecorpus_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// newRepo — настоящий репозиторий во временном каталоге: один отслеживаемый
// файл, одно правило игнорирования и один файл под ним.
//
// Дерево синтетическое, но git — настоящий: предмет проверки в том и состоит,
// что состав берётся у версионного контроля, поэтому подменять его нечем.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	write(".gitignore", "build/\n")
	write("sub/kept.go", "package sub\n")
	write("sub/build/generated.go", "package build\n")
	run("add", ".gitignore", "sub/kept.go")
	run("commit", "-qm", "seed")
	return dir
}

// TestUnderSeesTrackedAndNotIgnored — обе половины сразу, потому что порознь
// каждая зеленеет на сломанном: «игнорируемого нет» верно и когда корпус пуст,
// «отслеживаемое есть» верно и когда возвращено всё подряд.
func TestUnderSeesTrackedAndNotIgnored(t *testing.T) {
	repo := newRepo(t)

	files, err := treecorpus.Under(filepath.Join(repo, "sub"))
	if err != nil {
		t.Fatalf("Under: %v", err)
	}
	t.Logf("осмотрено: %d путей под sub/", len(files))

	var sawKept, sawIgnored bool
	for _, f := range files {
		switch filepath.Base(f) {
		case "kept.go":
			sawKept = true
		case "generated.go":
			sawIgnored = true
		}
	}
	if !sawKept {
		t.Errorf("отслеживаемый sub/kept.go в корпусе отсутствует — положительный "+
			"контроль провален, и «игнорируемого не видно» ничего не значит: пустой "+
			"корпус даёт тот же ответ. Получено: %v", files)
	}
	if sawIgnored {
		t.Errorf("sub/build/generated.go попал в корпус, хотя git его игнорирует "+
			"(.gitignore: build/). Такой файл не увидит ни свежий клон, ни CI: "+
			"вердикт проверки станет свойством рабочего каталога, а не коммита. "+
			"Получено: %v", files)
	}
}

// TestUnderRefusesOutsideARepository — недоступность индекса это ОТКАЗ.
//
// Молчаливый откат на обход диска вернул бы дефект, ради которого написан
// пакет, и сделал бы это невидимо: вызывающий получил бы непустой корпус и
// решил, что всё в порядке.
func TestUnderRefusesOutsideARepository(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "a.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := treecorpus.Under(outside)
	if err == nil {
		t.Fatalf("вне репозитория Under вернул %d путей и НЕ отказал: это тихий "+
			"откат на обход диска — ровно то, что пакет запрещает", len(files))
	}
	if len(files) != 0 {
		t.Errorf("при отказе корпус обязан быть пуст, получено %d", len(files))
	}
}

// TestUnderWithSuffixFiltersAndKeepsTheRefusal — отбор по суффиксу не отменяет
// ни одного из двух свойств выше.
func TestUnderWithSuffixFiltersAndKeepsTheRefusal(t *testing.T) {
	repo := newRepo(t)

	goFiles, err := treecorpus.UnderWithSuffix(repo, ".go")
	if err != nil {
		t.Fatalf("UnderWithSuffix: %v", err)
	}
	if len(goFiles) != 1 || filepath.Base(goFiles[0]) != "kept.go" {
		t.Errorf("ожидался ровно отслеживаемый kept.go, получено %v", goFiles)
	}

	all, err := treecorpus.UnderWithSuffix(repo)
	if err != nil {
		t.Fatalf("UnderWithSuffix без суффиксов: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("пустой набор суффиксов означает «все файлы»: ожидалось 2 "+
			"(.gitignore и sub/kept.go), получено %d — %v", len(all), all)
	}

	if _, err := treecorpus.UnderWithSuffix(t.TempDir(), ".go"); err == nil {
		t.Error("вне репозитория UnderWithSuffix обязан отказать так же, как Under")
	}
}
