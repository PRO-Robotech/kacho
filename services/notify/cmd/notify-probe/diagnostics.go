// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/authz/authzmetrics"
	"github.com/PRO-Robotech/corelib/listnarrow"
	"github.com/PRO-Robotech/corelib/listnarrow/narrowmetrics"
	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/observability/health"
	"github.com/PRO-Robotech/corelib/schemaguard"
	"github.com/PRO-Robotech/corelib/servicecontract"
)

// Штамп сборки — подставляется ldflags образа.
var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)

// metricsPrefix — сегмент имён серий пробы (kacho_<имя>_…): ASCII без дефиса.
const metricsPrefix = "notifyprobe"

// newRegistry — реестр величин процесса: рантайм Go, процесс, штамп сборки.
// Серии ленты (kacho_notifications_enabled, исходы, возраст старейшей строки)
// заводит фундамент в этом же реестре.
func newRegistry() *prometheus.Registry {
	version, commit := observability.NormalizeBuildStamp(buildVersion, buildCommit)
	reg := prometheus.NewRegistry()
	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "kacho_" + metricsPrefix + "_build_info",
		Help:        "Build metadata of the running notify-probe binary (constant 1).",
		ConstLabels: prometheus.Labels{"version": version, "commit": commit},
	})
	buildInfo.Set(1)
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		buildInfo,
	)
	return reg
}

// registerAuthzCollectors выводит величины окон положительных вердиктов и
// сужателя потока. Полос две: окно звена решения (вопрос на вызов) и окно
// сужателя (вопрос на строку журнала); слитые, они скрыли бы ту, что не
// попадает. Регистрируются при любом флаге: «сужений не было» обязано быть
// отличимо от «коллектора нет», поэтому без сужателя (доставка выключена)
// полосы сужателя отдаются нулями, а не исчезают.
func registerAuthzCollectors(reg prometheus.Registerer, cache *authzmetrics.Source, narrower *listnarrow.Narrower) {
	narrowCache := func() authz.CacheStats { return authz.CacheStats{} }
	var narrowCounts func() listnarrow.Counts
	if narrower != nil {
		narrowCache, narrowCounts = narrower.CacheStats, narrower.Counts
	}
	reg.MustRegister(authzmetrics.New(metricsPrefix, map[string]authzmetrics.Reader{
		authzmetrics.LaneRPC:    cache.Cache,
		authzmetrics.LaneNarrow: narrowCache,
	}, cache.Read))
	reg.MustRegister(narrowmetrics.New(metricsPrefix, narrowCounts))
}

// buildReadinessCheckers — готовность из именованных зависимостей: база и
// версия схемы (встроенный набор миграций против применённой версии).
func buildReadinessCheckers(pool *pgxpool.Pool, schemaCheck func(context.Context) error) []health.Checker {
	return []health.Checker{
		{Name: "database", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
		{Name: schemaguard.CheckerName, Check: schemaCheck},
	}
}

// describeDiagnosticSurface — профиль внутренней диагностической поверхности
// (/metrics, /healthz, /readyz).
func describeDiagnosticSurface(endpoint string, reg *prometheus.Registry, agg *health.Aggregator,
	mode servicecontract.Mode, logger *slog.Logger) (servicecontract.SurfaceDescriptor, error) {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.Handle("GET /healthz", agg.LiveHandler())
	mux.Handle("GET /readyz", agg.ReadyHandler())

	addr := servicecontract.Value(endpoint)
	if endpoint == "" {
		addr = servicecontract.NotApplicable[string](
			"KACHO_NOTIFYPROBE_METRICS_ADDR не задан профилем развёртывания: ни скрейпа, ни проб " +
				"живости и готовности на этой посадке нет")
	}
	return servicecontract.NewSurface(servicecontract.Surface{
		Service: serviceName,
		Name:    "диагностика (/metrics, /healthz, /readyz)",
		Mode:    mode,
		Logger:  logger,

		Addr:    addr,
		Handler: mux,

		Reach: servicecontract.ReachClusterInternal,
		Auth: servicecontract.NotApplicable[servicecontract.SurfaceAuthMech](
			"снята осознанно: поверхность выставлена только на внутренний Service и несёт " +
				"счётчики процесса — ни секретов, ни данных арендатора на проводе нет"),

		ReadHeaderBudget: diagReadHeaderBudget,
		RequestBudget:    servicecontract.Value(diagRequestBudget),
		IdleBudget:       diagIdleBudget,
		ShutdownBudget:   diagShutdownBudget,
	})
}

// Сроки диагностической поверхности — те же, что у соседних служб: заголовок
// запроса скрейпа и пробы короток, ответ /metrics — сотни серий.
const (
	diagReadHeaderBudget = 5 * time.Second
	diagRequestBudget    = 30 * time.Second
	diagIdleBudget       = 60 * time.Second
	diagShutdownBudget   = 5 * time.Second
)
