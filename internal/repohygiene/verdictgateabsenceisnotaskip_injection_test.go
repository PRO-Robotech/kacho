// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verdictgateabsenceisnotaskip_injection_test.go — доказательство способности
// гейта упасть и смолчать.
//
// Гейт ловит МОЛЧАЛИВЫЙ пропуск, поэтому вакуумным он становится проще всего:
// достаточно, чтобы распознаватель перестал узнавать форму условия, — и всякий
// пропуск пройдёт мимо, не дав ни красного, ни зелёного. Поэтому по каждой оси
// стоит пара, а близнец отличается от инъекции РОВНО ОДНИМ фактом.
//
// Фикстуры — дословный текст ДО и ПОСЛЕ починки настоящих прогонщиков, а не
// придуманная форма: инъекция обязана воспроизводить дефект, который был.
package repohygiene

import (
	"strings"
	"testing"
)

// injRunnerPreamble — общая часть обеих фикстур: переменная несёт путь скрипта,
// скрипт исполняется. Без исполнения переменная проверяющим не является, и
// молчание гейта означало бы не то, что доказывается.
const injRunnerPreamble = `#!/usr/bin/env bash
set -euo pipefail
RC=0
GATE="${GATE:-true}"
GATE_SCRIPT="$REPO_ROOT/services/iam/tests/newman/scripts/assert-suites-green.sh"
`

const injRunnerTail = `
echo "gated"
bash "$GATE_SCRIPT"
exit $?
`

// injScan — разбор одной фикстуры тем же телом, что исполняется на дереве.
func injGateScan(t *testing.T, body string) ([]VerdictGateFinding, []string, int, int) {
	t.Helper()
	f, checkers, _, conds, skipped := scanVerdictGateRunner("deploy/scripts/inj.sh", body)
	return f, checkers, conds, skipped
}

func injKinds(fs []VerdictGateFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Kind)
	}
	return out
}

// ── ВЕРНУТЬ ДЕФЕКТ: форма ДО починки, дословно ──────────────────────────────

// Условие взято дословно из newman-parallel.sh на release/kaname-tail до правки.
// Оно нарушает ОБЕ оси сразу, и именно так и должно: два состояния в одном
// условии, а ветвь выходит сырым счётом.
func TestVerdictGateGate_RedsOnThePreFixRunnerVerbatim(t *testing.T) {
	t.Parallel()
	body := injRunnerPreamble + `
if [ "$GATE" != "true" ] || [ ! -f "$GATE_SCRIPT" ]; then
  echo "[parallel] CI gate skipped (GATE=$GATE, script=$GATE_SCRIPT) — grading on the per-suite roll-up"
  exit "$RC"
fi
` + injRunnerTail

	found, checkers, conds, skipped := injGateScan(t, body)
	if len(checkers) != 1 || checkers[0] != "GATE_SCRIPT" {
		t.Fatalf("проверяющий не распознан: %v — тогда молчание ниже означало бы "+
			"сломанный распознаватель, а не чистоту", checkers)
	}
	if conds != 1 || skipped != 0 {
		t.Fatalf("условий осмотрено %d, отведено %d — ожидалось 1 и 0", conds, skipped)
	}
	kinds := injKinds(found)
	if len(kinds) != 2 {
		t.Fatalf("дофиксовая форма дала %d находок вместо двух: %v", len(kinds), kinds)
	}
	for _, want := range []string{"два-состояния-в-одном-условии", "отсутствие-выдано-за-вердикт"} {
		if !containsString(kinds, want) {
			t.Fatalf("ось %q не сработала: %v", want, kinds)
		}
	}
	// Находка обязана называть КООРДИНАТУ и переменную: без них читатель ищет
	// не там, и гейт снимают как непонятный.
	for _, f := range found {
		if f.Line == 0 || f.Var != "GATE_SCRIPT" || !strings.Contains(f.File, "inj.sh") {
			t.Fatalf("находка без координаты: %+v", f)
		}
	}
}

// ── ЗАКОННЫЙ БЛИЗНЕЦ: форма ПОСЛЕ починки, дословно ─────────────────────────

// Тот же прогонщик, отличающийся ровно тем, ради чего правка: два состояния
// разведены по разным условиям, отсутствие даёт ненулевую КОНСТАНТУ.
func TestVerdictGateGate_SilentOnThePostFixRunnerVerbatim(t *testing.T) {
	t.Parallel()
	body := injRunnerPreamble + `
if [ "$GATE" != "true" ]; then
  echo "[parallel] CI gate switched OFF by the knob (GATE=$GATE; script=$GATE_SCRIPT)"
  exit "$RC"
fi
if [ ! -f "$GATE_SCRIPT" ]; then
  echo "[parallel] DID NOT RUN: the verdict gate is not at $GATE_SCRIPT." >&2
  exit 2
fi
` + injRunnerTail

	found, checkers, conds, skipped := injGateScan(t, body)
	if len(checkers) != 1 {
		t.Fatalf("проверяющий не распознан: %v", checkers)
	}
	if conds != 1 || skipped != 0 {
		t.Fatalf("условий осмотрено %d, отведено %d — близнец обязан судиться, "+
			"иначе его молчание ничего не доказывает", conds, skipped)
	}
	if len(found) != 0 {
		t.Fatalf("починенная форма объявлена находкой: %v", found)
	}
}

// ── ось ПЕРВАЯ порознь: смешение состояний без выхода сырым счётом ──────────

func TestVerdictGateGate_MixedConditionAloneIsAFinding(t *testing.T) {
	t.Parallel()
	body := injRunnerPreamble + `
if [ "$GATE" = "true" ] && [ -f "$GATE_SCRIPT" ]; then
  bash "$GATE_SCRIPT"
  exit $?
fi
exit 0
` + injRunnerTail

	found, _, _, _ := injGateScan(t, body)
	kinds := injKinds(found)
	if len(kinds) != 1 || kinds[0] != "два-состояния-в-одном-условии" {
		t.Fatalf("смешение состояний без сырого выхода не поймано порознь: %v", kinds)
	}
}

// БЛИЗНЕЦ оси первой: ДВЕ проверки ОДНОГО предмета — не смешение.
// Без этой пробы ось краснела бы на `[ -f "$X" ] && [ -x "$X" ]`, то есть на
// верной работе, — и её отключили бы первой.
func TestVerdictGateGate_TwoTestsOfTheSameSubjectAreNotAMix(t *testing.T) {
	t.Parallel()
	body := injRunnerPreamble + `
if [ ! -f "$GATE_SCRIPT" ] || [ ! -r "$GATE_SCRIPT" ]; then
  echo "no gate at $GATE_SCRIPT" >&2
  exit 2
fi
` + injRunnerTail

	found, _, conds, _ := injGateScan(t, body)
	if conds != 1 {
		t.Fatalf("условие не осмотрено вовсе: %d", conds)
	}
	if len(found) != 0 {
		t.Fatalf("две проверки одного предмета объявлены смешением: %v", found)
	}
}

// ── ось ВТОРАЯ порознь: выход сырым счётом без смешения ─────────────────────

func TestVerdictGateGate_RawExitAloneIsAFinding(t *testing.T) {
	t.Parallel()
	body := injRunnerPreamble + `
if [ ! -f "$GATE_SCRIPT" ]; then
  echo "gate missing"
  exit "$RC"
fi
` + injRunnerTail

	found, _, _, _ := injGateScan(t, body)
	kinds := injKinds(found)
	if len(kinds) != 1 || kinds[0] != "отсутствие-выдано-за-вердикт" {
		t.Fatalf("выход сырым счётом не пойман порознь: %v", kinds)
	}
}

// БЛИЗНЕЦ оси второй: ВЕТВЬ ПРИСУТСТВИЯ вправе выходить кодом переменной —
// гейт там исполнился, и его код и есть вердикт.
func TestVerdictGateGate_ExitByVariableInThePresenceBranchIsLegal(t *testing.T) {
	t.Parallel()
	body := injRunnerPreamble + `
if [ -f "$GATE_SCRIPT" ]; then
  bash "$GATE_SCRIPT"
  GATE_RC=$?
  exit "$GATE_RC"
fi
exit 2
` + injRunnerTail

	found, _, _, _ := injGateScan(t, body)
	if len(found) != 0 {
		t.Fatalf("выход кодом ИСПОЛНИВШЕГОСЯ гейта объявлен находкой: %v", found)
	}
}

// ── ЧЕТВЁРТЫЙ ПРИЗНАК: прогонщик ВОЛНЫ отводится, и это считается ────────────

// Форма взята дословно из newman-parallel.sh (волна fail-closed). Она нарушила
// бы ось первую, если бы судилась, — и не судится, потому что ветвь прогон не
// завершает: волна не идёт, отчётов не оставляет, и вердиктный гейт ниже
// краснеет по её коллекциям сам. Обе ложные находки первого прогона были ровно
// этой формы.
func TestVerdictGateGate_WaveRunnerIsSetAsideAndCounted(t *testing.T) {
	t.Parallel()
	body := injRunnerPreamble + `
FAILCLOSED_WAVE="${FAILCLOSED_WAVE:-true}"
FAILCLOSED_SH="$REPO_ROOT/services/iam/tests/newman/scripts/run-failclosed.sh"
if [ "$FAILCLOSED_WAVE" = "true" ] && [ -f "$FAILCLOSED_SH" ]; then
  if ( bash "$FAILCLOSED_SH" ); then
    echo "GREEN"
  else
    RC=1
  fi
fi
` + injRunnerTail

	found, checkers, conds, skipped := injGateScan(t, body)
	if !containsString(checkers, "FAILCLOSED_SH") {
		t.Fatalf("прогонщик волны не попал в проверяющие: %v — тогда его отведение "+
			"ничего не доказывает", checkers)
	}
	if skipped != 1 {
		t.Fatalf("прогонщик волны не отведён четвёртым признаком: отведено %d из %d условий",
			skipped, conds)
	}
	for _, f := range found {
		if f.Var == "FAILCLOSED_SH" {
			t.Fatalf("ложная находка на прогонщике волны вернулась: %v", f)
		}
	}
}

// ── предпосылки: три РАЗНЫХ нуля — три отказа ───────────────────────────────

func TestVerdictGateGate_EachEmptyInputIsARefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		census VerdictGateCensus
		want   string
	}{
		{"рецептов ноль", VerdictGateCensus{}, "рецептов прогона"},
		{"переменных ноль", VerdictGateCensus{Files: 3}, "путём скрипта"},
		{"проверяющих ноль", VerdictGateCensus{Files: 3, ScriptVars: 2}, "ни один рецепт не проверяет"},
	} {
		faults := judgeVerdictGateRunners(nil, tc.census)
		if len(faults) != 1 || !strings.Contains(faults[0], tc.want) {
			t.Fatalf("%s: прошло как чистота либо смешалось с соседним отказом: %v", tc.name, faults)
		}
	}

	// Живая популяция — молчание: без этого отрицания выше зеленели бы на
	// суждении, которое отказывает всегда.
	live := VerdictGateCensus{Files: 30, ScriptVars: 6, Checkers: 4, Conditions: 4}
	if faults := judgeVerdictGateRunners(nil, live); len(faults) != 0 {
		t.Fatalf("живая перепись объявлена отказом: %v", faults)
	}
}

// ── распознаватель не краснеет на СОБСТВЕННОМ объяснении ────────────────────

func TestVerdictGateGate_ProseIsNotCode(t *testing.T) {
	t.Parallel()
	body := injRunnerPreamble + `
# Здесь НЕЛЬЗЯ писать так:
#   if [ "$GATE" != "true" ] || [ ! -f "$GATE_SCRIPT" ]; then exit "$RC"; fi
# потому что два состояния сливаются в одну ветвь.
if [ ! -f "$GATE_SCRIPT" ]; then
  echo "no gate at $GATE_SCRIPT" >&2
  exit 2
fi
` + injRunnerTail

	found, _, _, _ := injGateScan(t, body)
	if len(found) != 0 {
		t.Fatalf("гейт краснеет на собственном объяснении: %v", found)
	}
}
