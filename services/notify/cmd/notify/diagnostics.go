// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/observability/health"
	"github.com/PRO-Robotech/corelib/servicecontract"
)

// Штамп сборки — подставляется `-ldflags -X` из тех же аргументов, что и
// клеймо образа (Dockerfile). Несобранный штампом бинарь отвечает
// `unstamped`, а не правдоподобным «dev».
var (
	buildVersion = ""
	buildCommit  = ""
)

// newRegistry — реестр метрик процесса: среда исполнения и ряд сборки.
// Метрики предмета notify регистрирует их владелец в этом же реестре.
func newRegistry() *prometheus.Registry {
	version, commit := observability.NormalizeBuildStamp(buildVersion, buildCommit)
	reg := prometheus.NewRegistry()
	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "kacho_notify_build_info",
		Help:        "Build metadata of the running kacho-notify binary (constant 1).",
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

// Сроки диагностической поверхности — те же, что у соседних служб дерева:
// поверхность отдаёт счётчики и пробы, тело запроса у неё пустое.
const (
	diagReadHeaderBudget = 5 * time.Second
	diagRequestBudget    = 30 * time.Second
	diagIdleBudget       = 60 * time.Second
	diagShutdownBudget   = 5 * time.Second
)

// describeDiagnosticSurface — единственная поверхность notify: `/healthz`,
// `/readyz`, `/metrics` на внутреннем адресе кластера (З15, NTF1-G19).
func describeDiagnosticSurface(addr string, reg *prometheus.Registry, agg *health.Aggregator,
	mode servicecontract.Mode, logger *slog.Logger) (servicecontract.SurfaceDescriptor, error) {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.Handle("GET /healthz", agg.LiveHandler())
	mux.Handle("GET /readyz", agg.ReadyHandler())

	return servicecontract.NewSurface(servicecontract.Surface{
		Service: serviceName,
		Name:    "диагностика (/metrics, /healthz, /readyz)",
		Mode:    mode,
		Logger:  logger,

		Addr:    servicecontract.Value(addr),
		Handler: mux,

		Reach: servicecontract.ReachClusterInternal,
		Auth: servicecontract.NotApplicable[servicecontract.SurfaceAuthMech](
			"снята осознанно: поверхность выставлена только на внутренний Service и несёт " +
				"счётчики процесса и пробы — ни писем, ни адресатов, ни секретов, ни сведений о " +
				"размещении на проводе нет (security.md §«Инфра-чувствительные данные»)"),

		ReadHeaderBudget: diagReadHeaderBudget,
		RequestBudget:    servicecontract.Value(diagRequestBudget),
		IdleBudget:       diagIdleBudget,
		ShutdownBudget:   diagShutdownBudget,
	})
}
