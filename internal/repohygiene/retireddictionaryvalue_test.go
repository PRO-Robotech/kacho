// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// retiredDictionaryValues — НАДГРОБИЕ: значения словарей очередей, снятые
// решением. Контракт не вправе называть их ни при каком условии.
//
// Запись НЕ истекает от времени (см. шапку retireddictionaryvalue.go); истекает
// она только в одну сторону — если значение снова попало в живой словарь. Эту
// сторону проверяет TestRetiredDictionaryValue_LedgerDoesNotContradictLiveDictionary.
var retiredDictionaryValues = []RetiredDictionaryValue{
	{
		Value:      "jit_revoke",
		Dictionary: "kaname.subject_change_outbox.op",
		By:         "754001_subject_change_dictionary_admits_only_what_is_produced.sql",
		Reason: "подсистемы выдачи доступа по требованию в дереве нет вовсе " +
			"(предикат: git grep -rln 'JITAccess\\|jit_access\\|JustInTime' → ноль файлов); " +
			"производителя у значения не было ни одного",
	},
	{
		Value:      "bg_revoke",
		Dictionary: "kaname.subject_change_outbox.op",
		By:         "754001_subject_change_dictionary_admits_only_what_is_produced.sql",
		Reason: "фонового отзыва как подсистемы в дереве нет; производителя у значения " +
			"не было ни одного",
	},
}

// contractCorpus — файлы, в которых снятое значение является находкой всегда:
// исходный контракт и сгенерированные из него стабы.
func contractCorpus(t *testing.T) (root string, files []string) {
	t.Helper()
	root = repoRoot(t)
	protos, err := treecorpus.UnderWithSuffix(filepath.Join(root, "proto"), ".proto")
	if err != nil {
		t.Fatalf("корпус контракта не прочитан: %v — остановись здесь: "+
			"«снятых значений не найдено» неотличимо от «не смотрели»", err)
	}
	stubs, err := treecorpus.UnderWithSuffix(filepath.Join(root, "pkg", "api"), ".go")
	if err != nil {
		t.Fatalf("корпус стабов не прочитан: %v", err)
	}
	return root, append(protos, stubs...)
}

// TestRetiredDictionaryValue_ContractNamesNoRetiredValue — положительная сторона
// на НАСТОЯЩЕМ дереве: контракт не называет ни одного снятого значения.
func TestRetiredDictionaryValue_ContractNamesNoRetiredValue(t *testing.T) {
	root, files := contractCorpus(t)

	findings, census, err := AuditRetiredDictionaryValues(root, files, retiredDictionaryValues)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Logf("осмотрено: файлов контракта %d, строк %d; надгробий %d",
		census.Files, census.Lines, len(retiredDictionaryValues))

	// Премиса вердикта: корпус действительно прочитан. Числа — нижние границы,
	// заведомо перекрытые деревом; они держат отличие «ноль находок» от «ноль
	// прочитанного», а не пиннят размер дерева.
	if census.Files < 50 || census.Lines < 50000 {
		t.Fatalf("прочитано файлов %d, строк %d — корпус контракта заведомо больше; "+
			"молчание на таком входе ничего не доказывает", census.Files, census.Lines)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("контракт называет %d снятых значений словаря:\n%s\n"+
		"исходов три, четвёртого нет: (1) убрать значение из перечня контракта и "+
		"перегенерировать стабы; (2) если значение снова живое — вернуть его в словарь "+
		"миграцией и снять запись надгробия; (3) если речь о ДРУГОМ предмете с тем же "+
		"написанием — переименовать, потому что читатель контракта различить их не может",
		len(findings), strings.Join(lines, "\n"))
}

// TestRetiredDictionaryValue_LedgerDoesNotContradictLiveDictionary — вторая
// сторона самоистечения: надгробие, чьё значение снова стоит в живом словаре,
// лжёт о дереве.
func TestRetiredDictionaryValue_LedgerDoesNotContradictLiveDictionary(t *testing.T) {
	back := LiveIn(retiredDictionaryValues, declaredQueueEventDictionary)
	if len(back) > 0 {
		t.Errorf("надгробие называет снятыми значения, которые стоят в ЖИВОМ словаре: %s — "+
			"два места об одном предмете, из которых верно одно", strings.Join(back, ", "))
	}
	// Премиса: живой словарь прочитан, иначе молчание вакуумно.
	if len(declaredQueueEventDictionary) == 0 {
		t.Fatal("перепись живых словарей пуста — сверять надгробие не с чем")
	}
	t.Logf("сверено надгробий %d против живых словарей %d",
		len(retiredDictionaryValues), len(declaredQueueEventDictionary))
}

// TestRetiredDictionaryValue_LedgerIsWellFormed — сама перепись обязана быть
// переписью: без дублей и с причиной на каждой записи.
func TestRetiredDictionaryValue_LedgerIsWellFormed(t *testing.T) {
	seen := map[string]struct{}{}
	names := make([]string, 0, len(retiredDictionaryValues))
	for _, r := range retiredDictionaryValues {
		key := r.Dictionary + "=" + r.Value
		if _, dup := seen[key]; dup {
			t.Errorf("запись %q задвоена", key)
		}
		seen[key] = struct{}{}
		names = append(names, key)
		if strings.TrimSpace(r.Reason) == "" {
			t.Errorf("запись %q не несёт причины снятия", key)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Log("перепись не отсортирована — не ошибка, но затрудняет чтение диффа")
	}
}
