// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// carriedcoordinateledger.go — разбор ведомости координат, переносимых в F4
// (приёмка F2, сценарий F2-46, §9.4).
//
// # Предмет
//
// Фаза заводит принимающую сторону и НИЧЕГО не сносит: снятие внешнего сервера,
// его базы и зеркальной колонки — предмет более поздней фазы. Между фазами в
// дереве живут координаты, у которых обязан быть НАЗВАН исход. Четвёртого
// исхода — «осталось как есть, потому что не заметили» — не существует.
//
// Послабление, которое не умеет истечь, переживает свой предмет: запись,
// которой больше нечего переносить, объявляет живым закрытый долг, а
// освободившееся место наследует новый дефект с той же координатой.
//
// # Что здесь считается ВЕДОМОСТЬЮ
//
// Таблица разметки, у которой есть колонка координаты и колонка исхода. Имена
// колонок читаются из ЗАГОЛОВКА таблицы, а не считаются по позиции: порядок
// колонок принадлежит автору документа, и разбор по номеру сломался бы от
// перестановки, о которой никто бы не узнал.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **координата, названная прозой**, а не встроенным кодом: разбор берёт
//     содержимое обратных кавычек. Проза координатой не является — по ней
//     нельзя ни перейти, ни проверить существование.
//  2. **исход, названный синонимом** вне закрытого словаря: такая строка
//     становится НАХОДКОЙ, а не пропускается. Словарь исходов закрыт: «прочее»
//     не является корзиной приёма.
//  3. **вторая ведомость** в другом документе. Предмет разбора — названный
//     документ; два места об одном предмете разошлись бы молча, и потому
//     ведомость обязана быть одна.
package repohygiene

import (
	"fmt"
	"sort"
	"strings"
)

// Закрытый словарь исходов. Каждая координата, дожившая до F4, получает один из
// них — и только из них.
const (
	// CarriedOutcomeKept — оставлено как путь к прежнему контуру. ТОЛЬКО этот
	// исход требует предиката снятия: остальные три уже разрешились.
	CarriedOutcomeKept = "оставлено"
	// CarriedOutcomeRemoved — снято.
	CarriedOutcomeRemoved = "снято"
	// CarriedOutcomeRewritten — переписано на наш идентификатор.
	CarriedOutcomeRewritten = "переписано"
	// CarriedOutcomeNotSubject — предметом не является.
	CarriedOutcomeNotSubject = "не предмет"
)

// carriedOutcomeVocabulary — закрытый словарь в порядке проверки.
//
// «не предмет» стоит первым намеренно: он длиннее прочих и содержал бы их, будь
// порядок иным.
var carriedOutcomeVocabulary = []string{
	CarriedOutcomeNotSubject,
	CarriedOutcomeRewritten,
	CarriedOutcomeRemoved,
	CarriedOutcomeKept,
}

// CarriedLedgerRow — строка ведомости.
type CarriedLedgerRow struct {
	// Section — заголовок раздела, в котором лежит таблица. Предикат снятия
	// объявляется по разделу, поэтому раздел строке принадлежит.
	Section string
	// Line — номер строки документа: координата для отказа гейта.
	Line int
	// Coordinate — путь, названный встроенным кодом.
	Coordinate string
	// Outcome — исход из закрытого словаря; пусто, если не опознан.
	Outcome string
	// OutcomeCell — текст ячейки исхода целиком: нужен тексту отказа, потому
	// что по одному слову читатель не поймёт, о чём речь.
	OutcomeCell string
}

// CarriedLedgerSection — раздел документа.
type CarriedLedgerSection struct {
	Title string
	Line  int
	// HasRemovalPredicate — раздел объявляет предикат снятия.
	HasRemovalPredicate bool
}

// CarriedLedgerCensus — объём осмотренного.
type CarriedLedgerCensus struct {
	// Lines — строк документа прочитано.
	Lines int
	// Sections — разделов прочитано.
	Sections int
	// Tables — таблиц с колонками координаты и исхода прочитано.
	Tables int
	// Rows — строк таблиц прочитано.
	Rows int
	// RowsWithoutCoordinate — строк без встроенного кода в колонке координаты.
	// Печатается отдельно: это ровно та форма, которой разбор не видит.
	RowsWithoutCoordinate int
}

// ParseCarriedCoordinateLedger разбирает ведомость.
func ParseCarriedCoordinateLedger(body string) (
	[]CarriedLedgerRow, map[string]CarriedLedgerSection, CarriedLedgerCensus,
) {
	var (
		rows     []CarriedLedgerRow
		census   CarriedLedgerCensus
		sections = map[string]CarriedLedgerSection{}
	)
	lines := strings.Split(body, "\n")
	census.Lines = len(lines)

	section := ""
	// coordCol/outcomeCol — индексы колонок ТЕКУЩЕЙ таблицы; -1 означает, что
	// таблица не открыта либо колонок в ней нет.
	coordCol, outcomeCol := -1, -1
	inFence := false

	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			// Внутри огороженного блока лежит предикат, а не таблица: строка
			// с вертикальными чертами там ведомостью не является.
			continue
		}
		if strings.HasPrefix(line, "#") {
			title := strings.TrimLeft(line, "# ")
			section = title
			census.Sections++
			sections[title] = CarriedLedgerSection{Title: title, Line: i + 1}
			coordCol, outcomeCol = -1, -1
			continue
		}
		if section != "" && carriedMentionsRemovalPredicate(line) {
			s := sections[section]
			s.HasRemovalPredicate = true
			sections[section] = s
		}
		if !strings.HasPrefix(line, "|") {
			// Таблица кончилась вместе с чередой строк, начинающихся с черты.
			coordCol, outcomeCol = -1, -1
			continue
		}
		cells := carriedSplitRow(line)
		if carriedIsDelimiterRow(cells) {
			continue
		}
		if coordCol < 0 && outcomeCol < 0 {
			// Заголовок таблицы: колонки читаются ПО ИМЕНИ, а не по позиции.
			for idx, c := range cells {
				lc := strings.ToLower(c)
				switch {
				case strings.Contains(lc, "координат"):
					coordCol = idx
				case strings.Contains(lc, "исход"):
					outcomeCol = idx
				}
			}
			if coordCol >= 0 && outcomeCol >= 0 {
				census.Tables++
			}
			continue
		}
		if coordCol < 0 || outcomeCol < 0 || coordCol >= len(cells) || outcomeCol >= len(cells) {
			continue
		}
		census.Rows++
		row := CarriedLedgerRow{
			Section:     section,
			Line:        i + 1,
			Coordinate:  carriedInlineCode(cells[coordCol]),
			OutcomeCell: cells[outcomeCol],
			Outcome:     carriedOutcomeOf(cells[outcomeCol]),
		}
		if row.Coordinate == "" {
			census.RowsWithoutCoordinate++
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Line < rows[j].Line })
	return rows, sections, census
}

// carriedMentionsRemovalPredicate — раздел объявляет предикат снятия.
//
// Ищется НАЗВАНИЕ предиката, а не его форма: предикат бывает командой оболочки,
// бывает наблюдаемым состоянием дерева, и требовать от него одной записи значило
// бы судить форму вместо существа.
func carriedMentionsRemovalPredicate(line string) bool {
	lc := strings.ToLower(line)
	return strings.Contains(lc, "предикат снятия")
}

// carriedSplitRow — ячейки строки таблицы.
func carriedSplitRow(line string) []string {
	trimmed := strings.Trim(line, "|")
	parts := strings.Split(trimmed, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// carriedIsDelimiterRow — строка-разделитель заголовка таблицы.
func carriedIsDelimiterRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if c == "" {
			continue
		}
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}

// carriedInlineCode — первое содержимое обратных кавычек ячейки.
func carriedInlineCode(cell string) string {
	first := strings.Index(cell, "`")
	if first < 0 {
		return ""
	}
	rest := cell[first+1:]
	last := strings.Index(rest, "`")
	if last < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:last])
}

// carriedOutcomeOf — исход ячейки по закрытому словарю; пусто, если не опознан.
func carriedOutcomeOf(cell string) string {
	lc := strings.ToLower(cell)
	for _, o := range carriedOutcomeVocabulary {
		if strings.Contains(lc, o) {
			return o
		}
	}
	return ""
}

// CarriedLedgerVerdict — что не так с ведомостью относительно дерева.
//
// Отдельной функцией, а не телом теста: на пустой ведомости тело утверждало бы
// свойство ни о чём, и его способность упасть нечем было бы показать. Инъекция
// подаёт синтетику в ТУ ЖЕ функцию, которая судит дерево, — иначе проверялась бы
// копия правил, а не они сами.
//
// ledgerPath — путь ведомости для координат отказа; treeHas — состав дерева;
// mustBeNamed — координаты, которые ведомость обязана назвать.
func AdjudicateCarriedLedger(
	ledgerPath string,
	rows []CarriedLedgerRow,
	sections map[string]CarriedLedgerSection,
	treeHas func(string) bool,
	mustBeNamed []string,
) []string {
	var findings []string

	// Форма: исход из закрытого словаря, координата встроенным кодом.
	for _, r := range rows {
		if r.Outcome == "" {
			findings = append(findings, fmt.Sprintf(
				"%s:%d — исход %q вне закрытого словаря %v. «Прочее» не является корзиной "+
					"приёма: исход, названный синонимом, читается как решение и решением не "+
					"является", ledgerPath, r.Line, r.OutcomeCell, carriedOutcomeVocabulary))
			continue
		}
		if r.Coordinate == "" {
			findings = append(findings, fmt.Sprintf(
				"%s:%d — запись с исходом %q БЕЗ координаты. По прозе нельзя ни перейти, ни "+
					"проверить, что переносить ещё есть что", ledgerPath, r.Line, r.Outcome))
		}
	}

	// Предикат снятия — только у исхода «оставлено»: остальные три уже
	// разрешились, и снимать в них нечего.
	for _, r := range rows {
		if r.Outcome != CarriedOutcomeKept || sections[r.Section].HasRemovalPredicate {
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"%s:%d — %q оставлено, а раздел «%s» предиката снятия не объявляет. Послабление, "+
				"которое не умеет истечь, переживает свой предмет: снимать его будет некому, "+
				"и следующий читатель примет закрытый долг за живой",
			ledgerPath, r.Line, r.Coordinate, r.Section))
	}

	// Самоистечение: координата, которой в дереве больше нет, переносить нечего.
	for _, r := range rows {
		if r.Coordinate == "" || r.Outcome != CarriedOutcomeKept || treeHas(r.Coordinate) {
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"%s:%d — %q оставлено, а координаты в дереве НЕТ. Записи больше нечего переносить: "+
				"она объявляет живым закрытый долг, и освободившееся место унаследует новый "+
				"дефект с тем же путём. Снятие: убрать запись тем же изменением, которым ушла "+
				"координата", ledgerPath, r.Line, r.Coordinate))
	}

	// Полнота: координата, живущая в дереве и ведомостью не названная.
	named := map[string]bool{}
	for _, r := range rows {
		if r.Coordinate != "" {
			named[r.Coordinate] = true
		}
	}
	for _, f := range mustBeNamed {
		if named[f] {
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"%s читает зеркальную колонку и в ведомости НЕ НАЗВАН. Это и есть четвёртый исход, "+
				"которого не существует: «осталось как есть, потому что не заметили». Исходов "+
				"три: переписать на наш идентификатор, оставить с предикатом снятия, снять", f))
	}

	sort.Strings(findings)
	return findings
}
