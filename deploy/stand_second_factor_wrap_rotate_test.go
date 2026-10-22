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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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
 {"metadata":{"name":"kaname-x-1","labels":{"app.kubernetes.io/name":"kaname"},"annotations":{"prometheus.io/port":"9095","prometheus.io/scheme":"http"}},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},
 {"metadata":{"name":"kaname-x-2","labels":{"app.kubernetes.io/name":"kaname"},"annotations":{"prometheus.io/port":"9095","prometheus.io/scheme":"http"}},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},
 {"metadata":{"name":"kaname-x-old","labels":{"app.kubernetes.io/name":"kaname"},"deletionTimestamp":"2026-01-01T00:00:00Z","annotations":{}},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"False"}]}}
]}
JSON
    ;;
  *" get services -o json "*)
    if [ "${FAKE_EP:-}" = noservice ]; then
      echo '{"items":[{"metadata":{"name":"vpc"},"spec":{"selector":{"app":"vpc"}}}]}'
    else
      echo '{"items":[{"metadata":{"name":"vpc"},"spec":{"selector":{"app":"vpc"}}},{"metadata":{"name":"kaname"},"spec":{"selector":{"app.kubernetes.io/name":"kaname"}}},{"metadata":{"name":"kaname-internal"},"spec":{"selector":{"app.kubernetes.io/name":"kaname"}}}]}'
    fi ;;
  *" get endpointslices -o json "*)
    # Конечные точки догоняют перекат: первые FAKE_EP_LAG опросов адрес
    # kaname-x-2 ещё не готов, а адрес заменённого пода ещё числится.
    n=0; [ -f "$FAKE_DIR/ep-polls" ] && n="$(cat "$FAKE_DIR/ep-polls")"; n=$((n + 1)); echo "$n" >"$FAKE_DIR/ep-polls"
    ep() { printf '{"addresses":["10.0.0.%s"],"conditions":{"ready":%s},"targetRef":{"kind":"Pod","name":"%s"}}' "$1" "$2" "$3"; }
    if [ "${FAKE_EP:-}" = stale ] || [ "$n" -le "${FAKE_EP_LAG:-0}" ]; then
      eps="$(ep 1 true kaname-x-1),$(ep 2 false kaname-x-2),$(ep 9 true kaname-x-old)"
    else
      eps="$(ep 1 true kaname-x-1),$(ep 2 true kaname-x-2)"
    fi
    echo '{"items":[{"metadata":{"name":"vpc-abc","labels":{"kubernetes.io/service-name":"vpc"}},"endpoints":[]},{"metadata":{"name":"kaname-abc","labels":{"kubernetes.io/service-name":"kaname"}},"endpoints":['"$eps"']},{"metadata":{"name":"kaname-internal-abc","labels":{"kubernetes.io/service-name":"kaname-internal"}},"endpoints":['"$eps"']}]}' ;;
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
	route *fakeRoute
}

// fakeRoute — подставной вход консоли (kacho#3152): отвечает на адрес посева
// `/iam/v1/auth/csrf?form=login` так, как отвечает контроллер входа после
// переката службы доступа. Первые ответы — из seq (503 — страница контроллера
// «Service Temporarily Unavailable», 200 — признак формы), дальше — tail.
// Каждый ответ пишется в общий журнал строкой «ROUTE <путь> <код>»: порядок
// «край → маршрут» судится по нему же, что и вызовы kubectl.
type fakeRoute struct {
	mu   sync.Mutex
	log  string
	seq  []int
	tail int
	n    int
}

const nginx503 = "<html>\r\n<head><title>503 Service Temporarily Unavailable</title></head>\r\n<body>\r\n" +
	"<center><h1>503 Service Temporarily Unavailable</h1></center>\r\n<hr><center>nginx</center>\r\n</body>\r\n</html>\r\n"

func (f *fakeRoute) set(tail int, seq ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq, f.tail, f.n = seq, tail, 0
}

func (f *fakeRoute) asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

func (f *fakeRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	code := f.tail
	if f.n < len(f.seq) {
		code = f.seq[f.n]
	}
	f.n++
	f.mu.Unlock()
	if lf, err := os.OpenFile(f.log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintf(lf, "ROUTE %s %d\n", r.URL.RequestURI(), code)
		lf.Close()
	}
	if r.URL.RequestURI() != "/iam/v1/auth/csrf?form=login" {
		http.NotFound(w, r)
		return
	}
	switch code {
	case http.StatusOK:
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"csrfToken":"t-route"}`)
	case 299:
		// 200 без признака формы — так отвечала бы раздача консоли своей страницей.
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<!doctype html><title>console</title>")
	default:
		w.WriteHeader(code)
		fmt.Fprint(w, nginx503)
	}
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
	// Вход консоли: по умолчанию маршрут уже ведёт (200 с первого запроса).
	route := &fakeRoute{log: filepath.Join(dir, "log"), tail: http.StatusOK}
	rs := httptest.NewServer(route)
	t.Cleanup(rs.Close)
	return &wrapEnv{dir: dir, state: filepath.Join(dir, "state"), route: route,
		extra: []string{"FAKE_METRICS_PORT=" + port, "KACHO_CONSOLE_URL=" + rs.URL, "KACHO_WRAP_ROUTE_INTERVAL=0"}}
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
		"KACHO_WRAP_ENDPOINTS_INTERVAL=0",
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

// TestWrapRotate_EdgeIsAskedOnlyAfterTheServiceEndpointsAreReady — kacho#3145:
// после переката службы доступа `rollout status` уже «выкачено», а конечные
// точки её служб (Service) ещё не готовы — следующая проба получала 503 входа и
// connection refused адреса службы. Фикстура ждёт, пока готовые адреса КАЖДОЙ
// службы, выбирающей поды, станут ровно живыми готовыми подами (ни одного
// заменённого), STREAK опросов подряд, и только потом спрашивает край. Близнец:
// конечные точки догоняют за два опроса — проходит, и край спрошен после них;
// адрес заменённого пода держится — код 3 с его именем, край не спрошен;
// служб, выбирающих поды, нет — код 3.
func TestWrapRotate_EdgeIsAskedOnlyAfterTheServiceEndpointsAreReady(t *testing.T) {
	limits := []string{"KACHO_WRAP_ENDPOINTS_ATTEMPTS=4", "KACHO_WRAP_ENDPOINTS_INTERVAL=0", "KACHO_WRAP_ENDPOINTS_STREAK=2"}

	e := newWrapEnv(t, k1)
	r := e.run(t, append([]string{"FAKE_EP_LAG=2"}, limits...), "set", "K2,K1", "--state", e.state)
	if r.code != 0 {
		t.Fatalf("конечные точки догнали за два опроса — условие создано, код 0; получен %d:\n%s", r.code, r.out)
	}
	rollout := strings.Index(r.log, " rollout status ")
	firstEP := strings.Index(r.log, " get endpointslices ")
	lastEP := strings.LastIndex(r.log, " get endpointslices ")
	edge := strings.Index(r.log, " get --raw ")
	if rollout < 0 || firstEP < rollout || edge < lastEP || edge < 0 {
		t.Fatalf("порядок не «перекат → конечные точки → край»:\n%s", r.log)
	}
	if got := strings.Count(r.log, " get endpointslices "); got != 4 {
		t.Fatalf("ждалось 4 опроса конечных точек (2 не готовых + 2 подряд готовых), было %d:\n%s", got, r.log)
	}
	if !strings.Contains(r.out, "kaname-internal") {
		t.Fatalf("перепись служб не названа:\n%s", r.out)
	}
	_ = e.run(t, limits, "restore", "--state", e.state)

	e = newWrapEnv(t, k1)
	r = e.run(t, append([]string{"FAKE_EP=stale"}, limits...), "set", "K2,K1", "--state", e.state)
	if r.code != 3 || !strings.Contains(r.out, "kaname-x-old") {
		t.Fatalf("адрес заменённого пода держится — код 3 с его именем; получен %d:\n%s", r.code, r.out)
	}
	if strings.Contains(r.log, " get --raw ") {
		t.Fatalf("край спрошен при неготовых конечных точках:\n%s", r.log)
	}
	if got := strings.Count(r.log, " get endpointslices "); got != 4 {
		t.Fatalf("предел опросов 4, было %d", got)
	}

	e = newWrapEnv(t, k1)
	r = e.run(t, append([]string{"FAKE_EP=noservice"}, limits...), "restore", "--state", e.state)
	if r.code != 0 {
		t.Fatalf("restore без сохранённого ключа — восстанавливать нечего, код 0; получен %d", r.code)
	}
	r = e.run(t, append([]string{"FAKE_EP=noservice"}, limits...), "set", "K2", "--state", e.state)
	if r.code != 3 || !strings.Contains(r.out, "служб") {
		t.Fatalf("служб, выбирающих поды, ноль — не готовность, код 3; получен %d:\n%s", r.code, r.out)
	}
}

// TestWrapRotate_ReadyMeansTheSeedRouteAnswers — kacho#3152 (job 114526162992):
// конечные точки службы доступа готовы, `/readyz` края, спрошенный у пода
// напрямую, — 200, а первый запрос посева через вход консоли получал 503
// контроллера входа: маршрут «контроллер → служба края → под» ещё не вёл.
// Готовность — когда тот же адрес, что у посева (`$KACHO_CONSOLE_URL` +
// `/iam/v1/auth/csrf?form=login`), отвечает 200 с признаком формы STREAK раз
// подряд, и спрашивается он ПОСЛЕ края. Подставной ответ 503→200 — условие
// создано; 503 держится — код 3 с последним ответом; мерцание (503 между
// двумя 200) серию сбрасывает; 200 без признака формы — не готовность.
func TestWrapRotate_ReadyMeansTheSeedRouteAnswers(t *testing.T) {
	limits := []string{"KACHO_WRAP_ROUTE_ATTEMPTS=6", "KACHO_WRAP_ROUTE_STREAK=2"}

	// 503 → 200: условие создано, маршрут спрошен после края, 2 + 2 запроса.
	e := newWrapEnv(t, k1)
	e.route.set(http.StatusOK, 503, 503)
	r := e.run(t, limits, "set", "K2,K1", "--state", e.state)
	if r.code != 0 {
		t.Fatalf("маршрут ответил 200 после двух 503 — условие создано, код 0; получен %d:\n%s", r.code, r.out)
	}
	if got := e.route.asked(); got != 4 {
		t.Fatalf("ждалось 4 запроса маршрута (2 × 503 + 2 подряд 200), было %d:\n%s", got, r.log)
	}
	edge := strings.LastIndex(r.log, " get --raw ")
	route := strings.Index(r.log, "ROUTE /iam/v1/auth/csrf?form=login ")
	if edge < 0 || route < edge {
		t.Fatalf("порядок не «край → маршрут посева»:\n%s", r.log)
	}
	if !strings.Contains(r.out, "маршрут") || !strings.Contains(r.out, "wrap-rotate: keys=2") {
		t.Fatalf("готовность маршрута не названа либо самоотчёт не напечатан:\n%s", r.out)
	}

	// Мерцание: 503, 200, 503, 200, 200 — серия сброшена, готово на пятом.
	e.route.set(http.StatusOK, 503, 200, 503)
	r = e.run(t, limits, "set", "K2", "--state", e.state)
	if r.code != 0 || e.route.asked() != 5 {
		t.Fatalf("мерцание сбрасывает серию: ждались код 0 и 5 запросов, получены %d и %d:\n%s", r.code, e.route.asked(), r.out)
	}

	// Восстановление ключа стенда ждёт того же маршрута: следующая проба идёт за ним.
	e.route.set(http.StatusOK, 503)
	r = e.run(t, limits, "restore", "--state", e.state)
	if r.code != 0 || e.route.asked() != 3 || e.secret(t) != k1 {
		t.Fatalf("restore ждёт маршрута: код %d, запросов %d (ждалось 3), секрет восстановлен %v:\n%s",
			r.code, e.route.asked(), e.secret(t) == k1, r.out)
	}

	// 503 держится — код 3, последний ответ назван, попыток ровно предел.
	e = newWrapEnv(t, k1)
	e.route.set(503)
	r = e.run(t, limits, "set", "K2,K1", "--state", e.state)
	if r.code != 3 || !strings.Contains(r.out, "503") || !strings.Contains(r.out, "Service Temporarily Unavailable") {
		t.Fatalf("маршрут не повёл — код 3 с последним ответом 503; получен %d:\n%s", r.code, r.out)
	}
	if got := e.route.asked(); got != 6 {
		t.Fatalf("предел запросов маршрута 6, было %d", got)
	}
	if strings.Contains(r.out, "wrap-rotate: keys=") {
		t.Fatalf("самоотчёт напечатан при неготовом маршруте:\n%s", r.out)
	}
	_ = e.run(t, nil, "restore", "--state", e.state)

	// 200 без признака формы (страница раздачи) — не готовность.
	e = newWrapEnv(t, k1)
	e.route.set(299)
	r = e.run(t, limits, "set", "K2,K1", "--state", e.state)
	if r.code != 3 || !strings.Contains(r.out, "csrfToken") {
		t.Fatalf("200 без признака формы — не готовность, код 3 с именем csrfToken; получен %d:\n%s", r.code, r.out)
	}
	_ = e.run(t, nil, "restore", "--state", e.state)
}

// TestWrapRotate_RouteAddressIsNamedNotGuessed — адрес маршрута — тот, что у
// посева (KACHO_CONSOLE_URL); не назван — код 2 до первой записи, секрет не
// тронут. Близнец — тот же вызов с адресом — код 0.
func TestWrapRotate_RouteAddressIsNamedNotGuessed(t *testing.T) {
	e := newWrapEnv(t, k1)
	r := e.run(t, []string{"KACHO_CONSOLE_URL="}, "set", "K2,K1", "--state", e.state)
	if r.code != 2 || !strings.Contains(r.out, "KACHO_CONSOLE_URL") {
		t.Fatalf("адрес маршрута не назван — код 2 с именем KACHO_CONSOLE_URL; получен %d:\n%s", r.code, r.out)
	}
	if e.secret(t) != k1 || strings.Contains(r.log, "patch secret") || strings.Contains(r.log, "rollout restart") {
		t.Fatalf("отказ до первой записи тронул стенд:\n%s", r.log)
	}
	r = e.run(t, []string{"KACHO_CONSOLE_URL=console.kacho.local"}, "set", "K2,K1", "--state", e.state)
	if r.code != 2 || e.secret(t) != k1 {
		t.Fatalf("адрес без схемы — код 2; получен %d:\n%s", r.code, r.out)
	}
	r = e.run(t, nil, "set", "K2,K1", "--state", e.state)
	if r.code != 0 {
		t.Fatalf("близнец с адресом — код 0; получен %d:\n%s", r.code, r.out)
	}
	_ = e.run(t, nil, "restore", "--state", e.state)
}

// TestWrapRotate_RouteIsAskedLikeTheSeedAsksIt — вход стенда по TLS под своим
// именем: имя разрешается отображением KACHO_CONSOLE_HOST_IP, доверие — ровно
// ключу листа KACHO_CONSOLE_CA, как у браузера проб. Свой лист — 200; чужой
// лист по тому же адресу — не готовность (код 3), а не молчаливое доверие.
func TestWrapRotate_RouteIsAskedLikeTheSeedAsksIt(t *testing.T) {
	e := newWrapEnv(t, k1)
	srv := httptest.NewTLSServer(e.route)
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	leaf := filepath.Join(e.dir, "leaf.pem")
	writeLeaf(t, leaf, srv.TLS.Certificates[0])
	// Чужой лист — свой ключ: тестовые серверы httptest делят один встроенный
	// сертификат, и «другой сервер» дал бы тот же ключ.
	foreign := filepath.Join(e.dir, "foreign.pem")
	writeLeaf(t, foreign, freshLeaf(t))

	stand := []string{"KACHO_CONSOLE_URL=https://console.kacho.test:" + port, "KACHO_CONSOLE_HOST_IP=127.0.0.1",
		"KACHO_WRAP_ROUTE_ATTEMPTS=3", "KACHO_WRAP_ROUTE_STREAK=1"}
	r := e.run(t, append(stand, "KACHO_CONSOLE_CA="+leaf), "set", "K2,K1", "--state", e.state)
	if r.code != 0 || !strings.Contains(r.log, "ROUTE /iam/v1/auth/csrf?form=login 200") {
		t.Fatalf("маршрут по TLS под своим именем и своим листом — код 0; получен %d:\n%s\n%s", r.code, r.out, r.log)
	}
	r = e.run(t, append(stand, "KACHO_CONSOLE_CA="+foreign), "set", "K2", "--state", e.state)
	if r.code != 3 {
		t.Fatalf("лист стенда не тот — маршрут не готов, код 3; получен %d:\n%s", r.code, r.out)
	}
	r = e.run(t, append(stand, "KACHO_CONSOLE_CA="+filepath.Join(e.dir, "absent.pem")), "set", "K2", "--state", e.state)
	if r.code != 2 || !strings.Contains(r.out, "KACHO_CONSOLE_CA") {
		t.Fatalf("лист стенда не читается — код 2 с именем KACHO_CONSOLE_CA; получен %d:\n%s", r.code, r.out)
	}
	_ = e.run(t, append(stand, "KACHO_CONSOLE_CA="+leaf), "restore", "--state", e.state)
}

func freshLeaf(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "console.kacho.test"},
		DNSNames: []string{"console.kacho.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}}
}

func writeLeaf(t *testing.T, path string, c tls.Certificate) {
	t.Helper()
	b := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate[0]})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
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

// podEnv — окружение pod раннера своего пространства (kacho#3346): учётка pod
// называет пространство стенда, каталог просьб машине есть, файла конфигурации
// kubectl нет. machine — окружение шага машины: названный контекст -client.
func (e *wrapEnv) podEnv(t *testing.T, ns string) (pod []string, cond string) {
	t.Helper()
	cond = filepath.Join(e.dir, "cond")
	if err := os.MkdirAll(cond, 0o755); err != nil {
		t.Fatal(err)
	}
	nsf := filepath.Join(e.dir, "sa-namespace")
	if err := os.WriteFile(nsf, []byte(ns), 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{"KUBERNETES_SERVICE_HOST=10.0.0.1", "KACHO_STAND_COND_DIR=" + cond,
		"KACHO_WRAP_POD_NS_FILE=" + nsf, "KACHO_STAND_NS=" + ns, "HOME=" + t.TempDir()}, cond
}

// serveMachine — машина стенда (цикл runner_run stand-ns.sh): каждую просьбу pod
// «<id>.req» исполняет этим же скриптом в окружении машины и кладёт «<id>.out»,
// «<id>.rc». Возвращает остановку.
func (e *wrapEnv) serveMachine(t *testing.T, cond, ns string) func() {
	t.Helper()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
			}
			reqs, _ := filepath.Glob(filepath.Join(cond, "*.req"))
			for _, req := range reqs {
				b, err := os.ReadFile(req)
				if err != nil {
					continue
				}
				id := strings.TrimSuffix(req, ".req")
				_ = os.Rename(req, id+".taken")
				f := strings.Split(strings.TrimRight(string(b), "\n"), "\t")
				cmd := exec.Command("bash", append([]string{"scripts/stand-second-factor-wrap-rotate.sh"}, f[1:]...)...)
				cmd.Env = append([]string{
					"PATH=" + filepath.Join(e.dir, "bin") + ":" + os.Getenv("PATH"),
					"HOME=" + os.Getenv("HOME"),
					"FAKE_DIR=" + e.dir,
					"FAKE_CURRENT=u/x-infra",
					"KACHO_WRAP_ENDPOINTS_INTERVAL=0",
					"KACHO_CLIENT_CONTEXT=u/x-client",
					"KACHO_STAND_NS=" + ns,
					"KACHO_STAND_COND_STATE=" + filepath.Join(e.dir, "machine-state"),
				}, e.extra...)
				out, err := cmd.CombinedOutput()
				code := 0
				if ee, ok := err.(*exec.ExitError); ok {
					code = ee.ExitCode()
				} else if err != nil {
					code = 125
				}
				_ = os.WriteFile(id+".out", out, 0o600)
				_ = os.WriteFile(id+".rc", []byte(strconv.Itoa(code)+"\n"), 0o600)
			}
		}
	}()
	return func() { close(stop); <-done }
}

// TestWrapRotate_InRunnerPodTheWriteIsAMachineStep — kacho#3346, kacho#3348: в
// pod раннера учётка только читает, поэтому запись условия (секрет, перекат) —
// просьба машине, а исход (край, маршрут, самоотчёт старта) pod судит сам. Ни
// одной записи из pod: patch и rollout restart идут лишь контекстом -client машины.
// Инъекция — шаг машины убран (просьбу никто не взял): код 3 «условие не
// создано», секрет не тронут, а не 0 и не зелёное.
func TestWrapRotate_InRunnerPodTheWriteIsAMachineStep(t *testing.T) {
	const ns = "t3038-wrap"

	// Инъекция: машины нет.
	e0 := newWrapEnv(t, k1)
	pod0, cond0 := e0.podEnv(t, ns)
	r := e0.run(t, append(pod0, "KACHO_WRAP_MACHINE_WAIT=2"), "set", "K2,K1", "--state", e0.state)
	if r.code != 3 || !strings.Contains(r.out, "условие не создано") || !strings.Contains(r.out, "шаг машины") {
		t.Fatalf("шаг машины убран: ждался код 3 «условие не создано: шаг машины…», получен %d:\n%s", r.code, r.out)
	}
	if e0.secret(t) != k1 || strings.Contains(r.log, " patch ") || strings.Contains(r.log, "rollout restart") {
		t.Fatalf("без шага машины pod записал сам: секрет %q, журнал:\n%s", e0.secret(t), r.log)
	}
	if left, _ := filepath.Glob(filepath.Join(cond0, "*.req")); len(left) != 0 {
		t.Fatalf("невзятая просьба осталась лежать: %v", left)
	}

	// Близнец: машина служит — тот же вызов создаёт условие.
	e := newWrapEnv(t, k1)
	pod, cond := e.podEnv(t, ns)
	stop := e.serveMachine(t, cond, ns)
	defer stop()
	r = e.run(t, pod, "set", "K2,K1", "--state", e.state)
	if r.code != 0 || !strings.Contains(r.out, "keys=2") {
		t.Fatalf("pod раннера с машиной: set K2,K1 — код %d:\n%s", r.code, r.out)
	}
	parts := strings.Split(e.secret(t), ",")
	if len(parts) != 2 || parts[1] != k1 || !hex64.MatchString(parts[0]) {
		t.Fatalf("K2,K1 через шаг машины: секрет %q", e.secret(t))
	}
	k2 := parts[0]
	r = e.run(t, pod, "set", "K2", "--state", e.state)
	if r.code != 0 || e.secret(t) != k2 || !strings.Contains(r.out, "keys=1") {
		t.Fatalf("pod раннера: set K2 — код %d, секрет %q:\n%s", r.code, e.secret(t), r.out)
	}
	r = e.run(t, pod, "restore", "--state", e.state)
	if r.code != 0 || e.secret(t) != k1 {
		t.Fatalf("pod раннера: restore — код %d, секрет %q:\n%s", r.code, e.secret(t), r.out)
	}
	r = e.run(t, pod, "restore", "--state", e.state)
	if r.code != 0 || e.secret(t) != k1 || !strings.Contains(r.out, "восстанавливать нечего") {
		t.Fatalf("pod раннера: повторный restore — код %d, секрет %q:\n%s", r.code, e.secret(t), r.out)
	}
	for _, l := range strings.Split(r.log, "\n") {
		if (strings.Contains(l, " patch ") || strings.Contains(l, "rollout restart")) && !strings.Contains(l, "--context u/x-client") {
			t.Errorf("запись не шагом машины (без контекста -client): %s", l)
		}
	}
	if strings.Contains(r.out, k1) || strings.Contains(r.out, k2) {
		t.Fatalf("материал ключа напечатан в выводе pod")
	}

	// Шаг машины — только на машине и только своим пространством.
	e2 := newWrapEnv(t, k1)
	pod2, _ := e2.podEnv(t, ns)
	r = e2.run(t, append(pod2, "KACHO_STAND_COND_STATE="+filepath.Join(e2.dir, "ms")), "write", "set", "K2,K1", "key1")
	if r.code != 2 || e2.secret(t) != k1 {
		t.Fatalf("write в pod раннера: ждался отказ 2, получен %d:\n%s", r.code, r.out)
	}
	mach := []string{"KACHO_CLIENT_CONTEXT=u/x-client", "KACHO_STAND_NS=" + ns, "KACHO_STAND_COND_STATE=" + filepath.Join(e2.dir, "ms")}
	for _, args := range [][]string{
		{"write", "set", "K2,K1", "key1", "--ns", "t1-other"},
		{"write", "set", "K2,K1", "../up"},
		{"write", "drop", "key1"},
	} {
		r = e2.run(t, mach, args...)
		if r.code != 2 || e2.secret(t) != k1 {
			t.Errorf("write %v: ждался отказ 2, получен %d:\n%s", args, r.code, r.out)
		}
	}
}
