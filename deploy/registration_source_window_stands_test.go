// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// registration_source_window_stands_test.go — ОКНО ИСТОЧНИКА СТЕНДА НАБОРОВ
// ВМЕЩАЕТ ПРОГОН, А УПРАВЛЯЕМЫЕ СТЕНДЫ СТОЯТ НА ВЕЛИЧИНЕ ПРОДУКТА (kacho#2901).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// С kaname#456 каждая регистрация списывается с окна ИСТОЧНИКА, и величина окна
// — те же `authn.login.source-attempts` / `source-window`, что у оси неверных
// предъявлений входа. У сквозных проб консоли бегун один, значит источник один
// на весь прогон, а фикстура заводит человека на пробу. Наблюдалось на запросе
// #2908 (голова 65fbffda1f3): после пятидесяти регистраций пятьдесят первая
// получила `registration refused`, и пять проб упали на чужой причине.
//
// Отсюда два свойства, и оба судятся здесь:
//
//   - стенд, который поднимает `make dev-up` (kind, `guard-kind-context`), —
//     стенд наборов: его окно источника объявлено СВОЕЙ величиной, выше величины
//     продукта, при том же сроке окна (меняется один факт — счёт);
//   - любой другой стек таблицы — управляемые стенды и боевая посадка — стоит на
//     величине продукта (`values.prod.yaml`). Величина стенда наборов в их
//     цепочку не протекает, хотя `prorobotech` и `a8f60d` наследуют профиль
//     `values.dev.yaml`: рост потолка частоты по источнику на площадке, куда
//     ходят не пробы, ослабил бы рубеж перебора паролей молча.
//
// Какие стеки поднимает стенд проб, проба ВЫВОДИТ: цель подъёма — из работы
// конвейера консоли, стеки — из рецепта цели (`$(call STACK_ARGS,<стек>)`).
// Выписанное звено разошлось бы с подъёмом молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Что величины стенда ХВАТАЕТ прогону — это судит сам прогон сквозных проб
// консоли; сторож расхода окна прогоном — `ui-future/e2e/scripts/registration-budget.ts`,
// сверка его бюджета с величиной стенда — console_suite_budgets_fit_the_stand_test.go
// (kacho#2909). Здесь — только что величина стенда своя и что она не протекает.
package deploy_test

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// consoleSuiteWorkflow — работа конвейера, поднимающая стенд сквозных проб
// консоли. Цель подъёма берётся ОТТУДА, а не выписывается.
const consoleSuiteWorkflow = "../.github/workflows/console-e2e.yml"

// productStack — стек, чья величина — величина продукта.
const productStack = "prod"

// consoleStandUp — вызов цели make через владельца подъёма стенда одной строкой
// (`stand-up.sh … -- make <цель>`): так её зовёт работа консоли.
var consoleStandUp = regexp.MustCompile(`stand-up\.sh[^\n]*\s--\s+make\s+([a-z][a-z0-9-]*)`)

// consoleSuiteStacks — цель подъёма стенда проб консоли и стеки, которые её
// рецепт передаёт helm (без повторов, по порядку). Рецепт читает тот же
// читатель, что у соседней пробы боевой посадки шарда (`makeRecipeStacks`):
// второе выражение об одном предмете разошлось бы молча.
func consoleSuiteStacks(t *testing.T) (string, []string) {
	t.Helper()
	wf, err := os.ReadFile(consoleSuiteWorkflow)
	if err != nil {
		t.Fatalf("работа конвейера %s не читается (%v) — предпосылка пробы исчезла", consoleSuiteWorkflow, err)
	}
	m := consoleStandUp.FindSubmatch(wf)
	if m == nil {
		t.Fatalf("в %s не найден вызов цели make через владельца подъёма стенда — распознаватель "+
			"перестал узнавать работу, и суждение о стенде проб было бы о непрочитанном", consoleSuiteWorkflow)
	}
	target := string(m[1])
	seen := map[string]bool{}
	var out []string
	for _, name := range makeRecipeStacks(t, target) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return target, out
}

// sourceWindow — окно источника полосы входа в слитых значениях цепочки.
type sourceWindow struct {
	attempts int
	window   string
}

func (w sourceWindow) String() string { return fmt.Sprintf("%d за %s", w.attempts, w.window) }

func chainSourceWindow(t *testing.T, stack string, chain []string) sourceWindow {
	t.Helper()
	merged := readYAML(t, umbrellaDir+"/values.yaml")
	for _, p := range chain {
		merged = mergeValues(merged, readYAML(t, umbrellaDir+"/"+p))
	}
	rawAttempts, okA := lookup(merged, "kaname", "config", "authn", "login", "sourceAttempts")
	rawWindow, okW := lookup(merged, "kaname", "config", "authn", "login", "sourceWindow")
	attempts, isInt := rawAttempts.(int)
	window, isStr := rawWindow.(string)
	if !okA || !okW || !isInt || !isStr || attempts <= 0 || window == "" {
		t.Fatalf("стек %s (%s): окно источника не объявлено целиком — sourceAttempts=%v, sourceWindow=%v; "+
			"без обеих величин служба под посадкой own не стартует, а проба не вправе судить пустое",
			stack, strings.Join(chain, " + "), rawAttempts, rawWindow)
	}
	return sourceWindow{attempts: attempts, window: window}
}

func TestRegistrationSourceWindow_KindSuiteStandDeclaresItsOwnCeiling(t *testing.T) {
	stacks := deployStacks(t)
	product := chainSourceWindow(t, productStack, stacks[productStack])
	target, suite := consoleSuiteStacks(t)
	for _, name := range suite {
		chain, ok := stacks[name]
		if !ok {
			t.Fatalf("рецепт %s называет стек %q, которого в таблице нет", target, name)
		}
		got := chainSourceWindow(t, name, chain)
		switch {
		case got.window != product.window:
			t.Errorf("стек %s: срок окна источника %s, у продукта %s — стенд наборов меняет ОДИН факт, счёт, "+
				"а срок окна у него тот же", name, got.window, product.window)
		case got.attempts <= product.attempts:
			t.Errorf("стек %s: окно источника %s — не выше величины продукта (%s). Стенд наборов заводит "+
				"человека на пробу с одного источника, и на величине продукта регистрация сверх неё получает "+
				"`registration refused`: падает чужая проба по чужой причине (наблюдалось на #2908)",
				name, got, product)
		default:
			t.Logf("стек %s (%s): окно источника %s, у продукта %s", name, strings.Join(chain, " + "), got, product)
		}
	}
	t.Logf("перепись: стеков рецепта %s — %d (%s)", target, len(suite), strings.Join(suite, ", "))
}

func TestRegistrationSourceWindow_EveryOtherStackKeepsTheProductCeiling(t *testing.T) {
	stacks := deployStacks(t)
	product := chainSourceWindow(t, productStack, stacks[productStack])
	target, suiteStacks := consoleSuiteStacks(t)
	suite := map[string]bool{}
	for _, name := range suiteStacks {
		suite[name] = true
	}
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		if !suite[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) < 2 {
		t.Fatalf("вне рецепта %s в таблице стеков %d — проба судила бы одну боевую посадку саму с собой",
			target, len(names))
	}
	inheriting := 0
	for _, name := range names {
		chain := stacks[name]
		for _, p := range chain {
			if p == "values.dev.yaml" {
				inheriting++
				break
			}
		}
		got := chainSourceWindow(t, name, chain)
		if got != product {
			t.Errorf("стек %s (%s): окно источника %s, а величина продукта %s. Величина стенда наборов "+
				"протекла на стенд, куда ходят не пробы: потолок частоты по источнику там ослаблен молча",
				name, strings.Join(chain, " + "), got, product)
			continue
		}
		t.Logf("стек %s: окно источника %s — величина продукта", name, got)
	}
	t.Logf("перепись: стеков вне рецепта %s — %d, из них наследуют профиль стенда наборов — %d",
		target, len(names), inheriting)
}

// ─────────────────────────────────────────────────────────────────────────────
// СТЕНД ПРОБ В СВОЁМ ПРОСТРАНСТВЕ ИМЁН (scripts/stand-ns.sh, kacho#3223)
//
// Стенд проб поднимает любую цепочку таблицы, и сквозные пробы консоли гоняются
// на нём тем же бегуном с одного источника, что в конвейере. Цепочка `own` (и
// любая на боевом слое) несёт величину продукта — 50 за окно, — и набор проб
// консоли обрывался ранней остановкой на регистрации сверх неё (наблюдалось на
// стенде t3128-final). Поэтому слой стенда проб объявляет окну источника ту же
// величину, что у стенда наборов конвейера, ВЫВОДЯ её из его цепочки; срок окна
// — продукта, меняется один факт, счёт.
//
// Таблица стеков при этом не меняется: величина живёт в слое, который ставится
// только в пространство стенда проб (ClusterIP, без контроллера входа, срок ≤ 12
// ч), и до управляемых стендов и боевой посадки не доходит — это держит
// TestRegistrationSourceWindow_EveryOtherStackKeepsTheProductCeiling выше.

// judgeProbeStandWindow — находки: цепочки, на которых окно источника стенда
// проб со слоем стенда не равно окну стенда наборов конвейера.
func judgeProbeStandWindow(t *testing.T, stacks map[string][]string, overlay map[string]any, suite sourceWindow) []string {
	t.Helper()
	var out []string
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		merged := chainWith(t, stacks[name], overlay)
		a, _ := lookup(merged, "kaname", "config", "authn", "login", "sourceAttempts")
		w, _ := lookup(merged, "kaname", "config", "authn", "login", "sourceWindow")
		if a != suite.attempts || w != suite.window {
			out = append(out, fmt.Sprintf("стек %s со слоем стенда проб: окно источника %v за %v, у стенда наборов "+
				"конвейера %s — набор проб консоли упрётся в регистрацию сверх окна", name, a, w, suite))
		}
	}
	return out
}

// suiteWindow — окно источника стенда наборов конвейера: одно на все стеки
// рецепта, иначе «та же величина» не определена.
func suiteWindow(t *testing.T, stacks map[string][]string) sourceWindow {
	t.Helper()
	target, suite := consoleSuiteStacks(t)
	if len(suite) == 0 {
		t.Fatalf("рецепт %s не передаёт helm ни одного стека — окна стенда наборов нет", target)
	}
	w := chainSourceWindow(t, suite[0], stacks[suite[0]])
	for _, name := range suite[1:] {
		if got := chainSourceWindow(t, name, stacks[name]); got != w {
			t.Fatalf("стеки рецепта %s расходятся окном источника: %s у %s, %s у %s", target, w, suite[0], got, name)
		}
	}
	return w
}

func TestRegistrationSourceWindow_ProbeStandCarriesTheSuiteCeiling(t *testing.T) {
	stacks := deployStacks(t)
	suite := suiteWindow(t, stacks)
	product := chainSourceWindow(t, productStack, stacks[productStack])
	if suite.window != product.window {
		t.Fatalf("предпосылка: срок окна стенда наборов %s, у продукта %s", suite.window, product.window)
	}
	for _, f := range judgeProbeStandWindow(t, stacks, standOverlay(t), suite) {
		t.Error(f)
	}
	t.Logf("перепись: стеков таблицы со слоем стенда проб — %d, окно стенда наборов %s, продукта %s",
		len(stacks), suite, product)
}

// Близнец: без слоя стенда хотя бы один стек таблицы стоит на величине продукта —
// иначе проба выше судила бы цепочки, где окно и так стенда наборов.
func TestRegistrationSourceWindow_ProbeStandJudgeIsAbleToFail(t *testing.T) {
	stacks := deployStacks(t)
	suite := suiteWindow(t, stacks)
	if f := judgeProbeStandWindow(t, stacks, nil, suite); len(f) == 0 {
		t.Fatal("предпосылка пробы: без слоя стенда ни один стек не стоит на величине продукта — судить нечего")
	} else {
		t.Logf("без слоя стенда находок %d: %v", len(f), f)
	}
}
