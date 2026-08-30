// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream

import (
	"fmt"
	"strings"
	"testing"
)

// clientpage_injection_test.go — доказательство, что гейт клиентской страницы
// СПОСОБЕН упасть, и падает он на предмете, а не на форме.
//
// Проверка, чьё зелёное никогда не проверяли на настоящем дефекте, зелена и на
// сломанном дереве: она молчит одинаково. Поэтому здесь каждое утверждение гейта
// прогоняется дважды — на внесённом дефекте (обязано найтись) и на ЗАКОННОМ
// БЛИЗНЕЦЕ той же формы (обязано смолчать).
//
// Инъекция трогает СВОЙСТВО, а не заводит лишний элемент: страница-фикстура
// целая во всём, кроме одной величины. Инъекция вида «дописать ещё строку»
// нарушала бы разом всё, что требуется от строк, и красное приходило бы от
// соседа.

// fixturePage собирает синтетическую страницу с названным набором параметров.
//
// Форма таблицы дословно та же, что у настоящей страницы: гейт обязан ловить
// предмет, а не отличие вёрстки.
func fixturePage(params []string, values []wireValue) string {
	var b strings.Builder
	b.WriteString("# синтетическая страница\n\n")
	// Проза НАЗЫВАЕТ все параметры — в том числе те, которых нет в таблице.
	// Гейт, судящий страницу целиком, зеленел бы на этом; судящий таблицу —
	// нет. Разница и есть предмет утверждения «гейт читает таблицу».
	b.WriteString("В прозе упомянуты owner, kinds, projectId, ids и start.\n\n")
	for _, v := range values {
		b.WriteString(v.text)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(paramsHeading)
	b.WriteString("\n\n<table>\n  <tbody>\n")
	for _, p := range params {
		b.WriteString(fmt.Sprintf("    <tr>\n      <td><code>%s</code></td><td>да</td>\n    </tr>\n", p))
	}
	b.WriteString("  </tbody>\n</table>\n")
	return b.String()
}

// allParams — набор, который принимает ручка, в устойчивом порядке.
func allParams() []string { return sortedNames(knownParams) }

// TestClientPageGateFindsAParameterTheHandleAcceptsButThePageOmits — параметр
// есть в коде, страница о нём молчит.
func TestClientPageGateFindsAParameterTheHandleAcceptsButThePageOmits(t *testing.T) {
	full := allParams()
	for _, dropped := range full {
		kept := make([]string, 0, len(full)-1)
		for _, p := range full {
			if p != dropped {
				kept = append(kept, p)
			}
		}
		page := fixturePage(kept, wireValues())
		documented := paramsDeclaredByPage(t, page)
		missing, invented := compareParamSets(documented, knownParams)

		if len(missing) != 1 || missing[0] != dropped {
			t.Errorf("снят параметр %q, а гейт назвал пропущенными %v — находка обязана "+
				"называть КООРДИНАТУ, иначе по ней нечего чинить", dropped, missing)
		}
		if len(invented) != 0 {
			t.Errorf("снят параметр %q, а гейт вдобавок объявил лишними %v — инъекция обязана "+
				"ронять только проверяемое", dropped, invented)
		}
	}
	t.Logf("перепись: осей инъекции %d %v — по одной на каждый параметр ручки", len(full), full)
}

// TestClientPageGateFindsAParameterThePageInventsAndTheHandleRejects — страница
// советует параметр, которого ручка не принимает.
func TestClientPageGateFindsAParameterThePageInventsAndTheHandleRejects(t *testing.T) {
	// `projectid` — не выдуманное имя, а КАНОНИЧЕСКАЯ ОПЕЧАТКА РЕГИСТРА, ради
	// которой набор и объявлен закрытым: принятая молча, она дала бы поток, не
	// суженный по проекту.
	page := fixturePage(append(allParams(), "projectid"), wireValues())
	documented := paramsDeclaredByPage(t, page)
	missing, invented := compareParamSets(documented, knownParams)

	if len(missing) != 0 {
		t.Errorf("дописан лишний параметр, а гейт объявил пропущенными %v — красное пришло "+
			"не от предмета инъекции", missing)
	}
	if len(invented) != 1 || invented[0] != "projectid" {
		t.Fatalf("гейт не назвал лишним %q: он объявил %v — страница советовала бы вход, "+
			"который ручка отвергает `400`", "projectid", invented)
	}
}

// TestClientPageGateStaysSilentOnALegitimatePage — законный близнец той же формы.
//
// Без этого утверждения предыдущие два зеленели бы и на гейте, который краснеет
// ВСЕГДА: «нашёл дефект» и «находит всё подряд» неразличимы по одной стороне.
func TestClientPageGateStaysSilentOnALegitimatePage(t *testing.T) {
	page := fixturePage(allParams(), wireValues())
	documented := paramsDeclaredByPage(t, page)
	missing, invented := compareParamSets(documented, knownParams)

	if len(missing) != 0 || len(invented) != 0 {
		t.Fatalf("на согласной странице гейт нашёл пропущенные %v и лишние %v — проверка, "+
			"краснеющая на верном, будет снята первой", missing, invented)
	}
	if len(missingWireValues(page)) != 0 {
		t.Fatalf("на согласной странице гейт не нашёл величин на проводе: %v",
			missingWireValues(page))
	}
}

// TestClientPageGateFindsAWireValueThePageDoesNotName — величина, которую клиент
// не выведет ниоткуда, снята со страницы.
func TestClientPageGateFindsAWireValueThePageDoesNotName(t *testing.T) {
	all := wireValues()
	for i, dropped := range all {
		kept := make([]wireValue, 0, len(all)-1)
		kept = append(kept, all[:i]...)
		kept = append(kept, all[i+1:]...)

		page := fixturePage(allParams(), kept)
		found := missingWireValues(page)

		// Величины не должны перекрывать друг друга по подстроке: если снятие
		// одной оставляет её текст в странице через другую, гейт неспособен её
		// различить, и это находка САМОГО гейта.
		if len(found) != 1 || found[0].text != dropped.text {
			t.Errorf("снята величина %q (%s), а гейт назвал недостающими %d штук %v — "+
				"либо он её не различает, либо ронять его может соседняя",
				dropped.text, dropped.what, len(found), namesOf(found))
		}
	}
	t.Logf("перепись: осей инъекции %d — по одной на каждую величину на проводе", len(all))
}

// namesOf — тексты величин для находки.
func namesOf(vs []wireValue) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.text)
	}
	return out
}
