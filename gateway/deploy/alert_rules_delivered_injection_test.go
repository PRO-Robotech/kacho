// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// alert_rules_delivered_injection_test.go — сверка правил тревоги края со
// страницей СПОСОБНА упасть и молчит на законных близнецах.
//
// СКВОЗНАЯ инъекция — настоящий чарт, настоящая страница, ровно один
// изменённый факт: выключатель объекта выключен при вердикте «включено».
// СИНТЕТИЧЕСКИЕ — расхождения, которых в дереве нет: лишнее правило, уехавшее
// выражение, разошедшийся текст, ряд без производителя. Близнец про отступ
// доказывает, что нормализация выражения не краснеет на верной поставке.
package deploy_test

import (
	"strings"
	"testing"
)

func syntheticEdgeRendered(rules string) string {
	return "apiVersion: v1\nkind: Service\nmetadata:\n  name: api-gateway\n---\n" +
		"apiVersion: monitoring.coreos.com/v1\nkind: PrometheusRule\n" +
		"metadata:\n  name: api-gateway\nspec:\n  groups:\n    - name: g\n      rules:\n" + rules
}

const syntheticEdgeRuleGood = "        - alert: SampleStuck\n" +
	"          expr: kacho_api_gateway_sample_total > 1\n" +
	"          for: 5m\n" +
	"          annotations:\n" +
	"            summary: \"проба\"\n"

const syntheticEdgePage = "текст\n```yaml\n- alert: SampleStuck\n" +
	"  expr: kacho_api_gateway_sample_total > 1\n" +
	"  for: 5m\n" +
	"  annotations:\n" +
	"    summary: \"проба\"\n```\n"

func syntheticEdgePageRules(t *testing.T) []edgeAlertRule {
	t.Helper()
	rules, err := edgePageRulesFromText(syntheticEdgePage)
	if err != nil || len(rules) != 1 {
		t.Fatalf("фикстура страницы беспредметна: %v, правил %d", err, len(rules))
	}
	return rules
}

func judgeSynthetic(t *testing.T, rules string) []string {
	t.Helper()
	rendered := syntheticEdgeRendered(rules)
	chart, objects := edgeChartAlertRules(t, rendered)
	return judgeEdgeAlertRender(syntheticEdgePageRules(t), rendered, chart, objects, true)
}

// TestEdgeAlertRulesInjection_ControlIsSilent — контроль.
func TestEdgeAlertRulesInjection_ControlIsSilent(t *testing.T) {
	if f := judgeSynthetic(t, syntheticEdgeRuleGood); len(f) != 0 {
		t.Fatalf("сверка краснеет на совпадающих наборах: %v", f)
	}
}

// TestEdgeAlertRulesInjection_ChartRuleThePageDoesNotExplain — лишнее у объекта.
func TestEdgeAlertRulesInjection_ChartRuleThePageDoesNotExplain(t *testing.T) {
	extra := syntheticEdgeRuleGood + "        - alert: SampleUndocumented\n" +
		"          expr: kacho_api_gateway_sample_total > 2\n" +
		"          annotations:\n            summary: \"нигде\"\n"
	f := strings.Join(judgeSynthetic(t, extra), "\n")
	if !strings.Contains(f, "SampleUndocumented") {
		t.Fatalf("правило без объяснения на странице прошло молча: %q", f)
	}
}

// TestEdgeAlertRulesInjection_ExpressionDriftIsNamedOnBothSides — имена
// совпали, порог уехал: сверка по одному имени была бы здесь зелёной.
func TestEdgeAlertRulesInjection_ExpressionDriftIsNamedOnBothSides(t *testing.T) {
	drifted := strings.Replace(syntheticEdgeRuleGood, "> 1", "> 999", 1)
	if drifted == syntheticEdgeRuleGood {
		t.Fatal("инъекция не применилась")
	}
	f := judgeSynthetic(t, drifted)
	joined := strings.Join(f, "\n")
	if len(f) != 2 || !strings.Contains(joined, "не везёт: SampleStuck") || !strings.Contains(joined, "нет: SampleStuck") {
		t.Fatalf("уехавший порог не назван с обеих сторон: %v", f)
	}
}

// TestEdgeAlertRulesInjection_SummaryDriftIsAFinding — текст дежурного тоже часть правила.
func TestEdgeAlertRulesInjection_SummaryDriftIsAFinding(t *testing.T) {
	drifted := strings.Replace(syntheticEdgeRuleGood, "\"проба\"", "\"другое\"", 1)
	if len(judgeSynthetic(t, drifted)) == 0 {
		t.Fatal("разошедшийся текст для дежурного прошёл молча")
	}
}

// TestEdgeAlertRulesInjection_IndentationIsNotDrift — законный близнец.
func TestEdgeAlertRulesInjection_IndentationIsNotDrift(t *testing.T) {
	wrapped := "        - alert: SampleStuck\n" +
		"          expr: |\n" +
		"            kacho_api_gateway_sample_total\n" +
		"              > 1\n" +
		"          for: 5m\n" +
		"          annotations:\n" +
		"            summary: \"проба\"\n"
	if f := judgeSynthetic(t, wrapped); len(f) != 0 {
		t.Fatalf("перенос и отступ объявлены расхождением: %v", f)
	}
}

// TestEdgeAlertRulesInjection_MissingObjectWhileEnabledIsAFinding — объекта нет,
// а вердикт «включено»: обещание без поставки.
func TestEdgeAlertRulesInjection_MissingObjectWhileEnabledIsAFinding(t *testing.T) {
	rendered := "apiVersion: v1\nkind: Service\nmetadata:\n  name: x\n"
	chart, objects := edgeChartAlertRules(t, rendered)
	f := judgeEdgeAlertRender(syntheticEdgePageRules(t), rendered, chart, objects, true)
	if !strings.Contains(strings.Join(f, "\n"), "объектов правил 0") {
		t.Fatalf("включённый выключатель без объекта прошёл молча: %v", f)
	}
}

// TestEdgeAlertRulesInjection_ObjectWhileDisabledIsAFinding — объект в рендере
// при выключенном выключателе: на кластере без CRD это отказ всей установки.
func TestEdgeAlertRulesInjection_ObjectWhileDisabledIsAFinding(t *testing.T) {
	rendered := syntheticEdgeRendered(syntheticEdgeRuleGood)
	chart, objects := edgeChartAlertRules(t, rendered)
	if f := judgeEdgeAlertRender(nil, rendered, chart, objects, false); len(f) == 0 {
		t.Fatal("объект при выключенном выключателе прошёл молча")
	}
	if f := judgeEdgeAlertRender(nil, "kind: Service\n", nil, 0, false); len(f) != 0 {
		t.Fatalf("законный близнец (выключено и объекта нет) назван находкой: %v", f)
	}
}

// TestEdgeAlertRulesInjection_TheRealChartSwitchedOffDeliversNothing — СКВОЗНАЯ:
// настоящий чарт выключен, вердикт судит его как включённый — каждое правило
// страницы обязано быть названо недоставленным.
func TestEdgeAlertRulesInjection_TheRealChartSwitchedOffDeliversNothing(t *testing.T) {
	page := edgePageRules(t)
	if len(page) == 0 {
		t.Fatal("инъекция беспредметна: страница не несёт правил")
	}
	off, err := renderEdgeChain(t, edgeAlertChain{name: "chart"},
		map[string]any{"alertRules": map[string]any{"enabled": false, "disabledBecause": "инъекция"}})
	if err != nil {
		t.Fatalf("рендер выключенного чарта: %v\n%s", err, off)
	}
	chart, objects := edgeChartAlertRules(t, off)
	if objects != 0 {
		t.Fatalf("инъекция не применилась: объект остался (%d)", objects)
	}
	onlyPage, _ := diffEdgeRuleSets(page, chart)
	t.Logf("ПЕРЕПИСЬ сквозной инъекции: правил на странице %d · у объекта %d · только на странице %d",
		len(page), len(chart), len(onlyPage))
	if len(onlyPage) != len(page) {
		t.Fatalf("сверка не назвала все обещанные правила недоставленными (%d из %d) — она вакуумна",
			len(onlyPage), len(page))
	}
	if !strings.Contains(strings.Join(onlyPage, ","), "KachoApiGatewayAnonMailSaturated") {
		t.Fatalf("среди недоставленных не названо правило насыщения (Д66): %v", onlyPage)
	}
}

// TestEdgeAlertRulesInjection_SeriesWithoutProducerIsFound — ряд, которого край
// не производит, назван; ряд по границе имени (с суффиксом) — не засчитан.
func TestEdgeAlertRulesInjection_SeriesWithoutProducerIsFound(t *testing.T) {
	produced := map[string]bool{"kacho_api_gateway_sample_total": true}
	rules := []edgeAlertRule{
		{Alert: "A", Expr: "increase(kacho_api_gateway_sample_total[5m]) > 0"},
		{Alert: "B", Expr: "increase(kacho_api_gateway_sample_total_xx[5m]) > 0"},
	}
	named, missing := unproducedEdgeSeries(rules, produced)
	if len(named) != 2 || len(missing) != 1 || missing[0] != "kacho_api_gateway_sample_total_xx" {
		t.Fatalf("ряд без производителя не назван по границе имени: названо %v, без производителя %v", named, missing)
	}
	if _, m := unproducedEdgeSeries(rules[:1], produced); len(m) != 0 {
		t.Fatalf("законный близнец назван находкой: %v", m)
	}
}
