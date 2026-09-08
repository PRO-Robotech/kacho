// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что пробы прогона шарда СПОСОБНЫ упасть — и падают на
// существе, а не на форме записи.
//
// Инъекция идёт в обе стороны на ОДНОМ И ТОМ ЖЕ харнессе и ОДНОМ И ТОМ ЖЕ входе:
//
//	прежняя форма (развилка в теле `run:` под `bash -e`) → аннотации НЕТ,
//	                                                       наблюдатель не остановлен,
//	                                                       красная суита валит шаг;
//	нынешняя форма (скрипт, код возврата как данные)     → аннотация есть,
//	                                                       наблюдатель остановлен,
//	                                                       красная суита оставлена гейтам.
//
// Без первой половины пробы соседнего файла доказывали бы лишь то, что нынешний
// скрипт работает, — и остались бы зелёными, вернись прежняя форма обратно в YAML.
//
// Прежняя форма воспроизведена ДОСЛОВНО по структуре: те же операторы в том же
// порядке, что стояли в `.github/workflows/e2e-newman.yml` до #2346. Отличие ровно
// одно — способ взятия кода возврата, то есть ровно то, ради чего инъекция и
// ставится. Прогонщик и наблюдатель подставлены в ОБЕИХ половинах одинаково:
// иначе сравнение измеряло бы дублёров, а не форму.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyInlineShardRun — развилка прогона шарда в том виде, в каком она жила
// телом `run:`. Провайдер исполняет такой блок через `bash -e`, и `-e` обрывает
// оболочку на ненулевом коде прогонщика — до `rc=$?`, до `touch`, до `if`.
const legacyInlineShardRun = `rm -f "$NEWMAN_LIVE_STOP_FILE"
"$SHARD_WATCH_CMD" &
live_pid=$!
"$SHARD_RUN_CMD"; rc=$?
touch "$NEWMAN_LIVE_STOP_FILE"
wait "$live_pid" || true

if [ "$rc" -eq 2 ]; then
  echo "::error title=Прогон недействителен::rc=2 — результата нет, ни красного, ни зелёного."
  exit 2
fi
`

func writeLegacyShardRun(t *testing.T, s shardStubs) string {
	t.Helper()
	path := filepath.Join(s.dir, "legacy-inline.sh")
	if err := os.WriteFile(path, []byte(legacyInlineShardRun), 0o700); err != nil {
		t.Fatalf("не записана прежняя форма: %v", err)
	}
	return path
}

// TestLegacyInlineShardRunNeverPrintsTheAnnotation — прежняя форма НЕ печатает
// аннотацию третьей категории, хотя объявляет её.
//
// Улика сама по себе: шаг выходит кодом 2 — тем самым, которым он выходит и в
// своей единственной ветви, — но ветвь при этом не достигнута. Различить два
// случая по коду возврата нельзя, и именно поэтому дефект прожил незамеченным.
func TestLegacyInlineShardRunNeverPrintsTheAnnotation(t *testing.T) {
	t.Parallel()
	s := newShardStubs(t, 2)

	code, out := runShardScriptFile(t, writeLegacyShardRun(t, s), s)

	if code != 2 {
		t.Fatalf("исход %d, ожидался 2 — воспроизведение дефекта #2346 не удалось, "+
			"и тогда пробы прогона шарда ничего не доказывают\n%s", code, out)
	}
	if strings.Contains(out, invalidRunAnnotation) {
		t.Errorf("прежняя форма напечатала аннотацию — значит `-e` до развилки не "+
			"обрывает, и предпосылка починки ложна\n%s", out)
	}
	if s.watcherSawStop() {
		t.Errorf("прежняя форма остановила наблюдателя — значит оболочка дошла до " +
			"`touch`, и воспроизведение дефекта не удалось")
	}
}

// TestLegacyInlineShardRunFailsTheStepOnRedSuites — вторая половина дефекта:
// код 1 не проглатывался, хотя комментарий того же шага объявлял обратное.
func TestLegacyInlineShardRunFailsTheStepOnRedSuites(t *testing.T) {
	t.Parallel()
	s := newShardStubs(t, 1)

	code, out := runShardScriptFile(t, writeLegacyShardRun(t, s), s)

	if code != 1 {
		t.Errorf("исход %d, ожидался 1 — прежняя форма обязана ронять шаг прогона "+
			"на красной суите, отбирая вердикт у шагов-гейтов\n%s", code, out)
	}
}

// TestCurrentShardRunSurvivesWhereTheLegacyFormDied — законный близнец на том же
// харнессе и тех же входах: отличается только форма, и она решает исход.
func TestCurrentShardRunSurvivesWhereTheLegacyFormDied(t *testing.T) {
	t.Parallel()

	t.Run("код_2_даёт_аннотацию_и_останавливает_наблюдателя", func(t *testing.T) {
		t.Parallel()
		s := newShardStubs(t, 2)

		code, out := runShardScript(t, s)

		if code != 2 {
			t.Errorf("исход %d, ожидался 2\n%s", code, out)
		}
		if !strings.Contains(out, invalidRunAnnotation) {
			t.Errorf("аннотации нет там, где прежняя форма её тоже не печатала — "+
				"починка не состоялась\n%s", out)
		}
		if !s.watcherSawStop() {
			t.Error("наблюдатель не остановлен на том же входе, на котором прежняя " +
				"форма его тоже не останавливала")
		}
	})

	t.Run("код_1_оставляет_вердикт_гейтам", func(t *testing.T) {
		t.Parallel()
		s := newShardStubs(t, 1)

		code, out := runShardScript(t, s)

		if code != 0 {
			t.Errorf("исход %d, ожидался 0 на том же входе, на котором прежняя форма "+
				"давала 1\n%s", code, out)
		}
	})
}

// TestBothShardFormsWereRunTheSameWay — обе половины инъекции прогнаны ОДНИМ
// способом.
//
// Запусти мы прежнюю форму без `-e`, а нынешнюю с ним — сравнение измеряло бы
// способ запуска, а не форму записи.
func TestBothShardFormsWereRunTheSameWay(t *testing.T) {
	t.Parallel()
	if !strings.Contains(providerShellFlags, "-e") {
		t.Fatalf("харнесс запускает не так, как провайдер (%q): предпосылка инъекции "+
			"ложна, и её вердикт ничего не значит", providerShellFlags)
	}
}
