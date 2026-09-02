// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package manifest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest"
)

// resourceforms_test.go — ФОРМЫ раздела `resources`, заведённые ради блоков,
// которые канон несёт сегодня и которые прежняя форма не выражала ни при каком
// входе (#1845, #1846, #1853, #1858, #1860).
//
// Каждая проба здесь — ПАРА: отрицание (загрузчик отвергает и называет поле и
// правило) плюс положительный контроль (законный близнец проходит). Отрицание без
// пары зеленеет на форме, отвергающей всё, и потому утверждением не является.

// resourceDoc — манифест с ОДНИМ ресурсом vpc, куда подставляется проверяемый кусок.
func resourceDoc(body string) string {
	return "apiVersion: iam/v1\nmodule: vpc\nresources:\n  - name: network\n" +
		"    objectType: vpc_network\n    producer: derived\n" + body +
		"    verbs: [get]\n"
}

// mustRefuse требует отказа с названной причиной и с каждым из названных слов в
// тексте: отказ, не называющий поля и правила, посылает автора искать опечатку.
func mustRefuse(t *testing.T, doc string, kind error, mentions ...string) {
	t.Helper()
	_, err := manifest.Load([]byte(doc))
	if err == nil {
		t.Fatalf("вход принят, хотя должен быть отвергнут:\n%s", doc)
	}
	if !errors.Is(err, kind) {
		t.Fatalf("отказ не отнесён к своей причине (%v): %v", kind, err)
	}
	for _, m := range mentions {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("отказ не называет %q: %v", m, err)
		}
	}
}

// mustAccept — законный близнец: без него отрицание выше ничего не утверждает.
func mustAccept(t *testing.T, doc string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load([]byte(doc))
	if err != nil {
		t.Fatalf("парный положительный отвергнут: %v\n%s", err, doc)
	}
	return m
}

// ── Указатели: имя и тип раздельно (#1860), их бывает несколько (#1858) ──────

// TestMODMR28ParentTypeMayBeAClosedTableType — тип указателя расширен ТИПАМИ, а
// не якорями области.
//
// Замер: `registry_repository` указывает на `registry_registry`, который областью
// выдачи не является ни при каком написании. Якорь области остаётся закрытым
// набором и здесь не расширяется — расширяется словарь ТИПОВ.
func TestMODMR28ParentTypeMayBeAClosedTableType(t *testing.T) {
	m := mustAccept(t, "apiVersion: iam/v1\nmodule: registry\nresources:\n  - name: repository\n"+
		"    objectType: registry_repository\n    producer: authored\n"+
		"    parents:\n      - {name: parent, type: registry_registry}\n    verbs: [get]\n")
	p := m.Resources[0].Parents
	if len(p) != 1 || p[0].Name != "parent" || p[0].Type != "registry_registry" {
		t.Fatalf("имя и тип указателя не разделены: %+v", p)
	}

	// Отрицание: тип вне обоих словарей.
	mustRefuse(t, "apiVersion: iam/v1\nmodule: registry\nresources:\n  - name: repository\n"+
		"    objectType: registry_repository\n    producer: authored\n"+
		"    parents:\n      - {name: parent, type: registry_repositoryz}\n    verbs: [get]\n",
		manifest.ErrParentUnknown,
		"resources[0].parents[0].type", "registry_repositoryz", "project", "account", "cluster")
}

// TestMODMR28TheLongParentFormIsRefusedWhenNameEqualsType — одно значение
// выразимо ровно ОДНИМ способом.
//
// `{name: project, type: project}` и `project` дают побайтово одну и ту же строку
// блока. Приняв обе, раздел завёл бы два написания одного значения — тот самый
// класс, который манифест ловит у остальных ключей.
func TestMODMR28TheLongParentFormIsRefusedWhenNameEqualsType(t *testing.T) {
	mustRefuse(t, resourceDoc("    parents:\n      - {name: project, type: project}\n"),
		manifest.ErrParentFormRedundant,
		"resources[0].parents[0]", "parents: [project]")

	// Законный близнец: та же пара, записанная короткой формой.
	mustAccept(t, resourceDoc("    parents: [project]\n"))
	// И длинная форма там, где имя и тип РАЗЛИЧАЮТСЯ, остаётся законной.
	mustAccept(t, "apiVersion: iam/v1\nmodule: registry\nresources:\n  - name: repository\n"+
		"    objectType: registry_repository\n    producer: authored\n"+
		"    parents:\n      - {name: parent, type: registry_registry}\n    verbs: [get]\n")
}

// TestMODMR29DuplicateParentNameNamesBothIndices — два указателя под одним именем
// объявили бы одно отношение модели дважды.
func TestMODMR29DuplicateParentNameNamesBothIndices(t *testing.T) {
	mustRefuse(t, resourceDoc("    parents:\n      - project\n      - {name: project, type: account}\n"),
		manifest.ErrParentNameDuplicated,
		"resources[0].parents[1].name", "resources[0].parents[0]")

	// Законный близнец: два РАЗНЫХ имени — так написан `iam_access_binding`.
	mustAccept(t, resourceDoc("    parents: [project, account, cluster]\n"))
}

// TestMODMR29ParentsAreRequired — ресурс без указателя не с чем связать, и каскад
// супер-доступа выводить не от чего.
func TestMODMR29ParentsAreRequired(t *testing.T) {
	mustRefuse(t, resourceDoc(""), manifest.ErrParentRequired, "resources[0].parents")
	mustRefuse(t, resourceDoc("    parents: []\n"), manifest.ErrParentRequired, "resources[0].parents")
	mustAccept(t, resourceDoc("    parents: [project]\n"))
}

// TestMODMR29AnUnknownKeyInsideAParentIsRefusedWithItsLine — строгость к
// неизвестному ключу не теряется внутри собственного разбора формы.
//
// Библиотека не проносит `Decoder.KnownFields(true)` внутрь UnmarshalYAML — то же
// измерено у действия, — поэтому ключ сверяется до разбора, а отказ называет ключ
// и номер строки ровно как это делает библиотека.
func TestMODMR29AnUnknownKeyInsideAParentIsRefusedWithItsLine(t *testing.T) {
	_, err := manifest.Load([]byte(resourceDoc("    parents:\n      - {name: project, typ: project}\n")))
	if err == nil {
		t.Fatalf("неизвестный ключ указателя принят")
	}
	for _, want := range []string{"field typ not found in type parent", "line 8"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("отказ не называет %q: %v", want, err)
		}
	}
	// Законный близнец: тот же отображённый указатель с ВЕРНЫМ ключом.
	mustAccept(t, "apiVersion: iam/v1\nmodule: registry\nresources:\n  - name: repository\n"+
		"    objectType: registry_repository\n    producer: authored\n"+
		"    parents:\n      - {name: parent, type: registry_registry}\n    verbs: [get]\n")
}
