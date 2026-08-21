// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// reportprosematchestemplate_injection_test.go — ДОКАЗАТЕЛЬСТВО ИНЪЕКЦИЕЙ для
// сверки прозы отчёта с литералом, который её печатает.
//
// Соседний гейт утверждает «расхождений нет». Утверждение стоит ровно столько,
// сколько стоит способность сверки ЗАМЕТИТЬ расхождение: проверка, всегда
// возвращающая пустой список, дала бы тот же зелёный — и была бы неотличима от
// работающей.
//
// Обе стороны проверяются по каждой оси: правленый без пересъёмки шаблон обязан
// быть НАЗВАН, а сдвинувшиеся вместе — пройти молча.

import (
	"strings"
	"testing"
)

func TestProseGateSeesTheDriftAndKeepsQuietWithoutIt(t *testing.T) {
	const (
		oldProse = "R7-3 — ПРИБОР ОБЪЁМА: ОДНА ОПЕРАЦИЯ ПРОТИВ НАЛИТОЙ МАТРИЦЫ"
		newProse = "R7-3 — ПРИБОР ОБЪЁМА: ОДНА ОПЕРАЦИЯ ПРОТИВ НАЛИТОЙ СЕТКИ"
	)
	tpl := []tmpl{{where: "probe_test.go:1", literal: newProse}}

	// ── (а) ДЕФЕКТ: шаблон правлен, отчёт не переснят.
	stale := map[string]string{"REPORT-x.txt": "шапка\n" + oldProse + "\nчисла"}
	got := proseMissingFromReports(tpl, stale)
	if len(got) != 1 {
		t.Fatalf("правленый без пересъёмки шаблон не назван: находок %d, ждали 1 (%v)", len(got), got)
	}
	if !strings.Contains(got[0], "probe_test.go:1") {
		t.Fatalf("находка без координаты: %q", got[0])
	}

	// ── (б) ЗАКОННЫЙ БЛИЗНЕЦ: сдвинулись вместе — молчание.
	fresh := map[string]string{"REPORT-x.txt": "шапка\n" + newProse + "\nчисла"}
	if got := proseMissingFromReports(tpl, fresh); len(got) != 0 {
		t.Fatalf("пересня́тый отчёт покрашен: %v", got)
	}

	// ── (в) ЗАКОННЫЙ БЛИЗНЕЦ: отчётов несколько, совпадение в НЕ ПЕРВОМ.
	//        Без этого случая сверка, смотрящая только в первый отчёт, прошла бы
	//        (а) и (б) и всё равно была бы неверна.
	many := map[string]string{
		"REPORT-a.txt": "чужая шапка",
		"REPORT-b.txt": newProse,
	}
	if got := proseMissingFromReports(tpl, many); len(got) != 0 {
		t.Fatalf("совпадение во втором отчёте не найдено: %v", got)
	}

	// ── (г) ДЕФЕКТ: корпус пуст — совпасть не с чем, и это находка, а не тишина.
	if got := proseMissingFromReports(tpl, map[string]string{}); len(got) != 1 {
		t.Fatalf("на пустом корпусе сверка молчит — значит она молчит всегда: %v", got)
	}
}
