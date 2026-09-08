// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verdictenumeration_injection_test.go — доказательство, что Г6 СПОСОБЕН упасть
// и способен смолчать.
//
// Инъекция идёт в обе стороны и НАСТОЯЩИМИ формами: слева — чтение зеркала без
// привязки к предмету запроса, справа — то же чтение с привязкой. Без второй
// стороны гейт ловил бы форму, а не существо, и первый же ложный срабат его
// отключил бы.
package repohygiene

import (
	"strings"
	"testing"
)

const unboundedMirrorSQL = "`\n" + `
SELECT m.object_id
  FROM kaname.resource_mirror m
  JOIN kaname.access_bindings b ON b.resource_id = m.object_id
 WHERE b.status = 'ACTIVE'` + "\n`"

const boundedMirrorSQL = "`\n" + `
WITH scope(s_type, s_id) AS (SELECT $1::text, $2::text)
SELECT m.object_id
  FROM kaname.resource_mirror m
  JOIN scope sc ON sc.s_type = m.object_type AND sc.s_id = m.object_id
  JOIN kaname.access_bindings b ON b.resource_id = m.object_id
 WHERE b.status = 'ACTIVE'` + "\n`"

// registryValueSQL — ЗАКОННЫЙ БЛИЗНЕЦ: имя таблицы как ЗНАЧЕНИЕ реестра, а не
// как чтение. Гейт, считающий его чтением, объявил бы находкой каждую строку
// справочника «где лежат метки этого типа».
const registryValueSQL = `"kaname.accounts"`

// assembledSQL — ЗАКОННЫЙ БЛИЗНЕЦ: запрос, собранный склейкой. Кусок сам по
// себе ни параметров, ни предикатов не несёт — они в соседних слагаемых.
const assembledSQL = "\"SELECT m.labels FROM kaname.resource_mirror m WHERE m.object_id = \" + idParam + \"::text\""

// commaJoinedSQL — чтение, приписанное ЧЕРЕЗ ЗАПЯТУЮ. Форма законная и в
// продукте не встречающаяся; гейт обязан находить её так же, как через JOIN,
// иначе перечисление вернётся именно этим способом.
const commaJoinedSQL = "`\n" + `
SELECT m.object_id
  FROM kaname.access_bindings b, kaname.resource_mirror m
 WHERE b.id = $1::text` + "\n`"

// TestG6RedOnACommaJoinedRead — гейт видит запятую как соединение.
func TestG6RedOnACommaJoinedRead(t *testing.T) {
	found, c := auditInjectedSQL(t, commaJoinedSQL)
	var named bool
	for _, f := range found {
		if f.table == "resource_mirror" {
			named = true
		}
	}
	if !named {
		t.Fatalf("чтение через запятую находкой не признано (чтений %d, привязано %d, находок %+v): "+
			"перечисление вернулось бы именно этой формой", c.reads, c.bounded, found)
	}
	t.Logf("инъекция запятой: находок %d, первая — %s", len(found), found[0].table)
}

func auditInjectedSQL(t *testing.T, lit string) ([]enumFinding, enumCensus) {
	t.Helper()
	src := "package p\n\nvar idParam = \"$1\"\n\nvar q = " + lit + "\n"
	var (
		out []enumFinding
		c   enumCensus
	)
	c.tables = map[string]bool{}
	for _, l := range sqlLiteralsOf("injected.go", []byte(src)) {
		if !strings.Contains(l.sql, "kaname.") {
			continue
		}
		c.literals++
		f, cc := auditSQLForEnumeration("injected.go", l.line, stripSQLLineComments(l.sql))
		out = append(out, f...)
		c.reads += cc.reads
		c.bounded += cc.bounded
	}
	return out, c
}

// TestG6RedOnAReadWithNoBoundToTheSubject — гейт краснеет и НАЗЫВАЕТ координату.
func TestG6RedOnAReadWithNoBoundToTheSubject(t *testing.T) {
	found, c := auditInjectedSQL(t, unboundedMirrorSQL)
	if len(found) == 0 {
		t.Fatalf("чтение зеркала без привязки к предмету запроса находкой не признано "+
			"(перепись: литералов %d, чтений %d, привязано %d). Гейт, не краснеющий на "+
			"собственном предмете, удостоверяет своё молчание", c.literals, c.reads, c.bounded)
	}
	var named bool
	for _, f := range found {
		if f.table == "resource_mirror" && f.line > 0 {
			named = true
		}
	}
	if !named {
		t.Errorf("находка не называет ни таблицы, ни строки: %+v — чинить по такому вердикту нечего", found)
	}
	t.Logf("инъекция: %s:%d — чтение %s (%s)", found[0].file, found[0].line, found[0].table, found[0].alias)
}

// TestG6SilentOnTheSameReadOnceBound — вторая сторона инъекции.
func TestG6SilentOnTheSameReadOnceBound(t *testing.T) {
	for _, tw := range []struct{ name, sql, why string }{
		{"то же чтение с привязкой к цепи областей", boundedMirrorSQL,
			"привязка есть — стоимость принадлежит запросу, а не набору"},
		{"имя таблицы как значение реестра", registryValueSQL,
			"там оно значение, а не запрос: чтением не является"},
		{"запрос, собранный склейкой", assembledSQL,
			"кусок сам по себе ни параметров, ни предикатов не несёт"},
	} {
		found, c := auditInjectedSQL(t, tw.sql)
		if len(found) != 0 {
			t.Errorf("законный близнец «%s» дал %d находок (%+v): %s", tw.name, len(found), found, tw.why)
		}
		t.Logf("близнец «%s»: находок 0 (чтений %d, привязано %d)", tw.name, c.reads, c.bounded)
	}
}

// TestVerdictEnumerationLateralRuleAddsOnlyWhatIsAnchorByConstruction — контроль
// в обе стороны для правила, заведённого вместе с #758.
//
// Правило добавляет в якоря соединение вбок, НЕ ЧИТАЮЩЕЕ таблиц схемы. Без
// второго плеча оно было бы послаблением: соединение вбок, которое таблицу
// читает, обязано по-прежнему судиться как чтение — иначе обход цепи областей
// (он устроен ровно так) выпал бы из-под гейта целиком.
func TestVerdictEnumerationLateralRuleAddsOnlyWhatIsAnchorByConstruction(t *testing.T) {
	readsATable := "SELECT 1 FROM x CROSS JOIN LATERAL (\n" +
		"  SELECT pe.parent_type FROM kaname.resource_scope_edge pe\n" +
		"   WHERE pe.object_type = s.s_type) e"
	if got := computedLateralAliasesOf(readsATable); len(got) != 0 {
		t.Fatalf("соединение вбок, ЧИТАЮЩЕЕ таблицу схемы, попало в якоря (%v): гейт перестал бы "+
			"судить обход цепи областей, устроенный ровно так", got)
	}

	computesOnly := "SELECT 1 FROM x CROSS JOIN LATERAL (\n" +
		"  SELECT substr(x.subject, 7) AS body WHERE x.subject LIKE 'group:%') group_write"
	got := computedLateralAliasesOf(computesOnly)
	if len(got) != 1 || got[0] != "group_write" {
		t.Fatalf("вычисленное соединение вбок якорем НЕ стало (%v): правило не работает, и первый "+
			"же экземпляр новой формы получил бы ложную находку", got)
	}
	t.Logf("плечи правила: читающее вбок — якорем не стало; вычисленное — стало (%s)", got[0])
}
