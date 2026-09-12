// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// foundationtreepathliteral_test.go — гейт над ДЕРЕВОМ. Предмет и обе стороны
// разобраны на foundationtreepathliteral.go; здесь только добыча входа.
//
// Способность падать доказывает не этот прогон, а инъекция
// (foundationtreepathliteral_injection_test.go).
//
// # Литералы читаются УЗЛОМ разбора
//
// Координата встречается в комментариях этого дерева сотнями — и в шапке самого
// гейта тоже, — поэтому поиск по подстроке краснел бы на собственном объяснении.
// `go/parser` отдаёт строковый литерал отдельным узлом, комментарий узлом
// литерала не является by construction.

func TestFoundationProdCodeNamesNoForeignTreePath(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	tracked, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	if len(tracked) == 0 {
		t.Fatal("под корнем нет ни одного отслеживаемого файла Go — обход пуст")
	}

	roots := forbiddenTreeRootsForFoundation()
	census := treePathLiteralCensus{Roots: len(roots)}
	pkgs := map[string]struct{}{}
	var found []treePathLiteral

	for _, abs := range tracked {
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			t.Fatalf("путь %s относительно корня: %v", abs, relErr)
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "pkg/") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		// Переезжает то, чей класс — фундамент либо оснастка сборки: оснастка
		// физически живёт в `corelib` (см. foundationboundary.go), поэтому её
		// координаты ломаются тем же переездом.
		cls, ok := classOfPackage(path.Dir(rel))
		if !ok || (cls != classCorelib && cls != classToolchain) {
			continue
		}
		pkgs[path.Dir(rel)] = struct{}{}
		census.ProdFiles++

		src, readErr := os.ReadFile(abs)
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		af, parseErr := parser.ParseFile(token.NewFileSet(), rel, src, 0)
		if parseErr != nil {
			t.Fatalf("разбор %s: %v", rel, parseErr)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			bl, isLit := n.(*ast.BasicLit)
			if !isLit || bl.Kind != token.STRING {
				return true
			}
			value, uerr := strconv.Unquote(bl.Value)
			if uerr != nil {
				return true
			}
			census.Literals++
			if r, bad := literalNamesForbiddenRoot(value, roots); bad {
				found = append(found, treePathLiteral{File: rel, Value: value, Root: r})
			}
			return true
		})
	}
	census.Packages = len(pkgs)

	// ПРЕДМЕТ ОСИ ИСЧЕРПАН, А НЕ РАСПОЗНАВАТЕЛЬ ОСЛЕП: переезд `corelib`/
	// `оснастка сборки` из pkg/ этого дерева ЗАВЕРШЁН (`foundationboundary.go`,
	// §«переезд завершён» у foundationClasses) — под pkg/ не осталось ни одного
	// каталога этих двух классов, значит и «прод-кода фундамента» в смысле этой
	// оси больше нет: судить нечего, а не «не нашли». Признак измерим и
	// самообновляем — как только под pkg/ снова появится каталог класса
	// corelib/оснастка, census.Packages станет не нулём, и ось возобновится
	// сама, без правки этого файла. judgeFoundationTreePathLiterals при этом
	// НЕ меняется: вызванная прямо (инъекцией), она по-прежнему обязана
	// падать на пустом обходе — это её собственная предпосылка, отдельная от
	// того, легитимна ли пустота предмета здесь.
	if census.Packages == 0 {
		t.Skipf("каталогов pkg/* класса corelib/оснастка сборки нет: 0 предметов оси в " +
			"дереве — координаты чужого дерева проверяются в исходниках, которых не " +
			"существует. Возобновится сама, если такой каталог появится")
	}

	faults := judgeFoundationTreePathLiterals(found, census)
	t.Log(census.String())
	if len(faults) != 0 {
		t.Fatalf("прод-код фундамента называет координату чужого дерева (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
}
