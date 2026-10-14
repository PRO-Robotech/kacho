// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// umbrella_apply_runs_deps_first_test.go — КАЖДАЯ цель deploy/Makefile, которая
// отдаёт умбреллу helm'у, сначала зовёт владельца её зависимостей (kacho#2885).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Архивы локальных подчартов в git не лежат: их материализует
// `scripts/helm-umbrella-deps.sh` (единственный владелец — соседний
// helm_deps_single_owner_test.go). Цель, отдающая `./helm/umbrella` helm'у без
// этого шага, имеет ровно два исхода, и второй хуже первого:
//
//   - копия без архивов — отказ helm «missing in charts/ directory», код 2
//     (замер задачи: `stack-up STACK=own` остановился на `stack-secrets`);
//   - копия, где архивы собраны РАНЬШЕ, — helm применяет их как есть, и правка
//     шаблона подчарта после сборки архива до кластера не доезжает. Ни один шаг
//     цели этого не видит.
//
// Владелец сам решает, идти ли в сеть (отпечаток исходников), поэтому лишний
// вызов ничего не стоит, а пропущенный стоит стенда, исполняющего чужой шаблон.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ
//
// Обход — все цели Makefile, а не выписанный перечень: новая цель с `helm
// template ./helm/umbrella` без шага — находка тем же прогоном. Рецепт цели
// раскрывается вместе с предпосылками и с вызовами `$(MAKE) … <цель>` в том
// порядке, в каком их исполнит make. Применением умбреллы считается вызов helm
// (`upgrade`/`install`/`template`) с `./helm/umbrella` в позиции команды и вызов
// `stack-secrets.sh` — он рендерит умбреллу сам (scripts/stack-secrets.sh,
// `helm template … "$UMBRELLA"`). Строка внутри кавычек — подсказка оператору,
// а не вызов.
package deploy_test

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	// umbrellaApply — вызов, отдающий умбреллу helm'у.
	umbrellaApply = regexp.MustCompile(`(^|[;&|(]|\$\()[ \t@]*(helm[ \t]+(upgrade|install|template)\b[^;]*\./helm/umbrella(\s|$|\\)|(bash[ \t]+)?\S*stack-secrets\.sh\b)`)
	// umbrellaDepsStep — шаг владельца: сам скрипт либо цель-обёртка helm-deps.
	umbrellaDepsStep = regexp.MustCompile(`(^|[;&|(]|\$\()[ \t@]*((bash[ \t]+)?\S*helm-umbrella-deps\.sh\b|\$\(MAKE\)[^;]*\bhelm-deps\b)`)
	// subMakeCall — вызов другой цели того же Makefile.
	subMakeCall = regexp.MustCompile(`\$\(MAKE\)((?:[ \t]+[^;|&\s]+)+)`)
)

// callSite — совпадение стоит в позиции команды, а не внутри строкового
// литерала. Нечётное число кавычек слева — совпадение внутри незакрытой строки.
func callSite(re *regexp.Regexp, line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return false
	}
	loc := re.FindStringIndex(line)
	if loc == nil {
		return false
	}
	prefix := line[:loc[0]]
	return strings.Count(prefix, `"`)%2 == 0 && strings.Count(prefix, `'`)%2 == 0
}

// expandRecipe — строки рецепта цели в порядке исполнения: предпосылки, затем
// рецепт, где вызов `$(MAKE) … <цель>` раскрыт на месте.
func expandRecipe(targets map[string]recipeTarget, name string, seen map[string]bool) []string {
	if seen[name] {
		return nil
	}
	seen[name] = true
	t, ok := targets[name]
	if !ok {
		return nil
	}
	var out []string
	for _, p := range t.prereqs {
		out = append(out, expandRecipe(targets, p, seen)...)
	}
	for _, line := range t.recipe {
		out = append(out, line)
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, m := range subMakeCall.FindAllStringSubmatch(line, -1) {
			for _, w := range strings.Fields(m[1]) {
				if _, known := targets[w]; known && !strings.Contains(w, "=") {
					out = append(out, expandRecipe(targets, w, seen)...)
				}
			}
		}
	}
	return out
}

// umbrellaDepsCensus — перепись: целей осмотрено, целей с применением
// умбреллы, находки (цель → первая строка применения без шага перед ней).
type umbrellaDepsCensus struct {
	targets  int
	applying []string
	findings []string
}

func judgeUmbrellaDepsOrder(makefile string) umbrellaDepsCensus {
	targets := parseRecipeTargets(makefile)
	names := make([]string, 0, len(targets))
	for n := range targets {
		names = append(names, n)
	}
	sort.Strings(names)
	var c umbrellaDepsCensus
	for _, n := range names {
		c.targets++
		depsSeen := false
		for _, line := range expandRecipe(targets, n, map[string]bool{}) {
			if callSite(umbrellaDepsStep, line) {
				depsSeen = true
			}
			if callSite(umbrellaApply, line) {
				c.applying = append(c.applying, n)
				if !depsSeen {
					c.findings = append(c.findings, fmt.Sprintf("%s: %s", n, strings.TrimSpace(line)))
				}
				break
			}
		}
	}
	return c
}

func TestEveryUmbrellaApplyRunsTheDepsOwnerFirst(t *testing.T) {
	raw, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("deploy/Makefile не прочитан (%v) — судить нечего, и «ноль находок» "+
			"здесь ничего не значил бы", err)
	}
	c := judgeUmbrellaDepsOrder(string(raw))
	t.Logf("целей осмотрено: %d; отдают умбреллу helm'у: %d (%s); без шага зависимостей перед этим: %d",
		c.targets, len(c.applying), strings.Join(c.applying, " "), len(c.findings))
	if c.targets == 0 || len(c.applying) == 0 {
		t.Fatalf("обход не нашёл ни одной цели, отдающей умбреллу helm'у (целей %d) — "+
			"распознаватель разошёлся с формой записи, а не дерево чисто", c.targets)
	}
	for _, f := range c.findings {
		t.Errorf("цель отдаёт ./helm/umbrella helm'у, не позвав scripts/helm-umbrella-deps.sh "+
			"раньше: %s\n  в копии без архивов — отказ helm «missing in charts/ directory»; "+
			"в копии со старыми архивами — молча применённый прежний шаблон подчарта", f)
	}
}

// TestUmbrellaDepsOrderGateFindsAMissingStep — инъекция настоящим входом:
// рецепт `dev-up` из дерева без шага зависимостей и с шагом ПОСЛЕ helm даёт
// находку с именем цели; тот же рецепт без правки — молчание.
func TestUmbrellaDepsOrderGateFindsAMissingStep(t *testing.T) {
	raw, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("deploy/Makefile не прочитан: %v", err)
	}
	mk := string(raw)
	const step = "\t$(MAKE) --no-print-directory helm-deps; \\\n"
	targets := parseRecipeTargets(mk)
	devUp := strings.Join(targets["dev-up"].recipe, "\n")
	if !strings.Contains(devUp+"\n", strings.TrimSuffix(step, "\n")) {
		t.Fatalf("предпосылка пробы: в рецепте dev-up нет строки шага %q — инъекции не во что встать", step)
	}
	hasFinding := func(c umbrellaDepsCensus, target string) bool {
		for _, f := range c.findings {
			if strings.HasPrefix(f, target+": ") {
				return true
			}
		}
		return false
	}

	if c := judgeUmbrellaDepsOrder(mk); hasFinding(c, "dev-up") {
		t.Errorf("близнец: рецепт dev-up без правки дал находку %v", c.findings)
	}

	removed := strings.Replace(mk, step, "", 1)
	if c := judgeUmbrellaDepsOrder(removed); !hasFinding(c, "dev-up") {
		t.Errorf("инъекция «шага нет»: находки по dev-up нет (находки %v)", c.findings)
	}

	// Шаг ПОСЛЕ первого применения — тот же дефект: helm уже применил то, что лежало.
	idx := strings.Index(removed, "\thelm upgrade --install kacho-umbrella ./helm/umbrella -n kacho --create-namespace \\\n")
	if idx < 0 {
		t.Fatal("предпосылка пробы: строки применения умбреллы в dev-up нет")
	}
	end := idx + strings.Index(removed[idx:], "\n") + 1
	late := removed[:end] + step + removed[end:]
	if c := judgeUmbrellaDepsOrder(late); !hasFinding(c, "dev-up") {
		t.Errorf("инъекция «шаг после helm»: находки по dev-up нет (находки %v)", c.findings)
	}

	// Строка в кавычках — подсказка, а не вызов: не применение и не шаг.
	if callSite(umbrellaApply, `	echo "  helm upgrade --install x ./helm/umbrella"; \`) {
		t.Error("подсказка оператору в кавычках прочитана как применение умбреллы")
	}
}
