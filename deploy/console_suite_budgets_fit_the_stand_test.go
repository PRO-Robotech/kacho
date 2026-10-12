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
//   - счёт НЕВЕРНЫХ ПРЕДЪЯВЛЕНИЙ по оси источника (`scripts/ceremony-budget.ts`)
//     двумя долями своими ручками: отказы семи глаголов
//     (`KACHO_CEREMONY_FAILURE_BUDGET`, приёмка F8, F8-41) и попытки входа
//     ключом (`KACHO_ACCESS_KEY_SOURCE_BUDGET`, приёмка F8-S4, Р8, F8S4-15).
//     Доли идут на ОДИН счёт, поэтому против 80 % окна судится их СУММА: у
//     двух долей, каждая из которых ниже порога, сумма может быть выше него.
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

// sourceRefusalPercent — доля окна источника, которую не вправе превысить
// СУММА долей счёта неверных предъявлений (приёмка F8 Р7, F8-S4 Р8).
const sourceRefusalPercent = 80

var suiteBudgets = []suiteBudget{
	{guard: "scripts/registration-budget.ts", env: "KACHO_REGISTRATION_BUDGET", percent: 100},
	{guard: "scripts/ceremony-budget.ts", env: "KACHO_CEREMONY_FAILURE_BUDGET", percent: sourceRefusalPercent},
	{guard: "scripts/ceremony-budget.ts", env: "KACHO_ACCESS_KEY_SOURCE_BUDGET", percent: sourceRefusalPercent},
}

// sourceRefusalShares — доли одного счёта неверных предъявлений: судятся суммой.
var sourceRefusalShares = []string{"KACHO_CEREMONY_FAILURE_BUDGET", "KACHO_ACCESS_KEY_SOURCE_BUDGET"}

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

// declaredSuiteBudgets — бюджет каждой ручки (ключ — имя ручки) из шага, который
// зовёт её сторожа на отчёте прогона (самопроверка сторожа — другой скрипт и
// бюджета не несёт). Сторож без шага либо ручка без величины — находка: судить
// нечего, а не «бюджет ноль».
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
				got[b.env] = n
			}
		}
		if found != 1 {
			problems = append(problems, fmt.Sprintf("сторож %s (%s): шагов, зовущих его на отчёте прогона, %d, а не 1",
				b.guard, b.env, found))
		}
	}
	return got, problems
}

// judgeSuiteBudgets — находки: бюджет выше доли окна источника стенда; сумма
// долей счёта неверных предъявлений выше 80 % того же окна.
func judgeSuiteBudgets(budgets map[string]int, stand string, window sourceWindow) []string {
	var out []string
	for _, b := range suiteBudgets {
		n, ok := budgets[b.env]
		if !ok {
			continue
		}
		if n*100 > window.attempts*b.percent {
			out = append(out, fmt.Sprintf("стек %s: бюджет %s=%d выше %d %% окна источника стенда (%s) — "+
				"сторож зеленеет там, где стенд уже отказал", stand, b.env, n, b.percent, window))
		}
	}
	sum, parts := 0, make([]string, 0, len(sourceRefusalShares))
	for _, env := range sourceRefusalShares {
		n := budgets[env]
		sum += n
		parts = append(parts, fmt.Sprintf("%s=%d", env, n))
	}
	if sum*100 > window.attempts*sourceRefusalPercent {
		out = append(out, fmt.Sprintf("стек %s: сумма долей счёта неверных предъявлений %s = %d выше %d %% окна "+
			"источника стенда (%s) — каждая доля ниже порога, а счёт у них один (F8S4-15)",
			stand, strings.Join(parts, " + "), sum, sourceRefusalPercent, window))
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

	declared := fmt.Sprintf("KACHO_REGISTRATION_BUDGET: %q", strconv.Itoa(budgets["KACHO_REGISTRATION_BUDGET"]))
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

// TestF8S4_15_SourceRefusalSharesAreJudgedBySum — приёмка F8-S4, Р8, F8S4-15:
// доли F8-41 и S4 идут на ОДИН счёт неверных предъявлений, и судится их
// СУММА против 80 % окна стенда. Инъекция настоящим входом — работой из дерева
// с подменённой величиной доли S4: каждая доля по отдельности не выше 80 %
// окна, а сумма на единицу выше — находка с именем стека; сумма, равная 80 %
// окна, — молчание.
func TestF8S4_15_SourceRefusalSharesAreJudgedBySum(t *testing.T) {
	wf, err := os.ReadFile(consoleSuiteWorkflow)
	if err != nil {
		t.Fatalf("работа %s не читается: %v", consoleSuiteWorkflow, err)
	}
	stacks := deployStacks(t)
	_, suite := consoleSuiteStacks(t)
	w := chainSourceWindow(t, suite[0], stacks[suite[0]])

	budgets, problems := declaredSuiteBudgets(wf)
	f841, okF := budgets["KACHO_CEREMONY_FAILURE_BUDGET"]
	s4, okS := budgets["KACHO_ACCESS_KEY_SOURCE_BUDGET"]
	if len(problems) > 0 || !okF || !okS {
		t.Fatalf("предпосылка пробы: доли счёта неверных предъявлений из работы не прочитаны: %v %v", budgets, problems)
	}
	declared := fmt.Sprintf("KACHO_ACCESS_KEY_SOURCE_BUDGET: %q", strconv.Itoa(s4))
	if !strings.Contains(string(wf), declared) {
		t.Fatalf("предпосылка пробы: строки %s в работе нет — инъекции не во что встать", declared)
	}
	ceiling := w.attempts * sourceRefusalPercent / 100
	with := func(n int) map[string]int {
		b, p := declaredSuiteBudgets([]byte(strings.Replace(string(wf), declared,
			fmt.Sprintf("KACHO_ACCESS_KEY_SOURCE_BUDGET: %q", strconv.Itoa(n)), 1)))
		if len(p) > 0 {
			t.Fatalf("инъекция доли S4 = %d: работа не прочитана: %v", n, p)
		}
		return b
	}

	over := ceiling - f841 + 1
	if over*100 > w.attempts*sourceRefusalPercent || f841*100 > w.attempts*sourceRefusalPercent {
		t.Fatalf("предпосылка близнеца: доля по отдельности уже выше %d %% окна (%d, %d)", sourceRefusalPercent, f841, over)
	}
	got := judgeSuiteBudgets(with(over), suite[0], w)
	if len(got) != 1 || !strings.Contains(got[0], suite[0]) || !strings.Contains(got[0], "сумма") {
		t.Errorf("F8S4-15 · сумма %d + %d = %d выше %d: ожидалась одна находка суммы с именем стека, получено %v",
			f841, over, f841+over, ceiling, got)
	}
	if got := judgeSuiteBudgets(with(over-1), suite[0], w); len(got) != 0 {
		t.Errorf("F8S4-15 · сумма %d + %d = %d, равная %d %% окна, — не находка; получено %v",
			f841, over-1, f841+over-1, sourceRefusalPercent, got)
	}
	if got := judgeSuiteBudgets(budgets, suite[0], w); len(got) != 0 {
		t.Errorf("F8S4-15 · работа без правки (%d + %d) дала находки %v", f841, s4, got)
	}
	t.Logf("F8S4-15 · стек %s: доли %d + %d = %d при %d %% окна %d (%d)",
		suite[0], f841, s4, f841+s4, sourceRefusalPercent, w.attempts, ceiling)
}
