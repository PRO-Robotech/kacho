// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// proxyforbiddentypes_injection_test.go — доказательство способности гейта
// TestForbiddenProxyObjectTypesAgreeWithTheModel падать И молчать.
//
// # Почему инъекция подаёт ЗНАЧЕНИЯ, а не правит дерево
//
// Вердикт выносит judgeForbiddenProxyTypes над тремя перечнями; гейт лишь
// добывает их — импортом набора и разбором модели. Подавая перечни напрямую,
// инъекция проверяет ровно ту функцию, которую исполняет гейт, и не трогает ни
// общую рабочую копию, ни настоящую модель.
//
// # Прогонов ТРИ, и третий обязателен
//
// Инъекция обязана ронять ТОЛЬКО проверяемое: красное, пришедшее от соседней
// стороны, доказывало бы работу соседа, а не новой половины. Поэтому:
//
//	контроль            — набор и модель согласны, молчат ОБЕ стороны;
//	инъекция записи     — краснеет ТОЛЬКО сторона «запись без референта»;
//	инъекция типа       — краснеет ТОЛЬКО сторона «тип без записи».
//
// Плюс законный близнец: тип, ПРИНАДЛЕЖАЩИЙ модулю-эмитенту, в наборе отсутствует
// законно (его закрывает привязка к домену), и на нём гейт обязан молчать. Без
// близнеца сторона «тип без записи» краснела бы на всяком ресурсе платформы.

// fixture — согласованная тройка: набор покрывает все немодульные типы, каждая
// его запись объявлена моделью.
func forbiddenTypesFixture() (entries, modelTypes, domains []string) {
	entries = []string{"cluster", "account", "iam_role"}
	modelTypes = []string{"cluster", "account", "iam_role", "vpc_network", "nlb_listener"}
	domains = []string{"vpc", "nlb"}
	return entries, modelTypes, domains
}

func TestForbiddenTypesJudgeIsSilentOnAgreement(t *testing.T) {
	entries, modelTypes, domains := forbiddenTypesFixture()

	faults, census := judgeForbiddenProxyTypes(entries, modelTypes, domains)
	t.Logf("контроль: %s", census.Summary())

	if len(faults) != 0 {
		t.Fatalf("контроль покраснел на согласованной тройке (%d):\n%s",
			len(faults), strings.Join(faults, "\n"))
	}
	if census.NonModuleTypes != 3 || census.NonModuleCovered != 3 {
		t.Fatalf("перепись контроля не сошлась: вне модулей %d, в наборе %d, ожидалось 3 и 3",
			census.NonModuleTypes, census.NonModuleCovered)
	}
	// Законный близнец: оба модульных типа в наборе отсутствуют и находкой не
	// стали. Утверждается ЧИСЛОМ, а не молчанием: «ноль находок» без переписи
	// неотличимо от «ноль осмотренного».
	if census.ModelTypes-census.NonModuleTypes != 2 {
		t.Fatalf("модульных типов осмотрено %d, ожидалось 2 — законный близнец не подан",
			census.ModelTypes-census.NonModuleTypes)
	}
}

func TestForbiddenTypesJudgeCatchesAnEntryWithoutAReferent(t *testing.T) {
	entries, modelTypes, domains := forbiddenTypesFixture()
	entries = append(entries, "role") // тип модели называется `iam_role`

	faults, census := judgeForbiddenProxyTypes(entries, modelTypes, domains)
	t.Logf("инъекция записи: %s", census.Summary())

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка (только сторона «запись без референта»), "+
			"получено %d:\n%s", len(faults), strings.Join(faults, "\n"))
	}
	if !strings.Contains(faults[0], `"role"`) {
		t.Fatalf("находка не называет запись: %s", faults[0])
	}
	if !strings.Contains(faults[0], "не резолвится") {
		t.Fatalf("находка пришла не от той стороны: %s", faults[0])
	}
	if census.EntriesResolved != census.Entries-1 {
		t.Fatalf("перепись не отразила нерезолвящуюся запись: записей %d, резолвится %d",
			census.Entries, census.EntriesResolved)
	}
}

func TestForbiddenTypesJudgeCatchesANonModuleTypeWithoutAnEntry(t *testing.T) {
	entries, modelTypes, domains := forbiddenTypesFixture()
	modelTypes = append(modelTypes, "iam_group") // немодульный тип, записи нет

	faults, census := judgeForbiddenProxyTypes(entries, modelTypes, domains)
	t.Logf("инъекция типа: %s", census.Summary())

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка (только сторона «тип без записи»), "+
			"получено %d:\n%s", len(faults), strings.Join(faults, "\n"))
	}
	if !strings.Contains(faults[0], `"iam_group"`) {
		t.Fatalf("находка не называет тип: %s", faults[0])
	}
	if !strings.Contains(faults[0], "не назван запретительным набором") {
		t.Fatalf("находка пришла не от той стороны: %s", faults[0])
	}
	if census.NonModuleTypes != 4 || census.NonModuleCovered != 3 {
		t.Fatalf("перепись не отразила непокрытый тип: вне модулей %d, в наборе %d",
			census.NonModuleTypes, census.NonModuleCovered)
	}
}

// TestForbiddenTypesJudgeIsSilentOnAModuleTypeOutsideTheSet — законный близнец
// отдельным прогоном: тип, приставка которого называет домен-эмитент, в наборе
// отсутствует ЗАКОННО (его закрывает привязка к домену), и находкой не становится.
//
// Без этого прогона сторона «тип без записи» краснела бы на каждом ресурсе
// платформы, и первое же ложное срабатывание сняло бы гейт целиком.
func TestForbiddenTypesJudgeIsSilentOnAModuleTypeOutsideTheSet(t *testing.T) {
	entries, modelTypes, domains := forbiddenTypesFixture()
	modelTypes = append(modelTypes, "vpc_subnet") // модульный тип, записи нет — законно

	faults, census := judgeForbiddenProxyTypes(entries, modelTypes, domains)
	t.Logf("законный близнец: %s", census.Summary())

	if len(faults) != 0 {
		t.Fatalf("гейт покраснел на законном модульном типе (%d):\n%s",
			len(faults), strings.Join(faults, "\n"))
	}
	if census.ModelTypes != 6 {
		t.Fatalf("близнец не доехал до разбора: типов осмотрено %d, ожидалось 6",
			census.ModelTypes)
	}
}
