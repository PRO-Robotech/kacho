// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_verified_address_required_on_both_lanes_injection_test.go —
// доказательство того, что MAIL-52 СПОСОБЕН упасть, и падает ТОЛЬКО на своём
// предмете.
//
// Гейт превентивный: на рабочем дереве он молчит. Значит без этого файла его
// молчание неотличимо от молчания гейта, который не умеет краснеть, — а такой
// занимает слот, отчитывается зелёным и создаёт уверенность, которой нет.
//
// Инъекция идёт по КОПИИ ТЕКСТА объявления в памяти: рабочего дерева она не
// трогает вовсе.
package deploy_test

import (
	"strings"
	"testing"
)

// requirementLine — строка, которой требование и выражено. Снятие ИМЕННО ЕЁ и
// есть тот самый дешёвый способ «вернуть вход», против которого гейт заведён.
const requirementLine = "            - hook: require_verified_address\n"

func TestVerifiedAddressGateFailsOnAReturnedDefect(t *testing.T) {
	body := readFileForTest(t, identityConfigTemplate)

	// Предпосылка инъекции: строка требования в объявлении есть, и её ровно
	// столько, сколько полос. Пропала — доказательство исчезло вместе с ней, и
	// это отказ, а не «дефекта не осталось».
	lanes := loginLanesWithRequirement(t, body)
	if n := strings.Count(body, requirementLine); n < len(lanes) || len(lanes) == 0 {
		t.Fatalf("строк требования в объявлении %d при %d полосах входа — фикстура "+
			"перестала описывать дерево, и инъекция ниже условия не создаст",
			n, len(lanes))
	}

	t.Run("контроль: рабочее объявление — молчание", func(t *testing.T) {
		if found := verifiedAddressFindings(t, body); len(found) > 0 {
			t.Errorf("гейт краснеет на НЕТРОНУТОМ объявлении — его находки не про "+
				"инъекцию, и прогоны ниже недействительны:\n%s", strings.Join(found, "\n"))
		}
	})

	t.Run("инъекция: требование снято с ОДНОЙ полосы", func(t *testing.T) {
		// Снимается первое вхождение — то есть требование остаётся на второй
		// полосе. Гейт, считающий «требование где-то есть», здесь смолчал бы:
		// его предмет — КАЖДАЯ полоса, а не наличие требования вообще.
		injected := strings.Replace(body, requirementLine, "", 1)
		found := verifiedAddressFindings(t, injected)
		if len(found) == 0 {
			t.Fatalf("требование, снятое с одной полосы, гейт НЕ нашёл — он утверждает " +
				"«требование где-то есть» вместо «требование на каждой полосе», и " +
				"разошедшиеся полосы пройдут мимо него")
		}
		if len(found) != 1 {
			t.Errorf("находок %d при требовании, снятом с ОДНОЙ полосы, — ожидалась "+
				"одна:\n%s", len(found), strings.Join(found, "\n"))
		}
		// Находка обязана называть ИМЯ полосы: без него читатель не знает, где
		// чинить, тратит на это прогон и снимает гейт как непонятный.
		joined := strings.Join(found, "\n")
		named := false
		for n := range lanes {
			if strings.Contains(joined, `"`+n+`"`) {
				named = true
			}
		}
		if !named {
			t.Errorf("находка не называет ИМЯ полосы — диагностика есть часть "+
				"свойства, а не украшение:\n%s", joined)
		}
	})

	t.Run("инъекция: требование снято со ВСЕХ полос", func(t *testing.T) {
		injected := strings.ReplaceAll(body, requirementLine, "")
		found := verifiedAddressFindings(t, injected)
		if len(found) != len(lanes) {
			t.Errorf("находок %d при требовании, снятом со всех %d полос — гейт "+
				"перечисляет не каждую:\n%s", len(found), len(lanes),
				strings.Join(found, "\n"))
		}
	})

	t.Run("близнец: НОВАЯ полоса с требованием — молчание", func(t *testing.T) {
		// Гейт не вправе быть счётчиком «полос ровно две»: полоса входа
		// добавляется законно, и пока она несёт требование, находки нет.
		// Иначе первая же новая полоса покрасила бы исправное дерево.
		// Якорь — заголовок блока полос ВХОДА; в объявлении он единственный.
		// Проверка единственности не формальность: `        password:` тоже
		// выглядит годным якорем и встречается ДВАЖДЫ (вход и регистрация), а
		// правка по первому вхождению завела бы полосу не туда — условие
		// близнеца не создалось бы, и его молчание ничего не значило бы.
		anchor := "    login:\n      ui_url: {{ $flow }}/login\n      lifespan: 30m\n      after:\n"
		if n := strings.Count(body, anchor); n != 1 {
			t.Fatalf("якорь близнеца встречается %d раз, а нужен ровно один", n)
		}
		twin := "        code:\n          hooks:\n" + requirementLine
		injected := strings.Replace(body, anchor, anchor+twin, 1)

		lanesAfter := loginLanesWithRequirement(t, injected)
		if len(lanesAfter) != len(lanes)+1 {
			t.Fatalf("близнец не создал условия: полос было %d, стало %d — инъекция "+
				"не воспроизвела «новая полоса», и её молчание ничего не значит",
				len(lanes), len(lanesAfter))
		}
		if found := verifiedAddressFindings(t, injected); len(found) > 0 {
			t.Errorf("гейт покраснел на НОВОЙ полосе, которая требование НЕСЁТ, — он "+
				"судит число полос, а не наличие требования на каждой:\n%s",
				strings.Join(found, "\n"))
		}
	})
}
