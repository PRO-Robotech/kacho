// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_misc_filter_value_bounds.go — что контракт обещает о значении фильтра,
// разборщик обязан делать; чего не делает — контракт обещать не вправе.
//
// ПРЕДМЕТ (задача продукта #1654). Шесть контрактов vpc объявляли для значения
// фильтра алфавит и предел длины, которых у разборщика не было ни одного:
// клиент, построивший автоматизацию по объявленному, отказа на значении вне
// правила не получал. Расхождение сняли правкой ТЕКСТА — контракты стали
// говорить, что правил нет. Это верно описывало код и оставляло открытым
// вопрос по существу: значение приходит от клиента и уезжает в запрос.
//
// Правила заведены (`pkg/filter`), и теперь обе стороны расхождения возможны
// СНОВА, только зеркально: контракт может обещать не тот предел либо объявить,
// что предела нет. Гейт держит обе.
//
// ЧТО ГЕЙТ ДЕРЖИТ:
//
//  1. НИ ОДИН контракт не объявляет ОТСУТСТВИЕ правила длины. Такое объявление
//     было верным ровно до этой задачи и переживёт её молча.
//  2. Всякое НАЗВАННОЕ число знаков совпадает с `filter.MaxValueLen` — не с
//     копией величины, а с самой величиной: гейт импортирует пакет, поэтому
//     второго места о пределе не существует by construction.
//
// ПОЧЕМУ ТЕКСТ, А НЕ РАЗБОР СИНТАКСИСА. Обычно проверка обязана судить узлы, а
// не текст (`testing.md` §«Гейт на класс», п.4) — иначе она краснеет на
// комментарии, объясняющем защиту. Здесь предмет ОБРАТНЫЙ: судится именно
// комментарий контракта, потому что он и есть обещание клиенту. Сузить обход до
// блока, прилегающего к полю `filter`, всё же необходимо — иначе гейт краснел бы
// на чужой прозе о длинах в том же файле.
//
// ЧЕГО ГЕЙТ НЕ ДЕРЖИТ — названо, а не спрятано:
//
//   - что контракт вообще ОПИСЫВАЕТ правила. Требовать этого от каждого поля
//     `filter` значило бы краснеть на восемнадцати контрактах разом ради
//     единообразия прозы, которого никто не решал;
//   - правдивость самого правила — что разборщик и вправду отвергает длинное.
//     Это свойство вызова, и держат его парные пробы `pkg/filter`.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

var (
	// ct2FilterFieldRe — объявление поля фильтра в контракте.
	ct2FilterFieldRe = regexp.MustCompile(`^\s*string\s+filter\s*=`)
	// ct2FilterLenClaimRe — названный предел длины значения.
	ct2FilterLenClaimRe = regexp.MustCompile(`(\d+)\s+characters`)
	// ct2FilterNoRuleRe — объявление ОТСУТСТВИЯ правила длины. Форма взята
	// дословно из того, что стояло в шести контрактах до этой задачи.
	ct2FilterNoRuleRe = regexp.MustCompile(`applies no[^.]*length`)
)

// ct2FilterClaim — что объявлено об одном поле `filter`.
type ct2FilterClaim struct {
	// Rel и Line — координата поля; находка обязана её называть.
	Rel  string
	Line int
	// StatedLimits — числа знаков, названные в прилегающем блоке.
	StatedLimits []int
	// DeniesRule — блок объявляет, что правила длины нет.
	DeniesRule bool
}

// ct2FilterCensus — перепись обхода. Печатается ВСЕГДА: «ноль находок» обязано
// быть отличимо от «ноль прочитанного».
type ct2FilterCensus struct {
	Files    int
	Fields   int
	Claims   []ct2FilterClaim
	Stating  int
	Denying  int
	Agreeing int
}

// collectFilterValueClaims обходит контракты дерева.
//
// Состав дерева берётся у ИНДЕКСА git, а не у диска: рядом с контрактами лежат
// распаковки и кэши инструментов, и вердикт, собранный обходом файловой
// системы, стал бы свойством рабочего каталога, а не коммита.
func collectFilterValueClaims(tree *treecorpus.Tree, limit int) (ct2FilterCensus, error) {
	var c ct2FilterCensus

	for _, rel := range tree.SortedFiles() {
		if !strings.HasSuffix(rel, ".proto") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			return c, fmt.Errorf("чтение %s: %w", rel, err)
		}
		c.Files++
		lines := strings.Split(string(body), "\n")
		for i, l := range lines {
			if !ct2FilterFieldRe.MatchString(l) {
				continue
			}
			c.Fields++
			claim := ct2FilterClaim{Rel: rel, Line: i + 1}
			// Прилегающий блок — непрерывная лента комментариев НАД полем.
			block := ct2CommentBlockAbove(lines, i)
			claim.DeniesRule = ct2FilterNoRuleRe.MatchString(block)
			for _, m := range ct2FilterLenClaimRe.FindAllStringSubmatch(block, -1) {
				n, cerr := strconv.Atoi(m[1])
				if cerr == nil {
					claim.StatedLimits = append(claim.StatedLimits, n)
				}
			}
			if !claim.DeniesRule && len(claim.StatedLimits) == 0 {
				continue
			}
			c.Claims = append(c.Claims, claim)
		}
	}

	for _, cl := range c.Claims {
		if cl.DeniesRule {
			c.Denying++
		}
		if len(cl.StatedLimits) == 0 {
			continue
		}
		c.Stating++
		agree := true
		for _, n := range cl.StatedLimits {
			if n != limit {
				agree = false
			}
		}
		if agree && !cl.DeniesRule {
			c.Agreeing++
		}
	}
	return c, nil
}

// ct2CommentBlockAbove — непрерывная лента строк-комментариев прямо над полем,
// склеенная в один текст.
//
// Сужение обхода до этой ленты и есть то, чем гейт отличается от поиска по
// файлу: без него он краснел бы на чужой прозе о длинах в соседнем сообщении.
func ct2CommentBlockAbove(lines []string, at int) string {
	var out []string
	for i := at - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, "//") {
			break
		}
		out = append([]string{strings.TrimPrefix(t, "//")}, out...)
	}
	return strings.Join(out, " ")
}

// filterValueClaimFindings — расхождения, каждое с координатой.
func filterValueClaimFindings(c ct2FilterCensus, limit int) []string {
	var out []string
	for _, cl := range c.Claims {
		if cl.DeniesRule {
			out = append(out, fmt.Sprintf(
				"%s:%d: контракт объявляет, что правила длины у значения фильтра нет — "+
					"оно есть (pkg/filter.MaxValueLen = %d), и обещание пережило свой предмет",
				cl.Rel, cl.Line, limit))
			continue
		}
		for _, n := range cl.StatedLimits {
			if n != limit {
				out = append(out, fmt.Sprintf(
					"%s:%d: контракт объявляет предел %d знаков, разборщик применяет %d "+
						"(pkg/filter.MaxValueLen) — два места об одном предмете",
					cl.Rel, cl.Line, n, limit))
			}
		}
	}
	return out
}
