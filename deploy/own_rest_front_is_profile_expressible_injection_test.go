// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// own_rest_front_is_profile_expressible_injection_test.go — ДОКАЗАТЕЛЬСТВО
// СПОСОБНОСТИ УПАСТЬ для TestOwnRestFrontIsExpressibleByTheProfile.
//
// Инъекция кормит те же чистые функции (collectRestFrontRenders,
// auditOwnRestFront), что и настоящее дерево. По каждой оси — ДВЕ стороны:
// внесённый дефект даёт находку, законный близнец молчит. Каждая ось меняет
// РОВНО ОДИН факт против своего близнеца.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func injRestAudit(t *testing.T, path, tpl string, defaults []restFrontDefault) ([]string, restFrontCensus) {
	t.Helper()
	renders := collectRestFrontRenders(path, tpl)
	require.NotEmpty(t, renders,
		"инъекция беспредметна: распознаватель не нашёл ни одного места материализации")
	return auditOwnRestFront(renders, defaults, restFrontCensus{})
}

func TestOwnRestFrontInjection_UnconditionalRenderIsAFinding(t *testing.T) {
	defect := "data:\n  config.yaml: |\n    api-server:\n" +
		"      rest-endpoint: {{ printf \"tcp://0.0.0.0:%v\" .Values.ports.rest | quote }}\n"
	twin := "data:\n  config.yaml: |\n    api-server:\n" +
		"      {{- if .Values.ports.rest }}\n" +
		"      rest-endpoint: {{ printf \"tcp://0.0.0.0:%v\" .Values.ports.rest | quote }}\n" +
		"      {{- end }}\n"

	got, _ := injRestAudit(t, "cm.yaml", defect, nil)
	require.NotEmpty(t, got, "безусловный рендер адреса фронта обязан быть находкой")
	require.Contains(t, strings.Join(got, "\n"), "материализуется БЕЗУСЛОВНО")

	got, census := injRestAudit(t, "cm.yaml", twin, nil)
	require.Empty(t, got, "обусловленный объявлением рендер обязан молчать")
	require.Equal(t, 1, census.guarded, "перепись обязана засчитать обусловленное место")
}

func TestOwnRestFrontInjection_ConditionOnAnotherFrontDoesNotCount(t *testing.T) {
	// Условие есть, но оно про ДРУГОЙ фронт: публичный по-прежнему поднимается
	// безусловно. Это ровно тот способ ошибиться, который выглядит починкой.
	defect := "spec:\n  ports:\n" +
		"    {{- if .Values.ports.internalRest }}\n" +
		"    - name: http-rest\n      port: {{ .Values.ports.rest }}\n" +
		"    {{- end }}\n"
	twin := "spec:\n  ports:\n" +
		"    {{- if .Values.ports.rest }}\n" +
		"    - name: http-rest\n      port: {{ .Values.ports.rest }}\n" +
		"    {{- end }}\n"

	got, _ := injRestAudit(t, "svc.yaml", defect, nil)
	require.NotEmpty(t, got, "условие про соседний фронт обусловленностью не является")

	got, _ = injRestAudit(t, "svc.yaml", twin, nil)
	require.Empty(t, got, "условие про СВОЙ фронт обязано молчать")
}

func TestOwnRestFrontInjection_InlineDefaultIsAFinding(t *testing.T) {
	defect := "spec:\n  ports:\n" +
		"    {{- if .Values.ports.rest }}\n" +
		"    - name: http-rest\n      port: {{ .Values.ports.rest | default 9098 }}\n" +
		"    {{- end }}\n"
	twin := "spec:\n  ports:\n" +
		"    {{- if .Values.ports.rest }}\n" +
		"    - name: http-rest\n      port: {{ .Values.ports.rest }}\n" +
		"    {{- end }}\n"

	got, _ := injRestAudit(t, "svc.yaml", defect, nil)
	require.NotEmpty(t, got, "встроенное в шаблон умолчание порта обязано быть находкой")
	require.Contains(t, strings.Join(got, "\n"), "ВСТРОЕННОЕ в шаблон умолчание")

	got, _ = injRestAudit(t, "svc.yaml", twin, nil)
	require.Empty(t, got, "тот же рендер без встроенного умолчания обязан молчать")
}

func TestOwnRestFrontInjection_DerivedFromTheSameDeclarationIsNotADefault(t *testing.T) {
	// Номер, выведенный ИЗ ТОГО ЖЕ объявления фронта, умолчанием не является:
	// он не может пережить снятие фронта. Отличие от предыдущей оси ровно в
	// одном факте — вместо числа стоит тот же ключ.
	twin := "spec:\n  ports:\n" +
		"    {{- if .Values.ports.rest }}\n" +
		"    - name: http-rest\n" +
		"      port: {{ .Values.service.public.restPort | default .Values.ports.rest }}\n" +
		"    {{- end }}\n"
	got, _ := injRestAudit(t, "svc.yaml", twin, nil)
	require.Empty(t, got,
		"вывод номера из объявления фронта — не умолчание: снимется фронт — снимется и номер")
}

func TestOwnRestFrontInjection_ChartDefaultIsAFinding(t *testing.T) {
	tpl := "spec:\n  ports:\n" +
		"    {{- if .Values.ports.rest }}\n" +
		"    - name: http-rest\n      port: {{ .Values.ports.rest }}\n" +
		"    {{- end }}\n"

	got, _ := injRestAudit(t, "svc.yaml", tpl,
		[]restFrontDefault{{chart: "charts/kaname", key: "ports.rest", front: "публичный"}})
	require.NotEmpty(t, got, "умолчание чарта обязано быть находкой даже при обусловленном рендере")
	require.Contains(t, strings.Join(got, "\n"), "умолчание чарта")

	got, _ = injRestAudit(t, "svc.yaml", tpl, nil)
	require.Empty(t, got, "тот же шаблон без умолчания чарта обязан молчать")
}

func TestOwnRestFrontInjection_ProseIsNotARender(t *testing.T) {
	// Распознаватель судит исполняемую часть: строка комментария, объясняющая
	// снятое умолчание, рендером не является — иначе гейт краснел бы на
	// собственном объяснении, а оно стоит в дереве рядом с каждой правкой.
	tpl := "spec:\n  ports:\n" +
		"    # Здесь стояло `port: {{ .Values.ports.rest | default 9098 }}` — умолчание снято.\n" +
		"    {{- if .Values.ports.rest }}\n" +
		"    - name: http-rest\n      port: {{ .Values.ports.rest }}\n" +
		"    {{- end }}\n"
	got, _ := injRestAudit(t, "svc.yaml", tpl, nil)
	require.Empty(t, got, "проза о снятом умолчании умолчанием не является")
}

func TestOwnRestFrontInjection_UnknownPortNameIsNotSilence(t *testing.T) {
	// Ноль распознанных мест — «ноль прочитанного», а не «ноль находок».
	// Гейт обязан на этом отказать; здесь проверяется, что распознаватель
	// действительно ничего не нашёл, а не что дерево чисто.
	renders := collectRestFrontRenders("svc.yaml",
		"spec:\n  ports:\n    - name: http-grpc\n      port: 9090\n")
	require.Empty(t, renders,
		"чужой порт местом материализации фронта не является — иначе перепись врёт вверх")
}
