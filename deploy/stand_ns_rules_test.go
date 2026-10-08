// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_ns_rules_test.go — правила стенда проб в своём пространстве имён
// (scripts/stand-ns.sh, kacho#3102), которые судятся без кластера: имя
// `t<задача>-<коротко>` и отказ на `kacho`, устойчивый порт проброса, чтение срока
// (истёк · жив · не читается — три исхода, а не два).
package deploy

import (
	"os/exec"
	"strings"
	"testing"
)

func TestStandNamespaceRulesHoldWithoutACluster(t *testing.T) {
	out, err := exec.Command("bash", "scripts/stand-ns.sh", "self-test").CombinedOutput()
	if err != nil {
		t.Fatalf("самопроверка stand-ns отказала: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "провалено 0") || !strings.Contains(string(out), "«kacho» отвергнут") {
		t.Fatalf("самопроверка не напечатала переписи или не судила имя рабочего стенда:\n%s", out)
	}
}

// Близнец: имя рабочего стенда отвергается ДО обращения к кластеру — без
// kubectl, без STAND_APISERVER, кодом входа 2.
func TestStandNamespaceDownRefusesTheWorkingStandBeforeTouchingTheCluster(t *testing.T) {
	cmd := exec.Command("bash", "scripts/stand-ns.sh", "down", "kacho")
	cmd.Env = []string{"PATH=/nonexistent"}
	out, err := cmd.CombinedOutput()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 2 || !strings.Contains(string(out), "kacho") {
		t.Fatalf("down kacho: ждали отказ кодом 2 до кластера, получили %v:\n%s", err, out)
	}
}

// Прогон (`run`) и подъём отказывают ДО кластера на тех же правилах: имя рабочего
// стенда, непоименованная команда, срок вне 1..12 ч. Законный близнец каждого —
// t3102-probe с командой и сроком 12, он доходит до кластера (здесь — до
// отсутствующего kubectl, код 2 с другим текстом).
func TestStandNamespaceRunAndUpRefuseBeforeTheCluster(t *testing.T) {
	for _, c := range []struct {
		name string
		env  []string
		args []string
		want string
	}{
		{"run kacho", nil, []string{"run", "kacho", "--", "true"}, "kacho"},
		{"run без команды", nil, []string{"run", "t3102-probe"}, "команда не названа"},
		{"run срок 13", []string{"STAND_TTL_HOURS=13"}, []string{"run", "t3102-probe", "--", "true"}, "вне 1..12"},
		{"up срок 0", []string{"STAND_TTL_HOURS=0"}, []string{"up", "t3102-probe"}, "вне 1..12"},
		{"законный run", []string{"STAND_TTL_HOURS=12"}, []string{"run", "t3102-probe", "--", "true"}, "нет 'kubectl'"},
	} {
		cmd := exec.Command("bash", append([]string{"scripts/stand-ns.sh"}, c.args...)...)
		cmd.Env = append([]string{"PATH=/nonexistent"}, c.env...)
		out, err := cmd.CombinedOutput()
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 2 || !strings.Contains(string(out), c.want) {
			t.Errorf("%s: ждали код 2 и «%s», получили %v:\n%s", c.name, c.want, err, out)
		}
	}
}
