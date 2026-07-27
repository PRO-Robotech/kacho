// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// diagnostic_metrics_test.go — the scrape target the chart declares must be the one
// the process serves.
//
// The ServiceMonitor shipped with this service declares a scrape of `/metrics` on the
// `metrics` port. The process listened on that port and served `/healthz` only, so
// enabling the ServiceMonitor would have pointed Prometheus at a 404 — a monitoring
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

// TestServiceMonitorMatchesWhatTheProcessServes reads the shipped ServiceMonitor and
// checks its declared path against the mux. This is the assertion that was missing:
// the chart and the process were each internally consistent and disagreed with each
// other.
func TestServiceMonitorMatchesWhatTheProcessServes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "templates", "servicemonitor.yaml"))
	require.NoError(t, err, "the ServiceMonitor this test guards must exist")

	path := scrapePathOf(t, string(raw))
	rec := httptest.NewRecorder()
	diagnosticMux(metrics.New()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	assert.Equalf(t, http.StatusOK, rec.Code,
		"the ServiceMonitor scrapes %q; the process must serve it, or the declaration is monitoring nothing", path)
}

// scrapePathOf extracts the single `path:` value from the ServiceMonitor endpoint.
func scrapePathOf(t *testing.T, manifest string) string {
	t.Helper()
	for _, line := range strings.Split(manifest, "\n") {
		f := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(f, "path:"); ok {
			return strings.Trim(strings.TrimSpace(after), `"'`)
		}
	}
	t.Fatal("ServiceMonitor declares no scrape path — cannot check it against the process")
	return ""
}
