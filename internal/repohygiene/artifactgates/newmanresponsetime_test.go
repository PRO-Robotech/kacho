// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// newmanresponsetime — функциональный набор newman не судит время ответа
// (kacho#3263).
//
// Бюджет времени ответа в функциональном кейсе судит не поведение края, а
// загрузку машины прогона: под параллельным прогоном волны `vpc/security-group`
// краснел на `response time < 500ms` («expected 766 to be below 500») и
// `< 300ms` («expected 1057 to be below 300») при неизменном продукте. Вердикт
// набора становился функцией параллельности — это недействительный вердикт, а не
// красный. Бюджет перенесён в нагрузочный набор, где условия замера названы
// (подача, длительность, перцентиль): `services/vpc/tests/k6/resource_ops.js`,
// полосы чтения `<ресурс>_get` и `<ресурс>_list`.
//
// Предикат — ни одна исполняемая строка источника или коллекции newman не
// читает `responseTime` ответа. Комментарий о нём — не утверждение и молчит.
// Обход — отслеживаемые файлы наборов newman (`.py`, `.js`, `.json`); единица
// счёта — строка.

// responseTimeCensus — объём осмотренного: «ноль находок» отличим от «ноль
// прочитанного».
type responseTimeCensus struct {
	files int
	lines int
}

// isNewmanSuiteFile — файл набора newman: источник кейсов, генератор, общая
// библиотека или порождённая коллекция.
func isNewmanSuiteFile(rel string) bool {
	if !strings.HasPrefix(rel, "tests/newman/") && !strings.Contains(rel, "/tests/newman/") {
		return false
	}
	switch filepath.Ext(rel) {
	case ".py", ".js", ".json":
		return true
	}
	return false
}

// responseTimeFindings — строки, читающие время ответа. Строка-комментарий
// (`#` в Python, `//` в JS) — проза о свойстве, а не его утверждение.
func responseTimeFindings(rel string, body []byte, census *responseTimeCensus) []string {
	census.files++
	var out []string
	for i, line := range strings.Split(string(body), "\n") {
		census.lines++
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.Contains(line, ".responseTime") {
			out = append(out, fmt.Sprintf("%s:%d: %s", rel, i+1, trimmed))
		}
	}
	return out
}

// TestNewmanFunctionalSuiteDoesNotJudgeResponseTime — гейт по дереву: ни один
// набор newman не утверждает время ответа.
func TestNewmanFunctionalSuiteDoesNotJudgeResponseTime(t *testing.T) {
	t.Parallel()
	tt := newTrackedTree(t, repoRoot(t))
	var rels []string
	for rel := range tt.files {
		if isNewmanSuiteFile(rel) {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	var census responseTimeCensus
	var findings []string
	for _, rel := range rels {
		body, err := os.ReadFile(filepath.Join(tt.root, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		findings = append(findings, responseTimeFindings(rel, body, &census)...)
	}
	t.Logf("перепись: файлов наборов newman %d · строк %d · находок %d", census.files, census.lines,
		len(findings))
	// Предпосылка обхода: пустой обход — не вердикт.
	if census.files == 0 || census.lines == 0 {
		t.Fatalf("обход не нашёл ни одного файла набора newman (файлов %d, строк %d) — судить нечего, "+
			"это не зелёный", census.files, census.lines)
	}
	for _, f := range findings {
		t.Errorf("функциональный кейс судит время ответа — вердикт зависит от загрузки машины прогона; "+
			"бюджет живёт в нагрузочном наборе с названными условиями замера: %s", f)
	}
}

// vpcReadBudgetPaths — пути чтения, чей бюджет перенесён из функционального
// набора vpc (кейсы `*-GET-PERF-BASELINE`, `*-LST-PERF-BASELINE` шести
// ресурсов) в нагрузочный.
var vpcReadBudgetPaths = []string{
	"/vpc/v1/networks",
	"/vpc/v1/subnets",
	"/vpc/v1/addresses",
	"/vpc/v1/gateways",
	"/vpc/v1/routeTables",
	"/vpc/v1/securityGroups",
}

// vpcLoadReadLaneFile — нагрузочный набор, держащий бюджет чтения.
const vpcLoadReadLaneFile = "services/vpc/tests/k6/resource_ops.js"

// TestVpcReadBudgetLivesInTheLoadSuite — перенесённый бюджет не потерян: у
// каждого из шести путей есть полоса чтения в нагрузочном наборе, а сам набор
// судит её порогом перцентиля.
func TestVpcReadBudgetLivesInTheLoadSuite(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, vpcLoadReadLaneFile))
	if err != nil {
		t.Fatalf("%s: %v — бюджету чтения негде жить", vpcLoadReadLaneFile, err)
	}
	src := string(body)
	if !strings.Contains(src, "threshold: `p(99)<${BUDGET_MS}`") {
		t.Errorf("%s: нет порога перцентиля по бюджету — полоса меряет, но не судит", vpcLoadReadLaneFile)
	}
	for _, p := range vpcReadBudgetPaths {
		if !strings.Contains(src, "'"+p+"'") {
			t.Errorf("%s: у пути %s нет полосы чтения — бюджет снят с функционального набора и не "+
				"доехал до нагрузочного", vpcLoadReadLaneFile, p)
		}
	}
}
