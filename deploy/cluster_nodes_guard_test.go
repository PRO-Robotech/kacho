// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// cluster_nodes_guard_test.go — кластер площадки доказывается его УЗЛАМИ, а не
// файлом профиля (kacho#3065). Решение владельца 2026-10-10 дословно: «По
// префиксу нод можешь понять тот кластер или нет, куб конфет мог меняться».
//
// Файл профиля могут заменить, и его контекст -client поведёт в другой кластер.
// Стенд проб (scripts/stand-ns.sh) и `stack-up` стенда площадки принимают
// кластер, только если узлов ≥ 1 и каждое имя начинается с `<профиль>-client-`;
// иначе — отказ до первой записи. Проба судит ИСХОД подставным kubectl (его
// узлы — FAKE_NODES; FAKE_NODES_SWITCH_AFTER — «файл сменился» после N-го
// чтения узлов): код, текст отказа с шагом «что сделать» и то, что к кластеру
// не ушло ничего, кроме чтения узлов. Подставные — из stand_ns_context_test.go и
// stack_client_context_test.go; положительный близнец каждого отказа — их тесты
// (узлы client площадки по умолчанию).
package deploy

import (
	"strings"
	"testing"
)

// writesBeyondNodes — обращения к кластеру, кроме чтения узлов.
func writesBeyondNodes(calls []string) []string {
	var out []string
	for _, c := range calls {
		if !strings.Contains(c, " get nodes ") {
			out = append(out, c)
		}
	}
	return out
}

func nodesRead(calls []string) bool {
	for _, c := range calls {
		if strings.Contains(c, " get nodes ") {
			return true
		}
	}
	return false
}

type nodesCase struct {
	name string
	env  []string
	want string
}

func standNodeCases() []nodesCase {
	return []nodesCase{
		{"узлы infra", []string{"FAKE_NODES=x-infra-p1a2b3-aaaaa x-infra-p1a2b3-bbbbb"}, "не узлы x-client-"},
		{"смешанные client и infra", []string{"FAKE_NODES=x-client-p1a2b3-aaaaa x-infra-p1a2b3-bbbbb"}, "не узлы x-client-"},
		{"client с -infra- в хвосте", []string{"FAKE_NODES=x-client-p1a2b3-aaaaa x-client-p1-infra-ccccc"}, "не узлы x-client-"},
		{"узлы чужой площадки", []string{"FAKE_NODES=y-client-p1a2b3-aaaaa"}, "не узлы x-client-"},
		{"ноль узлов", []string{"FAKE_NODES="}, "0 узлов"},
		{"узлы не прочитаны", []string{"FAKE_NODES_FAIL=1"}, "НЕ ПРОЧИТАНЫ"},
		{"профиль не выведен", []string{"STAND_PROFILE="}, "профиль площадки не выведен"},
	}
}

// stand-ns: каждый отказ — кодом 2, с шагом «что сделать», ДО первой записи:
// к кластеру ушло только чтение узлов (а для «профиль не выведен» — ничего).
func TestStandNamespaceRefusesWhenNodesAreNotTheProfileClient(t *testing.T) {
	pdir := t.TempDir()
	good := standProfile(t, pdir, "good.yaml", standInfra, standClient, standInfra)
	for _, c := range standNodeCases() {
		for _, op := range [][]string{{"up", "t3065-nodes", "a8f60d"}, {"down", "t3065-nodes"}, {"census"}} {
			r := runStandNS(t, append([]string{"STAND_KUBECONFIG=" + good}, c.env...), op...)
			name := c.name + " / " + op[0]
			if r.code != 2 || !strings.Contains(r.out, c.want) || !strings.Contains(r.out, "Что сделать") {
				t.Errorf("%s: ждали код 2, «%s» и «Что сделать», получили %d:\n%s", name, c.want, r.code, r.out)
			}
			calls := callLines(r.log)
			if w := writesBeyondNodes(calls); len(w) != 0 {
				t.Errorf("%s: до отказа к кластеру ушло не только чтение узлов: %v", name, w)
			}
			if c.name != "профиль не выведен" && !nodesRead(calls) {
				t.Errorf("%s: узлы не спрошены — отказ не узлами, проба беспредметна:\n%s", name, r.out)
			}
		}
	}
}

// Профиль — из имени файла `*--<профиль>.yaml`: файл площадки x без
// STAND_PROFILE проходит (близнец «профиль не выведен»), файл площадки y при
// STAND_PROFILE=x — отказ без обращения к кластеру.
func TestStandNamespaceProfileComesFromTheFileName(t *testing.T) {
	pdir := t.TempDir()
	ofX := standProfile(t, pdir, "u--x.yaml", standInfra, standClient, standInfra)
	ofY := standProfile(t, pdir, "u--y.yaml", standInfra, standClient, standInfra)

	r := runStandNS(t, []string{"STAND_KUBECONFIG=" + ofX, "STAND_PROFILE="}, "census")
	if r.code != 0 || !strings.Contains(r.out, "кластер доказан узлами") {
		t.Errorf("файл площадки x без STAND_PROFILE: ждали код 0 и доказательство узлами, получили %d:\n%s", r.code, r.out)
	}
	r = runStandNS(t, []string{"STAND_KUBECONFIG=" + ofY}, "census")
	if r.code != 2 || !strings.Contains(r.out, "назван для площадки «y»") {
		t.Errorf("файл площадки y при STAND_PROFILE=x: ждали отказ, получили %d:\n%s", r.code, r.out)
	}
	if calls := callLines(r.log); len(calls) != 0 {
		t.Errorf("файл площадки y: до отказа было обращение к кластеру: %v", calls)
	}
}

// Файл сменился между фазами: при первом чтении узлы client, при следующем —
// infra. Снятие отказывает ПЕРЕД фазой записи и ничего не удаляет. Близнец — то
// же без смены (код 0).
func TestStandNamespaceRechecksNodesBeforeEachWritePhase(t *testing.T) {
	pdir := t.TempDir()
	good := standProfile(t, pdir, "good.yaml", standInfra, standClient, standInfra)

	r := runStandNS(t, []string{"STAND_KUBECONFIG=" + good}, "down", "t3065-nodes")
	if r.code != 0 || strings.Count(r.log, " get nodes ") < 2 {
		t.Fatalf("близнец: ждали код 0 и узлы, спрошенные заново перед записью; код %d:\n%s\n%s", r.code, r.out, r.log)
	}
	r = runStandNS(t, []string{"STAND_KUBECONFIG=" + good, "FAKE_NODES_SWITCH_AFTER=1",
		"FAKE_NODES_SWITCHED=x-infra-p1a2b3-aaaaa"}, "down", "t3065-nodes")
	if r.code != 2 || !strings.Contains(r.out, "перед фазой записи") {
		t.Errorf("узлы сменились между фазами: ждали отказ перед фазой записи, получили %d:\n%s", r.code, r.out)
	}
	for _, c := range callLines(r.log) {
		if strings.Contains(c, " delete ") || strings.Contains(c, " uninstall ") {
			t.Errorf("после смены узлов была запись: %s", c)
		}
	}
}

// stack-up STACK=a8f60d: узлы обязаны быть a8f60d-client-… (имя стенда —
// профиль площадки). Отказ — до guard-destructive и до первого helm; к
// кластеру — только чтение узлов. Сюда же смена узлов после первого чтения:
// страж внутри закрепления (verify) читает их заново и отказывает.
func TestStackUpRefusesWhenNodesAreNotTheProfileClient(t *testing.T) {
	pdir := t.TempDir()
	good := standProfile(t, pdir, "good.yaml", standInfra, standClient, standInfra)
	ofOther := standProfile(t, pdir, "u--b13604.yaml", standInfra, standClient, standInfra)
	for _, c := range []nodesCase{
		{"узлы infra", []string{"STAND_KUBECONFIG=" + good, "FAKE_NODES=a8f60d-infra-p1a2b3-aaaaa"}, "не узлы a8f60d-client-"},
		{"смешанные client и infra", []string{"STAND_KUBECONFIG=" + good, "FAKE_NODES=a8f60d-client-p1a2b3-aaaaa a8f60d-infra-p1a2b3-bbbbb"}, "не узлы a8f60d-client-"},
		{"узлы чужой площадки", []string{"STAND_KUBECONFIG=" + good, "FAKE_NODES=b13604-client-p1a2b3-aaaaa"}, "не узлы a8f60d-client-"},
		{"ноль узлов", []string{"STAND_KUBECONFIG=" + good, "FAKE_NODES="}, "0 узлов"},
		{"узлы не прочитаны", []string{"STAND_KUBECONFIG=" + good, "FAKE_NODES_FAIL=1"}, "НЕ ПРОЧИТАНЫ"},
		{"файл профиля другой площадки", []string{"STAND_KUBECONFIG=" + ofOther}, "назван для площадки «b13604»"},
		{"узлы сменились после первого чтения", []string{"STAND_KUBECONFIG=" + good, "FAKE_NODES_SWITCH_AFTER=1",
			"FAKE_NODES_SWITCHED=a8f60d-infra-p1a2b3-aaaaa"}, "не узлы a8f60d-client-"},
	} {
		r := runStackUp(t, c.env, "STACK=a8f60d", "CONFIRM=UP-a8f60d@"+standClient)
		if r.code == 0 || !strings.Contains(r.out, c.want) || !strings.Contains(r.out, "Что сделать") {
			t.Errorf("%s: ждали отказ с «%s» и «Что сделать», получили код %d:\n%s", c.name, c.want, r.code, r.out)
		}
		if w := writesBeyondNodes(clusterCalls(r.log)); len(w) != 0 {
			t.Errorf("%s: до отказа к кластеру ушло не только чтение узлов: %v", c.name, w)
		}
		if strings.Contains(r.out, "РАЗРУШИТЕЛЬНАЯ ОПЕРАЦИЯ") {
			t.Errorf("%s: дошли до guard-destructive — узлы не стоят первыми:\n%s", c.name, r.out)
		}
	}
}
