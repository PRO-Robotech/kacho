// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/authz/proxytuple"
)

// proxyforbiddentypes_test.go — гейт над ДЕРЕВОМ: запретительный набор типов
// проксируемой записи сверяется с моделью прав в обе стороны.
//
// Предмет и обе стороны разобраны на proxyforbiddentypes.go; здесь — только вход:
// набор берётся ИМПОРТОМ той же карты, которую исполняет владелец в рантайме
// (pkg/authz/proxytuple), а типы — разбором канонической модели из дерева. Копии
// набора здесь не заводится: копия, которую нельзя скомпилировать против
// оригинала, расходится молча — ровно тот довод, по которому правило и переехало
// в общий фундамент.
func TestForbiddenProxyObjectTypesAgreeWithTheModel(t *testing.T) {
	root := repoRoot(t)

	entries := proxytuple.ForbiddenObjectTypes()
	if len(entries) == 0 {
		t.Fatal("запретительный набор пуст: судить нечего, и всякое «ноль находок» " +
			"относилось бы к непрочитанному")
	}

	declared := collectModelObjectTypes(t, root)
	modelTypes := make([]string, 0, len(declared))
	for typ := range declared {
		modelTypes = append(modelTypes, typ)
	}
	sort.Strings(modelTypes)

	faults, census := judgeForbiddenProxyTypes(entries, modelTypes, proxyConsumerDomains)
	t.Logf("перепись: %s", census.Summary())

	if census.NonModuleTypes == 0 {
		t.Fatal("типов вне доменов-эмитентов не найдено ни одного: либо перепись " +
			"эмитентов покрыла собой всю модель, либо разбор модели читает не то — в " +
			"обоих случаях сторона «тип без записи» ничего не судит")
	}

	if len(faults) > 0 {
		t.Fatalf("запретительный набор разошёлся с моделью прав (%d):\n%s\n\nперепись: %s",
			len(faults), strings.Join(faults, "\n"), census.Summary())
	}
}
