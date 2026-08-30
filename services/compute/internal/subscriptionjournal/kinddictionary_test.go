// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
)

// emitKindArg — позиция вида предмета в аргументах производителя
// (`ctx, tx, kind, id, projectID, eventType, payload`).
const emitKindArg = 2

// TestJournalWordsAreDerivedFromTheEmitter — ключи словаря сверяются с
// ПРОИЗВОДИТЕЛЕМ строк журнала, а не со вторым рукописным перечнем.
//
// Ключ словаря есть слово, которым репозиторий пишет колонку `resource_kind`.
// Сегодня оно выписано в двух местах — литералом у каждого вызова производителя
// и константой здесь, — и расхождение между ними ТИХОЕ: строка с неназванным
// словом просто перестаёт доставляться, без отказа и без пропуска в нумерации.
// Проба, выписывающая слово третий раз, закрепила бы ОТВЕТ словаря, а не его
// согласие с деревом.
//
// Утверждаются обе стороны: каждое слово производителя названо словарём, и у
// каждого слова словаря есть производитель. Пустой обход — отказ.
func TestJournalWordsAreDerivedFromTheEmitter(t *testing.T) {
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
		if len(call.Args) <= emitKindArg {
			t.Errorf("%s: вызов %s с %d аргументами — позиция вида уехала, и разбор судит не то",
				fset.Position(call.Pos()), emitFunc, len(call.Args))
			return true
		}
		lit, ok := call.Args[emitKindArg].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			t.Errorf("%s: вид задан не строковым литералом — перепись его не увидит, "+
				"и слово окажется вне наблюдения", fset.Position(call.Pos()))
			return true
		}
		produced[lit.Value[1:len(lit.Value)-1]]++
		return true
	})

	if calls == 0 {
		t.Fatalf("в %s не найдено ни одного вызова %s — разбор сломан, и «расхождений нет» получено даром",
			emitterFile, emitFunc)
	}
	if len(produced) == 0 {
		t.Fatalf("вызовов %d, а слов ноль — разбор аргументов сломан", calls)
	}

	declared := Journal().Mapping.Kinds
	for word := range produced {
		if _, ok := declared[word]; !ok {
			t.Errorf("репозиторий пишет вид %q, а словарь его НЕ называет: строка с ним "+
				"недоставляема, и потеря эта тихая", word)
		}
	}
	for word := range declared {
		if produced[word] == 0 {
			t.Errorf("словарь называет вид %q, которого производитель не пишет НИ РАЗУ: "+
				"запись пережила свой предмет и читается как способность журнала", word)
		}
	}

	words := make([]string, 0, len(produced))
	for w := range produced {
		words = append(words, w)
	}
	sort.Strings(words)
	t.Logf("осмотрено вызовов производителя %d; слов различных %d: %v; объявлено словарём %d",
		calls, len(produced), words, len(declared))
}

// TestKindDictionaryIsWhatTheClientCanName — то, что compute объявляет клиенту,
// есть словарь ТИПОВ ОБЪЕКТА, а слово его хранилища наружу не выходит.
//
// Утверждение не косметическое: слово хранилища у этого журнала — `Instance`, с
// заглавной и без домена, то есть написание, которого в дереве больше нет
// нигде. Клиент, взявший его (а взять его было неоткуда, кроме неисполняемой
// пробы), получал бы отказ на всяком другом владельце.
func TestKindDictionaryIsWhatTheClientCanName(t *testing.T) {
	got := Journal().KindDictionary()
	want := []string{authzfilter.ResourceTypeInstance}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("словарь видов compute %q, ожидался %q", got, want)
	}
	if got[0] == JournalWordInstance {
		t.Fatalf("клиенту едет слово ХРАНИЛИЩА %q — как строка записана, есть частное дело владельца",
			JournalWordInstance)
	}
}
