// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// cachedverdict_test.go — пробы трёх состояний и текста отказа.
//
// Предмет пакета — РАЗЛИЧЕНИЕ: отказ по установленному факту и отказ по
// незнанию обязаны быть разными и выглядеть по-разному. Поэтому здесь стоит не
// одна проба «отказывает», а контроль в обе стороны по каждому состоянию: вход,
// на котором ответ обязан быть, и законный близнец, на котором обязано быть
// молчание.
package cachedverdict

import (
	"strings"
	"testing"
)

// argvOfRealToolRuns — формы командной строки, замеренные на go1.26.8
// (см. врезку пакета). Именно они, а не сочинённые: предикат обязан отвечать о
// том, что инструмент печатает на самом деле.
var (
	argvCacheable = []string{
		"/tmp/go-build1/b001/x.test",
		"-test.testlogfile=/tmp/go-build1/b001/testlog.txt",
		"-test.paniconexit0",
		"-test.timeout=10m0s",
	}
	argvCountOne = []string{
		"/tmp/go-build2/b001/x.test",
		"-test.paniconexit0",
		"-test.timeout=10m0s",
		"-test.count=1",
	}
	argvHandRun = []string{"/home/dk/x.test", "-test.v"}
	argvBare    = []string{"/home/dk/x.test"}
)

func TestObserveSeparatesTheThreeStates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		argv         []string
		willBeCached bool
		want         RunState
	}{
		{"инструмент заявил журнал обращений", argvCacheable, true, StateCached},
		{"инструмент запустил и кеш отключён", argvCountOne, false, StateNotCached},
		{"двоичный запущен руками с -test.v", argvHandRun, false, StateUnknown},
		{"двоичный запущен вовсе без флагов", argvBare, false, StateUnknown},
		{"командной строки нет", nil, false, StateUnknown},
	}
	for _, c := range cases {
		if got := observe(c.argv, c.willBeCached); got != c.want {
			t.Errorf("%s: состояние %q, ожидалось %q (argv=%q)", c.name, got, c.want, c.argv)
		}
	}
}

// TestRefusalSaysWhichOfTheThreeItIs — три состояния дают ТРИ разных исхода, и
// два текста отказа не путаются друг с другом.
func TestRefusalSaysWhichOfTheThreeItIs(t *testing.T) {
	t.Parallel()

	prev := runArgs
	t.Cleanup(func() { runArgs = prev })

	// Молчание — только на установленном «не кэшируемый». Это законный близнец:
	// без него отказ, срабатывающий всегда, выглядел бы работающим.
	runArgs = argvCountOne
	if msg := refusalFor("helm", observe(runArgs, false)); msg != "" {
		t.Errorf("на установленном НЕ кэшируемом прогоне отказ обязан молчать, а он сказал:\n%s", msg)
	}

	cached := refusalFor("helm", StateCached)
	unknown := refusalFor("helm", StateUnknown)
	if cached == "" || unknown == "" {
		t.Fatalf("оба состояния обязаны давать текст; кэшируемый=%q, не установлено=%q", cached, unknown)
	}
	if cached == unknown {
		t.Fatal("отказ по незнанию совпал с отказом по факту — различить их читателю нечем")
	}
	if !strings.Contains(unknown, "НЕ УСТАНОВЛЕНО") || !strings.Contains(unknown, "по НЕЗНАНИЮ") {
		t.Errorf("отказ по незнанию не называет себя незнанием:\n%s", unknown)
	}
	if strings.Contains(cached, "НЕ УСТАНОВЛЕНО") {
		t.Errorf("отказ по факту говорит о неустановленном:\n%s", cached)
	}
	for _, want := range []string{"helm", "-count=1", "make test-unit"} {
		if !strings.Contains(cached, want) {
			t.Errorf("текст отказа по факту не несёт %q:\n%s", want, cached)
		}
		if !strings.Contains(unknown, want) {
			t.Errorf("текст отказа по незнанию не несёт %q:\n%s", want, unknown)
		}
	}
}

// TestRefusalCarriesTheMitigatingFactAndDoesNotTurnItIntoAnExcuse — смягчающее
// («конвейер гоняет с -count=1») обязано быть В ТЕКСТЕ, и обязано быть названо
// областью, а не оправданием.
func TestRefusalCarriesTheMitigatingFactAndDoesNotTurnItIntoAnExcuse(t *testing.T) {
	t.Parallel()
	msg := refusalFor("helm", StateCached)
	for _, want := range []string{
		"-race -short -count=1",
		"не живой",
		"ОБЛАСТИ, а не оправдание",
		"ОТКАЗЫВАЕТСЯ отвечать",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("текст отказа не несёт %q — смягчающее либо не названо, либо названо оправданием:\n%s", want, msg)
		}
	}
}
