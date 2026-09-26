// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_dev_ceremony_band_test.go — полоса сборщика, отдающая адреса
// церемоний входа внешнему экрану, монтируется ТОЛЬКО там, где объявлен её
// адрес (задача #2733).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У одной полосы два объявления: раздача стенда (`configmap-nginx.yaml`) и
// карта проксирования сборщика в разработке (`vite.config.ts`). Сосед по
// пакету (`TestServingBandCoversEveryRouteTheConsoleSendsToTheIdentityService`)
// сверяет их СОСТАВ — множества сегментов. Об УСЛОВИИ монтирования он не
// говорит ничего, а условия у двух объявлений разошлись:
//
//   - раздача ставит полосу лишь там, где посадка объявила адрес внешнего
//     экрана (`host.upstreams.kratosUi`), и не ставит там, где не объявила;
//     там адреса церемоний достаются оболочке консоли;
//   - сборщик ставил её ВСЕГДА, а адрес при необъявленной переменной брал из
//     умолчания — порта, на котором в разработке не слушает никто.
//
// Цена измерена исходом, а не чтением: загрузчиком самого сборщика
// (`loadConfigFromFile`, как его читает `vite`) при необъявленной переменной
// карта проксирования `host` и `dashboard` отдавала мимо консоли пять адресов
// церемоний (`/login`, `/registration`, `/settings`, `/error`, `/logout`) —
// на порт, где никого нет. Экраны церемоний консоли на этих адресах в
// разработке недостижимы by construction, и на стенде при той же посадке
// ведут себя иначе, чем в разработке: решал это никто.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО УТВЕРЖДАЕТСЯ
//
// По КАЖДОЙ конфигурации сборщика, объявляющей полосу, два факта, и оба нужны:
//
//  1. у адреса полосы НЕТ непустого умолчания — только переменная окружения
//     (умолчание делает условие ниже тождественно-истинным);
//  2. полоса смонтирована ПОД УСЛОВИЕМ этого адреса — пустое место в карте
//     там, где адрес не объявлен.
//
// Законных форм условия ДВЕ, и обе названы здесь и проверены инъекцией:
// `...(адрес ? Object.fromEntries(…) : {})` и `...(адрес && Object.fromEntries(…))`.
// Безусловная форма `...Object.fromEntries(…)` — находка. Всё прочее —
// ОТКАЗ разбора, а не молчание: форма, которую разбор не понимает, дала бы
// не красное и не зелёное, а пропуск.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ПРОБА ЧИТАЕТ И ЧЕГО НЕ УТВЕРЖДАЕТ
//
// Читается ОБЪЯВЛЕНИЕ — исполняемая часть `vite.config.ts` со снятыми
// комментариями, по составу индекса git. Исполнить конфигурацию здесь нечем:
// загрузчику нужен установленный сборщик, которого у набора Go-проб нет, а
// проба, зависящая от него, пропускалась бы молча. Поэтому предел назван: это
// утверждение о форме объявления, а исход (что уходит мимо консоли) измерен
// загрузчиком один раз и записан выше.
//
// Какая полоса — полоса экранов входа, решает ОДИН распознаватель
// (`devProxyBandRe` соседа): второго места об этом не заводится.
//
// Полосу объявляет ноль конфигураций — это ЦЕЛЬ снятия (#2733), а не отказ:
// проба проходит и печатает «объявляют 0». Ноль ПРОЧИТАННЫХ конфигураций —
// отказ: «находок 0» тогда значило бы «прочитано 0».
package deploy_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// devBandForm — форма монтирования полосы.
type devBandForm string

const (
	devBandBare    devBandForm = "безусловная"           // ...Object.fromEntries(…)
	devBandTernary devBandForm = "условная (… ? … : {})" // ...(адрес ? Object.fromEntries(…) : {})
	devBandAnd     devBandForm = "условная (… && …)"     // ...(адрес && Object.fromEntries(…))
)

// devBand — что разбор прочёл в одной конфигурации сборщика.
type devBand struct {
	// declared — полоса объявлена (распознаватель соседа нашёл перечень путей).
	declared bool
	// list — имя перечня путей полосы; target — константа-адресат;
	// knob — переменная окружения, из которой адресат берётся.
	list, target, knob string
	// fallback — непустое умолчание адресата («» — умолчания нет).
	fallback string
	// form — как полоса смонтирована.
	form devBandForm
	// initSpan — инициализатор адресата; callSpan — `Object.fromEntries(…)`;
	// mountSpan — всё смонтированное выражение после `...`. Позиции — в
	// исходном тексте; ими правит инъекция, а не поиском по образцу.
	initSpan, callSpan, mountSpan [2]int
	// declLine, mountLine — строки объявления адресата и монтирования (с 1).
	declLine, mountLine int
}

var (
	// identifierRe — имя в начале текста.
	identifierRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*`)
	// devBandTargetRe — адресат записи карты проксирования.
	devBandTargetRe = regexp.MustCompile(`target\s*:\s*([A-Za-z_$][\w$]*)`)
	// devEnvInitRe — инициализатор из переменной окружения с необязательным
	// умолчанием после `||` либо `??`.
	devEnvInitRe = regexp.MustCompile(`^process\.env\.([A-Za-z_][A-Za-z0-9_]*)(?:\s*(\|\||\?\?)\s*(.+?))?$`)
	// devEmptyLiteralRe — пустой строковый литерал: умолчание, которое условие
	// читает как «не объявлен».
	devEmptyLiteralRe = regexp.MustCompile("^(?:\"\"|''|``)$")
	// devTernaryTailRe / devAndTailRe — хвост условной формы после
	// `Object.fromEntries(…)`.
	devTernaryTailRe = regexp.MustCompile(`^\s*:\s*\{\s*\}\s*\)`)
	devAndTailRe     = regexp.MustCompile(`^\s*\)`)
)

// stripTSComments — исполняемая часть исходника: комментарии `//…` и `/*…*/`
// заменены пробелами, переводы строк сохранены (номера строк не сдвигаются).
// Строковые литералы в двойных, одинарных и обратных кавычках и литералы
// регулярных выражений проходят нетронутыми: `//` внутри адреса `http://…` или
// внутри `/a\/\/b/` комментарием не является. Литерал регулярного выражения
// опознаётся по месту: косая черта там, где ожидается значение (после скобки,
// запятой, знака операции либо в начале строки), — там делением она быть не
// может.
func stripTSComments(src string) string {
	out := []byte(src)
	var quote byte
	// prev — последний значимый символ исполняемой части: по нему косая черта
	// различается как деление либо как начало регулярного выражения.
	var prev byte
	for i := 0; i < len(out); i++ {
		c := out[i]
		switch {
		case quote != 0:
			if c == '\\' && i+1 < len(out) {
				i++
				continue
			}
			if c == quote {
				quote = 0
				prev = c
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '/' && i+1 < len(out) && out[i+1] != '/' && out[i+1] != '*' && regexMayStartAfter(prev):
			i = skipRegexLiteral(out, i)
			prev = '/'
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			for ; i < len(out) && out[i] != '\n'; i++ {
				out[i] = ' '
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			end := strings.Index(string(out[i+2:]), "*/")
			stop := len(out)
			if end >= 0 {
				stop = i + 2 + end + 2
			}
			for ; i < stop; i++ {
				if out[i] != '\n' {
					out[i] = ' '
				}
			}
			i--
		case c != ' ' && c != '\t' && c != '\n' && c != '\r':
			prev = c
		}
	}
	return string(out)
}

// regexMayStartAfter — может ли после этого символа начинаться литерал
// регулярного выражения (там ожидается значение, а не деление).
func regexMayStartAfter(prev byte) bool {
	return prev == 0 || strings.IndexByte("(,=:[!&|?{};+-*%<>~^", prev) >= 0
}

// skipRegexLiteral — позиция закрывающей косой черты литерала, начатого в
// `open`; класс символов `[…]` и экранирование учитываются. Литерал не
// переходит строку: без закрытия в той же строке это деление, и позиция
// возвращается прежней.
func skipRegexLiteral(src []byte, open int) int {
	inClass := false
	for i := open + 1; i < len(src) && src[i] != '\n'; i++ {
		switch c := src[i]; {
		case c == '\\':
			i++
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			return i
		}
	}
	return open
}

// matchingParen — позиция закрывающей скобки для открывающей в `open`
// (строковые литералы пропускаются); -1, если пары нет.
func matchingParen(code string, open int) int {
	depth := 0
	var quote byte
	for i := open; i < len(code); i++ {
		c := code[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// lineOf — номер строки (с 1) позиции в тексте.
func lineOf(text string, pos int) int { return strings.Count(text[:pos], "\n") + 1 }

// readDevCeremonyBand — разбор одной конфигурации сборщика. Ошибка — форма,
// которую разбор не понимает: она обязана краснеть, а не пропускаться.
func readDevCeremonyBand(src string) (devBand, error) {
	code := stripTSComments(src)
	var b devBand

	decl := devProxyBandRe.FindStringIndex(code)
	if decl == nil {
		return b, nil
	}
	b.declared = true
	b.list = identifierRe.FindString(code[decl[0]:])

	// Монтирование: единственный вызов `<перечень>.map(`.
	mapRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(b.list) + `\s*\.\s*map\s*\(`)
	maps := mapRe.FindAllStringIndex(code, -1)
	if len(maps) != 1 {
		return b, fmt.Errorf("перечень путей %q смонтирован %d раз(а), ожидался ровно один вызов `.map(` — "+
			"форма разбору неизвестна", b.list, len(maps))
	}
	mapOpen := maps[0][1] - 1
	mapClose := matchingParen(code, mapOpen)
	if mapClose < 0 {
		return b, fmt.Errorf("у вызова `%s.map(` нет закрывающей скобки", b.list)
	}
	targets := devBandTargetRe.FindAllStringSubmatch(code[mapOpen:mapClose], -1)
	if len(targets) != 1 {
		return b, fmt.Errorf("в записях полосы адресат назван %d раз(а), ожидался один `target: <имя>`", len(targets))
	}
	b.target = targets[0][1]

	// Обёртка `Object.fromEntries(` вокруг `.map(` — ближайшая слева.
	callRe := regexp.MustCompile(`Object\s*\.\s*fromEntries\s*\(\s*$`)
	callAt := callRe.FindStringIndex(code[:maps[0][0]])
	if callAt == nil {
		return b, fmt.Errorf("записи полосы собраны не через `Object.fromEntries(%s.map(…))` — форма разбору неизвестна", b.list)
	}
	callOpen := strings.LastIndex(code[:maps[0][0]], "(")
	callClose := matchingParen(code, callOpen)
	if callClose < 0 {
		return b, errors.New("у `Object.fromEntries(` нет закрывающей скобки")
	}
	b.callSpan = [2]int{callAt[0], callClose + 1}
	b.mountLine = lineOf(code, callAt[0])

	// Условие монтирования — текст между `...` и вызовом.
	before := code[:callAt[0]]
	quotedTarget := regexp.QuoteMeta(b.target)
	bareRe := regexp.MustCompile(`\.\.\.\s*$`)
	ternRe := regexp.MustCompile(`\.\.\.\s*(\()\s*` + quotedTarget + `\s*\?\s*$`)
	andRe := regexp.MustCompile(`\.\.\.\s*(\()\s*` + quotedTarget + `\s*&&\s*$`)
	after := code[b.callSpan[1]:]
	switch {
	case bareRe.MatchString(before):
		b.form = devBandBare
		b.mountSpan = b.callSpan
	case ternRe.MatchString(before):
		tail := devTernaryTailRe.FindStringIndex(after)
		if tail == nil {
			return b, fmt.Errorf("условная форма `...(%s ? …)` без пустой ветви `: {}` — вторая ветвь монтировала бы "+
				"что-то там, где адрес не объявлен; форма разбору неизвестна", b.target)
		}
		m := ternRe.FindStringSubmatchIndex(before)
		b.form = devBandTernary
		b.mountSpan = [2]int{m[2], b.callSpan[1] + tail[1]}
	case andRe.MatchString(before):
		tail := devAndTailRe.FindStringIndex(after)
		if tail == nil {
			return b, fmt.Errorf("условная форма `...(%s && …)` не закрыта сразу после вызова — форма разбору неизвестна", b.target)
		}
		m := andRe.FindStringSubmatchIndex(before)
		b.form = devBandAnd
		b.mountSpan = [2]int{m[2], b.callSpan[1] + tail[1]}
	default:
		return b, fmt.Errorf("полоса смонтирована не через `...` ни в одной из известных форм (безусловная; "+
			"`...(%[1]s ? … : {})`; `...(%[1]s && …)`) — форма разбору неизвестна", b.target)
	}

	// Объявление адресата: единственное, одной строкой, из переменной окружения.
	declRe := regexp.MustCompile(`(?m)^[ \t]*(?:const|let|var)[ \t]+` + quotedTarget + `[ \t]*=[ \t]*([^;\n]*?)[ \t]*;?[ \t]*$`)
	decls := declRe.FindAllStringSubmatchIndex(code, -1)
	if len(decls) != 1 {
		return b, fmt.Errorf("адресат полосы %q объявлен %d раз(а) одной строкой, ожидалось одно объявление", b.target, len(decls))
	}
	b.initSpan = [2]int{decls[0][2], decls[0][3]}
	b.declLine = lineOf(code, decls[0][0])
	initText := code[b.initSpan[0]:b.initSpan[1]]
	env := devEnvInitRe.FindStringSubmatch(initText)
	if env == nil {
		return b, fmt.Errorf("адресат полосы %q берётся не из переменной окружения (`%s`) — форма разбору неизвестна", b.target, initText)
	}
	b.knob = env[1]
	if env[2] != "" && !devEmptyLiteralRe.MatchString(env[3]) {
		b.fallback = env[3]
	}
	return b, nil
}

// defects — находки по одной конфигурации; пусто — полоса смонтирована законно.
func (b devBand) defects(rel string) []string {
	var out []string
	if b.fallback != "" {
		out = append(out, fmt.Sprintf("%s:%d: у адреса полосы экранов входа (`%s`) умолчание %s — при "+
			"необъявленной `%s` полоса уходит на этот адрес, и условие её монтирования истинно всегда. "+
			"Раздача стенда ставит полосу только при объявленном адресе; в разработке обязано быть так же",
			rel, b.declLine, b.target, b.fallback, b.knob))
	}
	if b.form == devBandBare {
		out = append(out, fmt.Sprintf("%s:%d: полоса экранов входа (`%s`) смонтирована БЕЗУСЛОВНО — адреса "+
			"церемоний уходят мимо консоли и там, где адрес внешнего экрана не объявлен, и экраны консоли на них "+
			"в разработке недостижимы. Законно: `...(%s ? Object.fromEntries(…) : {})` либо `...(%s && …)`",
			rel, b.mountLine, b.list, b.target, b.target))
	}
	return out
}

// TestDevCeremonyBandIsMountedOnlyWhereItsAddressIsDeclared — каждая
// конфигурация сборщика, объявляющая полосу экранов входа, монтирует её только
// при объявленном адресе и без умолчания адреса.
func TestDevCeremonyBandIsMountedOnlyWhereItsAddressIsDeclared(t *testing.T) {
	root := repoRootFromTest(t)
	files, err := treecorpus.Under(filepath.Join(root, "ui-future"))
	if err != nil {
		t.Fatalf("состав ui-future: %v — без индекса «ноль находок» неотличимо от «ноль прочитанного»", err)
	}
	read, declaring, findings := 0, []string{}, []string{}
	for _, abs := range files {
		if filepath.Base(abs) != "vite.config.ts" {
			continue
		}
		body, rerr := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git этого дерева
		if rerr != nil {
			t.Fatalf("%s: %v", abs, rerr)
		}
		read++
		rel, _ := filepath.Rel(root, abs)
		b, perr := readDevCeremonyBand(string(body))
		if perr != nil {
			t.Errorf("%s: %v", rel, perr)
			continue
		}
		if !b.declared {
			continue
		}
		declaring = append(declaring, fmt.Sprintf("%s (%s)", rel, b.form))
		findings = append(findings, b.defects(rel)...)
	}
	t.Logf("осмотрено конфигураций сборщика %d; полосу экранов входа объявляют %d %v; находок %d",
		read, len(declaring), declaring, len(findings))
	if read == 0 {
		t.Fatal("не прочитано ни одной конфигурации сборщика — «находок 0» значило бы «прочитано 0»")
	}
	for _, f := range findings {
		t.Error(f)
	}
}
