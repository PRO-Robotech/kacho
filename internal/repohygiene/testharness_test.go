// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// harnessImporters — непроверочные файлы дерева, импортирующие пакет-харнесс.
func harnessImporters(sources map[string]string, pkg string) []string {
	imp := `"github.com/PRO-Robotech/kacho/` + pkg + `"`
	var out []string
	for rel, src := range sources {
		if strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, pkg+"/") {
			continue
		}
		if strings.Contains(src, imp) {
			out = append(out, rel)
		}
	}
	return out
}

// TestHarnessPackagesAreImportedOnlyByTests — пакет-харнесс вне суда
// продуктовых гейтов ровно до тех пор, пока его не импортирует продукт.
func TestHarnessPackagesAreImportedOnlyByTests(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен", err)
	}
	sources := map[string]string{}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr == nil {
			sources[rel] = string(b)
		}
	}
	t.Logf("перепись: пакетов-харнессов %d · файлов Go прочитано %d", len(TestHarnessPackages), len(sources))
	if len(sources) == 0 {
		t.Fatal("прочитано ноль файлов — «ноль импортёров» здесь означало бы «ноль прочитанного»")
	}
	for _, pkg := range TestHarnessPackages {
		own := 0
		for rel := range sources {
			if strings.HasPrefix(rel, pkg+"/") && !strings.HasSuffix(rel, "_test.go") {
				own++
			}
		}
		if own == 0 {
			t.Errorf("%s: пакета-харнесса нет в дереве — запись пережила свой предмет", pkg)
		}
		for _, imp := range harnessImporters(sources, pkg) {
			t.Errorf("%s: харнесс импортирует непроверочный файл %s — пакет стал частью продукта и "+
				"обязан стоять под его гейтами", pkg, imp)
		}
	}
}

// Инъекция в обе стороны: непроверочный импортёр — находка, проверочный — нет.
func TestHarnessImportersInjection(t *testing.T) {
	t.Parallel()
	pkg := "gateway/internal/e2e/ka1stand"
	imp := "import \"github.com/PRO-Robotech/kacho/" + pkg + "\"\n"
	if got := harnessImporters(map[string]string{"gateway/cmd/x/main.go": imp}, pkg); len(got) != 1 {
		t.Fatalf("непроверочный импортёр не найден: %v", got)
	}
	if got := harnessImporters(map[string]string{"gateway/cmd/x/main_test.go": imp, pkg + "/a.go": imp}, pkg); len(got) != 0 {
		t.Fatalf("проверочный импортёр либо сам пакет приняты за продукт: %v", got)
	}
}
