// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanshardrun_test.go — развилка по коду возврата прогонщика шарда
// ИСПОЛНЯЕТСЯ, а не только объявлена.
//
// ПРЕДМЕТ. У прогонщика суит три исхода: 0 — исполнились и зелены, 1 —
// исполнились, часть красная, 2 — прогон НЕДЕЙСТВИТЕЛЕН (вердикта нет ни
// красного, ни зелёного). Третья категория не вычитается из вердикта и не
// засчитывается за успех, и единственное место, где она отличается от красного
// для человека, — аннотация шага.
//
// ПОЧЕМУ ЭТОГО НЕ ЛОВИЛА НИ ОДНА ПРОБА. Развилка жила телом `run:`, а его не
// исполняет никто, кроме провайдера. Провайдер же запускает `run:` через
// `bash -e`, и под `-e` ненулевой код обрывает оболочку НЕМЕДЛЕННО — до
// присваивания `rc=$?`, до `case`, до аннотации. Три следствия, все тихие:
// аннотация «Прогон недействителен» не печаталась никогда; код 1 не
// проглатывался, хотя комментарий того же шага объявлял обратное; остановка
// наблюдателя стояла после обрывающей команды и тоже не исполнялась (#2346).
//
// ЧТО ЗДЕСЬ ДОКАЗЫВАЕТСЯ. Не текст скрипта, а его ПОВЕДЕНИЕ: проба запускает
// настоящий `newman-shard-run.sh` под флагами провайдера, подставляя ему
// прогонщик и наблюдателя. Проверка «в скрипте написан case» была бы той же
// формой без содержания — в прежнем блоке `case` тоже был написан.
package repohygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// shardStubs — подставные прогонщик и наблюдатель.
//
// Наблюдатель ждёт файл останова в цикле, как настоящий, и оставляет отметку,
// когда дождался: без неё «наблюдатель остановлен» пришлось бы проверять по
// тексту скрипта, то есть по объявлению вместо исхода.
type shardStubs struct {
	dir      string
	runCmd   string
	watchCmd string
	stopFile string
	ranFile  string
	seenStop string
}

func newShardStubs(t *testing.T, runnerCode int) shardStubs {
	t.Helper()
	dir := t.TempDir()
	s := shardStubs{
		dir:      dir,
		runCmd:   filepath.Join(dir, "run.sh"),
		watchCmd: filepath.Join(dir, "watch.sh"),
		stopFile: filepath.Join(dir, "stop"),
		ranFile:  filepath.Join(dir, "ran"),
		seenStop: filepath.Join(dir, "watcher-saw-stop"),
	}

	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatalf("не записан дублёр %s: %v", path, err)
		}
	}

	write(s.runCmd, "#!/usr/bin/env bash\n"+
		"echo x >> "+s.ranFile+"\n"+
		"echo '[дублёр прогонщика] отработал'\n"+
		"exit "+strconv.Itoa(runnerCode)+"\n")

	// Границы у дублёра ДВЕ, и вторая — не украшение.
	//
	// Файл останова — штатный выход. Но прежняя форма до `touch` не доходит
	// вовсе, и наблюдатель остаётся сиротой; без второй границы каждая проба
	// инъекции ждала бы полного бюджета впустую (замерено: 32с на пакет).
	// Поэтому дублёр выходит и тогда, когда исчез его родитель, — то есть в
	// точности на том исходе, который проба и утверждает.
	write(s.watchCmd, "#!/usr/bin/env bash\n"+
		// Дублёр отпускает УНАСЛЕДОВАННЫЙ канал вывода. Держи он его, осиротев,
		// — вызывающий ждал бы закрытия трубы, а не завершения скрипта, и проба
		// мерила бы бюджет дублёра вместо исхода шага (замерено: 38с там, где
		// шаг завершается мгновенно).
		"exec >"+filepath.Join(dir, "watch.log")+" 2>&1\n"+
		"ppid0=$PPID\n"+
		"for _ in $(seq 1 200); do\n"+
		"  if [ -e "+s.stopFile+" ]; then echo x > "+s.seenStop+"; exit 0; fi\n"+
		"  kill -0 \"$ppid0\" 2>/dev/null || exit 0\n"+
		"  sleep 0.05\n"+
		"done\n"+
		"exit 0\n")

	return s
}

func (s shardStubs) runnerCalls(t *testing.T) int {
	t.Helper()
	body, err := os.ReadFile(s.ranFile)
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(body)))
}

func (s shardStubs) watcherSawStop() bool {
	_, err := os.Stat(s.seenStop)
	return err == nil
}

// runShardScript — прогон настоящего скрипта под флагами провайдера.
//
// `providerShellFlags` берётся у соседней пробы (`prverdictwait_test.go`) НАРОЧНО:
// «как исполняет провайдер» обязано быть объявлено в дереве один раз. Разойдись
// эти два места, одна из проб измеряла бы не то, и заметить это было бы нечем.
func runShardScript(t *testing.T, s shardStubs) (code int, output string) {
	t.Helper()
	script := filepath.Join(repoRoot(t), ".github", "scripts", "newman-shard-run.sh")
	return runShardScriptFile(t, script, s)
}

func runShardScriptFile(t *testing.T, script string, s shardStubs) (code int, output string) {
	t.Helper()

	cmd := exec.Command("bash", providerShellFlags, script)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(),
		"SERVICES=vpc",
		"NEWMAN_LIVE_STOP_FILE="+s.stopFile,
		"NEWMAN_LIVE_INTERVAL=0",
		"SHARD_RUN_CMD="+s.runCmd,
		"SHARD_WATCH_CMD="+s.watchCmd,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			return ee.ExitCode(), string(out)
		}
		t.Fatalf("скрипт не запустился: %v\n%s", err, out)
	}
	return 0, string(out)
}

const invalidRunAnnotation = "::error title=Прогон недействителен::"

// TestShardRunIsGreenWhenTheRunnerIs — исход 0: шаг зелёный, аннотации нет.
//
// Положительный контроль. Без него утверждения об отсутствии аннотации ниже
// зеленели бы и на скрипте, который не печатает НИЧЕГО и никогда не падает.
func TestShardRunIsGreenWhenTheRunnerIs(t *testing.T) {
	t.Parallel()
	s := newShardStubs(t, 0)

	code, out := runShardScript(t, s)

	if code != 0 {
		t.Errorf("исход %d, ожидался 0\n%s", code, out)
	}
	if strings.Contains(out, invalidRunAnnotation) {
		t.Errorf("на зелёном прогоне напечатана аннотация третьей категории:\n%s", out)
	}
	if got := s.runnerCalls(t); got != 1 {
		t.Errorf("прогонщик вызван %d раз, ожидался 1", got)
	}
}

// TestShardRunSwallowsRedSuitesForTheGatesBelow — исход 1: суиты исполнились,
// часть красная. Шаг НЕ падает, аннотации третьей категории нет.
//
// Это регрессия на половину дефекта, которую прежняя форма ломала МОЛЧА: код 1
// обрывал оболочку, шаг падал, и вердикт выносился не там, где объявлен. Гейты
// суит стоят ниже под `always()` — они и есть держатель этого вердикта.
func TestShardRunSwallowsRedSuitesForTheGatesBelow(t *testing.T) {
	t.Parallel()
	s := newShardStubs(t, 1)

	code, out := runShardScript(t, s)

	if code != 0 {
		t.Errorf("исход %d, ожидался 0: красная суита обязана оставить вердикт "+
			"шагам-гейтам ниже, а не валить шаг прогона\n%s", code, out)
	}
	if strings.Contains(out, invalidRunAnnotation) {
		t.Errorf("красное объявлено недействительным — это разные категории:\n%s", out)
	}
	if !strings.Contains(out, "шаги-гейты ниже") {
		t.Errorf("шаг не сказал, ГДЕ выносится вердикт по красным суитам:\n%s", out)
	}
}

// TestShardRunAnnotatesAnInvalidRun — исход 2: прогон недействителен.
//
// Внесённый факт против близнеца выше ровно один — код возврата прогонщика.
func TestShardRunAnnotatesAnInvalidRun(t *testing.T) {
	t.Parallel()
	s := newShardStubs(t, 2)

	code, out := runShardScript(t, s)

	if code != 2 {
		t.Errorf("исход %d, ожидался 2\n%s", code, out)
	}
	if !strings.Contains(out, invalidRunAnnotation) {
		t.Errorf("третья категория не отличима от красного: аннотации нет\n%s", out)
	}
}

// TestShardRunTreatsAnUnknownCodeAsNotRun — код вне словаря прогонщика (0/1/2)
// означает, что он не дошёл до собственного вердикта: снят по времени, убит по
// памяти, сигнал.
//
// Без ветви `*` такой код проходил бы мимо развилки и выглядел бы обычным
// красным — то есть «не знаю» выдавалось бы за «нет».
func TestShardRunTreatsAnUnknownCodeAsNotRun(t *testing.T) {
	t.Parallel()
	s := newShardStubs(t, 137) // канонический код убитого по памяти

	code, out := runShardScript(t, s)

	if code != 2 {
		t.Errorf("исход %d, ожидался 2 — неизвестный код прогонщика есть «не выполнилось»\n%s", code, out)
	}
	if !strings.Contains(out, invalidRunAnnotation) {
		t.Errorf("неизвестный код не объявлен третьей категорией:\n%s", out)
	}
	if !strings.Contains(out, "137") {
		t.Errorf("аннотация не называет кода, который на самом деле пришёл:\n%s", out)
	}
}

// TestShardRunStopsTheWatcherOnEveryOutcome — наблюдатель останавливается на
// ВСЕХ трёх исходах, а не только на зелёном.
//
// В прежней форме остановка стояла ПОСЛЕ обрывающей команды, поэтому на любом
// ненулевом коде она не исполнялась вовсе. Утверждается исход (наблюдатель
// дождался файла останова), а не наличие строки `touch` в тексте скрипта.
func TestShardRunStopsTheWatcherOnEveryOutcome(t *testing.T) {
	t.Parallel()
	for _, code := range []int{0, 1, 2, 137} {
		t.Run("код_"+strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()
			s := newShardStubs(t, code)

			_, out := runShardScript(t, s)

			if !s.watcherSawStop() {
				t.Errorf("наблюдатель не дождался файла останова при коде %d — "+
					"остановка не исполнилась\n%s", code, out)
			}
		})
	}
}

// TestShardRunIsTheStepTheWorkflowRuns — скрипт, который проверяют пробы выше,
// и есть тот, который зовёт шаг шарда.
//
// Без этого утверждения пробы доказывали бы свойство файла, которого конвейер не
// исполняет: прежняя развилка жила в YAML, и именно поэтому её не проверял никто.
func TestShardRunIsTheStepTheWorkflowRuns(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "e2e-newman.yml"))
	if err != nil {
		t.Fatalf("не прочитан процесс e2e-newman: %v", err)
	}
	body := string(raw)

	if !strings.Contains(body, "newman-shard-run.sh") {
		t.Errorf("процесс не зовёт .github/scripts/newman-shard-run.sh — пробы выше " +
			"проверяют скрипт, которого конвейер не исполняет")
	}
	// Обратная сторона: развилка не должна вернуться в YAML. Там её исполняет
	// `bash -e`, где код возврата прогонщика — отказ, а не данные.
	if strings.Contains(body, "./scripts/newman-parallel.sh;") {
		t.Errorf("прогонщик снова зовётся из тела `run:` с чтением `$?` — под " +
			"`bash -e` развилка по коду не исполнится (#2346)")
	}
}
