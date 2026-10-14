// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// umbrella_deps_step_spelled_once_test.go — ШАГ СБОРКИ ЗАВИСИМОСТЕЙ УМБРЕЛЛЫ
// ЗАПИСАН В deploy/Makefile ОДИН РАЗ, и три цели подъёма зовут его (kacho#2885).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Соседняя проба (umbrella_apply_runs_deps_first_test.go) судит ПОРЯДОК: цель,
// отдающая умбреллу helm'у, сначала собирает зависимости. Она принимает обе
// формы шага — прямой вызов скрипта и цель `helm-deps`, — и поэтому молчит о
// том, сколько раз шаг выписан. Выписан он был в каждой цели отдельно, и
// `stack-up` его однажды не получил вовсе (признак kacho#2885): копия, где шаг
// у одной цели правят, а у соседней нет, расходится молча.
//
// Здесь судится ЧИСЛО МЕСТ: вызов `scripts/helm-umbrella-deps.sh` в позиции
// команды стоит ровно в одном рецепте — цели `helm-deps`, — а `dev-up`,
// `own-up` и `stack-up` доходят до неё вызовом `$(MAKE) … helm-deps` раньше
// первого применения умбреллы. Остальные цели файла, которым шаг нужен, зовут
// ту же цель; прямой вызов в любом рецепте, кроме неё, — находка с координатой.
package deploy_test

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// depsOwnerTarget — единственная цель, чей рецепт зовёт скрипт.
const depsOwnerTarget = "helm-deps"

// upTargets — три цели подъёма стенда, которых касается задача.
var upTargets = []string{"dev-up", "own-up", "stack-up"}

var (
	// depsScriptCall — прямой вызов скрипта владельца в позиции команды.
	depsScriptCall = regexp.MustCompile(`(^|[;&|(]|\$\()[ \t@]*(bash[ \t]+)?\S*helm-umbrella-deps\.sh\b`)
	// depsTargetCall — вызов цели-владельца шага.
	depsTargetCall = regexp.MustCompile(`\$\(MAKE\)[^;]*[ \t]helm-deps([ \t;\\]|$)`)
)

type depsSpellingCensus struct {
	targets     int
	directCalls []string // цель → строка прямого вызова вне helm-deps
	ownerCalls  int      // прямых вызовов в рецепте helm-deps
	upFindings  []string // цель подъёма, не дошедшая до helm-deps раньше применения
}

func judgeDepsSpelling(makefile string) depsSpellingCensus {
	targets := parseRecipeTargets(makefile)
	names := make([]string, 0, len(targets))
	for n := range targets {
		names = append(names, n)
	}
	sort.Strings(names)
	var c depsSpellingCensus
	for _, n := range names {
		c.targets++
		for _, line := range targets[n].recipe {
			if !callSite(depsScriptCall, line) {
				continue
			}
			if n == depsOwnerTarget {
				c.ownerCalls++
				continue
			}
			c.directCalls = append(c.directCalls, fmt.Sprintf("%s: %s", n, strings.TrimSpace(line)))
		}
	}
	for _, n := range upTargets {
		if _, ok := targets[n]; !ok {
			c.upFindings = append(c.upFindings, n+": цели нет в Makefile")
			continue
		}
		reached := false
		applied := false
		for _, line := range expandRecipe(targets, n, map[string]bool{}) {
			if callSite(depsTargetCall, line) {
				reached = true
			}
			if callSite(umbrellaApply, line) {
				applied = true
				break
			}
		}
		switch {
		case !applied:
			c.upFindings = append(c.upFindings, n+": умбрелла не применяется — распознаватель разошёлся с рецептом")
		case !reached:
			c.upFindings = append(c.upFindings, n+": до первого применения умбреллы цель helm-deps не позвана")
		}
	}
	return c
}

func TestUmbrellaDepsStepIsSpelledOnceAndEveryUpTargetCallsIt(t *testing.T) {
	raw, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("deploy/Makefile не прочитан (%v) — судить нечего", err)
	}
	c := judgeDepsSpelling(string(raw))
	t.Logf("целей осмотрено: %d; прямых вызовов скрипта в %s: %d, вне него: %d; целей подъёма %d (%s), без шага: %d",
		c.targets, depsOwnerTarget, c.ownerCalls, len(c.directCalls), len(upTargets),
		strings.Join(upTargets, " "), len(c.upFindings))
	if c.targets == 0 {
		t.Fatal("обход не нашёл ни одной цели — распознаватель разошёлся с формой записи")
	}
	if c.ownerCalls != 1 {
		t.Errorf("в рецепте %s прямых вызовов скрипта %d, ждали ровно один — шагу некому принадлежать",
			depsOwnerTarget, c.ownerCalls)
	}
	for _, f := range c.directCalls {
		t.Errorf("шаг сборки зависимостей выписан второй раз — прямой вызов скрипта вне цели %s: %s",
			depsOwnerTarget, f)
	}
	for _, f := range c.upFindings {
		t.Errorf("цель подъёма: %s", f)
	}
}

// TestDepsSpellingGateFindsASecondCopy — инъекция настоящим входом: вызов
// цели в own-up, заменённый прямым вызовом скрипта, даёт обе находки по own-up;
// неизменённый файл — молчание.
func TestDepsSpellingGateFindsASecondCopy(t *testing.T) {
	raw, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("deploy/Makefile не прочитан: %v", err)
	}
	mk := string(raw)
	targets := parseRecipeTargets(mk)
	var call string
	for _, line := range targets["own-up"].recipe {
		if callSite(depsTargetCall, line) {
			call = line
			break
		}
	}
	if call == "" {
		t.Fatal("предпосылка пробы: в рецепте own-up нет вызова helm-deps — инъекции не во что встать")
	}
	if c := judgeDepsSpelling(mk); len(c.directCalls) != 0 || len(c.upFindings) != 0 {
		t.Errorf("близнец: неизменённый Makefile дал находки %v %v", c.directCalls, c.upFindings)
	}
	// Строка вызова у трёх целей одинакова — правится та, что стоит в own-up.
	at := strings.Index(mk, "\nown-up:")
	if at < 0 {
		t.Fatal("предпосылка пробы: строки цели own-up в Makefile нет")
	}
	injected := mk[:at] + strings.Replace(mk[at:], call+"\n", "\tbash scripts/helm-umbrella-deps.sh; \\\n", 1)
	c := judgeDepsSpelling(injected)
	has := func(list []string) bool {
		for _, f := range list {
			if strings.HasPrefix(f, "own-up: ") {
				return true
			}
		}
		return false
	}
	if !has(c.directCalls) {
		t.Errorf("инъекция «вторая копия в own-up»: находки прямого вызова нет (%v)", c.directCalls)
	}
	if !has(c.upFindings) {
		t.Errorf("инъекция «вторая копия в own-up»: находки по цели подъёма нет (%v)", c.upFindings)
	}
}
