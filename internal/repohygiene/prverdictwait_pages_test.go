// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// prverdictwait_pages_test.go — набор проверок читается ЦЕЛИКОМ, а не первой
// страницей (#3123).
//
// ПРЕДМЕТ. Провайдер отдаёт check-runs страницами не больше сотни. Скрипт ожидания
// читал одну страницу, и, когда проверок на голове стало больше ста (PR #3122:
// 135), страж «прочитано не целиком» отказывал в вердикте на КАЖДОМ опросе при
// всех зелёных проверках. Страж был прав — недочитывал сам скрипт.
//
// ЧТО ЗДЕСЬ ДОКАЗЫВАЕТСЯ. Пробы идут по НАСТОЯЩЕМУ пути получения набора — тому,
// что исполняется без `VERDICT_FETCH_CMD`, — с подставным `gh` в PATH, который
// отдаёт набор страницами, как провайдер. Решатель-дублёр выносит «зелено» ТОЛЬКО
// если получил весь набор поимённо, поэтому зелёный исход доказывает, что
// страницы склеены, а не то, что скрипт дошёл до решателя.
//
// Пары:
//
//	135 проверок на двух страницах          → вердикт вынесен, решатель видел 135;
//	вторая страница оборвана                → «НЕ ЦЕЛИКОМ», решателя не звали;
//	прежняя форма (одна страница) на том же → «НЕ ЦЕЛИКОМ» — воспроизведение #3123;
//	набор менялся между страницами          → неудачный опрос и второй заход, а не вердикт.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// pagedForge — подставной `gh`: отдаёт `page-<N>.json` на запрос `page=<N>`,
// пишет каждый запрошенный адрес в журнал, падает один раз на странице, для
// которой лежит `page-<N>.fail`, и один раз отдаёт `page-<N>.once.json` вместо
// постоянной — так изображается набор, выросший между заходами.
type pagedForge struct {
	dir     string // рабочий каталог скрипта
	binDir  string // каталог с подставным gh, ставится первым в PATH
	journal string
	tally   string
}

const pagedForgeScript = `#!/usr/bin/env bash
set -u
dir=%q
path="${2:-}"
echo "$path" >> "$dir/journal"
page=1
# page= отдельным параметром; per_page= страницей не является.
case "$path" in *[?\&]page=*) page="${path##*[?\&]page=}"; page="${page%%%%&*}" ;; esac
if [ -f "$dir/page-$page.fail" ]; then
  rm -f "$dir/page-$page.fail"
  echo "обрыв соединения на странице $page" >&2
  exit 1
fi
if [ -f "$dir/page-$page.once.json" ]; then cat "$dir/page-$page.once.json"; rm -f "$dir/page-$page.once.json"; exit 0; fi
if [ -f "$dir/page-$page.json" ]; then cat "$dir/page-$page.json"; exit 0; fi
echo '{"total_count": 0, "check_runs": []}'
`

// Решатель-дублёр: «зелено» (0) только если получил ровно want различных имён,
// иначе «красно» (1). Счёт заходов — в tally.
const wholeSetDecider = `#!/usr/bin/env bash
echo x >> %q
python3 -c '
import json,sys
p = json.load(sys.stdin)
names = {r["name"] for r in p.get("check_runs", [])}
sys.exit(0 if len(names) == %d else 1)
'
`

func newPagedForge(t *testing.T, want int) (pagedForge, string) {
	t.Helper()
	dir := t.TempDir()
	f := pagedForge{
		dir:     dir,
		binDir:  filepath.Join(dir, "bin"),
		journal: filepath.Join(dir, "journal"),
		tally:   filepath.Join(dir, "tally"),
	}
	if err := os.MkdirAll(f.binDir, 0o700); err != nil {
		t.Fatalf("не создан каталог дублёра: %v", err)
	}
	writeExec(t, filepath.Join(f.binDir, "gh"), fmt.Sprintf(pagedForgeScript, dir))
	decide := filepath.Join(dir, "decide.sh")
	writeExec(t, decide, fmt.Sprintf(wholeSetDecider, f.tally, want))
	return f, decide
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("не записан дублёр %s: %v", path, err)
	}
}

// putPage кладёт страницу: проверки с номерами [from, from+n), total — как
// объявил бы провайдер.
func (f pagedForge) putPage(t *testing.T, page, total, from, n int) {
	t.Helper()
	f.putPageAs(t, "page-"+strconv.Itoa(page)+".json", total, from, n)
}

// putPageOnce — страница, которую подставной провайдер отдаст ОДИН раз.
func (f pagedForge) putPageOnce(t *testing.T, page, total, from, n int) {
	t.Helper()
	f.putPageAs(t, "page-"+strconv.Itoa(page)+".once.json", total, from, n)
}

func (f pagedForge) putPageAs(t *testing.T, file string, total, from, n int) {
	t.Helper()
	type run struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	runs := make([]run, 0, n)
	for i := from; i < from+n; i++ {
		runs = append(runs, run{ID: 1000 + i, Name: "check-" + strconv.Itoa(i), Status: "completed", Conclusion: "success"})
	}
	body, err := json.Marshal(map[string]any{"total_count": total, "check_runs": runs})
	if err != nil {
		t.Fatalf("страница не собрана: %v", err)
	}
	writeExec(t, filepath.Join(f.dir, file), string(body))
}

func (f pagedForge) failOnce(t *testing.T, page int) {
	t.Helper()
	writeExec(t, filepath.Join(f.dir, "page-"+strconv.Itoa(page)+".fail"), "")
}

func (f pagedForge) requested(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(f.journal)
	if err != nil {
		return nil
	}
	return strings.Fields(string(body))
}

func (f pagedForge) polls() int {
	body, err := os.ReadFile(f.tally)
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(body)))
}

// runWaitThroughForge — настоящий скрипт под флагами провайдера, БЕЗ
// `VERDICT_FETCH_CMD`, с подставным gh первым в PATH. fetchCmd непустой —
// подмена получателя (так ставится прежняя форма в инъекции).
func runWaitThroughForge(t *testing.T, f pagedForge, decide, fetchCmd string, attempts int) (int, string) {
	t.Helper()
	script := filepath.Join(repoRoot(t), ".github", "scripts", "pr-verdict-wait.sh")
	cmd := exec.Command("bash", providerShellFlags, script)
	cmd.Dir = f.dir
	env := []string{}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PATH=") || strings.HasPrefix(kv, "VERDICT_") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env,
		"PATH="+f.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"REPO=owner/repo",
		"SHA=deadbeef",
		"SELF=сводный вердикт (все проверки завершились и зелены)",
		"VERDICT_ATTEMPTS="+strconv.Itoa(attempts),
		"VERDICT_INTERVAL=0",
		"VERDICT_FETCH_CMD="+fetchCmd,
		"VERDICT_DECIDE_CMD="+decide,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if asExitError(err, &ee) {
			return ee.ExitCode(), string(out)
		}
		t.Fatalf("скрипт не запустился: %v\n%s", err, out)
	}
	return 0, string(out)
}

// TestVerdictWaitReadsEveryPage — 135 проверок на двух страницах: вердикт
// вынесен, и вынесен над всеми 135.
func TestVerdictWaitReadsEveryPage(t *testing.T) {
	t.Parallel()
	f, decide := newPagedForge(t, 135)
	f.putPage(t, 1, 135, 0, 100)
	f.putPage(t, 2, 135, 100, 35)

	code, out := runWaitThroughForge(t, f, decide, "", 3)

	if code != 0 {
		t.Errorf("исход %d, ожидался 0: 135 зелёных проверок на двух страницах — вердикт есть\n%s", code, out)
	}
	if strings.Contains(out, "НЕ ЦЕЛИКОМ") {
		t.Errorf("страж недочтения сработал на целиком прочитанном наборе — вторая страница не запрошена:\n%s", out)
	}
	if got := f.polls(); got != 1 {
		t.Errorf("решателя звали %d раз, ожидался 1", got)
	}
	req := f.requested(t)
	if len(req) != 2 || !strings.Contains(req[0], "page=1") || !strings.Contains(req[1], "page=2") {
		t.Errorf("запрошены адреса %q, ожидались page=1 и page=2", req)
	}
	for _, r := range req {
		if !strings.Contains(r, "per_page=100") {
			t.Errorf("адрес %q без per_page=100 — страниц было бы вчетверо больше", r)
		}
	}
}

// TestVerdictWaitRefusesACutSecondPage — вторая страница оборвана: прочитано
// 110 из 135. Это настоящее недочтение, и страж обязан его назвать.
func TestVerdictWaitRefusesACutSecondPage(t *testing.T) {
	t.Parallel()
	f, decide := newPagedForge(t, 110) // решатель СКАЗАЛ БЫ «зелено» над прочитанным
	f.putPage(t, 1, 135, 0, 100)
	f.putPage(t, 2, 135, 100, 10)

	code, out := runWaitThroughForge(t, f, decide, "", 3)

	if code == 0 {
		t.Errorf("исход 0 — вердикт вынесен по оборванному набору\n%s", out)
	}
	if !strings.Contains(out, "НЕ ЦЕЛИКОМ") || !strings.Contains(out, "проверок 135, получено 110") {
		t.Errorf("отказ не называет недочтение числами:\n%s", out)
	}
	if got := f.polls(); got != 0 {
		t.Errorf("решателя звали %d раз — недочтение обязано отсекаться ДО вердикта", got)
	}
}

// TestVerdictWaitRetriesAFailedSecondPage — сбой на второй странице — неудачный
// опрос, а не вердикт и не недочтение: второй заход читает обе страницы.
func TestVerdictWaitRetriesAFailedSecondPage(t *testing.T) {
	t.Parallel()
	f, decide := newPagedForge(t, 135)
	f.putPage(t, 1, 135, 0, 100)
	f.putPage(t, 2, 135, 100, 35)
	f.failOnce(t, 2)

	code, out := runWaitThroughForge(t, f, decide, "", 3)

	if code != 0 {
		t.Errorf("исход %d — сбой одной страницы принят за вердикт\n%s", code, out)
	}
	if !strings.Contains(out, "опрос не удался") {
		t.Errorf("сбой страницы не назван неудачным опросом:\n%s", out)
	}
	if got := len(f.requested(t)); got != 4 {
		t.Errorf("запросов %d, ожидалось 4 (1, 2-сбой, 1, 2)", got)
	}
}

// TestVerdictWaitRetriesASetThatMovedBetweenPages — проверка появилась между
// страницами: total_count первой страницы разошёлся со второй. Это не вердикт и
// не недочтение — опрос повторяется, и на устоявшемся наборе вердикт выносится.
// Без этой ветви рост набора во время чтения красил бы вердикт как недочтение.
func TestVerdictWaitRetriesASetThatMovedBetweenPages(t *testing.T) {
	t.Parallel()
	f, decide := newPagedForge(t, 135)
	f.putPageOnce(t, 1, 134, 0, 100) // первый заход: первая страница видела 134
	f.putPage(t, 1, 135, 0, 100)
	f.putPage(t, 2, 135, 100, 35)

	code, out := runWaitThroughForge(t, f, decide, "", 3)

	if code != 0 {
		t.Errorf("исход %d, ожидался 0 на втором заходе по устоявшемуся набору\n%s", code, out)
	}
	if !strings.Contains(out, "набор менялся между страницами") {
		t.Errorf("неудача первого опроса не называет причину:\n%s", out)
	}
	if strings.Contains(out, "НЕ ЦЕЛИКОМ") {
		t.Errorf("сдвиг набора назван недочтением — это не вердикт, а повод спросить снова:\n%s", out)
	}
	if got := f.polls(); got != 1 {
		t.Errorf("решателя звали %d раз, ожидался 1 — несогласованный набор до него доходить не должен", got)
	}
}

// TestSinglePageFormReproducesTheDefect — инъекция: прежний получатель
// (одна страница, дословно строка до #3123) на том же подставном провайдере даёт
// ровно наблюдённый отказ. Без этой половины пробы выше остались бы зелёными,
// вернись одностраничное чтение обратно.
func TestSinglePageFormReproducesTheDefect(t *testing.T) {
	t.Parallel()
	f, decide := newPagedForge(t, 135)
	f.putPage(t, 1, 135, 0, 100)
	f.putPage(t, 2, 135, 100, 35)
	legacy := filepath.Join(f.dir, "legacy-fetch.sh")
	writeExec(t, legacy, "#!/usr/bin/env bash\n"+
		`gh api "repos/$REPO/commits/$SHA/check-runs?per_page=100"`+"\n")

	code, out := runWaitThroughForge(t, f, decide, legacy, 3)

	if code == 0 {
		t.Errorf("исход 0 — прежняя форма не воспроизводит дефект, и пробы выше ничего не доказывают\n%s", out)
	}
	if !strings.Contains(out, "проверок 135, получено 100") {
		t.Errorf("прежняя форма обязана дать дословный отказ PR #3122 (135/100):\n%s", out)
	}
}
