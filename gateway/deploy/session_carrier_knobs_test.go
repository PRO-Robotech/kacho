// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// session_carrier_knobs_test.go — СОСТОЯНИЕ НОСИТЕЛЯ ВЫРАЗИМО ПРОФИЛЕМ.
//
// Ручки множества читателей и момента открытия окна встречались только в
// примере страницы настройки. Значит состояние, ради которого делалась вся
// работа, не выразимо НИ ОДНИМ профилем — только свободной картой переменных,
// которую не судит ни гейт чарта, ни декларативная проба. Ручка, объявленная
// процессом и не объявленная чартом, выглядит настраиваемой и не настраивается
// ничем: ровно класс `producerless_input_test.go`, только со стороны входа.
//
// Рядом посадка личности объявлена первоклассным блоком с разобранным доводом,
// и здесь сделано так же: ключ в `authn`, эмиссия РОВНО из него и только при
// непустом значении. Пустая переменная, доехав, читалась бы процессом так же,
// как отсутствующая, — «не задано» (config.ResolvedSessionCarriers,
// ResolvedSessionCarrierWindowOpenedAt), — поэтому `with` различает не
// состояния процесса, а то, что видно в поде: незаданная ручка переменной не
// рождает.
func TestSessionCarrierKnobs_AreDeclaredByTheChartAndRenderedFromOneKey(t *testing.T) {
	raw, err := os.ReadFile("values.yaml")
	if err != nil {
		t.Fatalf("чтение объявления чарта: %v", err)
	}
	var values struct {
		Authn map[string]any `yaml:"authn"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("values.yaml не разбирается: %v", err)
	}

	tpl, err := os.ReadFile("templates/deployment.yaml")
	if err != nil {
		t.Fatalf("чтение шаблона: %v", err)
	}
	body := string(tpl)

	cases := []struct {
		key  string
		knob string
		why  string
	}{
		{"sessionCarriers", config.SessionCarriersKnob,
			"чьё печенье край читает: только чужой · оба · только наш"},
		{"sessionCarrierWindowOpenedAt", config.SessionCarrierWindowOpenedAtKnob,
			"момент открытия переходного окна — граница между дочитываемыми и новыми"},
	}
	declared, rendered, guarded := 0, 0, 0
	for _, tc := range cases {
		v, ok := values.Authn[tc.key]
		if !ok {
			t.Errorf("ключ authn.%s не объявлен в базовом профиле — %s нечем задать ни одному "+
				"профилю, и состояние остаётся невыразимым", tc.key, tc.knob)
			continue
		}
		declared++
		// Базовый профиль объявляет посадку external: обе ручки обязаны быть
		// ПУСТЫ. Непустое значение здесь молча открыло бы окно на каждом
		// стенде, который профиль не переопределял.
		if s, _ := v.(string); s != "" {
			t.Errorf("базовый профиль объявляет external и обязан оставить authn.%s пустым, "+
				"получено %q", tc.key, s)
		}
		if !strings.Contains(body, "- name: "+tc.knob) {
			t.Errorf("шаблон не эмитит %s — ключ объявлен и до процесса не доезжает", tc.knob)
			continue
		}
		rendered++
		// Эмиссия обязана стоять под `with` того же ключа: незаданная ручка не
		// рождает переменной. Процесс прочёл бы пустую переменную как «не
		// задано», так что `with` держит форму эмиссии, а не различие состояний.
		if !strings.Contains(body, "{{- with .Values.authn."+tc.key+" }}") {
			t.Errorf("%s эмитится не под `with .Values.authn.%s` — пустое значение доедет "+
				"переменной, и «не задано» станет неотличимо от «задано пустым»", tc.knob, tc.key)
			continue
		}
		guarded++
	}
	t.Logf("перепись: ручек носителя %d · объявлено ключом %d · эмитится шаблоном %d · "+
		"под стражем пустоты %d", len(cases), declared, rendered, guarded)
}
