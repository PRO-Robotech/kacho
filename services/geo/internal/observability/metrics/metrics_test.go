// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	opmetrics "github.com/PRO-Robotech/kacho/pkg/operations"
)

// scrape собирает текст /metrics через приватный реестр адаптера.
func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics code=%d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestMetrics_ImplementsOperationsRecorder(t *testing.T) {
	var _ opmetrics.Recorder = New("v", "c")
}

// TestMetrics_TerminalWriteFailureVisibleInScrape локает НАБЛЮДАЕМОЕ поведение
// фикса: live-worker terminal-write exhaustion, ранее уходивший в NopRecorder
// (метрика «в никуда»), теперь виден в /metrics scrape с точным значением. Это и
// есть сигнал, который alerting должен ловить (async Region/Zone мутация не
// финализуется → forever-in-flight operation).
func TestMetrics_TerminalWriteFailureVisibleInScrape(t *testing.T) {
	m := New("test", "abc123")
	m.IncTerminalWriteRetries("MarkDone")
	m.IncTerminalWriteFailures("MarkError")
	m.IncOrphansRecovered("done")
	m.IncReconcileRuns()
	m.IncReconcileErrors()
	m.SetInflight(3)

	out := scrape(t, m)
	for _, want := range []string{
		"kacho_geo_operations_terminal_write_retries_total",
		"kacho_geo_operations_terminal_write_failures_total",
		"kacho_geo_operations_orphans_recovered_total",
		"kacho_geo_operations_reconcile_runs_total",
		"kacho_geo_operations_reconcile_errors_total",
		"kacho_geo_lro_workers_active",
		"kacho_geo_build_info",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/metrics missing series %q", want)
		}
	}
	if !strings.Contains(out, `kacho_geo_operations_terminal_write_failures_total{op="MarkError"} 1`) {
		t.Errorf("terminal_write_failures{MarkError} not 1; out:\n%s", out)
	}
	if !strings.Contains(out, `kacho_geo_lro_workers_active 3`) {
		t.Errorf("lro_workers_active not 3; out:\n%s", out)
	}
}
