// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_misc_authz_window.go — страница края, отправляющая клиента ждать
// authz-видимость, обязана назвать СВОЮ величину этого окна и назвать её верно.
//
// ПРЕДМЕТ (задача продукта #1645). Окно, после которого выдача становится
// наблюдаемой, складывается из ДВУХ слагаемых: материализация выдачи у владельца
// прав и кэш решений на самом крае. Второе слагаемое — собственность края: у
// него своя ручка и своё умолчание. Край называл только ПЕРВОЕ, да ещё числом,
// у которого в дереве нет производителя, — и клиент, отлаживающий свежую выдачу
// по странице края, ждал меньше, чем нужно, и заключал, что доступ не выдался.
//
// ЧТО ГЕЙТ ДЕРЖИТ — ДВЕ ПОЛОВИНЫ, и каждая ловит свою сторону дефекта:
//
//  1. НАЗВАНА ЛИ величина. Страница края, говорящая об authz-видимости и
//     отправляющая клиента повторять, обязана назвать ручку своего кэша
//     решений. Без этого она называет половину окна и выдаёт её за целое.
//  2. ВЕРНА ЛИ величина. Число, названное рядом с ручкой, обязано совпасть с
//     умолчанием, объявленным в конфигурации края. Иначе заводится второе место
//     об одном предмете, и расходится оно молча — правка умолчания страницы не
//     касается.
//
// ЧЕГО ГЕЙТ НЕ ДЕРЖИТ — названо, а не спрятано:
//
//   - что названо ВТОРОЕ слагаемое (материализация у владельца прав). Величины у
//     него край не знает и знать не обязан — она принадлежит IAM, и сумма
//     называется ОДНИМ местом, его документацией. Требовать её от края значило
//     бы завести второе изложение, которое разойдётся молча;
//   - правдивость самого умолчания — что кэш и вправду живёт столько. Это
//     свойство прогона, и держат его пробы кэша;
//   - имя ручки, записанное с ЭКРАНИРОВАННЫМИ подчёркиваниями (`&#95;`). Так она
//     стоит в таблице ручек на странице авторизации края — там величина лежит
//     ячейкой таблицы, а не фразой «по умолчанию N», и разбор ячеек был бы
//     распознавателем второй грамматики ради одного места. Названо, а не
//     спрятано: страница с таким написанием этому гейту невидима.
//
// ПОЧЕМУ УМОЛЧАНИЕ БЕРЁТСЯ РАЗБОРОМ, А НЕ ПОИСКОМ ПО ТЕКСТУ. Имя ручки стоит и в
// комментариях конфигурации, и в этой самой шапке; поиск по подстроке взял бы
// число из комментария рядом. Разбор судит ТЕГ структурного поля.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

const (
	// ct2AuthzCacheKnob — ручка, которой настраивается ВТОРОЕ слагаемое окна.
	ct2AuthzCacheKnob = "KACHO_API_GATEWAY_AUTHZ_CACHE_TTL_SECONDS"
	// ct2GatewayConfigFile — где объявлено её умолчание.
	ct2GatewayConfigFile = "gateway/internal/config/config.go"
	// ct2GatewayDocsDir — страницы края.
	ct2GatewayDocsDir = "gateway/docs/content/"
)

// ct2WindowMarkers — по чему опознаётся страница, отправляющая клиента ждать
// authz-видимость.
//
// Маркеров несколько НАМЕРЕННО: одна формулировка — это распознаватель, знающий
// одну форму записи предмета, и всё сказанное иначе осталось бы вне наблюдения
// (`testing.md` §«Гейт на класс», п.7).
var ct2WindowMarkers = []string{
	"authz-видимость",
	"authz-видимости",
	"видимость нового ресурса",
}

// ct2KnobValueRe — число, названное рядом с ручкой: «… секунд (по умолчанию 5)»
// либо «…, по умолчанию 5 с».
var ct2KnobValueRe = regexp.MustCompile(`по умолчанию\s+(\d+)`)

// ct2WindowPage — что найдено на ОДНОЙ странице края.
type ct2WindowPage struct {
	// Rel — координата страницы; находка обязана её называть.
	Rel string
	// SpeaksOfWindow — страница отправляет клиента ждать authz-видимость.
	SpeaksOfWindow bool
	// NamesKnob — страница называет ручку кэша решений края.
	NamesKnob bool
	// StatedValues — числа, названные рядом с ручкой.
	StatedValues []string
}

// ct2WindowCensus — перепись обхода. Печатается ВСЕГДА: «ноль находок» обязано
// быть отличимо от «ноль прочитанного».
type ct2WindowCensus struct {
	// DefaultValue — умолчание, объявленное конфигурацией края; "" — не найдено.
	DefaultValue string
	Pages        []ct2WindowPage
	PagesRead    int
	SpeakingOf   int
	NamingKnob   int
	Agreeing     int
}

// collectAuthzWindow обходит конфигурацию и страницы края.
//
// Состав дерева берётся у ИНДЕКСА git, а не у диска: рядом со страницами лежат
// сборки сайта, и вердикт, собранный обходом файловой системы, стал бы
// свойством рабочего каталога, а не коммита.
func collectAuthzWindow(tree *treecorpus.Tree) (ct2WindowCensus, error) {
	var c ct2WindowCensus

	if tree.HasFile(ct2GatewayConfigFile) {
		v, err := ct2EnvDefault(filepath.Join(tree.Root(), filepath.FromSlash(ct2GatewayConfigFile)), ct2AuthzCacheKnob)
		if err != nil {
			return c, err
		}
		c.DefaultValue = v
	}

	for _, rel := range tree.SortedFiles() {
		if !strings.HasPrefix(rel, ct2GatewayDocsDir) || !strings.HasSuffix(rel, ".mdx") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			return c, fmt.Errorf("чтение %s: %w", rel, err)
		}
		text := string(body)
		c.PagesRead++

		p := ct2WindowPage{Rel: rel, NamesKnob: strings.Contains(text, ct2AuthzCacheKnob)}
		for _, m := range ct2WindowMarkers {
			if strings.Contains(text, m) {
				p.SpeaksOfWindow = true
				break
			}
		}
		if p.NamesKnob {
			for _, line := range ct2KnobNeighbourhood(text) {
				if m := ct2KnobValueRe.FindStringSubmatch(line); m != nil {
					p.StatedValues = append(p.StatedValues, m[1])
				}
			}
		}
		if !p.SpeaksOfWindow && !p.NamesKnob {
			continue
		}
		c.Pages = append(c.Pages, p)
	}

	for _, p := range c.Pages {
		if p.SpeaksOfWindow {
			c.SpeakingOf++
		}
		if p.NamesKnob {
			c.NamingKnob++
		}
		if p.NamesKnob && len(p.StatedValues) > 0 && ct2AllEqual(p.StatedValues, c.DefaultValue) {
			c.Agreeing++
		}
	}
	return c, nil
}

// ct2KnobNeighbourhood — строки, в которых упомянута ручка, вместе с соседней
// снизу: величина часто переносится на следующую строку абзаца.
func ct2KnobNeighbourhood(text string) []string {
	lines := strings.Split(text, "\n")
	var out []string
	for i, l := range lines {
		if !strings.Contains(l, ct2AuthzCacheKnob) {
			continue
		}
		out = append(out, l)
		if i+1 < len(lines) {
			out = append(out, lines[i+1])
		}
	}
	return out
}

// ct2AllEqual — все названные величины совпали с умолчанием.
func ct2AllEqual(values []string, want string) bool {
	if want == "" {
		return false
	}
	for _, v := range values {
		if v != want {
			return false
		}
	}
	return true
}

// ct2EnvDefault читает умолчание ручки из ТЕГА структурного поля.
//
// Разбор, а не поиск по тексту: имя ручки стоит и в комментариях рядом, и
// подстрочный поиск взял бы число оттуда.
func ct2EnvDefault(path, knob string) (string, error) {
	// Путь собран из индекса дерева, а не из ввода вызывающего: подавления
	// анализатора здесь не требуется.
	src, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("чтение %s: %w", path, err)
	}
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, 0)
	if perr != nil {
		return "", fmt.Errorf("разбор %s: %w", path, perr)
	}
	var out string
	ast.Inspect(f, func(n ast.Node) bool {
		if out != "" {
			return false
		}
		field, ok := n.(*ast.Field)
		if !ok || field.Tag == nil {
			return true
		}
		raw, uerr := strconv.Unquote(field.Tag.Value)
		if uerr != nil {
			return true
		}
		tag := reflect.StructTag(raw)
		if tag.Get("envconfig") != knob {
			return true
		}
		out = tag.Get("default")
		return false
	})
	return out, nil
}

// authzWindowFindings — расхождения, каждое с координатой.
func authzWindowFindings(c ct2WindowCensus) []string {
	var out []string
	for _, p := range c.Pages {
		if p.SpeaksOfWindow && !p.NamesKnob {
			out = append(out, fmt.Sprintf(
				"%s: страница отправляет клиента ждать authz-видимость и не называет "+
					"%s — названа половина окна, а прочитана будет как целое",
				p.Rel, ct2AuthzCacheKnob))
			continue
		}
		if !p.NamesKnob {
			continue
		}
		if len(p.StatedValues) == 0 {
			out = append(out, fmt.Sprintf(
				"%s: %s назван без величины — клиенту нечего заложить в повтор",
				p.Rel, ct2AuthzCacheKnob))
			continue
		}
		for _, v := range p.StatedValues {
			if v != c.DefaultValue {
				out = append(out, fmt.Sprintf(
					"%s: названо умолчание %s, конфигурация края объявляет %s (%s) — "+
						"два места об одном предмете, и расходятся они молча",
					p.Rel, v, c.DefaultValue, ct2GatewayConfigFile))
			}
		}
	}
	return out
}
