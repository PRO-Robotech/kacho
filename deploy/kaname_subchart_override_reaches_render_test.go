// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// kaname_subchart_override_reaches_render_test.go — каждая величина подчарта
// службы доступа, которую зонт переопределяет, доходит до РЕНДЕРА подчарта на
// каждой цепочке таблицы стендов (kacho#3314).
//
// Урок сборщика 2026-10-19 (kacho#3291): величина подчарта, переопределённая
// зонтом, не доезжает до стенда, а проба одного чарта этого не видит — она
// читает умолчания подчарта, а не то, что ему передаёт цепочка. Здесь
// переопределение судится ИСХОДОМ: подчарт рендерится значениями цепочки
// (секция `kaname` и `global` зонта, слитые так, как их сливает helm), затем —
// теми же значениями с ОДНОЙ изменённой величиной. Рендер не изменился — величина
// до рендера не доходит: зонт объявляет то, чего стенд не исполнит.
//
// Изменение — того же вида (строка + «-probe», число + 1, флаг — отрицание).
// Отказ рендера на изменённой величине — величина ЧИТАЕТСЯ (страж её судил), это
// не находка. Списки и пустые узлы не меняются и не считаются.
//
// Намеренное — в разделе `overrides` ведомости kaname-chart-differences.yaml с
// причиной; запись, не мёртвая ни на одной цепочке, — находка (снять). Перепись
// печатается; ноль сравнённых величин — отказ.
package deploy_test

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// kanameOverride — строка раздела `overrides` ведомости.
type kanameOverride struct {
	Value string `yaml:"value"` // путь величины в значениях подчарта
	Why   string `yaml:"why"`
}

func readKanameOverrideLedger(t *testing.T) []kanameOverride {
	t.Helper()
	b, err := os.ReadFile(kanameChartDifferencesLedger)
	if err != nil {
		t.Fatalf("ведомость %s не прочитана: %v", kanameChartDifferencesLedger, err)
	}
	var doc struct {
		Overrides []kanameOverride `yaml:"overrides"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("ведомость %s не разобрана: %v", kanameChartDifferencesLedger, err)
	}
	return doc.Overrides
}

// scalarLeaves — листовые скаляры узла: путь → величина.
func scalarLeaves(node map[string]any, prefix []string, out map[string]any) {
	for k, v := range node {
		p := append(append([]string{}, prefix...), k)
		if m, ok := v.(map[string]any); ok {
			if len(m) > 0 {
				scalarLeaves(m, p, out)
			}
			continue
		}
		out[strings.Join(p, ".")] = v
	}
}

// perturbScalar — величина того же вида, отличная от данной; false — не меняется.
func perturbScalar(v any) (any, bool) {
	switch x := v.(type) {
	case bool:
		return !x, true
	case int:
		return x + 1, true
	case float64:
		return x + 1.5, true
	case string:
		if x == "" {
			return "probe", true
		}
		return x + "-probe", true
	}
	return nil, false
}

func deepCopyValues(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = deepCopyValues(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = deepCopyValues(e)
		}
		return out
	}
	return v
}

func setValuePath(tree map[string]any, path string, v any) {
	keys := strings.Split(path, ".")
	cur := tree
	for _, k := range keys[:len(keys)-1] {
		next, _ := cur[k].(map[string]any)
		cur = next
	}
	cur[keys[len(keys)-1]] = v
}

// judgeOverridesReachRender — пути объявленных зонтом величин, изменение которых
// рендера не меняет. render — рендер подчарта значениями; compared — сколько
// величин изменено и сравнено.
func judgeOverridesReachRender(sub map[string]any, declared []string,
	render func(map[string]any) (string, error)) (dead []string, compared int, err error) {
	base, err := render(sub)
	if err != nil {
		return nil, 0, fmt.Errorf("рендер цепочки без изменений отказал: %w\n%s", err, base)
	}
	eff := map[string]any{}
	scalarLeaves(sub, nil, eff)
	for _, p := range declared {
		cur, ok := eff[p]
		if !ok {
			continue
		}
		nv, ok := perturbScalar(cur)
		if !ok {
			continue
		}
		compared++
		v2, _ := deepCopyValues(sub).(map[string]any)
		setValuePath(v2, p, nv)
		out, rerr := render(v2)
		if rerr == nil && out == base {
			dead = append(dead, p)
		}
	}
	sort.Strings(dead)
	return dead, compared, nil
}

// stackSubchartValues — значения подчарта цепочки и пути величин, которые зонт
// объявляет под `kaname` (умолчания зонта и каждый слой цепочки).
func stackSubchartValues(t *testing.T, chain []string) (sub map[string]any, declared []string) {
	t.Helper()
	layers := []map[string]any{readYAML(t, umbrellaDir+"/values.yaml")}
	for _, p := range chain {
		layers = append(layers, readYAML(t, umbrellaDir+"/"+p))
	}
	merged := map[string]any{}
	seen := map[string]bool{}
	for _, l := range layers {
		merged = mergeValues(merged, l)
		if k, ok := l["kaname"].(map[string]any); ok {
			leaves := map[string]any{}
			scalarLeaves(k, nil, leaves)
			for p := range leaves {
				seen[p] = true
			}
		}
	}
	sub = map[string]any{}
	if g, ok := merged["global"].(map[string]any); ok {
		sub["global"] = g
	}
	if k, ok := merged["kaname"].(map[string]any); ok {
		sub = mergeValues(sub, k)
	}
	for p := range seen {
		declared = append(declared, p)
	}
	sort.Strings(declared)
	return sub, declared
}

func TestKanameSubchartOverridesReachTheRenderOnEveryChain(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	ledger := readKanameOverrideLedger(t)
	excused := map[string]string{}
	for _, o := range ledger {
		if strings.TrimSpace(o.Why) == "" {
			t.Errorf("ведомость: величина «%s» без причины", o.Value)
		}
		excused[o.Value] = o.Why
	}
	used := map[string]bool{}
	total := 0
	for _, name := range names {
		sub, declared := stackSubchartValues(t, stacks[name])
		dead, compared, err := judgeOverridesReachRender(sub, declared, func(v map[string]any) (string, error) {
			return renderStackSubchart(t, name, v)
		})
		if err != nil {
			t.Errorf("стек %s: %v", name, err)
			continue
		}
		total += compared
		t.Logf("стек %s: объявлено зонтом величин %d, изменено и сравнено %d, до рендера не доходит %d", name, len(declared), compared, len(dead))
		for _, p := range dead {
			if _, ok := excused[p]; ok {
				used[p] = true
				continue
			}
			t.Errorf("стек %s: величина kaname.%s объявлена зонтом, а рендер подчарта от её изменения не меняется — "+
				"до стенда она не доходит; снять объявление либо назвать в %s (overrides) с причиной",
				name, p, kanameChartDifferencesLedger)
		}
	}
	for _, o := range ledger {
		if !used[o.Value] {
			t.Errorf("ведомость: величина «%s» доходит до рендера на каждой цепочке — запись без предмета снять", o.Value)
		}
	}
	t.Logf("перепись: цепочек %d · величин сравнено %d · записей ведомости %d", len(names), total, len(ledger))
	if total == 0 {
		t.Fatal("сравнено величин 0 — вердикт беспредметен")
	}
}

// Инъекция настоящим рендером: величина, которую зонт объявил, а подчарт не читает,
// — находка; законный близнец — величина, которую подчарт читает.
func TestKanameOverrideReachInjection_UnreadDeclaredValueIsFoundAndReadOneIsSilent(t *testing.T) {
	sub, _ := stackSubchartValues(t, deployStacks(t)["dev-prod"])
	sub["probeOnlyNotRead"] = "x"
	render := func(v map[string]any) (string, error) { return renderStackSubchart(t, "inject", v) }
	dead, n, err := judgeOverridesReachRender(sub, []string{"probeOnlyNotRead", "config.logger.level"}, render)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(dead) != 1 || dead[0] != "probeOnlyNotRead" {
		t.Fatalf("ждали одну находку probeOnlyNotRead из двух сравнённых, получили %v (сравнено %d)", dead, n)
	}
}

func TestKanameOverrideReachInjection_NothingComparedIsNotGreen(t *testing.T) {
	render := func(map[string]any) (string, error) { return "same", nil }
	if _, n, _ := judgeOverridesReachRender(map[string]any{"a": []any{1}}, []string{"a"}, render); n != 0 {
		t.Fatalf("список не меняется и не считается: сравнено %d", n)
	}
}
