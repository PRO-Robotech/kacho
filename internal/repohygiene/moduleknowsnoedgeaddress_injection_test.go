// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Доказательство способности половины АДРЕСА падать и молчать (задача #1576).
//
// Прогонов ТРИ, а не два, и третий обязателен: инъекция нового свойства должна
// ронять ТОЛЬКО новую полосу, а инъекция существующего — только существующую.
// Без третьего молчание существующего контроля неотличимо от молчания мёртвого.
//
// Инъекция роняет ТОЛЬКО проверяемое: она не заводит лишнего модуля и не
// добавляет элемента, нарушающего всё сразу, — она снимает ОДНО свойство у
// элемента, чьи остальные свойства целы (`testing.md` §«Гейт на класс», п. 2в).

// lanesOf — полосы находок, по одной записи на полосу, в порядке появления.
func lanesOf(findings []ModuleKnowsNoEdgeFinding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range findings {
		if !seen[f.Lane] {
			seen[f.Lane] = true
			out = append(out, f.Lane)
		}
	}
	return out
}

func findingsInLane(findings []ModuleKnowsNoEdgeFinding, lane string) []ModuleKnowsNoEdgeFinding {
	var out []ModuleKnowsNoEdgeFinding
	for _, f := range findings {
		if f.Lane == lane {
			out = append(out, f)
		}
	}
	return out
}

// requireOnlyLane — находки есть, и ВСЕ они одной полосы. Обе половины
// утверждения несущие: «есть» доказывает, что гейт способен падать, «все одной»
// — что инъекция не задела соседа.
func requireOnlyLane(t *testing.T, findings []ModuleKnowsNoEdgeFinding, lane, module string) {
	t.Helper()
	if len(findings) == 0 {
		t.Fatalf("инъекция не найдена — полоса %q не способна падать", lane)
	}
	if got := lanesOf(findings); len(got) != 1 || got[0] != lane {
		t.Fatalf("инъекция уронила полосы %v, а проверяется только %q: красное пришло от соседа, "+
			"и о проверяемой полосе прогон не говорит ничего", got, lane)
	}
	for _, f := range findings {
		if f.Module != module {
			t.Fatalf("находка приписана модулю %q вместо %q: %s", f.Module, module, f)
		}
	}
	for _, f := range findings {
		t.Logf("НАХОДКА [%s] %s", f.Lane, f)
	}
}

// ── ПРОГОН 1 — КОНТРОЛЬ: всё цело, молчат ВСЕ ТРИ полосы ─────────────────────

// TestEdgeAddressControlAllThreeLanesSilent — на чистом дереве существующих
// законных близнецов молчит и новая полоса. Прогон доказывает, что расширение
// распознавателя не покраснело на том, что в дереве живёт и обязано жить.
func TestEdgeAddressControlAllThreeLanesSilent(t *testing.T) {
	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, cleanTwoModuleTree()))
	if len(findings) != 0 {
		t.Fatalf("законные близнецы объявлены находками: %v", findings)
	}
	t.Log("КОНТРОЛЬ: находок 0 — молчат все три полосы (тип · ручка · адрес)")
}

// ── ПРОГОН 2 — ИНЪЕКЦИЯ НОВОГО СВОЙСТВА ──────────────────────────────────────

// TestEdgeAddressInjectionRawDialInGoRedsOnlyTheAddressLane — (A) нетипизированный
// вызов края по сырому адресу. Контракта края он не требует, конвенции имён ручек
// не следует, и до этой правки анализатор молчал.
func TestEdgeAddressInjectionRawDialInGoRedsOnlyTheAddressLane(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/vpc/edgecall.go"] = "package svc\n\n" +
		"import \"net/http\"\n\n" +
		"func Ping() { _, _ = http.Get(\"http://api-gateway.kacho.svc:8080/vpc/v1/networks\") }\n"

	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, files))
	requireOnlyLane(t, findings, EdgeLaneAddress, "vpc")
	if !strings.Contains(findings[0].What, "http://api-gateway.kacho.svc:8080") {
		t.Fatalf("находка не называет цель дозвона: %s — читателю пришлось бы искать её глазами", findings[0].What)
	}
	if findings[0].Path != "services/vpc/edgecall.go" || findings[0].Line != 5 {
		t.Fatalf("находка не называет координату: %s:%d", findings[0].Path, findings[0].Line)
	}
}

// TestEdgeAddressInjectionChartWaitsForEdgeRedsOnlyTheAddressLane — (B) посадка
// ЖДЁТ край. Это дословно «модуль не поднимется там, где края нет вовсе» — вторая
// половина предмета из шапки гейта, к которой он был слеп.
func TestEdgeAddressInjectionChartWaitsForEdgeRedsOnlyTheAddressLane(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/vpc/deploy/deployment.yaml"] = "spec:\n  template:\n    spec:\n" +
		"      initContainers:\n        - name: wait-for-edge\n          image: busybox\n" +
		"          command: [\"sh\",\"-c\",\"until nc -z api-gateway.kacho.svc 8080; do sleep 1; done\"]\n"

	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, files))
	requireOnlyLane(t, findings, EdgeLaneAddress, "vpc")
	if !strings.Contains(findings[0].What, "api-gateway.kacho.svc 8080") {
		t.Fatalf("находка не называет цель ожидания: %s", findings[0].What)
	}
}

// ── ПРОГОН 3 — ИНЪЕКЦИЯ СУЩЕСТВУЮЩЕГО: краснеет только СОСЕД ─────────────────

// TestEdgeAddressInjectionOfTheTypeLaneLeavesTheAddressLaneSilent — снятие
// существующего свойства (импорт контракта края) обязано ронять полосу ТИПА и
// только её. Без этого прогона молчание существующей полосы после моей правки
// было бы неотличимо от того, что я её сломал.
func TestEdgeAddressInjectionOfTheTypeLaneLeavesTheAddressLaneSilent(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/vpc/typed.go"] = "package svc\n\n" +
		"import edge \"github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/apigateway/v1\"\n\n" +
		"var _ = edge.NewInternalAuthzCacheServiceClient\n"

	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, files))
	requireOnlyLane(t, findings, EdgeLaneType, "vpc")
}

// TestEdgeAddressInjectionOfTheKnobLaneLeavesTheAddressLaneSilent — то же для
// второй существующей полосы: ручка адреса края по конвенции имён.
func TestEdgeAddressInjectionOfTheKnobLaneLeavesTheAddressLaneSilent(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/vpc/wiring.go"] = "package svc\n\nconst envEdge = \"KACHO_VPC_GATEWAY_INTERNAL_ADDR\"\n"

	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, files))
	requireOnlyLane(t, findings, EdgeLaneKnob, "vpc")
}

// ── ЗАКОННЫЕ БЛИЗНЕЦЫ: гейт обязан МОЛЧАТЬ ───────────────────────────────────
//
// Каждый близнец — форма, которая в дереве живёт и обязана жить. Гейт, краснеющий
// хоть на одном, снимут в первый же день, и вместе с ним уйдёт всё остальное.

// twinSilent — общая проверка: близнец подан, находок ноль, и обход НЕ ПУСТ.
// Последнее несущее: молчание на пустом обходе доказывало бы не различение, а то,
// что читать было нечего.
func twinSilent(t *testing.T, name string, files map[string]string) {
	t.Helper()
	opts := moduleKnowsNoEdgeTree(t, files)
	findings, census, err := AuditModuleKnowsNoEdge(opts, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if census.EdgeNamedGo+census.EdgeNamedChart == 0 {
		t.Fatalf("близнец %q: край не назван ни разу — половина АДРЕСА его не осматривала, "+
			"и молчание ничего не доказывает", name)
	}
	if len(findings) != 0 {
		t.Fatalf("близнец %q объявлен находкой: %v", name, findings)
	}
	t.Logf("БЛИЗНЕЦ %q: находок 0 · край назван (литералов Go %d, строк чарта %d) — осмотрен и признан законным",
		name, census.EdgeNamedGo, census.EdgeNamedChart)
}

// TestEdgeAddressStaysSilentOnTheVpcGatewayResource — БЛИЗНЕЦ 1: домен vpc владеет
// ресурсом Gateway. Общее у него с краем — только слово; токен хоста `api-gateway`
// в нём не встречается, поэтому близнец закрыт by construction, а не перечнем.
func TestEdgeAddressStaysSilentOnTheVpcGatewayResource(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/vpc/gateway_resource.go"] = "package svc\n\n" +
		"import vpcgw \"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/gateway\"\n\n" +
		"// GatewayService домена vpc, таблица vpc_gateway, ресурс Gateway.\n" +
		"const table = \"vpc_gateway\"\n" +
		"const svcName = \"GatewayService\"\n" +
		"const restPath = \"/vpc/v1/gateways\"\n" +
		"const dial = \"grpc://kacho-vpc.kacho.svc:9090\"\n\n" +
		"var _, _, _, _, _ = vpcgw.Create, table, svcName, restPath, dial\n"
	twinSilent(t, "ресурс Gateway домена vpc", files)
}

// TestEdgeAddressStaysSilentOnTheTrustedForwarderCircle — БЛИЗНЕЦ 2: круг законных
// отправителей. Правило безопасности требует его НЕПУСТЫМ и пиненным по фактическим
// отправителям — то есть модуль ОБЯЗАН знать край по имени. Схема `spiffe` означает
// личность и не дозванивается никуда.
func TestEdgeAddressStaysSilentOnTheTrustedForwarderCircle(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/vpc/trust.go"] = "package svc\n\n" +
		"const san = \"spiffe://kacho.cloud/ns/kacho-system/sa/kacho-api-gateway\"\n" +
		"const caller = \"api-gateway\"\n" +
		"const refusal = \"api-gateway did not forward x-kacho-principal-* headers\"\n\n" +
		"var _, _, _ = san, caller, refusal\n"
	files["services/vpc/deploy/values.yaml"] = "networkPolicy:\n" +
		"  apiGatewayPodSelector:\n    matchLabels:\n      app: kacho-api-gateway\n" +
		"authz:\n  trustedForwarderSANs: \"spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway\"\n" +
		"image: kacho-api-gateway:dev\n"
	twinSilent(t, "круг законных отправителей и селектор пода края", files)
}

// TestEdgeAddressStaysSilentOnTheTokenIssuerAddress — БЛИЗНЕЦ 3: адрес издателя
// токена. Это идентификатор издателя OIDC и realm в заголовке отказа, который
// модуль ОБЪЯВЛЯЕТ докер-клиентам, — строка-идентификатор, а не исходящий вызов.
func TestEdgeAddressStaysSilentOnTheTokenIssuerAddress(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/iam/issuer.go"] = "package svc\n\n" +
		"// Издатель объявляется внешним адресом платформы; края в нём нет, и\n" +
		"// дозвона по нему модуль не делает — его предъявляют клиенту.\n" +
		"const issuer = \"https://api.kacho.local/iam/token\"\n" +
		"const realm = \"Bearer realm=\\\"https://api.kacho.local/iam/token\\\"\"\n\n" +
		"var _, _ = issuer, realm\n"
	twinSilent(t, "адрес издателя токена", files)
}

// TestEdgeAddressKnowsEveryFormItClaims — распознаватель обязан знать ВСЕ формы,
// которые объявляет, и НЕ признавать те, что объявлены законными. Проба судит
// распознаватель НАПРЯМУЮ: она о нём, а не о дереве, и потому перечисляет обе
// стороны по каждой оси.
func TestEdgeAddressKnowsEveryFormItClaims(t *testing.T) {
	red := []struct{ in, want string }{
		{"http://api-gateway.kacho.svc:8080/x", "http://api-gateway.kacho.svc:8080"},
		{"https://kacho-api-gateway:443", "https://kacho-api-gateway:443"},
		{"grpc://api-gateway:9091", "grpc://api-gateway:9091"},
		{"api-gateway.kacho.svc:8080", "api-gateway.kacho.svc:8080"},
	}
	for _, c := range red {
		got := edgeDialTargets(c.in, false)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("цель дозвона %q не распознана: %v (ждали %q)", c.in, got, c.want)
		}
	}

	green := []string{
		"api-gateway", // бесхозное имя
		"spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway", // личность, не адрес
		"kacho-api-gateway:dev",                              // тег образа, не порт
		"app: kacho-api-gateway",                             // селектор пода
		"KACHO_API_GATEWAY_VPC_GRPC",                         // ключ самого края
		"api-gateway did not forward x-kacho-principal-*",    // проза отказа
		"vpc_gateway", "GatewayService", "/vpc/v1/gateways", // ресурс домена vpc
		"api-gateway 8080", // порт через пробел ВНЕ чарта
	}
	for _, in := range green {
		if got := edgeDialTargets(in, false); len(got) != 0 {
			t.Errorf("законная форма %q объявлена целью дозвона: %v", in, got)
		}
	}

	// Порт через пробел — цель ТОЛЬКО в чарте: там это оболочечная проба.
	if got := edgeDialTargets("until nc -z api-gateway.kacho.svc 8080; do", true); len(got) != 1 {
		t.Errorf("оболочечная проба в чарте не распознана: %v", got)
	}
	t.Logf("распознаватель: дозвонных форм %d · законных близнецов %d — обе стороны утверждены",
		len(red)+1, len(green))
}
