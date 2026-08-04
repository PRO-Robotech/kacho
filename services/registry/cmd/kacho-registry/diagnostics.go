// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// diagnostics.go — cluster-internal diagnostic-листенер kacho-registry
// (/healthz, /metrics) и сканер состояния очереди регистраций.
//
// # Почему отдельный листенер
//
// Ни публичная gRPC-поверхность, ни data-plane для этого не годятся: первая
// tenant-facing, вторая принимает docker-клиентов снаружи, а внутренняя
// кардинальность (имена очередей, глубины, возраст головы) — инфра-информация,
// которой на этих поверхностях не место. Отдельный порт — тот же приём, что у
// остальных сервисов платформы.
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	outboxmetrics "github.com/PRO-Robotech/kacho/pkg/outbox/metrics"

	"github.com/PRO-Robotech/kacho/services/registry/internal/observability/metrics"
)

// outboxMetricsInterval — период скана очереди. Совпадает с остальными сервисами
// платформы: одна величина у всех очередей, чтобы порог тревоги, написанный по
// одной, читался одинаково на любой.
const outboxMetricsInterval = 15 * time.Second

// diagnosticMux — маршруты diagnostic-листенера. Вынесен отдельно, чтобы то, ЧТО
// обслуживается, проверялось без сети: расхождение между объявленным в чарте
// скрейпом и реально обслуживаемым путём иначе замечается только на живом
// Prometheus.
func diagnosticMux(m *metrics.Metrics) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET /metrics", m.Handler())
	return mux
}

// startDiagnosticListener поднимает diagnostic HTTP-listener. Пустой addr →
// (nil, no-op): выключен. net.Listen синхронный — ошибка привязки видна
// вызывающему сразу, а не «когда-нибудь в горутине».
func startDiagnosticListener(addr string, m *metrics.Metrics, logger *slog.Logger) (
	task func() error, shutdown func(context.Context), err error,
) {
	if addr == "" {
		logger.Warn("kacho-registry diagnostic listener disabled: очередь регистраций " +
			"останется без сканера состояния, застрявшая очередь будет молчать как пустая")
		return nil, func(context.Context) {}, nil
	}
	srv := &http.Server{Addr: addr, Handler: diagnosticMux(m), ReadHeaderTimeout: 5 * time.Second}
	lis, lerr := net.Listen("tcp", addr)
	if lerr != nil {
		return nil, nil, lerr
	}
	logger.Info("kacho-registry diagnostic listener", "endpoint", addr, "paths", "/healthz,/metrics")
	task = func() error {
		if serr := srv.Serve(lis); serr != nil && serr != http.ErrServerClosed {
			return serr
		}
		return nil
	}
	shutdown = func(sctx context.Context) { _ = srv.Shutdown(sctx) }
	return task, shutdown, nil
}

// runRegisterOutboxMetrics — периодический скан очереди регистраций: глубина,
// возраст самой старой недоставленной строки, число отравленных, и всё то же
// самое ПО НАПРАВЛЕНИЮ.
//
// Разложение здесь не украшение. Очередь несёт обе половины — постановку и
// снятие регистрации, — а реестры и репозитории создаются непрерывно, поэтому
// сводные величины остаются здоровыми при полностью мёртвом снятии: глубина
// мала, потому что выдачи дренятся; голова молода по той же причине. «Работает»
// и «ни разу не отозвано» дают одинаковую картину, и различает их только
// доставленное по направлению снятия.
//
// Скан не мутирует таблицу, не участвует в пути запроса и не может уронить под:
// ошибки логируются, цикл продолжается.
func runRegisterOutboxMetrics(ctx context.Context, pool *pgxpool.Pool, m *metrics.Metrics, logger *slog.Logger) {
	collector := outboxmetrics.NewCollector(pool, m, outboxmetrics.CollectorConfig{
		Table:      registerOutboxTable,
		Interval:   outboxMetricsInterval,
		Directions: outboxmetrics.RegisterOutboxDirections(),
	})
	collector.Run(ctx, func(err error) {
		logger.Warn("register outbox metrics scan failed", "table", registerOutboxTable, "err", err)
	})
}
