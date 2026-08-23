// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// prverdictwait_test.go — ожидание набора проверок ИСПОЛНЯЕТСЯ, а не только
// объявлено: «проверки ещё идут» обязано вести ко второму заходу, а не к отказу.
//
// ПРЕДМЕТ. У сводного вердикта три исхода, и третий — не вердикт: пока часть
// проверок идёт, сказать нечего, надо подождать и спросить снова. Задание
// стартует БЕЗ ЗАВИСИМОСТЕЙ (в этом его замысел), поэтому в момент первого опроса
// незавершённые проверки есть ВСЕГДА. Значит третий исход, прочитанный как отказ,
// красит вердикт на КАЖДОМ запросе слияния — и красит его тем вернее, чем лучше
// работает всё остальное.
//
// ПОЧЕМУ ЭТОГО НЕ ЛОВИЛА НИ ОДНА ПРОБА. `pr_verdict_test.py` проверяет `decide()`
// — чистую функцию, и она ВЕРНА: «часть идёт» она возвращает как отдельное
// состояние, и проба на это есть. Дефект жил в шелл-обёртке, которую не исполнял
// никто: провайдер запускает блок `run:` через `bash -e`, а под `-e` ненулевой код
// возврата обрывает шелл НЕМЕДЛЕННО — до `rc=$?`, до `case`, до `sleep`. Цикл не
// делал второго захода никогда. Наблюдаемое: задание падало за 6 секунд при
// объявленном пределе 90 минут, и падало кодом 3 — кодом, которым сам блок не
// выходит ни в одной своей ветке (#1073).
//
// ЧТО ЗДЕСЬ ДОКАЗЫВАЕТСЯ. Не текст скрипта, а его ПОВЕДЕНИЕ: проба запускает
// настоящий `pr-verdict-wait.sh`, подставляя ему получателя набора и решателя.
// Проверка «в скрипте написано sleep» была бы той же формой без содержания — в
// прежнем блоке `sleep` тоже был написан.
package repohygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// verdictStubs — подставные получатель набора и решатель. Решатель отдаёт коды из
// последовательности по одному на заход и ведёт счёт заходов в файле, поэтому
// проба может утверждать, СКОЛЬКО раз её спросили.
type verdictStubs struct {
	dir       string
	fetchCmd  string
	decideCmd string
	tallyFile string
}

func newVerdictStubs(t *testing.T, payload string, codes ...int) verdictStubs {
	t.Helper()
	dir := t.TempDir()
	s := verdictStubs{
		dir:       dir,
		fetchCmd:  filepath.Join(dir, "fetch.sh"),
		decideCmd: filepath.Join(dir, "decide.sh"),
		tallyFile: filepath.Join(dir, "tally"),
	}

	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatalf("не записан дублёр %s: %v", path, err)
		}
	}

	write(s.fetchCmd, "#!/usr/bin/env bash\ncat <<'PAYLOAD'\n"+payload+"\nPAYLOAD\n")

	// Коды перечислены строкой; заход N берёт N-й, последний повторяется дальше.
	var list []string
	for _, c := range codes {
		list = append(list, strconv.Itoa(c))
	}
	write(s.decideCmd, "#!/usr/bin/env bash\n"+
		"cat > /dev/null\n"+ // решатель обязан прочесть набор со стандартного входа
		"codes=("+strings.Join(list, " ")+")\n"+
		"echo x >> "+s.tallyFile+"\n"+
		"n=$(wc -l < "+s.tallyFile+")\n"+
		"i=$((n-1)); last=$(( ${#codes[@]} - 1 ))\n"+
		"[ $i -gt $last ] && i=$last\n"+
		"exit ${codes[$i]}\n")

	return s
}

func (s verdictStubs) polls(t *testing.T) int {
	t.Helper()
	body, err := os.ReadFile(s.tallyFile)
	if err != nil {
		return 0 // решателя не звали ни разу
	}
	return len(strings.Fields(string(body)))
}

const wholePayload = `{"total_count": 2, "check_runs": [{"name":"a"},{"name":"b"}]}`

// runWaitScript — прогон настоящего скрипта ожидания под `bash -e`, то есть ровно
// так, как его исполняет провайдер. Интервал сведён к нулю: предмет пробы —
// ЧИСЛО заходов, а не время между ними.
func runWaitScript(t *testing.T, s verdictStubs, attempts int) (code int, output string) {
	t.Helper()
	script := filepath.Join(repoRoot(t), ".github", "scripts", "pr-verdict-wait.sh")

	return runScriptUnderProviderShell(t, script, s, attempts)
}

// providerShellFlags — флаги, с которыми провайдер исполняет блок `run:`. Именно
// `-e` и превращает код возврата решателя в отказ, поэтому проба обязана
// запускать скрипт так же: под другими флагами она измеряла бы не то.
const providerShellFlags = "-e"

func runScriptUnderProviderShell(t *testing.T, script string, s verdictStubs, attempts int) (code int, output string) {
	t.Helper()

	cmd := exec.Command("bash", providerShellFlags, script)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(),
		"REPO=owner/repo",
		"SHA=deadbeef",
		"SELF=сводный вердикт (все проверки завершились и зелены)",
		"VERDICT_ATTEMPTS="+strconv.Itoa(attempts),
		"VERDICT_INTERVAL=0",
		"VERDICT_FETCH_CMD="+s.fetchCmd,
		"VERDICT_DECIDE_CMD="+s.decideCmd,
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

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// TestVerdictWaitPollsUntilTheChecksFinish — «ещё идут» ведёт к следующему заходу.
//
// Это и есть регрессия на #1073: под прежней формой (код возврата в блоке `run:`
// под `bash -e`) заходов было бы РОВНО ОДИН, а исход — 3.
func TestVerdictWaitPollsUntilTheChecksFinish(t *testing.T) {
	// Решатель говорит «ещё идут» дважды, на третий — «зелено».
	s := newVerdictStubs(t, wholePayload, 3, 3, 0)

	code, out := runWaitScript(t, s, 10)

	if code != 0 {
		t.Errorf("исход %d, ожидался 0: «ещё идут» — это состояние, а не отказ\n%s", code, out)
	}
	if got := s.polls(t); got != 3 {
		t.Errorf("заходов %d, ожидалось 3 — скрипт не ждёт, а выносит вердикт с первого опроса "+
			"(ровно дефект #1073: задание падало за 6 секунд при пределе 90 минут)", got)
	}
}

// TestVerdictWaitStopsAtTheFirstRed — красное решает сразу, ждать нечего.
func TestVerdictWaitStopsAtTheFirstRed(t *testing.T) {
	s := newVerdictStubs(t, wholePayload, 1)

	code, out := runWaitScript(t, s, 10)

	if code != 1 {
		t.Errorf("исход %d, ожидался 1 на красном\n%s", code, out)
	}
	if got := s.polls(t); got != 1 {
		t.Errorf("заходов %d, ожидался 1 — вердикт уже определён, повторный опрос его не изменит "+
			"и только держит ранер", got)
	}
}

// TestVerdictWaitNeverCallsUnfinishedGreen — заходы кончились, а проверки нет:
// это «вердикта НЕТ», а не «зелено».
func TestVerdictWaitNeverCallsUnfinishedGreen(t *testing.T) {
	s := newVerdictStubs(t, wholePayload, 3) // «ещё идут» всегда

	code, out := runWaitScript(t, s, 4)

	if code == 0 {
		t.Errorf("исход 0 — незавершённый набор выдан за зелёный\n%s", out)
	}
	if got := s.polls(t); got != 4 {
		t.Errorf("заходов %d, ожидалось 4 — предел заходов не соблюдён", got)
	}
	if !strings.Contains(out, "не завершились") {
		t.Errorf("отказ не называет причину — по такому тексту не отличить исчерпание от красноты:\n%s", out)
	}
}

// TestVerdictWaitRefusesATruncatedPage — страница вместо всего набора.
//
// `per_page=100` отдаёт первую сотню. Если проверок больше, «все зелены» было бы
// произнесено над подмножеством, которое никто целиком не читал, — то есть ложное
// зелёное в том самом месте, что стережёт ложное зелёное.
func TestVerdictWaitRefusesATruncatedPage(t *testing.T) {
	truncated := `{"total_count": 137, "check_runs": [{"name":"a"},{"name":"b"}]}`
	s := newVerdictStubs(t, truncated, 0) // решатель СКАЗАЛ БЫ «зелено»

	code, out := runWaitScript(t, s, 3)

	if code == 0 {
		t.Errorf("исход 0 — вердикт вынесен по усечённой странице\n%s", out)
	}
	if !strings.Contains(out, "НЕ ЦЕЛИКОМ") {
		t.Errorf("отказ не называет усечение:\n%s", out)
	}
	if got := s.polls(t); got != 0 {
		t.Errorf("решателя звали %d раз — усечение обязано отсекаться ДО вердикта", got)
	}
}

// TestVerdictWaitSurvivesAFailedPoll — сбой опроса не есть вердикт: сеть моргнула,
// спрашиваем снова.
func TestVerdictWaitSurvivesAFailedPoll(t *testing.T) {
	s := newVerdictStubs(t, wholePayload, 0)
	// Получатель падает на первом заходе и отвечает на втором.
	flag := filepath.Join(s.dir, "first-done")
	body := "#!/usr/bin/env bash\n" +
		"if [ ! -f " + flag + " ]; then touch " + flag + "; echo 'сеть моргнула' >&2; exit 1; fi\n" +
		"cat <<'PAYLOAD'\n" + wholePayload + "\nPAYLOAD\n"
	if err := os.WriteFile(s.fetchCmd, []byte(body), 0o700); err != nil {
		t.Fatalf("не записан дублёр: %v", err)
	}

	code, out := runWaitScript(t, s, 5)

	if code != 0 {
		t.Errorf("исход %d — сбой ОДНОГО опроса принят за вердикт\n%s", code, out)
	}
	if got := s.polls(t); got != 1 {
		t.Errorf("решателя звали %d раз, ожидался 1 (первый заход не дошёл до него)", got)
	}
}

// TestVerdictWaitRejectsAnUnknownCode — неизвестный код не «зелено».
func TestVerdictWaitRejectsAnUnknownCode(t *testing.T) {
	s := newVerdictStubs(t, wholePayload, 2) // 2 = вход не разобран

	code, out := runWaitScript(t, s, 3)

	if code == 0 {
		t.Errorf("исход 0 на неизвестном коде решателя\n%s", out)
	}
	if !strings.Contains(out, "вердикт не вынесен") {
		t.Errorf("отказ не называет предмет:\n%s", out)
	}
}

// TestVerdictWaitIsTheStepTheWorkflowRuns — скрипт, который проверяют пробы выше,
// и есть тот, который зовёт задание.
//
// Без этого утверждения пробы доказывали бы свойство файла, которого конвейер не
// исполняет: прежний цикл жил в YAML, и именно поэтому его не проверял никто.
func TestVerdictWaitIsTheStepTheWorkflowRuns(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "required-verdict.yml"))
	if err != nil {
		t.Fatalf("не прочитан процесс сводного вердикта: %v", err)
	}
	body := string(raw)

	if !strings.Contains(body, "pr-verdict-wait.sh") {
		t.Errorf("процесс не зовёт .github/scripts/pr-verdict-wait.sh — пробы выше проверяют " +
			"скрипт, которого конвейер не исполняет")
	}
	// Цикл опроса обязан жить в скрипте, а не вернуться в YAML: в блоке `run:`
	// его исполняет `bash -e`, где код возврата решателя — отказ, а не данные.
	if strings.Contains(body, "seq 1") {
		t.Errorf("в объявлении процесса снова заведён цикл опроса — под `bash -e` он не " +
			"переживёт кода «ещё идут» и упадёт на первом заходе (#1073)")
	}
}
