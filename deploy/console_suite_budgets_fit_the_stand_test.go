// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_suite_budgets_fit_the_stand_test.go — БЮДЖЕТЫ СТОРОЖЕЙ ПРОГОНА КОНСОЛИ
// НЕ ВЫШЕ ОКНА ИСТОЧНИКА СТЕНДА, КОТОРЫЙ ЭТОТ ПРОГОН ПОДНИМАЕТ (kacho#2909).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Работа сквозных проб консоли судит расход окна источника двумя сторожами по
// отчёту прогона, и бюджет каждого — ВХОД, объявленный в работе:
//
//   - окно РЕГИСТРАЦИЙ (`scripts/registration-budget.ts`,
//     `KACHO_REGISTRATION_BUDGET`): с kaname#456 каждая регистрация списывается
//     с окна источника, величина — `authn.login.sourceAttempts` посадки;
//   - отказы входа по оси источника (`scripts/ceremony-budget.ts`,
//     `KACHO_CEREMONY_FAILURE_BUDGET`, приёмка F8, F8-41): не выше 80 % той же
//     величины — так и объявлено у шага работы.
//
// Сторож, чей бюджет выше окна стенда, зеленеет ровно там, где стенд уже
// отказал: первой о переполнении снова скажет чужая проба по чужой причине.
// Поэтому бюджет сверяется с величиной стенда, который поднимает ТА ЖЕ работа
// (`make dev-up` через владельца подъёма). Стеки стенда проба ВЫВОДИТ из работы
// и рецепта цели (consoleSuiteStacks), величину — из слитых значений цепочки
// (chainSourceWindow): выписанное число разошлось бы с подъёмом молча.
//
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ. Что прогон в бюджет УКЛАДЫВАЕТСЯ — это судят сами
// сторожа по отчёту прогона. Прогон против чужого стенда (`workflow_dispatch` с
// адресом консоли) поднимает не этот стенд, и его окно здесь не судится.
//
// СНЯТИЕ. Окно регистраций читает величину оси входа (`sourceAttempts`), своей
// ручки у него нет. Заведёт kaname окну регистраций свою ручку — сверка бюджета
// регистраций переезжает на неё, а строка `sourceAttempts` здесь становится
// ложной: правится тем же изменением, что поднимает пин на такую ревизию.
package deploy_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// suiteBudget — бюджет сторожа, объявленный шагом работы.
type suiteBudget struct {
	guard   string // скрипт сторожа
	env     string // ручка бюджета
	percent int    // доля окна источника, которую бюджет не вправе превысить
}

var suiteBudgets = []suiteBudget{
	{guard: "scripts/registration-budget.ts", env: "KACHO_REGISTRATION_BUDGET", percent: 100},
	{guard: "scripts/ceremony-budget.ts", env: "KACHO_CEREMONY_FAILURE_BUDGET", percent: 80},
}

type workflowStep struct {
	ID  string            `yaml:"id"`
	Run string            `yaml:"run"`
	Env map[string]string `yaml:"env"`
}

type workflowDoc struct {
	Jobs map[string]struct {
		Steps []workflowStep `yaml:"steps"`
	} `yaml:"jobs"`
}

// declaredSuiteBudgets — бюджет каждого сторожа из шага, который зовёт его на
// отчёте прогона (самопроверка сторожа — другой скрипт и бюджета не несёт).
// Сторож без шага либо без величины — находка: судить нечего, а не «бюджет ноль».
func declaredSuiteBudgets(workflow []byte) (map[string]int, []string) {
	var doc workflowDoc
	if err := yaml.Unmarshal(workflow, &doc); err != nil {
		return nil, []string{fmt.Sprintf("работа не разобрана: %v", err)}
	}
	got := map[string]int{}
	var problems []string
	for _, b := range suiteBudgets {
		found := 0
		for _, job := range doc.Jobs {
			for _, st := range job.Steps {
				if !strings.Contains(st.Run, "node "+b.guard+" ") {
					continue
				}
				found++
				raw, ok := st.Env[b.env]
				n, err := strconv.Atoi(raw)
				if !ok || err != nil || n < 0 {
					problems = append(problems, fmt.Sprintf("шаг %q зовёт %s без целой величины %s (%q)",
						st.ID, b.guard, b.env, raw))
					continue
				}
				got[b.guard] = n
			}
		}
		if found != 1 {
			problems = append(problems, fmt.Sprintf("сторож %s: шагов, зовущих его на отчёте прогона, %d, а не 1",
				b.guard, found))
		}
	}
	return got, problems
}

// judgeSuiteBudgets — находки: бюджет выше доли окна источника стенда.
func judgeSuiteBudgets(budgets map[string]int, stand string, window sourceWindow) []string {
	var out []string
	for _, b := range suiteBudgets {
		n, ok := budgets[b.guard]
		if !ok {
			continue
		}
		if n*100 > window.attempts*b.percent {
			out = append(out, fmt.Sprintf("стек %s: бюджет %s=%d выше %d %% окна источника стенда (%s) — "+
				"сторож зеленеет там, где стенд уже отказал", stand, b.env, n, b.percent, window))
		}
	}
	return out
}

func TestConsoleSuiteBudgetsFitTheStandWindow(t *testing.T) {
	wf, err := os.ReadFile(consoleSuiteWorkflow)
	if err != nil {
		t.Fatalf("работа %s не читается: %v", consoleSuiteWorkflow, err)
	}
	budgets, problems := declaredSuiteBudgets(wf)
	for _, p := range problems {
		t.Error(p)
	}
	stacks := deployStacks(t)
	target, suite := consoleSuiteStacks(t)
	if len(suite) == 0 {
		t.Fatalf("рецепт %s не передаёт helm ни одного стека — судить бюджеты не с чем", target)
	}
	judged := 0
	for _, name := range suite {
		chain, ok := stacks[name]
		if !ok {
			t.Fatalf("рецепт %s называет стек %q, которого в таблице нет", target, name)
		}
		w := chainSourceWindow(t, name, chain)
		for _, f := range judgeSuiteBudgets(budgets, name, w) {
			t.Error(f)
		}
		judged++
		t.Logf("стек %s: окно источника %s; бюджеты %v", name, w, budgets)
	}
	t.Logf("перепись: сторожей %d, бюджетов объявлено %d, стеков стенда (%s) сверено %d",
		len(suiteBudgets), len(budgets), target, judged)
}

// TestConsoleSuiteBudgetGateIsAbleToFail — инъекция настоящим входом: работа из
// дерева с бюджетом регистраций выше окна стенда даёт находку, тот же вход без
// правки — молчание; работа без шага сторожа — находка, а не «ноль».
func TestConsoleSuiteBudgetGateIsAbleToFail(t *testing.T) {
	wf, err := os.ReadFile(consoleSuiteWorkflow)
	if err != nil {
		t.Fatalf("работа %s не читается: %v", consoleSuiteWorkflow, err)
	}
	stacks := deployStacks(t)
	_, suite := consoleSuiteStacks(t)
	w := chainSourceWindow(t, suite[0], stacks[suite[0]])

	budgets, problems := declaredSuiteBudgets(wf)
	if len(problems) > 0 || len(budgets) != len(suiteBudgets) {
		t.Fatalf("предпосылка пробы: бюджеты из работы не прочитаны целиком: %v %v", budgets, problems)
	}
	if f := judgeSuiteBudgets(budgets, suite[0], w); len(f) != 0 {
		t.Errorf("близнец: работа без правки дала находки %v", f)
	}

	declared := fmt.Sprintf("KACHO_REGISTRATION_BUDGET: %q", strconv.Itoa(budgets["scripts/registration-budget.ts"]))
	if !strings.Contains(string(wf), declared) {
		t.Fatalf("предпосылка пробы: строки %s в работе нет — инъекции не во что встать", declared)
	}
	over := strings.Replace(string(wf), declared,
		fmt.Sprintf("KACHO_REGISTRATION_BUDGET: %q", strconv.Itoa(w.attempts+1)), 1)
	b2, p2 := declaredSuiteBudgets([]byte(over))
	if len(p2) > 0 || len(judgeSuiteBudgets(b2, suite[0], w)) != 1 {
		t.Errorf("инъекция «бюджет на единицу выше окна %d»: ожидалась одна находка, получено %v %v",
			w.attempts, judgeSuiteBudgets(b2, suite[0], w), p2)
	}

	atWindow := strings.Replace(string(wf), declared,
		fmt.Sprintf("KACHO_REGISTRATION_BUDGET: %q", strconv.Itoa(w.attempts)), 1)
	b3, _ := declaredSuiteBudgets([]byte(atWindow))
	if f := judgeSuiteBudgets(b3, suite[0], w); len(f) != 0 {
		t.Errorf("граница: бюджет, равный окну, — не находка; получено %v", f)
	}

	gone := strings.Replace(string(wf), "node scripts/registration-budget.ts results.json", "true", 1)
	if _, p4 := declaredSuiteBudgets([]byte(gone)); len(p4) == 0 {
		t.Error("инъекция «шага сторожа нет»: находки нет — пустое прочитано бы как «в бюджете»")
	}
}
