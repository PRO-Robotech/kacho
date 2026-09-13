// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
)

// emitterFile — где живёт ПРОИЗВОДИТЕЛЬ слов журнала.
const emitterFile = "../repo/instance_repo.go"

// emitFunc — обёртка, которой репозиторий пишет строку журнала.
const emitFunc = "emitCompute"

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
	src, err := filepath.Abs(emitterFile)
	if err != nil {
		t.Fatalf("путь производителя не разрешился: %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("файла производителя нет (%s): разбор судил бы пустоту — %v", emitterFile, err)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, 0)
	if err != nil {
		t.Fatalf("производитель не разобрался: %v", err)
	}

	produced := map[string]int{}
	calls := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != emitFunc {
			return true
		}
		calls++
		if len(call.Args) <= emitChangeArg {
			t.Errorf("%s: вызов %s с %d аргументами — позиция рода изменения уехала, "+
				"и разбор судит не то", fset.Position(call.Pos()), emitFunc, len(call.Args))
			return true
		}
		lit, ok := call.Args[emitChangeArg].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			t.Errorf("%s: род изменения задан не строковым литералом — перепись его "+
				"не увидит, и слово окажется вне наблюдения", fset.Position(call.Pos()))
			return true
		}
		produced[lit.Value[1:len(lit.Value)-1]]++
		return true
	})

	if calls == 0 {
		t.Fatalf("в %s не найдено ни одного вызова %s — разбор сломан, и «расхождений нет» "+
			"получено даром", emitterFile, emitFunc)
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
	t.Logf("осмотрено вызовов производителя %d; слов различных %d: %v; объявлено словарём %d",
		calls, len(produced), words, len(declared))
}
