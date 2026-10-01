// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// stand_chain_fits_its_runner_render_test.go — гейт «цепочка, поднимаемая
// конвейером на kind, помещается в узел ранера по запросам процессора» ПО
// РЕНДЕРУ. Предмет, единица и чего гейт не утверждает — в шапке
// stand_chain_fits_its_runner_test.go; здесь только сбор входа: ноги, рендеры
// цепочек и релиза cert-manager, ёмкость и предпосылки базовой линии.
package deploy_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// renderDocBodies — тела документов рендера; неразборный документ — отказ.
func renderDocBodies(t *testing.T, what, rendered string) []map[string]any {
	t.Helper()
	docs, err := splitRender(rendered)
	if err != nil {
		t.Fatalf("рендер %s не разбирается (%v) — сумма запросов была бы о части рендера", what, err)
	}
	out := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		if d.Body != nil {
			out = append(out, d.Body)
		}
	}
	return out
}

// renderCertManagerRelease — рендер отдельного релиза cert-manager тем чартом и
// теми ручками, что применяет рецепт cert-manager-up.
func renderCertManagerRelease(t *testing.T, chart string, sets []string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm не в PATH — рендерная проба под тегом helmcharts обязана исполняться, а не пропускаться")
	}
	args := []string{"template", "kacho-cert-manager", chart, "-n", "kacho"}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	var so, se bytes.Buffer
	cmd := exec.Command("helm", args...) // #nosec G204 -- фиксированный бинарь, аргументы из дерева
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := cmd.Run(); err != nil {
		t.Fatalf("рендер релиза cert-manager (%s %v) не выполнен (%v) — условие не создано:\n%s",
			chart, sets, err, se.String())
	}
	return so.String()
}

// TestEveryConveyorRaisedChainFitsItsRunnerByCPURequests — сам гейт.
func TestEveryConveyorRaisedChainFitsItsRunnerByCPURequests(t *testing.T) {
	raw, err := os.ReadFile(standMakefile)
	if err != nil {
		t.Fatalf("рецепты подъёма %s не читаются: %v", standMakefile, err)
	}
	makefile := string(raw)
	targets := parseRecipeTargets(makefile)
	stacks := deployStacks(t)
	legs := conveyorLegs(t)

	sets, err := umbrellaSets(makefile)
	if err != nil {
		t.Fatalf("ручки применения умбреллы не прочитаны: %v — рендер был бы не тем стендом", err)
	}
	chart, cmSets, err := certManagerRelease(makefile)
	if err != nil {
		t.Fatalf("релиз cert-manager рецепта не прочитан: %v", err)
	}
	release, err := renderCPURequests(renderDocBodies(t, "релиза cert-manager",
		renderCertManagerRelease(t, chart, cmSets)), 1)
	if err != nil {
		t.Fatalf("запросы релиза cert-manager не вычислены: %v", err)
	}

	// ПРЕДПОСЫЛКИ БАЗОВОЙ ЛИНИИ: один узел, образ пина, пин той версии, на
	// которой линия измерена. Отказ любой — не «не помещается», а «сверять
	// базовую линию не с тем узлом».
	cfg, err := os.ReadFile(filepath.Join("kind", "kind-config.yaml"))
	if err != nil {
		t.Fatalf("конфиг кластера kind не читается: %v", err)
	}
	for _, f := range kindNodeFindings(string(cfg)) {
		t.Error(f)
	}
	capacity := runnerCapacities(t)

	var (
		cases        []runnerFitCase
		pinsChecked  = map[string]bool{}
		withoutChain int
		chainsSeen   = map[string]renderCPU{}
	)
	for _, l := range legs {
		if len(l.Phases) == 0 {
			withoutChain++
			continue
		}
		leg := l.Workflow + " / «" + l.Name + "»"
		recipe := strings.Join(flattenRecipe(targets, l.Target, map[string]bool{}), "\n")
		if !strings.Contains(recipe, "kind/create-cluster.sh") {
			t.Errorf("нога %s накладывает цепочки %v, а рецепт make %s кластера kind не создаёт — "+
				"ёмкость узла и базовая линия к ней не относятся", leg, l.Phases, l.Target)
			continue
		}
		if !pinsChecked[l.Workflow] {
			pinsChecked[l.Workflow] = true
			body, err := os.ReadFile(filepath.Join(filepath.Dir(conveyorWorkflowsGlob), l.Workflow))
			if err != nil {
				t.Fatalf("процесс %s не читается: %v", l.Workflow, err)
			}
			for _, f := range kindPinFindings(l.Workflow, string(body)) {
				t.Error(f)
			}
		}
		for _, phase := range l.Phases {
			chain, ok := stacks[phase]
			if !ok {
				t.Errorf("нога %s накладывает цепочку %s, которой в таблице стендов нет", leg, phase)
				continue
			}
			got, seen := chainsSeen[phase]
			if !seen {
				got, err = renderCPURequests(renderDocBodies(t, "цепочки "+phase,
					renderChainCached(t, chain, sets...)), 1)
				if err != nil {
					t.Fatalf("цепочка %s: запросы не вычислены: %v", phase, err)
				}
				chainsSeen[phase] = got
			}
			cases = append(cases, runnerFitCase{Leg: leg, Runner: l.RunsOn, Chain: phase,
				Umbrella: got, Release: release})
		}
	}
	if len(cases) == 0 {
		t.Fatalf("ни одной цепочки ни одной ноги не осмотрено (ног %d, без цепочки %d) — «помещается» "+
			"было бы объявлено о непрочитанном", len(legs), withoutChain)
	}

	names := make([]string, 0, len(chainsSeen))
	for n := range chainsSeen {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		r := chainsSeen[n]
		t.Logf("цепочка %s: объектов с подами %d · хуков вне суммы %d · CronJob вне суммы %d · запросы %dm",
			n, len(r.Lines), r.Hooks, r.Scheduled, r.Total())
	}
	labels := make([]string, 0, len(capacity))
	for l, v := range capacity {
		labels = append(labels, fmt.Sprintf("%s %dm", l, v))
	}
	sort.Strings(labels)
	t.Logf("релиз cert-manager: объектов с подами %d · запросы %dm; плоскость управления kind %s: %dm; "+
		"ёмкость узла: %s", len(release.Lines), release.Total(), kindBaselineKindVersion, kindBaselineMilli(),
		strings.Join(labels, ", "))
	for _, c := range cases {
		total := c.Umbrella.Total() + c.Release.Total() + kindBaselineMilli()
		t.Logf("нога %s · ранер %s · цепочка %s: %dm + %dm + %dm = %dm из %dm",
			c.Leg, c.Runner, c.Chain, c.Umbrella.Total(), c.Release.Total(), kindBaselineMilli(), total,
			capacity[c.Runner])
	}
	for _, f := range judgeRunnerFit(cases, capacity, kindBaselineMilli()) {
		t.Error(f)
	}
	t.Logf("осмотрено: ног подъёма %d (без цепочки %d) · пар нога×цепочка %d · цепочек отрендерено %d · "+
		"процессов с прочитанным пином kind %d", len(legs), withoutChain, len(cases), len(chainsSeen), len(pinsChecked))
}

// TestRunnerFitInjection_RaisedAppetiteOnARealChainIsFound — инъекция НАСТОЯЩИМ
// входом: рендер цепочки dev-prod (её поднимает конвейер, и она помещается) с
// ОДНИМ поднятым запросом базы против того же рендера без него. Близнец обязан
// молчать, инъекция — покраснеть и назвать поднятый объект.
func TestRunnerFitInjection_RaisedAppetiteOnARealChainIsFound(t *testing.T) {
	raw, err := os.ReadFile(standMakefile)
	if err != nil {
		t.Fatalf("рецепты подъёма %s не читаются: %v", standMakefile, err)
	}
	sets, err := umbrellaSets(string(raw))
	if err != nil {
		t.Fatalf("ручки применения умбреллы не прочитаны: %v", err)
	}
	chain, ok := deployStacks(t)["dev-prod"]
	if !ok {
		t.Fatal("цепочки dev-prod в таблице стендов нет — близнецу не из чего рендериться")
	}
	capacity := runnerCapacities(t)
	judge := func(extra ...string) []string {
		r, err := renderCPURequests(renderDocBodies(t, "цепочки dev-prod",
			renderChainCached(t, chain, append(append([]string(nil), sets...), extra...)...)), 1)
		if err != nil {
			t.Fatalf("запросы не вычислены: %v", err)
		}
		return judgeRunnerFit([]runnerFitCase{{Leg: "инъекция", Runner: "ubuntu-latest", Chain: "dev-prod",
			Umbrella: r}}, capacity, kindBaselineMilli())
	}
	if got := judge(); len(got) != 0 {
		t.Fatalf("законный близнец (dev-prod как есть) покраснел — инъекции не с чем сравнивать: %v", got)
	}
	got := judge("pg-vpc.primary.resources.requests.cpu=3000m")
	if len(got) != 1 || !strings.Contains(got[0], "kacho-umbrella-pg-vpc 3000m") {
		t.Errorf("поднятый до 3000m запрос базы vpc не найден либо не назван: %v", got)
	}
}
