// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// prverdictevent_test.go — сводный вердикт ПО СОБЫТИЮ исполняется, а не только
// объявлен (kacho#3266).
//
// ПРЕДМЕТ. Прежний свод держал ранер ~40 минут на голову, опрашивая набор каждые
// 20 секунд. Свод по событию поднимается на `workflow_run: completed` каждого
// процесса запроса, читает набор ОДИН раз и ставит commit status. Цена ошибки та
// же, что у опроса, только тише: опрос, не дождавшийся проверки, ПАДАЛ красным, а
// событие, прочитавшее «ещё идут» как «зелено», ставит зелёный статус и больше не
// вернётся — следующего события может не быть.
//
// ЧТО ЗДЕСЬ ДОКАЗЫВАЕТСЯ. Поведение настоящего `pr-verdict-event.sh` под теми же
// флагами шелла, что у площадки (`bash -e`), с подставным `gh` первым в PATH:
// какие статусы скрипт ставит на каждый исход решателя, что запрос из форка и
// голова без единственного запроса дают только pending, что скрипт не зовёт
// решатель там, где судить нельзя. Инъекция рядом воспроизводит прежнюю форму —
// «ещё идут» отображено в success — и требует, чтобы проба её поймала.
package repohygiene

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const eventHeadSha = "0123456789abcdef0123456789abcdef01234567"

// eventForgeScript — подставной `gh`: адрес и метод разбираются из аргументов,
// ответы лежат файлами в каталоге пробы, каждый POST статуса пишет своё
// состояние в `statuses`. Неизвестный адрес — отказ: скрипт, пошедший куда-то
// ещё, проба обязана заметить.
const eventForgeScript = `#!/usr/bin/env bash
set -u
dir=%q
printf '%%s\n' "$*" >> "$dir/journal"
method=GET; path=""; state=""; context=""
[ "${1:-}" = api ] && shift
while [ $# -gt 0 ]; do
  case "$1" in
    -X|--method) method="$2"; shift ;;
    -H|--header) shift ;;
    -f|-F|--field|--raw-field)
      case "$2" in state=*) state="${2#state=}" ;; context=*) context="${2#context=}" ;; esac
      shift ;;
    -*) ;;
    *) [ -z "$path" ] && path="$1" ;;
  esac
  shift
done
case "$method $path" in
  "POST "*/statuses/*) printf '%%s\t%%s\t%%s\n' "$state" "$context" "${path##*/}" >> "$dir/statuses"; echo '{}' ;;
  "GET "*/commits/*/pulls*) cat "$dir/pulls.json" ;;
  "GET "*/pulls/[0-9]*) cat "$dir/pr.json" ;;
  "POST graphql"|"GET graphql") cat "$dir/tree.json" ;;
  "GET "*/check-runs*) cat "$dir/runs.json" ;;
  *) echo "дублёр: неизвестный адрес $method $path" >&2; exit 1 ;;
esac
`

type eventForge struct {
	dir    string
	binDir string
	decide string // подставной решатель
	draft  string // подставной выбор черновика
}

const eventSelfOpen = "сводный вердикт: открытие"
const eventSelfPoll = "сводный вердикт (все проверки завершились и зелены)"
const eventContext = "сводный вердикт (событие)"

func prJSON(number int, sha, headRepo, baseRepo, base string, draft bool) string {
	return fmt.Sprintf(`{"number": %d, "state": "open", "draft": %t, "merge_commit_sha": "%s",`+
		` "head": {"sha": "%s", "repo": {"full_name": "%s"}},`+
		` "base": {"ref": "%s", "repo": {"full_name": "%s"}}}`,
		number, draft, strings.Repeat("e", 40), sha, headRepo, base, baseRepo)
}

// newEventForge — запрос #7 этого репозитория в main, голова eventHeadSha, один
// процесс в каталоге головы, решатель отдаёт code.
func newEventForge(t *testing.T, code int) eventForge {
	t.Helper()
	dir := t.TempDir()
	f := eventForge{dir: dir, binDir: filepath.Join(dir, "bin"),
		decide: filepath.Join(dir, "decide.sh"), draft: filepath.Join(dir, "draft.sh")}
	if err := os.MkdirAll(f.binDir, 0o700); err != nil {
		t.Fatalf("не создан каталог дублёра: %v", err)
	}
	writeExec(t, filepath.Join(f.binDir, "gh"), fmt.Sprintf(eventForgeScript, dir))
	pr := prJSON(7, eventHeadSha, "owner/repo", "owner/repo", "main", false)
	writeExec(t, filepath.Join(dir, "pulls.json"), "["+pr+"]")
	writeExec(t, filepath.Join(dir, "pr.json"), pr)
	f.putTree(t, treeEntry{"ci.yaml", "blob", "name: ci\n", 0}, treeEntry{"README.md", "blob", "x", 0})
	writeExec(t, filepath.Join(dir, "runs.json"), wholePayload)
	f.setDecider(t, code)
	writeExec(t, f.draft, "#!/usr/bin/env bash\necho '{\"full\": false, \"jobs\": [], \"always\": []}'\n")
	return f
}

// treeEntry — элемент каталога процессов головы в ответе GraphQL; size 0 —
// размер по тексту.
type treeEntry struct {
	name, kind, text string
	size             int
}

// putTree — ответ GraphQL на каталог процессов и карту выбора ревизии слияния.
func (f eventForge) putTree(t *testing.T, entries ...treeEntry) {
	t.Helper()
	var items []string
	for _, e := range entries {
		size := e.size
		if size == 0 {
			size = len(e.text)
		}
		items = append(items, fmt.Sprintf(`{"name": %q, "type": %q, "object": {"byteSize": %d,`+
			` "isBinary": false, "isTruncated": false, "text": %q}}`, e.name, e.kind, size, e.text))
	}
	writeExec(t, filepath.Join(f.dir, "tree.json"), `{"data": {"repository": {"wf": {"entries": [`+
		strings.Join(items, ",")+`]}, "map": {"byteSize": 2, "isBinary": false,`+
		` "isTruncated": false, "text": "{}"}}}}`)
}

// setDecider — решатель, пишущий свои аргументы построчно и отдающий код.
func (f eventForge) setDecider(t *testing.T, code int) {
	t.Helper()
	writeExec(t, f.decide, "#!/usr/bin/env bash\ncat > /dev/null\n"+
		"printf '%s\\n' \"$@\" > "+filepath.Join(f.dir, "decide.args")+"\n"+
		// каталог головы удаляется по выходу скрипта — его состав снимается здесь
		"prev=; for a in \"$@\"; do [ \"$prev\" = --workflows ] && "+
		"(ls \"$a\"; ls \"$a/..\") > "+filepath.Join(f.dir, "decide.files")+"; prev=\"$a\"; done\n"+
		"exit "+strconv.Itoa(code)+"\n")
}

func (f eventForge) statuses(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(f.dir, "statuses"))
	if err != nil {
		return nil
	}
	var out []string
	for _, ln := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if ln != "" {
			out = append(out, ln)
		}
	}
	return out
}

func (f eventForge) states(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, ln := range f.statuses(t) {
		out = append(out, strings.SplitN(ln, "\t", 2)[0])
	}
	return out
}

func (f eventForge) decideArgs(t *testing.T) (called bool, args []string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(f.dir, "decide.args"))
	if err != nil {
		return false, nil
	}
	return true, strings.Split(strings.TrimRight(string(body), "\n"), "\n")
}

func eventScript(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), ".github", "scripts", "pr-verdict-event.sh")
}

// runEvent — прогон скрипта под `bash -e`, как его исполняет площадка. extra
// перекрывает окружение события.
func runEvent(t *testing.T, f eventForge, script string, extra ...string) (int, string) {
	t.Helper()
	cmd := exec.Command("bash", providerShellFlags, script)
	cmd.Dir = f.dir
	env := []string{}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PATH=") || strings.HasPrefix(kv, "VERDICT_") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"PATH="+f.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"REPO=owner/repo",
		"HEAD_SHA="+eventHeadSha,
		"HEAD_REPO=owner/repo",
		"RUN_EVENT=pull_request",
		"CONTEXT="+eventContext,
		"SELF_NAMES="+eventSelfOpen+"\n"+eventSelfPoll,
		"VERDICT_DECIDE_CMD="+f.decide,
		"VERDICT_DRAFT_CMD="+f.draft,
		"VERDICT_RETRY_INTERVAL=0",
	)
	cmd.Env = append(env, extra...)
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

// pendingOnlyBreach — свойство, ради которого заведена эта проба: на «ещё идут»
// поставлен ровно pending, и success не поставлен ни разу. Пусто — свойство
// держится; иначе — текст находки. Одна функция судит и настоящий скрипт, и
// инъекцию: иначе инъекция проверяла бы другую пробу.
func pendingOnlyBreach(states []string) string {
	if len(states) == 0 {
		return "статус не поставлен вовсе — контекст отсутствует, и слияние им не гейтится (#614)"
	}
	for _, s := range states {
		if s != "pending" {
			return fmt.Sprintf("на «ещё идут» поставлен %q (все: %q) — набор, который ещё не "+
				"завершился, объявлен вердиктом, и следующего события может не быть", s, states)
		}
	}
	return ""
}

// TestVerdictEventKeepsPendingWhenAWorkflowNeverCompletes — «ещё идут» — это
// pending, а не success, и не отсутствие статуса.
func TestVerdictEventKeepsPendingWhenAWorkflowNeverCompletes(t *testing.T) {
	t.Parallel()
	f := newEventForge(t, 3)

	code, out := runEvent(t, f, eventScript(t))

	if code != 0 {
		t.Errorf("исход %d, ожидался 0: «ещё идут» — состояние, задание не краснеет\n%s", code, out)
	}
	if b := pendingOnlyBreach(f.states(t)); b != "" {
		t.Errorf("%s\n%s", b, out)
	}
}

// legacySuccessForm — прежняя форма отображения: всё, что не красное, — зелено.
// Так читал набор опрос до #1073, когда код 3 обрывал шелл; в событии ту же
// ошибку не ловит ничего, кроме этой пробы.
const legacySuccessForm = "3) state=success"

func TestVerdictEventInjectionLegacySuccessIsCaught(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(eventScript(t))
	if err != nil {
		t.Fatalf("скрипт события не прочитан: %v", err)
	}
	src := string(raw)
	const lawful = "3) state=pending"
	if strings.Count(src, lawful) != 1 {
		t.Fatalf("инъекция не легла: образец %q встречается %d раз, а не один — проба "+
			"ничего не доказывает", lawful, strings.Count(src, lawful))
	}
	f := newEventForge(t, 3)
	mutated := filepath.Join(f.dir, "legacy-event.sh")
	writeExec(t, mutated, strings.Replace(src, lawful, legacySuccessForm, 1))
	// скрипт зовёт соседа по каталогу — инъекция кладёт его копию рядом с собой
	lib, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "scripts", "pr-verdict-checks.sh"))
	if err != nil {
		t.Fatalf("сосед скрипта не прочитан: %v", err)
	}
	writeExec(t, filepath.Join(f.dir, "pr-verdict-checks.sh"), string(lib))

	_, out := runEvent(t, f, mutated)

	if b := pendingOnlyBreach(f.states(t)); b == "" {
		t.Errorf("прежняя форма с ложным success проходит пробу — проба слепа к своему "+
			"предмету\n%s", out)
	}
}

// TestVerdictEventMapsEveryOutcome — каждый исход решателя ставит свой статус, и
// «вердикт не вынесен» не равен ни зелёному, ни красному.
func TestVerdictEventMapsEveryOutcome(t *testing.T) {
	t.Parallel()
	for code, want := range map[int]string{0: "success", 1: "failure", 2: "error", 5: "error"} {
		f := newEventForge(t, code)
		_, out := runEvent(t, f, eventScript(t))
		got := f.statuses(t)
		if len(got) != 1 || !strings.HasPrefix(got[0], want+"\t"+eventContext+"\t"+eventHeadSha) {
			t.Errorf("решатель %d: статусы %q, ожидался один %q на контексте %q головы\n%s",
				code, got, want, eventContext, out)
		}
	}
}

// TestVerdictEventPassesTheLiveRequestToTheDecider — база берётся из запроса
// вживую, каталог процессов — головы, и исключены ОБЕ собственные джобы.
func TestVerdictEventPassesTheLiveRequestToTheDecider(t *testing.T) {
	t.Parallel()
	f := newEventForge(t, 0)
	pr := prJSON(7, eventHeadSha, "owner/repo", "owner/repo", "1266", false)
	writeExec(t, filepath.Join(f.dir, "pr.json"), pr)

	_, out := runEvent(t, f, eventScript(t))

	called, args := f.decideArgs(t)
	if !called {
		t.Fatalf("решатель не вызван\n%s", out)
	}
	joined := strings.Join(args, "\n")
	for _, want := range []string{"--base\n1266", "--self-name\n" + eventSelfOpen,
		"--self-name\n" + eventSelfPoll, "--workflows"} {
		if !strings.Contains(joined, want) {
			t.Errorf("решателю не передано %q: %q", want, args)
		}
	}
	if strings.Contains(joined, "--draft-selection") {
		t.Errorf("запрос не черновик, а выбор черновика передан: %q", args)
	}
	wf := ""
	for i, a := range args {
		if a == "--workflows" && i+1 < len(args) {
			wf = args[i+1]
		}
	}
	if filepath.Base(wf) != "workflows" || filepath.Base(filepath.Dir(wf)) != ".github" {
		t.Errorf("каталог процессов %q — не .github/workflows дерева головы", wf)
	}
	files, err := os.ReadFile(filepath.Join(f.dir, "decide.files"))
	if err != nil {
		t.Fatalf("состав каталога головы не снят: %v", err)
	}
	if got := strings.Fields(string(files)); strings.Join(got, " ") != "ci.yaml ci-paths.json workflows" {
		t.Errorf("каталог головы %q, ожидались ci.yaml (не-процесс README.md отброшен) и карта "+
			"выбора рядом с каталогом", got)
	}
}

// TestVerdictEventRefusesAnUnreadableWorkflowFile — процесс головы двоичный либо
// усечённый площадкой: вердикта нет (error), а не «процессом меньше».
func TestVerdictEventRefusesAnUnreadableWorkflowFile(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]string{
		"усечён":   `"isTruncated": false, "text": "name: x\n"`,
		"двоичный": `"isBinary": false, "isTruncated": false, "text": "name: x\n"`,
	} {
		f := newEventForge(t, 0)
		f.putTree(t, treeEntry{"ci.yaml", "blob", "name: ci\n", 0}, treeEntry{"x.yml", "blob", "name: x\n", 0})
		raw, err := os.ReadFile(filepath.Join(f.dir, "tree.json"))
		if err != nil || strings.Count(string(raw), mutate) != 1 {
			t.Fatalf("%s: инъекция не легла", name)
		}
		bad := map[string]string{"усечён": `"isTruncated": true, "text": "name: x\n"`,
			"двоичный": `"isBinary": true, "isTruncated": false, "text": "name: x\n"`}[name]
		writeExec(t, filepath.Join(f.dir, "tree.json"), strings.Replace(string(raw), mutate, bad, 1))
		_, out := runEvent(t, f, eventScript(t))
		if got := f.states(t); len(got) != 1 || got[0] != "error" {
			t.Errorf("%s: статусы %q, ожидался error\n%s", name, got, out)
		}
	}
	// законный близнец той же формы: подкаталог с именем процесса — не процесс
	f := newEventForge(t, 0)
	f.putTree(t, treeEntry{"ci.yaml", "blob", "name: ci\n", 0}, treeEntry{"old.yml", "tree", "", 1})
	_, out := runEvent(t, f, eventScript(t))
	if got := f.states(t); len(got) != 1 || got[0] != "success" {
		t.Errorf("подкаталог old.yml: статусы %q, ожидался success\n%s", got, out)
	}
}

// TestVerdictEventDraftCarriesTheSelection — черновик судится выбором (kacho#3148).
func TestVerdictEventDraftCarriesTheSelection(t *testing.T) {
	t.Parallel()
	f := newEventForge(t, 1)
	writeExec(t, filepath.Join(f.dir, "pr.json"),
		prJSON(7, eventHeadSha, "owner/repo", "owner/repo", "1266", true))

	_, out := runEvent(t, f, eventScript(t))

	_, args := f.decideArgs(t)
	if !strings.Contains(strings.Join(args, "\n"), "--draft-selection") {
		t.Errorf("черновик судится без выбора: %q\n%s", args, out)
	}
	if got := f.states(t); len(got) != 1 || got[0] != "failure" {
		t.Errorf("черновик: статусы %q, ожидался failure\n%s", got, out)
	}
}

// TestVerdictEventFromAForkOnlyPends — голова чужого репозитория: только pending,
// решатель и чтение процессов головы не зовутся.
func TestVerdictEventFromAForkOnlyPends(t *testing.T) {
	t.Parallel()
	for _, head := range []string{"someone/fork", ""} {
		f := newEventForge(t, 0) // решатель СКАЗАЛ БЫ «зелено»
		_, out := runEvent(t, f, eventScript(t), "HEAD_REPO="+head)
		if got := f.states(t); len(got) != 1 || got[0] != "pending" {
			t.Errorf("голова %q: статусы %q, ожидался один pending\n%s", head, got, out)
		}
		if called, _ := f.decideArgs(t); called {
			t.Errorf("голова %q: решатель вызван на запросе из форка", head)
		}
	}
}

// TestVerdictEventWithoutASingleRequestPends — ноль или больше одного кандидата
// по голове: pending, решатель не зовётся (У6).
func TestVerdictEventWithoutASingleRequestPends(t *testing.T) {
	t.Parallel()
	pr := prJSON(7, eventHeadSha, "owner/repo", "owner/repo", "main", false)
	other := prJSON(8, eventHeadSha, "owner/repo", "owner/repo", "1266", false)
	stale := prJSON(9, strings.Repeat("f", 40), "owner/repo", "owner/repo", "main", false)
	closed := strings.Replace(pr, `"state": "open"`, `"state": "closed"`, 1)
	for name, pulls := range map[string]string{
		"ноль":          "[]",
		"два":           "[" + pr + "," + other + "]",
		"голова другая": "[" + stale + "]",
		"закрытый":      "[" + closed + "]",
	} {
		f := newEventForge(t, 0)
		writeExec(t, filepath.Join(f.dir, "pulls.json"), pulls)
		_, out := runEvent(t, f, eventScript(t))
		if got := f.states(t); len(got) != 1 || got[0] != "pending" {
			t.Errorf("кандидатов %s: статусы %q, ожидался один pending\n%s", name, got, out)
		}
		if called, _ := f.decideArgs(t); called {
			t.Errorf("кандидатов %s: решатель вызван без единственного запроса", name)
		}
	}
}

// TestVerdictEventRejectsAMalformedHeadSha — голова не 40 hex: адреса статуса нет,
// ничего не ставится, задание краснеет (У2).
func TestVerdictEventRejectsAMalformedHeadSha(t *testing.T) {
	t.Parallel()
	for _, sha := range []string{"", "deadbeef", strings.Repeat("0", 39) + "G",
		eventHeadSha + "/../x", strings.ToUpper(eventHeadSha)} {
		f := newEventForge(t, 0)
		code, out := runEvent(t, f, eventScript(t), "HEAD_SHA="+sha)
		if code == 0 {
			t.Errorf("голова %q принята\n%s", sha, out)
		}
		if got := f.statuses(t); len(got) != 0 {
			t.Errorf("голова %q: поставлены статусы %q", sha, got)
		}
	}
}

// TestVerdictEventRefusesAnIllNamedWorkflowFile — имя файла процесса головы вне
// формы: вердикта нет (error), а не «процессом меньше» (У4).
func TestVerdictEventRefusesAnIllNamedWorkflowFile(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"../evil.yml", "a b.yml", ".hidden.yml", "x/y.yaml"} {
		f := newEventForge(t, 0)
		f.putTree(t, treeEntry{"ci.yaml", "blob", "name: ci\n", 0}, treeEntry{name, "blob", "name: x\n", 0})
		_, out := runEvent(t, f, eventScript(t))
		if got := f.states(t); len(got) != 1 || got[0] != "error" {
			t.Errorf("имя %q: статусы %q, ожидался error\n%s", name, got, out)
		}
		if called, _ := f.decideArgs(t); called {
			t.Errorf("имя %q: решатель вызван над неполным каталогом", name)
		}
	}
}

// TestVerdictEventRefusesAnOversizedWorkflowFile — предел размера файла головы.
func TestVerdictEventRefusesAnOversizedWorkflowFile(t *testing.T) {
	t.Parallel()
	f := newEventForge(t, 0)
	f.putTree(t, treeEntry{"ci.yaml", "blob", "name: ci\n", 99999999})
	_, out := runEvent(t, f, eventScript(t))
	if got := f.states(t); len(got) != 1 || got[0] != "error" {
		t.Errorf("статусы %q, ожидался error на файле сверх предела\n%s", got, out)
	}
}

// TestVerdictEventIgnoresForeignEvents — событие не от запроса: статуса нет.
func TestVerdictEventIgnoresForeignEvents(t *testing.T) {
	t.Parallel()
	f := newEventForge(t, 0)
	code, out := runEvent(t, f, eventScript(t), "RUN_EVENT=push")
	if code != 0 || len(f.statuses(t)) != 0 {
		t.Errorf("событие push: исход %d, статусы %q — свод судит только запросы\n%s",
			code, f.statuses(t), out)
	}
}

// TestVerdictEventReadsEveryPage — набор читается целиком тем же разбором, что у
// опроса (общий `pr-verdict-checks.sh`).
func TestVerdictEventReadsEveryPage(t *testing.T) {
	t.Parallel()
	pf, decide := newPagedForge(t, 135)
	pf.putPage(t, 1, 135, 0, 100)
	pf.putPage(t, 2, 135, 100, 35)
	f := newEventForge(t, 0)
	// страницы отдаёт дублёр набора, остальное — дублёр события
	writeExec(t, filepath.Join(f.dir, "fetch.sh"), "#!/usr/bin/env bash\nset -e\ncd "+pf.dir+
		"\nPATH="+pf.binDir+":$PATH\nsource "+filepath.Join(repoRoot(t), ".github", "scripts", "pr-verdict-checks.sh")+
		"\nSHA="+eventHeadSha+" fetch_all_pages\n")
	writeExec(t, f.decide, "#!/usr/bin/env bash\nexec "+decide+"\n")

	_, out := runEvent(t, f, eventScript(t), "VERDICT_FETCH_CMD="+filepath.Join(f.dir, "fetch.sh"))

	if got := f.states(t); len(got) != 1 || got[0] != "success" {
		t.Errorf("135 зелёных на двух страницах: статусы %q, ожидался success\n%s", got, out)
	}
	if got := pf.polls(); got != 1 {
		t.Errorf("решателя звали %d раз, ожидался 1", got)
	}
}

// TestVerdictEventIsTheStepTheWorkflowRuns — пробы выше судят тот скрипт, который
// зовёт процесс по событию, и процесс слушает завершение.
func TestVerdictEventIsTheStepTheWorkflowRuns(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "required-verdict-event.yml"))
	if err != nil {
		t.Fatalf("не прочитан процесс свода по событию: %v", err)
	}
	body := string(raw)
	for _, want := range []string{".github/scripts/pr-verdict-event.sh", "workflow_run:", "completed"} {
		if !strings.Contains(body, want) {
			t.Errorf("процесс свода по событию не содержит %q", want)
		}
	}
	if strings.Contains(body, "seq 1") || strings.Contains(body, "sleep") {
		t.Errorf("в процессе по событию заведено ожидание — ранер снова держится опросом")
	}
}

// TestVerdictEventReadsAnOldRevisionWithoutTheMap — ревизия старше карты выбора:
// пропусков нет, вердикт выносится (как у решателя); у запроса с конфликтом
// ревизии слияния нет — процессы читаются по голове.
func TestVerdictEventReadsAnOldRevisionWithoutTheMap(t *testing.T) {
	t.Parallel()
	f := newEventForge(t, 0)
	raw, err := os.ReadFile(filepath.Join(f.dir, "tree.json"))
	if err != nil {
		t.Fatal(err)
	}
	const mapBlob = `"map": {"byteSize": 2, "isBinary": false, "isTruncated": false, "text": "{}"}`
	if strings.Count(string(raw), mapBlob) != 1 {
		t.Fatalf("инъекция не легла: карты в ответе нет")
	}
	writeExec(t, filepath.Join(f.dir, "tree.json"), strings.Replace(string(raw), mapBlob, `"map": null`, 1))
	pr := strings.Replace(prJSON(7, eventHeadSha, "owner/repo", "owner/repo", "main", false),
		`"merge_commit_sha": "`+strings.Repeat("e", 40)+`"`, `"merge_commit_sha": null`, 1)
	writeExec(t, filepath.Join(f.dir, "pr.json"), pr)

	_, out := runEvent(t, f, eventScript(t))

	if got := f.states(t); len(got) != 1 || got[0] != "success" {
		t.Errorf("статусы %q, ожидался success\n%s", got, out)
	}
	journal, _ := os.ReadFile(filepath.Join(f.dir, "journal"))
	if !strings.Contains(string(journal), "wf="+eventHeadSha+":.github/workflows") {
		t.Errorf("без ревизии слияния процессы читались не по голове:\n%s", journal)
	}
	files, _ := os.ReadFile(filepath.Join(f.dir, "decide.files"))
	if strings.Contains(string(files), "ci-paths.json") {
		t.Errorf("карты на ревизии нет, а решателю подложена: %q", files)
	}
}
