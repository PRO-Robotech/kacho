// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// readpathconcat_test.go — #758: НА ПУТИ ЧТЕНИЯ СРАВНИВАЮТСЯ КОЛОНКИ, А НЕ ИХ
// СКЛЕЙКА.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Склейка колонки внутри УСЛОВИЯ выводит колонку из-под индекса: сравнивать
// приходится вычисленное значение, а вычисленное значение отбирает строки
// только ПОСЛЕ того, как они прочитаны. Ответ от этого не меняется, стоимость
// растёт с числом строк в системе, и заметно это только под нагрузкой.
//
// Класс измерен, а не предположен. Починка горячего пути (#359) перевела на пару
// колонок два места — вердикт и перечисление объектов. Перепись ПО СВОЙСТВУ
// («колонка склеивается там, где значение сравнивается») дала на том же дереве
// ЧЕТЫРЕ: два обратных вопроса (`expand.go`, `subjects.go`) и две точки прибора
// замера (`scalegrid/census.go`, `scalegrid/strength_census.go`), гасившие
// `access_binding_subjects_subject_scope_idx`. У одной из четырёх комментарий
// рядом обещал «колонками, а не склейкой» — то есть был ложен.
//
// Поведенческой пробы для этого мало: она мерит ту точку входа, которую зовёт,
// и молчит о трёх остальных — ровно это и позволило долгу прожить. Свойство —
// свойство ДЕРЕВА, и держать его обязан обход дерева.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ЭТОТ ГЕЙТ ОТЛИЧАЕТСЯ ОТ СОСЕДНЕГО (Г6, verdictenumeration_test.go)
//
// Тот судит ПРИВЯЗАННОСТЬ чтения к предмету запроса и вхождение в склеенный
// набор считает законной привязкой — НАМЕРЕННО и справедливо: `IN ('group:' ||
// gm.group_id, …)` действительно берёт членов НАЗВАННЫХ групп, а не всех групп
// системы. Ответ ограничен; из-под индекса выведена лишь ведущая колонка.
// Поэтому Г6 на этот класс молчит by construction, и второй предикат нужен не
// вместо него, а рядом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОБЪЁМ, ЕГО ГРАНИЦА И ДВА АДЪЮДИЦИРОВАННЫХ СЛУЧАЯ ВНЕ ЕГО
//
// Судится путь чтения и прибор, который его мерит. Множество файлов НЕ
// выписано: оно берётся у `scalegrid.ComputeFingerprint` — того же источника,
// которым определяется предмет замера. Выписанный перечень не двинулся бы от
// нового файла и продолжал бы сторожить прежние.
//
// Вне объёма по дереву есть ещё два вхождения формы, и оба разобраны, а не
// умолчаны — иначе «ноль находок» читалось бы шире, чем есть:
//
//   - `services/iam/internal/apps/kaname/seed/migrate_backfill.go` — сравнение
//     `o.payload->>'user' = 'account:' || b.resource_id`. Склеена колонка
//     ВНЕШНЕЙ, уже привязанной таблицы, то есть коррелированный скаляр; у
//     внутренней стороны колонки нет вовсе — там извлечение из JSONB, которое
//     индекса не имеет ни в какой форме. Из-под индекса ничего не выведено.
//   - `services/iam/tools/authzformbench/fullrel.go` — `(f.object_type || '#' || f.relation)
//     IN (…)` при уже связанном соединением `f.subject`. Склейка стоит
//     ОСТАТОЧНЫМ фильтром на строках, отобранных ключом, а не на месте ключа.
//
// Предикат переписи, которым это получено (повторяем его, а не верим):
//
//	git grep -n " || " -- '*.go' | grep -v _test.go | grep -E "'[a-z_]+:' *\|\||\|\| *'#"
//
// Читается ИСПОЛНЯЕМАЯ часть: литералы разбираются по синтаксическому дереву Go
// (SQL в Go-комментарии кодом не является), а внутри литерала снимаются
// SQL-комментарии — иначе гейт краснел бы на объяснении собственного запрета,
// как он и краснел в первой редакции.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// concatQualifiedColumn — квалифицированная колонка: `alias.column`.
var concatQualifiedColumn = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*\b`)

// predicateOpeners / predicateClosers — где условие начинается и где кончается.
//
// Перечни объявлены здесь, потому что они и есть СОДЕРЖАНИЕ запрета: слово,
// внесённое в закрыватели без основания, немедленно делает гейт слепым на целую
// форму условия.
var (
	predicateOpeners = []string{"ON", "WHERE", "AND", "OR", "HAVING"}
	predicateClosers = []string{
		"SELECT", "FROM", "JOIN", "GROUP", "ORDER", "LIMIT", "UNION", "WITH",
		"VALUES", "RETURNING", "INSERT", "UPDATE", "DELETE", "SET", "CASE", "THEN",
		"ELSE", "END", "AS",
	}
)

type concatFinding struct {
	file    string
	line    int
	operand string
}

type concatCensus struct {
	files, literals, segments, concatsInPredicate, concatsElsewhere int
}

// TestReadPathComparesColumnsNotConcatenations — на пути чтения нет условия,
// сравнивающего склейку колонки.
func TestReadPathComparesColumnsNotConcatenations(t *testing.T) {
	root := repoRoot(t)
	files, dirs := readPathGoFiles(t, root)

	findings, c := collectPredicateConcats(t, root, files)

	if c.files == 0 || c.literals == 0 || c.segments == 0 {
		t.Fatalf("предпосылка гейта не выполнена: файлов %d, литералов с SQL %d, условных "+
			"сегментов %d. «Ноль находок» тогда означает «ноль прочитанного»",
			c.files, c.literals, c.segments)
	}
	// ЗАКОННЫЙ БЛИЗНЕЦ ОБЯЗАН ПРИСУТСТВОВАТЬ. Склейка в ПРОЕКЦИИ (`SELECT
	// gm.member_type || ':' || gm.member_id`) законна и в этих же файлах живёт.
	// Если гейт её не встретил, значит он либо не дочитал до неё, либо считает
	// её условием — и тогда его молчание о находках ничего не стоит.
	if c.concatsElsewhere == 0 {
		t.Fatalf("в осмотренном не встретилось НИ ОДНОЙ склейки вне условия, а они там есть " +
			"(проекции обратных вопросов). Гейт либо не дочитал, либо не отличает проекцию от " +
			"условия — и его молчание о находках ничего не стоит")
	}

	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: каталогов выведено %d (%s), файлов %d, литералов с SQL %d, "+
		"условных сегментов %d, склеек в условии %d, склеек вне условия (законный близнец) %d, "+
		"находок %d",
		len(dirs), strings.Join(dirs, ", "), c.files, c.literals, c.segments,
		c.concatsInPredicate, c.concatsElsewhere, len(findings))

	for _, f := range findings {
		t.Errorf("%s:%d: условие сравнивает СКЛЕЙКУ колонки: %s\n"+
			"    Склейка выводит колонку из-под индекса: сравнивается вычисленное значение, а оно "+
			"отбирает строки только ПОСЛЕ чтения. Ответ тот же, стоимость растёт с числом строк в "+
			"системе.\n    Разбери значение ДО сравнения (см. speaker_pair в query.go и "+
			"membersOfNamedGroups в membership.go) и зайди голыми колонками.",
			f.file, f.line, f.operand)
	}
}

// fingerprintSource — объявление предмета замера, из которого ВЫВОДИТСЯ объём
// этого гейта.
//
// Импортировать пакет нельзя (он под `internal/` чужого дерева), поэтому
// объявление читается ТЕКСТОМ — но именно объявление, а не второй перечень.
// Свой перечень каталогов не двинулся бы от появления третьего и продолжал бы
// сторожить прежние два.
const fingerprintSource = "services/iam/internal/repo/kaname/pg/scalegrid/fingerprint.go"

// fingerprintDirDecl — строка вида `verdictDir = "…"` в этом объявлении.
var fingerprintDirDecl = regexp.MustCompile(`(?m)^\s*(\w*[Dd]ir)\s*=\s*"([^"]+)"`)

// readPathGoFiles — не-тестовые .go каталогов, составляющих предмет замера.
//
// Каталог, в котором таких файлов нет вовсе (каталог миграций), в объём просто
// не приносит ничего — исключать его СПИСКОМ не нужно, и списка здесь нет.
func readPathGoFiles(t *testing.T, root string) ([]string, []string) {
	t.Helper()
	body, err := readRepoFile(filepath.Join(root, fingerprintSource))
	if err != nil {
		t.Fatalf("объявление предмета замера не читается: %v", err)
	}
	found := fingerprintDirDecl.FindAllStringSubmatch(string(body), -1)
	if len(found) == 0 {
		t.Fatalf("в %s не нашлось ни одного объявления каталога по предикату %s: объём гейта "+
			"выведен быть не может, и «ноль находок» означало бы «ноль прочитанного»",
			fingerprintSource, fingerprintDirDecl.String())
	}
	var dirs, out []string
	for _, m := range found {
		dirs = append(dirs, m[2])
		entries, derr := os.ReadDir(filepath.Join(root, m[2]))
		if derr != nil {
			t.Fatalf("каталог %s, названный предметом замера, не читается: %v", m[2], derr)
		}
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			out = append(out, filepath.Join(m[2], n))
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatalf("каталоги предмета замера (%s) не дали НИ ОДНОГО не-тестового .go: судить "+
			"нечего, и молчание гейта означало бы свойство, которого никто не проверял",
			strings.Join(dirs, ", "))
	}
	return out, dirs
}

func collectPredicateConcats(t *testing.T, root string, files []string) ([]concatFinding, concatCensus) {
	t.Helper()
	var c concatCensus
	var out []concatFinding
	for _, rel := range files {
		body, err := readRepoFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
		c.files++
		for _, lit := range sqlLiteralsOf(filepath.Base(rel), body) {
			if !strings.Contains(lit.sql, "kaname.") {
				continue
			}
			c.literals++
			sql := stripSQLComments(lit.sql)
			// Содержимое строковых литералов SQL ЗАКРЫВАЕТСЯ, а длина
			// сохраняется. Без этого слово внутри литерала читается как
			// ключевое: `'group:'` обрывал условие на GROUP, и гейт молчал
			// ровно на той форме, ради которой написан (поймано инъекцией).
			masked := maskSQLStrings(sql)
			for _, seg := range predicateSegmentsOf(masked) {
				c.segments++
				for _, sp := range concatOperandSpans(masked[seg.at : seg.at+len(seg.text)]) {
					op := sql[seg.at+sp[0] : seg.at+sp[1]]
					if !concatQualifiedColumn.MatchString(masked[seg.at+sp[0] : seg.at+sp[1]]) {
						continue
					}
					c.concatsInPredicate++
					out = append(out, concatFinding{
						file:    rel,
						line:    lit.line + strings.Count(sql[:seg.at+sp[0]], "\n"),
						operand: strings.Join(strings.Fields(op), " "),
					})
				}
			}
			c.concatsElsewhere += countConcatsOutsidePredicates(masked)
		}
	}
	return out, c
}

// segment — кусок SQL, стоящий ПОД условием, и его смещение в литерале.
type segment struct {
	text string
	at   int
}

// stripSQLComments — снятие `--`-комментариев внутри литерала.
//
// Без него гейт краснеет на ОБЪЯСНЕНИИ собственного запрета: комментарий у
// исправленного места называет прежнюю форму дословно, и это правильно —
// комментарий обязан называть то, что запрещает.
func stripSQLComments(sql string) string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// maskSQLStrings — содержимое строковых литералов SQL закрыто, длина сохранена.
//
// Смещения остаются годными для ИСХОДНОГО текста, поэтому сообщение находки
// по-прежнему называет то, что написано, а не маску.
func maskSQLStrings(sql string) string {
	b := []byte(sql)
	in := false
	for i := 0; i < len(b); i++ {
		if b[i] == '\'' {
			in = !in
			continue
		}
		if in && b[i] != '\n' {
			b[i] = '~'
		}
	}
	return string(b)
}

// predicateSegmentsOf — куски, стоящие под условием.
//
// Разбор по ключевым словам, а не по строкам файла: условие переносится, и
// построчный разбор объявил бы находкой то, что стоит в проекции строкой выше.
func predicateSegmentsOf(sql string) []segment {
	words := sqlWordSpans(sql)
	var out []segment
	open := -1
	for _, w := range words {
		up := strings.ToUpper(sql[w[0]:w[1]])
		if containsWord(predicateOpeners, up) {
			if open >= 0 {
				out = append(out, segment{text: sql[open:w[0]], at: open})
			}
			open = w[1]
			continue
		}
		if containsWord(predicateClosers, up) && open >= 0 {
			out = append(out, segment{text: sql[open:w[0]], at: open})
			open = -1
		}
	}
	if open >= 0 {
		out = append(out, segment{text: sql[open:], at: open})
	}
	return out
}

// countConcatsOutsidePredicates — склейки, стоящие ВНЕ условия.
//
// Существует ради законного близнеца: без него «находок ноль» неотличимо от
// «гейт ничего не разобрал».
func countConcatsOutsidePredicates(sql string) int {
	inPredicate := map[int]bool{}
	for _, seg := range predicateSegmentsOf(sql) {
		for i := seg.at; i < seg.at+len(seg.text); i++ {
			inPredicate[i] = true
		}
	}
	n := 0
	for i := 0; i+1 < len(sql); i++ {
		if sql[i] == '|' && sql[i+1] == '|' && !inPredicate[i] {
			n++
			i++
		}
	}
	return n
}

// concatOperands — цепочки склейки внутри условия, каждая целиком.
//
// Цепочка ограничена запятой того же уровня скобок, оператором сравнения и
// границами сегмента: без этого одна находка размазывалась бы на весь предикат,
// и сообщение не называло бы того, что чинить.
func concatOperandSpans(seg string) [][2]int {
	var out [][2]int
	depth := 0
	start := 0
	hasConcat := false
	flush := func(end int) {
		if hasConcat {
			out = append(out, [2]int{start, end})
		}
		hasConcat = false
	}
	for i := 0; i < len(seg); i++ {
		switch {
		case seg[i] == '(':
			depth++
		case seg[i] == ')':
			if depth == 0 {
				flush(i)
				start = i + 1
				continue
			}
			depth--
		case depth == 0 && seg[i] == ',':
			flush(i)
			start = i + 1
		case depth == 0 && (seg[i] == '=' || seg[i] == '<' || seg[i] == '>' || seg[i] == '!'):
			flush(i)
			start = i + 1
		case seg[i] == '|' && i+1 < len(seg) && seg[i+1] == '|':
			hasConcat = true
			i++
		}
	}
	flush(len(seg))
	return out
}

// sqlWordSpans — границы слов SQL, чтобы `ON` в середине имени не считался
// ключевым словом.
func sqlWordSpans(sql string) [][2]int {
	var out [][2]int
	start := -1
	for i := 0; i <= len(sql); i++ {
		isWord := i < len(sql) && (sql[i] == '_' ||
			(sql[i] >= 'a' && sql[i] <= 'z') || (sql[i] >= 'A' && sql[i] <= 'Z') ||
			(sql[i] >= '0' && sql[i] <= '9'))
		switch {
		case isWord && start < 0:
			start = i
		case !isWord && start >= 0:
			out = append(out, [2]int{start, i})
			start = -1
		}
	}
	return out
}

func containsWord(set []string, w string) bool {
	for _, s := range set {
		if s == w {
			return true
		}
	}
	return false
}

func readRepoFile(path string) ([]byte, error) {
	body, err := os.ReadFile(path) // #nosec G304 -- путь получен обходом СОБСТВЕННОГО дерева
	if err != nil {
		return nil, fmt.Errorf("чтение %s: %w", path, err)
	}
	return body, nil
}
