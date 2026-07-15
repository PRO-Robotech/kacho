// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package metrics — Prometheus observability adapter kacho-geo.
//
// Живёт на adapter-границе (Clean Architecture): prometheus-клиент импортируется
// ТОЛЬКО здесь и в composition root (cmd/kacho-geo) — никогда в domain/ или
// service-слое. Метрики снимаются с отдельного cluster-internal diagnostic-порта
// (НЕ на public/internal gRPC-поверхности — internal-cardinality не tenant-facing).
//
// Адаптер реализует corelib operations.Recorder — durability-слой LRO worker'а и
// reconciler'а (terminal-write retries/failures, inflight, orphans, reconcile
// runs/errors). geo — leaf-сервис без register-outbox, поэтому outbox.Recorder
// здесь не нужен. Реестр — ПРИВАТНЫЙ (prometheus.NewRegistry, не global default):
// тесты герметичны, нет duplicate-register panic при рестартах composition root в
// одном процессе.
package metrics

import (
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	opmetrics "github.com/PRO-Robotech/kacho/pkg/operations"
)

// Metrics владеет приватным prometheus-реестром и коллекторами kacho-geo.
// Создаётся один раз в composition root и шарится diagnostic HTTP-listener'ом,
// LRO-worker'ом (через operations.WithRecorder) и LRO-reconciler'ом.
type Metrics struct {
	reg *prometheus.Registry

	// operations (durability LRO)
	terminalRetries  *prometheus.CounterVec
	terminalFailures *prometheus.CounterVec
	orphans          *prometheus.CounterVec
	reconcileRuns    prometheus.Counter
	reconcileErrors  prometheus.Counter
	inflight         atomic.Int64
}

// New конструирует адаптер, регистрирует Go + process runtime-коллекторы,
// build_info (const-метка сборки) и доменные коллекторы kacho-geo.
func New(version, commit string) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &Metrics{
		reg: reg,
		terminalRetries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kacho_geo_operations_terminal_write_retries_total",
			Help: "Retries of LRO terminal write (MarkDone/MarkError) on transient DB failure, by op.",
		}, []string{"op"}),
		terminalFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kacho_geo_operations_terminal_write_failures_total",
			Help: "LRO terminal writes that failed after exhausting retries (row stays done=false), by op.",
		}, []string{"op"}),
		orphans: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kacho_geo_operations_orphans_recovered_total",
			Help: "Orphaned LRO resolved by the reconciler, by terminal outcome (done|error).",
		}, []string{"outcome"}),
		reconcileRuns: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kacho_geo_operations_reconcile_runs_total",
			Help: "Reconciler sweep cycles executed.",
		}),
		reconcileErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kacho_geo_operations_reconcile_errors_total",
			Help: "Reconciler sweep cycles that hit an error.",
		}),
	}

	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "kacho_geo_build_info",
		Help:        "Build metadata of the running kacho-geo binary (constant 1).",
		ConstLabels: prometheus.Labels{"version": version, "commit": commit},
	})
	buildInfo.Set(1)

	// lro_workers_active — живой gauge числа исполняемых LRO worker'ов; значение
	// питается SetInflight (operations.Recorder), читается через GaugeFunc, чтобы
	// быть согласованным с operations.Active() без дубль-регистрации.
	lroActive := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "kacho_geo_lro_workers_active",
		Help: "In-flight LRO worker goroutines (operations.Active()).",
	}, func() float64 { return float64(m.inflight.Load()) })

	reg.MustRegister(
		m.terminalRetries, m.terminalFailures, m.orphans,
		m.reconcileRuns, m.reconcileErrors,
		buildInfo, lroActive,
	)
	return m
}

// Handler возвращает promhttp-handler приватного реестра. Монтируется ТОЛЬКО на
// выделенном cluster-internal diagnostic-listener'е.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// ---- operations.Recorder ----

// IncTerminalWriteRetries инкрементит ретраи терминальной записи по op-лейблу.
func (m *Metrics) IncTerminalWriteRetries(op string) { m.terminalRetries.WithLabelValues(op).Inc() }

// IncTerminalWriteFailures инкрементит невосстановимые терминальные записи.
func (m *Metrics) IncTerminalWriteFailures(op string) { m.terminalFailures.WithLabelValues(op).Inc() }

// SetInflight выставляет число исполняемых worker'ов (lro_workers_active gauge).
func (m *Metrics) SetInflight(n float64) { m.inflight.Store(int64(n)) }

// IncOrphansRecovered инкрементит разрешённые reconciler'ом orphan'ы по outcome.
func (m *Metrics) IncOrphansRecovered(outcome string) { m.orphans.WithLabelValues(outcome).Inc() }

// IncReconcileRuns инкрементит прогоны sweep-цикла reconciler'а.
func (m *Metrics) IncReconcileRuns() { m.reconcileRuns.Inc() }

// IncReconcileErrors инкрементит ошибки sweep-цикла reconciler'а.
func (m *Metrics) IncReconcileErrors() { m.reconcileErrors.Inc() }

// Compile-time: адаптер удовлетворяет corelib operations.Recorder-порту.
var _ opmetrics.Recorder = (*Metrics)(nil)
