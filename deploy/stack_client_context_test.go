// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stack_client_context_test.go — `stack-up` стенда, который ставится только в
// client, говорит с контекстом -client файла профиля, а не с активным
// контекстом файла (kacho#3065, решение владельца 2026-10-09).
//
// Прежде `make stack-up STACK=a8f60d` шёл в активный контекст через
// guard-destructive: при current-context=infra выкатка уехала бы в infra.
// Проба судит ИСХОД — какие аргументы получили kubectl и helm и какой контекст
// подтвердил страж, — подставными kubectl и helm, которые пишут каждый вызов в
// журнал; кластер не нужен. Подставной helm отказывает, поэтому цель
// останавливается на первом же helm, до любой записи в кластер.
package deploy

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Подставной kubectl: активный контекст — из первого файла KUBECONFIG,
// объявившего current-context (как сливает kubeconfig сам kubectl); адрес —
// один на весь файл.
const fakeStackKubectl = `#!/usr/bin/env bash
printf 'CALL kubectl %s\n' "$*" >>"$FAKE_LOG"
case " $* " in
  *" get nodes "*)
    n=$(( $(cat "$FAKE_LOG.nodes" 2>/dev/null || echo 0) + 1 )); echo "$n" >"$FAKE_LOG.nodes"
    [ -n "${FAKE_NODES_FAIL:-}" ] && { echo "Unable to connect to the server" >&2; exit 1; }
    set -- ${FAKE_NODES-}
    [ -n "${FAKE_NODES_SWITCH_AFTER:-}" ] && [ "$n" -gt "$FAKE_NODES_SWITCH_AFTER" ] && set -- ${FAKE_NODES_SWITCHED-}
    for v in "$@"; do echo "$v"; done ;;
  *" config current-context "*)
    IFS=: read -ra files <<<"${KUBECONFIG:-}"
    for f in "${files[@]}"; do
      v="$(sed -n 's/^current-context: *"\{0,1\}\([^"]*\)"\{0,1\} *$/\1/p' "$f" 2>/dev/null)"
      [ -n "$v" ] && { echo "$v"; exit 0; }
    done
    exit 1 ;;
  *" config view "*"context.cluster"*) echo -n "c" ;;
  *" config view "*"cluster.server"*) echo -n "https://cluster.invalid:6443" ;;
  *" config view "*) echo -n "" ;;
esac
exit 0
`

// Узлы client площадки a8f60d (имя стенда — профиль площадки).
const stackNodes = "a8f60d-client-p1a2b3-wn7jm-aaaaa a8f60d-client-p1a2b3-wn7jm-bbbbb"

const fakeStackHelm = `#!/usr/bin/env bash
printf 'CALL helm %s\n' "$*" >>"$FAKE_LOG"
exit 1
`

type stackRun struct {
	out  string
	code int
	log  string
}

func runStackUp(t *testing.T, env []string, args ...string) stackRun {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	tmp := filepath.Join(dir, "tmp")
	for _, d := range []string{bin, tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"kubectl": fakeStackKubectl, "helm": fakeStackHelm} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	logf := filepath.Join(dir, "calls.log")
	cmd := exec.Command("make", append([]string{"--no-print-directory", "stack-up"}, args...)...)
	cmd.Stdin = nil // не терминал: подтверждение — только ручкой CONFIRM
	cmd.Env = append([]string{
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GOPATH=" + os.Getenv("GOPATH"),
		"GOCACHE=" + os.Getenv("GOCACHE"),
		"TMPDIR=" + tmp,
		"FAKE_LOG=" + logf,
		"FAKE_NODES=" + stackNodes,
	}, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("make stack-up не запустился: %v", err)
	}
	logb, _ := os.ReadFile(logf)
	// Рабочий каталог закрепления (файл выбора и обёртки) уходит с вызовом.
	if left, _ := os.ReadDir(tmp); len(left) > 0 {
		var names []string
		for _, e := range left {
			names = append(names, e.Name())
		}
		t.Errorf("после вызова во временном каталоге осталось: %v", names)
	}
	return stackRun{string(out), code, string(logb)}
}

// clusterCalls — обращения, которые идут к кластеру: всё, кроме чтения файла
// конфигурации (`kubectl config …`).
func clusterCalls(log string) []string {
	var calls []string
	for _, l := range strings.Split(log, "\n") {
		if strings.HasPrefix(l, "CALL ") && !strings.HasPrefix(l, "CALL kubectl config ") {
			calls = append(calls, l)
		}
	}
	return calls
}

// current-context файла — infra. Каждый вызов helm и kubectl к кластеру несёт
// client явно и ни один не называет infra; guard-destructive подтверждён
// контекстом client (то есть и шаги, читающие активный контекст, видят client);
// файл профиля не изменён. Законный близнец — KUBECONFIG вместо
// STAND_KUBECONFIG: тот же исход.
func TestStackUpTalksToTheClientContextWhateverIsActive(t *testing.T) {
	for _, via := range []string{"STAND_KUBECONFIG", "KUBECONFIG"} {
		pdir := t.TempDir()
		prof := standProfile(t, pdir, "profile.yaml", standInfra, standClient, standInfra)
		before, _ := os.ReadFile(prof)
		r := runStackUp(t, []string{via + "=" + prof}, "STACK=a8f60d", "CONFIRM=UP-a8f60d@"+standClient)
		name := "stack-up через " + via
		if !strings.Contains(r.out, "подтверждено ручкой CONFIRM (без терминала): UP-a8f60d@"+standClient) {
			t.Errorf("%s: страж не подтвердил операцию контекстом client:\n%s", name, r.out)
		}
		calls := clusterCalls(r.log)
		if len(calls) == 0 {
			t.Fatalf("%s: ни одного обращения к кластеру — проба беспредметна (код %d):\n%s", name, r.code, r.out)
		}
		for _, c := range calls {
			want := "--context " + standClient + " "
			if strings.HasPrefix(c, "CALL helm ") {
				want = "--kube-context " + standClient + " "
			}
			if !strings.Contains(c+" ", want) {
				t.Errorf("%s: вызов без явного контекста client: %s", name, c)
			}
			if strings.Contains(c, standInfra) {
				t.Errorf("%s: вызов называет infra: %s", name, c)
			}
		}
		if strings.Contains(r.out, "активен '"+standInfra+"'") || strings.Contains(r.out, "контекст   "+standInfra) {
			t.Errorf("%s: шаг цели увидел активным infra:\n%s", name, r.out)
		}
		if after, _ := os.ReadFile(prof); !bytes.Equal(before, after) {
			t.Errorf("%s: файл профиля изменён", name)
		}
	}
}

// Отказы — ДО первого обращения к кластеру (в журнале нет ни одного вызова к
// кластеру), с названной причиной. Близнец каждого — профиль с одним client
// (тест выше). Сюда же — обход закрепления: KACHO_CLIENT_CONTEXT, выставленный
// рукой без обёрток, закреплением не считается.
func TestStackUpRefusesWithoutExactlyOneClientContext(t *testing.T) {
	pdir := t.TempDir()
	onlyInfra := standProfile(t, pdir, "infra.yaml", standInfra, standInfra)
	twoClients := standProfile(t, pdir, "two.yaml", standInfra, standClient, "u/y-client", standInfra)
	good := standProfile(t, pdir, "good.yaml", standInfra, standClient, standInfra)
	for _, c := range []struct {
		name string
		env  []string
		want string
	}{
		{"нет контекста -client", []string{"STAND_KUBECONFIG=" + onlyInfra}, "нужен ровно один"},
		{"два контекста -client", []string{"STAND_KUBECONFIG=" + twoClients}, "нужен ровно один"},
		{"STAND_CONTEXT — infra", []string{"STAND_KUBECONFIG=" + good, "STAND_CONTEXT=" + standInfra}, "не оканчивается на -client"},
		{"файл профиля не назван", nil, "файл профиля не назван"},
		{"KUBECONFIG — список", []string{"KUBECONFIG=" + good + ":" + onlyInfra}, "файл профиля должен быть один"},
		{"STAND_APISERVER не адрес контекста client", []string{"STAND_KUBECONFIG=" + good, "STAND_APISERVER=https://other.invalid:6443"}, "не адрес контекста -client"},
		{"закрепление выставлено рукой", []string{"KUBECONFIG=" + good, "KACHO_CLIENT_CONTEXT=" + standClient}, "не закреплённый client"},
	} {
		r := runStackUp(t, c.env, "STACK=a8f60d", "CONFIRM=UP-a8f60d@"+standClient)
		if r.code == 0 || !strings.Contains(r.out, c.want) {
			t.Errorf("%s: ждали отказ с «%s», получили код %d:\n%s", c.name, c.want, r.code, r.out)
		}
		if calls := clusterCalls(r.log); len(calls) != 0 {
			t.Errorf("%s: до отказа было обращение к кластеру: %v", c.name, calls)
		}
		if strings.Contains(r.out, "РАЗРУШИТЕЛЬНАЯ ОПЕРАЦИЯ") {
			t.Errorf("%s: дошли до guard-destructive — выбор контекста не стоит первым:\n%s", c.name, r.out)
		}
	}
}
