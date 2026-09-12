// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// readMovingTree — состав ПЕРЕЕЗЖАЮЩИХ каталогов `pkg/` и путь модуля платформы,
// объявленный корневым `go.mod`.
//
// Класс каталога читается у карты границы (classOfPackage), а не выписывается:
// выписанный перечень был бы вторым местом об одном предмете и разошёлся бы с
// картой молча — ровно на каталоге, заведённом после.
func readMovingTree(t *testing.T, root string) (string, map[string]bool, map[string]string) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("корневой go.mod не прочитан: %v", err)
	}
	platform, ok := declaredModulePath(string(raw))
	if !ok {
		t.Fatal("корневой go.mod не объявляет модуля — путь платформы выводить не из чего")
	}

	tracked, err := treecorpus.Under(filepath.Join(root, "pkg"))
	if err != nil {
		t.Fatalf("состав pkg/: %v", err)
	}
	if len(tracked) == 0 {
		t.Fatal("под pkg/ нет ни одного отслеживаемого файла — обход пуст, вердикт беспредметен")
	}

	moving := map[string]bool{}
	contents := map[string]string{}
	unclassified := map[string]bool{}
	for _, abs := range tracked {
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			t.Fatalf("относительный путь для %s: %v", abs, rerr)
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "pkg" {
			continue // файлы уровня фундамента переезжают поимённо, пакетами не являются
		}
		cls, known := classOfPackage(dir)
		if !known {
			unclassified[dir] = true
			continue
		}
		if cls != classCorelib && cls != classToolchain {
			continue
		}
		moving[dir] = true
		body, rerr := os.ReadFile(abs) // #nosec G304 -- путь из индекса git под корнем дерева
		if rerr != nil {
			t.Fatalf("файл %s не прочитан: %v", rel, rerr)
		}
		contents[rel] = string(body)
	}
	if len(unclassified) > 0 {
		names := make([]string, 0, len(unclassified))
		for d := range unclassified {
			names = append(names, d)
		}
		sort.Strings(names)
		t.Fatalf("каталог `pkg/*` без объявленного класса (%d): %s — класс объявляется картой "+
			"границы, а не умолчанием", len(names), strings.Join(names, ", "))
	}
	return platform, moving, contents
}

// TestFoundationNamesNoMovingPackageOutsideAnImport — координата переезжающего
// пакета, названная внутри фундамента НЕ импортом, уедет дословно и указывать
// будет в пустоту.
//
// Ось отдельная от направления рёбер (TestNoModuleEdgeRunsAgainstTheTargetLayout):
// там судится, кто кого ЗОВЁТ, здесь — кто кого НАЗЫВАЕТ. Ссылка в прозе ребра не
// заводит и той осью не видна вовсе, а после публикации ломается ровно так же.
func TestFoundationNamesNoMovingPackageOutsideAnImport(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	platform, moving, contents := readMovingTree(t, root)

	// ПРЕДМЕТ ОСИ ИСЧЕРПАН, А НЕ РАСПОЗНАВАТЕЛЬ ОСЛЕП: переезд `corelib`/
	// `оснастка сборки` из pkg/ этого дерева ЗАВЕРШЁН (`foundationboundary.go`,
	// §«переезд завершён» у foundationClasses) — каталогов этих двух классов
	// под pkg/ не осталось ни одного, значит и «переезжающих путей в прозе»
	// у ЭТОГО дерева больше не бывает: предмет уехал целиком, а не спрятался.
	// Признак измерим и самообновляем: как только под pkg/ снова появится
	// каталог класса corelib/оснастка (K3-1 требует классифицировать его
	// ПРАВИЛОМ приёмки, не молчанием), moving станет непустым, и проверка
	// возобновится сама — без правки этого файла.
	if len(moving) == 0 {
		t.Skipf("переезжающих каталогов pkg/* нет: 0 предметов класса corelib/оснастка "+
			"сборки в дереве (%d прод-файлов pkg/ осмотрено) — ось не проверяет прозу "+
			"о том, чего не существует. Возобновится сама, если такой каталог появится",
			len(contents))
	}

	findings, stale, census := judgeFoundationProseCoordinatesAgainst(
		platform, moving, contents, knownBakedDescriptors)

	t.Logf("модуль платформы %q", platform)
	t.Logf("%s", census)

	if census.Quoted == 0 {
		t.Fatal("ни одного пути переезжающего пакета В КАВЫЧКАХ не найдено — распознаватель " +
			"разошёлся с деревом, и «ноль находок» здесь означало бы «ноль прочитанного»")
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Errorf("%d координат(ы) переезжающих пакетов названы не импортом:%s", len(findings), b.String())
	}
	if len(stale) > 0 {
		t.Errorf("%d записи ведомости запечённых дескрипторов разошлись с деревом:\n  %s",
			len(stale), strings.Join(stale, "\n  "))
	}
}
