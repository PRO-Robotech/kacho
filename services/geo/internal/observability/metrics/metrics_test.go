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

// TestMetrics_ReconcilerSeriesVisibleInScrape — то, что kacho-geo действительно
// умеет двигать, видно в scrape с точным значением. Восстановление осиротевшей
// операции реально: синхронно-завершённая операция пишется двумя стейтментами
// (Create → MarkDone), и падение процесса между ними оставляет durable done=false
// строку, которую разрешает reconciler.
func TestMetrics_ReconcilerSeriesVisibleInScrape(t *testing.T) {
	m := New("test", "abc123")
	m.IncOrphansRecovered("done")
	m.IncReconcileRuns()
	m.IncReconcileErrors()

	out := scrape(t, m)
	for _, want := range []string{
		"kacho_geo_operations_orphans_recovered_total",
		"kacho_geo_operations_reconcile_runs_total",
		"kacho_geo_operations_reconcile_errors_total",
		"kacho_geo_build_info",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/metrics missing series %q", want)
		}
	}
	if !strings.Contains(out, `kacho_geo_operations_orphans_recovered_total{outcome="done"} 1`) {
		t.Errorf("orphans_recovered{done} not 1; out:\n%s", out)
	}
}

// TestMetrics_NoSeriesThatGeoCannotMove — ряд, который сервис не в состоянии
// сдвинуть, выставлять нельзя: вечный ноль читается дежурным как «отказов нет»,
// хотя правда — «не измеряется». Три ряда исполнителя длительных операций
// (ретраи и провалы терминальной записи, счётчик исполняемых worker'ов) движет
// ТОЛЬКО corelib-worker, а kacho-geo в него ничего не диспетчеризует: каталог —
// конфиг-INSERT, операция завершается синхронно (см. shared/syncop), и ни одного
// вызова operations.Run в сервисе нет. Reconciler эти три метода не зовёт вовсе —
// он инкрементит только свои. Поэтому ряды обязаны отсутствовать в scrape, а не
// стоять на нуле.
func TestMetrics_NoSeriesThatGeoCannotMove(t *testing.T) {
	m := New("test", "abc123")
	// Даже если Recorder-методы позвать (их требует интерфейс corelib), рядов
	// быть не должно — иначе вечный ноль вернётся в scrape.
	m.IncTerminalWriteRetries("MarkDone")
	m.IncTerminalWriteFailures("MarkError")
	m.SetInflight(3)

	out := scrape(t, m)
	for _, unwanted := range []string{
		"kacho_geo_operations_terminal_write_retries_total",
		"kacho_geo_operations_terminal_write_failures_total",
		"kacho_geo_lro_workers_active",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("/metrics exposes %q, which no kacho-geo code path can ever move — a forever-zero series reads as 'no failures' when the truth is 'not measured'", unwanted)
		}
	}
}
