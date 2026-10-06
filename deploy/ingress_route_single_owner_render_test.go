// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// ingress_route_single_owner_render_test.go — У ПАРЫ host+path РОВНО ОДИН ВХОД
// В КАЖДОЙ ЦЕПОЧКЕ deploy/stacks.txt (kacho#3028).
//
// Контроллер входа принимает объект, только если ни один уже принятый объект
// не объявил ту же пару host+path: второй объект отвергает его проверка при
// приёме, и установка падает целиком («host … and path … is already defined in
// ingress …»). `helm template` такой рендер выпускает молча, а судья маршрута
// (ingressRoute в edge_front_link_identity_render_test.go) из двух точных
// совпадений берёт последнее — то есть два входа на одну пару зелёные везде,
// кроме живого кластера.
//
// Этот гейт судит рендер каждой цепочки: каждая пара host+path объявлена
// ровно одним объектом Ingress (и одной строкой внутри него). ЗНАМЕНАТЕЛЬ
// печатается: цепочки, объекты входа, пары. Ноль объектов или пар — гейт не
// исполнился. Рядом инъекция: настоящий рендер, в который добавлена копия
// одного входа под другим именем, — находка называет пару и оба объекта.
package deploy_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// ingressRouteCensus — объём осмотренного одной цепочкой.
type ingressRouteCensus struct {
	Ingresses, Pairs int
}

// judgeIngressRouteOwners — находки «пара host+path объявлена больше одного
// раза» по документам одной цепочки. Чистая функция.
func judgeIngressRouteOwners(docs []map[string]any) ([]string, ingressRouteCensus) {
	var c ingressRouteCensus
	owners := map[string][]string{}
	for _, d := range docs {
		if docKind(d) != "Ingress" {
			continue
		}
		c.Ingresses++
		for _, r := range slice(submap(d, "spec"), "rules") {
			rm, _ := r.(map[string]any)
			host := str(rm, "host")
			for _, p := range slice(submap(rm, "http"), "paths") {
				pm, _ := p.(map[string]any)
				key := host + " " + str(pm, "path")
				owners[key] = append(owners[key], docName(d))
			}
		}
	}
	keys := make([]string, 0, len(owners))
	for k := range owners {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		c.Pairs++
		if len(owners[k]) > 1 {
			out = append(out, fmt.Sprintf("пара host+path %q объявлена %d раз: %s — контроллер отвергнет второй вход при приёме",
				k, len(owners[k]), strings.Join(owners[k], ", ")))
		}
	}
	return out, c
}

func TestEveryStackDeclaresEachIngressHostPathOnce(t *testing.T) {
	stacks := deployStacks(t)
	var total ingressRouteCensus
	for _, name := range sortedStackNames(stacks) {
		got, c := judgeIngressRouteOwners(npChainDocs(t, name))
		t.Logf("%s: объектов входа %d · пар host+path %d", name, c.Ingresses, c.Pairs)
		total.Ingresses += c.Ingresses
		total.Pairs += c.Pairs
		for _, f := range got {
			t.Errorf("%s: %s", name, f)
		}
	}
	t.Logf("перепись: цепочек %d · объектов входа %d · пар %d", len(stacks), total.Ingresses, total.Pairs)
	if total.Ingresses == 0 || total.Pairs == 0 {
		t.Fatalf("знаменатель пуст (%+v) — гейт не исполнился", total)
	}
}

// TestIngressRouteOwnerJudgeCatchesASecondEntry — близнец без находок, затем
// тот же рендер с копией одного объекта входа под другим именем.
func TestIngressRouteOwnerJudgeCatchesASecondEntry(t *testing.T) {
	const chain = "dev-prod"
	base := npChainDocs(t, chain)
	if got, c := judgeIngressRouteOwners(base); len(got) != 0 || c.Pairs == 0 {
		t.Fatalf("близнец %s: находки %v либо знаменатель пуст %+v", chain, got, c)
	}
	docs := npCopyDocs(base)
	var src map[string]any
	for _, d := range docs {
		if docKind(d) == "Ingress" {
			src = d
			break
		}
	}
	if src == nil {
		t.Fatalf("в рендере %s нет ни одного Ingress — предпосылка инъекции исчезла", chain)
	}
	dup := npCopyDocs([]map[string]any{src})[0]
	submap(dup, "metadata")["name"] = "second-entry-by-injection"
	docs = append(docs, dup)
	got, _ := judgeIngressRouteOwners(docs)
	if len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), "second-entry-by-injection") {
		t.Fatalf("второй вход на ту же пару не пойман либо находка не называет объект: %v", got)
	}
}
