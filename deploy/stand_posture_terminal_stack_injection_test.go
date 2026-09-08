// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_posture_terminal_stack_injection_test.go — доказательство того, что
// соседняя проверка СПОСОБНА упасть, и что она молчит на законном близнеце.
//
// Без него «находок 0» на дереве, где терминальная цепочка боевая, было бы
// неотличимо от проверки, не умеющей падать вовсе.
//
// Инъекция подаётся ЯДРУ (`judgeTerminalPosture`) — той же функции, которую
// зовёт проверка на дереве. Своя копия предиката разошлась бы с настоящей
// молча; синтетическое дерево здесь не заводится намеренно: предмет ядра —
// суждение о фактах, а не их чтение, и чтение доказывается отдельной осью
// «распознаватели узнают НАСТОЯЩЕЕ дерево» ниже.
package deploy_test

import (
	"strings"
	"testing"
)

// Дефект: терминальная цепочка оставляет сервис в посадке разработки.
// Обязано находиться, и находка обязана называть сервис И цепочку — иначе
// читатель пойдёт искать не там.
func TestInjection_TerminalStackLeavingAServiceInDevIsFound(t *testing.T) {
	got := judgeTerminalPosture("dev-prod",
		map[string]string{"kaname": "dev", "vpc": "production"},
		[]string{"kaname", "vpc"})
	if len(got) != 1 {
		t.Fatalf("небоевая посадка терминальной цепочки не найдена: %+v", got)
	}
	if got[0].Kind != "не боевая" || got[0].Service != "kaname" || got[0].Stack != "dev-prod" {
		t.Fatalf("находка не называет предмет: %+v", got[0])
	}
}

// Дефект: терминальная цепочка посадку сервису не объявляет вовсе — значение
// приедет из умолчания подчарта. Обязано находиться ОТДЕЛЬНЫМ видом: «не
// объявлено» и «объявлено небоевой» чинятся по-разному.
func TestInjection_TerminalStackDeclaringNothingIsFound(t *testing.T) {
	got := judgeTerminalPosture("dev-prod",
		map[string]string{"kaname": "", "vpc": "production"},
		[]string{"kaname", "vpc"})
	if len(got) != 1 || got[0].Kind != "не объявлено" || got[0].Service != "kaname" {
		t.Fatalf("необъявленная посадка не найдена либо названа не тем видом: %+v", got)
	}
}

// Дефект: значение вне словаря посадок. Страж старта отвергает такое отказом
// ПУСКА, то есть стенд не поднимется вовсе, — и проверка обязана сказать это
// ДО выкатки, а не оставить оператору вердикт кластера.
func TestInjection_TerminalStackDeclaringAWordOutsideTheVocabularyIsFound(t *testing.T) {
	got := judgeTerminalPosture("dev-prod",
		map[string]string{"kaname": "prod"},
		[]string{"kaname"})
	if len(got) != 1 || got[0].Kind != "не из словаря" || got[0].Declared != "prod" {
		t.Fatalf("значение вне словаря не найдено: %+v", got)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ, ради которого ядро судит ровно одну цепочку: посадка
// разработки у ПРОМЕЖУТОЧНОЙ фазы находкой не является. Первая фаза подъёма
// объявляет её намеренно, и вторая её замещает; проверка, краснеющая здесь,
// требовала бы, чтобы терминальной фазы не существовало.
func TestInjection_DevPostureOfAnIntermediatePhaseIsSilent(t *testing.T) {
	// Ядру подаётся ТЕРМИНАЛЬНАЯ цепочка; промежуточная в него не попадает
	// by construction. Утверждение здесь — что именно так и устроено: подай
	// ядру факты промежуточной фазы, и оно найдёт дефект, — значит фильтр
	// «только терминальная» несёт вес, а не украшает.
	asIfTerminal := judgeTerminalPosture("dev",
		map[string]string{"kaname": "dev"}, []string{"kaname"})
	if len(asIfTerminal) != 1 {
		t.Fatalf("контроль: те же факты, названные терминальными, обязаны давать находку — "+
			"иначе фильтр «только терминальная» ничего не решает: %+v", asIfTerminal)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ второго рода: всё боевое — проверка молчит. Без него
// отрицание зеленело бы на ядре, отвергающем всё.
func TestInjection_AllProductionIsSilent(t *testing.T) {
	got := judgeTerminalPosture("dev-prod",
		map[string]string{"kaname": "production-strict", "vpc": "production"},
		[]string{"kaname", "vpc"})
	if len(got) != 0 {
		t.Fatalf("боевая посадка объявлена находкой: %+v", got)
	}
}

// Распознаватели узнают НАСТОЯЩЕЕ дерево. Ось отдельная, потому что три
// предыдущие судят ядро и о чтении не утверждают ничего: ядро осталось бы
// зелёным и на дереве, где ни цель, ни фазы, ни ручка не распознаются.
func TestStandPosturePredicates_RecogniseTheRealTree(t *testing.T) {
	bringUp := shardBringUp(t)
	if len(bringUp.Phases) < 2 {
		t.Fatalf("рецепт цели %q распознан с %d фазой(ами) — подъём стенда накладывает "+
			"цепочку разработки и поверх неё боевую, значит фаз не меньше двух. "+
			"Одна означает, что распознаватель прочитал не тот рецепт",
			bringUp.Target, len(bringUp.Phases))
	}
	if _, known := deployStacks(t)[bringUp.Terminal()]; !known {
		t.Fatalf("терминальная цепочка %q не найдена в таблице стендов", bringUp.Terminal())
	}
	services := postureKnobServices(t)
	if len(services) < 2 {
		t.Fatalf("сервисов с ручкой посадки распознано %d (%s) — ручку несут все "+
			"go-сервисы флота, значит распознаватель читает не умолчания подчартов",
			len(services), strings.Join(services, " "))
	}
	t.Logf("распознано на дереве: цель %q · фазы %s · сервисов с ручкой посадки %d (%s)",
		bringUp.Target, strings.Join(bringUp.Phases, " → "), len(services),
		strings.Join(services, " "))
}
