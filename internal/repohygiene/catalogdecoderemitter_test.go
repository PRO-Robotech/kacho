// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogdecoderemitter_test.go — гейт: поле декодера каталога разрешений
// обязано иметь ЭМИТЕНТА.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Каталог разрешений описан ДВАЖДЫ и в двух местах дерева:
//
//	эмитент  gateway/cmd/protoc-gen-kacho-permissions/main.go   — что ПИШЕТСЯ в JSON
//	декодер  gateway/internal/middleware/permission_catalog.go  — что ЧИТАЕТСЯ из него
//
// Два места об одном предмете расходятся молча, и здесь — особенно тихо:
// JSON-декодер пропускает неизвестный ключ без ошибки, а поле, которого не
// эмитит никто, остаётся нулевым при ЛЮБОМ входе. Ни сборка, ни прогон, ни
// обзор диффа этого не видят: структура компилируется, запись разбирается,
// значение просто всегда пустое.
//
// Со стороны такое поле читается как несомая каталогом возможность — и по ней
// строят планы. Именно так и вышло: задача #2569 была заведена как «окно
// свежести аутентификации объявлено и не читается ни одним звеном», а замер
// показал, что `requires_mfa_fresh` — ОДИН ИЗ СЕМИ полей того же класса, и
// провязать его было нельзя ни при каком входе: опции, которой его выставляют,
// в словаре разметки не существует вовсе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАМЕР НА МОМЕНТ ЗАВЕДЕНИЯ (перемеряй прогоном, а не памятью)
//
// ЕДИНИЦА СЧЁТА НАЗВАНА, потому что их две и они дают разные числа: перепись
// гейта считает теги ПО ВСЕМ парным типам (`CatalogEntry` плюс вложенный
// `ScopeExtractor`), а разбор дефекта ниже — теги ОДНОГО типа `CatalogEntry`.
// Читатель, взявший не ту, не сойдётся с прогоном и решит, что одно из чисел
// устарело.
//
//	по всем парным типам, до правки   эмитент 11 · декодер 17 · без эмитента 7
//	по всем парным типам, после       эмитент 11 · декодер 10 · без эмитента 0
//	по типу CatalogEntry, до правки   эмитент  8 · декодер 14
//
// Семь без эмитента: domain · resource_type · action · requires_mfa_fresh ·
// risk_level · description · emitted_build_sha. Ни одного из них не было ни в
// одной из 338 записей вшитого каталога. Шесть из семи не читались прод-кодом
// вовсе; седьмое (`risk_level`) читалось журналом отказа и писало туда ПУСТУЮ
// строку на каждом отказе за всю жизнь процесса.
//
// Первая строка таблицы — не память: она получена ИНЪЕКЦИЕЙ настоящего дефекта
// (`git show <ревизия до правки>:<декодер>` на место декодера), и гейт на ней
// краснеет, называя все семь тегов по именам.
//
// Их godoc утверждал о продукте неправду по трём разным поводам: «Filled by
// plugin during emission» (плагин такого поля не имеет), «used by audit to
// detect catalog-vs-binary drift» (такого аудита нет), «Defaults follow
// RiskLevel: LOW/MEDIUM → false, HIGH/CRITICAL → true» (правила нет ни строкой,
// а его вход отсутствует в каждой записи).
//
// ─────────────────────────────────────────────────────────────────────────────
// НАПРАВЛЕНИЕ У ГЕЙТА ОДНО, И ЭТО РЕШЕНИЕ
//
// Находка — тег ДЕКОДЕРА, которого нет у эмитента. Обратное — тег эмитента,
// которого нет у декодера, — законно и молчит: декодер читает то, на чём
// принимает решения, а не всё подряд. В дереве такой тег есть прямо сейчас
// (`exempt_reason`), и он положительный контроль этого решения: гейт, судящий
// обе стороны, краснел бы на нём — то есть на верном коде, — и его отключили бы
// первым.
//
// ПЕРЕЧЕНЬ СВЕРЯЕМЫХ ТИПОВ ВЫВОДИТСЯ, а не выписывается: сверяется каждое имя
// типа, объявленное в ОБОИХ файлах. Рукописный список разошёлся бы с деревом
// молча — ровно тот класс, который гейт и ловит.
package repohygiene_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Координаты двух мест об одном предмете. Переедет любое — гейт скажет, что
// файла нет, а не промолчит.
const (
	catalogEmitterPath = "gateway/cmd/protoc-gen-kacho-permissions/main.go"
	catalogDecoderPath = "gateway/internal/middleware/permission_catalog.go"
)

// catalogStructTags — ПРЕДИКАТ гейта, отделённый от источника входа: имя типа →
// имена json-тегов его полей.
//
// Отделён затем, чтобы инъекция гоняла ТУ ЖЕ функцию, что судит дерево. Проба,
// доказывающая способность падать на своей копии предиката, доказывает свойство
// копии.
//
// Судит по УЗЛУ РАЗБОРА, а не по тексту: имя поля и его тег встречаются и в
// комментариях (шапка декодера объясняет этот самый класс и перечисляет снятые
// теги по именам), поэтому поиск по подстроке краснел бы на собственном
// объяснении. Поле без тега `json` в перечень не попадает — оно на wire не
// выходит; тег `json:"-"` не попадает по той же причине.
func catalogStructTags(src []byte) (map[string][]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "src.go", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		tags := []string{}
		for _, f := range st.Fields.List {
			if f.Tag == nil {
				continue
			}
			raw, err := strconv.Unquote(f.Tag.Value)
			if err != nil {
				continue
			}
			name := strings.Split(reflect.StructTag(raw).Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			tags = append(tags, name)
		}
		if len(tags) > 0 {
			sort.Strings(tags)
			out[ts.Name.Name] = tags
		}
		return true
	})
	return out, nil
}

// orphanTags — теги декодера, которых эмитент не производит. Возвращает находки
// по каждому парному типу и ЧИСЛО осмотренных пар: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
func orphanTags(emitter, decoder map[string][]string) (orphans map[string][]string, pairs []string) {
	orphans = map[string][]string{}
	for typeName, decTags := range decoder {
		emTags, paired := emitter[typeName]
		if !paired {
			// Тип есть только у декодера — он не парный, и об эмитенте его
			// полей этот гейт не высказывается. Свой предмет у такого типа
			// может быть иной (внутренняя форма, не приезжающая из каталога).
			continue
		}
		pairs = append(pairs, typeName)
		known := map[string]struct{}{}
		for _, t := range emTags {
			known[t] = struct{}{}
		}
		for _, t := range decTags {
			if _, ok := known[t]; !ok {
				orphans[typeName] = append(orphans[typeName], t)
			}
		}
	}
	sort.Strings(pairs)
	return orphans, pairs
}

// TestCatalogDecoderDeclaresNoFieldTheEmitterNeverProduces — гейт дерева.
func TestCatalogDecoderDeclaresNoFieldTheEmitterNeverProduces(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)

	read := func(rel string) map[string][]string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(root, rel))
		require.NoErrorf(t, err, "координата %s не читается — гейт потерял предмет, "+
			"а не нашёл чистое дерево; переехал файл — переедет и эта строка", rel)
		tags, err := catalogStructTags(body)
		require.NoErrorf(t, err, "%s не разобрался — вердикта о нём нет", rel)
		require.NotEmptyf(t, tags, "в %s не найдено ни одной структуры с json-тегами — "+
			"обход пуст, вердикт беспредметен, а не чист", rel)
		return tags
	}

	emitter := read(catalogEmitterPath)
	decoder := read(catalogDecoderPath)

	orphans, pairs := orphanTags(emitter, decoder)

	require.Containsf(t, pairs, "CatalogEntry",
		"предмет гейта — запись каталога, и она обязана быть объявлена ОБОИМИ "+
			"файлами. Парных типов найдено: %v. Переименовали тип с одной стороны — "+
			"гейт замолчал бы на всём дереве, поэтому это красное, а не пропуск", pairs)

	t.Logf("перепись: пар типов осмотрено %d (%s) · тегов у эмитента %d · "+
		"у декодера %d · без эмитента %d",
		len(pairs), strings.Join(pairs, ", "),
		countTags(emitter, pairs), countTags(decoder, pairs), countOrphans(orphans))

	for _, typeName := range pairs {
		found := orphans[typeName]
		sort.Strings(found)
		assert.Emptyf(t, found,
			"%s: декодер (%s) объявляет поля, которых эмитент (%s) не производит: %s\n"+
				"Такое поле остаётся нулевым при ЛЮБОМ входе, а со стороны читается как "+
				"несомая каталогом возможность.\n"+
				"Исходов три, четвёртого нет: завести поле у ЭМИТЕНТА (и опцию разметки, "+
				"которой его выставляют) · снять его у декодера · назвать решением, если "+
				"поле приезжает из источника, которого этот гейт не знает, — и тогда "+
				"правь гейт ТЕМ ЖЕ изменением.\n"+
				"Обратное направление находкой НЕ является: тег эмитента, которого декодер "+
				"не читает, законен.",
			typeName, catalogDecoderPath, catalogEmitterPath, strings.Join(found, ", "))
	}
}

func countTags(m map[string][]string, pairs []string) int {
	n := 0
	for _, p := range pairs {
		n += len(m[p])
	}
	return n
}

func countOrphans(m map[string][]string) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}

// TestCatalogDecoderEmitterPredicateCanFail — ДОКАЗАТЕЛЬСТВО способности
// предиката падать и молчать, на входе той же формы, что и настоящие файлы.
//
// Каждая инъекция меняет РОВНО ОДИН факт против своего законного близнеца.
func TestCatalogDecoderEmitterPredicateCanFail(t *testing.T) {
	t.Parallel()

	const emitterSrc = `package gen
type CatalogEntry struct {
	FQN            string         ` + "`json:\"fqn\"`" + `
	Permission     string         ` + "`json:\"permission\"`" + `
	ScopeExtractor ScopeExtractor ` + "`json:\"scope_extractor\"`" + `
	ExemptReason   string         ` + "`json:\"exempt_reason,omitempty\"`" + `
}
type ScopeExtractor struct {
	ObjectType string ` + "`json:\"object_type\"`" + `
}
`
	// Законный близнец: декодер УЖЕ, чем эмитент (не читает exempt_reason).
	const decoderTwin = `package middleware
// requires_mfa_fresh, risk_level — снятые теги, названные в прозе шапки.
// Комментарий обязан быть НЕВИДИМ предикату: иначе гейт краснеет на
// собственном объяснении.
type CatalogEntry struct {
	FQN            string         ` + "`json:\"fqn\"`" + `
	Permission     string         ` + "`json:\"permission\"`" + `
	ScopeExtractor ScopeExtractor ` + "`json:\"scope_extractor\"`" + `
}
type ScopeExtractor struct {
	ObjectType string ` + "`json:\"object_type\"`" + `
}
`
	// Инъекция 1: у декодера появился тег, которого эмитент не производит.
	const decoderOrphan = `package middleware
type CatalogEntry struct {
	FQN              string         ` + "`json:\"fqn\"`" + `
	Permission       string         ` + "`json:\"permission\"`" + `
	ScopeExtractor   ScopeExtractor ` + "`json:\"scope_extractor\"`" + `
	RequiresMFAFresh bool           ` + "`json:\"requires_mfa_fresh\"`" + `
}
type ScopeExtractor struct {
	ObjectType string ` + "`json:\"object_type\"`" + `
}
`
	// Инъекция 2: сирота во ВЛОЖЕННОЙ структуре — она тоже парная.
	const decoderNestedOrphan = `package middleware
type CatalogEntry struct {
	FQN            string         ` + "`json:\"fqn\"`" + `
	Permission     string         ` + "`json:\"permission\"`" + `
	ScopeExtractor ScopeExtractor ` + "`json:\"scope_extractor\"`" + `
}
type ScopeExtractor struct {
	ObjectType                 string ` + "`json:\"object_type\"`" + `
	ObjectTypeFromRequestField string ` + "`json:\"object_type_from_request_field\"`" + `
}
`

	emitter, err := catalogStructTags([]byte(emitterSrc))
	require.NoError(t, err)
	require.Equal(t, []string{"exempt_reason", "fqn", "permission", "scope_extractor"},
		emitter["CatalogEntry"], "разбор эмитента обязан снимать суффикс omitempty")

	twin, err := catalogStructTags([]byte(decoderTwin))
	require.NoError(t, err)
	twinOrphans, twinPairs := orphanTags(emitter, twin)
	require.Equal(t, []string{"CatalogEntry", "ScopeExtractor"}, twinPairs,
		"парность обязана ВЫВОДИТЬСЯ из обоих файлов, включая вложенный тип")
	assert.Empty(t, twinOrphans,
		"законный близнец обязан молчать: декодер уже эмитента, и это законно "+
			"(`exempt_reason` — ровно такой тег в дереве)")

	orphan, err := catalogStructTags([]byte(decoderOrphan))
	require.NoError(t, err)
	gotOrphans, _ := orphanTags(emitter, orphan)
	require.Equal(t, map[string][]string{"CatalogEntry": {"requires_mfa_fresh"}}, gotOrphans,
		"предикат обязан НАЙТИ поле без эмитента и НАЗВАТЬ его тег")

	nested, err := catalogStructTags([]byte(decoderNestedOrphan))
	require.NoError(t, err)
	nestedOrphans, _ := orphanTags(emitter, nested)
	require.Equal(t, map[string][]string{"ScopeExtractor": {"object_type_from_request_field"}},
		nestedOrphans, "вложенная структура обязана осматриваться наравне с верхней")

	// Предпосылка предиката: комментарий, называющий снятый тег, находкой НЕ
	// является. Шапка настоящего декодера перечисляет все семь снятых тегов по
	// именам — предикат по подстроке краснел бы на ней.
	require.NotContains(t, twin["CatalogEntry"], "risk_level",
		"тег, названный только в комментарии, полем НЕ является — иначе гейт "+
			"краснеет на собственном объяснении")

	// Пустой обход не даёт зелёного и здесь: предикат обязан ОТДАТЬ пусто, а
	// вызывающий — упасть на этом, что и делает проба дерева выше.
	empty, err := catalogStructTags([]byte("package p\ntype T struct{ A string }\n"))
	require.NoError(t, err)
	require.Empty(t, empty,
		"структура без json-тегов в перечень не попадает: она на wire не выходит")

	t.Logf("перепись инъекции: входов проверено 5 · парных типов 2 · " +
		"инъекций нашлось 2 · законных близнецов промолчало 2")
}
