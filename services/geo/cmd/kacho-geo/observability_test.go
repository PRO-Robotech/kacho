// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/geo/internal/observability/metrics"
)

// freeAddr резервирует свободный TCP-порт на loopback и отдаёт его адрес (listener
// закрыт — startDiagnosticListener переоткроет). Небольшое TOCTOU-окно приемлемо
// для unit-теста.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestStartLROWorker_ConfiguresDefaultAndStarts локает НАБЛЮДАЕМОЕ поведение
// фикса: composition root подключает Recorder+Logger к package-level
// default-registry (ConfigureDefault) и явно поднимает dispatcher-loop (Start),
// поэтому LRO-worker наблюдаем (Ready()=true до трафика) вместо ленивого старта на
// первом Run с NopRecorder. default-registry — package-global, поэтому этот тест
// стартует его один раз для всего пакета (последующий ConfigureDefault вернул бы
// ErrWorkerStarted — что и проверяем).
func TestStartLROWorker_ConfiguresDefaultAndStarts(t *testing.T) {
	rec := metrics.New("test", "abc")
	if err := startLROWorker(rec, discardLogger()); err != nil {
		t.Fatalf("startLROWorker: %v", err)
	}
	if !operations.Ready() {
		t.Fatal("operations.Ready() must be true after Start (dispatcher up before traffic)")
	}
	// Повторный ConfigureDefault после Start отвергается — доказывает, что
	// startLROWorker реально сконфигурировал default-registry ДО старта.
	if err := startLROWorker(rec, discardLogger()); err == nil {
		t.Fatal("second startLROWorker must fail (ErrWorkerStarted): default-registry already started")
	}
}

// TestStartDiagnosticListener_Disabled — пустой addr отключает listener (back-compat):
// task=nil, shutdown — no-op, без ошибки.
func TestStartDiagnosticListener_Disabled(t *testing.T) {
	task, shutdown, err := startDiagnosticListener("", metrics.New("v", "c"), discardLogger())
	if err != nil {
		t.Fatalf("disabled listener err: %v", err)
	}
	if task != nil {
		t.Fatal("disabled listener must yield nil task")
	}
	shutdown(context.Background()) // no-op, must not panic
}

// TestStartDiagnosticListener_ServesMetrics — непустой addr поднимает listener,
// который отдаёт /metrics (LRO-durability серии видны Prometheus scrape'у).
func TestStartDiagnosticListener_ServesMetrics(t *testing.T) {
	m := metrics.New("v", "c")
	m.IncTerminalWriteFailures("MarkError")
	addr := freeAddr(t)
	task, shutdown, err := startDiagnosticListener(addr, m, discardLogger())
	if err != nil {
		t.Fatalf("start listener: %v", err)
	}
	if task == nil {
		t.Fatal("enabled listener must yield a task")
	}
	// listener уже слушает (net.Listen отработал синхронно в startDiagnosticListener);
	// task обслуживает соединения. Дёргаем /metrics через фактический порт.
	go func() { _ = task() }()
	defer shutdown(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		r, e := http.Get("http://" + addr + "/metrics")
		if e == nil {
			resp = r
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resp == nil {
		t.Fatal("GET /metrics never succeeded")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics status=%d, want 200", resp.StatusCode)
	}
}
