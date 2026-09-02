// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modulemanifestrights_injection_test.go — доказательство способности гейта
// УПАСТЬ и СМОЛЧАТЬ, по каждой оси отдельно (приёмка §9; сценарии MOD-MR-19, 24,
// 25, 26).
//
// # Почему на СИНТЕТИЧЕСКОМ дереве, а не на дереве продукта
//
// Манифестов в дереве продукта НОЛЬ, значит записи, о которой гейт судит, нет
// ВОВСЕ: его молчание там есть молчание ПО ОТСУТСТВИЮ ПРЕДМЕТА, а не по
// правильности, и контроль на нём выполнялся бы тривиально (`testing.md`
// §«Четыре класса проверок», п. 1). Здесь предмет существует by construction.
//
// # Инъекция роняет ТОЛЬКО проверяемое
//
// Форма «завести ещё один элемент» негодна: новый элемент нарушает всё, что
// требуется от элементов вообще. Поэтому у каждой оси снимается НОВОЕ свойство
// элемента, чьё СТАРОЕ на месте, и прогонов три: контроль (молчат все) ·
// инъекция (краснеет одна ось) · законный близнец (молчит снова).
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syntheticRightsTree — минимальное дерево, на котором предмет гейта существует.
//
// Каталог, модель и миграции кладутся ПО ТЕМ ЖЕ путям, что в продукте: гейт
// читает их ОТ КОРНЯ ДЕРЕВА, и только поэтому синтетика вообще возможна. Читай
// он от корня репозитория — доказательство меряло бы дерево продукта.
func syntheticRightsTree(t *testing.T, manifestBody string) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}

	// Каталог: на типе `vpc_network` аннотации якорят строку, на
	// `vpc_address_pool` — не якорят ни одной (тот же расклад, что в продукте).
	write(catalogRelPath, `[
  {"permission":"vpc.networks.get","required_relation":"viewer",
   "scope_extractor":{"object_type":"vpc_network","from_request_field":"network_id"}},
  {"permission":"vpc.address_pools.get","required_relation":"system_admin",
   "scope_extractor":{"object_type":"cluster","from_request_field":"*"}}
]`)
	write(modelRelPath, "type vpc_network\ntype vpc_address_pool\ntype cluster\n")
	write(migrationsRelDir+"/0001_roles.sql",
		`INSERT INTO roles (rules) VALUES ('[{"module":"vpc","verbs":["read","get"]}]');`)
	write("services/vpc/"+manifestBaseName, manifestBody)
	return root
}

// rightsManifest — манифест синтетического дерева, собранный из частей.
func rightsManifest(networkProducer, poolProducer, poolType, deprecatedVerb string) string {
	return "apiVersion: iam/v1\nmodule: vpc\nresources:\n" +
		"  - name: network\n    objectType: vpc_network\n    parent: project\n    producer: " + networkProducer + "\n    verbs: [get]\n" +
		"  - name: addressPool\n    objectType: " + poolType + "\n    parent: cluster\n    producer: " + poolProducer + "\n    verbs: [get]\n" +
		"deprecatedVerbs:\n  " + deprecatedVerb + ":\n    class: get\n    since: \"2026-08-23\"\n" +
		"    reason: синоним чтения из прежней грамматики\n    removeWhen: выдач с таким правом ноль\n"
}

// faultsOn — находки гейта на синтетическом дереве.
func faultsOn(t *testing.T, manifestBody string) []manifestRightFinding {
	t.Helper()
	root := syntheticRightsTree(t, manifestBody)
	tr := readTreeRights(t, newSyntheticTree(t, root))
	if len(tr.Manifests) != 1 {
		t.Fatalf("синтетика несёт %d манифестов вместо одного — инъекция проверяла бы не то, "+
			"что объявляет", len(tr.Manifests))
	}
	if tr.CatalogRows == 0 || len(tr.ModelTypes) == 0 || tr.MigrationFiles == 0 {
		t.Fatalf("предпосылка синтетики не создана: строк каталога %d · типов модели %d · "+
			"файлов миграций %d", tr.CatalogRows, len(tr.ModelTypes), tr.MigrationFiles)
	}
	return findManifestRightFaults(tr)
}

// namesOnly — находка обязана называть КООРДИНАТУ и предмет, а не симптом:
// находка, называющая симптом, посылает читателя искать не там.
func namesOnly(t *testing.T, faults []manifestRightFinding, wants ...string) {
	t.Helper()
	if len(faults) != 1 {
		t.Fatalf("находок %d при одной подсаженной: %v", len(faults), faults)
	}
	for _, want := range wants {
		if !strings.Contains(faults[0].Rel+" "+faults[0].Detail, want) {
			t.Errorf("находка не называет %q: %s: %s", want, faults[0].Rel, faults[0].Detail)
		}
	}
}

// TestManifestRightsGateControlIsSilent — контроль: целый манифест молчит.
//
// Без него всякое «краснеет» ниже зеленело бы и на гейте, который краснеет
// ВСЕГДА.
func TestManifestRightsGateControlIsSilent(t *testing.T) {
	faults := faultsOn(t, rightsManifest("derived", "authored", "vpc_address_pool", "read"))
	if len(faults) != 0 {
		t.Fatalf("законный манифест дал находки: %v", faults)
	}
}

// TestManifestRightsGateFindsDerivedWithoutAProducer — ось 1: `derived` там, где
// аннотации не якорят ни одной строки.
func TestManifestRightsGateFindsDerivedWithoutAProducer(t *testing.T) {
	namesOnly(t, faultsOn(t, rightsManifest("derived", "derived", "vpc_address_pool", "read")),
		"services/vpc/manifest.yaml", "resources[1]", "addressPool", "vpc_address_pool", "producer: authored")
}

// TestManifestRightsGateFindsAuthoredThatOutlivedItsSubject — ось 2: `authored`
// там, где аннотация ПОЯВИЛАСЬ. Зеркало первой оси; без него пометка жила бы
// вечно.
func TestManifestRightsGateFindsAuthoredThatOutlivedItsSubject(t *testing.T) {
	namesOnly(t, faultsOn(t, rightsManifest("authored", "authored", "vpc_address_pool", "read")),
		"resources[0]", "network", "vpc_network", "пережила свой предмет")
}

// TestManifestRightsGateFindsATypeTheModelDoesNotDeclare — ось 3: право на тип,
// которого не существует.
func TestManifestRightsGateFindsATypeTheModelDoesNotDeclare(t *testing.T) {
	namesOnly(t, faultsOn(t, rightsManifest("derived", "authored", "vpc_addresz_pool", "read")),
		"resources[1]", "vpc_addresz_pool", "не объявлен каноническим текстом модели")
}

// TestManifestRightsGateFindsADeprecatedVerbThatIsProducedAgain — ось 4: глагол
// объявлен устаревшим, а каталог его производит.
func TestManifestRightsGateFindsADeprecatedVerbThatIsProducedAgain(t *testing.T) {
	namesOnly(t, faultsOn(t, rightsManifest("derived", "authored", "vpc_address_pool", "get")),
		"deprecatedVerbs.get", "каталог его ПРОИЗВОДИТ")
}

// TestManifestRightsGateFindsADeprecatedVerbWithoutASubject — ось 5, ЗЕРКАЛО
// четвёртой: глагол исчез из правил ролей, и у записи не осталось предмета.
//
// Обе половины обязательны: одна даёт послабление, которое не истечёт.
func TestManifestRightsGateFindsADeprecatedVerbWithoutASubject(t *testing.T) {
	namesOnly(t, faultsOn(t, rightsManifest("derived", "authored", "vpc_address_pool", "browse")),
		"deprecatedVerbs.browse", "не осталось предмета")
}

// TestManifestRightsRecognizerChangedWhatIsInspected — расширение
// распознавателя обязано МЕНЯТЬ ОСМОТРЕННОЕ (MOD-MR-24).
//
// Не изменилось — расширение холостое и снимается, а не остаётся «на всякий
// случай». Перепись печатает числа ПО КАЖДОЙ форме отдельно: одно число скрыло
// бы ровно тот случай, ради которого гейт заведён.
func TestManifestRightsRecognizerChangedWhatIsInspected(t *testing.T) {
	root := syntheticRightsTree(t, rightsManifest("derived", "authored", "vpc_address_pool", "read"))
	tt := newSyntheticTree(t, root)

	goFiles, manifests := 0, 0
	for rel := range tt.files {
		switch {
		case strings.HasSuffix(rel, ".go"):
			goFiles++
		case filepath.Base(rel) == manifestBaseName:
			manifests++
		}
	}
	// До расширения распознаватель читал ТОЛЬКО не-тестовые `.go`; на этом
	// дереве их ноль. После — читает манифесты, и их единица. Осмотренное
	// изменилось, и это измерено, а не объявлено.
	if goFiles != 0 {
		t.Fatalf("синтетика несёт %d файлов Go — доказательство мерило бы не ту разницу", goFiles)
	}
	if manifests == 0 {
		t.Fatal("манифестов ноль: расширение распознавателя не изменило осмотренного, " +
			"и тогда оно холостое")
	}
	t.Logf("перепись синтетики: файлов Go %d · манифестов %d — до расширения гейт читал "+
		"первое число, после читает оба", goFiles, manifests)
}
