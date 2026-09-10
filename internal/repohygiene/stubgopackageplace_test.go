// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// stubgopackageplace_test.go — гейт над ДЕРЕВОМ. Предмет, обе стороны и цена
// разобраны на stubgopackageplace.go; здесь только добыча входа.
//
// Способность падать доказывает не этот прогон, а инъекция
// (stubgopackageplace_injection_test.go).
//
// # Модуль файла берётся БЛИЖАЙШИМ объявлением вверх, а не константой
//
// Объявлений модуля в дереве сегодня два, и вся раскладка порождённого лежит в
// корневом. Выписать его константой было бы дешевле — и сделало бы гейт слепым
// ровно в день, ради которого он заведён: после разъезда стабы фундамента
// принадлежат ДРУГОМУ модулю, и константа объявила бы их расхождением, ничего
// не измерив.

func TestGeneratedStubGoPackageMatchesItsPlace(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	stubs, err := treecorpus.UnderWithSuffix(root, ".pb.go")
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	if len(stubs) == 0 {
		t.Fatal("под корнем нет ни одного отслеживаемого файла .pb.go — обход пуст")
	}
	sort.Strings(stubs)

	// `treecorpus` отдаёт АБСОЛЮТНЫЕ пути; предмет гейта — путь ОТ КОРНЯ дерева
	// (из него выводится путь импорта). Приведение делается один раз здесь, а не
	// в судящей функции: та обязана оставаться чистой.
	rels := make([]string, 0, len(stubs))
	for _, abs := range stubs {
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			t.Fatalf("путь %s относительно корня: %v", abs, relErr)
		}
		rels = append(rels, filepath.ToSlash(rel))
	}

	modules := moduleDeclarations(t, root)

	var (
		sites      []stubGoPackageSite
		unparsable []string
		without    int
	)
	for _, rel := range rels {
		src, readErr := os.ReadFile(filepath.Join(root, rel))
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		raw, ok, parseErr := rawDescriptorLiteral(src, rel)
		if parseErr != nil {
			t.Fatalf("%s: %v", rel, parseErr)
		}
		if !ok {
			without++
			continue
		}
		baked, descErr := goPackageOfDescriptor(raw)
		if descErr != nil {
			unparsable = append(unparsable, rel)
			continue
		}
		modDir, modPath := owningModule(modules, rel)
		if modPath == "" {
			t.Fatalf("%s: ни одно объявление модуля не покрывает этот файл — "+
				"вход гейта не построен, и вердикт о нём был бы вердиктом ни о чём", rel)
		}
		sites = append(sites, stubGoPackageSite{
			File:     rel,
			Baked:    baked,
			Expected: expectedImportPath(modPath, modDir, rel),
			Module:   modPath,
		})
	}

	faults, census := judgeStubGoPackagePlace(sites, unparsable, len(stubs), without)
	t.Log(census.String())
	if len(faults) != 0 {
		t.Fatalf("запечённый путь стаба расходится с его местом (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
}

// moduleDeclarations — каталог каждого `go.mod` дерева и объявленный им путь.
func moduleDeclarations(t *testing.T, root string) map[string]string {
	t.Helper()

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("индекс дерева: %v", err)
	}
	out := map[string]string{}
	for _, rel := range tree.SortedFiles() {
		if rel != "go.mod" && !strings.HasSuffix(rel, "/go.mod") {
			continue
		}
		src, readErr := os.ReadFile(filepath.Join(root, rel))
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		for _, line := range strings.Split(string(src), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "module ") {
				continue
			}
			dir := filepath.ToSlash(filepath.Dir(rel))
			if dir == "." {
				dir = ""
			}
			out[dir] = strings.TrimSpace(strings.TrimPrefix(line, "module "))
			break
		}
	}
	if len(out) == 0 {
		t.Fatal("в дереве нет ни одного объявления модуля — вход гейта не построен")
	}
	return out
}

// owningModule — объявление модуля, которому принадлежит файл: самая длинная
// совпавшая приставка каталога.
//
// Длиннейшая приставка, а не первая совпавшая: вложенное объявление обязано
// перебивать корневое, иначе файл службы приписался бы платформе.
func owningModule(modules map[string]string, file string) (string, string) {
	best, bestPath := "", ""
	for dir, mod := range modules {
		if dir != "" && !strings.HasPrefix(file, dir+"/") {
			continue
		}
		if bestPath == "" || len(dir) > len(best) {
			best, bestPath = dir, mod
		}
	}
	return best, bestPath
}
