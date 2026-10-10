// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_second_factor_wrap_rotate_test.go — производитель условия Ф12-35 «в»
// (kacho#3038): смена перечня ключей обёртки секретов второго фактора на стенде
// с перекатом службы доступа и ожиданием готовности края.
//
// Проба судит ИСХОД скрипта `scripts/stand-second-factor-wrap-rotate.sh` —
// какую величину получил секрет, кого перекатили, что напечатано, — подставным
// kubectl, который держит состояние секрета в файле и отвечает ровно на те
// вопросы, что задаёт скрипт и `wait-edge-ready.sh`. Кластер не нужен.
//
// Отрицание — в паре с близнецом: стенд без названного кластера отказывает
// кодом 2, тот же вызов с названным — проходит. Материал ключей не печатается
// и не уходит в аргументы kubectl ни на одном шаге.
package deploy_test

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Подставной kubectl. Состояние — файлы рядом с журналом:
//
//	$FAKE_DIR/secret   — величина ключа секрета (открытым текстом, как её читает процесс)
//	$FAKE_DIR/log      — каждый вызов: CALL <аргументы>
//
// Самоотчёт старта службы выводится из ТЕКУЩЕЙ величины секрета (число ключей
// перечня), если FAKE_REPORT_KEYS не задан: так подставной процесс читает то,
// что лежит в секрете, как настоящий на старте.
// Подставной kind: адрес apiserver'а своего кластера — тот, что объявил случай.
const fakeWrapKind = `#!/usr/bin/env bash
printf 'CALL kind %s\n' "$*" >>"$FAKE_DIR/log"
printf 'apiVersion: v1\nclusters:\n- cluster:\n    server: %s\n  name: kind-kacho\n' "${FAKE_KIND_SERVER:-https://127.0.0.1:6443}"
`

const fakeWrapKubectl = `#!/usr/bin/env bash
printf 'CALL %s\n' "$*" >>"$FAKE_DIR/log"
args=" $* "
keys() {
  if [ -n "${FAKE_REPORT_KEYS:-}" ]; then echo "$FAKE_REPORT_KEYS"; return; fi
  v="$(cat "$FAKE_DIR/secret")"; n=1; c="${v//[^,]/}"; echo $(( n + ${#c} ))
}
case "$args" in
  *" config current-context "*) echo "$FAKE_CURRENT" ;;
  *" config view "*"jsonpath"*) printf 'https://127.0.0.1:6443' ;;
  *" config view "*)
    printf 'apiVersion: v1\nkind: Config\ncurrent-context: %s\n' "$FAKE_CURRENT" ;;
  *" get deploy -o json "*|*" get deployment "*" -o json "*)
    case "$args" in
      *" api-gateway "*)
        echo '{"spec":{"selector":{"matchLabels":{"app":"edge"}}}}' ;;
      *)
        extra=""
        [ -n "${FAKE_ENV_ATTEMPTS:-}" ] && extra=',{"name":"KANAME_AUTHN__LOGIN__ADDRESS_ATTEMPTS","value":"'"$FAKE_ENV_ATTEMPTS"'"}'
        cat <<JSON
{"items":[
 {"metadata":{"name":"vpc"},"spec":{"selector":{"matchLabels":{"app":"vpc"}},"template":{"spec":{"containers":[{"name":"vpc","env":[{"name":"X","value":"y"}]}]}}}},
 {"metadata":{"name":"${FAKE_DEPLOY:-kaname-x}"},"spec":{"selector":{"matchLabels":{"app.kubernetes.io/name":"kaname"}},"template":{"spec":{"volumes":[{"name":"config","configMap":{"name":"kaname-x-config"}}],"containers":[{"name":"kaname","env":[{"name":"KANAME_SECOND_FACTOR_ENC_KEY","valueFrom":{"secretKeyRef":{"name":"sf-secret","key":"enc_key","optional":true}}}$extra]}]}}}}
]}
JSON
        ;;
    esac ;;
  *" get secret sf-secret "*)
    [ -f "$FAKE_DIR/secret" ] || { echo 'Error from server (NotFound): secrets "sf-secret" not found' >&2; exit 1; }
    printf '%s' "$(cat "$FAKE_DIR/secret")" | base64 -w0 ;;
  *" patch secret sf-secret "*)
    f=""; prev=""
    for a in "$@"; do [ "$prev" = "--patch-file" ] && f="$a"; prev="$a"; done
    [ -n "$f" ] || { echo "patch без --patch-file" >&2; exit 1; }
    python3 -c 'import json,sys,base64; print(base64.b64decode(json.load(open(sys.argv[1]))["data"]["enc_key"]).decode(), end="")' "$f" >"$FAKE_DIR/secret" ;;
  *" get configmap kaname-x-config "*)
    printf 'logger:\n  level: INFO\nauthn:\n  login:\n    address-attempts: %s\n    address-window: "15m"\n' "${FAKE_CM_ATTEMPTS:-5}" ;;
  *" rollout restart "*) : ;;
  *" rollout status "*) : ;;
  *" get pods -l "*)
    cat <<JSON
{"items":[
 {"metadata":{"name":"kaname-x-1","annotations":{"prometheus.io/port":"9095","prometheus.io/scheme":"http"}},"status":{"phase":"Running"}},
 {"metadata":{"name":"kaname-x-2","annotations":{"prometheus.io/port":"9095","prometheus.io/scheme":"http"}},"status":{"phase":"Running"}},
 {"metadata":{"name":"kaname-x-old","deletionTimestamp":"2026-01-01T00:00:00Z","annotations":{}},"status":{"phase":"Running"}}
]}
JSON
    ;;
  *" get pods -o json "*)
    echo '{"items":[{"metadata":{"name":"edge-1","labels":{"app":"edge"}},"status":{"phase":"Running"},"spec":{"containers":[{"ports":[{"name":"cmux","containerPort":8443}]}]}}]}' ;;
  *" get --raw "*) echo ok ;;
  *" logs -l "*)
    printf '%s\n' \
      '[pod/kaname-x-1/kaname] {"time":"t","level":"INFO","msg":"login ok"}' \
      '[pod/kaname-x-1/kaname] {"time":"t","level":"ERROR","msg":"step-up: second factor material does not open"}' \
      '[pod/kaname-x-2/kaname] {"time":"t","level":"WARN","msg":"slow"}' ;;
  *" logs "*)
    [ "${FAKE_NO_REPORT:-}" = "$(echo "$args" | sed -n 's/.* logs \([^ ]*\) .*/\1/p')" ] && { echo '{"level":"INFO","msg":"started"}'; exit 0; }
    echo '{"level":"INFO","msg":"second-factor wrapping keys declared","keys":'"$(keys)"',"knob":"authn.second-factor-encryption-key-hex"}' ;;
  *" port-forward "*)
    echo "Forwarding from 127.0.0.1:$FAKE_METRICS_PORT -> 9095"
    exec sleep 30 ;;
esac
exit 0
`

type wrapRun struct {
	out  string
	code int
	log  string
}

// wrapEnv — окружение вызова: подставной kubectl первым в PATH, состояние в dir.
type wrapEnv struct {
	dir   string
	state string
	extra []string
}

func newWrapEnv(t *testing.T, initial string) *wrapEnv {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"kubectl": fakeWrapKubectl, "kind": fakeWrapKind} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if initial != "" {
		if err := os.WriteFile(filepath.Join(dir, "secret"), []byte(initial), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Проброс к поду (готовность края при дороге port-forward) ведёт на свой
	// слушатель: /readyz — 200. Случай, которому нужны счётчики, ставит свой.
	edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			fmt.Fprintln(w, "ok")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(edge.Close)
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(edge.URL, "http://"))
	return &wrapEnv{dir: dir, state: filepath.Join(dir, "state"), extra: []string{"FAKE_METRICS_PORT=" + port}}
}

func (e *wrapEnv) run(t *testing.T, env []string, args ...string) wrapRun {
	t.Helper()
	cmd := exec.Command("bash", append([]string{"scripts/stand-second-factor-wrap-rotate.sh"}, args...)...)
	cmd.Env = append([]string{
		"PATH=" + filepath.Join(e.dir, "bin") + ":" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"FAKE_DIR=" + e.dir,
		"FAKE_CURRENT=kind-kacho",
		"EDGE_READY_ATTEMPTS=3",
		"EDGE_READY_INTERVAL=0",
		"EDGE_READY_STREAK=1",
	}, append(e.extra, env...)...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("скрипт не запустился: %v", err)
	}
	logb, _ := os.ReadFile(filepath.Join(e.dir, "log"))
	return wrapRun{string(out), code, string(logb)}
}

func (e *wrapEnv) secret(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.dir, "secret"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// k1 — ключ стенда до пробы: 32 байта hex, как его чеканит посев dev-prod-secrets.sh.
const k1 = "1111111111111111111111111111111111111111111111111111111111111111"

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestWrapRotate_RefusesAClusterThatIsNotNamed — стенд проб в своём
// пространстве внешнего кластера без названного контекста — код 2 с именем
// недостающего; рабочее пространство kacho на чужом кластере (активный
// контекст не kind) — тоже 2. Близнец: тот же вызов на kind — код 0.
func TestWrapRotate_RefusesAClusterThatIsNotNamed(t *testing.T) {
	e := newWrapEnv(t, k1)

	r := e.run(t, []string{"KACHO_STAND_NS=t3038-wrap"}, "set", "K2,K1", "--state", e.state)
	if r.code != 2 || !strings.Contains(r.out, "KACHO_CLIENT_CONTEXT") {
		t.Fatalf("пространство стенда без названного контекста: ждался код 2 с именем KACHO_CLIENT_CONTEXT, получен %d:\n%s", r.code, r.out)
	}
	r = e.run(t, []string{"FAKE_CURRENT=u/x-client"}, "set", "K2,K1", "--state", e.state)
	if r.code != 2 || !strings.Contains(r.out, "kind") {
		t.Fatalf("пространство kacho при активном контексте не kind: ждался код 2, получен %d:\n%s", r.code, r.out)
	}
	r = e.run(t, []string{"KACHO_CLIENT_CONTEXT=u/x-client"}, "set", "K2,K1", "--ns", "kacho", "--state", e.state)
	if r.code != 2 {
		t.Fatalf("рабочее пространство kacho на названном внешнем кластере обязано отвергаться, код %d:\n%s", r.code, r.out)
	}
	// Контекст назван kind-kacho, а ведёт не в кластер, о котором говорит kind, — 2.
	r = e.run(t, []string{"FAKE_KIND_SERVER=https://127.0.0.1:7443"}, "set", "K2,K1", "--state", e.state)
	if r.code != 2 || !strings.Contains(r.out, "кластер не тот") {
		t.Fatalf("одноимённый контекст другого кластера: ждался код 2, получен %d:\n%s", r.code, r.out)
	}
	if e.secret(t) != k1 {
		t.Fatalf("отказ изменил секрет: %q", e.secret(t))
	}
	// Близнец: kind — проходит.
	r = e.run(t, nil, "set", "K2,K1", "--state", e.state)
	if r.code != 0 {
		t.Fatalf("близнец на kind обязан пройти, код %d:\n%s", r.code, r.out)
	}
	// Названный внешний контекст и пространство стенда — проходит, и каждый
	// вызов kubectl несёт этот контекст явно.
	e2 := newWrapEnv(t, k1)
	r = e2.run(t, []string{"KACHO_STAND_NS=t3038-wrap", "KACHO_CLIENT_CONTEXT=u/x-client", "FAKE_CURRENT=u/x-infra"},
		"set", "K2,K1", "--state", e2.state)
	if r.code != 0 {
		t.Fatalf("названный контекст стенда обязан пройти, код %d:\n%s", r.code, r.out)
	}
	for _, l := range strings.Split(r.log, "\n") {
		if !strings.HasPrefix(l, "CALL ") || strings.Contains(l, " config ") {
			continue
		}
		if strings.Contains(l, "--context u/x-client") || strings.Contains(l, "port-forward") || strings.Contains(l, "--raw") ||
			strings.Contains(l, "get pods -o json") || strings.Contains(l, "get deployment api-gateway") {
			continue
		}
		t.Errorf("вызов kubectl без явного контекста стенда: %s", l)
	}
}

// TestWrapRotate_ListsAreComposedFromTheStandKeyAndAFreshOne — перечни
// сценария: K2,K1 → K2 → восстановление K1. K1 — ключ стенда до пробы, K2 —
// свежий 32-байтный и один на весь ход; перекатывается Deployment, чья
// переменная читает секрет (имя выведено, а не выписано); материал не
// печатается и в аргументы kubectl не уходит.
func TestWrapRotate_ListsAreComposedFromTheStandKeyAndAFreshOne(t *testing.T) {
	e := newWrapEnv(t, k1)

	r := e.run(t, nil, "set", "K2,K1", "--state", e.state)
	if r.code != 0 {
		t.Fatalf("set K2,K1: код %d:\n%s", r.code, r.out)
	}
	parts := strings.Split(e.secret(t), ",")
	if len(parts) != 2 || parts[1] != k1 || !hex64.MatchString(parts[0]) || parts[0] == k1 {
		t.Fatalf("K2,K1: секрет %q — ждались свежий 64-hex ключ и прежний ключ стенда за ним", e.secret(t))
	}
	k2 := parts[0]
	if !strings.Contains(r.out, "keys=2") {
		t.Fatalf("самоотчёт старта (2 ключа) не напечатан:\n%s", r.out)
	}
	if !strings.Contains(r.log, "rollout restart deployment/kaname-x") || !strings.Contains(r.log, "rollout status deployment/kaname-x") {
		t.Fatalf("перекатан не Deployment, читающий секрет (kaname-x):\n%s", r.log)
	}

	r = e.run(t, nil, "set", "K2", "--state", e.state)
	if r.code != 0 || e.secret(t) != k2 || !strings.Contains(r.out, "keys=1") {
		t.Fatalf("set K2: код %d, секрет %q (ждался тот же K2 без K1):\n%s", r.code, e.secret(t), r.out)
	}

	r = e.run(t, nil, "restore", "--state", e.state)
	if r.code != 0 || e.secret(t) != k1 {
		t.Fatalf("restore: код %d, секрет %q (ждался ключ стенда до пробы):\n%s", r.code, e.secret(t), r.out)
	}
	if left, _ := os.ReadDir(e.state); len(left) != 0 {
		t.Fatalf("после восстановления в каталоге состояния остались файлы: %v", left)
	}
	// Повторное восстановление — восстанавливать нечего, код 0, секрет не тронут.
	r = e.run(t, nil, "restore", "--state", e.state)
	if r.code != 0 || e.secret(t) != k1 {
		t.Fatalf("повторный restore: код %d, секрет %q:\n%s", r.code, e.secret(t), r.out)
	}

	all := r.out + e.run(t, nil, "set", "K1", "--state", e.state).out
	logb, _ := os.ReadFile(filepath.Join(e.dir, "log"))
	for _, m := range []string{k1, k2, base64.StdEncoding.EncodeToString([]byte(k1))} {
		if strings.Contains(all, m) || strings.Contains(string(logb), m) {
			t.Fatalf("материал ключа напечатан либо ушёл в аргументы kubectl")
		}
	}
	_ = e.run(t, nil, "restore", "--state", e.state)
}

// TestWrapRotate_ReportsWhatTheProcessDeclared — число ключей печатается из
// самоотчёта процесса, а не из заданного перечня: процесс, объявивший 3 при
// перечне из 2, печатается как 3 (судит проба). Под без самоотчёта — условие не
// создано, код 3.
func TestWrapRotate_ReportsWhatTheProcessDeclared(t *testing.T) {
	e := newWrapEnv(t, k1)
	r := e.run(t, []string{"FAKE_REPORT_KEYS=3"}, "set", "K2,K1", "--state", e.state)
	if r.code != 0 || !strings.Contains(r.out, "keys=3") {
		t.Fatalf("напечатано не объявленное процессом (3): код %d:\n%s", r.code, r.out)
	}
	r = e.run(t, []string{"FAKE_NO_REPORT=kaname-x-2"}, "set", "K2", "--state", e.state)
	if r.code != 3 || !strings.Contains(r.out, "kaname-x-2") {
		t.Fatalf("под без самоотчёта старта обязан дать код 3 с его именем, получен %d:\n%s", r.code, r.out)
	}
	_ = e.run(t, nil, "restore", "--state", e.state)
}

// TestWrapRotate_RefusesWhatItCannotCompose — имя вне словаря {K1, K2} и
// пустой перечень — код 2; секрета нет — код 2 (стенд не заводил ключа, и
// «ключ стенда до пробы» взять неоткуда). Секрет не тронут.
func TestWrapRotate_RefusesWhatItCannotCompose(t *testing.T) {
	e := newWrapEnv(t, k1)
	for _, keys := range []string{"K3", "", "K2,,K1", "K2,K2"} {
		r := e.run(t, nil, "set", keys, "--state", e.state)
		if r.code != 2 {
			t.Errorf("перечень %q: ждался код 2, получен %d:\n%s", keys, r.code, r.out)
		}
	}
	if e.secret(t) != k1 {
		t.Fatalf("отказ изменил секрет: %q", e.secret(t))
	}
	r := e.run(t, nil, "set", "K2,K1")
	if r.code != 2 || !strings.Contains(r.out, "--state") {
		t.Fatalf("без каталога состояния: ждался код 2 с именем --state, получен %d:\n%s", r.code, r.out)
	}
	none := newWrapEnv(t, "")
	r = none.run(t, nil, "set", "K2,K1", "--state", none.state)
	if r.code != 2 || !strings.Contains(r.out, "sf-secret") {
		t.Fatalf("секрета нет: ждался код 2 с его именем, получен %d:\n%s", r.code, r.out)
	}
}

// TestWrapRotate_CellSumsTheSeriesOverLivePods — клетка счётчика — сумма серии
// по живым подам службы (под в завершении не спрашивается); серии нет —
// печатается 0, а не отказ: «клетка ещё не заводилась» — законное начальное
// состояние счётчика.
func TestWrapRotate_CellSumsTheSeriesOverLivePods(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, `# HELP kaname_second_factor_refusals_total x`)
		fmt.Fprintln(w, `kaname_second_factor_refusals_total{reason="unavailable"} 2`)
		fmt.Fprintln(w, `kaname_second_factor_refusals_total{reason="not-enrolled"} 9`)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	e := newWrapEnv(t, k1)
	e.extra = []string{"FAKE_METRICS_PORT=" + port}

	r := e.run(t, nil, "cell", `kaname_second_factor_refusals_total{reason="unavailable"}`)
	if r.code != 0 || strings.TrimSpace(lastLine(r.out)) != "4" {
		t.Fatalf("сумма по двум живым подам (2+2) — 4; код %d:\n%s", r.code, r.out)
	}
	r = e.run(t, nil, "cell", `kaname_second_factor_presentations_total{method="totp",outcome="material-unreadable"}`)
	if r.code != 0 || strings.TrimSpace(lastLine(r.out)) != "0" {
		t.Fatalf("незаведённая клетка — 0; код %d:\n%s", r.code, r.out)
	}
	if strings.Contains(r.log, "kaname-x-old") {
		t.Fatalf("спрошен под в завершении:\n%s", r.log)
	}
}

// TestWrapRotate_ErrorsAreTheErrorLevelRecordsOfTheService — записи уровня
// ошибки службы с момента — только ERROR, с числом в последней строке.
func TestWrapRotate_ErrorsAreTheErrorLevelRecordsOfTheService(t *testing.T) {
	e := newWrapEnv(t, k1)
	r := e.run(t, nil, "errors", "2026-10-11T00:00:00Z")
	if r.code != 0 || lastLine(r.out) != "errors=1" || !strings.Contains(r.out, "second factor material does not open") ||
		strings.Contains(r.out, "login ok") || strings.Contains(r.out, "slow") {
		t.Fatalf("ждалась одна запись уровня ошибки; код %d:\n%s", r.code, r.out)
	}
	if !strings.Contains(r.log, "--since-time=2026-10-11T00:00:00Z") {
		t.Fatalf("журнал прочитан не с названного момента:\n%s", r.log)
	}
	if r := e.run(t, nil, "errors", "вчера"); r.code != 2 {
		t.Fatalf("момент не RFC3339 — код 2, получен %d", r.code)
	}
}

// TestWrapRotate_LimitIsWhatTheProcessReads — предел неверных предъявлений на
// адрес: переменная процесса перекрывает файл настроек (порядок разборщика
// службы), файл — иначе.
func TestWrapRotate_LimitIsWhatTheProcessReads(t *testing.T) {
	e := newWrapEnv(t, k1)
	r := e.run(t, []string{"FAKE_CM_ATTEMPTS=7"}, "limit")
	if r.code != 0 || lastLine(r.out) != "7" {
		t.Fatalf("предел из файла настроек — 7; код %d:\n%s", r.code, r.out)
	}
	r = e.run(t, []string{"FAKE_CM_ATTEMPTS=7", "FAKE_ENV_ATTEMPTS=11"}, "limit")
	if r.code != 0 || lastLine(r.out) != "11" {
		t.Fatalf("переменная процесса перекрывает файл — 11; код %d:\n%s", r.code, r.out)
	}
	if _, err := strconv.Atoi(lastLine(r.out)); err != nil {
		t.Fatalf("предел не число: %q", lastLine(r.out))
	}
}

// TestWrapRotate_MakeTargetIsTheEntry — цель deploy/Makefile зовёт скрипт и
// объявлена в .PHONY: производитель условия вызывается целью, а не рукой.
func TestWrapRotate_MakeTargetIsTheEntry(t *testing.T) {
	b, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	mk := string(b)
	i := strings.Index(mk, "\nstand-second-factor-wrap-rotate:")
	if i < 0 {
		t.Fatal("цели stand-second-factor-wrap-rotate в deploy/Makefile нет")
	}
	recipe := mk[i:]
	if j := strings.Index(recipe[1:], "\n\n"); j > 0 {
		recipe = recipe[:j+1]
	}
	if !strings.Contains(recipe, "scripts/stand-second-factor-wrap-rotate.sh") {
		t.Fatalf("рецепт цели не зовёт скрипт:\n%s", recipe)
	}
	if !regexp.MustCompile(`(?s)\.PHONY:[^\n]*(\\\n[^\n]*)*stand-second-factor-wrap-rotate`).MatchString(mk) {
		t.Fatal("цель не объявлена в .PHONY")
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}
