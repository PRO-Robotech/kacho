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
