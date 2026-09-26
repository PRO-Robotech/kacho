// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// knob_producer_parity_injection_test.go — способность двухколоночного гейта
// упасть и смолчать, доказанная на СИНТЕТИКЕ: живая запись ведомости истекла
// бы вместе со своим предметом и увела бы пробу с собой.
//
// Каждая пара меняет РОВНО ОДИН факт против законного близнеца.
package deploy_test

import (
	"fmt"
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

// knobEmissionForms — ЗАКОННЫЕ формы записи элемента `env:` в YAML шаблона, каждая
// с одним и тем же именем-сиротой на известной строке. Перечень выведен из
// грамматики элемента списка, а не из того, что сегодня встречается в чарте
// (сегодня — только первая): форма, которой в чарте нет, ровно так же законна,
// и ручка-сирота, записанная ею, до этого перечня не краснела ни в одной
// колонке.
var knobEmissionForms = []struct {
	name string
	body string
	line int
}{
	{"голое имя", "env:\n  - name: KACHO_X_ORPHAN\n    value: \"1\"\n", 2},
	{"имя в двойных кавычках", "env:\n  - name: \"KACHO_X_ORPHAN\"\n    value: \"1\"\n", 2},
	{"имя в одинарных кавычках", "env:\n  - name: 'KACHO_X_ORPHAN'\n    value: \"1\"\n", 2},
	{"хвостовой комментарий", "env:\n  - name: KACHO_X_ORPHAN  # пояснение\n    value: \"1\"\n", 2},
	{"ключ в кавычках", "env:\n  - \"name\": KACHO_X_ORPHAN\n    value: \"1\"\n", 2},
	{"имя не первым ключом элемента", "env:\n  - value: \"1\"\n    name: KACHO_X_ORPHAN\n", 3},
	{"потоковая форма", "env:\n  - {name: KACHO_X_ORPHAN, value: \"1\"}\n", 2},
	{"потоковая форма с кавычками и пробелами", "env:\n  - { value: \"1\", name: \"KACHO_X_ORPHAN\" }\n", 2},
}

// Каждая законная форма: эмиссия найдена на своей строке, «назад» краснеет с
// координатой, «снятое» краснеет с координатой, перепись форм пуста.
func TestKnobParityInjection_EveryLawfulEmissionFormIsSeen(t *testing.T) {
	for _, f := range knobEmissionForms {
		scan := scanTemplate("t.yaml", f.body)
		where := fmt.Sprintf("t.yaml:%d", f.line)
		if got := scan.Emitted["KACHO_X_ORPHAN"]; len(got) != 1 || got[0] != where {
			t.Errorf("%s: эмиссия не найдена на %s: %v (перепись форм %v)", f.name, where, scan.Emitted, scan.Unrecognized)
			continue
		}
		if len(scan.Unrecognized) != 0 {
			t.Errorf("%s: знакомая форма записана в нераспознанные: %v", f.name, scan.Unrecognized)
		}
		cols := knobColumns{Declared: map[string]bool{}, Emitted: scan.Emitted, TemplateFiles: 1, TemplateLines: 1}
		if back := judgeEmittedWithoutReader(cols); len(back) != 1 || !strings.Contains(back[0], where) {
			t.Errorf("%s: сирота не найдена «назад» с координатой %s: %q", f.name, where, back)
		}
		c := judgeRetiredKnobs(map[string]string{"KACHO_X_ORPHAN": "снята вместе с читателем"}, cols)
		if c.StillEmitted != 1 || len(c.FindingsByKnob) != 1 || !strings.Contains(c.FindingsByKnob[0], where) {
			t.Errorf("%s: эмиссия снятой ручки не найдена с координатой %s: %+v", f.name, where, c)
		}
	}
}

// Законные близнецы: имя продукта в НЕисполняемой части — в строке-комментарии,
// в хвостовом комментарии, в комментарии шаблона на одной и на нескольких
// строках, — не эмиссия и не нераспознанная форма. Имя не продукта в той же
// форме — не эмиссия.
func TestKnobParityInjection_NameOutsideTheExecutablePartIsSilent(t *testing.T) {
	body := "env:\n" +
		"  # - name: \"KACHO_X_PROSE\"\n" +
		"  - name: KACHO_X_REAL  # была KACHO_X_PROSE\n" +
		"    value: \"1\"\n" +
		"  {{- /* - name: KACHO_X_PROSE */}}\n" +
		"  {{- /* снятая ручка:\n" +
		"         name: KACHO_X_PROSE\n" +
		"         {name: KACHO_X_PROSE} */ -}}\n" +
		"  - name: volume-name\n" +
		"  - {name: other-name, value: \"KACHO\"}\n"
	scan := scanTemplate("t.yaml", body)
	if where := scan.Emitted["KACHO_X_REAL"]; len(where) != 1 || where[0] != "t.yaml:3" {
		t.Fatalf("исполняемая эмиссия не найдена на своей строке: %v", scan.Emitted)
	}
	if len(scan.Emitted) != 1 {
		t.Fatalf("эмиссией засчитано имя из неисполняемой части или имя не продукта: %v", scan.Emitted)
	}
	if len(scan.Unrecognized) != 0 {
		t.Fatalf("имя в неисполняемой части записано нераспознанной формой: %v", scan.Unrecognized)
	}
	if scan.NamedLines != 1 {
		t.Fatalf("исполняемых строк с именем продукта %d, ожидалась 1", scan.NamedLines)
	}
}

// Имя продукта на исполняемой строке в форме, которой распознаватель НЕ знает, —
// находка с координатой, а не молчание: блочный скаляр, ссылка на переменную в
// значении. Без этой половины каждая следующая незнакомая форма снова была бы
// слепой зоной, и заметить её было бы нечем.
func TestKnobParityInjection_UnknownFormIsAFindingWithItsCoordinate(t *testing.T) {
	body := "env:\n" +
		"  - name: >-\n" +
		"      KACHO_X_FOLDED\n" +
		"  - name: KACHO_X_REAL\n" +
		"    value: \"$(KACHO_X_REF)/v1\"\n"
	scan := scanTemplate("t.yaml", body)
	if scan.NamedLines != 3 {
		t.Fatalf("исполняемых строк с именем продукта %d, ожидалось 3: %v", scan.NamedLines, scan.Unrecognized)
	}
	cols := knobColumns{Declared: map[string]bool{"KACHO_X_REAL": true}, Emitted: scan.Emitted,
		Unrecognized: scan.Unrecognized, TemplateFiles: 1, TemplateLines: 1}
	got := judgeUnrecognizedForms(cols)
	if len(got) != 2 || !strings.Contains(got[0], "t.yaml:3") || !strings.Contains(got[1], "t.yaml:5") {
		t.Fatalf("незнакомые формы обязаны быть двумя находками с координатами t.yaml:3 и t.yaml:5: %q", got)
	}
	if back := judgeEmittedWithoutReader(cols); len(back) != 0 {
		t.Fatalf("объявленная эмиссия в знакомой форме объявлена находкой: %q", back)
	}
}
