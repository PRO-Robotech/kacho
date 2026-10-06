// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// network_policy_admission_render_test.go — гейт «политика сети пропускает тех,
// кто к службе звонит, и называет только поды рендера» на КАЖДОЙ цепочке таблицы
// stacks.txt, и инъекция в настоящий рендер (kacho#2941).
//
// Предмет, обе половины и границы — в шапке network_policy_admission_test.go;
// оси судьи на синтетике — network_policy_admission_injection_test.go. Здесь
// только сбор входа и то, что синтетика доказать не может: что судья, получив
// НАСТОЯЩИЙ рендер, краснеет ровно на внесённом дефекте и молчит без него.
package deploy_test

import (
	"os"
	"strings"
	"testing"
)

// npNamespace — пространство имён, в которое рецепт подъёма ставит умбреллу;
// тот же `-n`, что у renderChainOutcome.
const npNamespace = "kacho"

// npChainDocs — документы рендера цепочки с ручками UMBRELLA_OPTS, то есть
// ровно тем вызовом helm, которым стенд поднимается.
func npChainDocs(t *testing.T, name string) []map[string]any {
	t.Helper()
	return npChainDocsWith(t, name)
}

// npChainDocsWith — то же, с ручками поверх профилей цепочки (фикстура, у
// которой то, что профиль стенда сегодня выключает, включено ручками чарта).
func npChainDocsWith(t *testing.T, name string, extra ...string) []map[string]any {
	t.Helper()
	requireUmbrellaPackagedFromTree(t)
	chain, ok := deployStacks(t)[name]
	if !ok {
		t.Fatalf("стека %q в таблице %s нет — предпосылка пробы исчезла, а не рендер стал чистым", name, stacksTable)
	}
	raw, err := os.ReadFile(standMakefile)
	if err != nil {
		t.Fatalf("рецепты подъёма %s не читаются: %v", standMakefile, err)
	}
	sets, err := umbrellaSets(string(raw))
	if err != nil {
		t.Fatalf("ручки применения умбреллы не прочитаны: %v — рендер был бы не тем стендом", err)
	}
	return renderDocBodies(t, "цепочки "+name, renderChainCached(t, chain, append(sets, extra...)...))
}

// TestEveryStackNetworkPolicyAdmitsItsDialersAndNamesOnlyRenderedPods — сам гейт.
func TestEveryStackNetworkPolicyAdmitsItsDialersAndNamesOnlyRenderedPods(t *testing.T) {
	stacks := deployStacks(t)
	var policies, isolated, dials, judged int
	for _, name := range sortedStackNames(stacks) {
		v, err := judgeNetworkPolicies(npNamespace, npChainDocs(t, name))
		if err != nil {
			t.Fatalf("стек %s: судья отказал — %v", name, err)
		}
		policies += v.policies
		isolated += v.isolated
		dials += v.dials
		judged += v.selectorsJudged
		t.Logf("  %-11s (%s): рабочих объектов %d · политик %d · селекторов осмотрено %d · "+
			"отправителей вне модели %d · звонков %d, из них к закрытым подам %d · "+
			"к себе %d · к неопубликованному порту %d · находок %d",
			name, strings.Join(stacks[name], " + "), v.workloads, v.policies, v.selectorsJudged,
			v.unmodeledPeers, v.dials, v.isolated, v.selfDials, v.unexposedPorts, len(v.findings))
		for _, f := range v.findings {
			t.Errorf("стек %s: %s", name, f)
		}
	}
	if policies == 0 {
		t.Fatalf("ни на одной из %d цепочек нет ни одной политики сети — судить нечего, это не «чисто»",
			len(stacks))
	}
	if isolated == 0 {
		t.Fatalf("звонков %d, но ни один не пришёл к поду под политикой — половина достижимости "+
			"не осмотрела ничего, и её зелёное было бы объявлено о непрочитанном", dials)
	}
	t.Logf("перепись: цепочек %d (таблица %s) · политик %d · селекторов %d · звонков %d, к закрытым %d",
		len(stacks), stacksTable, policies, judged, dials, isolated)
}

// ─────────────────────────────────────────────────────────────────────────────
// Инъекции в настоящий рендер. Цепочка — стенд `own`: на ней дефект наблюдался
// (край не доставал до службы балансировщиков), и политики на ней включены.

const npInjectStack = "own"

func npDeepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = npDeepCopy(e)
		}
		return m
	case []any:
		s := make([]any, len(x))
		for i, e := range x {
			s[i] = npDeepCopy(e)
		}
		return s
	}
	return v
}

func npCopyDocs(docs []map[string]any) []map[string]any {
	out := make([]map[string]any, len(docs))
	for i, d := range docs {
		out[i], _ = npDeepCopy(d).(map[string]any)
	}
	return out
}

// npPeerSelectors — селекторы отправителей во всех правилах входа документа
// политики, отображениями, которые можно править на месте.
func npPeerSelectors(d map[string]any) []map[string]any {
	spec, _ := d["spec"].(map[string]any)
	rules, _ := spec["ingress"].([]any)
	var out []map[string]any
	for _, r := range rules {
		rm, _ := r.(map[string]any)
		peers, _ := rm["from"].([]any)
		for _, p := range peers {
			pm, _ := p.(map[string]any)
			if ps, ok := pm["podSelector"].(map[string]any); ok {
				out = append(out, ps)
			}
		}
	}
	return out
}

func npPodLabels(t *testing.T, docs []map[string]any, kind, name string) map[string]string {
	t.Helper()
	for _, d := range docs {
		md, _ := d["metadata"].(map[string]any)
		if d["kind"] == kind && md["name"] == name {
			tpl, _ := podTemplateOf(d)
			tm, _ := tpl["metadata"].(map[string]any)
			return stringMap(tm["labels"])
		}
	}
	t.Fatalf("в рендере %s нет %s/%s — предпосылка инъекции исчезла", npInjectStack, kind, name)
	return nil
}

func npSameLabels(sel map[string]any, labels map[string]string) bool {
	ml := stringMap(sel["matchLabels"])
	if len(ml) == 0 || len(ml) != len(labels) {
		return false
	}
	for k, v := range ml {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// npTwinGreen — предпосылка инъекций: настоящий рендер без вмешательства чист.
// Иначе «краснеет ровно внесённое» неотличимо от «краснело и до».
func npTwinGreen(t *testing.T, docs []map[string]any) {
	t.Helper()
	v, err := judgeNetworkPolicies(npNamespace, docs)
	if err != nil {
		t.Fatalf("близнец: судья отказал — %v", err)
	}
	if len(v.findings) != 0 {
		t.Fatalf("законный близнец (рендер %s без вмешательства) красный — инъекции нечего доказывать:\n%s",
			npInjectStack, strings.Join(v.findings, "\n"))
	}
}

// TestNetworkPolicyAdmissionRenderInjection_EdgeLabelTheEdgeDoesNotCarry —
// возвращён прежний дефект: каждое правило, впускающее край, ждёт метку
// `app: kacho-api-gateway`, которой под края не несёт. Краснеют обе половины, и
// каждая находка называет край.
func TestNetworkPolicyAdmissionRenderInjection_EdgeLabelTheEdgeDoesNotCarry(t *testing.T) {
	docs := npChainDocs(t, npInjectStack)
	npTwinGreen(t, docs)

	edge := npPodLabels(t, docs, "Deployment", "api-gateway")
	injected := npCopyDocs(docs)
	replaced := 0
	for _, d := range injected {
		if d["kind"] != "NetworkPolicy" {
			continue
		}
		for _, ps := range npPeerSelectors(d) {
			if npSameLabels(ps, edge) {
				ps["matchLabels"] = map[string]any{"app": "kacho-api-gateway"}
				replaced++
			}
		}
	}
	if replaced == 0 {
		t.Fatalf("ни одно правило рендера %s не впускает край по его меткам %v — предпосылка инъекции исчезла",
			npInjectStack, edge)
	}
	v, err := judgeNetworkPolicies(npNamespace, injected)
	if err != nil {
		t.Fatalf("судья отказал на инъекции: %v", err)
	}
	var dead, unreachable int
	for _, f := range v.findings {
		switch {
		case strings.HasPrefix(f, "(А)") && strings.Contains(f, "{app=kacho-api-gateway}"):
			dead++
		case strings.HasPrefix(f, "(Б) Deployment/api-gateway звонит"):
			unreachable++
		default:
			t.Errorf("находка не о внесённом дефекте: %s", f)
		}
	}
	for _, want := range []string{"kacho-nlb:9090", "kacho-nlb-internal:9091", "vpc:9091"} {
		found := false
		for _, f := range v.findings {
			if strings.Contains(f, "звонит "+want+" ") {
				found = true
			}
		}
		if !found {
			t.Errorf("недостижимость края до %s не найдена:\n%s", want, strings.Join(v.findings, "\n"))
		}
	}
	if dead != replaced {
		t.Errorf("мёртвых селекторов найдено %d, внесено %d", dead, replaced)
	}
	t.Logf("инъекция: правил края переписано %d · находок (А) %d · (Б) %d", replaced, dead, unreachable)
}

// TestNetworkPolicyAdmissionRenderInjection_NeighbourDroppedFromTheInternalRule —
// из правила внутреннего слушателя сетей убран сосед, который туда звонит.
// Половина (А) молчит — оставшиеся селекторы выбирают поды; краснеет только (Б),
// и ровно одним звонком.
func TestNetworkPolicyAdmissionRenderInjection_NeighbourDroppedFromTheInternalRule(t *testing.T) {
	docs := npChainDocs(t, npInjectStack)
	npTwinGreen(t, docs)

	compute := npPodLabels(t, docs, "Deployment", "compute")
	injected := npCopyDocs(docs)
	dropped := 0
	for _, d := range injected {
		md, _ := d["metadata"].(map[string]any)
		if d["kind"] != "NetworkPolicy" || md["name"] != "vpc-internal-allowlist" {
			continue
		}
		spec, _ := d["spec"].(map[string]any)
		rules, _ := spec["ingress"].([]any)
		for _, r := range rules {
			rm, _ := r.(map[string]any)
			peers, _ := rm["from"].([]any)
			kept := peers[:0]
			for _, p := range peers {
				pm, _ := p.(map[string]any)
				if ps, ok := pm["podSelector"].(map[string]any); ok && npSameLabels(ps, compute) {
					dropped++
					continue
				}
				kept = append(kept, p)
			}
			rm["from"] = kept
		}
	}
	if dropped != 1 {
		t.Fatalf("сосед compute назван правилами vpc-internal-allowlist %d раз, ждали 1 — предпосылка инъекции", dropped)
	}
	v, err := judgeNetworkPolicies(npNamespace, injected)
	if err != nil {
		t.Fatalf("судья отказал на инъекции: %v", err)
	}
	if len(v.findings) != 1 || !strings.HasPrefix(v.findings[0], "(Б) Deployment/compute звонит vpc:9091 ") {
		t.Fatalf("ждали ровно одну находку «compute звонит vpc:9091», есть %d:\n%s",
			len(v.findings), strings.Join(v.findings, "\n"))
	}
}

// TestNetworkPolicyAdmissionRenderInjection_EdgePodLabelSpoiled — порча ОДНОГО
// факта на стороне пода: метка шаблона пода края в рендере переписана, политики
// не тронуты. Каждая находка называет политику, которая край больше не впускает,
// и обе политики, впускающие край, названы.
func TestNetworkPolicyAdmissionRenderInjection_EdgePodLabelSpoiled(t *testing.T) {
	docs := npChainDocs(t, npInjectStack)
	npTwinGreen(t, docs)

	injected := npCopyDocs(docs)
	spoiled := 0
	for _, d := range injected {
		md, _ := d["metadata"].(map[string]any)
		if d["kind"] != "Deployment" || md["name"] != "api-gateway" {
			continue
		}
		tpl, _ := podTemplateOf(d)
		tm, _ := tpl["metadata"].(map[string]any)
		labels, _ := tm["labels"].(map[string]any)
		if _, ok := labels["app"]; ok {
			labels["app"] = "kacho-api-gateway"
			spoiled++
		}
	}
	if spoiled != 1 {
		t.Fatalf("метка `app` пода края переписана %d раз, ждали 1 — предпосылка инъекции", spoiled)
	}
	v, err := judgeNetworkPolicies(npNamespace, injected)
	if err != nil {
		t.Fatalf("судья отказал на инъекции: %v", err)
	}
	named := map[string]int{}
	for _, f := range v.findings {
		// Политика самого края (kacho#3028) выбирает под края той же меткой:
		// испорченная метка оставляет её без цели — это тот же внесённый дефект.
		hit := strings.HasPrefix(f, "(А) политика api-gateway: селектор цели")
		for _, p := range []string{"kacho-nlb", "vpc-internal-allowlist"} {
			if strings.Contains(f, "политика "+p+",") || strings.Contains(f, "политик ["+p+"]") {
				named[p]++
				hit = true
			}
		}
		if !hit {
			t.Errorf("находка не называет политику, переставшую впускать край: %s", f)
		}
	}
	for _, p := range []string{"kacho-nlb", "vpc-internal-allowlist"} {
		if named[p] == 0 {
			t.Errorf("политика %s, впускавшая край, не названа ни одной находкой:\n%s", p, strings.Join(v.findings, "\n"))
		}
	}
	t.Logf("инъекция: находок %d · по политикам %v", len(v.findings), named)
}
