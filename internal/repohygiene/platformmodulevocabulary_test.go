// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/platformmodules"
)

// platformmodulevocabulary_test.go — гейт над ДЕРЕВОМ: словарь имён модулей
// платформы сверяется с тремя своими производителями (задача #1885).
//
// Предмет и обе стороны каждой колонки разобраны на
// platformmodulevocabulary.go; здесь — только добыча входа. Способность падать
// доказывает не этот прогон, а инъекция
// (platformmodulevocabulary_injection_test.go).
func TestPlatformModuleVocabularyMatchesTheTree(t *testing.T) {
	root := repoRoot(t)

	declared := platformmodules.All()
	if len(declared) == 0 {
		t.Fatal("словарь модулей пуст: судить нечего, и всякое «ноль находок» " +
			"относилось бы к непрочитанному")
	}

	faults, census := judgePlatformVocabulary(
		declared,
		dirNamesUnder(t, filepath.Join(root, "services")),
		dirNamesUnder(t, filepath.Join(root, "proto", "kacho", "cloud")),
		collectModelObjectTypes(t, root),
	)

	t.Logf("перепись: %s", census.Summary())

	if len(faults) > 0 {
		t.Fatalf("словарь имён модулей разошёлся с деревом (%d):\n  %s\n\nперепись: %s",
			len(faults), strings.Join(faults, "\n  "), census.Summary())
	}
}

// dirNamesUnder — имена подкаталогов, множеством. Пустой обход — отказ: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
func dirNamesUnder(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("читаю %s: %v", dir, err)
	}
	out := map[string]struct{}{}
	for _, e := range entries {
		if e.IsDir() {
			out[e.Name()] = struct{}{}
		}
	}
	if len(out) == 0 {
		t.Fatalf("в %s не найдено ни одного подкаталога — обход пуст, и вердикт "+
			"относился бы к непрочитанному", dir)
	}
	return out
}
