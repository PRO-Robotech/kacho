// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// moduleconstantoutlivesitsmodule_test.go — ГЕЙТ НА ДЕРЕВЕ.
//
// Норма, границы и разбор класса живут рядом с суждением —
// moduleconstantoutlivesitsmodule.go. Здесь только добыча входа.
//
// Корпус — ВСЕ файлы Go состава дерева, а не каталог гейтов. Причина в самом
// классе: константа с путём модуля живёт там, где судят чужой пакет, и это не
// привилегия одного каталога. Замер 2026-09-08 по release/kaname-tail: таких
// констант 61 в 6 каталогах, из них 16 называют вынесенный модуль — 5 вне его
// собственного дерева и 11 внутри (эти уедут ВМЕСТЕ с ним и находкой не станут).
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// TestModulePathConstantDoesNotOutliveItsModule — ось на живом дереве.
func TestModulePathConstantDoesNotOutliveItsModule(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tree := newTrackedTree(t, root)

	// Модули — из СОСТАВА, а не обходом диска: вердикт обязан быть свойством
	// коммита. Неотслеживаемый `go.mod` временного каталога объявил бы модуль,
	// которого в клоне нет.
	declared, required := knownModulesOfTree(t, root, tree)
	owners := moduleNameOwners(declared)

	files, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав дерева не прочитан: %v — вердикт беспредметен", err)
	}

	var consts []ModulePathConstant
	read, parsed := 0, 0
	for _, abs := range files {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			t.Fatalf("путь %s не приводится к корню: %v", abs, err)
		}
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(abs) // #nosec G304 — путь из состава дерева
		if err != nil {
			t.Fatalf("%s не прочитан: %v — это отказ, а не пропуск", rel, err)
		}
		found, n, err := collectModulePathConstants(rel, string(b), owners)
		if err != nil {
			// Неразбираемый файл Go — отказ: молча пропустив его, гейт объявил
			// бы «ноль находок» о непрочитанном.
			t.Fatalf("%s не разобран: %v", rel, err)
		}
		parsed++
		read += n
		consts = append(consts, found...)
	}

	faults, census := judgeModulePathConstants(consts, declared, required, parsed, read, len(owners))

	t.Logf("осмотрено: находок %d; объявленные модули: %s; затребованные свои: %s; %s",
		len(faults), strings.Join(declared, ", "), strings.Join(ownNamed(required, owners), ", "), census)

	for _, f := range faults {
		t.Errorf("%s.\n"+
			"  ЧТО ДЕЛАТЬ — один из двух исходов, «оставить как есть» в них не входит:\n"+
			"    1) перевести условие на признак, который дерево ПРОИЗВОДИТ — координату\n"+
			"       каталога либо объявление модуля, — тогда снятие предмета краснеет само;\n"+
			"    2) снять константу ВМЕСТЕ с предметом, тем же изменением, что и модуль.", f)
	}
}

// knownModulesOfTree — модули, которые дерево ЗНАЕТ: объявленные и затребованные.
//
// Читается СОСТАВ, а не диск: вердикт обязан быть свойством коммита.
// Неотслеживаемый `go.mod` временного каталога объявил бы модуль, которого в
// клоне нет.
func knownModulesOfTree(t *testing.T, root string, tree *trackedTree) (declared, required []string) {
	t.Helper()
	for _, rel := range tree.Tree.SortedFiles() {
		if filepath.ToSlash(rel) != "go.mod" && !strings.HasSuffix(rel, "/go.mod") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s есть в составе, но не прочитан: %v — это отказ, а не пропуск", rel, err)
		}
		m, ok := declaredModulePath(string(b))
		if !ok {
			t.Fatalf("%s не объявляет модуля: разбор `go.mod` разошёлся с деревом", rel)
		}
		declared = append(declared, m)
		required = append(required, requiredModulePaths(string(b))...)
	}
	if len(required) == 0 {
		t.Fatalf("ни один `go.mod` состава не объявляет `require`: разборщик затребованных "+
			"модулей разошёлся с деревом, и всякий затребованный модуль был бы объявлен "+
			"незнаемым — это отказ, а не чистота (объявленных прочитано %d)", len(declared))
	}
	return declared, required
}

// ownNamed — затребованные модули НАШЕГО владельца имён. Только они участвуют в
// оси, поэтому только их и осмысленно печатать: перечень из полутора сотен
// чужих пинов перепись бы не читали.
func ownNamed(modules, owners []string) []string {
	var out []string
	for _, m := range modules {
		for _, o := range owners {
			if strings.HasPrefix(m, o+"/") {
				out = append(out, m)
				break
			}
		}
	}
	return out
}
