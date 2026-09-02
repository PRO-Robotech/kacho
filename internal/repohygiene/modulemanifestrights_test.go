// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modulemanifestrights_test.go — манифест домена не вправе быть ВТОРЫМ
// объявлением права (задача PRO-Robotech/kacho#1813; приёмка
// services/iam/docs/engineering/acceptance/module-manifest-resources-roles-deprecated.md
// §2.3, §2.8; сценарии MOD-MR-19, 23, 24, 24а, 25, 26).
//
// # Почему это заводится ВМЕСТЕ с разделом `resources`, а не преемником
//
// Гейт TestNoServiceDeclaresItsPermissionsASecondTime обходит только не-тестовые
// файлы Go и YAML не читает НИ ПРИ КАКОМ УСЛОВИИ. Пока раздела `resources` не
// было, он не лгал — искать было нечего. Он начал бы лгать в тот момент, когда
// раздел заведён, и молчание выглядело бы исправной работой: право, объявленное
// в YAML, не встретило бы НИ ОДНОЙ проверки.
//
// # Что этот гейт читает и чем он НЕ является
//
// Он читает из манифеста ровно три вещи — модуль, пару `objectType`/`producer`
// каждого ресурса и ключи раздела `deprecatedVerbs`, — и вторым судьёй ФОРМЫ не
// является: форму судит ОДИН исполнитель, разбор в Go-структуры
// `services/iam/internal/manifest` плюс `KnownFields(true)`. Здесь судится
// СОГЛАСИЕ манифеста С ДЕРЕВОМ, и предмет у этих двух проверок разный.
//
// Читать манифест настоящим загрузчиком отсюда НЕЛЬЗЯ: `services/iam/internal/…`
// закрыт правилом видимости Go для пакета корня репозитория. Отступление от §6
// приёмки, называющей местом гейта этот пакет, названо вместе с причиной — она
// свойство языка, а не выбор.
//
// # Чего гейт НЕ проверяет, и это сказано прямо
//
// Он НЕ сверяет глаголы манифеста с глаголами каталога имя в имя. Написание
// действия в манифесте есть контракт ГЕНЕРАТОРА (#1092), а генератора сегодня
// нет: черновик воркспейса переименовывает действия осознанно (`set_default_sg`
// → `setDefaultSecurityGroup`), и сверка по имени дала бы находку на КАЖДОМ
// глаголе законного манифеста. Ось, на которой соединение определено уже
// сегодня, — ЯКОРЬ ОБЛАСТИ: тип объекта, который манифест называет дословно и
// который каталог несёт в `scope_extractor.object_type`. Оставшаяся половина —
// пословная сверка — заводится вместе с генератором.
package repohygiene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	// manifestBaseName — имя, под которым модуль кладёт свой манифест. То же
	// самое имя знает проверка дерева iam; второе место, называющее его,
	// разошлось бы с первым молча, и часть манифестов перестала бы находиться,
	// не дав ни одной находки. Здесь оно повторено потому, что первое лежит за
	// границей видимости Go, — и это названо, а не умолчано.
	manifestBaseName = "manifest.yaml"
	// catalogRelPath — каталог прав, ПОРОЖДЁННЫЙ из аннотаций контрактов.
	// Читается ОТ КОРНЯ ДЕРЕВА, а не от корня репозитория: иначе синтетическое
	// дерево, на котором доказывается способность гейта упасть, читало бы
	// каталог продукта.
	catalogRelPath = "gateway/internal/middleware/embed/permission_catalog.json"
	// modelRelPath — канонический текст модели прав.
	modelRelPath = "proto/kacho/cloud/iam/v1/fga_model.fga"
	// migrationsRelDir — применённые миграции iam: единственный источник
	// системных ролей, а значит и правил, в которых живут устаревшие глаголы.
	migrationsRelDir = "services/iam/internal/migrations"
)

// manifestRights — то немногое, что гейт читает из манифеста.
//
// Строгости `KnownFields` здесь НАМЕРЕННО нет: неизвестный ключ — предмет
// загрузчика, а не этого гейта, и вторая проверка того же предмета разошлась бы
// с первой молча.
type manifestRights struct {
	Module    string `yaml:"module"`
	Resources []struct {
		Name       string `yaml:"name"`
		ObjectType string `yaml:"objectType"`
		Producer   string `yaml:"producer"`
	} `yaml:"resources"`
	DeprecatedVerbs map[string]struct {
		Class string `yaml:"class"`
	} `yaml:"deprecatedVerbs"`
}

// manifestRightsFile — один прочитанный манифест вместе с его координатой.
type manifestRightsFile struct {
	Rel    string
	Rights manifestRights
}

// treeRights — всё, что гейт прочитал о дереве, и ОБЪЁМ прочитанного.
//
// Перепись печатается ПО КАЖДОЙ форме отдельно: одно число скрыло бы ровно тот
// случай, ради которого гейт заведён — «манифестов ноль» неотличимо от
// «манифесты прочитаны и в них ничего нет».
type treeRights struct {
	Manifests []manifestRightsFile
	// AnchoredTypes — типы объектов, на которых каталог якорит хоть одну
	// строку: то есть типы, о которых аннотации ГОВОРЯТ.
	AnchoredTypes map[string]int
	// VerbsByModule — производимые действия по модулям, нормализованные.
	VerbsByModule map[string]map[string]bool
	// ModelTypes — типы, объявленные каноническим текстом модели.
	ModelTypes map[string]bool
	// RoleRuleVerbs — действия, названные правилами ролей применённых миграций.
	RoleRuleVerbs map[string]int
	// Объёмы прочитанного, каждый своим числом.
	CatalogRows, ModelTypeCount, MigrationFiles int
}

// normalizeVerb — приведение написаний к одному виду.
//
// Каталог пишет действие в змеином регистре (`get_internal`), манифест — в
// верблюжьем (`internalGet`), и сравнение БЕЗ приведения не совпало бы НИ РАЗУ,
// молча отняв право у живой записи. Приведение снимает различие РЕГИСТРА и
// РАЗДЕЛИТЕЛЯ и НЕ снимает различия порядка слов — второе есть предмет
// генератора (#1092), и притворяться, будто гейт его судит, нельзя.
func normalizeVerb(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		if r == '_' || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// readTreeRights — состав дерева глазами этого гейта.
func readTreeRights(t *testing.T, tt *trackedTree) treeRights {
	t.Helper()
	out := treeRights{
		AnchoredTypes: map[string]int{},
		VerbsByModule: map[string]map[string]bool{},
		ModelTypes:    map[string]bool{},
		RoleRuleVerbs: map[string]int{},
	}

	rels := make([]string, 0, tt.count())
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		if filepath.Base(rel) != manifestBaseName {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tt.root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("манифест %s не прочитан: %v — «ноль находок» означало бы "+
				"«ноль прочитанного»", rel, err)
		}
		var rights manifestRights
		if err := yaml.Unmarshal(body, &rights); err != nil {
			// Форму судит загрузчик и его цель сборки; здесь неразбираемый
			// манифест — не находка этого гейта, а его СЛЕПАЯ ЗОНА, и она
			// обязана быть названа, а не пропущена.
			t.Errorf("манифест %s не разбирается (%v) — этот гейт о нём не "+
				"утверждает ничего; форму судит `make -C services/iam module-manifest-check`",
				rel, err)
			continue
		}
		out.Manifests = append(out.Manifests, manifestRightsFile{Rel: rel, Rights: rights})
	}

	if raw, err := os.ReadFile(filepath.Join(tt.root, filepath.FromSlash(catalogRelPath))); err == nil {
		var rows []struct {
			Permission     string `json:"permission"`
			ScopeExtractor *struct {
				ObjectType string `json:"object_type"`
			} `json:"scope_extractor"`
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatalf("каталог прав %s не разбирается: %v", catalogRelPath, err)
		}
		out.CatalogRows = len(rows)
		for _, row := range rows {
			if row.ScopeExtractor != nil && row.ScopeExtractor.ObjectType != "" {
				out.AnchoredTypes[row.ScopeExtractor.ObjectType]++
			}
			parts := strings.Split(row.Permission, ".")
			if len(parts) < 3 || strings.HasPrefix(row.Permission, "<") {
				continue
			}
			module := parts[0]
			if out.VerbsByModule[module] == nil {
				out.VerbsByModule[module] = map[string]bool{}
			}
			out.VerbsByModule[module][normalizeVerb(parts[len(parts)-1])] = true
		}
	}

	if raw, err := os.ReadFile(filepath.Join(tt.root, filepath.FromSlash(modelRelPath))); err == nil {
		for _, m := range regexp.MustCompile(`(?m)^type ([a-z_0-9]+)`).FindAllStringSubmatch(string(raw), -1) {
			out.ModelTypes[m[1]] = true
		}
		out.ModelTypeCount = len(out.ModelTypes)
	}

	verbsInRules := regexp.MustCompile(`"verbs"\s*:\s*\[([^\]]*)\]`)
	quoted := regexp.MustCompile(`"([^"]+)"`)
	entries, _ := os.ReadDir(filepath.Join(tt.root, filepath.FromSlash(migrationsRelDir)))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tt.root, filepath.FromSlash(migrationsRelDir), e.Name()))
		if err != nil {
			continue
		}
		out.MigrationFiles++
		for _, block := range verbsInRules.FindAllStringSubmatch(string(body), -1) {
			for _, v := range quoted.FindAllStringSubmatch(block[1], -1) {
				out.RoleRuleVerbs[normalizeVerb(v[1])]++
			}
		}
	}
	return out
}

// manifestRightFinding — одна находка с координатой.
type manifestRightFinding struct {
	Rel    string
	Detail string
}

// findManifestRightFaults — согласие манифестов с деревом.
//
// Четыре оси, и у каждой ОБЕ стороны: находка и законный близнец. Односторонняя
// проверка зеленела бы на дереве, где всё сломано одинаково.
func findManifestRightFaults(tr treeRights) []manifestRightFinding {
	var out []manifestRightFinding
	for _, mf := range tr.Manifests {
		for i, r := range mf.Rights.Resources {
			anchored := tr.AnchoredTypes[r.ObjectType]
			switch {
			case r.ObjectType != "" && len(tr.ModelTypes) > 0 && !tr.ModelTypes[r.ObjectType]:
				out = append(out, manifestRightFinding{mf.Rel, "resources[" + itoa(i) + "] (" + r.Name +
					"): тип объекта " + r.ObjectType + " не объявлен каноническим текстом модели прав — " +
					"манифест объявляет право на тип, которого не существует"})
			case r.Producer == "derived" && anchored == 0 && tr.CatalogRows > 0:
				out = append(out, manifestRightFinding{mf.Rel, "resources[" + itoa(i) + "] (" + r.Name +
					"): помечен `producer: derived`, но аннотации не якорят на типе " + r.ObjectType +
					" НИ ОДНОЙ строки каталога — порождать его не из чего, и запись есть ВТОРОЕ " +
					"объявление права. Исходов два: пометить `producer: authored` и назвать причину " +
					"либо снять запись"})
			case r.Producer == "authored" && anchored > 0:
				out = append(out, manifestRightFinding{mf.Rel, "resources[" + itoa(i) + "] (" + r.Name +
					"): помечен `producer: authored`, а аннотации якорят на типе " + r.ObjectType +
					" строк каталога: " + itoa(anchored) + " — пометка пережила свой предмет, и " +
					"перегенерация обойдёт запись стороной"})
			}
		}
		for _, verb := range sortedKeysOfDeprecated(mf.Rights.DeprecatedVerbs) {
			norm := normalizeVerb(verb)
			if tr.VerbsByModule[mf.Rights.Module][norm] {
				out = append(out, manifestRightFinding{mf.Rel, "deprecatedVerbs." + verb +
					": глагол объявлен устаревшим, а каталог его ПРОИЗВОДИТ — запись лжёт, " +
					"и снимать её надо вместе с предметом"})
			}
			if tr.MigrationFiles > 0 && tr.RoleRuleVerbs[norm] == 0 {
				out = append(out, manifestRightFinding{mf.Rel, "deprecatedVerbs." + verb +
					": глагол не назван НИ ОДНИМ правилом роли применённых миграций — у записи " +
					"не осталось предмета, и без этого зеркала послабление не истекло бы никогда"})
			}
		}
	}
	return out
}

func sortedKeysOfDeprecated(m map[string]struct {
	Class string `yaml:"class"`
}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestManifestIsNotASecondDeclarationOfARight — гейт устарелости порождённого
// (MOD-MR-19, 25, 26) на дереве продукта.
//
// Сегодня манифестов в дереве НОЛЬ, и это ТРЕТЬЯ КАТЕГОРИЯ: «не с чем сверять»,
// а не «находок ноль». Перепись говорит об этом отдельной строкой, потому что
// молчание по отсутствию предмета неотличимо от молчания по правильности.
// Способность гейта упасть доказывается на СИНТЕТИЧЕСКОМ дереве, где предмет
// есть by construction (modulemanifestrights_injection_test.go).
func TestManifestIsNotASecondDeclarationOfARight(t *testing.T) {
	root := repoRoot(t)
	tr := readTreeRights(t, newTrackedTree(t, root))

	if tr.CatalogRows == 0 {
		t.Fatalf("каталог прав не прочитан (%s) — сверять манифест не с чем, "+
			"и «находок ноль» было бы свойством обхода, а не дерева", catalogRelPath)
	}
	if len(tr.ModelTypes) == 0 {
		t.Fatalf("канонический текст модели не прочитан (%s) — то же самое", modelRelPath)
	}

	for _, f := range findManifestRightFaults(tr) {
		t.Errorf("%s: %s", f.Rel, f.Detail)
	}

	t.Logf("перепись: манифестов %d · строк каталога %d · типов модели %d · "+
		"файлов миграций %d · действий в правилах ролей %d",
		len(tr.Manifests), tr.CatalogRows, tr.ModelTypeCount, tr.MigrationFiles, len(tr.RoleRuleVerbs))
	if len(tr.Manifests) == 0 {
		t.Logf("манифестов НОЛЬ — половина гейта, читающая YAML, предмета не имела: " +
			"это третья категория («не с чем сверять»), и в «находок ноль» она НЕ засчитывается")
	}
}
