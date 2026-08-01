// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// listfiltergatewiring_test.go — гейт «сервис, у которого есть страж сужения
// списка, обязан быть в цикле CI, который этих стражей зовёт».
//
// # Что охраняется
//
// audit-list-filter утверждает, что публичный List отдаёт только те строки,
// которые вызывающему видны. У этого класса ДВЕ половины, и каждая уже отказывала
// в этом дереве по отдельности: страж, который ничего не проверяет, и страж,
// которого никто не зовёт. Здесь — вторая.
//
// # Почему список сервисов НЕ выписан рукой
//
// Рукописный перечень расходится с деревом — это свойство механизма, а не
// аккуратности автора. Именно так iam и оказался вне гейта: цикл в CI перечислял
// пять сервисов, а шестой, с САМОЙ БОЛЬШОЙ сужаемой List-поверхностью на
// платформе, в перечень просто не попал, и заметить это было нечем.
//
// Поэтому перечень здесь ВЫВОДИТСЯ: множество сервисов берётся из дерева (у кого
// объявлена цель `audit-list-filter`), множество вызываемых — из самого workflow.
// Новый сервис попадает под гейт по построению, а не после того, как кто-то
// вспомнит. Обратное направление проверяется тоже: цикл, зовущий сервис без
// цели, — такая же находка (шаг молча позеленел бы на несуществующем таргете
// ровно так же, как раньше зеленел таргет, ничего не проверявший).
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const listFilterTarget = "audit-list-filter"

// serviceLoopRe находит `for <var> in <слова>;` в шаге workflow — так цикл
// раскрывается в конкретные сервисы, которые он обойдёт.
var serviceLoopRe = regexp.MustCompile(`for\s+[A-Za-z_][A-Za-z0-9_]*\s+in\s+([^;\n]+)`)

// targetDeclRe находит объявление цели в Makefile (не .PHONY-строку).
var targetDeclRe = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(listFilterTarget) + `\s*:(?:[^=]|$)`)

// servicesDeclaringTheGate — сервисы, чей Makefile ОБЪЯВЛЯЕТ цель. Это факт о
// дереве, а не список в голове автора.
func servicesDeclaringTheGate(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatalf("не прочитан services/: %v", err)
	}
	var out []string
	scanned := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mk := filepath.Join(root, "services", e.Name(), "Makefile")
		raw, rerr := os.ReadFile(mk) //nolint:gosec // путь собран из обхода дерева репозитория
		if rerr != nil {
			continue
		}
		scanned++
		if targetDeclRe.Match(raw) {
			out = append(out, e.Name())
		}
	}
	if scanned == 0 {
		t.Fatal("не прочитано ни одного services/*/Makefile — гейт смотрит не туда, его вердикт беспредметен")
	}
	sort.Strings(out)
	return out
}

// servicesTheWorkflowRuns — сервисы, которые обходит шаг CI, зовущий цель.
func servicesTheWorkflowRuns(t *testing.T, root string) []string {
	t.Helper()
	wfDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		t.Fatalf("не прочитан %s: %v", wfDir, err)
	}
	seen := map[string]bool{}
	files := 0
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || (!strings.HasSuffix(n, ".yml") && !strings.HasSuffix(n, ".yaml")) {
			continue
		}
		files++
		raw, rerr := os.ReadFile(filepath.Join(wfDir, n)) //nolint:gosec // обход каталога workflow'ов репозитория
		if rerr != nil {
			t.Fatalf("не прочитан %s: %v", n, rerr)
		}
		for _, block := range strings.Split(string(raw), "- name:") {
			if !strings.Contains(block, listFilterTarget) {
				continue
			}
			// Комментарии шага не являются исполняемой частью: перечень сервисов
			// берётся из тела цикла, а не из прозы, которая его объясняет.
			var code []string
			for _, ln := range strings.Split(block, "\n") {
				if s := strings.TrimSpace(ln); !strings.HasPrefix(s, "#") {
					code = append(code, ln)
				}
			}
			m := serviceLoopRe.FindStringSubmatch(strings.Join(code, "\n"))
			if m == nil {
				continue
			}
			for _, svc := range strings.Fields(m[1]) {
				seen[svc] = true
			}
		}
	}
	if files == 0 {
		t.Fatal("не прочитано ни одного workflow — гейт смотрит не туда")
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Оба множества обязаны совпадать — в ОБЕ стороны.
func TestListFilterGate_EveryDeclaringServiceIsRunByCI(t *testing.T) {
	root := repoRoot(t)
	declared := servicesDeclaringTheGate(t, root)
	run := servicesTheWorkflowRuns(t, root)

	if len(declared) == 0 {
		t.Fatal("ни один сервис не объявляет цель — проверять нечего, и это не то же самое, что «нарушений нет»")
	}

	runSet := map[string]bool{}
	for _, s := range run {
		runSet[s] = true
	}
	declaredSet := map[string]bool{}
	for _, s := range declared {
		declaredSet[s] = true
	}

	for _, s := range declared {
		if !runSet[s] {
			t.Errorf("services/%s объявляет цель %s, но CI её не зовёт — страж есть, исполнять его некому "+
				"(ровно так iam и оставался вне гейта)", s, listFilterTarget)
		}
	}
	for _, s := range run {
		if !declaredSet[s] {
			t.Errorf("CI зовёт %s для services/%s, но такой цели там не объявлено — шаг позеленеет ни на чём",
				listFilterTarget, s)
		}
	}

	t.Logf("перепись: объявляют цель %d (%s); зовёт CI %d (%s)",
		len(declared), strings.Join(declared, ", "), len(run), strings.Join(run, ", "))
}
