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
	"fmt"
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

// rangeRule — правило, открывающее диапазон портов [from, to] без отправителей.
func rangeRule(from, to int, proto string) map[string]any {
	return map[string]any{"ports": []any{map[string]any{"protocol": proto, "port": from, "endPort": to}}}
}

// synthWorkload — Deployment с метками пода и строкой настроек.
func synthWorkload(name string, labels map[string]any, env ...string) map[string]any {
	var envs []any
	for i, v := range env {
		envs = append(envs, map[string]any{"name": fmt.Sprintf("ADDR_%d", i), "value": v})
	}
	return map[string]any{
		"kind": "Deployment", "metadata": map[string]any{"name": name},
		"spec": map[string]any{"template": map[string]any{
			"metadata": map[string]any{"labels": labels},
			"spec":     map[string]any{"containers": []any{map[string]any{"env": envs}}},
		}},
	}
}

// synthEdgeService — служба края: имя, по которому звено фронта звонит краю.
func synthEdgeService() map[string]any {
	return map[string]any{
		"kind": "Service", "metadata": map[string]any{"name": edgeDeploymentName},
		"spec": map[string]any{
			"selector": map[string]any{"app": "api-gateway"},
			"ports": []any{
				map[string]any{"port": 8080, "targetPort": "cmux"},
				map[string]any{"port": 8443, "targetPort": "tls"},
			},
		},
	}
}

var (
	frontLabels = map[string]any{"app": "uif", "app.kubernetes.io/component": "host", "app.kubernetes.io/instance": "rel"}
	frontPeer   = map[string]any{"podSelector": map[string]any{"matchLabels": map[string]any{"app": "uif", "app.kubernetes.io/component": "host"}}}
	metricsRule = rule(9095)
	// Раздача консоли — звено фронта: адрес края у неё в настройках.
	synthFront = synthWorkload("uif", frontLabels, "http://api-gateway:8080")
	// Соседний рабочий объект той же установки — к краю не звонит.
	synthNeighbour = synthWorkload("uif-vpc", map[string]any{"app": "uif-vpc", "app.kubernetes.io/instance": "rel"})
	// Контроллер входа класса nginx и вход, ведущий на край.
	synthController = func() map[string]any {
		d := synthWorkload("ingress-nginx-controller", map[string]any{
			"app.kubernetes.io/name": "ingress-nginx", "app.kubernetes.io/component": "controller", "app.kubernetes.io/instance": "rel"})
		c := d["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
		c["args"] = []any{"/nginx-ingress-controller", "--controller-class=k8s.io/ingress-nginx", "--ingress-class=nginx"}
		return d
	}()
	synthEdgeIngress = map[string]any{
		"kind": "Ingress", "metadata": map[string]any{"name": "api-gateway"},
		"spec": map[string]any{"ingressClassName": "nginx", "rules": []any{map[string]any{"http": map[string]any{
			"paths": []any{map[string]any{"path": "/", "backend": map[string]any{"service": map[string]any{
				"name": edgeDeploymentName, "port": map[string]any{"name": "tls"}}}}}}}}},
	}
	controllerPeer = map[string]any{"podSelector": map[string]any{"matchLabels": map[string]any{
		"app.kubernetes.io/name": "ingress-nginx", "app.kubernetes.io/component": "controller"}}}
)

// withLinks — рендер с краем, его службой, раздачей и соседом: фон, на
// котором судья отличает звено фронта от прочих подов.
func withLinks(circle string, extra ...map[string]any) []map[string]any {
	return append([]map[string]any{synthEdge(circle), synthEdgeService(), synthFront, synthNeighbour}, extra...)
}

func TestEdgeAdmissionJudgement_CanFailAndStaysSilent(t *testing.T) {
	const private = "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,100.64.0.0/10,fc00::/7"
	noTypes := func(p map[string]any) map[string]any {
		delete(p["spec"].(map[string]any), "policyTypes")
		return p
	}
	emptyTypes := func(p map[string]any) map[string]any {
		p["spec"].(map[string]any)["policyTypes"] = []any{}
		return p
	}
	egressOnly := func(p map[string]any) map[string]any {
		p["spec"].(map[string]any)["policyTypes"] = []any{"Egress"}
		return p
	}
	named := func(name string, p map[string]any) map[string]any {
		p["metadata"] = map[string]any{"name": name}
		return p
	}
	for _, c := range []struct {
		name    string
		docs    []map[string]any
		mustSay string
	}{
		{name: "близнец: круг и политика, впускающая раздачу на cmux, сбор открыт",
			docs: withLinks(private, synthPolicy(rule("cmux", frontPeer), metricsRule))},
		{name: "близнец: круг пуст — «никому», политика не нужна",
			docs: []map[string]any{synthEdge("")}},
		{name: "близнец: правило без портов, но с отправителем — раздачей",
			docs: withLinks(private, synthPolicy(rule(nil, frontPeer)))},
		{name: "близнец: порт числом, отправитель — раздача",
			docs: withLinks(private, synthPolicy(rule(8443, frontPeer)))},
		{name: "близнец: контроллер входа, ведущего на край, на tls",
			docs: withLinks(private, synthController, synthEdgeIngress, synthPolicy(rule("cmux", frontPeer), rule("tls", controllerPeer)))},
		{name: "близнец: диапазон портов мимо портов края открыт всем",
			docs: withLinks(private, synthPolicy(rule("cmux", frontPeer), rangeRule(9000, 9100, "TCP")))},
		{name: "близнец: диапазон портов края открыт всем, но по UDP",
			docs: withLinks(private, synthPolicy(rule("cmux", frontPeer), rangeRule(1024, 65535, "UDP")))},
		{name: "близнец: доп. политика только на исход, её правила входа не действуют",
			docs: withLinks(private, synthPolicy(rule("cmux", frontPeer)), named("egress-only", egressOnly(synthPolicy(rule(nil)))))},
		{name: "ручки нет",
			docs: []map[string]any{func() map[string]any {
				d := synthEdge(private)
				c := d["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
				c["env"] = []any{}
				return d
			}()}, mustSay: "нет " + edgeCircleKnob},
		{name: "весь адресный простор", docs: withLinks("0.0.0.0/0", synthPolicy(rule("cmux", frontPeer))),
			mustSay: "не законен"},
		{name: "круг непуст, политики нет", docs: withLinks(private),
			mustSay: "не выбран ни одной политикой"},
		{name: "политика выбирает не край", docs: withLinks(private, func() map[string]any {
			p := synthPolicy(rule("cmux", frontPeer))
			p["spec"].(map[string]any)["podSelector"] = map[string]any{"matchLabels": map[string]any{"app": "vpc"}}
			return p
		}()), mustSay: "не выбран ни одной политикой"},
		{name: "порт пересылки без отправителей", docs: withLinks(private, synthPolicy(rule("tls"))),
			mustSay: "открыто всем"},
		{name: "порт пересылки числом без отправителей", docs: withLinks(private, synthPolicy(rule(8080))),
			mustSay: "открыто всем"},
		{name: "все поды пространства", docs: withLinks(private,
			synthPolicy(rule("cmux", map[string]any{"podSelector": map[string]any{}}))),
			mustSay: "все поды пространства"},
		{name: "блок адресов", docs: withLinks(private,
			synthPolicy(rule("cmux", map[string]any{"ipBlock": map[string]any{"cidr": "10.0.0.0/8"}}))),
			mustSay: "блок адресов"},
		{name: "селектор пространства", docs: withLinks(private,
			synthPolicy(rule("tls", map[string]any{"namespaceSelector": map[string]any{}}))),
			mustSay: "селектору пространства"},
		{name: "правило без портов и без отправителей", docs: withLinks(private, synthPolicy(rule(nil))),
			mustSay: "открыто всем"},
		// Формы записи, которых судья прежде не знал (kacho#3028, круг 2).
		{name: "доп. политика без policyTypes открывает край всем",
			docs:    withLinks(private, synthPolicy(rule("cmux", frontPeer)), named("extra", noTypes(synthPolicy(rule(nil))))),
			mustSay: "политика extra, правило 0 (порты все) открыто всем"},
		{name: "доп. политика с пустым policyTypes открывает край всем",
			docs:    withLinks(private, synthPolicy(rule("cmux", frontPeer)), named("extra", emptyTypes(synthPolicy(rule(nil))))),
			mustSay: "политика extra, правило 0 (порты все) открыто всем"},
		{name: "диапазон портов endPort накрывает cmux, отправителей нет",
			docs:    withLinks(private, synthPolicy(rule("cmux", frontPeer), rangeRule(1024, 65535, "TCP"))),
			mustSay: "правило 1 (порты 8080,8081,8443) открыто всем"},
		{name: "селектор отправителя выбирает не только раздачу",
			docs: withLinks(private, synthPolicy(rule("cmux", map[string]any{"podSelector": map[string]any{
				"matchLabels": map[string]any{"app.kubernetes.io/instance": "rel"}}}))),
			mustSay: "впускает Deployment/uif-vpc — не звено фронта"},
		{name: "селектор отправителя выражением выбирает соседа",
			docs: withLinks(private, synthPolicy(rule("cmux", map[string]any{"podSelector": map[string]any{
				"matchExpressions": []any{map[string]any{"key": "app", "operator": "Exists"}}}}))),
			mustSay: "впускает Deployment/uif-vpc — не звено фронта"},
		{name: "селектор отправителя не выбирает ни одного пода рендера (поды администрирования)",
			docs: withLinks(private, synthPolicy(rule("cmux", frontPeer), rule("cmux", map[string]any{"podSelector": map[string]any{
				"matchLabels": map[string]any{"role": "admin"}}}))),
			mustSay: "не выбирает ни одного пода рендера"},
		{name: "контроллер входа без входа, ведущего на край, — не звено",
			docs:    withLinks(private, synthController, synthPolicy(rule("cmux", frontPeer), rule("tls", controllerPeer))),
			mustSay: "впускает Deployment/ingress-nginx-controller — не звено фронта"},
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
	// Слепые формы круга 1 (check-verifier r2: I2b, I3, I7) — каждая внесена в
	// настоящий рендер и называется своей находкой; законный близнец той же
	// формы молчит.
	judgeInjected := func(t *testing.T, injected []map[string]any, mustSay string) {
		t.Helper()
		got, _ := judgeEdgeAdmission(circleSubjectStack, injected)
		joined := strings.Join(got, "\n")
		if mustSay == "" {
			if len(got) != 0 {
				t.Fatalf("законный близнец не молчит:\n%s", joined)
			}
			return
		}
		if !strings.Contains(joined, mustSay) {
			t.Fatalf("ни одна находка не называет %q:\n%s", mustSay, joined)
		}
		t.Logf("находки: %s", joined)
	}
	appendRule := func(t *testing.T, r map[string]any) []map[string]any {
		t.Helper()
		injected := npCopyDocs(docs)
		pols := edgePolicies(t, injected)
		if len(pols) != 1 {
			t.Fatalf("политик края %d, ждали 1 — предпосылка инъекции", len(pols))
		}
		spec := pols[0]["spec"].(map[string]any)
		spec["ingress"] = append(slice(spec, "ingress"), r)
		return injected
	}
	extraPolicy := func(types []any) []map[string]any {
		p := synthPolicy(map[string]any{})
		p["metadata"] = map[string]any{"name": "extra-open"}
		spec := p["spec"].(map[string]any)
		spec["podSelector"] = map[string]any{"matchLabels": map[string]any{"app": edgeDeploymentName}}
		if types == nil {
			delete(spec, "policyTypes")
		} else {
			spec["policyTypes"] = types
		}
		return append(npCopyDocs(docs), p)
	}
	t.Run("I2b: отправитель правила раздачи расширен до всей установки", func(t *testing.T) {
		injected := npCopyDocs(docs)
		widened := 0
		for _, p := range edgePolicies(t, injected) {
			for _, ps := range npPeerSelectors(p) {
				ps["matchLabels"] = map[string]any{"app.kubernetes.io/instance": "kacho-umbrella"}
				delete(ps, "matchExpressions")
				widened++
			}
		}
		if widened == 0 {
			t.Fatal("ни один отправитель политики края не назван селектором пода — предпосылка инъекции")
		}
		judgeInjected(t, injected, "— не звено фронта")
	})
	t.Run("I3: доп. политика на край без policyTypes открывает всё всем", func(t *testing.T) {
		judgeInjected(t, extraPolicy(nil), "политика extra-open, правило 0 (порты все) открыто всем")
	})
	t.Run("I3: доп. политика на край с пустым policyTypes открывает всё всем", func(t *testing.T) {
		judgeInjected(t, extraPolicy([]any{}), "политика extra-open, правило 0 (порты все) открыто всем")
	})
	t.Run("T3: доп. политика только на исход — близнец молчит", func(t *testing.T) {
		judgeInjected(t, extraPolicy([]any{"Egress"}), "")
	})
	t.Run("I7: правило диапазоном 1024-65535 без отправителей", func(t *testing.T) {
		judgeInjected(t, appendRule(t, map[string]any{"ports": []any{map[string]any{"protocol": "TCP", "port": 1024, "endPort": 65535}}}),
			"открыто всем")
	})
	t.Run("T7: правило одним портом 1024 без отправителей — близнец молчит", func(t *testing.T) {
		judgeInjected(t, appendRule(t, map[string]any{"ports": []any{map[string]any{"protocol": "TCP", "port": 1024}}}), "")
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
