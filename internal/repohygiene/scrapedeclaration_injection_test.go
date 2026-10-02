// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// TestScrapePortOfAKnobWithoutADefault — порт поверхности процесса, у ручки
// адреса которой НЕТ умолчания, берётся у значения чарта, которым чарт эту
// ручку задаёт (NTF-1, полоса D1; замысел З15, З20).
//
// Предмет. Ручка адреса диагностики notify (`KACHO_NOTIFY_DIAG_ADDR`) умолчания
// не несёт намеренно (`sec-no-silent-default-for-guarded-knob`): незаданная —
// отказ старта. Значит, порт, на котором поднимется поверхность, называет
// ТОЛЬКО чарт, задающий ручку; второго места, где его узнать, нет. Гейт сбора
// обязан брать порт оттуда, а не требовать умолчания в коде — иначе он
// требовал бы ровно то, что запрещает страж старта.
//
// Пары — по обе стороны каждой оси, настоящим входом формы чарта notify:
//
//	строка ручки   есть → порт значения; нет → находка с именем ручки;
//	значение       есть и целое → порт; нет → находка с путём; не целое → находка;
//	процесс        ручка без умолчания в конфигурации → узнана; с умолчанием → нет
//	               (её порт берёт прежняя ветвь, второй не заводится).
func TestScrapePortOfAKnobWithoutADefault(t *testing.T) {
	t.Parallel()

	const env = "KACHO_NOTIFY_DIAG_ADDR"
	cm := "data:\n  " + env + `: {{ printf ":%d" (int .Values.ports.diag) | quote }}` + "\n"
	values := map[string]any{"ports": map[string]any{"diag": 9095}}

	// Близнец: строка ручки читает значение чарта, значение — целое.
	port, finding := chartKnobPort(map[string]string{"templates/configmap.yaml": cm}, env, values)
	if finding != "" || port != 9095 {
		t.Fatalf("близнец: ожидался порт 9095 без находки, получено %d и %q", port, finding)
	}

	// Строки ручки в шаблонах нет — находка с именем ручки.
	_, finding = chartKnobPort(map[string]string{"templates/configmap.yaml": "data: {}\n"}, env, values)
	if !strings.Contains(finding, env) {
		t.Fatalf("строки ручки нет: ожидалась находка с именем %s, получено %q", env, finding)
	}

	// Строка есть, значения по её пути нет — находка с путём.
	_, finding = chartKnobPort(map[string]string{"templates/configmap.yaml": cm}, env, map[string]any{})
	if !strings.Contains(finding, "ports.diag") {
		t.Fatalf("значения нет: ожидалась находка с путём ports.diag, получено %q", finding)
	}

	// Значение не целое — находка: порт, которого нет, сверять не с чем.
	_, finding = chartKnobPort(map[string]string{"templates/configmap.yaml": cm}, env,
		map[string]any{"ports": map[string]any{"diag": ":9095"}})
	if !strings.Contains(finding, "ports.diag") {
		t.Fatalf("значение не целое: ожидалась находка с путём ports.diag, получено %q", finding)
	}

	// Узнавание ручки в конфигурации процесса — по обе стороны.
	noDefault := "type Config struct {\n\tDiagAddr string `envconfig:\"" + env + "\" knob:\"notify.diagAddr\"`\n}\n"
	if got := surfaceKnobWithoutDefault([]byte(noDefault)); got != env {
		t.Fatalf("ручка без умолчания не узнана: получено %q", got)
	}
	withDefault := "type Config struct {\n\tMetricsAddr string `envconfig:\"KACHO_VPC_METRICS_ADDR\" default:\":9095\"`\n}\n"
	if got := surfaceKnobWithoutDefault([]byte(withDefault)); got != "" {
		t.Fatalf("ручка с умолчанием узнана как ручка без умолчания: %q — её порт берёт ветвь умолчания", got)
	}
}
