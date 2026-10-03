// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// ka1_call_budgets_render_test.go — приёмка KA1, сценарий KA1-25 (Р4): каждый
// стенд объявляет обе ручки бюджета вызовов края.
//
// # Единица отбора — СТЕНД, а не файл профиля
//
// Профиль поставки — слой: большинство профилей накладываются поверх другого и
// в одиночку не отрисовываются вовсе. Поэтому множество стендов — строки таблицы
// `stacks.txt`, прочитанные её единственным читателем (`deployStacks`), а цепочка
// стенда — ровно та, что в таблице. Проба — три звена: читатель таблицы →
// отрисовка цепочки (`renderStack`) → СУЖДЕНИЕ над множеством «стенд → исход
// отрисовки, разобранные документы». Копию таблицы или слоя не читает ни одно
// звено, поэтому близнецы подаются суждению значением.
//
// Полноту — что каждый профиль назван хотя бы одной цепочкой — держит
// `TestStackTableNamesEveryTrackedProfile`; здесь она не дублируется.
package deploy_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// ka1CallBudgets — ручки Р4 и величина, которую объявляет каждый стенд.
var ka1CallBudgets = map[string]string{
	"KACHO_API_GATEWAY_IDENTITY_CALL_BUDGET": "1s",
	"KACHO_API_GATEWAY_BACKEND_CALL_BUDGET":  "30s",
}

// ka1StandRender — исход отрисовки одного стенда.
type ka1StandRender struct {
	err  error
	out  string
	docs []renderedDoc
}

type ka1BudgetCensus struct{ stands, rendered, raiseEdge int }

// edgeContainerEnv — окружение контейнера `api-gateway` в Deployment края;
// ok=false — отрисовка края не поднимает.
func edgeContainerEnv(docs []renderedDoc) (env map[string]string, ok bool) {
	for _, d := range docs {
		if str(d, "kind") != "Deployment" || str(submap(d, "metadata"), "name") != "api-gateway" {
			continue
		}
		env = map[string]string{}
		pod := submap(submap(submap(d, "spec"), "template"), "spec")
		for _, c := range slice(pod, "containers") {
			cm, _ := c.(map[string]any)
			if str(cm, "name") != "api-gateway" {
				continue
			}
			for _, e := range slice(cm, "env") {
				em, _ := e.(map[string]any)
				env[str(em, "name")] = str(em, "value")
			}
		}
		return env, true
	}
	return nil, false
}

// judgeCallBudgets — суждение: каждая отрисовка состоялась, и каждая, что
// поднимает край, несёт обе ручки с объявленной величиной.
func judgeCallBudgets(stands map[string]ka1StandRender) ([]string, ka1BudgetCensus) {
	var findings []string
	census := ka1BudgetCensus{stands: len(stands)}
	names := make([]string, 0, len(stands))
	for n := range stands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		r := stands[name]
		if r.err != nil {
			findings = append(findings, fmt.Sprintf("стенд %s: отрисовка не состоялась (%v):\n%s", name, r.err, ka1LastLines(r.out, 8)))
			continue
		}
		census.rendered++
		env, ok := edgeContainerEnv(r.docs)
		if !ok {
			continue
		}
		census.raiseEdge++
		knobs := make([]string, 0, len(ka1CallBudgets))
		for k := range ka1CallBudgets {
			knobs = append(knobs, k)
		}
		sort.Strings(knobs)
		for _, k := range knobs {
			got, present := env[k]
			switch {
			case !present:
				findings = append(findings, fmt.Sprintf("стенд %s: окружение контейнера края не несёт %s", name, k))
			case got != ka1CallBudgets[k]:
				findings = append(findings, fmt.Sprintf("стенд %s: %s=%q, объявлено %q", name, k, got, ka1CallBudgets[k]))
			}
		}
	}
	return findings, census
}

func ka1LastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func renderStand(t *testing.T, chain []string) ka1StandRender {
	t.Helper()
	out, err := renderStack(t, chain)
	r := ka1StandRender{err: err, out: out}
	if err == nil {
		r.docs = decodeRender(t, out)
	}
	return r
}

// TestKA1_25_EveryStandDeclaresBothCallBudgets — KA1-25.
func TestKA1_25_EveryStandDeclaresBothCallBudgets(t *testing.T) {
	table := deployStacks(t)
	stands := map[string]ka1StandRender{}
	for name, chain := range table {
		stands[name] = renderStand(t, chain)
	}
	findings, c := judgeCallBudgets(stands)
	t.Logf("перепись: стендов в таблице %d · отрисовано %d · поднимают край %d", c.stands, c.rendered, c.raiseEdge)
	if c.stands == 0 || c.rendered < c.stands || c.raiseEdge == 0 {
		t.Errorf("предпосылка: стендов %d, отрисовано %d, поднимают край %d — таблица пуста, отрисовок меньше, чем стендов, либо край не поднят нигде",
			c.stands, c.rendered, c.raiseEdge)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// Близнец (а): из отрисовки стенда удалена одна запись ручки — суждение красно
// и называет стенд и ручку; та же отрисовка без удаления — зелёна.
func TestKA1_25_Twin_RemovedKnobIsNamed(t *testing.T) {
	table := deployStacks(t)
	r := renderStand(t, table["dev"])
	if r.err != nil {
		t.Fatalf("предпосылка близнеца: стенд dev не отрисован: %v", r.err)
	}
	if f, _ := judgeCallBudgets(map[string]ka1StandRender{"dev": r}); len(f) != 0 {
		t.Fatalf("близнец без удаления обязан быть зелёным: %v", f)
	}
	for _, d := range r.docs {
		if str(d, "kind") != "Deployment" || str(submap(d, "metadata"), "name") != "api-gateway" {
			continue
		}
		pod := submap(submap(submap(d, "spec"), "template"), "spec")
		for _, c := range slice(pod, "containers") {
			cm, _ := c.(map[string]any)
			kept := []any{}
			for _, e := range slice(cm, "env") {
				if str(e.(map[string]any), "name") != "KACHO_API_GATEWAY_BACKEND_CALL_BUDGET" {
					kept = append(kept, e)
				}
			}
			cm["env"] = kept
		}
	}
	f, _ := judgeCallBudgets(map[string]ka1StandRender{"dev": r})
	if len(f) != 1 || !strings.Contains(f[0], "стенд dev") || !strings.Contains(f[0], "KACHO_API_GATEWAY_BACKEND_CALL_BUDGET") {
		t.Fatalf("удалённая ручка не названа со стендом: %v", f)
	}
}

// Близнец (б): стенд dev-prod с цепочкой без нижнего слоя — отказ отрисовки,
// названный стендом; та же строка с цепочкой из таблицы — зелёна.
func TestKA1_25_Twin_BrokenChainIsARedNamingTheStand(t *testing.T) {
	table := deployStacks(t)
	broken := renderStand(t, []string{"values.dev-prod.yaml"})
	f, _ := judgeCallBudgets(map[string]ka1StandRender{"dev-prod": broken})
	if len(f) == 0 || !strings.Contains(f[0], "стенд dev-prod: отрисовка не состоялась") {
		t.Fatalf("отказ отрисовки не назван стендом: %v", f)
	}
	if f, _ := judgeCallBudgets(map[string]ka1StandRender{"dev-prod": renderStand(t, table["dev-prod"])}); len(f) != 0 {
		t.Fatalf("цепочка из таблицы обязана быть зелёной: %v", f)
	}
}
