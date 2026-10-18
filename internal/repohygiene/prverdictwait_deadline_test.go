// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// prverdictwait_deadline_test.go — свод ждёт набор до СРОКА, который переживает
// измеренный набор, и не ждёт проверок вытесненного прогона (kacho#3299).
//
// ПРЕДМЕТ 1 — ПРЕДЕЛ. Ожидание кончалось через 170 заходов: заход — 20 с сна и
// ~1,2 с опроса, итого 60 минут, раньше `timeout-minutes: 90` задания и раньше
// конца набора. Замер 2026-10-18 по check-runs четырёх голов (от старта свода до
// последней завершённой проверки): 116,0 · 112,2 · 110,9 · 107,2 мин. Все четыре
// свода отказали «не завершились» на 60-й минуте над набором, кончившимся
// зелёным. Пары на одном харнессе и одном входе «набор кончается через 112 мин»:
//
//	прежний предел (170 заходов по 21 с)  → отказ «не завершились»;
//	нынешний предел (срок по часам)       → зелёный.
//
// И сверка чисел дерева: срок не ниже замера, задание живёт дольше срока — с
// инъекцией в обе стороны.
//
// ПРЕДМЕТ 2 — ВЫТЕСНЕНИЕ. Прогон процесса с общей группой `concurrency` без
// отмены провайдер отменяет, пока он ждёт, — заданий у него ноль, и его проверки
// не появятся. Пары:
//
//	все прогоны процесса на голове отменены без заданий → отказ, названа причина;
//	тот же прогон перезапущен (попытка ждёт)             → ждём и зеленеем;
//	рядом есть прогон того же процесса с исходом         → не вытеснен;
//	отменён, но задания заводил                          → не вытеснен (судит решатель);
//	перечень прогонов не прочитан                        → не вердикт, ждём дальше.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Замер, на который опирается срок (2026-10-18, `gh api
// repos/PRO-Robotech/kacho/commits/<sha>/check-runs`, головы 5a586e18,
// 50cf2727, 636bd543, af80b448): наибольший промежуток от старта свода до
// последней завершённой проверки — 116,0 мин.
const measuredSetSpanSeconds = 116 * 60

// Заход прежнего цикла стоил 20 с сна и ~1,2 с опроса: 170 заходов на голове
// 5a586e18 прошли с 17:27:10 до 18:27:15, то есть 21,2 с на заход.
const measuredPollSeconds = 21

// Вход пар предела: набор, который кончается зелёным через 112 мин.
const setEndsAtSeconds = 112 * 60

// Подготовка задания до шага ожидания (checkout, python, pyyaml) — замер 4–6 с;
// резерв в 10 мин между сроком скрипта и пределом задания.
const setupReserveSeconds = 10 * 60

// legacyAttemptCeiling — цикл ожидания в форме до kacho#3299: предел — ЧИСЛО
// заходов. Коды возврата в нём уже данные (после #1073), так что инъекция роняет
// ровно одно — форму предела.
const legacyAttemptCeiling = `set -uo pipefail
for i in $(seq 1 "$VERDICT_ATTEMPTS"); do
  if ! "$VERDICT_FETCH_CMD" > runs.json 2> api.err; then
    sleep "$VERDICT_INTERVAL"; continue
  fi
  "$VERDICT_DECIDE_CMD" < runs.json && rc=0 || rc=$?
  case "$rc" in
    0) exit 0 ;;
    1) exit 1 ;;
    3) sleep "$VERDICT_INTERVAL" ;;
    *) echo "::error::вердикт не вынесен (код $rc)"; exit 1 ;;
  esac
done
echo "::error::проверки не завершились за отведённое время ($VERDICT_ATTEMPTS заходов) — вердикта НЕТ, и это не «зелено»"
exit 1
`

// setEndingAt — подставной набор, который идёт до endsAt секунд и кончается
// зелёным. Время в нём двигает ОПРОС: каждый вызов решателя — measuredPollSeconds
// по часам, как у настоящего захода; часы скрипта эти часы только читают. Так
// обе формы цикла живут в одном времени, хоть прежняя часов и не спрашивает.
func setEndingAt(t *testing.T, endsAt int) (s verdictStubs, nowCmd string) {
	t.Helper()
	s = newVerdictStubs(t, wholePayload, 3)
	clock := filepath.Join(s.dir, "clock")
	writeExec(t, clock, "0\n")
	writeExec(t, s.decideCmd, "#!/usr/bin/env bash\n"+
		"cat > /dev/null\n"+
		"echo x >> "+s.tallyFile+"\n"+
		"t=$(cat "+clock+")\n"+
		"echo $(( t + "+strconv.Itoa(measuredPollSeconds)+" )) > "+clock+"\n"+
		"[ \"$t\" -ge "+strconv.Itoa(endsAt)+" ] && exit 0\n"+
		"exit 3\n")
	nowCmd = filepath.Join(s.dir, "now.sh")
	writeExec(t, nowCmd, "#!/usr/bin/env bash\ncat "+clock+"\n")
	return s, nowCmd
}

func emptyRunsCmd(t *testing.T, dir string) string {
	t.Helper()
	runs := filepath.Join(dir, "runs-empty.sh")
	writeExec(t, runs, "#!/usr/bin/env bash\necho '{\"total_count\": 0, \"workflow_runs\": []}'\n")
	return runs
}

func waitScriptPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), ".github", "scripts", "pr-verdict-wait.sh")
}

// TestAttemptCeilingDiesBeforeASetThatEndsAt112Minutes — инъекция: прежний
// предел (170 заходов) на наборе, кончающемся через 112 мин, отказывает раньше
// конца набора. Без этой половины пара ниже доказывала бы лишь, что нынешний
// скрипт умеет ждать.
func TestAttemptCeilingDiesBeforeASetThatEndsAt112Minutes(t *testing.T) {
	t.Parallel()
	s, _ := setEndingAt(t, setEndsAtSeconds)
	legacy := filepath.Join(s.dir, "legacy-ceiling.sh")
	writeExec(t, legacy, legacyAttemptCeiling)

	code, out := runUnderProviderShell(t, legacy, s.dir,
		"VERDICT_ATTEMPTS=170",
		"VERDICT_FETCH_CMD="+s.fetchCmd,
		"VERDICT_DECIDE_CMD="+s.decideCmd,
	)

	if code != 1 || !strings.Contains(out, "не завершились") {
		t.Errorf("исход %d, ожидался отказ «не завершились» — воспроизведение kacho#3299 "+
			"не удалось, и пара ничего не доказывает\n%s", code, out)
	}
	if got, want := s.polls(t), 170; got != want {
		t.Errorf("заходов %d, ожидалось %d", got, want)
	}
	if spent := s.polls(t) * measuredPollSeconds; spent >= setEndsAtSeconds {
		t.Errorf("прежний предел прожил %d с при наборе в %d с — вход пары не тот", spent, setEndsAtSeconds)
	}
}

// TestVerdictWaitOutlastsASetThatEndsAt112Minutes — законный близнец: настоящий
// скрипт с БОЕВЫМ сроком (ручка срока не подставлена) на том же входе дожидается
// конца набора и выносит зелёный.
func TestVerdictWaitOutlastsASetThatEndsAt112Minutes(t *testing.T) {
	t.Parallel()
	s, nowCmd := setEndingAt(t, setEndsAtSeconds)

	code, out := runUnderProviderShell(t, waitScriptPath(t), s.dir,
		"VERDICT_DEADLINE=", // пусто — значит боевое умолчание скрипта
		"VERDICT_NOW_CMD="+nowCmd,
		"VERDICT_RUNS_CMD="+emptyRunsCmd(t, s.dir),
		"VERDICT_FETCH_CMD="+s.fetchCmd,
		"VERDICT_DECIDE_CMD="+s.decideCmd,
	)

	if code != 0 {
		tail := out
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		t.Fatalf("исход %d, ожидался 0 — срок скрипта короче набора в 112 мин\n%s", code, tail)
	}
	if got, want := s.polls(t), setEndsAtSeconds/measuredPollSeconds+1; got != want {
		t.Errorf("заходов %d, ожидалось %d — зелёный вынесен не на конце набора", got, want)
	}
}

// deadlineFindings — сверка чисел предела: скрипт ждёт СРОКОМ, срок не ниже
// замера набора, задание свода живёт дольше срока на резерв подготовки.
func deadlineFindings(script, workflow []byte) []string {
	var out []string
	if strings.Contains(string(script), "VERDICT_ATTEMPTS") {
		out = append(out, "предел ожидания задан числом заходов (VERDICT_ATTEMPTS): "+
			"заход стоит не только сна, и предел наступает раньше, чем объявлено")
	}
	m := regexp.MustCompile(`(?m)^: "\$\{VERDICT_DEADLINE:=(\d+)\}"`).FindSubmatch(script)
	if m == nil {
		return append(out, "у скрипта нет срока ожидания `: \"${VERDICT_DEADLINE:=<с>}\"`")
	}
	deadline, _ := strconv.Atoi(string(m[1]))
	if deadline < measuredSetSpanSeconds {
		out = append(out, fmt.Sprintf("срок %d с короче измеренного набора %d с — свод "+
			"откажет «не завершились» над набором, который ещё идёт", deadline, measuredSetSpanSeconds))
	}
	var doc struct {
		Jobs map[string]struct {
			TimeoutMinutes *int `yaml:"timeout-minutes"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflow, &doc); err != nil {
		return append(out, "процесс свода не разобран: "+err.Error())
	}
	job, ok := doc.Jobs["all-checks-green"]
	switch {
	case !ok:
		out = append(out, "в процессе свода нет задания all-checks-green")
	case job.TimeoutMinutes == nil:
		out = append(out, "у задания all-checks-green нет timeout-minutes")
	case *job.TimeoutMinutes*60 < deadline+setupReserveSeconds:
		out = append(out, fmt.Sprintf("timeout-minutes %d короче срока скрипта %d с плюс "+
			"подготовка %d с — провайдер убьёт задание раньше, и причины не будет",
			*job.TimeoutMinutes, deadline, setupReserveSeconds))
	}
	return out
}

func readVerdictTreeFile(t *testing.T, rel ...string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(append([]string{repoRoot(t)}, rel...)...))
	if err != nil {
		t.Fatalf("не прочитан %s: %v", filepath.Join(rel...), err)
	}
	return body
}

// TestVerdictWaitDeadlineOutlivesTheMeasuredSet — на дереве сверка молчит, и
// каждая инъекция краснеет со своей причиной.
func TestVerdictWaitDeadlineOutlivesTheMeasuredSet(t *testing.T) {
	t.Parallel()
	script := readVerdictTreeFile(t, ".github", "scripts", "pr-verdict-wait.sh")
	workflow := readVerdictTreeFile(t, ".github", "workflows", "required-verdict.yml")

	if got := deadlineFindings(script, workflow); len(got) != 0 {
		t.Errorf("дерево: %s", strings.Join(got, "; "))
	}

	deadline := regexp.MustCompile(`(?m)^: "\$\{VERDICT_DEADLINE:=\d+\}"`)
	timeout := regexp.MustCompile(`(?m)^(\s+timeout-minutes:) \d+$`)
	if !deadline.Match(script) || len(timeout.FindAll(workflow, -1)) != 1 {
		t.Fatalf("предпосылка инъекций ложна: строка срока в скрипте или единственный " +
			"timeout-minutes в процессе не найдены")
	}
	for _, c := range []struct {
		name, want       string
		script, workflow []byte
	}{
		{"прежний предел заходами", "числом заходов",
			deadline.ReplaceAll(script, []byte(`: "${VERDICT_ATTEMPTS:=170}"`)), workflow},
		{"срок короче замера", "короче измеренного набора",
			deadline.ReplaceAll(script, []byte(`: "${VERDICT_DEADLINE:=3400}"`)), workflow},
		{"задание короче срока", "timeout-minutes 90",
			script, timeout.ReplaceAll(workflow, []byte("$1 90"))},
	} {
		got := strings.Join(deadlineFindings(c.script, c.workflow), "; ")
		if !strings.Contains(got, c.want) {
			t.Errorf("инъекция «%s»: ожидалась находка «%s», получено: %q", c.name, c.want, got)
		}
	}
}

// headRun — прогон процесса на голове в форме ответа actions/runs.
type headRun struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
	RunAttempt int    `json:"run_attempt"`
	HTMLURL    string `json:"html_url"`
	JobsTotal  *int   `json:"jobs_total,omitempty"`
}

func headJobs(n int) *int { return &n }

func postureRun(id int, status, conclusion string, jobsTotal *int) headRun {
	return headRun{ID: id, Name: "production-posture", Path: ".github/workflows/production-posture.yml",
		Status: status, Conclusion: conclusion, RunAttempt: 1,
		HTMLURL: "https://example.invalid/runs/" + strconv.Itoa(id), JobsTotal: jobsTotal}
}

var ciRunningRun = headRun{ID: 7, Name: "ci", Path: ".github/workflows/ci.yaml",
	Status: "in_progress", RunAttempt: 1, JobsTotal: headJobs(40)}

// runWithHeadRuns — настоящий скрипт: решатель отдаёт коды, перечень прогонов
// головы — подставной, gh — подставной (отвечает число заданий прогона).
func runWithHeadRuns(t *testing.T, runs []headRun, ghJobsTotal int, codes ...int) (verdictStubs, int, string) {
	t.Helper()
	s := newVerdictStubs(t, wholePayload, codes...)
	nowCmd, _ := verdictClocks(t, s.dir)
	body, err := json.Marshal(map[string]any{"total_count": len(runs), "workflow_runs": runs})
	if err != nil {
		t.Fatalf("перечень прогонов не собран: %v", err)
	}
	runsCmd := filepath.Join(s.dir, "head-runs.sh")
	writeExec(t, runsCmd, "#!/usr/bin/env bash\ncat <<'RUNS'\n"+string(body)+"\nRUNS\n")
	bin := filepath.Join(s.dir, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatalf("не создан каталог дублёра: %v", err)
	}
	writeExec(t, filepath.Join(bin, "gh"), fmt.Sprintf("#!/usr/bin/env bash\n"+
		"echo \"$*\" >> %q\necho '{\"total_count\": %d, \"jobs\": []}'\n",
		filepath.Join(s.dir, "gh-journal"), ghJobsTotal))

	code, out := runUnderProviderShell(t, waitScriptPath(t), s.dir,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"VERDICT_DEADLINE="+strconv.Itoa(10*clockStep),
		"VERDICT_NOW_CMD="+nowCmd,
		"VERDICT_RUNS_CMD="+runsCmd,
		"VERDICT_DISPLACED_PERIOD=0",
		"VERDICT_FETCH_CMD="+s.fetchCmd,
		"VERDICT_DECIDE_CMD="+s.decideCmd,
	)
	return s, code, out
}

const displacedCause = "production-posture вытеснен — нужен перезапуск на голове"

// TestVerdictWaitRefusesADisplacedRunWithItsCause — прогон posture отменён, не
// заведя заданий: свод отказывает на первом же «ещё идут», называя причину и
// прогон, а не ждёт срока.
func TestVerdictWaitRefusesADisplacedRunWithItsCause(t *testing.T) {
	t.Parallel()
	s, code, out := runWithHeadRuns(t, []headRun{ciRunningRun, postureRun(42, "completed", "cancelled", headJobs(0))}, 0, 3)

	if code != 1 {
		t.Errorf("исход %d, ожидался отказ\n%s", code, out)
	}
	if !strings.Contains(out, displacedCause) || !strings.Contains(out, "прогон 42") {
		t.Errorf("отказ не называет вытеснение и прогон:\n%s", out)
	}
	if strings.Contains(out, "не завершились") {
		t.Errorf("причиной назван срок, а не вытеснение:\n%s", out)
	}
	if got := s.polls(t); got != 1 {
		t.Errorf("заходов %d, ожидался 1 — вытеснение окончательно, ждать нечего", got)
	}
}

// TestVerdictWaitCountsJobsOfACancelledRunThroughTheForge — число заданий
// отменённого прогона берётся у провайдера: ноль — вытеснен, не ноль — прогон
// шёл, и его проверки судит решатель.
func TestVerdictWaitCountsJobsOfACancelledRunThroughTheForge(t *testing.T) {
	t.Parallel()
	t.Run("заданий ноль — вытеснен", func(t *testing.T) {
		t.Parallel()
		s, code, out := runWithHeadRuns(t, []headRun{postureRun(42, "completed", "cancelled", nil)}, 0, 3)
		if code != 1 || !strings.Contains(out, displacedCause) {
			t.Errorf("исход %d, ожидался отказ «%s»\n%s", code, displacedCause, out)
		}
		journal, _ := os.ReadFile(filepath.Join(s.dir, "gh-journal"))
		if !strings.Contains(string(journal), "actions/runs/42/jobs") {
			t.Errorf("число заданий не спрошено у провайдера: %q", journal)
		}
	})
	t.Run("задания были — не вытеснен", func(t *testing.T) {
		t.Parallel()
		_, code, out := runWithHeadRuns(t, []headRun{postureRun(42, "completed", "cancelled", nil)}, 2, 3, 3, 0)
		if code != 0 || strings.Contains(out, "вытеснен") {
			t.Errorf("исход %d — отменённый на ходу прогон принят за вытесненный\n%s", code, out)
		}
	})
}

// TestVerdictWaitKeepsWaitingWhenTheProcessHasALiveRun — законные близнецы: тот же
// процесс перезапущен (попытка ждёт очереди) либо на голове есть его прогон с
// исходом — признак снят, свод ждёт и выносит вердикт решателя.
func TestVerdictWaitKeepsWaitingWhenTheProcessHasALiveRun(t *testing.T) {
	t.Parallel()
	for name, runs := range map[string][]headRun{
		"перезапуск ждёт очереди": {ciRunningRun, postureRun(42, "queued", "", headJobs(0))},
		"есть прогон с исходом":   {postureRun(42, "completed", "cancelled", headJobs(0)), postureRun(43, "completed", "success", headJobs(3))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, code, out := runWithHeadRuns(t, runs, 0, 3, 3, 0)
			if code != 0 || strings.Contains(out, "вытеснен") {
				t.Errorf("исход %d, ожидался 0 без вытеснения\n%s", code, out)
			}
			if got := s.polls(t); got != 3 {
				t.Errorf("заходов %d, ожидалось 3", got)
			}
		})
	}
}

// TestVerdictWaitUnreadHeadRunsAreNotAVerdict — перечень прогонов головы не
// прочитан: ни «вытеснен», ни отказ — предупреждение и следующий заход.
func TestVerdictWaitUnreadHeadRunsAreNotAVerdict(t *testing.T) {
	t.Parallel()
	s := newVerdictStubs(t, wholePayload, 3, 0)
	nowCmd, _ := verdictClocks(t, s.dir)
	runsCmd := filepath.Join(s.dir, "runs-broken.sh")
	writeExec(t, runsCmd, "#!/usr/bin/env bash\necho 'лимит запросов исчерпан' >&2\nexit 1\n")

	code, out := runUnderProviderShell(t, waitScriptPath(t), s.dir,
		"VERDICT_DEADLINE="+strconv.Itoa(10*clockStep),
		"VERDICT_NOW_CMD="+nowCmd,
		"VERDICT_RUNS_CMD="+runsCmd,
		"VERDICT_DISPLACED_PERIOD=0",
		"VERDICT_FETCH_CMD="+s.fetchCmd,
		"VERDICT_DECIDE_CMD="+s.decideCmd,
	)
	if code != 0 {
		t.Errorf("исход %d — непрочитанный перечень прогонов принят за вердикт\n%s", code, out)
	}
	if !strings.Contains(out, "перечень прогонов головы не прочитан") {
		t.Errorf("сбой чтения не назван:\n%s", out)
	}
}
