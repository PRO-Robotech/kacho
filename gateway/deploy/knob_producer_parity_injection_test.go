// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// knob_producer_parity_injection_test.go — ИНЪЕКЦИЯ в обе стороны для гейта
// парности «объявлено процессом · эмитируется чартом».
//
// Инъекция строится на СИНТЕТИКЕ, а не на живой записи ведомости: проба,
// привязанная к снимаемому предмету, истекает вместе с ним — ведомость
// переименования однажды опустеет, и самопроверка покраснела бы на достижении
// своей цели.
//
// Зовётся ТО ЖЕ тело, что исполняется на дереве (judge*-функции), а не его
// копия: своя копия предиката разошлась бы с настоящей пробой молча.
package deploy_test

import (
	"strings"
	"testing"
)

// synthColumns — минимальные колонки под инъекцию.
func synthColumns(declared []string, edge, tree map[string][]string) knobColumns {
	cols := knobColumns{
		Declared:          map[string]bool{},
		EmittedByEdge:     edge,
		EmittedInTree:     tree,
		TemplateFiles:     1,
		EdgeTemplateFiles: 1,
	}
	for _, d := range declared {
		cols.Declared[d] = true
	}
	if cols.EmittedByEdge == nil {
		cols.EmittedByEdge = map[string][]string{}
	}
	if cols.EmittedInTree == nil {
		cols.EmittedInTree = map[string][]string{}
	}
	return cols
}

// ДЕФЕКТ, НАЗВАННЫЙ АУДИТОМ: ручку ПЕРЕИМЕНОВАЛИ в объявлении процесса, а
// производитель остался на прежнем имени. Обязано находиться — и назвать ОБЕ
// половины: старое имя всё ещё эмитируется, новое не эмитируется ничем.
func TestInjection_ARenamedKnobWhoseProducerStayedBehindIsFound(t *testing.T) {
	t.Parallel()
	cols := synthColumns(
		[]string{"KACHO_SYNTH_NEW_NAME"},
		map[string][]string{"KACHO_SYNTH_OLD_NAME": {"templates/synthetic.yaml:1"}},
		map[string][]string{"KACHO_SYNTH_OLD_NAME": {"templates/synthetic.yaml:1"}},
	)
	findings := judgeRenamedKnobProducers(
		map[string]string{"KACHO_SYNTH_OLD_NAME": "KACHO_SYNTH_NEW_NAME"}, cols)
	if len(findings) != 2 {
		t.Fatalf("переименование с отставшим производителем: находок %d, ожидалось 2: %v",
			len(findings), findings)
	}
	joined := strings.Join(findings, "\n")
	if !strings.Contains(joined, "СНЯТОЕ имя всё ещё эмитируется") {
		t.Errorf("находка не называет оставшегося производителя: %s", joined)
	}
	if !strings.Contains(joined, "не эмитит НИ ОДИН") {
		t.Errorf("находка не называет ОТСУТСТВИЕ производителя у живого имени: %s", joined)
	}
	if !strings.Contains(joined, "templates/synthetic.yaml:1") {
		t.Errorf("находка не называет координату: %s", joined)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ: производитель переехал вместе с читателем — гейт молчит.
//
// Против дефекта меняется РОВНО ОДИН факт: какое имя эмитит шаблон.
func TestInjection_ARenamedKnobWhoseProducerMovedIsSilent(t *testing.T) {
	t.Parallel()
	cols := synthColumns(
		[]string{"KACHO_SYNTH_NEW_NAME"},
		map[string][]string{"KACHO_SYNTH_NEW_NAME": {"templates/synthetic.yaml:1"}},
		map[string][]string{"KACHO_SYNTH_NEW_NAME": {"templates/synthetic.yaml:1"}},
	)
	findings := judgeRenamedKnobProducers(
		map[string]string{"KACHO_SYNTH_OLD_NAME": "KACHO_SYNTH_NEW_NAME"}, cols)
	if len(findings) != 0 {
		t.Fatalf("переехавший производитель объявлен находкой: %v", findings)
	}
}

// ПУСТАЯ ведомость переименования — ЦЕЛЬ, а не отказ: проба не имеет права
// падать на достижении своей цели.
func TestInjection_AnEmptyRenameLedgerIsTheGoalNotAFailure(t *testing.T) {
	t.Parallel()
	cols := synthColumns([]string{"KACHO_SYNTH_NEW_NAME"},
		map[string][]string{"KACHO_SYNTH_NEW_NAME": {"templates/synthetic.yaml:1"}},
		map[string][]string{"KACHO_SYNTH_NEW_NAME": {"templates/synthetic.yaml:1"}})
	if findings := judgeRenamedKnobProducers(map[string]string{}, cols); len(findings) != 0 {
		t.Fatalf("пустая ведомость объявлена находкой: %v", findings)
	}
}

// ДЕФЕКТ НАЗАД: чарт эмитит имя, которого не объявляет ни одно поле, и записи в
// ведомости у него нет. Обязано находиться СВЕЖЕЙ находкой.
func TestInjection_AnEmittedKnobWithNoReaderIsFound(t *testing.T) {
	t.Parallel()
	cols := synthColumns(
		[]string{"KACHO_SYNTH_LIVE"},
		map[string][]string{
			"KACHO_SYNTH_LIVE": {"templates/synthetic.yaml:1"},
			"KACHO_SYNTH_DEAD": {"templates/synthetic.yaml:2"},
		}, nil)
	fresh, known := judgeEmittedWithoutReader(cols, nil)
	if len(known) != 0 {
		t.Fatalf("при пустой ведомости запись попала в известные: %v", known)
	}
	if len(fresh) != 1 || !strings.Contains(fresh[0], "KACHO_SYNTH_DEAD") {
		t.Fatalf("мёртвая эмиссия не найдена: %v", fresh)
	}
	if !strings.Contains(fresh[0], "templates/synthetic.yaml:2") {
		t.Errorf("находка не называет координату: %v", fresh)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ: та же эмиссия, но у имени ЕСТЬ читатель — гейт молчит.
func TestInjection_AnEmittedKnobWithAReaderIsSilent(t *testing.T) {
	t.Parallel()
	cols := synthColumns(
		[]string{"KACHO_SYNTH_LIVE", "KACHO_SYNTH_DEAD"},
		map[string][]string{
			"KACHO_SYNTH_LIVE": {"templates/synthetic.yaml:1"},
			"KACHO_SYNTH_DEAD": {"templates/synthetic.yaml:2"},
		}, nil)
	fresh, _ := judgeEmittedWithoutReader(cols, nil)
	if len(fresh) != 0 {
		t.Fatalf("живая эмиссия объявлена находкой: %v", fresh)
	}
}

// ВЕДОМОСТЬ: запись переводит находку из свежих в известные — и не в зелень.
func TestInjection_ADebtEntryMovesTheFindingOutOfFreshButNotOutOfSight(t *testing.T) {
	t.Parallel()
	cols := synthColumns([]string{}, map[string][]string{
		"KACHO_SYNTH_DEAD": {"templates/synthetic.yaml:2"},
	}, nil)
	fresh, known := judgeEmittedWithoutReader(cols, []knobEmitterDebtEntry{
		{Knob: "KACHO_SYNTH_DEAD", Why: "почему"},
	})
	if len(fresh) != 0 {
		t.Fatalf("записанная находка осталась свежей: %v", fresh)
	}
	if len(known) != 1 {
		t.Fatalf("записанная находка исчезла из виду целиком: %v", known)
	}
}

// ВЕДОМОСТЬ: запись без обоснования и запись без координаты — находки сами по
// себе, безотносительно дерева. Обоснование приходит из ведомости процесса, и
// запись, добавленная туда без причины, обязана быть замечена здесь.
func TestInjection_ABareDebtEntryIsFound(t *testing.T) {
	t.Parallel()
	defects := knobEmitterDebtDefects([]knobEmitterDebtEntry{
		{Knob: "KACHO_SYNTH_A"},
		{Knob: "", Why: "есть"},
		{Knob: "KACHO_SYNTH_B", Why: "есть"},
		{Knob: "KACHO_SYNTH_B", Why: "есть"},
	})
	joined := strings.Join(defects, "\n")
	for _, want := range []string{
		"без письменного обоснования",
		"запись без координаты",
		"дубль в ведомости",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("дефект ведомости %q не назван: %s", want, joined)
		}
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ ведомости: полная запись молчит, ПУСТАЯ ведомость — тоже.
func TestInjection_AFullDebtEntryAndAnEmptyLedgerAreSilent(t *testing.T) {
	t.Parallel()
	if d := knobEmitterDebtDefects([]knobEmitterDebtEntry{
		{Knob: "KACHO_SYNTH_A", Why: "почему"},
	}); len(d) != 0 {
		t.Fatalf("полная запись объявлена дефектом: %v", d)
	}
	if d := knobEmitterDebtDefects(nil); len(d) != 0 {
		t.Fatalf("пустая ведомость объявлена дефектом: %v", d)
	}
}

// ВЕДОМОСТЬ ВЫВОДИТСЯ ПЕРЕСЕЧЕНИЕМ: снятая ручка, которую чарт БОЛЬШЕ НЕ
// эмитит, в ведомость не попадает вовсе — исключению нечего пережить.
func TestInjection_ARetiredKnobNoLongerEmittedLeavesTheLedgerByItself(t *testing.T) {
	t.Parallel()
	retired := map[string]string{
		"KACHO_SYNTH_STILL_EMITTED": "снята вместе с читателем",
		"KACHO_SYNTH_GONE":          "снята вместе с читателем и с производителем",
	}
	debt := knobEmitterDebt(retired, map[string][]string{
		"KACHO_SYNTH_STILL_EMITTED": {"templates/synthetic.yaml:1"},
	})
	if len(debt) != 1 || debt[0].Knob != "KACHO_SYNTH_STILL_EMITTED" {
		t.Fatalf("ведомость выведена неверно: %+v", debt)
	}
}
