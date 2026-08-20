// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package metrics — Prometheus observability adapter kacho-geo.
//
// Живёт на adapter-границе (Clean Architecture): prometheus-клиент импортируется
// ТОЛЬКО здесь и в composition root (cmd/kacho-geo) — никогда в domain/ или
// service-слое. Метрики снимаются с отдельного cluster-internal diagnostic-порта
// (НЕ на public/internal gRPC-поверхности — internal-cardinality не tenant-facing).
//
// Адаптер реализует corelib operations.Recorder, но ВЫСТАВЛЯЕТ только те ряды,
// которые kacho-geo способен сдвинуть: восстановление осиротевших операций
// (orphans, reconcile runs/errors). Три ряда исполнителя длительных операций
// (ретраи и провалы терминальной записи, счётчик исполняемых worker'ов) движет
// ТОЛЬКО corelib-worker, а geo в него ничего не диспетчеризует — каталог это
// конфиг-INSERT, операция завершается синхронно (shared/syncop), вызовов
// operations.Run в сервисе нет. Выставленный вечный ноль читался бы дежурным как
// «отказов нет», хотя правда — «не измеряется», поэтому соответствующие методы
// Recorder'а остаются no-op без ряда (интерфейс требует их присутствия — его
// принимает operations.WithReconcilerRecorder). geo — leaf-сервис без
// register-outbox, поэтому outbox.Recorder здесь не нужен. Реестр — ПРИВАТНЫЙ
// (prometheus.NewRegistry, не global default): тесты герметичны, нет
// duplicate-register panic при рестартах composition root в одном процессе.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/PRO-Robotech/kacho/pkg/authz/authzmetrics"
	opmetrics "github.com/PRO-Robotech/kacho/pkg/operations"
)

// Metrics владеет приватным prometheus-реестром и коллекторами kacho-geo.
// Создаётся один раз в composition root и шарится diagnostic HTTP-listener'ом и
// LRO-reconciler'ом.
type Metrics struct {
	reg *prometheus.Registry

	// operations (durability LRO — только восстановление осиротевших)
	orphans         *prometheus.CounterVec
	reconcileRuns   prometheus.Counter
	reconcileErrors prometheus.Counter
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

	reg.MustRegister(
		m.orphans, m.reconcileRuns, m.reconcileErrors,
		buildInfo,
	)
	return m
}

// Handler возвращает promhttp-handler приватного реестра. Монтируется ТОЛЬКО на
// выделенном cluster-internal diagnostic-listener'е.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// ---- operations.Recorder ----

// Три метода ниже принадлежат исполнителю длительных операций, которого kacho-geo
// не использует (мутации каталога завершаются синхронно — shared/syncop, ни одного
// operations.Run в сервисе). Интерфейс Recorder требует их присутствия, но ряда за
// ними нет: вечный ноль в scrape дежурный прочитал бы как «отказов нет», хотя
// правда — «не измеряется». Появится в geo асинхронный путь — ряды заводятся
// вместе с ним, а не заранее.

// IncTerminalWriteRetries — no-op: терминальную запись в geo делает не worker.
func (m *Metrics) IncTerminalWriteRetries(string) {}

// IncTerminalWriteFailures — no-op: терминальную запись в geo делает не worker.
func (m *Metrics) IncTerminalWriteFailures(string) {}

// SetInflight — no-op: в geo нет исполняемых worker'ов, которых можно считать.
func (m *Metrics) SetInflight(float64) {}

// IncOrphansRecovered инкрементит разрешённые reconciler'ом orphan'ы по outcome.
func (m *Metrics) IncOrphansRecovered(outcome string) { m.orphans.WithLabelValues(outcome).Inc() }

// IncReconcileRuns инкрементит прогоны sweep-цикла reconciler'а.
func (m *Metrics) IncReconcileRuns() { m.reconcileRuns.Inc() }

// IncReconcileErrors инкрементит ошибки sweep-цикла reconciler'а.
func (m *Metrics) IncReconcileErrors() { m.reconcileErrors.Inc() }

// Compile-time: адаптер удовлетворяет corelib operations.Recorder-порту.
var _ opmetrics.Recorder = (*Metrics)(nil)

// RegisterAuthzCache провязывает читателей величин КЕША ПОЛОЖИТЕЛЬНЫХ ВЕРДИКТОВ.
//
// Коллектор — ОДНА реализация на все сервисы (`pkg/authz/authzmetrics`) и одно
// правило имени серии, однородное с краем
// (`kacho_api_gateway_authz_cache_total`): собиратель, у которого уже есть
// правило на край, читает сервисы тем же выражением.
//
// Полосы объявляет ВЫЗЫВАЮЩИЙ: кешей вердиктов в процессе бывает больше одного,
// и полоса, которой в этом процессе нет, не рисуется нулями — иначе экспозиция
// утверждала бы существование окна, которого нет.
//
// Решения звена — величина ПРОЦЕССА, а не полосы: звено у сервиса одно, и оба
// слушателя проходят через него.
func (m *Metrics) RegisterAuthzCache(lanes map[string]authzmetrics.Reader,
	decisions authzmetrics.DecisionReader) {
	m.reg.MustRegister(authzmetrics.New("geo", lanes, decisions))
}
