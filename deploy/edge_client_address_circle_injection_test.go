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

// synthEdge — край с кругом; при непустом круге звено поимённо — служба
// раздачи консоли (законный близнец узкого круга).
func synthEdge(circle string) map[string]any {
	peers := ""
	if circle != "" {
		peers = "front-console"
	}
	return synthEdgeWith(circle, peers)
}

func synthEdgeWith(circle, peers string) map[string]any {
	return map[string]any{
		"kind": "Deployment", "metadata": map[string]any{"name": edgeDeploymentName},
		"spec": map[string]any{"template": map[string]any{
			"metadata": map[string]any{"labels": map[string]any{"app": "api-gateway"}},
			"spec": map[string]any{"containers": []any{map[string]any{
				"env": []any{map[string]any{"name": edgeCircleKnob, "value": circle},
					map[string]any{"name": edgePeersKnob, "value": peers}},
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
				map[string]any{"name": "cmux", "port": 8080, "targetPort": "cmux"},
				map[string]any{"name": "tls", "port": 8443, "targetPort": "tls"},
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

// synthPeerService — безголовая служба звена: имя, которое край разрешает в
// адреса подов фронта.
func synthPeerService(name string, selector map[string]any) map[string]any {
	return map[string]any{
		"kind": "Service", "metadata": map[string]any{"name": name},
		"spec": map[string]any{"clusterIP": "None", "publishNotReadyAddresses": true, "selector": selector},
	}
}

var (
	consolePeerSvc = synthPeerService("front-console", map[string]any{"app": "uif", "app.kubernetes.io/component": "host"})
	ingressPeerSvc = synthPeerService("front-ingress", map[string]any{
		"app.kubernetes.io/name": "ingress-nginx", "app.kubernetes.io/component": "controller"})
)

// externalSvc — внешняя служба перед подами, выбранными селектором.
func externalSvc(name, typ, policy string, selector map[string]any) map[string]any {
	spec := map[string]any{"type": typ, "selector": selector,
		"ports": []any{map[string]any{"port": 443, "targetPort": 8443}}}
	if policy != "" {
		spec["externalTrafficPolicy"] = policy
	}
	return map[string]any{"kind": "Service", "metadata": map[string]any{"name": name}, "spec": spec}
}

// withLinks — рендер с краем, его службой, раздачей, службой её звена и
// соседом: фон, на котором судья отличает звено фронта от прочих подов.
func withLinks(circle string, extra ...map[string]any) []map[string]any {
	return withEdge(synthEdge(circle), extra...)
}

func withEdge(edge map[string]any, extra ...map[string]any) []map[string]any {
	return append([]map[string]any{edge, synthEdgeService(), synthFront, consolePeerSvc, synthNeighbour}, extra...)
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
		// Звену открывается ровно порт, до которого оно звонит (kacho#3028, C5):
		// правило без портов открывает раздаче и internal-rest, и tls, до которых
		// она не звонит, — вход края мимо её полосы.
		{name: "правило без портов, отправитель — раздача",
			docs:    withLinks(private, synthPolicy(rule(nil, frontPeer))),
			mustSay: "звену Deployment/uif открыт порт края 8081, до которого оно не звонит"},
		{name: "близнец: порт числом, отправитель — раздача",
			docs: withLinks(private, synthPolicy(rule(8080, frontPeer)))},
		{name: "раздаче открыт tls, до которого она не звонит",
			docs:    withLinks(private, synthPolicy(rule(8443, frontPeer))),
			mustSay: "звену Deployment/uif открыт порт края 8443, до которого оно не звонит"},
		{name: "два отправителя в одном правиле",
			docs: withEdge(synthEdgeWith(private, "front-console,front-ingress"), synthController, ingressPeerSvc,
				synthEdgeIngress, synthPolicy(rule("cmux", frontPeer, controllerPeer), rule("tls", controllerPeer))),
			mustSay: "отправителей 2, ожидался ровно один"},
		{name: "политика края без policyTypes",
			docs: withLinks(private, func() map[string]any {
				d := synthPolicy(rule("cmux", frontPeer))
				delete(d["spec"].(map[string]any), "policyTypes")
				return d
			}()),
			mustSay: "policyTypes не объявляет Ingress явно"},
		{name: "близнец: контроллер входа, ведущего на край, на tls",
			docs: withEdge(synthEdgeWith(private, "front-console,front-ingress"), synthController, ingressPeerSvc,
				synthEdgeIngress, synthPolicy(rule("cmux", frontPeer), rule("tls", controllerPeer)))},
		// Узкий круг (kacho#3028, круг 3): звено узнаётся поимённо.
		{name: "близнец: имя звена с пространством имён",
			docs: withEdge(synthEdgeWith(private, "front-console.kacho.svc"), synthPolicy(rule("cmux", frontPeer)))},
		{name: "близнец: внешняя служба перед раздачей с политикой Local",
			docs: withLinks(private, synthPolicy(rule("cmux", frontPeer)),
				externalSvc("console-lb", "LoadBalancer", "Local", map[string]any{"app": "uif"}))},
		{name: "близнец: внешняя служба Cluster, но перед соседом, а не звеном",
			docs: withLinks(private, synthPolicy(rule("cmux", frontPeer)),
				externalSvc("vpc-lb", "LoadBalancer", "", map[string]any{"app": "uif-vpc"}))},
		{name: "круг непуст, звеньев поимённо нет",
			docs:    withEdge(synthEdgeWith(private, ""), synthPolicy(rule("cmux", frontPeer))),
			mustSay: "звеньев поимённо нет"},
		{name: "круг пуст, звенья поимённо объявлены",
			docs:    []map[string]any{synthEdgeWith("", "front-console")},
			mustSay: "край откажет в старте"},
		{name: "адрес вместо имени звена",
			docs:    withEdge(synthEdgeWith(private, "10.244.1.17"), synthPolicy(rule("cmux", frontPeer))),
			mustSay: "не законны"},
		{name: "имя звена не названо службой рендера",
			docs:    withEdge(synthEdgeWith(private, "front-console,front-ghost"), synthPolicy(rule("cmux", frontPeer))),
			mustSay: "звено \"front-ghost\""},
		{name: "служба звена не безголовая",
			docs: withEdge(synthEdgeWith(private, "front-console,front-vip"), synthPolicy(rule("cmux", frontPeer)),
				func() map[string]any {
					d := synthPeerService("front-vip", map[string]any{"app": "uif"})
					d["spec"].(map[string]any)["clusterIP"] = "10.96.0.40"
					return d
				}()),
			mustSay: "служба звена front-vip не безголовая"},
		{name: "служба звена выбирает соседа",
			docs: withEdge(synthEdgeWith(private, "front-console,front-wide"), synthPolicy(rule("cmux", frontPeer)),
				synthPeerService("front-wide", map[string]any{"app.kubernetes.io/instance": "rel"})),
			mustSay: "служба звена front-wide: селектор {app.kubernetes.io/instance=rel} выбирает Deployment/uif-vpc"},
		{name: "служба звена не выбирает ни одного пода рендера",
			docs: withEdge(synthEdgeWith(private, "front-console,front-admin"), synthPolicy(rule("cmux", frontPeer)),
				synthPeerService("front-admin", map[string]any{"role": "admin"})),
			mustSay: "служба звена front-admin: селектор {role=admin} не выбирает ни одного пода"},
		{name: "служба звена без селектора",
			docs: withEdge(synthEdgeWith(private, "front-console,front-manual"), synthPolicy(rule("cmux", frontPeer)),
				synthPeerService("front-manual", nil)),
			mustSay: "служба звена front-manual без селектора"},
		{name: "звено фронта (контроллер входа) не названо поимённо",
			docs: withEdge(synthEdgeWith(private, "front-console"), synthController, ingressPeerSvc, synthEdgeIngress,
				synthPolicy(rule("cmux", frontPeer), rule("tls", controllerPeer))),
			mustSay: "звено фронта Deployment/ingress-nginx-controller не выбрано ни одной службой"},
		{name: "внешняя служба перед раздачей без политики — Cluster по умолчанию",
			docs: withLinks(private, synthPolicy(rule("cmux", frontPeer)),
				externalSvc("console-lb", "LoadBalancer", "", map[string]any{"app": "uif"})),
			mustSay: "внешняя служба console-lb (LoadBalancer) перед звеном фронта Deployment/uif: externalTrafficPolicy \"—\""},
		{name: "внешняя служба NodePort перед контроллером входа — Cluster",
			docs: withEdge(synthEdgeWith(private, "front-console,front-ingress"), synthController, ingressPeerSvc,
				synthEdgeIngress, synthPolicy(rule("cmux", frontPeer), rule("tls", controllerPeer)),
				externalSvc("ingress-lb", "NodePort", "Cluster", map[string]any{"app.kubernetes.io/name": "ingress-nginx"})),
			mustSay: "внешняя служба ingress-lb (NodePort) перед звеном фронта Deployment/ingress-nginx-controller: externalTrafficPolicy \"Cluster\""},
		// Диапазона портов в политике края нет вовсе (kacho#3028, C5): даже мимо
		// портов края или по UDP он — открытие «на будущее» соседних портов.
		{name: "диапазон портов мимо портов края",
			docs:    withLinks(private, synthPolicy(rule("cmux", frontPeer), rangeRule(9000, 9100, "TCP"))),
			mustSay: "диапазон портов 9000-9100 (endPort)"},
		{name: "диапазон портов края по UDP",
			docs:    withLinks(private, synthPolicy(rule("cmux", frontPeer), rangeRule(1024, 65535, "UDP"))),
			mustSay: "диапазон портов 1024-65535 (endPort)"},
		{name: "близнец: порт сбора величин одним номером без отправителей",
			docs: withLinks(private, synthPolicy(rule("cmux", frontPeer), metricsRule))},
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
	// Узкий круг (kacho#3028, круг 3) — дефект в настоящем рендере.
	t.Run("звенья поимённо сняты: доверие всей сети подов", func(t *testing.T) {
		judgeInjected(t, setEdgeEnv(t, npCopyDocs(docs), edgePeersKnob, ""), "звеньев поимённо нет")
	})
	t.Run("служба звена расширена до всей установки", func(t *testing.T) {
		injected := npCopyDocs(docs)
		peerServiceSelector(t, injected, frontConsolePeerService)["app.kubernetes.io/instance"] = "kacho-umbrella"
		for k := range peerServiceSelector(t, injected, frontConsolePeerService) {
			if k != "app.kubernetes.io/instance" {
				delete(peerServiceSelector(t, injected, frontConsolePeerService), k)
			}
		}
		judgeInjected(t, injected, "не звено фронта: его заголовок адреса край примет")
	})
	t.Run("служба звена снята", func(t *testing.T) {
		var kept []map[string]any
		for _, d := range npCopyDocs(docs) {
			if docKind(d) == "Service" && docName(d) == frontConsolePeerService {
				continue
			}
			kept = append(kept, d)
		}
		judgeInjected(t, kept, "не названо ни одной службой рендера")
	})
	// Внешний вход раздачи профиль стенда сегодня выключает (kacho#3024: вход
	// переезжает на доменное имя), поэтому служба раздачи судится на ФИКСТУРЕ —
	// той же цепочке со входом, включённым ручками чарта (publicFrontFixtureSets).
	// Близнец — фикстура без правки — молчит.
	t.Run("внешняя служба раздачи без Local", func(t *testing.T) {
		front := npChainDocsWith(t, circleSubjectStack, publicFrontFixtureSets...)
		if got, a := judgeEdgeAdmission(circleSubjectStack, front); len(got) != 0 || a.ExternalJudged == 0 {
			t.Fatalf("законный близнец (фикстура входа без правки): находок %d, внешних служб сверено %d:\n%s",
				len(got), a.ExternalJudged, strings.Join(got, "\n"))
		}
		injected := npCopyDocs(front)
		if dropExternalPolicy(injected) == 0 {
			t.Fatal("в рендере фикстуры нет внешней службы с политикой Local — предпосылка инъекции")
		}
		got, _ := judgeEdgeAdmission(circleSubjectStack, injected)
		if !strings.Contains(strings.Join(got, "\n"), "externalTrafficPolicy \"—\", ожидался Local") {
			t.Fatalf("ни одна находка не называет службу раздачи без Local:\n%s", strings.Join(got, "\n"))
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
	if len(v.findings) != 1 || !strings.Contains(v.findings[0], "звонит api-gateway:8443") {
		t.Fatalf("ждали ровно одну находку «раздача звонит api-gateway:8443», есть %d:\n%s",
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

// frontConsolePeerService / frontIngressPeerService — безголовые службы
// звеньев, которые рендерит умбрелла (templates/service-api-gateway-front-links.yaml).
const (
	frontConsolePeerService = "api-gateway-front-console"
	frontIngressPeerService = "api-gateway-front-ingress"
)

// setEdgeEnv — значение ручки у контейнеров края в копии рендера.
func setEdgeEnv(t *testing.T, docs []map[string]any, knob, value string) []map[string]any {
	t.Helper()
	set := 0
	for _, d := range docs {
		if docKind(d) != "Deployment" || docName(d) != edgeDeploymentName {
			continue
		}
		tpl, _ := podTemplateOf(d)
		for _, c := range slice(submap(tpl, "spec"), "containers") {
			for _, e := range slice(c.(map[string]any), "env") {
				if em := e.(map[string]any); em["name"] == knob {
					em["value"] = value
					set++
				}
			}
		}
	}
	if set == 0 {
		t.Fatalf("у края нет %s — предпосылка инъекции", knob)
	}
	return docs
}

// peerServiceSelector — селектор безголовой службы звена в копии рендера.
func peerServiceSelector(t *testing.T, docs []map[string]any, name string) map[string]any {
	t.Helper()
	for _, d := range docs {
		if docKind(d) == "Service" && docName(d) == name {
			if sel, ok := submap(d, "spec")["selector"].(map[string]any); ok {
				return sel
			}
		}
	}
	t.Fatalf("службы звена %s с селектором в рендере нет — предпосылка инъекции", name)
	return nil
}

// dropExternalPolicy — снимает externalTrafficPolicy Local у внешних служб.
func dropExternalPolicy(docs []map[string]any) int {
	n := 0
	for _, d := range docs {
		spec := submap(d, "spec")
		if docKind(d) == "Service" && spec["externalTrafficPolicy"] == "Local" {
			delete(spec, "externalTrafficPolicy")
			n++
		}
	}
	return n
}

// Контроллер входа — звено фронта цепочки prod (kacho#3028, круг 3,
// security-auditor r3): его внешняя служба обязана нести Local, а сам он —
// быть названным поимённо. Близнец — рендер без правки — молчит.
func TestEdgeAdmissionRenderInjection_IngressControllerFront(t *testing.T) {
	const stack = "prod"
	docs := npChainDocs(t, stack)
	got, a := judgeEdgeAdmission(stack, docs)
	if len(got) != 0 || !a.Trusting || a.ExternalJudged == 0 || a.PeersJudged == 0 {
		t.Fatalf("законный близнец (рендер %s без правки): находок %d, круг непуст %v, внешних служб %d, служб звеньев %d:\n%s",
			stack, len(got), a.Trusting, a.ExternalJudged, a.PeersJudged, strings.Join(got, "\n"))
	}
	judge := func(t *testing.T, injected []map[string]any, mustSay string) {
		t.Helper()
		got, _ := judgeEdgeAdmission(stack, injected)
		if joined := strings.Join(got, "\n"); !strings.Contains(joined, mustSay) {
			t.Fatalf("ни одна находка не называет %q:\n%s", mustSay, joined)
		}
	}
	t.Run("внешняя служба контроллера без Local", func(t *testing.T) {
		injected := npCopyDocs(docs)
		if dropExternalPolicy(injected) == 0 {
			t.Fatal("в рендере нет внешней службы с политикой Local — предпосылка инъекции")
		}
		judge(t, injected, "перед звеном фронта Deployment/kacho-umbrella-ingress-nginx-controller: externalTrafficPolicy")
	})
	t.Run("контроллер не назван поимённо", func(t *testing.T) {
		judge(t, setEdgeEnv(t, npCopyDocs(docs), edgePeersKnob, frontConsolePeerService),
			"звено \""+frontConsolePeerService+"\" ("+edgePeersKnob+") не названо ни одной службой рендера")
	})
	t.Run("служба звена контроллера выбирает и джобы допуска", func(t *testing.T) {
		injected := npCopyDocs(docs)
		delete(peerServiceSelector(t, injected, frontIngressPeerService), "app.kubernetes.io/component")
		judge(t, injected, "не звено фронта: его заголовок адреса край примет")
	})
}

// ФОРМА ПОЛИТИКИ КРАЯ (kacho#3028, C5) на настоящем рендере цепочки own — там
// к краю звонят оба звена фронта: раздача консоли (порт cmux) и контроллер
// входа (порт tls). Каждая инъекция меняет в политике края ОДИН факт; законный
// близнец — рендер без правки — молчит. Инъекции I2b, I3, I7 — те, что
// check-verifier r2 пронёс мимо гейта зелёными.
func TestEdgeAdmissionRenderInjection_PolicyShape(t *testing.T) {
	const stack = "own"
	docs := npChainDocs(t, stack)
	got, a := judgeEdgeAdmission(stack, docs)
	if len(got) != 0 || !a.Trusting || len(a.Links) < 2 || a.RulesJudged < 2 {
		t.Fatalf("законный близнец (рендер %s без правки): находок %d, круг непуст %v, звеньев %v, правил %d — "+
			"инъекциям нечего доказывать:\n%s", stack, len(got), a.Trusting, a.Links, a.RulesJudged, strings.Join(got, "\n"))
	}
	// edgeRules — правила входа политики края в копии рендера.
	edgeRules := func(t *testing.T, docs []map[string]any) (map[string]any, []any) {
		t.Helper()
		ps := edgePolicies(t, docs)
		if len(ps) != 1 {
			t.Fatalf("политик на край %d, ожидалась одна — предпосылка инъекции", len(ps))
		}
		spec := submap(ps[0], "spec")
		return spec, slice(spec, "ingress")
	}
	// ruleOfSender — правило политики края, чей отправитель — под с меткой
	// компонента component (раздача консоли и контроллер входа оба звонят на
	// порт `tls`, kacho#3028 круг 5, — правило различает отправитель, а не порт).
	ruleOfSender := func(t *testing.T, rules []any, component string) map[string]any {
		t.Helper()
		for _, r := range rules {
			rm, _ := r.(map[string]any)
			for _, ps := range npPeerSelectors(map[string]any{"spec": map[string]any{"ingress": []any{rm}}}) {
				if stringMap(ps["matchLabels"])["app.kubernetes.io/component"] == component {
					return rm
				}
			}
		}
		t.Fatalf("правила с отправителем %q в политике края нет — предпосылка инъекции", component)
		return nil
	}
	cases := []struct {
		name    string
		mutate  func(t *testing.T, docs []map[string]any) []map[string]any
		mustSay string
	}{
		{
			name: "I2b селектор раздачи расширен до всей установки",
			mutate: func(t *testing.T, docs []map[string]any) []map[string]any {
				_, rules := edgeRules(t, docs)
				from := slice(ruleOfSender(t, rules, consoleHostComponent), "from")
				from[0] = map[string]any{"podSelector": map[string]any{
					"matchLabels": map[string]any{"app.kubernetes.io/instance": "kacho-umbrella"}}}
				return docs
			},
			mustSay: "не звено фронта",
		},
		{
			name: "I3 соседняя политика без policyTypes открывает край всем",
			mutate: func(t *testing.T, docs []map[string]any) []map[string]any {
				return append(docs, map[string]any{
					"kind": "NetworkPolicy", "metadata": map[string]any{"name": "allow-all-edge"},
					"spec": map[string]any{
						"podSelector": map[string]any{"matchLabels": map[string]any{"app": edgeDeploymentName}},
						"ingress":     []any{map[string]any{}},
					},
				})
			},
			mustSay: "allow-all-edge, правило 0 (порты все) открыто всем",
		},
		{
			name: "I3' политика края без policyTypes",
			mutate: func(t *testing.T, docs []map[string]any) []map[string]any {
				spec, _ := edgeRules(t, docs)
				delete(spec, "policyTypes")
				return docs
			},
			mustSay: "policyTypes не объявляет Ingress явно",
		},
		{
			name: "I7 правило сбора величин диапазоном портов",
			mutate: func(t *testing.T, docs []map[string]any) []map[string]any {
				_, rules := edgeRules(t, docs)
				for _, r := range rules {
					rm, _ := r.(map[string]any)
					if len(slice(rm, "from")) == 0 {
						rm["ports"] = []any{map[string]any{"protocol": "TCP", "port": 1024, "endPort": 65535}}
						return docs
					}
				}
				t.Fatal("правила сбора величин в политике края нет — предпосылка инъекции")
				return nil
			},
			mustSay: "(endPort)",
		},
		{
			name: "второй отправитель в правиле звена",
			mutate: func(t *testing.T, docs []map[string]any) []map[string]any {
				_, rules := edgeRules(t, docs)
				console := ruleOfSender(t, rules, consoleHostComponent)
				controller := ruleOfSender(t, rules, "controller")
				console["from"] = append(slice(console, "from"), npDeepCopy(slice(controller, "from")[0]))
				return docs
			},
			mustSay: "отправителей 2, ожидался ровно один",
		},
		{
			name: "звену открыт internal-rest, до которого не звонит никто",
			mutate: func(t *testing.T, docs []map[string]any) []map[string]any {
				_, rules := edgeRules(t, docs)
				console := ruleOfSender(t, rules, consoleHostComponent)
				console["ports"] = append(slice(console, "ports"), map[string]any{"protocol": "TCP", "port": "internal-rest"})
				return docs
			},
			mustSay: "открыт порт края 8081, до которого оно не звонит",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			injected := c.mutate(t, npCopyDocs(docs))
			got, _ := judgeEdgeAdmission(stack, injected)
			if !strings.Contains(strings.Join(got, "\n"), c.mustSay) {
				t.Fatalf("ни одна находка не называет %q:\n%s", c.mustSay, strings.Join(got, "\n"))
			}
		})
	}
}
