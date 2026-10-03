// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_memory_request_injection_test.go — доказательство того, что сверка
// запроса памяти с пределом на посадке `own` СПОСОБНА упасть и способна
// смолчать (kacho#2727).
//
// Судья проверяется на синтетическом входе, где каждый случай меняет РОВНО
// ОДИН факт против законного близнеца. Вывод запроса из предела проверяется
// НАСТОЯЩИМ входом: рендером цепочки стенда `own` с другим пределом.
package deploy_test

import (
	"strings"
	"testing"
)

// legalLaneRequestFacts — законный близнец: запрос памяти равен пределу, процессор
// не бюджетирован, инициализирующий контейнер ресурсов не объявляет — Burstable.
func legalLaneRequestFacts() laneRequestFacts {
	main := laneContainerResources{
		Name:     "kaname",
		Requests: map[string]string{"cpu": "100m", "memory": "1280Mi"},
		Limits:   map[string]string{"cpu": "1000m", "memory": "1280Mi"},
	}
	return laneRequestFacts{
		Stack: "own",
		Main:  main,
		Containers: []laneContainerResources{
			{Name: "migrate", Init: true},
			main,
		},
	}
}

// withMain — меняет контейнер службы и в Main, и в перечне контейнеров пода:
// это ОДИН факт рендера, записанный в двух местах структуры.
func withMain(f laneRequestFacts, mutate func(c *laneContainerResources)) laneRequestFacts {
	c := laneContainerResources{
		Name:     f.Main.Name,
		Requests: map[string]string{},
		Limits:   map[string]string{},
	}
	for k, v := range f.Main.Requests {
		c.Requests[k] = v
	}
	for k, v := range f.Main.Limits {
		c.Limits[k] = v
	}
	mutate(&c)
	f.Main = c
	f.Containers = []laneContainerResources{f.Containers[0], c}
	return f
}

func TestLaneMemoryRequestJudgement_CanFailAndStaysSilent(t *testing.T) {
	cases := []struct {
		name    string
		facts   func() laneRequestFacts
		want    int
		mustSay string
	}{
		{
			name:  "законный близнец: запрос памяти равен пределу, класс Burstable — молчит",
			facts: legalLaneRequestFacts,
		},
		{
			// Предмет задачи: источник сменился, запрос остался прежним.
			name: "предел поднят, запрос не сдвинулся — находка",
			facts: func() laneRequestFacts {
				return withMain(legalLaneRequestFacts(), func(c *laneContainerResources) { c.Limits["memory"] = "1536Mi" })
			},
			want:    1,
			mustSay: "две величины одного бюджета разошлись",
		},
		{
			name: "запрос умолчанием подчарта при бюджетном пределе — находка",
			facts: func() laneRequestFacts {
				return withMain(legalLaneRequestFacts(), func(c *laneContainerResources) { c.Requests["memory"] = "128Mi" })
			},
			want:    1,
			mustSay: "запрос памяти 128Mi, а предел 1280Mi",
		},
		{
			name: "та же величина другими единицами — молчит: сравнивается количество, а не строка",
			facts: func() laneRequestFacts {
				return withMain(legalLaneRequestFacts(), func(c *laneContainerResources) { c.Requests["memory"] = "1342177280" })
			},
		},
		{
			name: "запрос не задан — молчит: Kubernetes берёт его равным пределу",
			facts: func() laneRequestFacts {
				return withMain(legalLaneRequestFacts(), func(c *laneContainerResources) { delete(c.Requests, "memory") })
			},
		},
		{
			name: "предела памяти нет — находка: запросу не из чего выводиться",
			facts: func() laneRequestFacts {
				return withMain(legalLaneRequestFacts(), func(c *laneContainerResources) { delete(c.Limits, "memory") })
			},
			want:    1,
			mustSay: "нет предела памяти",
		},
		{
			// Класс сменился правкой ресурсов, а не решением: у контейнера
			// миграции объявлены равные пределы, у службы процессор равен
			// пределу — под стал Guaranteed.
			name: "класс стал Guaranteed без смены решения — находка",
			facts: func() laneRequestFacts {
				f := withMain(legalLaneRequestFacts(), func(c *laneContainerResources) { c.Requests["cpu"] = "1" })
				f.Containers[0] = laneContainerResources{
					Name: "migrate", Init: true,
					Requests: map[string]string{"cpu": "100m", "memory": "64Mi"},
					Limits:   map[string]string{"cpu": "100m", "memory": "64Mi"},
				}
				return f
			},
			want:    1,
			mustSay: "класс обслуживания пода Guaranteed",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings := judgeLaneMemoryRequest([]laneRequestFacts{c.facts()})
			if len(findings) != c.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(findings), c.want, findings)
			}
			if c.mustSay != "" && !strings.Contains(strings.Join(findings, "\n"), c.mustSay) {
				t.Errorf("ни одна находка не называет %q: %v", c.mustSay, findings)
			}
		})
	}
}

// TestLanePodQoSClass_FollowsKubernetesRules — вычисление класса обслуживания:
// все три класса различимы, и инициализирующий контейнер участвует.
func TestLanePodQoSClass_FollowsKubernetesRules(t *testing.T) {
	eq := laneContainerResources{
		Requests: map[string]string{"cpu": "1", "memory": "1Gi"},
		Limits:   map[string]string{"cpu": "1000m", "memory": "1024Mi"},
	}
	limitsOnly := laneContainerResources{Limits: map[string]string{"cpu": "1", "memory": "1Gi"}}
	cases := []struct {
		name       string
		containers []laneContainerResources
		want       string
	}{
		{name: "ни у кого ничего — BestEffort", containers: []laneContainerResources{{}, {}}, want: "BestEffort"},
		{name: "у всех запрос равен пределу — Guaranteed", containers: []laneContainerResources{eq, eq}, want: "Guaranteed"},
		{name: "только пределы — Guaranteed: запрос берётся равным пределу", containers: []laneContainerResources{limitsOnly}, want: "Guaranteed"},
		{name: "инициализирующий без ресурсов — Burstable", containers: []laneContainerResources{{Init: true}, eq}, want: "Burstable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := podQoSClass(c.containers)
			if err != nil {
				t.Fatalf("класс не вычислен: %v", err)
			}
			if got != c.want {
				t.Errorf("класс %s, ожидался %s", got, c.want)
			}
		})
	}
}

// TestOwnLaneMemoryRequest_TheLimitMovesTheRequest — НАСТОЯЩИЙ вход: цепочка
// стенда `own` с другим пределом. Запрос обязан сдвинуться вслед за ним —
// правка источника двигает обе величины (kacho#2727, п. 1 предиката).
func TestOwnLaneMemoryRequest_TheLimitMovesTheRequest(t *testing.T) {
	facts, census := renderOwnLaneRequestFacts(t, []string{"resources.limits.memory=1536Mi"})
	if census.Rendered == 0 {
		t.Fatalf("отрендерено 0 стендов на own (%s) — инъекции некуда было попасть", census)
	}
	for _, f := range facts {
		if got := f.Main.Requests["memory"]; got != "1536Mi" {
			t.Errorf("стенд %s: предел поднят до 1536Mi, а запрос памяти %q — источник не двигает запрос", f.Stack, got)
		}
	}
	if findings := judgeLaneMemoryRequest(facts); len(findings) != 0 {
		t.Errorf("сдвинутый вместе предел и запрос дали находки: %v", findings)
	}
}
