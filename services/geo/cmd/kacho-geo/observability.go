// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Observability-проводка composition root: LRO-worker durability-метрики +
// cluster-internal diagnostic HTTP-listener (/metrics). prometheus импортируется
// только в adapter-пакете internal/observability/metrics (Clean Architecture) —
// здесь лишь wiring. geo — leaf-сервис без register-outbox, поэтому набор метрик
// ограничен LRO-durability (worker terminal-write + reconciler).

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/geo/internal/observability/metrics"
)

// build-info — инжектится через -ldflags "-X main.buildVersion=… -X main.buildCommit=…";
// дефолты для локальной сборки.
var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)

// startLROWorker подключает Prometheus-Recorder и логгер к package-level
// default-registry LRO-worker'а (ConfigureDefault) и поднимает его dispatcher-loop
// (Start) ДО приёма трафика. Решает dead live-worker метрики: default-registry
// создаётся с NopRecorder, поэтому terminal-write retries/failures и inflight
// gauge от ЖИВОГО worker-пути (по которому идут async admin-мутации Region/Zone
// через operations.Run) не эмитились никуда — исчезал ровно тот сигнал, ради
// которого метрики существуют (terminal-write exhaustion оставляет durable
// done=false строки, клиент виснет в polling). WithRecorder подключает их к
// /metrics; явный Start делает Ready()=true до трафика.
//
// ConfigureDefault обязан предшествовать Start; вызывается один раз из composition
// root (повторный вызов после старта вернул бы ErrWorkerStarted).
func startLROWorker(rec operations.Recorder, logger *slog.Logger) error {
	if err := operations.ConfigureDefault(operations.WithRecorder(rec), operations.WithLogger(logger)); err != nil {
		return fmt.Errorf("configure LRO default-registry: %w", err)
	}
	operations.Start()
	return nil
}

// startDiagnosticListener поднимает cluster-internal HTTP-listener для метрик.
// Возвращает task (блокирующий Serve — вешается на фоновую goroutine) и
// shutdown-функцию. Отключён (пустой addr) → (nil, no-op): листенер не поднимается
// (back-compat). net.Listen выполняется синхронно, поэтому ошибка привязки порта
// видна вызывающему сразу (а не в фоне).
func startDiagnosticListener(addr string, m *metrics.Metrics, logger *slog.Logger) (task func() error, shutdown func(context.Context), err error) {
	if addr == "" {
		return nil, func(context.Context) {}, nil
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Handler())

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	lis, lerr := net.Listen("tcp", addr)
	if lerr != nil {
		return nil, nil, lerr
	}
	logger.Info("kacho-geo diagnostic listener", "endpoint", addr, "paths", "/metrics")

	task = func() error {
		if serr := srv.Serve(lis); serr != nil && serr != http.ErrServerClosed {
			return serr
		}
		return nil
	}
	shutdown = func(ctx context.Context) { _ = srv.Shutdown(ctx) }
	return task, shutdown, nil
}
