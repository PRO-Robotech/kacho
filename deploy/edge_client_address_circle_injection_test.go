// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// edge_client_address_circle_injection_test.go — судья «заголовку адреса край
// доверяет только от звена фронта» СПОСОБЕН упасть и способен смолчать
// (kacho#3028). Две части:
//
//   - синтетика: каждая ось судьи — отдельный случай, меняющий ОДИН факт против
//     законного близнеца;
//   - настоящий рендер стенда предмета: близнец (рендер без правки) молчит,
//     а дефект, внесённый в него, называется своей находкой.
package deploy_test

import (
	"strings"
	"testing"
)

func synthEdge(circle string) map[string]any {
	return map[string]any{
		"kind": "Deployment", "metadata": map[string]any{"name": edgeDeploymentName},
		"spec": map[string]any{"template": map[string]any{
			"metadata": map[string]any{"labels": map[string]any{"app": "api-gateway"}},
			"spec": map[string]any{"containers": []any{map[string]any{
				"env": []any{map[string]any{"name": edgeCircleKnob, "value": circle}},
				"ports": []any{
					map[string]any{"name": "cmux", "containerPort": 8080},
					map[string]any{"name": "internal-rest", "containerPort": 8081},
					map[string]any{"name": "tls", "containerPort": 8443},
				},
			}}},
		}},
	}
}

func synthPolicy(rules ...any) map[string]any {
	return map[string]any{
		"kind": "NetworkPolicy", "metadata": map[string]any{"name": "api-gateway"},
		"spec": map[string]any{
			"podSelector": map[string]any{"matchLabels": map[string]any{"app": "api-gateway"}},
			"policyTypes": []any{"Ingress"},
			"ingress":     rules,
		},
	}
}

func rule(port any, from ...any) map[string]any {
	r := map[string]any{}
	if port != nil {
		r["ports"] = []any{map[string]any{"protocol": "TCP", "port": port}}
	}
	if len(from) > 0 {
		r["from"] = from
	}
	return r
}

var (
	frontPeer   = map[string]any{"podSelector": map[string]any{"matchLabels": map[string]any{"app": "uif", "app.kubernetes.io/component": "host"}}}
	metricsRule = rule(9095)
)

func TestEdgeAdmissionJudgement_CanFailAndStaysSilent(t *testing.T) {
	const private = "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,100.64.0.0/10,fc00::/7"
	for _, c := range []struct {
		name    string
		docs    []map[string]any
		mustSay string
	}{
		{name: "близнец: круг и политика, впускающая раздачу на cmux, сбор открыт",
			docs: []map[string]any{synthEdge(private), synthPolicy(rule("cmux", frontPeer), metricsRule)}},
		{name: "близнец: круг пуст — «никому», политика не нужна",
			docs: []map[string]any{synthEdge("")}},
		{name: "близнец: правило без портов, но с названным отправителем",
			docs: []map[string]any{synthEdge(private), synthPolicy(rule(nil, frontPeer))}},
		{name: "близнец: порт числом, отправитель назван",
			docs: []map[string]any{synthEdge(private), synthPolicy(rule(8443, frontPeer))}},
		{name: "ручки нет",
			docs: []map[string]any{func() map[string]any {
				d := synthEdge(private)
				c := d["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
				c["env"] = []any{}
				return d
			}()}, mustSay: "нет " + edgeCircleKnob},
		{name: "весь адресный простор", docs: []map[string]any{synthEdge("0.0.0.0/0"), synthPolicy(rule("cmux", frontPeer))},
			mustSay: "не законен"},
		{name: "круг непуст, политики нет", docs: []map[string]any{synthEdge(private)},
			mustSay: "не выбран ни одной политикой"},
		{name: "политика выбирает не край", docs: []map[string]any{synthEdge(private), func() map[string]any {
			p := synthPolicy(rule("cmux", frontPeer))
			p["spec"].(map[string]any)["podSelector"] = map[string]any{"matchLabels": map[string]any{"app": "vpc"}}
			return p
		}()}, mustSay: "не выбран ни одной политикой"},
		{name: "порт пересылки без отправителей", docs: []map[string]any{synthEdge(private), synthPolicy(rule("tls"))},
			mustSay: "открыто всем"},
		{name: "порт пересылки числом без отправителей", docs: []map[string]any{synthEdge(private), synthPolicy(rule(8080))},
			mustSay: "открыто всем"},
		{name: "все поды пространства", docs: []map[string]any{synthEdge(private),
			synthPolicy(rule("cmux", map[string]any{"podSelector": map[string]any{}}))},
			mustSay: "все поды пространства"},
		{name: "блок адресов", docs: []map[string]any{synthEdge(private),
			synthPolicy(rule("cmux", map[string]any{"ipBlock": map[string]any{"cidr": "10.0.0.0/8"}}))},
			mustSay: "блок адресов"},
		{name: "селектор пространства", docs: []map[string]any{synthEdge(private),
			synthPolicy(rule("tls", map[string]any{"namespaceSelector": map[string]any{}}))},
			mustSay: "селектору пространства"},
		{name: "правило без портов и без отправителей", docs: []map[string]any{synthEdge(private), synthPolicy(rule(nil))},
			mustSay: "открыто всем"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, a := judgeEdgeAdmission("инъекция", c.docs)
			if !a.Found {
				t.Fatal("край в синтетике не найден — случай ничего не проверил")
			}
			if c.mustSay == "" {
				if len(got) != 0 {
					t.Fatalf("близнец не молчит: %v", got)
				}
				return
			}
			if !strings.Contains(strings.Join(got, "\n"), c.mustSay) {
				t.Fatalf("ни одна находка не называет %q: %v", c.mustSay, got)
			}
		})
	}
}

// edgePolicies — политики рендера, выбирающие под края.
func edgePolicies(t *testing.T, docs []map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, d := range docs {
		if docKind(d) != "NetworkPolicy" {
			continue
		}
		spec, _ := d["spec"].(map[string]any)
		ml := stringMap(submap(spec, "podSelector")["matchLabels"])
		if ml["app"] == edgeDeploymentName {
			out = append(out, d)
		}
	}
	return out
}

func TestEdgeAdmissionRenderInjection_SubjectStand(t *testing.T) {
	docs := npChainDocs(t, circleSubjectStack)
	if got, a := judgeEdgeAdmission(circleSubjectStack, docs); len(got) != 0 || !a.Trusting || a.RulesJudged == 0 {
		t.Fatalf("законный близнец (рендер %s без правки): находок %d, круг непуст %v, правил %d — инъекции нечего доказывать:\n%s",
			circleSubjectStack, len(got), a.Trusting, a.RulesJudged, strings.Join(got, "\n"))
	}
	if len(edgePolicies(t, docs)) == 0 {
		t.Fatalf("в рендере %s нет политики на край — предпосылка инъекций исчезла", circleSubjectStack)
	}

	t.Run("политика края снята", func(t *testing.T) {
		var kept []map[string]any
		for _, d := range docs {
			if docKind(d) == "NetworkPolicy" && len(edgePolicies(t, []map[string]any{d})) == 1 {
				continue
			}
			kept = append(kept, d)
		}
		got, _ := judgeEdgeAdmission(circleSubjectStack, kept)
		if !strings.Contains(strings.Join(got, "\n"), "не выбран ни одной политикой") {
			t.Fatalf("снятая политика края не найдена: %v", got)
		}
	})
	t.Run("правило раздачи расширено до всех подов пространства", func(t *testing.T) {
		injected := npCopyDocs(docs)
		widened := 0
		for _, p := range edgePolicies(t, injected) {
			for _, ps := range npPeerSelectors(p) {
				delete(ps, "matchLabels")
				delete(ps, "matchExpressions")
				widened++
			}
		}
		if widened == 0 {
			t.Fatal("ни один отправитель политики края не назван селектором пода — предпосылка инъекции")
		}
		got, _ := judgeEdgeAdmission(circleSubjectStack, injected)
		if !strings.Contains(strings.Join(got, "\n"), "все поды пространства") {
			t.Fatalf("расширенное правило не найдено: %v", got)
		}
	})
	t.Run("круг края — весь адресный простор", func(t *testing.T) {
		injected := npCopyDocs(docs)
		for _, d := range injected {
			if docKind(d) != "Deployment" || docName(d) != edgeDeploymentName {
				continue
			}
			tpl, _ := podTemplateOf(d)
			for _, c := range slice(submap(tpl, "spec"), "containers") {
				for _, e := range slice(c.(map[string]any), "env") {
					if em := e.(map[string]any); em["name"] == edgeCircleKnob {
						em["value"] = "0.0.0.0/0"
					}
				}
			}
		}
		got, _ := judgeEdgeAdmission(circleSubjectStack, injected)
		if !strings.Contains(strings.Join(got, "\n"), "не законен") {
			t.Fatalf("круг на весь простор не найден: %v", got)
		}
	})
}

// Раздача консоли до порта края ДОХОДИТ: правило, впускающее её, снято из
// политики края — гейт политик сети (половина «достижимость») называет звонок
// раздачи краю. Без этого «только фронт» было бы «никто», и судья доверия
// молчал бы: закрытый порт ничего не открывает.
func TestEdgeAdmissionRenderInjection_FrontRuleDroppedIsAnUnreachableDial(t *testing.T) {
	docs := npChainDocs(t, circleSubjectStack)
	npTwinGreenOn(t, circleSubjectStack, docs)
	injected := npCopyDocs(docs)
	dropped := 0
	for _, p := range edgePolicies(t, injected) {
		spec, _ := p["spec"].(map[string]any)
		var kept []any
		for _, r := range slice(spec, "ingress") {
			rm, _ := r.(map[string]any)
			front := false
			for _, ps := range npPeerSelectors(map[string]any{"spec": map[string]any{"ingress": []any{rm}}}) {
				front = front || stringMap(ps["matchLabels"])["app.kubernetes.io/component"] == consoleHostComponent
			}
			if front {
				dropped++
				continue
			}
			kept = append(kept, r)
		}
		spec["ingress"] = kept
	}
	if dropped != 1 {
		t.Fatalf("правило раздачи в политике края найдено %d раз, ждали 1 — предпосылка инъекции", dropped)
	}
	v, err := judgeNetworkPolicies(npNamespace, injected)
	if err != nil {
		t.Fatalf("судья отказал на инъекции: %v", err)
	}
	if len(v.findings) != 1 || !strings.Contains(v.findings[0], "звонит api-gateway:8080") {
		t.Fatalf("ждали ровно одну находку «раздача звонит api-gateway:8080», есть %d:\n%s",
			len(v.findings), strings.Join(v.findings, "\n"))
	}
	t.Logf("находка: %s", v.findings[0])
}

func npTwinGreenOn(t *testing.T, stack string, docs []map[string]any) {
	t.Helper()
	v, err := judgeNetworkPolicies(npNamespace, docs)
	if err != nil || len(v.findings) != 0 {
		t.Fatalf("законный близнец (рендер %s без правки): ошибка %v, находки:\n%s", stack, err, strings.Join(v.findings, "\n"))
	}
}
