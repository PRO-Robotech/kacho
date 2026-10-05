// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"sort"
	"testing"

	"github.com/PRO-Robotech/corelib/subscription"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/subscriptionjournal"
)

// TestComputeJournal_NTF360_EveryModelTypedKindIsDeclaredWithNameFormAndScope —
// NTF3-60 (половина модуля compute), NTF3-61 (объявление), З2 замысла issue-2918.
//
// У compute три вида с публичным созданием и типом модели прав: машина, группа
// размещения, гостевой ключ (приёмка NTF-3 §1.2). Опубликованы они тогда и только
// тогда, когда стоят в закрытом словаре `Mapping.Kinds`: строка вне словаря
// недоставляема, а вид вне словаря отвергается на открытии подписки.
//
// Каждый вид обязан ОБЪЯВИТЬ форму имени и якорь: функция фундамента
// (`subscription.Journal.Emit`) пишет строку вида без объявления отказом, а
// сервер потока у такого вида имени снятия не отдаёт. Все три вида compute —
// проектные, имя у всех — DNS-метка.
//
// Тип объекта берётся у ПРОИЗВОДИТЕЛЯ (`authzfilter`), а не выписывается.
func TestComputeJournal_NTF360_EveryModelTypedKindIsDeclaredWithNameFormAndScope(t *testing.T) {
	want := map[string]string{
		"Instance":       authzfilter.ResourceTypeInstance,
		"PlacementGroup": authzfilter.ResourceTypePlacementGroup,
		"GuestAccessKey": authzfilter.ResourceTypeGuestAccessKey,
	}
	kinds := subscriptionjournal.Journal().Mapping.Kinds
	if len(kinds) == 0 {
		t.Fatal("словарь видов compute пуст — судить нечего, и «расхождений нет» было бы получено даром")
	}

	for word, objectType := range want {
		k, ok := kinds[word]
		if !ok {
			t.Errorf("вид %s (тип модели %s) не опубликован: его нет в Mapping.Kinds compute", word, objectType)
			continue
		}
		if k.ObjectType != objectType {
			t.Errorf("вид %s едет типом %q, ожидался %q", word, k.ObjectType, objectType)
		}
		if k.Action == "" {
			t.Errorf("вид %s без действия — унаследовал бы чужой глагол", word)
		}
	}
	for word, k := range kinds {
		if k.NameForm != subscription.NameFormDNS {
			t.Errorf("вид %s: NameForm = %d, ожидалась NameFormDNS (%d) — имя вида есть DNS-метка, и снятие обязано нести его снимок",
				word, k.NameForm, subscription.NameFormDNS)
		}
		if k.Scope != subscription.ScopeProject {
			t.Errorf("вид %s: Scope = %d, ожидался ScopeProject (%d) — предмет живёт в проекте",
				word, k.Scope, subscription.ScopeProject)
		}
	}

	dict := subscriptionjournal.Journal().KindDictionary()
	got := map[string]bool{}
	for _, d := range dict {
		got[d] = true
	}
	for word, objectType := range want {
		if !got[objectType] {
			t.Errorf("клиент не может назвать вид %s: типа %q нет в словаре видов %v", word, objectType, dict)
		}
	}
	words := make([]string, 0, len(kinds))
	for w := range kinds {
		words = append(words, w)
	}
	sort.Strings(words)
	t.Logf("объявлено видов %d: %v; ожидалось опубликованных %d", len(kinds), words, len(want))
}
