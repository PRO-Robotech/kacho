// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/PRO-Robotech/corelib/observability/health"
	"github.com/PRO-Robotech/corelib/servicecontract"
)

// Сроки диагностической поверхности — те же, что у notify-sender.
const (
	diagReadHeaderBudget = 5 * time.Second
	diagRequestBudget    = 30 * time.Second
	diagIdleBudget       = 60 * time.Second
	diagShutdownBudget   = 5 * time.Second
)

// describeDiagnosticSurface — объявление диагностической поверхности
// notify-api (/metrics, /healthz, /readyz); поднимает её носитель поверхностей.
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
				"счётчики процесса и пробы — ни извещений, ни адресатов, ни секретов на проводе нет " +
				"(security.md §«Инфра-чувствительные данные»)"),

		ReadHeaderBudget: diagReadHeaderBudget,
		RequestBudget:    servicecontract.Value(diagRequestBudget),
		IdleBudget:       diagIdleBudget,
		ShutdownBudget:   diagShutdownBudget,
	})
}
