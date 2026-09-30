// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// identityprobefate.go — поимённая судьба проб полосы личности `deploy/`
// (задача #2731): у каждой пробы ровно одна строка ведомости, у строки ровно
// один исход из закрытого словаря и довод с координатой, которая резолвится.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Снятие внешнего поставщика личности (эпик #2564, #1276, #2818) решает судьбу
// пятидесяти с лишним проб `deploy/identity_*_test.go`. Прежде она была записана
// ГРУППАМИ: запись называла признак группы, и за ним стояли файлы, которых
// поимённо не назвал никто. Групповой признак ошибается в обе стороны — проба
// может называть снимаемое в прозе и судить свою полосу, и может не называть его
// и целиком на нём стоять, — поэтому судьба каждого файла выводится разбором его
// содержимого, а держит её эта ведомость.
//
// Ведомость без держателя стареет молча: пробу добавили, сняли, переименовали —
// и запись описывает уже не дерево. Отсюда требования, и каждое из них краснеет:
//
//  1. СОСТАВ. Множество строк ведомости совпадает с множеством файлов
//     `deploy/identity_*_test.go` индекса git: файл без строки — находка, строка
//     без файла — находка, файл дважды — находка.
//  2. ОДНА СТРОКА — ОДИН ФАЙЛ. Ячейка файла — ровно одно имя пробы. Групповая
//     запись (образец, перечень, каталог) — находка: за ней снова стоят файлы,
//     которых поимённо не назвал никто.
//  3. ИСХОД ИЗ ЗАКРЫТОГО СЛОВАРЯ — `identityProbeFates`. Четвёртого исхода и
//     корзины «прочее» нет.
//  4. ДОВОД С КООРДИНАТОЙ, КОТОРАЯ РЕЗОЛВИТСЯ. У строки есть хотя бы одна
//     координата в САМОЙ пробе (довод — то, что проба утверждает), и каждая
//     координата строки указывает в отслеживаемый файл и в существующие строки.
//     Координата, ушедшая за конец файла, — находка: довод описывает уже другое.
//  5. РАЗБИВКА СХОДИТСЯ. Письменная разбивка по исходам равна счёту по строкам,
//     итог равен числу файлов дерева: число в документе, которое никто не
//     пересчитывает, — первое, что в нём лжёт.
//  6. КООРДИНАТА СТОИТ НА СВОЁМ ПРЕДМЕТЕ. Требование 4 судит, что строка
//     существует, — и молчит, когда файл под координатой сдвинулся: номер
//     остался в пределах файла, а указывает уже в другое (сборка 1 волны 4:
//     17 записей о 14 координатах сошли так со своих предметов, гейт был
//     зелёным). Поэтому у каждой координаты документа — строк ведомости, прозы
//     и соседних таблиц — есть ЯКОРЬ: текст, стоящий на её первой строке, а у
//     диапазона и на последней. Якорь не на своей строке — находка, и она
//     называет, где он стоит теперь; координата без якоря и якорь без
//     координаты — тоже находки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ФОРМЫ ЗАПИСИ, КОТОРЫЕ РАЗБОР ЗНАЕТ, — ПЕРЕЧИСЛЕНЫ, И ПРОЧИХ НЕТ
//
// Ведомость — таблица Markdown с заголовком `identityProbeFateHeader` (ровно
// одна в документе), таблица разбивки с заголовком `identityProbeFateTotals` и
// таблица якорей с заголовком `identityProbeFateAnchorsHeader` (ровно одна).
// Координата — фрагмент в обратных кавычках, двух форм:
//
//	`:<строка>[-<строка>]`         в самой пробе строки — только в ячейке довода
//	`<путь>:<строка>[-<строка>]`   путь от корня репозитория — в ячейке довода и
//	                               в любом месте документа вне ограждённого кода
//	                               и таблицы якорей
//
// Форма `:<строка>` вне строки ведомости не резолвится ни во что и координатой
// не читается. Фрагмент ТОЙ ЖЕ ФОРМЫ, который не резолвится, — находка, а не
// молчание: иначе всё записанное в незнакомом написании ушло бы из-под
// наблюдения. Фрагменты другой формы (имена ключей, ручек) координатами не
// являются и не читаются.
//
// Строка таблицы якорей: координата с путём от корня, якорь первой строки и
// якорь последней — у одиночной координаты вместо него `—`. Якорь — один
// фрагмент в обратных кавычках, и он стоит на строке ПОДСТРОКОЙ: обратной
// кавычки и `|` в нём нет по построению формы, поэтому якорем берётся часть
// строки, а не строка целиком. Координата, записанная в документе несколько
// раз, прибита одной строкой и судится одной находкой.
//
// ПРЕДЕЛ ТОЧНОСТИ назван, а не подразумевается. Якорь, стоящий в файле не на
// одной строке (две одинаковые строки хука), ловит сдвиг, кроме сдвига ровно на
// расстояние до своего двойника; число таких якорей печатает перепись. Якорь
// последней строки диапазона судится только на ней (`}` законно стоит на многих
// строках): он ловит рост и усадку предмета внутри диапазона, а сдвиг ловит
// якорь первой.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЦЕЛЬ — ПУСТАЯ ВЕДОМОСТЬ, И ОНА НЕ ОТКАЗ
//
// Пробы снимаются по ходу снятия поставщика, и строки уходят вместе с ними.
// Ведомость, у которой ноль строк при нуле файлов, — достигнутая цель: гейт
// проходит и печатает перепись «строк 0 · файлов 0». Отказом остаётся
// нераспознанная форма: ведомость, заголовка которой разбор не нашёл, дала бы
// «строк 0» при любом содержимом. Когда последняя проба уйдёт, гейт и ведомость
// снимаются одним изменением.

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// IdentityProbeFateLedgerFile — ведомость, от корня репозитория.
const IdentityProbeFateLedgerFile = "docs/architecture/identity-probe-fate-census.md"

// identityProbeDir — каталог проб, чью судьбу держит ведомость.
const identityProbeDir = "deploy"

// identityProbeName — имя пробы полосы личности: ровно один файл каталога.
var identityProbeName = regexp.MustCompile(`^identity_[a-z0-9_]+_test\.go$`)

// Исходы. Словарь ЗАКРЫТ: исход вне него — находка.
const (
	// identityFateRemove — проба существует только ради снимаемого предмета.
	identityFateRemove = "снять"
	// identityFateKeep — проба судит свою полосу, снимаемое в ней упомянуто, но
	// предметом не является.
	identityFateKeep = "оставить"
	// identityFateRewrite — проба судит живой предмет через снимаемое и после
	// снятия обязана продолжить судить его на нашем носителе.
	identityFateRewrite = "переписать"
)

// identityProbeFates — закрытый словарь в порядке печати.
var identityProbeFates = []string{identityFateRemove, identityFateKeep, identityFateRewrite}

// identityProbeFateHeader — заголовок ведомости. Разбор узнаёт таблицу по нему,
// и документ обязан нести его ровно один раз.
var identityProbeFateHeader = []string{"#", "файл", "что утверждает", "предпосылка", "исход", "основание (координата)"}

// identityProbeFateTotals — заголовок таблицы разбивки по исходам.
var identityProbeFateTotals = []string{"исход", "файлов"}

// identityProbeFateTotalRow — ячейка итоговой строки разбивки.
const identityProbeFateTotalRow = "итого"

// identityProbeFateAnchorsHeader — заголовок таблицы якорей координат.
var identityProbeFateAnchorsHeader = []string{"координата", "первая строка", "последняя строка"}

// identityProbeAnchorNone — ячейка последней строки у одиночной координаты.
const identityProbeAnchorNone = "—"

// identityProbeCoordKey — координата как ключ: путь от корня и диапазон строк.
type identityProbeCoordKey struct {
	Path     string
	From, To int
}

// String — координата в форме `<путь>:<строка>[-<строка>]`.
func (k identityProbeCoordKey) String() string {
	if k.To == k.From {
		return fmt.Sprintf("%s:%d", k.Path, k.From)
	}
	return fmt.Sprintf("%s:%d-%d", k.Path, k.From, k.To)
}

// identityProbeAnchorCoord — ячейка координаты таблицы якорей: ровно одна
// координата, и путь в ней от корня. Форма `:<строка>` здесь не годится —
// у таблицы якорей нет пробы строки, в которую она резолвилась бы.
var identityProbeAnchorCoord = regexp.MustCompile("^`([A-Za-z0-9_./-]+):([0-9]+)(?:-([0-9]+))?`$")

// identityProbeAnchorText — ячейка якоря: ровно один фрагмент в обратных
// кавычках. Обратной кавычки и `|` в якоре нет по построению формы: первая
// закрыла бы фрагмент, вторая — ячейку.
var identityProbeAnchorText = regexp.MustCompile("^`([^`]+)`$")

// identityProbeAnchorWhereCap — сколько мест якоря называет находка.
const identityProbeAnchorWhereCap = 5

// identityProbeCoord — координата в обратных кавычках: путь (пустой = сама
// проба строки) и строка либо диапазон строк.
var identityProbeCoord = regexp.MustCompile("`([A-Za-z0-9_./-]*):([0-9]+)(?:-([0-9]+))?`")

// Отказы разбора: там, где судить не по чему. Третья категория не растворяется
// в зелёном.
var (
	// errIdentityProbeFateNoLedger — заголовка ведомости в документе нет:
	// «строк 0» здесь означало бы «ноль прочитанного».
	errIdentityProbeFateNoLedger = errors.New("заголовок ведомости не найден")
	// errIdentityProbeFateTwoLedgers — заголовок стоит дважды: два перечня об
	// одном предмете, и действующим оказался бы прочитанный первым.
	errIdentityProbeFateTwoLedgers = errors.New("заголовок ведомости стоит больше одного раза")
	// errIdentityProbeFateNoTotals — разбивки по исходам в документе нет.
	errIdentityProbeFateNoTotals = errors.New("таблица разбивки по исходам не найдена")
	// errIdentityProbeFateNoAnchors — таблицы якорей нет.
	errIdentityProbeFateNoAnchors = errors.New("таблица якорей координат не найдена")
	// errIdentityProbeFateTwoAnchorTables — таблица якорей стоит дважды.
	errIdentityProbeFateTwoAnchorTables = errors.New("таблица якорей координат стоит больше одного раза")
)

// identityProbeCoordRef — одна прочитанная координата документа.
type identityProbeCoordRef struct {
	// Path — путь от корня; для формы `:<строка>` — путь самой пробы строки.
	Path string
	// From, To — диапазон строк; у одиночной строки From == To.
	From, To int
	// Raw — фрагмент, как записан.
	Raw string
	// Line — номер строки документа, где координата записана.
	Line int
}

// key — координата как ключ таблицы якорей.
func (c identityProbeCoordRef) key() identityProbeCoordKey {
	return identityProbeCoordKey{Path: c.Path, From: c.From, To: c.To}
}

// identityProbeCoordAnchor — строка таблицы якорей: текст, который стоит на
// первой строке координаты, и у диапазона — на последней.
type identityProbeCoordAnchor struct {
	// Line — номер строки документа.
	Line int
	// Key — координата, которую строка прибивает.
	Key identityProbeCoordKey
	// Raw — ячейка координаты, как записана.
	Raw string
	// First — якорь первой строки.
	First string
	// Last — якорь последней строки; HasLast == false — записано «—».
	Last    string
	HasLast bool
	// Unreadable — почему строку нельзя прочитать; пусто — прочитана.
	Unreadable string
}

// identityProbeFateRow — одна строка ведомости.
type identityProbeFateRow struct {
	// Line — номер строки документа.
	Line int
	// File — содержимое ячейки файла без обратных кавычек.
	File string
	// Fate — ячейка исхода, как записана.
	Fate string
	// Coords — координаты ячейки довода.
	Coords []identityProbeCoordRef
	// Malformed — ячеек не столько, сколько объявляет заголовок.
	Malformed bool
}

// identityProbeFateLedger — разобранный документ.
type identityProbeFateLedger struct {
	Rows []identityProbeFateRow
	// Totals — письменная разбивка: ячейка исхода → число; итог — отдельно.
	Totals map[string]int
	// TotalsLine — строка документа каждой записи разбивки.
	TotalsLine map[string]int
	// Total — итоговая строка разбивки; -1, если её нет.
	Total int
	// TotalsUnreadable — записи разбивки, чьё число не читается.
	TotalsUnreadable []string
	// Elsewhere — координаты с путём вне строк ведомости: проза, соседние
	// таблицы. Форма `:<строка>` вне строки ведомости не резолвится ни во что и
	// координатой не читается.
	Elsewhere []identityProbeCoordRef
	// Anchors — таблица якорей в порядке записи.
	Anchors []identityProbeCoordAnchor
}

// identityProbeFateFacts — дерево, против которого судится ведомость.
// Отделено от чтения диска ради инъекции.
type identityProbeFateFacts struct {
	// Probes — имена файлов `deploy/identity_*_test.go` индекса git.
	Probes []string
	// Text — строки отслеживаемого файла по пути от корня, как их нумерует
	// редактор (строка N — Text[p][N-1]). Пути нет — файл не отслеживается.
	Text map[string][]string
}

// identityProbeFateCensus — объём осмотренного: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
type identityProbeFateCensus struct {
	Rows   int
	Probes int
	ByFate map[string]int
	// Coords — координаты строк ведомости; OwnCoords — из них в самой пробе.
	Coords    int
	OwnCoords int
	// OtherCoords — координаты вне строк ведомости.
	OtherCoords int
	// Anchors — прочитанные строки таблицы якорей.
	Anchors int
	// OnSubject — координаты (ключом), чей якорь стоит на их строках.
	OnSubject int
	// SharedAnchors — из них те, чей якорь первой строки стоит в файле не на
	// одной строке: точность проверки у них ниже, и перепись это называет.
	SharedAnchors int
}

// String — перепись одной строкой, для печати каждого прогона.
func (c identityProbeFateCensus) String() string {
	parts := []string{
		fmt.Sprintf("строк ведомости %d", c.Rows),
		fmt.Sprintf("файлов в дереве %d", c.Probes),
	}
	for _, f := range identityProbeFates {
		parts = append(parts, fmt.Sprintf("%s %d", f, c.ByFate[f]))
	}
	parts = append(parts,
		fmt.Sprintf("координат проверено %d (в самой пробе %d)", c.Coords, c.OwnCoords),
		fmt.Sprintf("вне строк ведомости %d", c.OtherCoords),
		fmt.Sprintf("якорей %d", c.Anchors),
		fmt.Sprintf("на своём предмете %d (якорь не единственный в файле у %d)", c.OnSubject, c.SharedAnchors))
	return strings.Join(parts, " · ")
}

// splitMarkdownRow — ячейки строки таблицы Markdown без крайних разделителей.
// Не строка таблицы — nil.
func splitMarkdownRow(line string) []string {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "|") || !strings.HasSuffix(s, "|") || len(s) < 2 {
		return nil
	}
	cells := strings.Split(s[1:len(s)-1], "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells
}

// isMarkdownSeparator — строка-разделитель под заголовком (`|---|:---:|`).
func isMarkdownSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if strings.Trim(c, ":-") != "" || !strings.Contains(c, "-") {
			return false
		}
	}
	return true
}

// sameCells — ячейки заголовка совпадают с объявленными дословно.
func sameCells(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// plainCell — ячейка без выделения жирным.
func plainCell(c string) string { return strings.TrimSpace(strings.ReplaceAll(c, "**", "")) }

// tableAfter — строки данных таблицы, чей заголовок стоит на строке at
// (индекс в lines). Таблица кончается первой строкой, таблицей не являющейся.
func tableAfter(lines []string, at int) (rows [][]string, rowLines []int) {
	i := at + 1
	if i < len(lines) && isMarkdownSeparator(splitMarkdownRow(lines[i])) {
		i++
	}
	for ; i < len(lines); i++ {
		cells := splitMarkdownRow(lines[i])
		if cells == nil {
			break
		}
		rows = append(rows, cells)
		rowLines = append(rowLines, i+1)
	}
	return rows, rowLines
}

// parseIdentityProbeFateLedger разбирает документ ведомости.
func parseIdentityProbeFateLedger(text, probeDir string) (identityProbeFateLedger, error) {
	l := identityProbeFateLedger{Totals: map[string]int{}, TotalsLine: map[string]int{}, Total: -1}
	lines := strings.Split(text, "\n")

	var ledgerAt, totalsAt, anchorsAt []int
	for i, line := range lines {
		cells := splitMarkdownRow(line)
		switch {
		case sameCells(cells, identityProbeFateHeader):
			ledgerAt = append(ledgerAt, i)
		case sameCells(cells, identityProbeFateTotals):
			totalsAt = append(totalsAt, i)
		case sameCells(cells, identityProbeFateAnchorsHeader):
			anchorsAt = append(anchorsAt, i)
		}
	}
	switch {
	case len(ledgerAt) == 0:
		return l, errIdentityProbeFateNoLedger
	case len(ledgerAt) > 1:
		return l, errIdentityProbeFateTwoLedgers
	case len(totalsAt) == 0:
		return l, errIdentityProbeFateNoTotals
	case len(anchorsAt) == 0:
		return l, errIdentityProbeFateNoAnchors
	case len(anchorsAt) > 1:
		return l, errIdentityProbeFateTwoAnchorTables
	}

	// Строки ведомости и таблицы якорей: координаты первой читаются по
	// ячейкам, второй — координатами документа не являются вовсе.
	tableLines := map[int]bool{}
	claim := func(at int, rowLines []int) {
		tableLines[at+1] = true
		if at+1 < len(lines) && isMarkdownSeparator(splitMarkdownRow(lines[at+1])) {
			tableLines[at+2] = true
		}
		for _, n := range rowLines {
			tableLines[n] = true
		}
	}

	rows, rowLines := tableAfter(lines, ledgerAt[0])
	claim(ledgerAt[0], rowLines)
	for i, cells := range rows {
		r := identityProbeFateRow{Line: rowLines[i]}
		if len(cells) != len(identityProbeFateHeader) {
			r.Malformed = true
			l.Rows = append(l.Rows, r)
			continue
		}
		r.File = strings.Trim(cells[1], "` ")
		r.Fate = plainCell(cells[4])
		r.Coords = coordsOf(cells[5], probeDir+"/"+r.File, r.Line)
		l.Rows = append(l.Rows, r)
	}

	arows, alines := tableAfter(lines, anchorsAt[0])
	claim(anchorsAt[0], alines)
	for i, cells := range arows {
		l.Anchors = append(l.Anchors, anchorOf(cells, alines[i]))
	}

	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced || tableLines[i+1] {
			continue
		}
		l.Elsewhere = append(l.Elsewhere, coordsOf(line, "", i+1)...)
	}

	for _, at := range totalsAt {
		trows, tlines := tableAfter(lines, at)
		for i, cells := range trows {
			if len(cells) != len(identityProbeFateTotals) {
				l.TotalsUnreadable = append(l.TotalsUnreadable, fmt.Sprintf("строка %d", tlines[i]))
				continue
			}
			key := plainCell(cells[0])
			n, err := strconv.Atoi(plainCell(cells[1]))
			if err != nil {
				l.TotalsUnreadable = append(l.TotalsUnreadable, fmt.Sprintf("строка %d (%q)", tlines[i], cells[1]))
				continue
			}
			if key == identityProbeFateTotalRow {
				l.Total = n
				continue
			}
			if _, dup := l.Totals[key]; dup {
				l.TotalsUnreadable = append(l.TotalsUnreadable,
					fmt.Sprintf("строка %d: исход %q в разбивке второй раз", tlines[i], key))
				continue
			}
			l.Totals[key] = n
			l.TotalsLine[key] = tlines[i]
		}
	}
	return l, nil
}

// coordsOf — координаты фрагмента документа. own — путь пробы строки
// ведомости; пустой own значит «вне строки ведомости», и форма `:<строка>`
// там не читается: резолвиться ей не во что.
func coordsOf(text, own string, line int) []identityProbeCoordRef {
	var out []identityProbeCoordRef
	for _, m := range identityProbeCoord.FindAllStringSubmatch(text, -1) {
		c := identityProbeCoordRef{Path: m[1], Raw: m[0], Line: line}
		if c.Path == "" {
			if own == "" {
				continue
			}
			c.Path = own
		}
		c.From, _ = strconv.Atoi(m[2])
		c.To = c.From
		if m[3] != "" {
			c.To, _ = strconv.Atoi(m[3])
		}
		out = append(out, c)
	}
	return out
}

// anchorText — содержимое ячейки якоря; false — ячейка не один непустой
// фрагмент в обратных кавычках.
func anchorText(cell string) (string, bool) {
	m := identityProbeAnchorText.FindStringSubmatch(cell)
	if m == nil {
		return "", false
	}
	t := strings.TrimSpace(m[1])
	return t, t != ""
}

// anchorOf — разбор строки таблицы якорей.
func anchorOf(cells []string, line int) identityProbeCoordAnchor {
	a := identityProbeCoordAnchor{Line: line}
	if len(cells) != len(identityProbeFateAnchorsHeader) {
		a.Unreadable = fmt.Sprintf("ячеек %d, а не %d", len(cells), len(identityProbeFateAnchorsHeader))
		if len(cells) > 0 {
			a.Raw = cells[0]
		}
		return a
	}
	a.Raw = cells[0]
	m := identityProbeAnchorCoord.FindStringSubmatch(cells[0])
	if m == nil {
		a.Unreadable = "ячейка координаты — не одна координата `<путь от корня>:<строка>[-<строка>]`"
		return a
	}
	a.Key.Path = m[1]
	a.Key.From, _ = strconv.Atoi(m[2])
	a.Key.To = a.Key.From
	if m[3] != "" {
		a.Key.To, _ = strconv.Atoi(m[3])
	}
	var ok bool
	if a.First, ok = anchorText(cells[1]); !ok {
		a.Unreadable = fmt.Sprintf("ячейка первой строки %q — не один непустой фрагмент в обратных кавычках", cells[1])
		return a
	}
	if cells[2] == identityProbeAnchorNone {
		return a
	}
	if a.Last, ok = anchorText(cells[2]); !ok {
		a.Unreadable = fmt.Sprintf("ячейка последней строки %q — не фрагмент в обратных кавычках и не «%s»",
			cells[2], identityProbeAnchorNone)
		return a
	}
	a.HasLast = true
	return a
}

// linesWith — номера строк текста, несущих фрагмент.
func linesWith(text []string, frag string) []int {
	var at []int
	for i, l := range text {
		if strings.Contains(l, frag) {
			at = append(at, i+1)
		}
	}
	return at
}

// whereAnchor — где якорь стоит в файле, словами находки.
func whereAnchor(at []int) string {
	if len(at) == 0 {
		return "в файле его нет — предмет переписан или снят: перечитайте пробу и назовите строку заново"
	}
	shown := at
	if len(shown) > identityProbeAnchorWhereCap {
		shown = shown[:identityProbeAnchorWhereCap]
	}
	parts := make([]string, len(shown))
	for i, n := range shown {
		parts[i] = fmt.Sprintf(":%d", n)
	}
	tail := ""
	if len(at) > len(shown) {
		tail = fmt.Sprintf(" и ещё %d", len(at)-len(shown))
	}
	return "в файле он стоит на " + strings.Join(parts, ", ") + tail +
		" — переведите координату на строку предмета, перечитав его"
}

// knownFate — исход из закрытого словаря.
func knownFate(f string) bool {
	for _, k := range identityProbeFates {
		if f == k {
			return true
		}
	}
	return false
}

// resolveCoord — координата указывает в отслеживаемый файл и в его строки.
// Пусто — резолвится; иначе — причина словами находки.
func resolveCoord(co identityProbeCoordRef, f identityProbeFateFacts) string {
	text, tracked := f.Text[co.Path]
	switch {
	case !tracked:
		return fmt.Sprintf("координата %s указывает в %s, которого в индексе нет: довод ссылается "+
			"на то, чего в дереве не существует", co.Raw, co.Path)
	case co.From < 1 || co.To < co.From || co.To > len(text):
		return fmt.Sprintf("координата %s вне файла %s (строк в нём %d): довод описывает уже другое "+
			"содержимое, перечитайте пробу и назовите строку заново", co.Raw, co.Path, len(text))
	}
	return ""
}

// judgeIdentityProbeAnchors — требование 6: координата стоит на своём
// предмете. Судится ключом координаты, а не её вхождением: одна координата,
// записанная в документе четырежды, — одна находка с перечнем строк документа.
func judgeIdentityProbeAnchors(l identityProbeFateLedger, f identityProbeFateFacts, resolved []identityProbeCoordRef,
	mentioned map[identityProbeCoordKey]bool, ledgerFile string, c *identityProbeFateCensus) []string {
	var found []string
	anchors := map[identityProbeCoordKey]identityProbeCoordAnchor{}
	for _, a := range l.Anchors {
		if a.Unreadable != "" {
			found = append(found, fmt.Sprintf("%s:%d: строка таблицы якорей %s не читается — %s",
				ledgerFile, a.Line, a.Raw, a.Unreadable))
			continue
		}
		if prev, dup := anchors[a.Key]; dup {
			found = append(found, fmt.Sprintf("%s:%d: якорь %s назван второй раз (первый — строка %d) — "+
				"два якоря одной координаты, и действующим оказался бы прочитанный последним",
				ledgerFile, a.Line, a.Raw, prev.Line))
			continue
		}
		if !mentioned[a.Key] {
			found = append(found, fmt.Sprintf("%s:%d: якорь %s пережил свою координату — в документе её "+
				"нет; снимите строку якоря тем же изменением, что координату", ledgerFile, a.Line, a.Raw))
		}
		c.Anchors++
		anchors[a.Key] = a
	}

	type use struct {
		raw   string
		lines []int
	}
	uses := map[identityProbeCoordKey]*use{}
	var order []identityProbeCoordKey
	for _, co := range resolved {
		k := co.key()
		if uses[k] == nil {
			uses[k] = &use{raw: co.Raw}
			order = append(order, k)
		}
		uses[k].lines = append(uses[k].lines, co.Line)
	}
	for _, k := range order {
		u := uses[k]
		at := fmt.Sprintf("%s:%d", ledgerFile, u.lines[0])
		name := fmt.Sprintf("координата %s (%s)", u.raw, k)
		if len(u.lines) > 1 {
			rest := make([]string, len(u.lines)-1)
			for i, n := range u.lines[1:] {
				rest[i] = strconv.Itoa(n)
			}
			name += ", записанная ещё на строках документа " + strings.Join(rest, ", ") + ","
		}
		a, ok := anchors[k]
		if !ok {
			found = append(found, fmt.Sprintf("%s: %s без якоря — в таблице якорей нет строки `%s`: без "+
				"якоря сдвиг файла под координатой не виден. Допишите текст первой строки предмета "+
				"(и у диапазона — последней)", at, name, k))
			continue
		}
		text := f.Text[k.Path]
		first := linesWith(text, a.First)
		if !strings.Contains(text[k.From-1], a.First) {
			found = append(found, fmt.Sprintf("%s: %s сошла со своего предмета: на строке %d якоря «%s» нет; %s",
				at, name, k.From, a.First, whereAnchor(first)))
			continue
		}
		onSubject := true
		switch {
		case k.To == k.From && a.HasLast:
			found = append(found, fmt.Sprintf("%s:%d: у одиночной координаты %s якорь последней строки — "+
				"конца у неё нет, пишите «%s»", ledgerFile, a.Line, a.Raw, identityProbeAnchorNone))
			onSubject = false
		case k.To != k.From && !a.HasLast:
			found = append(found, fmt.Sprintf("%s:%d: у диапазона %s нет якоря последней строки — рост "+
				"предмета внутри диапазона прошёл бы молча", ledgerFile, a.Line, a.Raw))
			onSubject = false
		case k.To != k.From && !strings.Contains(text[k.To-1], a.Last):
			found = append(found, fmt.Sprintf("%s: %s: конец диапазона сошёл с предмета — на строке %d якоря "+
				"последней строки «%s» нет; %s", at, name, k.To, a.Last, whereAnchor(linesWith(text, a.Last))))
			onSubject = false
		}
		if onSubject {
			c.OnSubject++
			if len(first) > 1 {
				c.SharedAnchors++
			}
		}
	}
	return found
}

// judgeIdentityProbeFate — шесть требований шапки. Пусто = норма.
func judgeIdentityProbeFate(l identityProbeFateLedger, f identityProbeFateFacts, probeDir, ledgerFile string) ([]string, identityProbeFateCensus) {
	c := identityProbeFateCensus{Rows: len(l.Rows), Probes: len(f.Probes), ByFate: map[string]int{}}
	var found []string
	at := func(r identityProbeFateRow) string { return fmt.Sprintf("%s:%d", ledgerFile, r.Line) }

	inTree := map[string]bool{}
	for _, p := range f.Probes {
		inTree[p] = true
	}

	// mentioned — каждая координата документа, судимая или нет: якорь без неё
	// пережил свою координату. resolved — те, что указывают в строки файла:
	// только у них есть строка, на которой якорю стоять.
	mentioned := map[identityProbeCoordKey]bool{}
	for _, r := range l.Rows {
		for _, co := range r.Coords {
			mentioned[co.key()] = true
		}
	}
	for _, co := range l.Elsewhere {
		mentioned[co.key()] = true
	}
	var resolved []identityProbeCoordRef

	named := map[string]int{}
	for _, r := range l.Rows {
		if r.Malformed {
			found = append(found, fmt.Sprintf("%s: строка ведомости несёт не %d ячеек — "+
				"разбор не знает, где в ней файл, исход и довод, и судить её нечем",
				at(r), len(identityProbeFateHeader)))
			continue
		}
		if !identityProbeName.MatchString(r.File) {
			found = append(found, fmt.Sprintf("%s: ячейка файла %q — не одно имя пробы "+
				"`identity_<имя>_test.go`. Групповая запись (образец, перечень, каталог) "+
				"снова прячет за признаком файлы, которых поимённо не назвал никто: "+
				"судьба выводится по файлу, одна строка — один файл", at(r), r.File))
			continue
		}
		if prev, dup := named[r.File]; dup {
			found = append(found, fmt.Sprintf("%s: %s назван ведомостью второй раз (первый — "+
				"строка %d) — два исхода об одной пробе, и действующим оказался бы прочитанный "+
				"последним", at(r), r.File, prev))
			continue
		}
		named[r.File] = r.Line

		if !inTree[r.File] {
			found = append(found, fmt.Sprintf("%s: строка о %s, а файла %s/%s в индексе нет — "+
				"запись пережила свою пробу. Снята проба — снимите строку тем же изменением",
				at(r), r.File, probeDir, r.File))
		}

		if knownFate(r.Fate) {
			c.ByFate[r.Fate]++
		} else {
			found = append(found, fmt.Sprintf("%s: %s — исход %q вне закрытого словаря %v. "+
				"Четвёртого исхода и корзины «прочее» нет", at(r), r.File, r.Fate, identityProbeFates))
		}

		own := probeDir + "/" + r.File
		ownSeen := false
		for _, co := range r.Coords {
			c.Coords++
			if why := resolveCoord(co, f); why != "" {
				found = append(found, fmt.Sprintf("%s: %s — %s", at(r), r.File, why))
				continue
			}
			resolved = append(resolved, co)
			if co.Path == own {
				ownSeen = true
				c.OwnCoords++
			}
		}
		if !ownSeen {
			found = append(found, fmt.Sprintf("%s: %s — у строки нет ни одной резолвящейся "+
				"координаты в самой пробе (`:<строка>` либо `%s:<строка>`). Довод — то, что "+
				"проба утверждает, и он обязан указывать в неё", at(r), r.File, own))
		}
	}

	for _, co := range l.Elsewhere {
		c.OtherCoords++
		if why := resolveCoord(co, f); why != "" {
			found = append(found, fmt.Sprintf("%s:%d: вне строк ведомости — %s", ledgerFile, co.Line, why))
			continue
		}
		resolved = append(resolved, co)
	}
	found = append(found, judgeIdentityProbeAnchors(l, f, resolved, mentioned, ledgerFile, &c)...)

	var missing []string
	for _, p := range f.Probes {
		if _, ok := named[p]; !ok {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	for _, p := range missing {
		found = append(found, fmt.Sprintf("%s/%s в индексе есть, а строки о нём в %s нет — "+
			"судьба пробы не названа: прочитайте её и назовите исход с доводом",
			probeDir, p, ledgerFile))
	}

	for _, u := range l.TotalsUnreadable {
		found = append(found, fmt.Sprintf("%s: разбивка по исходам не читается — %s", ledgerFile, u))
	}
	for key, line := range l.TotalsLine {
		if !knownFate(key) {
			found = append(found, fmt.Sprintf("%s:%d: в разбивке исход %q вне закрытого словаря %v",
				ledgerFile, line, key, identityProbeFates))
		}
	}
	for _, fate := range identityProbeFates {
		written, ok := l.Totals[fate]
		switch {
		case !ok:
			found = append(found, fmt.Sprintf("%s: в разбивке нет строки исхода %q — у словаря "+
				"три исхода, и ноль тоже пишется числом", ledgerFile, fate))
		case written != c.ByFate[fate]:
			found = append(found, fmt.Sprintf("%s:%d: разбивка пишет «%s %d», а строк ведомости с "+
				"этим исходом %d — число в документе разошлось с ведомостью под ним",
				ledgerFile, l.TotalsLine[fate], fate, written, c.ByFate[fate]))
		}
	}
	switch {
	case l.Total < 0:
		found = append(found, fmt.Sprintf("%s: в разбивке нет итоговой строки %q", ledgerFile, identityProbeFateTotalRow))
	case l.Total != len(f.Probes):
		found = append(found, fmt.Sprintf("%s: итог разбивки %d, а файлов %s/identity_*_test.go "+
			"в индексе %d — сумма по исходам обязана равняться числу проб", ledgerFile, l.Total,
			probeDir, len(f.Probes)))
	}

	sort.Strings(found)
	return found, c
}
