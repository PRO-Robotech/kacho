// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_seed_matches_chart_schema_test.go — посев церемонии обязан писать
// признаки личности ПО ТОЙ СХЕМЕ, которую чарт объявляет действующей.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Схема личности и посев — ДВА объявления об одном предмете, и до этой проверки
// они разошлись молча. Посев называл схему `default` — умолчание ПОСТАВЩИКА, —
// и слал признак `name`, которого в нашей схеме нет. Пока конфигурацию службы
// личности никто не монтировал, действовало умолчание поставщика, и посев
// проходил. Как только конфигурация доехала до процесса (#904), схемой стала
// наша: заведение личности начало отвечать отказом, церемония вставала на
// четвёртой стадии, и НИ ОДНА коллекция набора не запускалась.
//
// Это не «плохая фикстура»: это класс «два места об одном предмете, из которых
// верно одно». Чинить его правкой одной строки — значит оставить механизм,
// который разведёт их снова при следующей смене схемы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — ТРИ ОСИ, А НЕ ОДНА
//
//  1. имя схемы в посеве совпадает с тем, что чарт объявляет умолчанием;
//  2. каждый признак, который шлёт посев, схема ЗНАЕТ (она строгая:
//     `additionalProperties: false` отвергает запрос целиком, а не молча
//     выбрасывает лишнее);
//  3. каждый ОБЯЗАТЕЛЬНЫЙ признак схемы посев шлёт.
//
// Одной первой оси мало: имя схемы могло совпасть, а состав признаков — нет,
// и отказ выглядел бы точно так же.
//
// ПОЧЕМУ БЕЗ helm. Проверка читает ОБЪЯВЛЕНИЕ, а не рендер: рендер требует
// скачанных зависимостей и потому пропускается там, где их нет, — а
// пропущенная проверка не краснеет никогда.
package deploy_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// Схема и умолчание объявлены в ОДНОМ файле — там же, откуда рендерится
	// содержимое обеих карт настроек. Карты-обёртки сами по себе ничего не
	// объявляют: они зовут именованный шаблон, чтобы отпечаток содержимого и
	// сама карта считались по одному тексту (см. _kratos-identity.tpl).
	identitySchemaTemplate = "helm/umbrella/charts/kaname/templates/_kratos-identity.tpl"
	identityConfigTemplate = "helm/umbrella/charts/kaname/templates/_kratos-identity.tpl"
	// Посев церемонии — единственный производитель личности через админский API
	// (предикат: `grep -rn 'admin/identities' --include=*.py --include=*.sh .`).
	ceremonySeed = "../tests/authz-fixtures/prodseed_ceremony.py"
	// Именованный шаблон, рендерящий схему.
	identitySchemaTemplateName = "kacho.identity.schemaJSON"
)

// templateExpr — подстановка шаблона. В теле схемы она стоит только в `$id`,
// который на состав признаков не влияет; чтобы разобрать схему как JSON, её
// достаточно заменить на строковую заглушку.
var templateExpr = regexp.MustCompile(`\{\{[^}]*\}\}`)

var (
	defaultSchemaIDDecl = regexp.MustCompile(`(?m)^\s*default_schema_id:\s*(\S+)\s*$`)
	seedSchemaIDDecl    = regexp.MustCompile(`(?m)^IDENTITY_SCHEMA_ID\s*=\s*"([^"]+)"`)
)

// defineBody возвращает тело именованного шаблона — то, что стоит между
// `{{- define "<имя>" -}}` и его `{{- end -}}`. Читать надо ИМЕННО его: карта
// настроек содержимого больше не несёт, она зовёт этот шаблон, чтобы карта и
// отпечаток её содержимого считались по ОДНОМУ тексту.
func defineBody(src, name string) string {
	head := `{{- define "` + name + `" -}}`
	at := strings.Index(src, head)
	if at < 0 {
		return ""
	}
	rest := src[at+len(head):]
	end := strings.Index(rest, "\n{{- end -}}")
	if end < 0 {
		return ""
	}
	return strings.TrimPrefix(rest[:end], "\n")
}

// balancedBraces возвращает содержимое фигурных скобок, открывающихся после
// `marker`. Счёт скобок, а не `[^}]*`: вложенный объект (именно так выглядел
// отвергаемый признак `name`) оборвал бы нежадное выражение на первой же
// закрывающей скобке, и разошедшийся посев прочитался бы как согласный.
func balancedBraces(body, marker string) string {
	at := strings.Index(body, marker)
	if at < 0 {
		return ""
	}
	rest := body[at+len(marker):]
	open := strings.Index(rest, "{")
	if open < 0 {
		return ""
	}
	depth := 0
	for i := open; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[open+1 : i]
			}
		}
	}
	return ""
}

// topLevelKeys вынимает имена ключей ВЕРХНЕГО уровня объекта. Вложенные ключи
// сюда не попадают — отвергает схема именно верхний уровень.
func topLevelKeys(obj string) []string {
	var out []string
	depth := 0
	for i := 0; i < len(obj); i++ {
		switch obj[i] {
		case '{', '[':
			depth++
			continue
		case '}', ']':
			depth--
			continue
		case '"':
			end := strings.IndexByte(obj[i+1:], '"')
			if end < 0 {
				return out
			}
			name := obj[i+1 : i+1+end]
			i += end + 1
			if depth == 0 {
				rest := strings.TrimLeft(obj[i+1:], " \t\n")
				if strings.HasPrefix(rest, ":") {
					out = append(out, name)
				}
			}
		}
	}
	return out
}

func TestCeremonySeedWritesTraitsOfTheChartsIdentitySchema(t *testing.T) {
	schemaBody, err := os.ReadFile(filepath.Clean(identitySchemaTemplate)) // #nosec G304 -- путь — константа собственного дерева
	if err != nil {
		t.Fatalf("чтение объявления схемы личности %s: %v", identitySchemaTemplate, err)
	}
	configBody, err := os.ReadFile(filepath.Clean(identityConfigTemplate)) // #nosec G304 -- путь — константа собственного дерева
	if err != nil {
		t.Fatalf("чтение объявления настроек службы личности %s: %v", identityConfigTemplate, err)
	}
	seedBody, err := os.ReadFile(filepath.Clean(ceremonySeed)) // #nosec G304 -- путь — константа собственного дерева
	if err != nil {
		t.Fatalf("чтение посева церемонии %s: %v", ceremonySeed, err)
	}

	// ── ось 1: имя схемы ────────────────────────────────────────────────────
	chartID := ""
	if m := defaultSchemaIDDecl.FindStringSubmatch(string(configBody)); m != nil {
		chartID = strings.Trim(m[1], `"'`)
	}
	if chartID == "" {
		t.Fatalf("в %s не найдено объявление `default_schema_id` — "+
			"проверять согласие не с чем; «ноль находок» здесь неотличимо "+
			"от «ноль прочитанного», поэтому это отказ", filepath.Base(identityConfigTemplate))
	}
	seedID := ""
	if m := seedSchemaIDDecl.FindStringSubmatch(string(seedBody)); m != nil {
		seedID = m[1]
	}
	if seedID == "" {
		t.Fatalf("в %s не найдено объявление IDENTITY_SCHEMA_ID — имя схемы, "+
			"по которой пишутся признаки, обязано быть названо ОДИН раз рядом "+
			"с ними, иначе его смену не с чем сверить", filepath.Base(ceremonySeed))
	}
	if seedID != chartID {
		t.Errorf("схема личности названа по-разному: чарт объявляет умолчанием %q, "+
			"посев пишет признаки по %q. Заведение личности ответит отказом "+
			"«Unable to find JSON Schema ID», церемония встанет и НИ ОДНА "+
			"коллекция набора не запустится", chartID, seedID)
	}

	// ── оси 2 и 3: состав признаков ─────────────────────────────────────────
	raw := defineBody(string(schemaBody), identitySchemaTemplateName)
	if raw == "" {
		t.Fatalf("в %s не найдено тело именованного шаблона %q — состав признаков "+
			"прочитать нечем", filepath.Base(identitySchemaTemplate), identitySchemaTemplateName)
	}
	var schema struct {
		Properties struct {
			Traits struct {
				Properties           map[string]json.RawMessage `json:"properties"`
				Required             []string                   `json:"required"`
				AdditionalProperties *bool                      `json:"additionalProperties"`
			} `json:"traits"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(templateExpr.ReplaceAllString(raw, "x")), &schema); err != nil {
		t.Fatalf("схема личности не разбирается как JSON: %v", err)
	}
	known := schema.Properties.Traits.Properties
	if len(known) == 0 {
		t.Fatalf("схема личности не объявляет НИ ОДНОГО признака — разбор слеп, " +
			"и любое утверждение о согласии было бы вакуумным")
	}

	seedTraits := topLevelKeys(balancedBraces(string(seedBody), `"traits":`))
	if len(seedTraits) == 0 {
		t.Fatalf("в %s не разобран состав признаков — сверять нечего", filepath.Base(ceremonySeed))
	}
	sort.Strings(seedTraits)

	strict := schema.Properties.Traits.AdditionalProperties != nil && !*schema.Properties.Traits.AdditionalProperties
	for _, tr := range seedTraits {
		if _, ok := known[tr]; ok {
			continue
		}
		if strict {
			t.Errorf("посев шлёт признак %q, которого схема %q НЕ знает, а она строгая "+
				"(`additionalProperties: false`) — запрос отвергается ЦЕЛИКОМ, "+
				"а не очищается от лишнего", tr, chartID)
			continue
		}
		t.Errorf("посев шлёт признак %q, которого схема %q не объявляет", tr, chartID)
	}
	for _, req := range schema.Properties.Traits.Required {
		if !contains(seedTraits, req) {
			t.Errorf("схема %q требует признак %q, а посев его не шлёт — "+
				"личность не заведётся", chartID, req)
		}
	}

	names := make([]string, 0, len(known))
	for k := range known {
		names = append(names, k)
	}
	sort.Strings(names)
	t.Logf("осмотрено: схема %q, признаков объявлено %d (%s), строгая=%v, обязательных %d; "+
		"посев шлёт %d (%s)", chartID, len(names), strings.Join(names, ", "), strict,
		len(schema.Properties.Traits.Required), len(seedTraits), strings.Join(seedTraits, ", "))
}
