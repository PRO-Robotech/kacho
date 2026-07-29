// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// hideexistence_test.go — гейт против расхождения таблиц скрытия существования с
// тем, что вокруг них осталось.
//
// Когда чтение отвергается на объекте, о котором вызывающему знать не положено,
// обе стороны отвечают «не найдено» — ТЕКСТОМ владельца. Защита ровно настолько
// хороша, насколько текст совпадает: различимая формулировка отделяет «не твоё»
// от «нет такого», то есть ровно тот оракул, ради закрытия которого всё и
// написано.
//
// Существующая проверка рядом идёт в ОДНУ сторону: каждый достижимый по каталогу
// тип обязан быть в таблице. Обратной не было — и таблицы пережили удаление
// целого семейства ресурсов: три типа объектов блочного хранения compute остались
// в обеих копиях после того, как дубль был снят, а комментарий «Источники»
// продолжал ссылаться на файлы репозитория, которых в дереве нет. Мёртвая строка
// не отвергает запросов, но она — заявление о продукте, и оно было ложным.
package repohygiene

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// hideExistenceTables — обе копии таблицы. Ни одна не может импортировать другую
// (одна живёт под internal/ шлюза), поэтому разбор идёт по исходнику.
var hideExistenceTables = []string{
	"pkg/authz/hide_existence.go",
	"gateway/internal/middleware/permission_denied_response.go",
}

// hideExistenceVarName — имя карты в обоих файлах. Часть предпосылки гейта.
const hideExistenceVarName = "hideExistenceNotFoundFormats"

// composedNotFoundFormats — форматы, которых в исходнике владельца нет ЦЕЛИКОМ,
// потому что имя ресурса подставляется на месте вызова.
//
// registry отвечает через общий отображатель ошибок: `"%w: %s %s not found"` с
// именем ресурса аргументом (`wrapPgErr(err, "Registry", id)`). Собранный текст
// побайтово тот же, но строкой в дереве не лежит, поэтому поиск по исходнику его
// не найдёт — и объявить это расхождением значило бы падать на законной форме.
// Запись живёт, пока владелец собирает текст именно так.
var composedNotFoundFormats = map[string]string{
	"registry_registry": "собирается в services/registry/internal/repo/kacho/pg/errmap.go " +
		"из шаблона `%w: %s %s not found` и имени ресурса, переданного вызовом",
}

// TestHideExistenceTablesCarryNoUnreachableType — строка таблицы, до которой не
// доводит ни одна запись каталога, — находка.
//
// Что делать, если гейт сработал, — ровно два исхода, третьего нет:
//
//  1. ресурс снят с контракта -> удалить строку из ОБЕИХ копий (и из
//     соответствующего кейса рядом с ними);
//  2. ресурс жив, но каталог его больше не якорит -> это отдельный дефект: RPC
//     ресурса перестал быть пообъектным, и скрытие существования для него не
//     работает вовсе. Чинить в каталоге, а не здесь.
//
// «Оставить как есть» исходом не является: мёртвая строка утверждает, что
// платформа отвечает текстом владельца, которого больше нет.
func TestHideExistenceTablesCarryNoUnreachableType(t *testing.T) {
	root := repoRoot(t)
	reachable := catalogScopeObjectTypes(t, root)

	for _, rel := range hideExistenceTables {
		formats := parseHideExistenceTable(t, root, rel)
		var dead []string
		for objType := range formats {
			if !reachable[objType] {
				dead = append(dead, objType)
			}
		}
		sort.Strings(dead)
		for _, objType := range dead {
			t.Errorf("%s: тип объекта %q не якорит ни одна запись каталога — до этой строки "+
				"дойти нельзя:\n  %s\n\n"+
				"Исходы: удалить строку из обеих копий, если ресурс снят с контракта / починить "+
				"каталог, если ресурс жив, но перестал быть пообъектным (тогда скрытие "+
				"существования для него не работает вовсе).",
				rel, objType, formats[objType])
		}
	}
}

// TestHideExistenceFormatsMatchTheOwningServiceSource — текст отказа обязан быть
// текстом владельца, а не похожим на него.
//
// Проверяется по ДЕРЕВУ сервисов, а не по комментарию «Источники» рядом с
// таблицей: комментарий пережил удаление файлов, на которые ссылался, и ничего об
// этом не сказало.
func TestHideExistenceFormatsMatchTheOwningServiceSource(t *testing.T) {
	root := repoRoot(t)
	sources := serviceSourceCorpus(t, root)
	if len(sources) == 0 {
		t.Fatal("обход не прочитал ни одного не-тестового исходника сервисов — гейт ниже " +
			"объявил бы несовпадающими ВСЕ форматы, что было бы неверно")
	}

	var matched, exempted int
	for _, rel := range hideExistenceTables {
		for objType, format := range parseHideExistenceTable(t, root, rel) {
			if strings.Contains(sources, format) {
				matched++
				continue
			}
			if why, ok := composedNotFoundFormats[objType]; ok {
				exempted++
				_ = why
				continue
			}
			t.Errorf("%s: формат для %q не встречается ни в одном не-тестовом исходнике "+
				"сервисов:\n  %q\n\n"+
				"Отказ обязан быть побайтово тем же, что настоящий промах владельца — иначе "+
				"текст различает «не твоё» и «нет такого». Исходы: привести формат к тому, что "+
				"производит владелец / внести в composedNotFoundFormats, если владелец собирает "+
				"текст из шаблона и имени ресурса.", rel, objType, format)
		}
	}
	if matched == 0 {
		t.Fatal("ни один формат не найден в дереве — предпосылка поиска не держится")
	}

	// Исключение живёт, пока у него есть предмет.
	for objType := range composedNotFoundFormats {
		var seen bool
		for _, rel := range hideExistenceTables {
			if _, ok := parseHideExistenceTable(t, root, rel)[objType]; ok {
				seen = true
			}
		}
		if !seen {
			t.Errorf("запись composedNotFoundFormats пережила свой предмет — типа %q в таблицах "+
				"больше нет. Удали её, иначе исключение станет слепой зоной.", objType)
		}
	}
}

// catalogScopeObjectTypes — множество типов объектов, которые каталог якорит хотя
// бы одной записью.
func catalogScopeObjectTypes(t *testing.T, root string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, catalogEmbedPath))
	if err != nil {
		t.Fatalf("не прочитан вшитый каталог %s: %v", catalogEmbedPath, err)
	}
	var rows []struct {
		ScopeExtractor struct {
			ObjectType string `json:"object_type"`
		} `json:"scope_extractor"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("разбор каталога: %v", err)
	}
	out := map[string]bool{}
	for _, r := range rows {
		if r.ScopeExtractor.ObjectType != "" {
			out[r.ScopeExtractor.ObjectType] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("каталог не якорит ни одного типа объекта — гейты выше беспредметны")
	}
	return out
}

// parseHideExistenceTable читает карту из исходника по дереву разбора: та же
// строка встречается в прозе рядом (комментарии объясняют выбор текста), и
// текстовый поиск зачёл бы объяснение за объявление.
func parseHideExistenceTable(t *testing.T, root, rel string) map[string]string {
	t.Helper()
	path := filepath.Join(root, rel)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("таблица скрытия существования не там, где её ждёт гейт (%s): %v — перенеси "+
			"гейт вместе с ней, а не удаляй", rel, err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}
	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != hideExistenceVarName || len(vs.Values) != 1 {
			return true
		}
		lit, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok {
			return false
		}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			k, kok := kv.Key.(*ast.BasicLit)
			v, vok := kv.Value.(*ast.BasicLit)
			if !kok || !vok {
				continue
			}
			key, kerr := strconv.Unquote(k.Value)
			val, verr := strconv.Unquote(v.Value)
			if kerr == nil && verr == nil {
				out[key] = val
			}
		}
		return false
	})
	if len(out) == 0 {
		t.Fatalf("%s: карта %s не разобрана — либо переименована, либо изменила форму. "+
			"Гейты в этом файле в таком состоянии не утверждают ничего.", rel, hideExistenceVarName)
	}
	return out
}

// serviceSourceCorpus — конкатенация не-тестовых Go-исходников сервисов.
// Тесты исключены намеренно: их «ожидаемый текст» — копия того же утверждения, а
// не его источник, поэтому совпадение с ними ничего бы не доказывало.
func serviceSourceCorpus(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(filepath.Join(root, "services"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if slices.Contains([]string{"node_modules", "vendor", "testdata"}, info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, rerr := os.ReadFile(path) // #nosec G304 -- пути внутри дерева этого модуля
		if rerr != nil {
			return rerr
		}
		b.Write(raw)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("обход services/: %v", err)
	}
	return b.String()
}
