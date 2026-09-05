// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moduleKnowsNoEdgeTree раскладывает синтетическое МНОГОМОДУЛЬНОЕ дерево.
// Многомодульное намеренно: единственный модуль в синтетике не отличил бы
// «свойство проверяется у всех» от «свойство проверяется у одного», а именно это
// расширение и заводится.
func moduleKnowsNoEdgeTree(t *testing.T, files map[string]string) ModuleKnowsNoEdgeOptions {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}
	return ModuleKnowsNoEdgeOptions{
		Root:        root,
		ServicesDir: "services",
		ChartDirTemplates: []string{
			"services/%s/deploy",
			"deploy/helm/umbrella/charts/kacho-%s",
		},
	}
}

func auditModuleTree(t *testing.T, opts ModuleKnowsNoEdgeOptions) []ModuleKnowsNoEdgeFinding {
	t.Helper()
	findings, census, err := AuditModuleKnowsNoEdge(opts, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if census.GoFiles == 0 && census.ChartFiles == 0 {
		t.Fatal("обход пуст — инъекция ничего не доказывает")
	}
	return findings
}

// findingsOf отбирает находки одного модуля: инъекция обязана ронять ТОЛЬКО
// проверяемое, и проверить это можно лишь атрибуцией.
func findingsOf(findings []ModuleKnowsNoEdgeFinding, module string) []ModuleKnowsNoEdgeFinding {
	var out []ModuleKnowsNoEdgeFinding
	for _, f := range findings {
		if f.Module == module {
			out = append(out, f)
		}
	}
	return out
}

// ── законные близнецы, взятые с НАСТОЯЩЕГО дерева ────────────────────────────
//
// Каждый близнец воспроизводит форму, которая в дереве живёт и обязана жить.
// Без них гейт ловил бы форму, а не существо, и первый же ложный срабат его
// отключил бы.

// legalModuleGoTwin — проза о снятом, круг законных отправителей и личность
// вызывающего в литералах, ключ САМОГО края, чужой контракт соседа и импорт
// каталога ресурса Gateway домена vpc.
const legalModuleGoTwin = `package svc

import (
	geo "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	vpcgw "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/gateway"
)

// Здесь когда-то жил импорт pkg/api/kacho/cloud/apigateway/v1 и ручка
// KACHO_VPC_GATEWAY_INTERNAL_ADDR. Проза о снятом — не ребро.

// gatewayServiceName — короткое имя службы края: решение «кого я впускаю»,
// а не адрес исходящего вызова.
const gatewayServiceName = "api-gateway"

// edgeOwnKey — ключ САМОГО края, названный в объяснении. Край держит адреса
// модулей законно.
const edgeOwnKey = "KACHO_API_GATEWAY_VPC_GRPC"

const trustedSAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"

var (
	_ = geo.NewRegionServiceClient
	_ = vpcgw.Create
	_ = gatewayServiceName
	_ = edgeOwnKey
	_ = trustedSAN
)
`

// legalModuleChartTwin — селектор пода края (направление край→модуль), круг
// законных отправителей и проза о снятой ручке.
const legalModuleChartTwin = `# ручка KACHO_VPC_GATEWAY_INTERNAL_ADDR снята задачей #1024
networkPolicy:
  # Public gRPC :9090 — only api-gateway may reach it.
  apiGatewayPodSelector:
    matchLabels:
      app: kacho-api-gateway
authz:
  trustedForwarderSANs: "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"
`

// cleanTwoModuleTree — чистое дерево из двух модулей: iam (предмет прежнего
// анализатора) и vpc (модуль, которого он не судил).
func cleanTwoModuleTree() map[string]string {
	return map[string]string{
		"services/iam/legal.go":           strings.ReplaceAll(legalModuleGoTwin, "KACHO_VPC_", "KANAME_"),
		"services/iam/deploy/values.yaml": strings.ReplaceAll(legalModuleChartTwin, "KACHO_VPC_", "KANAME_"),
		"services/vpc/legal.go":           legalModuleGoTwin,
		"services/vpc/deploy/values.yaml": legalModuleChartTwin,
	}
}

// ── ПРОГОН 1 — КОНТРОЛЬ: всё цело, молчат обе полосы ─────────────────────────

// TestModuleKnowsNoEdgeControlBothLanesSilent — на чистом двухмодульном дереве
// молчат и новая полоса (vpc), и прежняя (iam). Без этого прогона молчание
// существующего контроля было бы неотличимо от молчания мёртвого.
func TestModuleKnowsNoEdgeControlBothLanesSilent(t *testing.T) {
	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, cleanTwoModuleTree()))
	if len(findings) != 0 {
		t.Fatalf("законные близнецы объявлены находками: %v", findings)
	}
}

// ── ПРОГОН 2 — ИНЪЕКЦИЯ НОВОГО: дефект в модуле вне прежней популяции ────────

// TestModuleKnowsNoEdgeInjectionOutsideIamRedsOnlyThatModule — дефект в vpc
// находится (прежний анализатор был к нему слеп), а полоса iam при этом молчит.
func TestModuleKnowsNoEdgeInjectionOutsideIamRedsOnlyThatModule(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/vpc/wiring.go"] = "package svc\n\nconst envEdge = \"KACHO_VPC_GATEWAY_INTERNAL_ADDR\"\n"
	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, files))

	vpc := findingsOf(findings, "vpc")
	if len(vpc) != 1 {
		t.Fatalf("ожидалась ровно одна находка у vpc, получено %d: %v", len(vpc), findings)
	}
	if !strings.Contains(vpc[0].Path, "wiring.go") || vpc[0].Line != 3 {
		t.Fatalf("находка не называет координату внесённой ручки: %s", vpc[0])
	}
	if iam := findingsOf(findings, "iam"); len(iam) != 0 {
		t.Fatalf("инъекция в vpc уронила чужую полосу iam: %v", iam)
	}
}

// ── ПРОГОН 3 — ИНЪЕКЦИЯ СТАРОГО: прежний предмет не потерян при расширении ───

// TestModuleKnowsNoEdgeInjectionInsideIamRedsOnlyThatModule — тот же дефект в
// iam по-прежнему находится, а полоса vpc молчит. Перепись, сошедшаяся с
// прежней, о сохранности способности падать не говорит ничего — говорит этот
// прогон.
func TestModuleKnowsNoEdgeInjectionInsideIamRedsOnlyThatModule(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/iam/wiring.go"] = "package svc\n\nconst envEdge = \"KANAME_GATEWAY_INTERNAL_ADDR\"\n"
	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, files))

	iam := findingsOf(findings, "iam")
	if len(iam) != 1 {
		t.Fatalf("ожидалась ровно одна находка у iam, получено %d: %v", len(iam), findings)
	}
	if !strings.Contains(iam[0].What, "ручка адреса края") {
		t.Fatalf("прежний предмет утрачен при расширении: %s", iam[0])
	}
	if vpc := findingsOf(findings, "vpc"); len(vpc) != 0 {
		t.Fatalf("инъекция в iam уронила чужую полосу vpc: %v", vpc)
	}
}

// ── ФОРМЫ ЗАПИСИ ПРЕДМЕТА — по инъекции на каждую ────────────────────────────

// TestModuleKnowsNoEdgeFallsOnTheContractImport — форма 1: тип края в импорте.
func TestModuleKnowsNoEdgeFallsOnTheContractImport(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/nlb/push.go"] = `package svc

import edge "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/apigateway/v1"

var _ = edge.NewInternalAuthzCacheServiceClient
`
	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, files))
	nlb := findingsOf(findings, "nlb")
	if len(nlb) != 1 {
		t.Fatalf("ожидалась ровно одна находка у nlb, получено %d: %v", len(nlb), findings)
	}
	if !strings.Contains(nlb[0].Path, "push.go") || nlb[0].Line != 3 ||
		!strings.Contains(nlb[0].What, "импорт контракта края") {
		t.Fatalf("находка не называет координату внесённого импорта: %s", nlb[0])
	}
}

// TestModuleKnowsNoEdgeFallsOnTheEdgeCodeImport — форма 2: КОД края в импорте.
// Судится отдельно от контракта: модуль, собранный вместе с краем, не поднимется
// там, где края нет, даже не назвав ни одной ручки.
func TestModuleKnowsNoEdgeFallsOnTheEdgeCodeImport(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/storage/reach.go"] = `package svc

import mux "github.com/PRO-Robotech/kacho/gateway/internal/restmux"

var _ = mux.New
`
	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, files))
	st := findingsOf(findings, "storage")
	if len(st) != 1 || !strings.Contains(st[0].What, "импорт кода края") {
		t.Fatalf("импорт кода края не найден отдельно от контракта: %v", findings)
	}
}

// TestModuleKnowsNoEdgeFallsOnTheChartKnob — форма 3: ручка в чарте. Снятый код
// при живой посадке оставляет отказ старта.
func TestModuleKnowsNoEdgeFallsOnTheChartKnob(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/compute/deploy/deployment.yaml"] = "env:\n" +
		"  # исторически здесь стояла KACHO_COMPUTE_GATEWAY_INTERNAL_ADDR\n" +
		"  - name: KACHO_COMPUTE_GATEWAY_INTERNAL_ADDR\n" +
		"    value: edge:9091\n"
	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, files))
	cp := findingsOf(findings, "compute")
	if len(cp) != 1 {
		t.Fatalf("ожидалась ровно одна находка (объявление), получено %d: %v", len(cp), findings)
	}
	if cp[0].Line != 3 {
		t.Fatalf("находкой названа строка %d — комментарий не отделён от объявления: %s",
			cp[0].Line, cp[0])
	}
}

// TestModuleKnowsNoEdgeJudgesTheSecondChartHome — форма 4: у модуля два
// возможных дома чарта, и второй (зонтичный) обязан судиться так же. Иначе
// расширение перечня шаблонов было бы холостым.
func TestModuleKnowsNoEdgeJudgesTheSecondChartHome(t *testing.T) {
	files := cleanTwoModuleTree()
	files["deploy/helm/umbrella/charts/kacho-geo/values.yaml"] =
		"gatewayInternalAddr: KACHO_GEO_GATEWAY_INTERNAL_ADDR\n"
	files["services/geo/legal.go"] = strings.ReplaceAll(legalModuleGoTwin, "KACHO_VPC_", "KACHO_GEO_")
	findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, files))
	geo := findingsOf(findings, "geo")
	if len(geo) != 1 || !strings.Contains(geo[0].Path, "umbrella") {
		t.Fatalf("зонтичный дом чарта не осмотрен: %v", findings)
	}
}

// TestModuleKnowsNoEdgeStaysSilentOnTestFiles — проба, импортирующая контракт
// края, находкой не является: судится ПРОД-код.
func TestModuleKnowsNoEdgeStaysSilentOnTestFiles(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/vpc/probe_test.go"] = `package svc

import _ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/apigateway/v1"
`
	if findings := auditModuleTree(t, moduleKnowsNoEdgeTree(t, files)); len(findings) != 0 {
		t.Fatalf("проба объявлена находкой: %v", findings)
	}
}

// ── ПРЕДПОСЫЛКА И ОБЪЁМ ──────────────────────────────────────────────────────

// TestModuleKnowsNoEdgeRefusesWhenItsPremiseBreaks — приставка модуля, накрывшая
// ключи самого края, делает анализатор ложным: край держит адреса модулей
// законно, и каждая проза о нём стала бы находкой. Отказ громче молчания.
func TestModuleKnowsNoEdgeRefusesWhenItsPremiseBreaks(t *testing.T) {
	opts := moduleKnowsNoEdgeTree(t, map[string]string{
		"services/api/legal.go": "package svc\n",
	})
	_, _, err := AuditModuleKnowsNoEdge(opts, nil)
	if err == nil {
		t.Fatal("модуль `api` выводит приставку ключей самого края — анализатор обязан отказать")
	}
	if !strings.Contains(err.Error(), "предпосылка") {
		t.Fatalf("отказ не называет нарушенную предпосылку: %v", err)
	}
}

// TestModuleKnowsNoEdgeRefusesOnAnEmptyWalk — ноль модулей это «ноль
// прочитанного», а не «ноль находок».
func TestModuleKnowsNoEdgeRefusesOnAnEmptyWalk(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "services"), 0o750); err != nil {
		t.Fatal(err)
	}
	_, _, err := AuditModuleKnowsNoEdge(ModuleKnowsNoEdgeOptions{
		Root: root, ServicesDir: "services",
	}, nil)
	if err == nil {
		t.Fatal("пустой обход обязан быть отказом, а не молчаливым нулём находок")
	}
}

// TestModuleKnowsNoEdgeDerivesTheModuleListFromTheTree — перечень выводится, а не
// выписан: модуль, заведённый в дереве, попадает в перепись сам.
func TestModuleKnowsNoEdgeDerivesTheModuleListFromTheTree(t *testing.T) {
	files := cleanTwoModuleTree()
	files["services/brandnew/legal.go"] = "package svc\n"
	_, census, err := AuditModuleKnowsNoEdge(moduleKnowsNoEdgeTree(t, files), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if strings.Join(census.Modules, ",") != "brandnew,iam,vpc" {
		t.Fatalf("перечень модулей не выведен из дерева: %v", census.Modules)
	}
}

// TestModuleKnowsNoEdgeKnobPrefixIsDerived — приставка ручки выводится из имени
// модуля. Проба зовёт ТУ ЖЕ функцию, что анализатор: вторая копия предиката
// разошлась бы с первой молча.
func TestModuleKnowsNoEdgeKnobPrefixIsDerived(t *testing.T) {
	for module, want := range map[string]string{
		"iam": "KANAME_GATEWAY",
		"vpc": "KACHO_VPC_GATEWAY",
		"nlb": "KACHO_NLB_GATEWAY",
	} {
		if got := EdgeAddressKnobPrefixFor(module); got != want {
			t.Fatalf("приставка модуля %s: получено %q, ожидалось %q", module, got, want)
		}
	}
}
