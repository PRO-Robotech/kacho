// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Маршрут, показанный клиенту, обязан существовать в контракте.
//
// ПРЕДМЕТ. Справочник и быстрый старт — это то, по чему клиент ДЕЙСТВУЕТ. Адрес,
// который они показывают, есть обещание: «вызови это, и получится». Если контракт
// такого маршрута не производит, обещание неисполнимо, и узнаётся это единственным
// способом — вызовом. Край отвечает на неизвестный путь `404` без тела и без
// подсказки, поэтому клиент не получает даже направления, куда идти дальше.
//
// ЧТО НАБЛЮДАЛОСЬ (задача продукта #1617 и шире неё). Шестой шаг быстрого старта из
// семи — «привязать группу целей к балансировщику», то есть то, ради чего
// балансировщик и заводят, — звал снятый с контракта глагол. Перепись по дереву
// показала, что предмет шире названного в задаче: снятых глаголов, показанных
// документацией как живые, оказалось ЧЕТЫРЕ, а не два (кроме привязки и отвязки —
// ещё пуск и остановка, которые заменены полем `adminState`).
//
// РАДИУС БЕРЁТСЯ ПО МЕХАНИЗМУ, А НЕ ПО КООРДИНАТЕ ИЗ ЗАДАЧИ. Задача называла один
// файл; тот же дефект стоял на пяти страницах — в быстром старте, в справочнике
// ресурса, в двух сводных таблицах и во введении. Починка «там, где заметили»
// оставила бы класс с ощущением, что вопрос закрыт.
//
// ЧЕМ ЭТОТ ГЕЙТ НЕ ЯВЛЯЕТСЯ. Он не судит, ПРАВИЛЬНО ли описан живой маршрут, —
// только то, что показанный маршрут контрактом производится. Обратная сторона
// (живой маршрут, не показанный нигде) намеренно НЕ проверяется: документация
// вправе не описывать внутренние глаголы, и требование полноты сделало бы гейт
// красным на законном решении, а такой гейт отключают первым.

var (
	// option (google.api.http) = {get: "/nlb/v1/..."} — в одну строку и в блоке.
	nlbHTTPRuleRe = regexp.MustCompile(`(?m)^\s*(?:option \(google\.api\.http\) = \{\s*)?(get|post|put|patch|delete):\s*"([^"]+)"`)

	// Пути, показанные документацией. Три формы записи, и знать надо ВСЕ:
	//   1. атрибут компонента:  endpoint="/nlb/v1/listeners/{id}"
	//   2. inline-код таблицы:  <code>/nlb/v1/networkLoadBalancers/&#123;id&#125;:start</code>
	//   3. пример curl:         'http://localhost:18080/nlb/v1/listeners/lst...'
	// Форма, о которой распознаватель не знает, — не редкость, а НЕВИДИМОСТЬ:
	// всё записанное в ней молча уходит из-под наблюдения.
	nlbDocRouteRe = regexp.MustCompile(`/nlb/v1/[A-Za-z0-9{}&#;:_.\-/]*`)
)

// nlbRouteClaim — один показанный клиенту маршрут.
type nlbRouteClaim struct {
	file string
	line int
	raw  string
	norm string
}

// nlbRouteCensus — объём осмотренного.
type nlbRouteCensus struct {
	ProtoFiles     int
	DocFiles       int
	ContractRoutes map[string]struct{}
	Claims         []nlbRouteClaim
}

// normaliseNlbRoute — приводит путь к форме, в которой контракт и документация
// сравнимы: сегмент-подстановка становится `*`, а `:verb` сохраняется — именно он
// и отличает снятый глагол от живого.
func normaliseNlbRoute(p string) string {
	p = strings.ReplaceAll(p, "&#123;", "{")
	p = strings.ReplaceAll(p, "&#125;", "}")
	// Многоточие примера (`nlb...`) — ПОДСТАНОВКА, а не пунктуация конца фразы.
	// Снимать хвостовые точки раньше, чем его опознали, значит превращать живой
	// маршрут в несуществующий: `/nlb/v1/listeners/lst...` становилось
	// `/nlb/v1/listeners/lst`, и гейт объявлял находкой ИСПРАВНУЮ страницу.
	// Найдено первым же прогоном по дереву: 2 ложных из 10.
	const ellipsis = "\x00"
	p = strings.ReplaceAll(p, "...", ellipsis)
	p = strings.TrimRight(p, ".,;)'\"`")
	if i := strings.Index(p, "/nlb/v1/"); i > 0 {
		p = p[i:]
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		verb := ""
		if c := strings.IndexByte(s, ':'); c >= 0 {
			verb, s = s[c:], s[:c]
		}
		switch {
		case s == "":
		case strings.HasPrefix(s, "{"):
			s = "*"
		case strings.Contains(s, ellipsis): // `nlb...`, `lst...`, `tgr...` из примеров curl
			s = "*"
		}
		segs[i] = s + verb
	}
	return strings.Join(segs, "/")
}

// collectNlbDocumentedRoutes — что производит контракт и что показывает документация.
//
// Состав дерева приходит СОСТАВЛЕННЫМ (`treecorpus.Tree`), а не собирается здесь
// обходом диска: конструктор выбирает вызывающий — гейт берёт индекс git, а
// инъекционная проба `treecorpus.SyntheticTree`. Разбор — clienttruth_treefiles.go.
func collectNlbDocumentedRoutes(tree *treecorpus.Tree) (nlbRouteCensus, error) {
	c := nlbRouteCensus{ContractRoutes: map[string]struct{}{}}

	for _, rel := range clientTruthTreeFiles(tree, "proto/kacho/cloud/loadbalancer/v1", false, ".proto") {
		body, rerr := clientTruthReadTreeFile(tree, rel)
		if rerr != nil {
			return c, rerr
		}
		c.ProtoFiles++
		for _, m := range nlbHTTPRuleRe.FindAllStringSubmatch(string(body), -1) {
			c.ContractRoutes[normaliseNlbRoute(m[2])] = struct{}{}
		}
	}

	for _, rel := range clientTruthTreeFiles(tree, "services/nlb/docs/content", true, ".mdx") {
		body, rerr := clientTruthReadTreeFile(tree, rel)
		if rerr != nil {
			return c, rerr
		}
		c.DocFiles++
		for i, ln := range strings.Split(string(body), "\n") {
			for _, raw := range nlbDocRouteRe.FindAllString(ln, -1) {
				n := normaliseNlbRoute(raw)
				if n == "/nlb/v1" || n == "/nlb/v1/" {
					continue // общий префикс, а не маршрут
				}
				c.Claims = append(c.Claims, nlbRouteClaim{file: rel, line: i + 1, raw: raw, norm: n})
			}
		}
	}
	return c, nil
}

// nlbDocumentedRouteFindings — показанные маршруты, которых контракт не производит.
func nlbDocumentedRouteFindings(c nlbRouteCensus) []string {
	seen := map[string]bool{}
	var out []string
	for _, cl := range c.Claims {
		if _, ok := c.ContractRoutes[cl.norm]; ok {
			continue
		}
		key := cl.file + ":" + cl.norm
		if seen[key] {
			continue // один и тот же маршрут на странице — одна находка
		}
		seen[key] = true
		out = append(out, fmt.Sprintf("%s:%d показывает %q — контракт такого маршрута не производит",
			cl.file, cl.line, cl.raw))
	}
	sort.Strings(out)
	return out
}
