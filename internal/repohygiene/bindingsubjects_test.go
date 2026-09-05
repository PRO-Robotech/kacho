// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// bindingsubjects_test.go — выдача записывается ВМЕСТЕ с составом субъектов.
//
// ПРЕДМЕТ. Форма вердикта заходит в выдачи с пары «субъект + область» через
// kacho_iam.access_binding_subjects. Выдача, у которой дочерних строк нет,
// невидима вердикту целиком: право записано, читается всеми списками — и не
// действует. Отличить это от «права не выдавали» нельзя ничем, кроме прямого
// запроса к базе, поэтому состояние и живёт незамеченным.
//
// ЗАМЕР, ради которого гейт заведён (живой стенд, 2026-08-21): выдач 450,
// дочерних строк 339, выдач без состава 111 — из них 110 от собственной выдачи
// администратора на проект и одна от приглашения. Свежие датированы днём
// замера: дефект действующий, а не наследие.
//
// ПОЧЕМУ НЕ ТРИГГЕРОМ. Первое решение проецировало пару субъекта из самой
// выдачи триггером — и было опровергнуто существующей пробой: выдача вправе
// ПЕРЕОПРЕДЕЛИТЬ состав, и тогда пара в её строке в состав не входит. Триггер,
// сработавший раньше, чем вызывающий записал состав, добавляет субъекта,
// которому не выдавали. Цена ошибки несимметрична — это расширение доступа, —
// поэтому инвариант держится статически, а не записью.
//
// ГОНКИ ЗДЕСЬ НЕТ: строка принадлежит выдаче, которую та же работа и создаёт.
// Предмет ban #10 — «инвариант под конкуренцией» — отсутствует; предмет «о нём
// забыли» ловится этим гейтом.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestBindingInsertAlwaysWritesItsSubjects — у каждого вызова Insert выдачи в
// прод-коде есть вызов InsertSubjects в той же функции.
func TestBindingInsertAlwaysWritesItsSubjects(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "services", "iam", "internal", "apps")

	var filesRead, funcsWithInsert int
	var offenders []string

	// Состав берётся у ИНДЕКСА, а не у файловой системы: неотслеживаемый файл не
	// принадлежит дереву, и обход диска дал бы вердикт о том, что лежит у
	// конкретного разработчика, а не о том, что в репозитории.
	files, err := treecorpus.UnderWithSuffix(dir, ".go")
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			continue
		}
		filesRead++
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			var hasInsert, hasSubjects bool
			ast.Inspect(fn.Body, func(m ast.Node) bool {
				sel, ok := m.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "Insert":
					// Только Insert У ПИСАТЕЛЯ ВЫДАЧ, а не любой Insert.
					if inner, ok := sel.X.(*ast.CallExpr); ok {
						if isel, ok := inner.Fun.(*ast.SelectorExpr); ok &&
							isel.Sel.Name == "AccessBindingsW" {
							hasInsert = true
						}
					}
				case "InsertSubjects":
					hasSubjects = true
				}
				return true
			})
			if hasInsert {
				funcsWithInsert++
				if !hasSubjects {
					rel, _ := filepath.Rel(root, path)
					offenders = append(offenders, rel+": "+fn.Name.Name)
				}
			}
			return true
		})
	}
	// Перепись объявляется всегда: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	t.Logf("перепись: файлов прочитано %d; функций, вставляющих выдачу, %d; без состава субъектов %d",
		filesRead, funcsWithInsert, len(offenders))

	if filesRead == 0 || funcsWithInsert == 0 {
		t.Fatalf("предпосылка гейта не выполнена: прочитано файлов %d, найдено вставок выдачи %d — "+
			"проверять нечего, и молчание здесь означало бы отсутствие проверки, а не отсутствие дефекта",
			filesRead, funcsWithInsert)
	}

	for _, o := range offenders {
		t.Errorf("выдача записана без состава субъектов: %s\n"+
			"    Такая выдача НЕВИДИМА форме вердикта: право записано, читается списками — и не действует.\n"+
			"    Почини вызовом InsertSubjects в той же транзакции, рядом со вставкой выдачи.", o)
	}
}
