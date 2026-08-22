// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// diagnostic_metrics_test.go — the scrape target the chart declares must be the one
// the process serves.
//
// Объявление сбора этой службы называет путь `/metrics` на the
// `metrics` port. The process listened on that port and served `/healthz` only, so
// включённый сбор указал бы сборщику на 404 — то есть наблюдениеoring
// declaration with nothing behind it, which reads as "this service is observed" while
// observing nothing.
//
// The resolution is to serve the metrics, not to delete the declaration, because this
// service drains a register outbox and the platform requires its backlog to be
// visible: a queue that silently delivers nothing is a class this codebase has already
// been bitten by. So these tests assert the endpoint exists AND that the series behind
// it are real — an empty handler that returns 200 would be the same defect wearing a
// different hat.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/observability/metrics"
)

// outboxSeries are the series this service commits to exposing. They are the
// platform's outbox-delivery contract (backlog depth, age of the oldest undelivered
// row, poisoned rows) — the signals that make "this queue has delivered nothing"
// alertable instead of a line in a log nobody reads.
var outboxSeries = []string{
	"kacho_storage_outbox_backlog_depth",
	"kacho_storage_outbox_oldest_pending_age_seconds",
	"kacho_storage_outbox_poisoned_rows",
	"kacho_storage_outbox_poisoned_total",
}

// TestDiagnosticListenerServesMetricsAndHealth — the mux answers both paths.
func TestDiagnosticListenerServesMetricsAndHealth(t *testing.T) {
	mux := diagnosticMux(metrics.New())

	for _, path := range []string{"/metrics", "/healthz"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equalf(t, http.StatusOK, rec.Code, "diagnostic listener must serve %s", path)
	}
}

// TestMetricsEndpointExposesOutboxSeries — the endpoint carries the series it exists
// for. A 200 with an empty body would satisfy a scrape and tell an operator nothing.
func TestMetricsEndpointExposesOutboxSeries(t *testing.T) {
	m := metrics.New()
	// Record one observation per series, as the Collector and the drainer's poison
	// observer do at runtime — Prometheus omits a labelled series until it has a
	// value, so an assertion on a never-written registry would pass vacuously.
	const table = "kacho_storage.fga_register_outbox"
	m.SetBacklogDepth(table, 3)
	m.SetOldestPendingAgeSeconds(table, 42)
	m.SetPoisonedCount(table, 1)
	m.IncPoisoned(table)

	rec := httptest.NewRecorder()
	diagnosticMux(m).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	for _, series := range outboxSeries {
		assert.Containsf(t, body, series, "/metrics must expose %s", series)
	}
	assert.Contains(t, body, table, "the series must be labelled by outbox table, so a second queue can never conflate with this one")
	assert.Contains(t, body, "go_goroutines",
		"runtime collectors must be registered — they are what turns a leak into a graph")
}

// TestScrapeAnnotationMatchesWhatTheProcessServes читает ОБЪЯВЛЕНИЕ СБОРА и
// сверяет названный им путь с тем, что обслуживает мультиплексор. Это то самое
// утверждение, которого не хватало: чарт и процесс были внутренне
// согласованы каждый и расходились друг с другом.
//
// Прежде утверждение читало отдельный объект описи. Он снят задачей #955:
// объявлений сбора было два, а действует одно — аннотация на поде. Предмет
// утверждения от этого не изменился, изменился носитель, поэтому проба
// переориентирована, а не удалена: удалить её значило бы снять проверку
// вместе с механизмом, который она стерегла и который остался.
func TestScrapeAnnotationMatchesWhatTheProcessServes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "templates", "deployment.yaml"))
	require.NoError(t, err, "объявление, которое стережёт эта проба, обязано существовать")

	path := scrapePathOf(t, string(raw))
	rec := httptest.NewRecorder()
	diagnosticMux(metrics.New()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	assert.Equalf(t, http.StatusOK, rec.Code,
		"объявление сбора называет %q; процесс обязан его обслуживать, иначе объявление is monitoring nothing", path)
}

// scrapePathOf extracts the single `path:` value from the ServiceMonitor endpoint.
func scrapePathOf(t *testing.T, manifest string) string {
	t.Helper()
	for _, line := range strings.Split(manifest, "\n") {
		f := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(f, "prometheus.io/path:"); ok {
			return strings.Trim(strings.TrimSpace(after), `"'`)
		}
	}
	t.Fatal("объявление сбора не называет пути — сверять с процессом нечего")
	return ""
}
