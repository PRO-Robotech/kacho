// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_misc_authz_window.go — страница, отправляющая клиента ждать
// authz-видимость, обязана назвать СВОЮ величину этого окна и назвать её верно.
//
// ПРЕДМЕТ (задача продукта #1645). Окно, после которого выдача становится
// наблюдаемой, складывается из ДВУХ слагаемых: материализация выдачи у владельца
// прав и кэш вердиктов у того, кто спрашивает. Второе слагаемое — собственность
// спрашивающего: у него своя ручка и своё умолчание. Спрашивающий называл только
// ПЕРВОЕ, да ещё числом, у которого в дереве нет производителя, — и клиент,
// отлаживающий свежую выдачу по этой странице, ждал меньше, чем нужно, и
// заключал, что доступ не выдался.
//
// ВЛАДЕЛЬЦЕВ ОКНА В ДЕРЕВЕ ДВА, и это не совпадение, а класс: всякий, кто кэширует
// положительный вердикт, заводит своё слагаемое. Край назвал половину — и registry
// назвал половину, причём числом СНЯТОГО механизма: «~0.6–2 с (распространение
// прав)» была ценой очереди к внешнему хранилищу отношений, которого в
// развёртывании больше нет. Гейт поэтому обходит ПЕРЕЧЕНЬ владельцев, а не один
// каталог: третий, заведя кэш, обязан покраснеть в тот же день.
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
//     спрятано: страница с таким написанием этому гейту невидима;
//   - КОРПУС ПРОБ. Обход — каталоги клиентских страниц владельцев, и на дерево
//     проб он не распространён РЕШЕНИЕМ, а не по умолчанию (задача продукта
//     #1730). Довод тот же, что у предыдущего пункта, и он о грамматике: обе
//     половины гейта предполагают СТРАНИЦУ («названа ли ручка», «совпала ли
//     величина с умолчанием»), а распознаёт он разметку и оборот «по умолчанию
//     N». Комментарий оболочки и комментарий генератора случаев страницами не
//     являются и клиента ждать не отправляют — они обосновывают БЮДЖЕТ повтора;
//     требовать от них этой грамматики значило бы завести вторую грамматику
//     внутри одного гейта.
//     Следствие названо, а не умолчано: у величины в корпусе проб держателя
//     здесь НЕТ. Её держит отдельный гейт со своим предметом — величина, у
//     которой в дереве нет производителя (ct3_registry_probe_window_value.go).
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

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// ct2WindowOwner — тот, кто кэширует вердикт и потому владеет своим слагаемым
// окна: его страницы, его ручка, его конфигурация.
type ct2WindowOwner struct {
	// Name — как владелец зовётся в переписи и в находке.
	Name string
	// DocsDir — каталог его клиентских страниц.
	DocsDir string
	// ConfigFile — где объявлено умолчание ручки.
	ConfigFile string
	// Knob — имя ручки, которой настраивается его слагаемое.
	Knob string
}

// ct2WindowOwners — ВСЕ, кто кэширует положительный вердикт и отправляет клиента
// ждать. Перечень выписан, а не выведен из дерева, и это осознанно: вывести его
// можно было бы только поиском по имени ручки, то есть предикатом, совпадающим с
// предметом проверки, — он подтверждал бы сам себя.
var ct2WindowOwners = []ct2WindowOwner{
	{
		Name:       "gateway",
		DocsDir:    "gateway/docs/content/",
		ConfigFile: "gateway/internal/config/config.go",
		Knob:       "KACHO_API_GATEWAY_AUTHZ_CACHE_TTL_SECONDS",
	},
	{
		Name:       "registry",
		DocsDir:    "services/registry/docs/content/",
		ConfigFile: "services/registry/internal/apps/kacho/config/config.go",
		Knob:       "KACHO_REGISTRY_AUTHZ_CACHE_TTL",
	},
}

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
	// Форма registry: врезка называет предмет своим именем.
	"grant-latency",
	"Grant-latency",
}

// ЧТО В ЭТОТ ПЕРЕЧЕНЬ НЕ ПОПАЛО И ПОЧЕМУ. Первая редакция несла ещё
// «authz-фильтрованном» — и немедленно дала ложную находку: страница обзора
// registry этой фразой ОПИСЫВАЕТ механизм («чтобы создатель видел реестр в
// authz-фильтрованном списке»), а не отправляет клиента ждать. Маркер обязан
// узнавать НАМЕРЕНИЕ страницы, а не её словарь; проверка, у которой ложные
// находки, перестаёт читаться — и вместе с ней перестают читаться настоящие.

// ct2KnobValueRe — число, названное рядом с ручкой: «… секунд (по умолчанию 5)»
// либо «…, по умолчанию 2 с».
var ct2KnobValueRe = regexp.MustCompile(`по умолчанию\s+(\d+)`)

// ct2DurationDefaultRe — умолчание, объявленное длительностью (`2s`), а не голым
// числом. Обе формы приводятся к СЕКУНДАМ: ручка называет окно ожидания, и
// сравнивать её с числом на странице иначе нечем. Форма, о которой
// распознаватель не знает, дала бы не находку, а невидимость (п.7 §«Гейт на
// класс»), поэтому обе названы здесь.
var ct2DurationDefaultRe = regexp.MustCompile(`^(\d+)s$`)

// ct2WindowPage — что найдено на ОДНОЙ странице края.
type ct2WindowPage struct {
	// Owner — чья это страница; находка обязана его называть.
	Owner string
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
	// Defaults — «владелец» → умолчание его ручки в секундах; "" — не найдено.
	Defaults   map[string]string
	Owners     []ct2WindowOwner
	Pages      []ct2WindowPage
	PagesRead  int
	SpeakingOf int
	NamingKnob int
	Agreeing   int
}

// collectAuthzWindow обходит конфигурацию и страницы края.
//
// Состав дерева берётся у ИНДЕКСА git, а не у диска: рядом со страницами лежат
// сборки сайта, и вердикт, собранный обходом файловой системы, стал бы
// свойством рабочего каталога, а не коммита.
func collectAuthzWindow(tree *treecorpus.Tree) (ct2WindowCensus, error) {
	c := ct2WindowCensus{
		Defaults: map[string]string{},
		Owners:   append([]ct2WindowOwner(nil), ct2WindowOwners...),
	}

	for _, o := range c.Owners {
		if !tree.HasFile(o.ConfigFile) {
			continue
		}
		v, err := ct2EnvDefault(filepath.Join(tree.Root(), filepath.FromSlash(o.ConfigFile)), o.Knob)
		if err != nil {
			return c, err
		}
		c.Defaults[o.Name] = ct2SecondsOf(v)
	}

	for _, rel := range tree.SortedFiles() {
		if !strings.HasSuffix(rel, ".mdx") {
			continue
		}
		owner := ct2WindowOwnerOf(rel, c.Owners)
		if owner == nil {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			return c, fmt.Errorf("чтение %s: %w", rel, err)
		}
		text := string(body)
		c.PagesRead++

		p := ct2WindowPage{
			Owner:     owner.Name,
			Rel:       rel,
			NamesKnob: strings.Contains(text, owner.Knob),
		}
		for _, m := range ct2WindowMarkers {
			if strings.Contains(text, m) {
				p.SpeaksOfWindow = true
				break
			}
		}
		if p.NamesKnob {
			for _, line := range ct2KnobNeighbourhood(text, owner.Knob) {
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
		if p.NamesKnob && len(p.StatedValues) > 0 && ct2AllEqual(p.StatedValues, c.Defaults[p.Owner]) {
			c.Agreeing++
		}
	}
	return c, nil
}

// ct2WindowOwnerOf — чья это страница; nil — ничья из названных.
func ct2WindowOwnerOf(rel string, owners []ct2WindowOwner) *ct2WindowOwner {
	for i := range owners {
		if strings.HasPrefix(rel, owners[i].DocsDir) {
			return &owners[i]
		}
	}
	return nil
}

// ct2SecondsOf приводит объявленное умолчание к СЕКУНДАМ.
//
// Ручки этого перечня объявляют окно ожидания и потому обе измеряются секундами:
// одна голым числом (поле названо `…Seconds`), другая длительностью (`2s`).
// Приведение названо, а не подразумевается: единица счёта — часть числа, и без
// неё сравнение страницы с конфигурацией было бы сравнением разных величин.
func ct2SecondsOf(raw string) string {
	if m := ct2DurationDefaultRe.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	return raw
}

// ct2KnobNeighbourhood — строки, в которых упомянута ручка, вместе с соседней
// снизу: величина часто переносится на следующую строку абзаца.
func ct2KnobNeighbourhood(text, knob string) []string {
	lines := strings.Split(text, "\n")
	var out []string
	for i, l := range lines {
		if !strings.Contains(l, knob) {
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
	// #nosec G304 -- путь собран из перечня владельцев в этом же модуле
	// (ct2WindowOwners, поле ConfigFile) и корня обхода, а его наличие вызывающий
	// проверил по индексу git (tree.HasFile); постороннего ввода тут нет.
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
	knobOf := map[string]ct2WindowOwner{}
	for _, o := range c.Owners {
		knobOf[o.Name] = o
	}
	var out []string
	for _, p := range c.Pages {
		o := knobOf[p.Owner]
		if p.SpeaksOfWindow && !p.NamesKnob {
			out = append(out, fmt.Sprintf(
				"%s: страница отправляет клиента ждать authz-видимость и не называет "+
					"%s — названа половина окна, а прочитана будет как целое",
				p.Rel, o.Knob))
			continue
		}
		if !p.NamesKnob {
			continue
		}
		// Величины требует только страница, ОТПРАВЛЯЮЩАЯ ждать. Страница
		// настройки перечисляет ручку таблицей, и величина у неё стоит ячейкой,
		// а не фразой; требовать от неё фразы значило бы краснеть на справочнике,
		// который делает ровно свою работу.
		if p.SpeaksOfWindow && len(p.StatedValues) == 0 {
			out = append(out, fmt.Sprintf(
				"%s: %s назван без величины — клиенту нечего заложить в повтор",
				p.Rel, o.Knob))
			continue
		}
		for _, v := range p.StatedValues {
			if v != c.Defaults[p.Owner] {
				out = append(out, fmt.Sprintf(
					"%s: названо умолчание %s, конфигурация владельца %s объявляет %s (%s) — "+
						"два места об одном предмете, и расходятся они молча",
					p.Rel, v, p.Owner, c.Defaults[p.Owner], o.ConfigFile))
			}
		}
	}
	// Владелец, чьё умолчание не выведено, — слепая зона, а не молчание.
	for _, o := range c.Owners {
		if c.Defaults[o.Name] == "" {
			out = append(out, fmt.Sprintf(
				"%s: умолчание %s не выведено из %s — сверять названное не с чем",
				o.Name, o.Knob, o.ConfigFile))
		}
	}
	return out
}
