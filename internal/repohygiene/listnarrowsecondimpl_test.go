// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestListNarrowSecondImplementationKeepsItsReason — вторая реализация сужения
// списков живёт, пока её причина верна (#723).
//
// # Предмет
//
// Реализаций сужения на дереве ДВЕ: фундамент `pkg/listnarrow` и собственный
// пакет iam. Это записанное решение, а не недосмотр: у iam видимость строки
// определяется НАБОРОМ отношений, а фундамент принимает ОДНО действие.
//
// # Что именно проверяется — и почему именно это
//
// Проверяется не «две реализации существуют» (это состояние, а не свойство), а
// то, что **причина всё ещё верна**. Причина одна и она машинно наблюдаема:
// фундамент принимает одиночное `action string`. Научится набору — причина
// отпадает, и вторая реализация обязана уйти тем же изменением.
//
// Молчаливое сохранение обеих после этого — то же, что оставить в документе
// фундамента слово «единственная»: утверждение, пережившее свой предмет.
func TestListNarrowSecondImplementationKeepsItsReason(t *testing.T) {
	root := repoRoot(t)

	const (
		foundationDir = "pkg/listnarrow"
		secondImpl    = "services/iam/internal/authzfilter"
		decisionDoc   = "services/iam/docs/engineering/architecture/" +
			"list-narrowing-has-two-implementations.md"
	)

	secondExists := dirHasGo(t, filepath.Join(root, secondImpl))
	singleAction, scanned := foundationTakesASingleAction(t, filepath.Join(root, foundationDir))

	t.Logf("осмотрено функций фундамента %d; принимает одиночное действие: %v; "+
		"вторая реализация в дереве: %v", scanned, singleAction, secondExists)

	if scanned == 0 {
		t.Fatal("в пакете фундамента не разобрано ни одной функции — признак " +
			"разошёлся с деревом, и гейт стал тождественно-зелёным")
	}

	if !secondExists {
		// Идеал достигнут: вторая реализация снята. Гейт не имеет права падать на
		// собственной цели — он лишь требует, чтобы документ решения ушёл следом.
		if _, err := os.Stat(filepath.Join(root, decisionDoc)); err == nil {
			t.Errorf("%s: вторая реализация снята, а документ её решения остался — "+
				"утверждение пережило свой предмет", decisionDoc)
		}
		return
	}

	// Вторая реализация есть — значит её причина обязана быть верной И записанной.
	if !singleAction {
		t.Errorf("%s принимает НАБОР действий — причина держать %s отпала. "+
			"Переведи iam на фундамент и сними вторую реализацию вместе с её "+
			"пробами и документом решения (%s)", foundationDir, secondImpl, decisionDoc)
	}
	if _, err := os.Stat(filepath.Join(root, decisionDoc)); err != nil {
		t.Errorf("%s: решение держать вторую реализацию не записано (%v). Без записи "+
			"следующий читатель увидит дубликат и либо сведёт их, либо оставит, "+
			"не зная почему", decisionDoc, err)
	}

	// Шапка фундамента не вправе называть себя единственной, пока вторая стоит.
	doc, err := os.ReadFile(filepath.Join(root, foundationDir, "doc.go"))
	if err != nil {
		t.Fatalf("прочитать шапку фундамента: %v", err)
	}
	head := string(doc)
	if i := strings.Index(head, "# Здесь стояло"); i > 0 {
		head = head[:i] // разбор собственной ошибки — не утверждение о дереве
	}
	if strings.Contains(head, "ЕДИНСТВЕННАЯ реализация сужения") {
		t.Errorf("%s/doc.go называет себя единственной реализацией, при живой второй "+
			"(%s) — два места об одном предмете, верно одно", foundationDir, secondImpl)
	}
}

// dirHasGo — есть ли в каталоге хоть один непробный исходник.
func dirHasGo(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			return true
		}
	}
	return false
}

// foundationTakesASingleAction — принимает ли фундамент ОДНО действие.
//
// Признак: у экспортируемых функций пакета есть параметр `action` строкового
// типа и НЕТ параметра-набора действий. Разбор по AST, а не по тексту: слово
// «action» встречается в комментариях этого же пакета десятками.
func foundationTakesASingleAction(t *testing.T, dir string) (bool, int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("прочитать каталог фундамента: %v", err)
	}

	var scanned int
	sawSingle, sawSet := false, false
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
		if perr != nil {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Type.Params == nil {
				continue
			}
			scanned++
			for _, p := range fn.Type.Params.List {
				for _, name := range p.Names {
					if name.Name != "action" && name.Name != "actions" {
						continue
					}
					if _, isSlice := p.Type.(*ast.ArrayType); isSlice {
						sawSet = true
						continue
					}
					if id, isIdent := p.Type.(*ast.Ident); isIdent && id.Name == "string" {
						sawSingle = true
					}
				}
			}
		}
	}
	return sawSingle && !sawSet, scanned
}
