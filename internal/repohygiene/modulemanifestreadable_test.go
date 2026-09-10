// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modulemanifestreadable_test.go — манифест модуля ПЛАТФОРМЫ разбираем и
// самоописателен (задача продукта #2361).
//
// # Почему этот гейт заведён ЗДЕСЬ
//
// Разбираемость манифестов судили две пробы внутри службы доступа
// (`services/iam/internal/manifest/peekmodule_internal_test.go` и
// `peekobjecttypes_internal_test.go`): они обходили ЖИВОЕ дерево платформы —
// `services/*/manifest.yaml` — и требовали, чтобы снятое двухступенчатым
// разбором имя модуля и снятые типы объектов совпадали с разобранными целиком.
//
// Документы при этом принадлежат ПЛАТФОРМЕ: их пишет и правит каждая служба у
// себя. Судья же уезжает вместе со службой доступа. После разреза шесть
// документов в дереве платформы не будет читать НИКТО: ни одна проверка не
// откроет их, и первая же опечатка — сбитый отступ, дубль ключа, потерянный
// `module` — доедет до потребителя молча.
//
// Поэтому платформенная половина заводится здесь ДО разреза.
//
// # Чем этот гейт НЕ является — и это половина его смысла
//
// Он НЕ второй судья ФОРМЫ. Схема манифеста принадлежит адресату документа
// (`apiVersion: iam/v1`), и полный разбор со строгими ключами живёт у него;
// повторить схему здесь значило бы завести два места об одном предмете, которые
// разойдутся на первом же новом разделе — и разойдутся молча.
//
// Здесь судится то, что платформа вправе утверждать о СВОИХ документах, не
// владея их схемой, и что после разреза не сможет утверждать никто другой:
//
//  1. РАЗБИРАЕМОСТЬ — документ читается как YAML. Нечитаемый документ есть
//     находка, а не «модуль ничего не объявил»;
//  2. САМООПИСАНИЕ — он называет адресата (`apiVersion`) и свой модуль
//     (`module`) непустыми строками. Документ без адресата неотличим от чужого
//     файла, случайно названного так же;
//  3. ЕДИНСТВЕННОСТЬ МОДУЛЯ — два каталога не объявляют один модуль. Имя
//     модуля есть ключ, по которому адресат их различает; совпадение делает
//     вердикт о них функцией порядка обхода;
//  4. ЕДИНСТВЕННОСТЬ ТИПА ОБЪЕКТА — два модуля не объявляют один тип. Тип есть
//     координата решения о доступе: объявленный дважды, он принадлежит тому,
//     кого прочли последним.
//
// Имя модуля и каталог совпадать НЕ обязаны, и это не послабление: каталог
// `nlb` объявляет модуль `loadbalancer`. Требовать совпадения значило бы
// объявить находкой действующее решение.
//
// # Почему перечень манифестов ВЫВОДИТСЯ
//
// Выписанный перечень разошёлся бы с деревом молча — и разошёлся бы именно
// тогда, когда заводят новую службу: манифест есть, под сторожем его нет, и ни
// одна проверка об этом не говорит. Состав берётся у ИНДЕКСА git: обход диска
// прочитал бы игнорируемые каталоги (рабочие копии, распаковки), и вердикт
// перестал бы быть свойством коммита.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/corelib/modulemanifest"
)

// manifestServicesDir — каталог, под которым лежат службы платформы.
const manifestServicesDir = "services"

// moduleManifestDoc — ровно те поля, о которых этот гейт утверждает. Схему
// документа он не воспроизводит: у неё другой владелец.
type moduleManifestDoc struct {
	APIVersion string `yaml:"apiVersion"`
	Module     string `yaml:"module"`
	Resources  []struct {
		ObjectType string `yaml:"objectType"`
	} `yaml:"resources"`
}

// readableManifest — один прочитанный документ.
type readableManifest struct {
	Rel string
	Doc moduleManifestDoc
}

// auditReadableManifests — предикат четырёх осей. Пусто = норма.
//
// Чистая функция от уже прочитанных документов: её же зовёт инъекция, подавая
// синтетику. Гейт, чью способность падать доказывают правкой живого дерева,
// доказательства не имеет.
func auditReadableManifests(docs []readableManifest) []string {
	var found []string
	byModule := map[string][]string{}
	byType := map[string][]string{}

	for _, d := range docs {
		if strings.TrimSpace(d.Doc.APIVersion) == "" {
			found = append(found, fmt.Sprintf(
				"%s: пустой `apiVersion` — документ не называет адресата и неотличим от "+
					"чужого файла с тем же именем", d.Rel))
		}
		if strings.TrimSpace(d.Doc.Module) == "" {
			found = append(found, fmt.Sprintf(
				"%s: пустой `module` — адресату нечем различить этот документ", d.Rel))
		} else {
			byModule[d.Doc.Module] = append(byModule[d.Doc.Module], d.Rel)
		}
		for i, r := range d.Doc.Resources {
			ot := strings.TrimSpace(r.ObjectType)
			if ot == "" {
				found = append(found, fmt.Sprintf(
					"%s: ресурс №%d без `objectType` — адресовать его нечем", d.Rel, i+1))
				continue
			}
			byType[ot] = append(byType[ot], d.Rel)
		}
	}

	for module, rels := range byModule {
		if len(rels) > 1 {
			sort.Strings(rels)
			found = append(found, fmt.Sprintf(
				"модуль %q объявлен %d документами (%s) — имя модуля есть ключ, по "+
					"которому адресат их различает, и вердикт о них стал функцией порядка обхода",
				module, len(rels), strings.Join(rels, ", ")))
		}
	}
	for ot, rels := range byType {
		if len(rels) > 1 {
			sort.Strings(rels)
			found = append(found, fmt.Sprintf(
				"тип объекта %q объявлен %d документами (%s) — тип есть координата решения "+
					"о доступе, и объявленный дважды он принадлежит тому, кого прочли последним",
				ot, len(rels), strings.Join(rels, ", ")))
		}
	}
	sort.Strings(found)
	return found
}

// TestModuleManifestsOfThePlatformAreReadable — сам гейт.
func TestModuleManifestsOfThePlatformAreReadable(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		parts := strings.Split(rel, "/")
		if len(parts) != 3 || parts[0] != manifestServicesDir || parts[2] != modulemanifest.FileName {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	// Каталогов служб в дереве СТОЛЬКО-ТО, манифестов — столько-то. Обе величины
	// печатаются: одно число скрывает ровно тот случай, ради которого гейт заведён,
	// — службу, чей манифест никем не читается, потому что его нет.
	serviceDirs := map[string]bool{}
	for rel := range tt.files {
		parts := strings.Split(rel, "/")
		if len(parts) >= 2 && parts[0] == manifestServicesDir {
			serviceDirs[parts[1]] = true
		}
	}

	var (
		docs      []readableManifest
		unreadble []string
	)
	for _, rel := range rels {
		// #nosec G304 -- путь из индекса git.
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			unreadble = append(unreadble, fmt.Sprintf("%s: не прочитан: %v", rel, err))
			continue
		}
		var doc moduleManifestDoc
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			unreadble = append(unreadble, fmt.Sprintf("%s: не разобран как YAML: %v", rel, err))
			continue
		}
		docs = append(docs, readableManifest{Rel: rel, Doc: doc})
	}

	types := 0
	for _, d := range docs {
		types += len(d.Doc.Resources)
	}
	t.Logf("перепись: каталогов служб %d, манифестов найдено %d, разобрано %d, "+
		"объявлено ресурсов %d", len(serviceDirs), len(rels), len(docs), types)

	if len(rels) == 0 {
		t.Fatalf("под %s/*/%s не найдено ни одного манифеста при %d каталогах служб — "+
			"обход пуст, и «ноль находок» означало бы «ноль прочитанного»",
			manifestServicesDir, modulemanifest.FileName, len(serviceDirs))
	}
	if len(unreadble) > 0 {
		t.Fatalf("манифест не прочитан — %d находка(и):\n  %s\n\n"+
			"Непрочитанное есть НАХОДКА, а не «модуль ничего не объявил».",
			len(unreadble), strings.Join(unreadble, "\n  "))
	}
	if types == 0 {
		t.Fatalf("на %d манифестах объявлено НОЛЬ ресурсов — разбор перестал видеть "+
			"предмет, и две оси из четырёх молчали бы ни о чём", len(docs))
	}

	found := auditReadableManifests(docs)
	if len(found) > 0 {
		t.Fatalf("манифесты модулей платформы разошлись сами с собой — %d находка(и):\n  %s",
			len(found), strings.Join(found, "\n  "))
	}
}
