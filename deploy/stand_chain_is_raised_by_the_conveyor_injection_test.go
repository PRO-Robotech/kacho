// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_chain_is_raised_by_the_conveyor_injection_test.go — гейт «цепочку
// поднимает конвейер» УМЕЕТ краснеть, и краснеет на СВОЁМ факте.
//
// Каждый случай меняет ровно один факт против законного близнеца: нога не на
// запросе, нога не судит посадку, нога не судит готовность, послабление без
// предмета. Без близнеца «краснеет» неотличимо от «краснеет всегда».
package deploy_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func legFor(chain string, onPR, judged bool) standLeg {
	return standLeg{Workflow: "w.yml", Job: "j", Name: "n", OnPullRequest: onPR,
		Target: chain + "-up", Phases: []string{chain}, Judged: judged}
}

func TestConveyorChainFindingsRedOnItsOwnFact(t *testing.T) {
	chains := map[string][]string{"own": {"values.prod.yaml"}, "site": {"values.site.yaml"}}
	exempt := map[string]string{"site": "довод"}

	if got := conveyorChainFindings(chains, []standLeg{legFor("own", true, true)}, exempt); len(got) != 0 {
		t.Fatalf("контроль: цепочка поднята на запросе и судима, а находка есть — %v", got)
	}
	for _, c := range []struct {
		name string
		legs []standLeg
		ex   map[string]string
		want string
	}{
		{"ноги нет", nil, exempt, "цепочку own"},
		{"нога не на запросе", []standLeg{legFor("own", false, true)}, exempt, "цепочку own"},
		{"нога не судит поднятое", []standLeg{legFor("own", true, false)}, exempt, "цепочку own"},
		{"послабление о поднятой цепочке", []standLeg{legFor("own", true, true)},
			map[string]string{"site": "довод", "own": "довод"}, "исключать ему нечего"},
		{"послабление без цепочки в таблице", []standLeg{legFor("own", true, true)},
			map[string]string{"site": "довод", "gone": "довод"}, "пережила свой предмет"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := conveyorChainFindings(chains, c.legs, c.ex)
			if len(got) != 1 || !strings.Contains(got[0], c.want) {
				t.Errorf("ожидалась ровно одна находка со словами %q, получено %v", c.want, got)
			}
		})
	}
}

// Рецепт судит поднятое, только если ПОСЛЕ последней цепочки зовёт оба суда.
// Близнецы различаются ровно одной строкой.
func TestResolveLegJudgesOnlyWhatComesAfterTheLastChain(t *testing.T) {
	const mk = "up-base: preflight\n" +
		"\t@set -e; helm upgrade x $(call STACK_ARGS,dev); \\\n" +
		"\t$(MAKE) --no-print-directory assert-rollout-ready; \\\n" +
		"\t$(MAKE) --no-print-directory assert-production-posture\n" +
		"preflight:\n\t@true\n" +
		"up-full: up-base\n" +
		"\t@helm upgrade x $(call STACK_ARGS,own); \\\n" +
		"\t$(MAKE) --no-print-directory assert-rollout-ready; \\\n" +
		"\t$(MAKE) --no-print-directory assert-production-posture\n" +
		"up-no-rollout: up-base\n" +
		"\t@helm upgrade x $(call STACK_ARGS,own); \\\n" +
		"\t$(MAKE) --no-print-directory assert-production-posture\n" +
		"up-no-posture: up-base\n" +
		"\t@helm upgrade x $(call STACK_ARGS,own); \\\n" +
		"\t$(MAKE) --no-print-directory assert-rollout-ready\n"
	targets := parseRecipeTargets(mk)
	for _, c := range []struct {
		target string
		phases string
		judged bool
	}{
		{"up-base", "dev", true},
		{"up-full", "dev own", true},
		{"up-no-rollout", "dev own", false},
		{"up-no-posture", "dev own", false},
	} {
		phases, judged, err := resolveLeg(targets, c.target)
		if err != nil {
			t.Fatalf("%s: %v", c.target, err)
		}
		if strings.Join(phases, " ") != c.phases || judged != c.judged {
			t.Errorf("%s: цепочки %v, судит %v — ожидались %q и %v", c.target, phases, judged, c.phases, c.judged)
		}
	}
	if _, _, err := resolveLeg(targets, "absent-up"); err == nil {
		t.Error("цель, которой в Makefile нет, разрешилась без отказа")
	}
}

// Литеральная матрица раскрывается по ногам; нераскрываемая — отказ, а не
// одна нога с неподставленной целью.
func TestExpandMatrixSplitsLiteralLegsAndRefusesTheRest(t *testing.T) {
	job := func(body string) map[string]any {
		var j map[string]any
		if err := yaml.Unmarshal([]byte(body), &j); err != nil {
			t.Fatalf("синтетическая работа не разобрана: %v", err)
		}
		return j
	}
	legs := expandMatrix(job("strategy:\n  matrix:\n    stack: [dev-prod, own]\n"),
		"${{ matrix.stack }}-up", "цепочка ${{ matrix.stack }}")
	if len(legs) != 2 || legs[0].target != "dev-prod-up" || legs[1].target != "own-up" ||
		legs[1].name != "цепочка own" || legs[0].err != "" {
		t.Errorf("литеральная матрица раскрыта неверно: %+v", legs)
	}
	for name, body := range map[string]string{
		"include":   "strategy:\n  matrix:\n    stack: [own]\n    include: [{stack: dev}]\n",
		"выражение": "strategy:\n  matrix: ${{ fromJSON(x) }}\n",
		"нет матрицы": "runs-on: x\n",
	} {
		got := expandMatrix(job(body), "${{ matrix.stack }}-up", "n")
		if len(got) != 1 || got[0].err == "" {
			t.Errorf("%s: нераскрываемая матрица дала ноги без отказа: %+v", name, got)
		}
	}
	if got := expandMatrix(job("runs-on: x\n"), "dev-up", "n"); len(got) != 1 || got[0].target != "dev-up" {
		t.Errorf("цель без ссылки на матрицу — одна нога: %+v", got)
	}
}
