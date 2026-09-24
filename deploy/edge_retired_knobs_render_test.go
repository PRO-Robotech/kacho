// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// edge_retired_knobs_render_test.go — ручка, СНЯТАЯ с процесса края, не
// приезжает в его под ни на одной цепочке (#2778).
//
// # Чего не видит проба исходника
//
// Двухколоночный гейт края (`gateway/deploy/knob_producer_parity_test.go`)
// читает исходник шаблона: он видит всякое имя, которое чарт умеет эмитировать
// при любом условии. Имя, приезжающее в под через `extraEnv` профиля, в
// исходнике шаблона не записано вовсе — его видит только РЕНДЕР цепочки. Снятая
// ручка, возвращённая профилем таким путём, дошла бы до процесса, который её не
// читает, и оператор видел бы значение в поде, получая поведение по умолчанию.
//
// # Что судится и чем
//
// Рендер чарта края САМОГО ПО СЕБЕ (без профиля) и рендер умбреллы по КАЖДОЙ
// цепочке `deploy/stacks.txt`. Из рендера берётся под края — Deployment с
// именем `api-gateway` — и имена переменных окружения ВСЕХ его контейнеров.
// Число эмиссий снятых ручек печатается ПО ЦЕПОЧКЕ, а не суммой: сумма скрыла бы,
// на каком стенде ручка вернулась.
//
// Перечень снятых — ведомость `internal/retiredknobs`, тот же источник, что у
// двухколоночного гейта: второй перечень здесь разошёлся бы с первым молча.
//
// # Предпосылка
//
// Под края обязан найтись на КАЖДОМ рендере и нести хотя бы одну переменную
// продукта: «эмиссий ноль» на рендере, где пода края нет, означало бы «смотреть
// было не на что», а не «снятое не вернулось».
package deploy_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/retiredknobs"
)

// edgeChartDir — чарт края относительно пакета проб развёртывания.
var edgeChartDir = filepath.Join("..", "gateway", "deploy")

// edgeDeploymentName — имя пода края в рендере (`name` в values чарта края).
const edgeDeploymentName = "api-gateway"

// edgeEnvNames — имена переменных окружения всех контейнеров пода края в
// рендере; found=false, если пода края в рендере нет.
func edgeEnvNames(t *testing.T, rendered string) (names []string, found bool) {
	t.Helper()
	for _, d := range decodeRender(t, rendered) {
		if str(d, "kind") != "Deployment" || str(submap(d, "metadata"), "name") != edgeDeploymentName {
			continue
		}
		found = true
		spec := submap(submap(submap(d, "spec"), "template"), "spec")
		for _, group := range []string{"initContainers", "containers"} {
			for _, c := range slice(spec, group) {
				cm, _ := c.(map[string]any)
				for _, e := range slice(cm, "env") {
					em, _ := e.(map[string]any)
					if n := str(em, "name"); n != "" {
						names = append(names, n)
					}
				}
			}
		}
	}
	sort.Strings(names)
	return names, found
}

// judgeRetiredEmissions — снятые имена среди эмитированных, отсортированно.
func judgeRetiredEmissions(names []string, retired map[string]string) []string {
	var out []string
	for _, n := range names {
		if _, gone := retired[n]; gone {
			out = append(out, n)
		}
	}
	return out
}

// renderEdgeChartAlone — рендер чарта края без профиля.
func renderEdgeChartAlone(t *testing.T) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm не в PATH — рендерная проба под тегом helmcharts обязана исполняться, " +
			"а не пропускаться: без helm условие этого задания не создано")
	}
	out, err := exec.Command("helm", "template", edgeDeploymentName, edgeChartDir, "-n", "kacho").CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева
	return string(out), err
}

func TestNoStackRendersARetiredEdgeKnob(t *testing.T) {
	retired := retiredknobs.Edge()
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	type render struct {
		label string
		out   string
		err   error
	}
	renders := make([]render, 0, len(names)+1)
	alone, aloneErr := renderEdgeChartAlone(t)
	renders = append(renders, render{"чарт края без профиля", alone, aloneErr})
	for _, n := range names {
		out, err := renderStack(t, stacks[n])
		renders = append(renders, render{fmt.Sprintf("%s (%s)", n, strings.Join(stacks[n], " + ")), out, err})
	}

	perChain := make([]string, 0, len(renders))
	for _, r := range renders {
		if r.err != nil {
			t.Fatalf("%s: рендер не выполнен (%v) — условие не создано, вердикта нет:\n%s",
				r.label, r.err, r.out)
		}
		env, found := edgeEnvNames(t, r.out)
		if !found {
			t.Fatalf("%s: в рендере нет пода края (Deployment %q) — смотреть было не на что, "+
				"и «эмиссий ноль» здесь не утверждение", r.label, edgeDeploymentName)
		}
		if len(env) == 0 {
			t.Fatalf("%s: под края не несёт ни одной переменной окружения — разбор рендера "+
				"прочитал не то", r.label)
		}
		got := judgeRetiredEmissions(env, retired)
		perChain = append(perChain, fmt.Sprintf("%s: переменных %d, снятых %d", r.label, len(env), len(got)))
		for _, g := range got {
			t.Errorf("%s: под края получает СНЯТУЮ ручку %s (%s) — процесс её не читает, "+
				"значение в поде есть, поведение по умолчанию", r.label, g, retired[g])
		}
	}
	t.Logf("перепись: снятых ручек в ведомости %d; рендеров %d (цепочек %d + чарт без профиля)",
		len(retired), len(renders), len(names))
	for _, line := range perChain {
		t.Log("  " + line)
	}
}

// Инъекция на синтетическом рендере: под края с одной снятой ручкой —
// находка; тот же под без неё — молчание; рендер без пода края — «не найден».
func TestEdgeRetiredKnobsRenderJudgeFiresAndStaysSilent(t *testing.T) {
	doc := func(env ...string) string {
		var b strings.Builder
		b.WriteString("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: " + edgeDeploymentName +
			"\nspec:\n  template:\n    spec:\n      containers:\n        - name: c\n          env:\n")
		for _, e := range env {
			b.WriteString("            - name: " + e + "\n              value: x\n")
		}
		return b.String()
	}
	retired := map[string]string{"KACHO_X_GONE": "снята вместе с читателем"}

	names, found := edgeEnvNames(t, doc("KACHO_X_LIVE", "KACHO_X_GONE"))
	if !found || len(judgeRetiredEmissions(names, retired)) != 1 {
		t.Fatalf("снятая ручка в поде края не найдена: found=%v names=%v", found, names)
	}
	names, found = edgeEnvNames(t, doc("KACHO_X_LIVE"))
	if !found || len(judgeRetiredEmissions(names, retired)) != 0 {
		t.Fatalf("законный близнец объявлен находкой: found=%v names=%v", found, names)
	}
	other := strings.Replace(doc("KACHO_X_GONE"), "name: "+edgeDeploymentName, "name: other", 1)
	if _, found = edgeEnvNames(t, other); found {
		t.Fatal("под чужого имени принят за под края — перепись судила бы не тот объект")
	}
}
