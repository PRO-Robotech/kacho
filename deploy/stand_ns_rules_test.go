// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_ns_rules_test.go — правила стенда проб в своём пространстве имён
// (scripts/stand-ns.sh, kacho#3102), которые судятся без кластера: имя
// `t<задача>-<коротко>` и отказ на `kacho`, устойчивый порт проброса, чтение срока
// (истёк · жив · не читается — три исхода, а не два).
package deploy

import (
	"os/exec"
	"regexp"
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

// Цель make кода скрипта наружу не передаёт (GNU make на любом ненулевом коде
// рецепта выходит 2), поэтому код `stand-ns.sh run` печатается ПОСЛЕДНЕЙ строкой
// рецепта. Проба — без кластера: имя рабочего стенда скрипт отвергает до kubectl
// кодом 2, и строка обязана назвать именно его код. Близнец: без NS отказывает сам
// make до скрипта — строки кода скрипта там нет, потому что скрипт не звался.
func TestStandNamespaceRunTargetPrintsTheScriptCodeLast(t *testing.T) {
	const line = "stand-ns-run: код scripts/stand-ns.sh run = "
	run := func(level string, args ...string) (string, int) {
		cmd := exec.Command("make", append([]string{"-s", "--no-print-directory", "stand-ns-run"}, args...)...)
		cmd.Env = append(cmd.Environ(), "STAND_APISERVER=", "KUBECONFIG=/nonexistent", "MAKELEVEL="+level)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("make не запустился: %v", err)
		}
		return string(out), code
	}

	// Прямой запуск и вложенный: в конвейере цель зовётся из другого make
	// (MAKELEVEL ≥ 1), и сам make печатает свой отказ как «make[1]: *** …».
	// Это строка make, а не рецепта, — предмет пробы среди строк рецепта.
	for _, level := range []string{"", "1"} {
		out, code := run(level, "NS=kacho", "CMD=exit 7")
		last := lastRecipeLine(out)
		if code != 2 || !strings.HasPrefix(last, line+"2 ") {
			t.Fatalf("MAKELEVEL=%q NS=kacho: ждали код make 2 и последней строкой рецепта «%s2 …», получили %d, «%s»:\n%s", level, line, code, last, out)
		}

		out, code = run(level, "CMD=exit 7")
		if code != 2 || strings.Contains(out, line) || !strings.Contains(out, "NS не задан") {
			t.Fatalf("MAKELEVEL=%q без NS: ждали отказ make до скрипта без строки кода, получили %d:\n%s", level, code, out)
		}
	}
}

// makeOwnLine — строка, которую печатает сам make, а не рецепт: «make: *** …»
// на верхнем уровне и «make[N]: *** …» при вложенном запуске.
var makeOwnLine = regexp.MustCompile(`^make(\[[0-9]+\])?: \*\*\* `)

// lastRecipeLine — последняя строка вывода, напечатанная рецептом.
func lastRecipeLine(out string) string {
	var last string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if !makeOwnLine.MatchString(l) {
			last = l
		}
	}
	return last
}

// Близнец фильтра: строки make обоих уровней отсеиваются, строка рецепта,
// похожая на них лишь началом, — нет. Без него фильтр, отсеивающий всё, сделал
// бы пробу выше пустой.
func TestStandNamespaceRunTargetFilterDropsOnlyMakeOwnLines(t *testing.T) {
	const rec = "stand-ns-run: код scripts/stand-ns.sh run = 2 (команда завершилась ненулевым кодом)"
	for _, c := range []struct{ out, want string }{
		{rec + "\nmake: *** [Makefile:1143: stand-ns-run] Error 2", rec},
		{rec + "\nmake[1]: *** [Makefile:1143: stand-ns-run] Error 2", rec},
		{rec + "\nmake[12]: *** [Makefile:1143: stand-ns-run] Error 2\nmake: *** [Makefile:9: ci] Error 2", rec},
		{"make[1]: *** x\nmake[1]: ***не-make", "make[1]: ***не-make"},
		{rec + "\nmakefile: *** рецепт", "makefile: *** рецепт"},
	} {
		if got := lastRecipeLine(c.out); got != c.want {
			t.Errorf("lastRecipeLine(%q) = %q, ждали %q", c.out, got, c.want)
		}
	}
}
