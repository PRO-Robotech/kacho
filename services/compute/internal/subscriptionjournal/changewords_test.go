// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
)

// emitterDir — где живёт ПРОИЗВОДИТЕЛЬ слов журнала: каждый не-тестовый файл
// репозитория. Перечень файлов выводится обходом, а не выписывается: вид,
// заведённый новым файлом репозитория, иначе остался бы вне переписи.
const emitterDir = "../repo"

// emitFunc — обёртка, которой репозиторий пишет строку журнала.
const emitFunc = "emitCompute"

// emitterCalls — все вызовы [emitFunc] в не-тестовых файлах [emitterDir] и
// число осмотренных файлов. Пустой обход — отказ пробы, а не пустой ответ.
func emitterCalls(t *testing.T) (calls []*ast.CallExpr, fset *token.FileSet, files int) {
	t.Helper()
	root, err := filepath.Abs(emitterDir)
	if err != nil {
		t.Fatalf("путь производителя не разрешился: %v", err)
	}
	fset = token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		files++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == emitFunc {
				calls = append(calls, call)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("обход производителя (%s) не удался: %v", emitterDir, err)
	}
	if files == 0 {
		t.Fatalf("в %s не осмотрено ни одного файла — разбор судил бы пустоту", emitterDir)
	}
	return calls, fset, files
}

// emitChangeArg — позиция рода изменения в её аргументах
// (`ctx, tx, kind, id, projectID, eventType, payload`).
const emitChangeArg = 5

// TestChangeDictionaryIsDerivedFromTheEmitter — словарь родов изменения сверяется
// с ПРОИЗВОДИТЕЛЕМ, а не со вторым рукописным перечнем.
//
// # Почему перепись, а не список
//
// У журнала compute нет ограничения базы на это поле — в отличие от соседнего
// журнала, где перечень берётся у `CHECK (action IN (…))`. Значит единственный
// производитель слов здесь — сам репозиторий, и сверять надо с ним. Проба,
// выписывающая слова второй раз, закрепляет ОТВЕТ словаря, а не его согласие с
// деревом: слово, заменённое на горячем пути на необъявленное, такой пробой не
// ловится ничем — ни здесь, ни на настоящей базе, где строка просто перестаёт
// доставляться, тихо.
//
// # Что именно утверждается — ОБЕ стороны
//
//	каждое слово производителя названо словарём  — иначе строка недоставляема;
//	каждое слово словаря имеет производителя     — иначе запись переживёт свой
//	                                               предмет и будет читаться как
//	                                               способность журнала.
//
// Пустой обход — отказ: ноль найденных вызовов означает, что разбор сломан
// (переименовали обёртку, сменили позицию аргумента), и тогда «расхождений нет»
// получено даром.
func TestChangeDictionaryIsDerivedFromTheEmitter(t *testing.T) {
	found, fset, files := emitterCalls(t)

	produced := map[string]int{}
	calls := 0
	for _, call := range found {
		calls++
		if len(call.Args) <= emitChangeArg {
			t.Errorf("%s: вызов %s с %d аргументами — позиция рода изменения уехала, "+
				"и разбор судит не то", fset.Position(call.Pos()), emitFunc, len(call.Args))
			continue
		}
		lit, ok := call.Args[emitChangeArg].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			t.Errorf("%s: род изменения задан не строковым литералом — перепись его "+
				"не увидит, и слово окажется вне наблюдения", fset.Position(call.Pos()))
			continue
		}
		produced[lit.Value[1:len(lit.Value)-1]]++
	}

	if calls == 0 {
		t.Fatalf("в %s не найдено ни одного вызова %s — разбор сломан, и «расхождений нет» "+
			"получено даром", emitterDir, emitFunc)
	}
	if len(produced) == 0 {
		t.Fatalf("вызовов %d, а слов ноль — разбор аргументов сломан", calls)
	}

	declared := Journal().Mapping.Changes

	for word := range produced {
		if declared[word] == subscriptionv1.SubscriptionEvent_CHANGE_UNSPECIFIED {
			t.Errorf("репозиторий пишет род %q, а словарь его НЕ называет: строка с ним "+
				"недоставляема, и потеря эта тихая — ни отказа, ни пропуска в нумерации", word)
		}
	}
	for word := range declared {
		if produced[word] == 0 {
			t.Errorf("словарь называет род %q, которого производитель не пишет НИ РАЗУ: "+
				"запись пережила свой предмет и читается как способность журнала", word)
		}
	}

	words := make([]string, 0, len(produced))
	for w, n := range produced {
		words = append(words, w)
		_ = n
	}
	sort.Strings(words)
	t.Logf("осмотрено файлов %d, вызовов производителя %d; слов различных %d: %v; объявлено словарём %d",
		files, calls, len(produced), words, len(declared))
}
