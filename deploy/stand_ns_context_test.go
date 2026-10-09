// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_ns_context_test.go — стенд проб говорит с контекстом -client явно
// названного файла профиля, а не с активным контекстом файла (kacho#3065).
//
// Файл профиля площадки несёт два контекста одного кластера — `…-client` и
// `…-infra`, — и его current-context переключает человек. Прежний страж брал
// активный контекст окружающего KUBECONFIG и пинил кластер адресом: при
// current-context=infra стенд уехал бы в infra. Проба судит ИСХОД — какие
// аргументы и какое окружение получил kubectl, — подставным kubectl и helm,
// которые пишут каждый вызов в журнал; кластер не нужен.
package deploy

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Подставной kubectl: пишет аргументы, KUBECONFIG и первую строку current-context
// из первого файла KUBECONFIG (что увидит дочерний шаг, читающий активный
// контекст), и отвечает на ровно те вопросы, что задают census и down.
const fakeStandKubectl = `#!/usr/bin/env bash
{
  printf 'CALL %s\n' "$*"
  printf 'ENV %s\n' "${KUBECONFIG:-}"
  first="${KUBECONFIG%%:*}"
  [ -n "$first" ] && [ -f "$first" ] && printf 'AMBIENT %s\n' "$(grep '^current-context:' "$first")"
} >>"$FAKE_LOG"
case " $* " in
  *" config view "*) echo -n "https://cluster.invalid:6443" ;;
  *" get nodes "*)
    n=$(( $(cat "$FAKE_LOG.nodes" 2>/dev/null || echo 0) + 1 )); echo "$n" >"$FAKE_LOG.nodes"
    [ -n "${FAKE_NODES_FAIL:-}" ] && { echo "Unable to connect to the server" >&2; exit 1; }
    set -- ${FAKE_NODES-}
    [ -n "${FAKE_NODES_SWITCH_AFTER:-}" ] && [ "$n" -gt "$FAKE_NODES_SWITCH_AFTER" ] && set -- ${FAKE_NODES_SWITCHED-}
    for v in "$@"; do echo "$v"; done ;;
  *" config current-context "*) sed -n 's/^current-context: "\(.*\)"$/\1/p' "${KUBECONFIG%%:*}" ;;
  *" get namespace -l "*|*" get clusterissuer "*"-o json"*|*" get deploy -A "*) echo '{"items":[]}' ;;
  *" get namespace "*) echo "Error from server (NotFound): namespaces not found" >&2; exit 1 ;;
esac
exit 0
`

const fakeStandHelm = `#!/usr/bin/env bash
printf 'CALL helm %s\n' "$*" >>"$FAKE_LOG"
exit 1
`

const (
	standClient = "u/x-client"
	standInfra  = "u/x-infra"
	// Узлы client площадки x: префикс x-client-, хвост — пул.
	standNodes = "x-client-p1a2b3-wn7jm-aaaaa x-client-p1a2b3-wn7jm-bbbbb"
)

func standProfile(t *testing.T, dir, name, current string, contexts ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Config\nclusters:\n- name: c\n  cluster: {server: \"https://cluster.invalid:6443\"}\nusers:\n- name: u\n  user: {token: x}\ncontexts:\n")
	for _, c := range contexts {
		b.WriteString("- name: " + c + "\n  context: {cluster: c, user: u}\n")
	}
	b.WriteString("current-context: " + current + "\n")
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

type standRun struct {
	out  string
	code int
	log  string
}

func runStandNS(t *testing.T, env []string, args ...string) standRun {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"kubectl": fakeStandKubectl, "helm": fakeStandHelm} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	work := filepath.Join(dir, "work")
	logf := filepath.Join(dir, "calls.log")
	cmd := exec.Command("bash", append([]string{"scripts/stand-ns.sh"}, args...)...)
	// Умолчание — кластер профиля x: его узлы x-client-…, профиль назван
	// переменной (файлы проб названы не по площадке). Случай переопределяет.
	cmd.Env = append([]string{
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"FAKE_LOG=" + logf,
		"KACHO_STAND_NS_WORKDIR=" + work,
		"STAND_PROFILE=x",
		"FAKE_NODES=" + standNodes,
	}, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("stand-ns.sh не запустился: %v", err)
	}
	logb, _ := os.ReadFile(logf)
	// Файл выбора контекста — рабочий файл вызова и уходит вместе с ним.
	if left, _ := filepath.Glob(filepath.Join(work, ".context.*")); len(left) > 0 {
		t.Errorf("после вызова остались файлы выбора контекста: %v", left)
	}
	return standRun{string(out), code, string(logb)}
}

var configRead = regexp.MustCompile(`^CALL (--kubeconfig \S+ --context \S+ )?config `)

// callLines — обращения к кластеру: чтение файла конфигурации (`kubectl config …`)
// кластер не спрашивает и сюда не входит.
func callLines(log string) []string {
	var calls []string
	for _, l := range strings.Split(log, "\n") {
		if strings.HasPrefix(l, "CALL ") && !configRead.MatchString(l) {
			calls = append(calls, l)
		}
	}
	return calls
}

// current-context файла — infra; операции census и down идут в client: каждый
// вызов kubectl несёт --context client и файл профиля, ни один не называет infra,
// а дочерний шаг, читающий активный контекст, видит client. Файл профиля не
// меняется. Законный близнец — KUBECONFIG вместо STAND_KUBECONFIG: тот же исход.
func TestStandNamespaceTalksToTheClientContextWhateverIsActive(t *testing.T) {
	for _, op := range [][]string{{"census"}, {"down", "t3065-ctx"}} {
		for _, via := range []string{"STAND_KUBECONFIG", "KUBECONFIG"} {
			pdir := t.TempDir()
			prof := standProfile(t, pdir, "profile.yaml", standInfra, standClient, standInfra)
			before, _ := os.ReadFile(prof)
			r := runStandNS(t, []string{via + "=" + prof}, op...)
			name := strings.Join(op, " ") + " через " + via
			if r.code != 0 {
				t.Fatalf("%s: ждали код 0, получили %d:\n%s", name, r.code, r.out)
			}
			calls := callLines(r.log)
			if len(calls) == 0 {
				t.Fatalf("%s: ни одного вызова kubectl — проба беспредметна:\n%s", name, r.out)
			}
			// Чтение файла конфигурации дочерним стражем (stand-cluster-pin.sh)
			// идёт без --context — оно читает АКТИВНЫЙ контекст, и что он client,
			// утверждает строка AMBIENT ниже; в calls его нет.
			for _, c := range calls {
				if !strings.Contains(c, "--context "+standClient+" ") || !strings.Contains(c, "--kubeconfig "+prof+" ") {
					t.Errorf("%s: вызов без явного контекста client и файла профиля: %s", name, c)
				}
				if strings.Contains(c, standInfra) {
					t.Errorf("%s: вызов называет infra: %s", name, c)
				}
			}
			if !strings.Contains(r.log, "AMBIENT current-context: \""+standClient+"\"") ||
				strings.Contains(r.log, "AMBIENT current-context: \""+standInfra+"\"") {
				t.Errorf("%s: дочерний шаг увидел бы активным не client:\n%s", name, r.log)
			}
			if !strings.Contains(r.log, "ENV ") || !strings.Contains(r.log, ":"+prof+"\n") {
				t.Errorf("%s: KUBECONFIG дочерних шагов не оканчивается файлом профиля:\n%s", name, r.log)
			}
			if after, _ := os.ReadFile(prof); !bytes.Equal(before, after) {
				t.Errorf("%s: файл профиля изменён", name)
			}
		}
	}
}

// Отказы — ДО первого обращения к кластеру (журнал kubectl пуст), кодом 2 и с
// шагом «что сделать». Близнец каждого — профиль с одним client (тест выше).
func TestStandNamespaceRefusesWithoutExactlyOneClientContext(t *testing.T) {
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
		{"STAND_CONTEXT — client, которого в файле нет", []string{"STAND_KUBECONFIG=" + good, "STAND_CONTEXT=u/z-client"}, "нет контекста STAND_CONTEXT"},
		{"файл профиля не назван", nil, "файл профиля не назван"},
		{"STAND_APISERVER не адрес контекста client", []string{"STAND_KUBECONFIG=" + good, "STAND_APISERVER=https://other.invalid:6443"}, "не адрес контекста -client"},
		{"KUBECONFIG — список", []string{"KUBECONFIG=" + good + ":" + onlyInfra}, "файл профиля должен быть один"},
	} {
		for _, op := range [][]string{{"up", "t3065-ctx", "a8f60d"}, {"down", "t3065-ctx"}, {"census"}} {
			r := runStandNS(t, c.env, op...)
			name := c.name + " / " + op[0]
			if r.code != 2 || !strings.Contains(r.out, c.want) {
				t.Errorf("%s: ждали код 2 и «%s», получили %d:\n%s", name, c.want, r.code, r.out)
			}
			if c.want == "нужен ровно один" && !strings.Contains(r.out, "Что сделать") {
				t.Errorf("%s: отказ без шага «что сделать»:\n%s", name, r.out)
			}
			if calls := callLines(r.log); len(calls) != 0 {
				t.Errorf("%s: до отказа было обращение к кластеру: %v", name, calls)
			}
		}
	}
}
