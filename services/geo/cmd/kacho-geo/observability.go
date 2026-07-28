// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Observability-проводка composition root: cluster-internal diagnostic
// HTTP-listener (/metrics). prometheus импортируется только в adapter-пакете
// internal/observability/metrics (Clean Architecture) — здесь лишь wiring. geo —
// leaf-сервис без register-outbox и без асинхронных операций, поэтому набор метрик
// ограничен восстановлением осиротевших LRO (reconciler).

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/PRO-Robotech/kacho/services/geo/internal/observability/metrics"
)

// build-info — инжектится через -ldflags "-X main.buildVersion=… -X main.buildCommit=…";
// дефолты для локальной сборки.
var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)

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
