// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanvars_test.go — гейт связности newman-наборов: каждая `{{переменная}}`, которую
// использует коллекция, обязана кем-то выставляться.
//
// Зачем. Postman НЕ ругается на неразрешённую переменную — он подставляет её ЛИТЕРАЛОМ.
// Наружу это вылезает бессмысленным симптомом далеко от причины:
//   - `invalid resource id '{{lifeId}}'` → 400 вместо ожидаемого 404;
//   - `getaddrinfo ENOTFOUND {{internalbaseurl}}`;
//   - `expected [] to include undefined`.
// Автор такого кейса видит «баг продукта», хотя продукт ни при чём. Хуже: кейс может
// быть ЗЕЛЁНЫМ по неверной причине, если его ожидания случайно совпадут с ответом на
// мусорный запрос.
//
// Реальный случай (2026-07-16): `list-filter-d` требовал jwtSubnetSubsetViewer /
// listFilterProjectId / subnetVisibleId / subnetHiddenId. Его docstring прямо утверждал
// «Pre-conditions готовит tests/authz-fixtures/setup.sh… Setup патчит env-файл, добавляя
// …» — а setup их НИКОГДА не добавлял. Док противоречил коду, тест был сломан с момента
// написания и никто этого не замечал: падение выглядело как продуктовый 401/undefined.
//
// Гейт статический (никакого стенда) и точный: на 6 сервисах даёт 0 ложных
// срабатываний — переменные, выставляемые скриптами коллекции (`pm.environment.set`),
// учитываются как определённые.
package repohygiene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// runtimeVars — выставляются харнессом при запуске (deploy/scripts/newman-e2e.sh
// передаёт их через --env-var), а не env-файлом. Не считаются пропущенными.
var runtimeVars = map[string]bool{
	"baseUrl":         true,
	"internalBaseUrl": true,
	"runId":           true,
}

// knownGaps — ИЗВЕСТНЫЕ, отслеживаемые пробелы фикстур. Не «чтобы гейт молчал», а чтобы
// он защищал от НОВЫХ случаев, пока чинится старый: без allowlist'а пришлось бы либо
// держать CI красным, либо не ставить гейт вовсе.
//
// Каждая запись обязана нести ссылку на тикет. Пустеет — удаляется вместе с этой картой.
var knownGaps = map[string]string{
	// list-filter-d (kacho-vpc): per-object filtered List. Фикстуры не умеют
	// AccessBinding с resourceNames (per-object грант) и не сеют subnet'ы — нужен
	// новый субъект S + проект + 2 подсети + binding на одну из них.
	// Сломан с момента написания; docstring кейса утверждает обратное.
	"vpc/listFilterProjectId": "PRO-Robotech/kacho#1",
	"vpc/subnetVisibleId":     "PRO-Robotech/kacho#1",
	"vpc/subnetHiddenId":      "PRO-Robotech/kacho#1",
}

var (
	varRe = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}`)
	setRe = regexp.MustCompile(`(?:environment|collectionVariables|globals)\.set\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)`)
)

// TestNewmanVariablesAreDefined — ни одна используемая {{var}} не остаётся без источника.
func TestNewmanVariablesAreDefined(t *testing.T) {
	root := repoRoot(t)
	suites, err := filepath.Glob(filepath.Join(root, "services", "*", "tests", "newman"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if gw := filepath.Join(root, "gateway", "tests", "newman"); dirExists(gw) {
		suites = append(suites, gw)
	}
	if len(suites) == 0 {
		t.Skip("newman-наборов не найдено")
	}

	var problems []string
	for _, suite := range suites {
		svc := suiteName(root, suite)
		cols, _ := filepath.Glob(filepath.Join(suite, "collections", "*.json"))
		if len(cols) == 0 {
			continue // набора нет (напр. geo — только README)
		}

		defined := map[string]bool{}
		envs, _ := filepath.Glob(filepath.Join(suite, "environments", "*.json"))
		for _, e := range envs {
			for _, k := range envKeys(t, e) {
				defined[k] = true
			}
		}

		used := map[string]bool{}
		for _, c := range cols {
			b, err := os.ReadFile(c) //nolint:gosec // путь из glob по репо
			if err != nil {
				t.Fatalf("read %s: %v", c, err)
			}
			s := string(b)
			for _, m := range varRe.FindAllStringSubmatch(s, -1) {
				used[m[1]] = true
			}
			// Переменные, которые коллекция выставляет себе сама по ходу прогона.
			for _, m := range setRe.FindAllStringSubmatch(s, -1) {
				defined[m[1]] = true
			}
		}

		var missing []string
		for v := range used {
			if defined[v] || runtimeVars[v] {
				continue
			}
			if _, known := knownGaps[svc+"/"+v]; known {
				continue
			}
			missing = append(missing, v)
		}
		sort.Strings(missing)
		for _, m := range missing {
			problems = append(problems, svc+": {{"+m+"}} — не в env, не выставляется скриптом, не runtime")
		}
	}

	if len(problems) > 0 {
		t.Errorf("%d newman-переменн(ая|ых) без источника — Postman подставит их ЛИТЕРАЛОМ, и падение "+
			"будет выглядеть багом продукта:\n%s\n\nпочинить: засеять в tests/authz-fixtures/setup.sh "+
			"(+ patch-env) либо выставлять в самой коллекции",
			len(problems), strings.Join(problems, "\n"))
	}
}

// TestKnownGapsAreTracked — каждая запись knownGaps несёт ссылку на тикет, и карта не
// разрастается молча.
func TestKnownGapsAreTracked(t *testing.T) {
	for k, issue := range knownGaps {
		if !strings.Contains(issue, "#") {
			t.Errorf("knownGaps[%q] = %q — нужна ссылка на тикет (owner/repo#N)", k, issue)
		}
	}
}

func envKeys(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // путь из glob по репо
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc struct {
		Values []struct {
			Key string `json:"key"`
		} `json:"values"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := make([]string, 0, len(doc.Values))
	for _, v := range doc.Values {
		out = append(out, v.Key)
	}
	return out
}

func suiteName(root, suite string) string {
	rel, err := filepath.Rel(root, suite)
	if err != nil {
		return suite
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) >= 2 && parts[0] == "services" {
		return parts[1]
	}
	return parts[0]
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
