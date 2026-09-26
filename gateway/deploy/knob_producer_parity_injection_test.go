// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// knob_producer_parity_injection_test.go — способность двухколоночного гейта
// упасть и смолчать, доказанная на СИНТЕТИКЕ: живая запись ведомости истекла
// бы вместе со своим предметом и увела бы пробу с собой.
//
// Каждая пара меняет РОВНО ОДИН факт против законного близнеца.
package deploy_test

import (
	"strings"
	"testing"
)

func syntheticColumns(declared []string, emitted map[string]string) knobColumns {
	c := knobColumns{Declared: map[string]bool{}, Emitted: map[string][]string{},
		TemplateFiles: 1, TemplateLines: 1}
	for _, d := range declared {
		c.Declared[d] = true
	}
	for name, where := range emitted {
		c.Emitted[name] = []string{where}
	}
	return c
}

// Эмиссия имени, которого процесс не объявляет, — находка с координатой.
func TestKnobParityInjection_EmittedButUndeclaredIsFound(t *testing.T) {
	cols := syntheticColumns([]string{"KACHO_X_LIVE"},
		map[string]string{"KACHO_X_LIVE": "t.yaml:1", "KACHO_X_ORPHAN": "t.yaml:7"})
	got := judgeEmittedWithoutReader(cols)
	if len(got) != 1 || !strings.Contains(got[0], "KACHO_X_ORPHAN") || !strings.Contains(got[0], "t.yaml:7") {
		t.Fatalf("эмиссия без читателя не найдена или найдена без координаты: %q", got)
	}
}

// Законный близнец: то же имя объявлено — молчание.
func TestKnobParityInjection_EmittedAndDeclaredIsSilent(t *testing.T) {
	cols := syntheticColumns([]string{"KACHO_X_LIVE", "KACHO_X_ORPHAN"},
		map[string]string{"KACHO_X_LIVE": "t.yaml:1", "KACHO_X_ORPHAN": "t.yaml:7"})
	if got := judgeEmittedWithoutReader(cols); len(got) != 0 {
		t.Fatalf("объявленная эмиссия объявлена находкой: %q", got)
	}
}

// Снятая ручка, которую процесс снова объявил, — находка стороны читателя.
func TestKnobParityInjection_RetiredButDeclaredIsFound(t *testing.T) {
	cols := syntheticColumns([]string{"KACHO_X_GONE"}, map[string]string{})
	c := judgeRetiredKnobs(map[string]string{"KACHO_X_GONE": "снята вместе с читателем"}, cols)
	if c.StillDeclared != 1 || len(c.FindingsByKnob) != 1 ||
		!strings.Contains(c.FindingsByKnob[0], "объявляет") {
		t.Fatalf("возврат снятой ручки читателю не найден: %+v", c)
	}
}

// Снятая ручка, которую чарт всё ещё эмитирует, — находка стороны
// производителя, с координатой.
func TestKnobParityInjection_RetiredButEmittedIsFound(t *testing.T) {
	cols := syntheticColumns(nil, map[string]string{"KACHO_X_GONE": "t.yaml:9"})
	c := judgeRetiredKnobs(map[string]string{"KACHO_X_GONE": "снята вместе с читателем"}, cols)
	if c.StillEmitted != 1 || len(c.FindingsByKnob) != 1 ||
		!strings.Contains(c.FindingsByKnob[0], "t.yaml:9") {
		t.Fatalf("эмиссия снятой ручки не найдена или найдена без координаты: %+v", c)
	}
}

// Законный близнец: снятая ручка не объявлена и не эмитируется — молчание.
func TestKnobParityInjection_RetiredAndGoneIsSilent(t *testing.T) {
	cols := syntheticColumns([]string{"KACHO_X_LIVE"}, map[string]string{"KACHO_X_LIVE": "t.yaml:1"})
	c := judgeRetiredKnobs(map[string]string{"KACHO_X_GONE": "снята вместе с читателем"}, cols)
	if c.Retired != 1 || c.StillDeclared != 0 || c.StillEmitted != 0 || len(c.FindingsByKnob) != 0 {
		t.Fatalf("законно снятая ручка объявлена находкой: %+v", c)
	}
}

// Запись без обоснования — находка о самой ведомости.
func TestKnobParityInjection_RetiredWithoutReasonIsFound(t *testing.T) {
	cols := syntheticColumns(nil, map[string]string{})
	c := judgeRetiredKnobs(map[string]string{"KACHO_X_GONE": " "}, cols)
	if len(c.FindingsByKnob) != 1 || !strings.Contains(c.FindingsByKnob[0], "обоснования") {
		t.Fatalf("запись без обоснования не найдена: %+v", c)
	}
}

// Комментарий шаблона не исполняется: имя в прозе не является эмиссией, а
// такое же имя в исполняемой строке — является.
func TestKnobParityInjection_CommentIsNotAnEmission(t *testing.T) {
	body := "env:\n" +
		"  # - name: KACHO_X_PROSE\n" +
		"  - name: KACHO_X_REAL\n" +
		"    value: \"1\"\n" +
		"  - name: volume-name\n"
	got := templateEmissions("t.yaml", body)
	if _, prose := got["KACHO_X_PROSE"]; prose {
		t.Fatalf("имя из комментария засчитано эмиссией: %v", got)
	}
	if where := got["KACHO_X_REAL"]; len(where) != 1 || where[0] != "t.yaml:3" {
		t.Fatalf("исполняемая эмиссия не найдена на своей строке: %v", got)
	}
	if len(got) != 1 {
		t.Fatalf("в перепись попало имя не продукта: %v", got)
	}
}
