// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// alert_rules_delivered_test.go — правила тревоги края ПОСТАВЛЯЮТСЯ ОБЪЕКТОМ,
// объект не расходится с опубликованной страницей, и выключатель объекта
// работает на каждой цепочке стендов (NTF-2, решение Д66/Д69).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ УТВЕРЖДАЕТСЯ
//
//	Р1  на каждой цепочке deploy/stacks.txt и на чарте как есть: включённый
//	    объект — ровно один, непустой, и его набор правил СОВПАДАЕТ с набором
//	    страницы в обе стороны (имя, выражение, выдержка, текст для дежурного);
//	    выключенный — не оставляет в рендере следа и называет причину;
//	Р2  каждая цепочка рендерится с кодом 0 при ОБОИХ положениях выключателя:
//	    включение на цепочке без него и выключение на цепочке с ним — законные
//	    правки профиля, и ни одна не должна ронять рендер;
//	Р3  выключение без причины рендер отвергает (и законный близнец с причиной
//	    проходит);
//	Р4  каждый ряд, названный выражением правила, имеет производителя в
//	    не-тестовом коде края — строковый литерал имени серии, найденный
//	    разбором Go, а не поиском по тексту.
//
// Перепись печатается числами; пустая страница, пустой обход цепочек и ни
// одной включённой цепочки роняют прогон: «расхождений ноль» на пустом входе
// вердиктом не является.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ЗАКРЫВАЕТ
//
// Она не утверждает, что на кластере цепочки CRD PrometheusRule есть или нет:
// это свойство кластера, и профиль называет его причиной выключения (замер или
// «не измерено»). И не утверждает верность порога: числа нагрузочного прогона
// звена нет, порог консервативный — это записано в шаблоне и на странице.
package deploy_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// edgeObservabilityPage — опубликованная страница наблюдаемости края
// относительно этого пакета.
var edgeObservabilityPage = filepath.Join("..", "docs", "content", "install", "observability.mdx")

// edgeAlertPageBlockRe — блок кода страницы. Берётся блок, а не строки: имя
// `alert:` встречается и в прозе вокруг.
var edgeAlertPageBlockRe = regexp.MustCompile("(?s)```yaml\n(.*?)```")

// edgeSeriesRe — имя серии края в выражении правила, по границе имени.
var edgeSeriesRe = regexp.MustCompile(`\bkacho_api_gateway_[a-z0-9_]+\b`)

// edgeAlertRule — правило в том виде, в каком его сверяют две стороны.
type edgeAlertRule struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for"`
	Annotations map[string]string `yaml:"annotations"`
}

// key — предмет сверки. Выражение нормализуется по пробелам: отступ блочного
// скаляра у страницы и у объекта разный by construction, а различие ТЕКСТА
// остаётся видимым.
func (r edgeAlertRule) key() string {
	return r.Alert + "\x00" + strings.Join(strings.Fields(r.Expr), " ") +
		"\x00" + r.For + "\x00" + r.Annotations["summary"]
}

func parseEdgeAlertRules(text string) ([]edgeAlertRule, error) {
	var out []edgeAlertRule
	if err := yaml.Unmarshal([]byte(text), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// edgePageRulesFromText — правила из текста страницы.
func edgePageRulesFromText(text string) ([]edgeAlertRule, error) {
	var out []edgeAlertRule
	for _, m := range edgeAlertPageBlockRe.FindAllStringSubmatch(text, -1) {
		if !strings.Contains(m[1], "- alert:") {
			continue
		}
		parsed, err := parseEdgeAlertRules(m[1])
		if err != nil {
			return nil, err
		}
		out = append(out, parsed...)
	}
	return out, nil
}

func edgePageRules(t *testing.T) []edgeAlertRule {
	t.Helper()
	raw, err := os.ReadFile(edgeObservabilityPage) // #nosec G304 -- страница этого дерева
	if err != nil {
		t.Fatalf("опубликованная страница наблюдаемости края не читается (%v): %s", err, edgeObservabilityPage)
	}
	rules, err := edgePageRulesFromText(string(raw))
	if err != nil {
		t.Fatalf("блок правил страницы не разбирается как YAML (%v): %s", err, edgeObservabilityPage)
	}
	return rules
}

// edgeChartAlertRules — правила всех объектов PrometheusRule рендера и число
// таких объектов. Документ читается разбором, а не поиском строки.
func edgeChartAlertRules(t *testing.T, rendered string) (rules []edgeAlertRule, objects int) {
	t.Helper()
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("рендер не разбирается как YAML: %v", err)
		}
		if doc == nil || doc["kind"] != "PrometheusRule" {
			continue
		}
		objects++
		spec, _ := doc["spec"].(map[string]any)
		groups, _ := spec["groups"].([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			raw, err := yaml.Marshal(gm["rules"])
			if err != nil {
				t.Fatalf("правила объекта не пересобираются: %v", err)
			}
			parsed, perr := parseEdgeAlertRules(string(raw))
			if perr != nil {
				t.Fatalf("правила объекта не разбираются: %v", perr)
			}
			rules = append(rules, parsed...)
		}
	}
	return rules, objects
}

// diffEdgeRuleSets — что есть у одной стороны и нет у другой, в обе стороны.
func diffEdgeRuleSets(page, chart []edgeAlertRule) (onlyPage, onlyChart []string) {
	inChart, inPage := map[string]bool{}, map[string]bool{}
	for _, r := range chart {
		inChart[r.key()] = true
	}
	for _, r := range page {
		inPage[r.key()] = true
	}
	for _, r := range page {
		if !inChart[r.key()] {
			onlyPage = append(onlyPage, r.Alert)
		}
	}
	for _, r := range chart {
		if !inPage[r.key()] {
			onlyChart = append(onlyChart, r.Alert)
		}
	}
	sort.Strings(onlyPage)
	sort.Strings(onlyChart)
	return onlyPage, onlyChart
}

// edgeAlertChain — что рендерится: чарт как есть (chain == nil) либо цепочка
// стенда, наложенная на базу зонта.
type edgeAlertChain struct {
	name  string
	chain []string
}

func edgeAlertChains(t *testing.T) []edgeAlertChain {
	t.Helper()
	stacks := deployableStacks(t)
	out := []edgeAlertChain{{name: "chart"}}
	for _, name := range sortedStackNames(stacks) {
		out = append(out, edgeAlertChain{name: name, chain: stacks[name]})
	}
	return out
}

// edgeLayers — слои значений края для цепочки: поддерево края и `global` из
// базы зонта и каждого профиля, ровно так, как их получает подчарт.
func edgeLayers(t *testing.T, c edgeAlertChain) []map[string]any {
	t.Helper()
	if c.chain == nil {
		return nil
	}
	var out []map[string]any
	for _, profile := range append([]string{"values.yaml"}, c.chain...) {
		tree := umbrellaValues(t, profile)
		layer := map[string]any{}
		if sub, ok := tree["api-gateway"].(map[string]any); ok {
			layer = mergeInto(layer, sub)
		}
		if g, ok := tree["global"]; ok {
			layer["global"] = g
		}
		out = append(out, layer)
	}
	return out
}

// edgeAlertSwitch — положение выключателя на цепочке: умолчание чарта,
// затем слои по порядку.
func edgeAlertSwitch(t *testing.T, c edgeAlertChain) (enabled bool, reason string) {
	t.Helper()
	merged := mergeInto(map[string]any{}, gatewayChartValues(t))
	for _, l := range edgeLayers(t, c) {
		merged = mergeInto(merged, l)
	}
	ar, _ := merged["alertRules"].(map[string]any)
	enabled, _ = ar["enabled"].(bool)
	reason, _ = ar["disabledBecause"].(string)
	return enabled, reason
}

// renderEdgeChain рендерит чарт края слоями цепочки и extra поверх. Ошибка
// рендера возвращается: часть проб утверждает именно отказ.
func renderEdgeChain(t *testing.T, c edgeAlertChain, extra ...map[string]any) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-гейт пропущен")
	}
	dir := t.TempDir()
	args := []string{"template", "api-gateway", ".", "-n", "kacho"}
	for i, layer := range append(edgeLayers(t, c), extra...) {
		raw, err := yaml.Marshal(layer)
		if err != nil {
			t.Fatalf("слой %d цепочки %s не сериализовался: %v", i, c.name, err)
		}
		path := filepath.Join(dir, fmt.Sprintf("%02d.yaml", i))
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatalf("слой %d не записан: %v", i, err)
		}
		args = append(args, "-f", path)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева
	return string(out), err
}

// judgeEdgeAlertRender — вердикт рендера при известном положении выключателя.
func judgeEdgeAlertRender(page []edgeAlertRule, rendered string, rules []edgeAlertRule, objects int, enabled bool) []string {
	var findings []string
	if !enabled {
		if objects != 0 || strings.Contains(rendered, "PrometheusRule") {
			findings = append(findings, fmt.Sprintf("выключатель выключен, а объект в рендере (%d) — "+
				"на кластере без CRD установка упадёт целиком", objects))
		}
		return findings
	}
	if objects != 1 {
		findings = append(findings, fmt.Sprintf("объектов правил %d, ожидается ровно один", objects))
	}
	if len(rules) == 0 {
		findings = append(findings, "объект правил пуст")
	}
	onlyPage, onlyChart := diffEdgeRuleSets(page, rules)
	if len(onlyPage) > 0 {
		findings = append(findings, "страница обещает правила, которых объект не везёт: "+strings.Join(onlyPage, ", "))
	}
	if len(onlyChart) > 0 {
		findings = append(findings, "объект везёт правила, которых на странице нет: "+strings.Join(onlyChart, ", "))
	}
	return findings
}

// TestEdgeAlertRules_DeliveredSetMatchesThePublishedPage — Р1.
func TestEdgeAlertRules_DeliveredSetMatchesThePublishedPage(t *testing.T) {
	page := edgePageRules(t)
	if len(page) == 0 {
		t.Fatalf("страница %s не несёт ни одного правила — сверять нечего, вердикт беспредметен", edgeObservabilityPage)
	}
	chains := edgeAlertChains(t)
	enabledStacks, disabled := 0, 0
	for _, c := range chains {
		enabled, reason := edgeAlertSwitch(t, c)
		rendered, err := renderEdgeChain(t, c)
		if err != nil {
			t.Errorf("[%s] рендер не прошёл: %v\n%s", c.name, err, rendered)
			continue
		}
		rules, objects := edgeChartAlertRules(t, rendered)
		t.Logf("[%s] выключатель %v · объектов %d · правил у объекта %d · правил на странице %d · причина выключения %q",
			c.name, enabled, objects, len(rules), len(page), reason)
		if enabled {
			if c.chain != nil {
				enabledStacks++
			}
		} else {
			disabled++
			if strings.TrimSpace(reason) == "" {
				t.Errorf("[%s] выключено без причины", c.name)
			}
		}
		for _, f := range judgeEdgeAlertRender(page, rendered, rules, objects, enabled) {
			t.Errorf("[%s] %s", c.name, f)
		}
	}
	t.Logf("ПЕРЕПИСЬ: цепочек осмотрено %d (с чартом как есть) · включено на стендах %d · выключено %d",
		len(chains), enabledStacks, disabled)
	if len(chains) < 2 {
		t.Fatalf("обход цепочек пуст — «ноль находок» означал бы «ноль прочитанного»")
	}
	if enabledStacks == 0 {
		t.Fatalf("ни на одной цепочке стендов объект правил не включён — поставка не едет никуда, " +
			"а страница её обещает")
	}
}

// TestEdgeAlertRules_EveryChainRendersWithTheSwitchEitherWay — Р2: обратное
// положение выключателя на каждой цепочке рендерится, и его исход судится
// тем же вердиктом.
func TestEdgeAlertRules_EveryChainRendersWithTheSwitchEitherWay(t *testing.T) {
	page := edgePageRules(t)
	chains := edgeAlertChains(t)
	for _, c := range chains {
		enabled, _ := edgeAlertSwitch(t, c)
		flip := map[string]any{"alertRules": map[string]any{"enabled": true}}
		if enabled {
			flip = map[string]any{"alertRules": map[string]any{
				"enabled": false, "disabledBecause": "проба выключателя"}}
		}
		rendered, err := renderEdgeChain(t, c, flip)
		if err != nil {
			t.Errorf("[%s] рендер с выключателем %v не прошёл: %v\n%s", c.name, !enabled, err, rendered)
			continue
		}
		rules, objects := edgeChartAlertRules(t, rendered)
		t.Logf("[%s] обратное положение %v · объектов %d · правил %d", c.name, !enabled, objects, len(rules))
		for _, f := range judgeEdgeAlertRender(page, rendered, rules, objects, !enabled) {
			t.Errorf("[%s, выключатель %v] %s", c.name, !enabled, f)
		}
	}
	if len(chains) < 2 {
		t.Fatalf("обход цепочек пуст")
	}
}

// TestEdgeAlertRules_DisabledWithoutReasonIsRefused — Р3 и законный близнец.
func TestEdgeAlertRules_DisabledWithoutReasonIsRefused(t *testing.T) {
	chart := edgeAlertChain{name: "chart"}
	out, err := renderEdgeChain(t, chart, map[string]any{"alertRules": map[string]any{"enabled": false}})
	if err == nil {
		t.Fatalf("выключение без причины отрендерилось — молчащая тревога неотличима от «забыли»:\n%s", out)
	}
	if !strings.Contains(out, "alertRules.disabledBecause") {
		t.Fatalf("отказ рендера не называет ручку причины:\n%s", out)
	}
	twin, err := renderEdgeChain(t, chart, map[string]any{"alertRules": map[string]any{
		"enabled": false, "disabledBecause": "оператора нет"}})
	if err != nil {
		t.Fatalf("законный близнец (выключено с причиной) отвергнут: %v\n%s", err, twin)
	}
	if _, objects := edgeChartAlertRules(t, twin); objects != 0 {
		t.Fatalf("выключенный с причиной объект остался в рендере (%d)", objects)
	}
}

// edgeProducedSeries — строковые литералы не-тестового Go края, найденные
// разбором файлов (узел BasicLit STRING), а не поиском по тексту.
func edgeProducedSeries(t *testing.T, roots ...string) (map[string]bool, int) {
	t.Helper()
	lits := map[string]bool{}
	files := 0
	fset := token.NewFileSet()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if perr != nil {
				return perr
			}
			files++
			ast.Inspect(f, func(n ast.Node) bool {
				if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					if s, uerr := strconv.Unquote(bl.Value); uerr == nil {
						lits[s] = true
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", root, err)
		}
	}
	return lits, files
}

// unproducedEdgeSeries — ряды выражений правил, которых производитель не
// объявляет.
func unproducedEdgeSeries(rules []edgeAlertRule, produced map[string]bool) (named []string, missing []string) {
	seen := map[string]bool{}
	for _, r := range rules {
		for _, s := range edgeSeriesRe.FindAllString(r.Expr, -1) {
			if seen[s] {
				continue
			}
			seen[s] = true
			named = append(named, s)
			if !produced[s] {
				missing = append(missing, s)
			}
		}
	}
	sort.Strings(named)
	sort.Strings(missing)
	return named, missing
}

// TestEdgeAlertRules_EveryRuleNamesASeriesTheEdgeProduces — Р4.
func TestEdgeAlertRules_EveryRuleNamesASeriesTheEdgeProduces(t *testing.T) {
	page := edgePageRules(t)
	produced, files := edgeProducedSeries(t, filepath.Join("..", "internal"), filepath.Join("..", "cmd"))
	named, missing := unproducedEdgeSeries(page, produced)
	t.Logf("ПЕРЕПИСЬ: файлов Go края разобрано %d · строковых литералов %d · рядов в правилах %d · без производителя %d",
		files, len(produced), len(named), len(missing))
	if files == 0 || len(named) == 0 {
		t.Fatalf("обход пуст (файлов %d, рядов %d) — вердикт беспредметен", files, len(named))
	}
	if len(missing) > 0 {
		t.Fatalf("правила называют ряды, которых край не производит: %s — такое правило не звонит никогда",
			strings.Join(missing, ", "))
	}
}
