// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// network_policy_admission_injection_test.go — судья политик сети краснеет на
// каждом классе, который объявляет, и молчит на законном близнеце (kacho#2941).
//
// Входы здесь синтетические и минимальные: судья проверяется на ОСЯХ, а не на
// дереве. Тот же судья на настоящем рендере каждой цепочки и инъекция в
// настоящий рендер — network_policy_admission_render_test.go (тег helmcharts).
package deploy_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func npDocs(t *testing.T, text string) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := yaml.NewDecoder(strings.NewReader(text))
	for {
		var d map[string]any
		if err := dec.Decode(&d); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("синтетический рендер не разбирается: %v", err)
		}
		if d != nil {
			out = append(out, d)
		}
	}
	return out
}

// npFixture — край звонит службе по публичному и внутреннему адресу; служба
// закрыта политикой. edgeLabel — метка, которую политика ждёт от края; под края
// несёт `app: api-gateway`.
func npFixture(edgeLabel, extraPolicySpec string) string {
	return `
apiVersion: apps/v1
kind: Deployment
metadata: {name: api-gateway, namespace: kacho}
spec:
  template:
    metadata: {labels: {app: api-gateway}}
    spec:
      containers:
        - name: gw
          image: docker.io/prorobotech/kacho-api-gateway:2798-87329e29
          env:
            - {name: KACHO_API_GATEWAY_NLB_GRPC, value: "kacho-nlb.kacho.svc:9090"}
            - {name: KACHO_API_GATEWAY_NLB_INTERNAL_GRPC, value: "kacho-nlb-internal.kacho.svc:9091"}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: kacho-nlb, namespace: kacho}
spec:
  template:
    metadata: {labels: {app: kacho-nlb}}
    spec:
      containers:
        - name: nlb
          image: docker.io/prorobotech/kacho-nlb:2798-87329e29
          ports:
            - {name: grpc, containerPort: 9090}
            - {name: igrpc, containerPort: 9091}
---
apiVersion: v1
kind: Service
metadata: {name: kacho-nlb, namespace: kacho}
spec:
  selector: {app: kacho-nlb}
  ports: [{port: 9090, targetPort: grpc}]
---
apiVersion: v1
kind: Service
metadata: {name: kacho-nlb-internal, namespace: kacho}
spec:
  selector: {app: kacho-nlb}
  ports: [{port: 9091, targetPort: igrpc}]
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: kacho-nlb, namespace: kacho}
spec:
  podSelector: {matchLabels: {app: kacho-nlb}}
` + extraPolicySpec + `
  ingress:
    - from: [{podSelector: {matchLabels: {app: ` + edgeLabel + `}}}]
      ports: [{protocol: TCP, port: 9090}, {protocol: TCP, port: 9091}]
`
}

func judgeText(t *testing.T, text string) npVerdict {
	t.Helper()
	v, err := judgeNetworkPolicies("kacho", npDocs(t, text))
	if err != nil {
		t.Fatalf("судья отказал на разборном входе: %v", err)
	}
	return v
}

// TestNetworkPolicyAdmissionInjection_EdgeLabelThePodDoesNotCarry — дефект,
// ради которого судья заведён: политика ждёт от края метку, которой под края не
// несёт. Краснеют обе половины, и находки называют край и оба порта.
func TestNetworkPolicyAdmissionInjection_EdgeLabelThePodDoesNotCarry(t *testing.T) {
	t.Parallel()
	red := judgeText(t, npFixture("kacho-api-gateway", "  policyTypes: [Ingress]"))
	want := []string{
		"(А) политика kacho-nlb, правило входа (отправитель) #0 (порты 9090,9091): селектор {app=kacho-api-gateway}",
		"(Б) Deployment/api-gateway звонит kacho-nlb-internal:9091 → под Deployment/kacho-nlb, порт 9091 (igrpc)",
		"(Б) Deployment/api-gateway звонит kacho-nlb:9090 → под Deployment/kacho-nlb, порт 9090 (grpc)",
	}
	if len(red.findings) != len(want) {
		t.Fatalf("находок %d, ждали %d:\n%s", len(red.findings), len(want), strings.Join(red.findings, "\n"))
	}
	for i, w := range want {
		if !strings.HasPrefix(red.findings[i], w) {
			t.Errorf("находка %d:\n  есть %s\n  ждали начало %s", i, red.findings[i], w)
		}
	}
	twin := judgeText(t, npFixture("api-gateway", "  policyTypes: [Ingress]"))
	if len(twin.findings) != 0 {
		t.Errorf("законный близнец (метка края верна) красный:\n%s", strings.Join(twin.findings, "\n"))
	}
	if twin.dials != 2 || twin.isolated != 2 {
		t.Errorf("близнец: звонков %d, к закрытым %d — ждали 2 и 2: перепись адресов не дошла до предмета",
			twin.dials, twin.isolated)
	}
}

// TestNetworkPolicyAdmissionInjection_SelectorOfTheWrongPodIsCaughtByReachability —
// селектор, выбирающий НЕ ТОТ под (метка самой службы вместо метки края): половина
// (А) зелёная — селектор выбирает под, — краснеет только половина (Б).
func TestNetworkPolicyAdmissionInjection_SelectorOfTheWrongPodIsCaughtByReachability(t *testing.T) {
	t.Parallel()
	v := judgeText(t, npFixture("kacho-nlb", ""))
	if len(v.findings) != 2 {
		t.Fatalf("ждали ровно две находки достижимости, есть %d:\n%s", len(v.findings), strings.Join(v.findings, "\n"))
	}
	for _, f := range v.findings {
		if !strings.HasPrefix(f, "(Б) Deployment/api-gateway") {
			t.Errorf("находка не о достижимости края: %s", f)
		}
	}
}

// TestNetworkPolicyAdmissionInjection_PortByNameAndDefaultTypes — порт правила,
// названный именем порта контейнера, пропускает; отсутствие policyTypes закрывает
// вход (умолчание API). Близнец с портом, которого служба не публикует, красный.
func TestNetworkPolicyAdmissionInjection_PortByNameAndDefaultTypes(t *testing.T) {
	t.Parallel()
	byName := strings.Replace(npFixture("api-gateway", ""),
		"ports: [{protocol: TCP, port: 9090}, {protocol: TCP, port: 9091}]",
		"ports: [{port: grpc}, {port: igrpc}]", 1)
	if v := judgeText(t, byName); len(v.findings) != 0 {
		t.Errorf("порт по имени не засчитан:\n%s", strings.Join(v.findings, "\n"))
	}
	onlyPublic := strings.Replace(npFixture("api-gateway", ""),
		"ports: [{protocol: TCP, port: 9090}, {protocol: TCP, port: 9091}]",
		"ports: [{port: grpc}]", 1)
	v := judgeText(t, onlyPublic)
	if len(v.findings) != 1 || !strings.Contains(v.findings[0], "kacho-nlb-internal:9091") {
		t.Errorf("закрытый внутренний порт не найден:\n%s", strings.Join(v.findings, "\n"))
	}
	udp := strings.Replace(npFixture("api-gateway", ""), "protocol: TCP, port: 9091", "protocol: UDP, port: 9091", 1)
	if v := judgeText(t, udp); len(v.findings) != 1 {
		t.Errorf("правило UDP засчитано за TCP-звонок:\n%s", strings.Join(v.findings, "\n"))
	}
}

// TestNetworkPolicyAdmissionInjection_EgressOfTheDialerIsJudged — отправитель,
// ограниченный по исходу, обязан выпускать к адресату; близнец выпускает.
func TestNetworkPolicyAdmissionInjection_EgressOfTheDialerIsJudged(t *testing.T) {
	t.Parallel()
	egress := func(to string) string {
		return npFixture("api-gateway", "") + `---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: edge-egress, namespace: kacho}
spec:
  podSelector: {matchLabels: {app: api-gateway}}
  policyTypes: [Egress]
  egress:
    - to: [{podSelector: {matchLabels: {app: ` + to + `}}}]
`
	}
	red := judgeText(t, egress("kacho-geo"))
	var out, dead int
	for _, f := range red.findings {
		if strings.Contains(f, "исход не выпускает") {
			out++
		}
		if strings.Contains(f, "{app=kacho-geo}") {
			dead++
		}
	}
	if out != 2 || dead != 1 {
		t.Errorf("исход: находок о выпуске %d (ждали 2), о мёртвом получателе %d (ждали 1):\n%s",
			out, dead, strings.Join(red.findings, "\n"))
	}
	if v := judgeText(t, egress("kacho-nlb")); len(v.findings) != 0 {
		t.Errorf("исход к адресату открыт, а судья красный:\n%s", strings.Join(v.findings, "\n"))
	}
}

// TestNetworkPolicyAdmissionInjection_UnmodeledPeerNeverAdmits — отправитель вне
// пространства имён рендером не моделируется: он СЧИТАЕТСЯ и не засчитывается ни
// как выбирающий, ни как пропускающий.
func TestNetworkPolicyAdmissionInjection_UnmodeledPeerNeverAdmits(t *testing.T) {
	t.Parallel()
	text := strings.Replace(npFixture("api-gateway", ""),
		"- from: [{podSelector: {matchLabels: {app: api-gateway}}}]",
		"- from: [{namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: kacho}}, podSelector: {matchLabels: {app: api-gateway}}}]", 1)
	v := judgeText(t, text)
	if v.unmodeledPeers != 1 {
		t.Errorf("отправителей вне модели посчитано %d, ждали 1", v.unmodeledPeers)
	}
	if len(v.findings) != 2 {
		t.Errorf("неисполнимое правило засчитано пропускающим:\n%s", strings.Join(v.findings, "\n"))
	}
}

// TestNetworkPolicyAdmissionJudge_EmptyRenderAndUnknownOperatorAreRefusals —
// пустой рендер и выражение, которого судья не исполняет, — отказ, а не «чисто».
func TestNetworkPolicyAdmissionJudge_EmptyRenderAndUnknownOperatorAreRefusals(t *testing.T) {
	t.Parallel()
	if _, err := judgeNetworkPolicies("kacho", nil); err == nil {
		t.Error("пустой рендер принят за чистый")
	}
	bad := strings.Replace(npFixture("api-gateway", ""),
		"podSelector: {matchLabels: {app: kacho-nlb}}",
		"podSelector: {matchExpressions: [{key: app, operator: Gt, values: [\"1\"]}]}", 1)
	if _, err := judgeNetworkPolicies("kacho", npDocs(t, bad)); err == nil {
		t.Error("оператор, которого судья не исполняет, проглочен молча")
	}
}

// TestDialTargets_KnowsEveryFormItClaims — распознаватель адреса судится
// напрямую, по обе стороны каждой оси.
func TestDialTargets_KnowsEveryFormItClaims(t *testing.T) {
	t.Parallel()
	red := map[string]npHostPort{
		"vpc.kacho.svc:9091":                         {"vpc", 9091},
		`addr: "vpc:9090"`:                           {"vpc", 9090},
		"tcp://kacho-nlb:9090":                       {"kacho-nlb", 9090},
		"dns:///vpc.kacho.svc.cluster.local:9091":    {"vpc", 9091},
		"postgres://u:p@pg-vpc.kacho:5432/kacho_vpc": {"pg-vpc", 5432},
		"--peer=kaname-internal.kacho.svc:9091.":     {"kaname-internal", 9091},
	}
	for in, want := range red {
		got := dialTargets(in, "kacho")
		if len(got) != 1 || got[0] != want {
			t.Errorf("адрес %q: %v, ждали %v", in, got, want)
		}
	}
	green := []string{
		"docker.io/prorobotech/kacho-nlb:2798-87329e29", // тег образа
		"kacho-nlb:dev",      // тег без цифр
		"vpc.other.svc:9091", // чужое пространство имён
		"KACHO_VPC:9091",     // ключ, а не хост
		"10.96.0.1:443",      // адрес, а не имя службы
		"registry/vpc:9091",  // путь образа
		"vpc:123456",         // не порт
	}
	for _, in := range green {
		if got := dialTargets(in, "kacho"); len(got) != 0 {
			t.Errorf("не-адрес %q распознан: %v", in, got)
		}
	}
}
