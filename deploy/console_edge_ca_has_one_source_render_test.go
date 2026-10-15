// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// console_edge_ca_has_one_source_render_test.go — удостоверяющий центр края,
// которым консоль проверяет край, у консоли и края ОДИН источник (kacho#3219).
//
// Консоль берёт `ca.crt` из секрета серверного листа края. Комментарий values
// края велил заводить этот секрет командой `kubectl create secret tls … --cert
// --key`, а в такой форме `ca.crt` нет: звено консоли к краю не проверяло бы
// сервер и не поднималось.
//
// Источник один — Certificate того же рендера, выпускающий серверный лист края
// (`api-gateway.mtls.serverSecretName`): cert-manager кладёт в его секрет и
// `ca.crt`. Проба судит две вещи:
//
//	(1) по рендеру каждой цепочки, где есть консоль: секрет тома `edge-ca`
//	    раздачи производит Certificate того же рендера;
//	(2) по объявлениям: ни values края, ни values консоли не велят заводить
//	    этот секрет формой `create secret tls`, которая `ca.crt` не несёт.
package deploy_test

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// edgeCAVolumeSecrets — секреты тома `edge-ca` во всех подах рендера.
func edgeCAVolumeSecrets(docs []renderedDoc) []string {
	var out []string
	for _, d := range docsOfKind(docs, "Deployment") {
		pod := submap(submap(submap(d, "spec"), "template"), "spec")
		for _, v := range slice(pod, "volumes") {
			vm, _ := v.(map[string]any)
			if str(vm, "name") == "edge-ca" {
				out = append(out, str(submap(vm, "secret"), "secretName"))
			}
		}
	}
	return out
}

// judgeEdgeCASource — находки: секрет `edge-ca`, которого не производит ни один
// Certificate рендера.
func judgeEdgeCASource(stack string, docs []renderedDoc) (judged int, findings []string) {
	produced := map[string]bool{}
	for _, c := range docsOfKind(docs, "Certificate") {
		produced[str(submap(c, "spec"), "secretName")] = true
	}
	for _, s := range edgeCAVolumeSecrets(docs) {
		judged++
		if !produced[s] {
			findings = append(findings, "стенд "+stack+": консоль берёт ca.crt края из секрета "+s+
				", а Certificate, производящего его, в рендере нет — источник ca.crt не назван")
		}
	}
	return judged, findings
}

// judgeTLSSecretInstruction — находки: строка, велящая заводить секрет формой
// `create secret tls` (такая форма ca.crt не несёт).
func judgeTLSSecretInstruction(path, text string) []string {
	var out []string
	for i, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "create secret tls") {
			out = append(out, path+":"+strconv.Itoa(i+1)+": велит заводить секрет формой «create secret tls» — "+
				"ca.crt в ней нет, и консоль не проверит край")
		}
	}
	return out
}

func TestConsoleEdgeCAHasOneSource(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	judged := 0
	for _, n := range names {
		out, err := renderStack(t, stacks[n])
		if err != nil {
			t.Fatalf("стенд %s не рендерится: %v\n%s", n, err, out)
		}
		j, f := judgeEdgeCASource(n, decodeRender(t, out))
		judged += j
		for _, x := range f {
			t.Error(x)
		}
	}
	for _, p := range []string{"../gateway/deploy/values.yaml", "../ui-future/deploy/values.yaml"} {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s не прочитан: %v", p, err)
		}
		for _, x := range judgeTLSSecretInstruction(p, string(raw)) {
			t.Error(x)
		}
	}
	t.Logf("перепись: стендов %d · томов edge-ca судимо %d", len(names), judged)
	if judged == 0 {
		t.Fatal("ни в одном стенде нет тома edge-ca — обход пуст")
	}
}

func TestConsoleEdgeCAInjection_UnproducedSecretIsFound(t *testing.T) {
	docs := []renderedDoc{{
		"kind": "Deployment",
		"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
			"volumes": []any{map[string]any{"name": "edge-ca", "secret": map[string]any{"secretName": "api-gateway-tls"}}},
		}}},
	}}
	if _, f := judgeEdgeCASource("synthetic", docs); len(f) != 1 {
		t.Fatalf("секрет без производителя не найден: %v", f)
	}
	docs = append(docs, renderedDoc{"kind": "Certificate", "spec": map[string]any{"secretName": "api-gateway-tls"}})
	if _, f := judgeEdgeCASource("synthetic", docs); len(f) != 0 {
		t.Fatalf("законный близнец назван находкой: %v", f)
	}
}

func TestConsoleEdgeCAInjection_CreateSecretTLSInstructionIsFound(t *testing.T) {
	if f := judgeTLSSecretInstruction("x", "# kubectl create secret tls api-gateway-tls --cert=a --key=b"); len(f) != 1 {
		t.Fatalf("инструкция без ca.crt не найдена: %v", f)
	}
	if f := judgeTLSSecretInstruction("x", "# секрет выпускает Certificate чарта (ca.crt внутри)"); len(f) != 0 {
		t.Fatalf("законный близнец назван находкой: %v", f)
	}
}
