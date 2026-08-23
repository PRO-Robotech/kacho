// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что пробы ожидания вердикта СПОСОБНЫ упасть — и падают на
// существе, а не на форме записи.
//
// Инъекция идёт в обе стороны на ОДНОМ И ТОМ ЖЕ харнессе:
//
//	прежняя форма (цикл в блоке `run:` под `bash -e`) → ОДИН заход и исход 3;
//	нынешняя форма (скрипт, коды возврата как данные) → три захода и исход 0.
//
// Без первой половины пробы выше доказывали бы лишь то, что нынешний скрипт
// работает, — и остались бы зелёными, вернись прежняя форма обратно в YAML.
//
// Прежняя форма воспроизведена ДОСЛОВНО: это тот же цикл, что стоял в
// `.github/workflows/required-verdict.yml` до #1073, с теми же `set -uo pipefail`
// и той же `case`-развилкой. Отличие ровно одно — то, ради чего инъекция и
// ставится.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyInlineLoop — цикл ожидания в том виде, в каком он жил блоком `run:`.
// Провайдер исполняет такой блок через `bash -e`, и `set -uo pipefail` внутри
// НЕ снимает `-e`: ненулевой код решателя обрывает шелл до `rc=$?`.
const legacyInlineLoop = `set -uo pipefail

for i in $(seq 1 "$VERDICT_ATTEMPTS"); do
  if ! "$VERDICT_FETCH_CMD" > runs.json 2> api.err; then
    echo "::warning::опрос не удался (заход $i)"
    sleep "$VERDICT_INTERVAL"; continue
  fi

  "$VERDICT_DECIDE_CMD" < runs.json
  rc=$?
  case "$rc" in
    0) exit 0 ;;
    1) exit 1 ;;
    3) sleep "$VERDICT_INTERVAL" ;;
    *) echo "::error::вердикт не вынесен (код $rc)"; exit 1 ;;
  esac
done

echo "::error::проверки не завершились за отведённое время"
exit 1
`

// TestLegacyInlineLoopExhibitsTheDefect — прежняя форма не переживает кода «ещё
// идут»: один заход и исход 3.
//
// Исход 3 сам по себе — улика: блок не выходит этим кодом НИ В ОДНОЙ своей ветке
// (только 0 и 1), значит `case` не был достигнут.
func TestLegacyInlineLoopExhibitsTheDefect(t *testing.T) {
	s := newVerdictStubs(t, wholePayload, 3, 3, 0) // на третьем заходе было бы зелено

	legacy := filepath.Join(s.dir, "legacy-inline.sh")
	if err := os.WriteFile(legacy, []byte(legacyInlineLoop), 0o700); err != nil {
		t.Fatalf("не записана прежняя форма: %v", err)
	}

	code, out := runScriptUnderProviderShell(t, legacy, s, 10)

	if code != 3 {
		t.Errorf("исход %d, ожидался 3 — воспроизведение дефекта #1073 не удалось, "+
			"и тогда пробы ожидания ничего не доказывают\n%s", code, out)
	}
	if got := s.polls(t); got != 1 {
		t.Errorf("заходов %d, ожидался 1 — прежняя форма обязана падать на ПЕРВОМ "+
			"коде «ещё идут», не дойдя до sleep", got)
	}
}

// TestCurrentScriptSurvivesWhereTheLegacyFormDied — законный близнец на том же
// харнессе и том же входе: отличается только форма, и она решает исход.
func TestCurrentScriptSurvivesWhereTheLegacyFormDied(t *testing.T) {
	s := newVerdictStubs(t, wholePayload, 3, 3, 0)

	script := filepath.Join(repoRoot(t), ".github", "scripts", "pr-verdict-wait.sh")
	code, out := runScriptUnderProviderShell(t, script, s, 10)

	if code != 0 {
		t.Errorf("исход %d, ожидался 0 на том же входе, на котором прежняя форма "+
			"давала 3\n%s", code, out)
	}
	if got := s.polls(t); got != 3 {
		t.Errorf("заходов %d, ожидалось 3", got)
	}
}

// TestBothFormsWereRunTheSameWay — обе половины инъекции прогнаны ОДНИМ способом.
//
// Если бы прежнюю форму запускали без `-e`, а нынешнюю с ним, сравнение измеряло
// бы способ запуска, а не форму записи.
func TestBothFormsWereRunTheSameWay(t *testing.T) {
	if !strings.Contains(providerShellFlags, "-e") {
		t.Fatalf("харнесс запускает не так, как провайдер (%q): предпосылка инъекции "+
			"ложна, и её вердикт ничего не значит", providerShellFlags)
	}
}
