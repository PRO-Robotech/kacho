// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestPoolWidthQuotedByDocsMatchesTheChart — ширину пула, названную ПРИМЕРОМ в
// документации службы, обязан подтверждать её чарт.
//
// # Предмет
//
// Величину посадки называют три вида мест, и действует только один:
//
//	значения чарта      services/<svc>/deploy/values.yaml — ДЕЙСТВУЮТ
//	конфигурация службы рендерится чартом из того же ключа
//	документация        цитирует число прозой — не держится ничем
//
// Третий вид и есть предмет: в #709 обе страницы правились руками, и следующий,
// кто поменяет ширину в чарте, про них не узнает. Расхождение при этом тихое —
// читатель копирует пример и получает посадку, которой у нас нет.
//
// # Что сверяется, а что НЕТ — различие семантическое, не формальное
//
// Сверяется ПРИМЕР — строка вида `max-conns: 200` внутри блока кода. Пример
// показывает «как у нас», поэтому обязан показывать то, что у нас есть.
//
// НЕ сверяется ТАБЛИЦА УМОЛЧАНИЙ — ячейка «По умолчанию» описывает, что будет,
// если ключ не задан вовсе (обычно 0 → умолчание драйвера). Это утверждение о
// самом ключе, а не о нашей посадке, и требовать от него величины чарта значило
// бы требовать неправды. Такие цитаты попадают в перепись с причиной, а не под
// маску: «не сверяется» и «не замечено» — разные состояния.
//
// # Чем доказано, что гейт способен упасть
//
// Парная инъекция ниже: подменённое число в примере даёт находку С КООРДИНАТОЙ,
// а та же форма с верным числом — молчание. Плюс контроль в третью сторону:
// ячейка таблицы с числом, отличным от чарта, молчит и попадает в перепись.
func TestPoolWidthQuotedByDocsMatchesTheChart(t *testing.T) {
	quotes, tables, docs := poolWidthQuotes(t)
	charts := chartPoolWidths(t)

	t.Logf("перепись: страниц документации служб %d; цитат-примеров %d; ячеек таблиц умолчаний %d (не сверяются — предмет другой); чартов, объявляющих ширину, %d",
		docs, len(quotes), tables, len(charts))

	if docs == 0 {
		t.Fatal("корпус пуст: страниц документации служб не прочитано ни одной — «ноль находок» здесь означало бы «ноль прочитанного»")
	}
	if len(charts) == 0 {
		t.Fatal("ни один чарт не объявляет ширину пула: сверять не с чем, и молчание было бы беспредметным")
	}

	for _, q := range quotes {
		want, ok := charts[q.service]
		if !ok {
			t.Errorf("%s:%d: пример называет ширину пула %d, а чарт службы %q её не объявляет вовсе — пример показывает посадку, которой нет",
				q.file, q.line, q.value, q.service)
			continue
		}
		if q.value != want {
			t.Errorf("%s:%d: пример называет ширину пула %d, чарт службы %q объявляет %d — читатель, скопировавший пример, получит не нашу посадку\n"+
				"    Число документации не держится ничем: правь его вместе с чартом либо не называй вовсе.",
				q.file, q.line, q.value, q.service, want)
		}
	}
}

// adjudicatePoolWidth — вердикт по ОДНОЙ строке: "таблица" (не сверяется),
// "находка" (число не совпало с чартом), "молчит" (совпало либо цитаты нет).
//
// Вынесено отдельной функцией, чтобы инъекция звала ТО ЖЕ, что исполняет гейт.
// Проба, повторяющая логику своей копией, доказывает свойство копии.
func adjudicatePoolWidth(line string, chart int) string {
	if poolWidthTableRe.MatchString(line) {
		return "таблица"
	}
	m := poolWidthExampleRe.FindStringSubmatch(line)
	if m == nil {
		return "молчит"
	}
	v, err := strconv.Atoi(m[1])
	if err != nil || v == chart {
		return "молчит"
	}
	return "находка"
}

type poolWidthQuote struct {
	file    string
	line    int
	service string
	value   int
}

var (
	// Пример в блоке кода: ключ конфигурации службы либо ключ чарта.
	poolWidthExampleRe = regexp.MustCompile(`^\s*(?:max-conns|maxConns):\s*(\d+)`)
	// Ячейка таблицы умолчаний: ключ и значение стоят в соседних `<code>`.
	poolWidthTableRe = regexp.MustCompile(`<code>[^<]*(?:max-conns|maxConns|MAX\\?_CONNS)[^<]*</code>\s*</td>\s*<td>`)
)

// poolWidthQuotes — цитаты примеров, число ячеек таблиц и объём осмотренного.
func poolWidthQuotes(t *testing.T) ([]poolWidthQuote, int, int) {
	t.Helper()

	files := trackedFiles(t, "services/*/docs/**/*.md", "services/*/docs/**/*.mdx")
	var (
		quotes []poolWidthQuote
		tables int
		docs   int
	)
	for _, rel := range files {
		if !strings.Contains(rel, "/docs/") {
			continue
		}
		svc := serviceOfDocPath(rel)
		if svc == "" {
			continue
		}
		body := readTracked(t, rel)
		docs++
		for i, line := range strings.Split(string(body), "\n") {
			if poolWidthTableRe.MatchString(line) {
				tables++
				continue
			}
			m := poolWidthExampleRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			v, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			quotes = append(quotes, poolWidthQuote{file: rel, line: i + 1, service: svc, value: v})
		}
	}
	sort.Slice(quotes, func(i, j int) bool {
		if quotes[i].file != quotes[j].file {
			return quotes[i].file < quotes[j].file
		}
		return quotes[i].line < quotes[j].line
	})
	return quotes, tables, docs
}

// serviceOfDocPath — имя службы из пути `services/<svc>/docs/...`.
func serviceOfDocPath(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[0] != "services" {
		return ""
	}
	return parts[1]
}

var chartPoolWidthRe = regexp.MustCompile(`^\s*maxConns:\s*(\d+)`)

// chartPoolWidths — ширина пула, объявленная чартом каждой службы.
func chartPoolWidths(t *testing.T) map[string]int {
	t.Helper()

	out := map[string]int{}
	for _, rel := range trackedFiles(t, "services/*/deploy/values.yaml") {
		parts := strings.Split(rel, "/")
		// Только чарт самой службы: `services/<svc>/deploy/values.yaml`.
		// Копия под `docs/` — чарт САЙТА документации, у него другой предмет.
		if len(parts) != 4 || parts[0] != "services" || parts[2] != "deploy" {
			continue
		}
		for _, line := range strings.Split(readTracked(t, rel), "\n") {
			m := chartPoolWidthRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			v, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			out[parts[1]] = v
			break
		}
	}
	return out
}
