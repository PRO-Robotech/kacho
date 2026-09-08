// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// rules_overlay_left_no_remnant_injection_test.go — ДОКАЗАТЕЛЬСТВО СПОСОБНОСТИ
// УПАСТЬ для TestRulesOverlayLeftNoRemnantInTheDelivery.
//
// Гейт стоит на двух распознавателях, и оба судят НЕ СЛОВО В ТЕКСТЕ:
//
//	hasRulesOverlayKey — ключ РАЗОБРАННОГО дерева значений. Ручка встречается и в
//	                     комментариях (в том числе в объяснении её снятия), и
//	                     проверка по подстроке краснела бы на собственной прозе;
//	executableLines    — исполняемая часть шаблона: строка комментария и блок
//	                     комментария шаблона предметом не являются.
//
// По каждой оси — ДВЕ стороны: внесённый дефект даёт находку, законный близнец
// молчит. Каждая ось меняет РОВНО ОДИН факт против своего близнеца.
//
// Отдельная ось — ПРЕДПОСЫЛКА гейта: признак потребителя читается по коду, а не
// по воспоминанию о нём. Без неё гейт, начав считать комментарии, объявил бы
// «потребитель вернулся» на дереве, где вернулась только проза.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRulesOverlayInjection_ValuesKeyIsFoundByNodeNotBySubstring(t *testing.T) {
	// Дефект: ручка ОБЪЯВЛЕНА в дереве значений.
	defect := map[string]any{
		"kaname": map[string]any{
			"opaSidecar": map[string]any{"enabled": false},
		},
	}
	var found []string
	hasRulesOverlayKey(defect, nil, "opaSidecar", &found)
	require.NotEmpty(t, found, "объявленная ручка обязана быть найдена")
	require.Equal(t, "kaname.opaSidecar", found[0],
		"находка обязана называть ПУТЬ ключа, иначе оператор не найдёт, что править")

	// Близнец: того же имени НЕТ ключом — оно лишь ЗНАЧЕНИЕ соседнего ключа,
	// то есть проза о предмете. Различие ровно в одном факте.
	twin := map[string]any{
		"kaname": map[string]any{
			"note": "ручка opaSidecar снята вместе со своим потребителем",
		},
	}
	found = nil
	hasRulesOverlayKey(twin, nil, "opaSidecar", &found)
	require.Empty(t, found, "имя ручки в ЗНАЧЕНИИ ключом не является — гейт обязан молчать")
}

func TestRulesOverlayInjection_KeyNestedAnywhereIsFound(t *testing.T) {
	// Ручка объявляется и верхним уровнем, и внутри блока подчарта: распознаватель
	// обязан знать обе формы, иначе профиль остаётся вне наблюдения молча.
	deep := map[string]any{
		"a": map[string]any{"b": []any{map[string]any{"opaSidecar": map[string]any{}}}},
	}
	var found []string
	hasRulesOverlayKey(deep, nil, "opaSidecar", &found)
	require.NotEmpty(t, found, "ручка внутри последовательности обязана быть найдена")
	require.Contains(t, found[0], "opaSidecar")
}

func TestRulesOverlayInjection_LabelInCodeIsAFindingInProseIsNot(t *testing.T) {
	// Дефект: метка стоит ИСПОЛНЯЕМОЙ строкой шаблона.
	defect := "metadata:\n  labels:\n    kacho.cloud/opa-sidecar: \"true\"\n"
	require.Contains(t, strings.Join(executableLines(defect), "\n"), "kacho.cloud/opa-sidecar",
		"метка исполняемой строкой обязана дойти до распознавателя")

	// Близнец 1: та же метка в комментарии YAML. Различие — один знак решётки.
	twin1 := "metadata:\n  labels:\n    # kacho.cloud/opa-sidecar: \"true\" — снято вместе с предметом\n"
	require.NotContains(t, strings.Join(executableLines(twin1), "\n"), "kacho.cloud/opa-sidecar",
		"комментарий YAML предметом не является — иначе гейт краснеет на объяснении снятия")

	// Близнец 2: та же метка внутри блока комментария шаблона.
	twin2 := "{{/*\nПрежде здесь стояла метка kacho.cloud/opa-sidecar: \"true\".\n*/}}\nmetadata: {}\n"
	require.NotContains(t, strings.Join(executableLines(twin2), "\n"), "kacho.cloud/opa-sidecar",
		"блок комментария шаблона предметом не является")
}

func TestRulesOverlayInjection_PremiseReadsCodeNotItsMemoryOfIt(t *testing.T) {
	// ПРЕДПОСЫЛКА гейта: «у наложения нет потребителя» устанавливается по
	// обращению к вердикту, а не по упоминанию о нём.
	//
	// Дефект: потребитель ВЕРНУЛСЯ — обращение стоит исполняемой строкой. Гейт
	// обязан на этом отказать ДРУГИМ текстом, а не молчать и не объявлять
	// остатки: послабление истекает от появления предмета.
	consumer := "func check() {\n\tresp, err := http.Post(base+\"/v1/data/x/deny\", ct, body)\n}\n"
	hit := false
	for _, line := range goExecutableLines(consumer) {
		if strings.Contains(line, "/v1/data/") {
			hit = true
		}
	}
	require.True(t, hit, "вернувшийся потребитель обязан быть виден предпосылке")

	// Близнец: воспоминание о снятом потребителе — комментарий. Он предпосылку
	// не переворачивает, иначе гейт объявил бы возврат механизма на прозе о нём.
	memory := "// Наложение снято: шаг вердикта по адресу /v1/data/x/deny больше не зовётся.\n" +
		"func check() { return }\n"
	hit = false
	for _, line := range goExecutableLines(memory) {
		if strings.Contains(line, "/v1/data/") {
			hit = true
		}
	}
	require.False(t, hit,
		"комментарий о снятом обращении обращением не является — предпосылка судит код")
}
