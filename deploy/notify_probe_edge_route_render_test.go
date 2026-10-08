// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// notify_probe_edge_route_render_test.go — маршрут края к стендовой пробе
// notify-probe сходится с самой пробой в каждой цепочке stacks.txt.
//
// ПРЕДМЕТ. Решение владельца 2026-10-08 (1): InternalNotifyProbeService/Send
// зовёт администратор кластера своей личностью через ВНУТРЕННИЙ блок края.
// Процесс края регистрирует этот маршрут только при объявленном адресе
// (KACHO_API_GATEWAY_NOTIFY_PROBE_INTERNAL_GRPC), а адрес объявляет чарт края по
// профилю. Без этого держателя ручка процесса была выразима и не произведена
// ни одним профилем: проба поднималась, а маршрута к ней не было — и это
// неотличимо от исправного по готовности подов.
//
// ЧТО СУДИТСЯ в каждой цепочке:
//
//	(а) проба в рендере ⇔ адрес пробы у края;
//	(б) адрес у края равен службе пробы <имя>.<ns>.svc:<порт grpc-internal>;
//	(в) адрес у края только вместе с ребром mTLS к пробе — слушатели пробы
//	    принимают только mTLS.
//
// ЧЕГО НЕ ДЕЛАЕТ: не поднимает стенд и не утверждает, что маршрут ОТВЕЧАЕТ, —
// это проба на поднятом стенде; не судит, что маршрут живёт только во
// внутреннем блоке, — это держат пробы края (gateway/internal/restmux,
// notify_probe_routability_test.go).

import (
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	edgeNotifyProbeAddrEnv = "KACHO_API_GATEWAY_NOTIFY_PROBE_INTERNAL_GRPC"
	edgeNotifyProbeMTLSEnv = "KACHO_API_GATEWAY_MTLS_NOTIFY_PROBE_ENABLE"
	notifyProbeComponent   = "notify-probe"
)

type notifyProbeRouteView struct {
	probeDeployments int
	serviceAddr      string // <имя>.<ns>.svc:<порт grpc-internal> службы пробы
	edgeAddr         string
	edgeMTLS         string
	edgeSeen         bool
}

// notifyProbeRouteViewOf — вид цепочки: проба по метке компонента, край — по
// источнику подчарта края.
func notifyProbeRouteViewOf(t *testing.T, rendered, ns string) notifyProbeRouteView {
	t.Helper()
	var v notifyProbeRouteView
	all := parseRendered(t, rendered)
	for _, d := range objsOfKind(all, "Deployment") {
		if nstr(ndig(d.doc, "metadata", "labels", "kacho.cloud/component")) == notifyProbeComponent {
			v.probeDeployments++
		}
	}
	for _, s := range objsOfKind(all, "Service") {
		if nstr(ndig(s.doc, "metadata", "labels", "kacho.cloud/component")) != notifyProbeComponent {
			continue
		}
		for _, p := range nlist(ndig(s.doc, "spec", "ports")) {
			if nstr(ndig(p, "name")) == "grpc-internal" {
				port, _ := ndig(p, "port").(int)
				v.serviceAddr = s.name + "." + ns + ".svc:" + strconv.Itoa(port)
			}
		}
	}
	for _, d := range objsOfKind(all, "Deployment") {
		if !strings.Contains(d.source, "/charts/api-gateway/") {
			continue
		}
		for _, c := range nlist(nPodSpec(d)["containers"]) {
			cm, _ := c.(map[string]any)
			for _, e := range nlist(cm["env"]) {
				m, _ := e.(map[string]any)
				switch nstr(m["name"]) {
				case edgeNotifyProbeAddrEnv:
					v.edgeAddr = nstr(m["value"])
				case edgeNotifyProbeMTLSEnv:
					v.edgeMTLS = nstr(m["value"])
				}
			}
			v.edgeSeen = true
		}
	}
	return v
}

// judgeNotifyProbeRoute — решение держателя: находки «предмет — почему».
func judgeNotifyProbeRoute(v notifyProbeRouteView) []string {
	var out []string
	if !v.edgeSeen {
		return out
	}
	switch {
	case v.probeDeployments > 0 && v.edgeAddr == "":
		out = append(out, edgeNotifyProbeAddrEnv+": (а) проба notify-probe в рендере, а край адреса пробы не знает — InternalNotifyProbeService/Send недостижим через внутренний блок края")
	case v.probeDeployments == 0 && v.edgeAddr != "":
		out = append(out, edgeNotifyProbeAddrEnv+": (а) край зовёт пробу ("+v.edgeAddr+"), а пробы в рендере нет")
	case v.probeDeployments > 0 && v.edgeAddr != v.serviceAddr:
		out = append(out, edgeNotifyProbeAddrEnv+": (б) адрес у края "+v.edgeAddr+" ≠ служба пробы "+v.serviceAddr)
	}
	if v.edgeAddr != "" && v.edgeMTLS != "true" {
		out = append(out, edgeNotifyProbeMTLSEnv+": (в) адрес пробы у края без ребра mTLS — слушатели пробы принимают только mTLS")
	}
	return out
}

func notifyProbeRouteChainViews(t *testing.T, c umbrellaCopy) map[string]notifyProbeRouteView {
	t.Helper()
	stacks := deployStacksForRender(t, renderGateOperatorSample)
	out := map[string]notifyProbeRouteView{}
	for n, chain := range stacks {
		rendered, err := renderStandProfile(t, c, chain)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: рендер цепочки %s отказал: %v\n%s", n, err, lastLines(rendered, 5))
		}
		out[n] = notifyProbeRouteViewOf(t, rendered, "kacho")
	}
	return out
}

// TestEdgeRouteToNotifyProbeMeetsTheProbe — держатель на дереве как оно есть.
func TestEdgeRouteToNotifyProbeMeetsTheProbe(t *testing.T) {
	views := notifyProbeRouteChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{}))
	names := make([]string, 0, len(views))
	withProbe, withEdge := 0, 0
	for n, v := range views {
		names = append(names, n)
		if v.probeDeployments > 0 {
			withProbe++
		}
		if v.edgeSeen {
			withEdge++
		}
	}
	sort.Strings(names)
	t.Logf("цепочек %d · с краем %d · с пробой notify-probe %d", len(views), withEdge, withProbe)
	if withProbe == 0 || withEdge == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: цепочек с пробой %d, с краем %d — судить нечего", withProbe, withEdge)
	}
	for _, n := range names {
		v := views[n]
		f := judgeNotifyProbeRoute(v)
		t.Logf("  %s: проба %d, служба %q, адрес у края %q, mTLS %q, находок %d",
			n, v.probeDeployments, v.serviceAddr, v.edgeAddr, v.edgeMTLS, len(f))
		for _, x := range f {
			t.Errorf("КРАСНЫЙ: %s: %s", n, x)
		}
	}
}

// TestEdgeRouteToNotifyProbeInjections — держатель краснеет на настоящем входе
// дерева, и находка называет ровно внесённый дефект.
func TestEdgeRouteToNotifyProbeInjections(t *testing.T) {
	expect := func(name, prefix string, views map[string]notifyProbeRouteView, chains ...string) {
		t.Helper()
		for _, chain := range chains {
			v, ok := views[chain]
			if !ok {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: цепочки %s в таблице нет", chain)
			}
			hit := false
			for _, f := range judgeNotifyProbeRoute(v) {
				if strings.HasPrefix(f, prefix) {
					hit = true
					t.Logf("инъекция «%s» → %s: %s", name, chain, f)
				}
			}
			if !hit {
				t.Errorf("инъекция «%s»: держатель промолчал о %s в цепочке %s", name, prefix, chain)
			}
		}
	}

	// Близнец — дерево как есть: находок нет.
	for chain, v := range notifyProbeRouteChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{})) {
		if f := judgeNotifyProbeRoute(v); len(f) != 0 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнец %s красный — инъекции недействительны: %v", chain, f)
		}
	}

	const devAddr = `    notifyProbeInternal: kacho-notify-probe.kacho.svc:9091`

	// Проба поднята, адреса у края нет.
	views := notifyProbeRouteChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{umbrellaEdits: map[string]func(string) string{
		"values.dev.yaml": replaceOnce(devAddr, `    notifyProbeInternal: ""`),
	}}))
	expect("адрес пробы у края снят", edgeNotifyProbeAddrEnv+": (а)", views, "dev", "dev-prod")

	// Край зовёт пробу там, где её нет (снято снятие адреса в prorobotech).
	views = notifyProbeRouteChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{umbrellaEdits: map[string]func(string) string{
		"values.prorobotech.yaml": replaceOnce(`    notifyProbeInternal: ""`, devAddr),
	}}))
	expect("адрес пробы без пробы", edgeNotifyProbeAddrEnv+": (а)", views, "prorobotech")

	// Адрес у края расходится со службой пробы.
	views = notifyProbeRouteChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{umbrellaEdits: map[string]func(string) string{
		"values.dev.yaml": replaceOnce(devAddr, `    notifyProbeInternal: kacho-notify-probe.kacho.svc:9090`),
	}}))
	expect("адрес у края не тот", edgeNotifyProbeAddrEnv+": (б)", views, "dev")

	// Ребро mTLS к пробе выключено при заданном адресе — чарт края отказывает
	// в рендере; отказ называет ручку, а не молчит.
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{umbrellaEdits: map[string]func(string) string{
		"values.dev.yaml": replaceOnce(`      notifyProbe: true `, `      notifyProbe: false `),
	}})
	out, err := renderStandProfile(t, c, deployStacksForRender(t, renderGateOperatorSample)["dev"])
	switch {
	case err == nil:
		t.Errorf("инъекция «ребро mTLS к пробе выключено»: рендер прошёл, отказа нет")
	case !strings.Contains(out, "mtls.edges.notifyProbe"):
		t.Errorf("инъекция «ребро mTLS к пробе выключено»: отказ без имени ручки:\n%s", lastLines(out, 3))
	default:
		t.Logf("инъекция «ребро mTLS к пробе выключено» → отказ рендера: %s", lineWith(out, "mtls.edges.notifyProbe"))
	}
}
