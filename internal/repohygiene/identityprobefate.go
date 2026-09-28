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
//
// ─────────────────────────────────────────────────────────────────────────────
// ФОРМЫ ЗАПИСИ, КОТОРЫЕ РАЗБОР ЗНАЕТ, — ПЕРЕЧИСЛЕНЫ, И ПРОЧИХ НЕТ
//
// Ведомость — таблица Markdown с заголовком `identityProbeFateHeader` (ровно
// одна в документе) и таблица разбивки с заголовком `identityProbeFateTotals`.
// Координата — фрагмент в обратных кавычках в ячейке довода, двух форм:
//
//	`:<строка>[-<строка>]`         в самой пробе строки
//	`<путь>:<строка>[-<строка>]`   путь от корня репозитория
//
// Фрагмент ТОЙ ЖЕ ФОРМЫ, который не резолвится, — находка, а не молчание:
// иначе всё записанное в незнакомом написании ушло бы из-под наблюдения.
// Фрагменты другой формы (имена ключей, ручек) координатами не являются и не
// читаются.
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

// identityProbeCoord — координата в обратных кавычках: путь (пустой = сама
// проба строки) и строка либо диапазон строк.
var identityProbeCoord = regexp.MustCompile("`([A-Za-z0-9_./-]*):([0-9]+)(?:-([0-9]+))?`")

// identityProbeCodeSpan — фрагмент в обратных кавычках.
var identityProbeCodeSpan = regexp.MustCompile("`([^`]*)`")

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
)

// identityProbeCoordRef — одна прочитанная координата строки.
type identityProbeCoordRef struct {
	// Path — путь от корня; для формы `:<строка>` — путь самой пробы строки.
	Path string
	// From, To — диапазон строк; у одиночной строки From == To.
	From, To int
	// Raw — фрагмент, как записан.
	Raw string
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
}

// identityProbeFateFacts — дерево, против которого судится ведомость.
// Отделено от чтения диска ради инъекции.
type identityProbeFateFacts struct {
	// Probes — имена файлов `deploy/identity_*_test.go` индекса git.
	Probes []string
	// Lines — число строк отслеживаемого файла по пути от корня. Пути нет —
	// файл не отслеживается.
	Lines map[string]int
}

// identityProbeFateCensus — объём осмотренного: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
type identityProbeFateCensus struct {
	Rows      int
	Probes    int
	ByFate    map[string]int
	Coords    int
	OwnCoords int
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
	parts = append(parts, fmt.Sprintf("координат проверено %d (в самой пробе %d)", c.Coords, c.OwnCoords))
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

	var ledgerAt, totalsAt []int
	for i, line := range lines {
		cells := splitMarkdownRow(line)
		switch {
		case sameCells(cells, identityProbeFateHeader):
			ledgerAt = append(ledgerAt, i)
		case sameCells(cells, identityProbeFateTotals):
			totalsAt = append(totalsAt, i)
		}
	}
	switch {
	case len(ledgerAt) == 0:
		return l, errIdentityProbeFateNoLedger
	case len(ledgerAt) > 1:
		return l, errIdentityProbeFateTwoLedgers
	case len(totalsAt) == 0:
		return l, errIdentityProbeFateNoTotals
	}

	rows, rowLines := tableAfter(lines, ledgerAt[0])
	for i, cells := range rows {
		r := identityProbeFateRow{Line: rowLines[i]}
		if len(cells) != len(identityProbeFateHeader) {
			r.Malformed = true
			l.Rows = append(l.Rows, r)
			continue
		}
		r.File = strings.Trim(cells[1], "` ")
		r.Fate = plainCell(cells[4])
		own := probeDir + "/" + r.File
		for _, m := range identityProbeCoord.FindAllStringSubmatch(cells[5], -1) {
			c := identityProbeCoordRef{Path: m[1], Raw: m[0]}
			if c.Path == "" {
				c.Path = own
			}
			c.From, _ = strconv.Atoi(m[2])
			c.To = c.From
			if m[3] != "" {
				c.To, _ = strconv.Atoi(m[3])
			}
			r.Coords = append(r.Coords, c)
		}
		l.Rows = append(l.Rows, r)
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

// knownFate — исход из закрытого словаря.
func knownFate(f string) bool {
	for _, k := range identityProbeFates {
		if f == k {
			return true
		}
	}
	return false
}

// judgeIdentityProbeFate — пять требований шапки. Пусто = норма.
func judgeIdentityProbeFate(l identityProbeFateLedger, f identityProbeFateFacts, probeDir, ledgerFile string) ([]string, identityProbeFateCensus) {
	c := identityProbeFateCensus{Rows: len(l.Rows), Probes: len(f.Probes), ByFate: map[string]int{}}
	var found []string
	at := func(r identityProbeFateRow) string { return fmt.Sprintf("%s:%d", ledgerFile, r.Line) }

	inTree := map[string]bool{}
	for _, p := range f.Probes {
		inTree[p] = true
	}

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
			n, tracked := f.Lines[co.Path]
			switch {
			case !tracked:
				found = append(found, fmt.Sprintf("%s: %s — координата %s указывает в %s, "+
					"которого в индексе нет: довод ссылается на то, чего в дереве не существует",
					at(r), r.File, co.Raw, co.Path))
				continue
			case co.From < 1 || co.To < co.From || co.To > n:
				found = append(found, fmt.Sprintf("%s: %s — координата %s вне файла %s (строк в "+
					"нём %d): довод описывает уже другое содержимое, перечитайте пробу и "+
					"назовите строку заново", at(r), r.File, co.Raw, co.Path, n))
				continue
			}
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
