// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package metrics — Prometheus observability adapter kacho-vpc.
//
// Живет на adapter-границе (Clean Architecture): prometheus-клиент импортируется
// ТОЛЬКО здесь и в composition root (cmd/vpc) — никогда в domain/ или use-case'ах.
// Метрики снимаются с отдельного cluster-internal diagnostic-порта (НЕ на public/
// internal gRPC-поверхности — security.md: internal-cardinality не tenant-facing).
//
// Один тип Metrics реализует оба corelib Recorder-порта:
//   - operations.Recorder — terminal-write retries/failures, inflight, orphans,
//     reconcile runs/errors (durability-слой LRO);
//   - outbox/metrics.Recorder — backlog/oldest/poisoned register-outbox.
//
// Плюс dependency_up зеркало readiness и build_info. Реестр — ПРИВАТНЫЙ
// (prometheus.NewRegistry, не global default): тесты герметичны, нет
// duplicate-register panic при рестартах composition root в одном процессе.
package metrics

import (
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowmetrics"

	"github.com/PRO-Robotech/kacho/pkg/authz/authzmetrics"
	opmetrics "github.com/PRO-Robotech/kacho/pkg/operations"
	outboxmetrics "github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
)

// Metrics владеет приватным prometheus-реестром и коллекторами kacho-vpc.
// Создается один раз в composition root и шарится diagnostic HTTP-listener'ом,
// reconciler'ом, outbox-collector'ом/drainer'ом и readiness-агрегатором.
type Metrics struct {
	reg *prometheus.Registry

	// operations (durability LRO)
	terminalRetries  *prometheus.CounterVec
	terminalFailures *prometheus.CounterVec
	orphans          *prometheus.CounterVec
	reconcileRuns    prometheus.Counter
	reconcileErrors  prometheus.Counter
	inflight         atomic.Int64

	// outbox (register-intent доставка)
	outboxBacklog   *prometheus.GaugeVec
	outboxOldest    *prometheus.GaugeVec
	outboxPoisonCur *prometheus.GaugeVec
	outboxPoisonTot *prometheus.CounterVec

	// Per-direction breakdown of the SAME queue. The table-wide series above aggregate
	// grants and withdrawals, and grants flow continuously — so the aggregate stays
	// healthy no matter whether a single withdrawal ever lands, and "it works" reads
	// exactly like "it was never revoked". delivered_total is the one that separates
	// "there were none" from "none get through".
	outboxDirBacklog   *prometheus.GaugeVec
	outboxDirOldest    *prometheus.GaugeVec
	outboxDirDelivered *prometheus.CounterVec

	// readiness mirror
	dependencyUp *prometheus.GaugeVec
}

// New конструирует адаптер, регистрирует Go + process runtime-коллекторы,
// build_info (const-метка сборки) и доменные коллекторы kacho-vpc.
func New(version, commit string) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &Metrics{
		reg: reg,
		terminalRetries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kacho_vpc_operations_terminal_write_retries_total",
			Help: "Retries of LRO terminal write (MarkDone/MarkError) on transient DB failure, by op.",
		}, []string{"op"}),
		terminalFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kacho_vpc_operations_terminal_write_failures_total",
			Help: "LRO terminal writes that failed after exhausting retries (row stays done=false), by op.",
		}, []string{"op"}),
		orphans: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kacho_vpc_operations_orphans_recovered_total",
			Help: "Orphaned LRO resolved by the reconciler, by terminal outcome (done|error).",
		}, []string{"outcome"}),
		reconcileRuns: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kacho_vpc_operations_reconcile_runs_total",
			Help: "Reconciler sweep cycles executed.",
		}),
		reconcileErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kacho_vpc_operations_reconcile_errors_total",
			Help: "Reconciler sweep cycles that hit an error.",
		}),
		outboxBacklog: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_vpc_outbox_backlog_depth",
			Help: "Pending rows in the register-outbox (sent_at IS NULL), by table.",
		}, []string{"table"}),
		outboxOldest: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_vpc_outbox_oldest_pending_age_seconds",
			Help: "Age of the oldest pending register-outbox row, by table.",
		}, []string{"table"}),
		outboxPoisonCur: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_vpc_outbox_poisoned_current",
			Help: "Current poisoned rows in the register-outbox, by table (Collector scan).",
		}, []string{"table"}),
		outboxPoisonTot: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kacho_vpc_outbox_poisoned_total",
			Help: "Monotonic register-outbox poison events (lost owner-tuple delivery), by table.",
		}, []string{"table"}),
		outboxDirBacklog: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_vpc_outbox_backlog_depth_by_direction",
			Help: "Pending register-outbox rows by table and queue direction (grant|withdrawal).",
		}, []string{"table", "direction"}),
		outboxDirOldest: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_vpc_outbox_oldest_pending_age_seconds_by_direction",
			Help: "Age of the oldest pending register-outbox row by table and queue direction; " +
				"for direction=withdrawal this is how long ago revocation stopped arriving.",
		}, []string{"table", "direction"}),
		outboxDirDelivered: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kacho_vpc_outbox_delivered_by_direction",
			Help: "Register-outbox rows delivered by table and queue direction, counted at " +
				"the moment of delivery. Zero for a direction means not one row of it has " +
				"been delivered SINCE THIS PROCESS STARTED — the state no other series here " +
				"can distinguish from a quiet queue. A counter, not a gauge: the value is " +
				"independent of how many rows are still stored, so sweeping delivered rows " +
				"never lowers it. Alert on increase() over a window, not on the raw value.",
		}, []string{"table", "direction"}),
		dependencyUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_vpc_dependency_up",
			Help: "Readiness mirror: 1 if the dependency is up, 0 if down, by dependency.",
		}, []string{"dependency"}),
	}

	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "kacho_vpc_build_info",
		Help:        "Build metadata of the running kacho-vpc binary (constant 1).",
		ConstLabels: prometheus.Labels{"version": version, "commit": commit},
	})
	buildInfo.Set(1)

	// lro_workers_active — живой gauge числа исполняемых LRO worker'ов; значение
	// питается SetInflight (operations.Recorder), читается через GaugeFunc, чтобы
	// быть согласованным с operations.Active() без дубль-регистрации.
	lroActive := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "kacho_vpc_lro_workers_active",
		Help: "In-flight LRO worker goroutines (operations.Active()).",
	}, func() float64 { return float64(m.inflight.Load()) })

	reg.MustRegister(
		m.terminalRetries, m.terminalFailures, m.orphans,
		m.reconcileRuns, m.reconcileErrors,
		m.outboxBacklog, m.outboxOldest, m.outboxPoisonCur, m.outboxPoisonTot,
		m.outboxDirBacklog, m.outboxDirOldest, m.outboxDirDelivered,
		m.dependencyUp, buildInfo, lroActive,
	)
	return m
}

// Handler возвращает promhttp-handler приватного реестра. Монтируется ТОЛЬКО на
// выделенном cluster-internal diagnostic-listener'е.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// Registerer отдаёт реестр этого сервиса как ПРИЁМНИК регистрации.
//
// # Зачем окно наружу, если рядом есть именованные
//
// Соседние окна (`RegisterAuthzCache`, `RegisterListNarrow`, `RegisterPoolStats`)
// принимают ЧИТАТЕЛЯ готовых величин: они существуют, чтобы этот пакет не
// импортировал доменные. Здесь предмет обратный — носителю входящего пути надо
// ЗАВЕСТИ своё семейство серий, а не отдать читателя, и заводит он его СВОИМИ
// руками. Тогда несогласованное объявление (то же имя с другой размерностью)
// становится отказом подъёма, а не молчаливой пропажей семейства со скрейпа.
//
// Разбор решения — у поля, ради которого окно открыто:
// `pkg/servicecontract.Spec.Metrics` (отказ старта О13). Здесь он не
// пересказывается: два места об одном предмете расходятся на первом уточнении.
//
// Отдаётся именно `Registerer`, а не сам реестр: собирать величины через это
// окно нельзя, и сузить его до одной операции дешевле, чем потом выяснять, кто
// ещё им воспользовался.
func (m *Metrics) Registerer() prometheus.Registerer { return m.reg }

// ---- operations.Recorder ----

// IncTerminalWriteRetries инкрементит ретраи терминальной записи по op-лейблу.
func (m *Metrics) IncTerminalWriteRetries(op string) { m.terminalRetries.WithLabelValues(op).Inc() }

// IncTerminalWriteFailures инкрементит невосстановимые терминальные записи.
func (m *Metrics) IncTerminalWriteFailures(op string) { m.terminalFailures.WithLabelValues(op).Inc() }

// SetInflight выставляет число исполняемых worker'ов (lro_workers_active gauge).
func (m *Metrics) SetInflight(n float64) { m.inflight.Store(int64(n)) }

// IncOrphansRecovered инкрементит разрешенные reconciler'ом orphan'ы по outcome.
func (m *Metrics) IncOrphansRecovered(outcome string) { m.orphans.WithLabelValues(outcome).Inc() }

// IncReconcileRuns инкрементит прогоны sweep-цикла reconciler'а.
func (m *Metrics) IncReconcileRuns() { m.reconcileRuns.Inc() }

// IncReconcileErrors инкрементит ошибки sweep-цикла reconciler'а.
func (m *Metrics) IncReconcileErrors() { m.reconcileErrors.Inc() }

// ---- outbox/metrics.Recorder ----

// SetBacklogDepth выставляет глубину pending-очереди register-outbox по таблице.
func (m *Metrics) SetBacklogDepth(table string, depth float64) {
	m.outboxBacklog.WithLabelValues(table).Set(depth)
}

// SetOldestPendingAgeSeconds выставляет возраст старейшей pending-строки.
func (m *Metrics) SetOldestPendingAgeSeconds(table string, age float64) {
	m.outboxOldest.WithLabelValues(table).Set(age)
}

// SetPoisonedCount выставляет текущее число отравленных строк (Collector scan).
func (m *Metrics) SetPoisonedCount(table string, count float64) {
	m.outboxPoisonCur.WithLabelValues(table).Set(count)
}

// IncPoisoned инкрементит монотонный poison-счетчик (drainer poison-observer).
func (m *Metrics) IncPoisoned(table string) { m.outboxPoisonTot.WithLabelValues(table).Inc() }

// ---- readiness mirror ----

// SetDependencyUp зеркалит readiness-состояние зависимости (1=up, 0=down).
func (m *Metrics) SetDependencyUp(dependency string, up bool) {
	v := 0.0
	if up {
		v = 1.0
	}
	m.dependencyUp.WithLabelValues(dependency).Set(v)
}

// Compile-time: адаптер удовлетворяет обоим corelib Recorder-портам.
var (
	_ opmetrics.Recorder              = (*Metrics)(nil)
	_ outboxmetrics.Recorder          = (*Metrics)(nil)
	_ outboxmetrics.DirectionRecorder = (*Metrics)(nil)
)

// ---- outbox/metrics.DirectionRecorder ----
//
// The Collector asserts this capability at run time, so losing these three methods in a
// refactor would not break the build — it would silently stop publishing the only series
// that says whether withdrawals arrive at all. The compile-time assertion above is what
// keeps that from happening quietly.

// SetBacklogDepthByDirection — pending rows of one direction of the queue.
func (m *Metrics) SetBacklogDepthByDirection(table, direction string, depth float64) {
	m.outboxDirBacklog.WithLabelValues(table, direction).Set(depth)
}

// SetOldestPendingAgeByDirection — age of the oldest pending row of one direction.
func (m *Metrics) SetOldestPendingAgeByDirection(table, direction string, age float64) {
	m.outboxDirOldest.WithLabelValues(table, direction).Set(age)
}

// IncDeliveredByDirection — ОДНА доставленная строка направления.
//
// СЧЁТЧИК, инкрементируемый наблюдателем дренажа, а не измеритель, ставящийся
// сканом (#1714): величина объявлена «за всё время», и счёт по живым строкам
// совпадал с этим ровно до появления уборки доставленных строк.
func (m *Metrics) IncDeliveredByDirection(table, direction string) {
	m.outboxDirDelivered.WithLabelValues(table, direction).Inc()
}

// InitDeliveredByDirection заводит серию направления с нулём, не увеличивая её:
// дочерняя серия счётчика иначе появилась бы только после ПЕРВОЙ доставки, и
// «ни одного отзыва не доставлено» выражалось бы отсутствием ряда вместо нуля.
func (m *Metrics) InitDeliveredByDirection(table, direction string) {
	m.outboxDirDelivered.WithLabelValues(table, direction)
}

// RegisterListNarrow провязывает читателя величин СУЖАТЕЛЯ СПИСКОВ.
//
// Коллектор — ОДНА реализация на все сервисы (`pkg/listnarrow/narrowmetrics`),
// потому что пять копий одинаковых на вид полос разъезжаются молча — ровно по
// той же причине, по которой единствен и сам сужатель. Здесь только имя
// сервиса и читатель.
//
// `read == nil` не отменяет серий: четыре полосы объявляются нулями и на
// посадке без сужателя, иначе «сужений не было» стало бы неотличимо от
// «коллектора нет».
func (m *Metrics) RegisterListNarrow(read func() listnarrow.Counts) {
	m.reg.MustRegister(narrowmetrics.New("vpc", read))
}

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
	m.reg.MustRegister(authzmetrics.New("vpc", lanes, decisions))
}
