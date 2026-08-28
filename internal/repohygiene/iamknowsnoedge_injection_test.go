// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// iamKnowsNoEdgeTree раскладывает синтетическое дерево: прод-код владельца и его
// чарт.
func iamKnowsNoEdgeTree(t *testing.T, files map[string]string) IamKnowsNoEdgeOptions {
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
	return IamKnowsNoEdgeOptions{Root: root, GoRoot: "svc", ChartRoots: []string{"chart"}}
}

func auditTree(t *testing.T, opts IamKnowsNoEdgeOptions) []IamKnowsNoEdgeFinding {
	t.Helper()
	findings, census, err := AuditIamKnowsNoEdge(opts, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if census.GoFiles == 0 && census.ChartFiles == 0 {
		t.Fatal("обход пуст — инъекция ничего не доказывает")
	}
	return findings
}

// законный близнец кода: имя контракта края стоит ПРОЗОЙ, а не импортом.
const legalGoTwin = `package svc

// Здесь когда-то жил импорт pkg/api/kacho/cloud/apigateway/v1 и ручка
// KACHO_IAM_GATEWAY_INTERNAL_ADDR. Проза о снятом — не ребро.
func Nothing() {}
`

// TestIamKnowsNoEdgeGateFallsOnTheImport — тип края, внесённый обратно, находится
// по координате.
func TestIamKnowsNoEdgeGateFallsOnTheImport(t *testing.T) {
	opts := iamKnowsNoEdgeTree(t, map[string]string{
		"svc/push.go": `package svc

import edge "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/apigateway/v1"

var _ = edge.NewInternalAuthzCacheServiceClient
`,
		"svc/legal.go":          legalGoTwin,
		"chart/deployment.yaml": "spec:\n  containers: []\n",
	})
	findings := auditTree(t, opts)
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Path, "push.go") || findings[0].Line != 3 {
		t.Fatalf("находка не называет координату внесённого импорта: %s", findings[0])
	}
}

// TestIamKnowsNoEdgeGateFallsOnTheAddressKnob — ручка адреса края, внесённая
// обратно в код, находится отдельно от импорта: половины судятся порознь.
func TestIamKnowsNoEdgeGateFallsOnTheAddressKnob(t *testing.T) {
	opts := iamKnowsNoEdgeTree(t, map[string]string{
		"svc/wiring.go": `package svc

const envEdge = "KACHO_IAM_GATEWAY_INTERNAL_ADDR"
`,
		"svc/legal.go":          legalGoTwin,
		"chart/deployment.yaml": "spec: {}\n",
	})
	findings := auditTree(t, opts)
	if len(findings) != 1 || !strings.Contains(findings[0].What, "ручка адреса края") {
		t.Fatalf("ручка в литерале не найдена отдельно от импорта: %v", findings)
	}
}

// TestIamKnowsNoEdgeGateFallsOnTheChartKnob — и в чарте тоже: снятый код при
// живой посадке оставляет отказ старта.
func TestIamKnowsNoEdgeGateFallsOnTheChartKnob(t *testing.T) {
	opts := iamKnowsNoEdgeTree(t, map[string]string{
		"svc/legal.go": legalGoTwin,
		"chart/deployment.yaml": "env:\n" +
			"  # исторически здесь стояла KACHO_IAM_GATEWAY_INTERNAL_ADDR\n" +
			"  - name: KACHO_IAM_GATEWAY_INTERNAL_ADDR\n" +
			"    value: edge:9091\n",
	})
	findings := auditTree(t, opts)
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка (объявление), получено %d: %v", len(findings), findings)
	}
	if findings[0].Line != 3 {
		t.Fatalf("находкой названа строка %d — комментарий не отделён от объявления: %s",
			findings[0].Line, findings[0])
	}
}

// TestIamKnowsNoEdgeGateStaysSilentOnLegalTwins — контроль в обратную сторону:
// проза о снятом и чужой контракт молчат. Без него гейт ловил бы форму, а не
// существо, и первый же ложный срабат его отключил бы.
func TestIamKnowsNoEdgeGateStaysSilentOnLegalTwins(t *testing.T) {
	opts := iamKnowsNoEdgeTree(t, map[string]string{
		"svc/legal.go": legalGoTwin,
		"svc/peer.go": `package svc

import geo "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

const envPeer = "KACHO_IAM_GEO_INTERNAL_ADDR"

var _ = geo.NewRegionServiceClient
`,
		"svc/probe_test.go": `package svc

import _ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/apigateway/v1"
`,
		"chart/values.yaml": "# ручка KACHO_IAM_GATEWAY_INTERNAL_ADDR снята задачей #1024\n" +
			"gatewayInternalAddr: \"\"\n",
	})
	if findings := auditTree(t, opts); len(findings) != 0 {
		t.Fatalf("законные близнецы объявлены находками: %v", findings)
	}
}
