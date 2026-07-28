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

// TestLROWorkerStaysUndispatched — kacho-geo не поднимает исполнителя длительных
// операций и ничего в него не отправляет: мутации каталога завершаются синхронно
// (shared/syncop), поэтому package-level default-registry corelib остаётся
// незапущенным на всю жизнь процесса. Это и есть основание, по которому у сервиса
// нет рядов терминальной записи и счётчика исполняемых worker'ов — ряд, который
// невозможно сдвинуть, читается дежурным как «отказов нет».
func TestLROWorkerStaysUndispatched(t *testing.T) {
	if operations.Ready() {
		t.Fatal("kacho-geo must not start the corelib LRO dispatcher: it dispatches nothing to it")
	}
	if n := operations.Active(); n != 0 {
		t.Fatalf("operations.Active() = %d, want 0 — geo runs no async operations", n)
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
	m.IncOrphansRecovered("done")
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
