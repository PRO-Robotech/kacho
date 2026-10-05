// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"go/ast"
	"go/token"
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
	found, fset, files := emitterCalls(t)

	produced := map[string]int{}
	calls := 0
	for _, call := range found {
		calls++
		if len(call.Args) <= emitKindArg {
			t.Errorf("%s: вызов %s с %d аргументами — позиция вида уехала, и разбор судит не то",
				fset.Position(call.Pos()), emitFunc, len(call.Args))
			continue
		}
		lit, ok := call.Args[emitKindArg].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			t.Errorf("%s: вид задан не строковым литералом — перепись его не увидит, "+
				"и слово окажется вне наблюдения", fset.Position(call.Pos()))
			continue
		}
		produced[lit.Value[1:len(lit.Value)-1]]++
	}

	if calls == 0 {
		t.Fatalf("в %s не найдено ни одного вызова %s — разбор сломан, и «расхождений нет» получено даром",
			emitterDir, emitFunc)
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
	t.Logf("осмотрено файлов %d, вызовов производителя %d; слов различных %d: %v; объявлено словарём %d",
		files, calls, len(produced), words, len(declared))
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
	// Ожидаемое — перечень типов объекта, которые compute сужает поштучно
	// (`authzfilter.PerObjectTypes`): у каждого из них есть публичное создание и
	// тип модели, и каждый обязан быть опубликован (NTF3-60). Перечень взят у
	// производителя, а не выписан третий раз.
	want := append([]string(nil), authzfilter.PerObjectTypes...)
	sort.Strings(want)
	sorted := append([]string(nil), got...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(sorted, want) {
		t.Fatalf("словарь видов compute %q, ожидался %q", sorted, want)
	}
	for word := range Journal().Mapping.Kinds {
		for _, d := range got {
			if d == word {
				t.Fatalf("клиенту едет слово ХРАНИЛИЩА %q — как строка записана, есть частное дело владельца", word)
			}
		}
	}
}
