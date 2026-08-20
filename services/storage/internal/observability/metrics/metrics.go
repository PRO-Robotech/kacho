// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package metrics — Prometheus observability adapter kacho-storage.
//
// Живёт на adapter-границе (Clean Architecture): prometheus-клиент импортируется
// ТОЛЬКО здесь и в composition root (cmd/storage) — никогда в domain/ или
// service-слое. Метрики снимаются с отдельного cluster-internal diagnostic-порта
// (НЕ на public/internal gRPC-поверхности: внутренняя кардинальность не
// tenant-facing). Реестр ПРИВАТНЫЙ (prometheus.NewRegistry, не global default):
// тесты герметичны, нет duplicate-register panic при рестартах composition root в
// одном процессе. Зеркалит kacho-compute / kacho-vpc.
//
// # Что здесь есть и почему именно это
//
// Ровно ОДНО семейство доменных серий — доставка register-outbox'а
// (`kacho_storage.fga_register_outbox`). Оно выбрано не «чтобы эндпоинт был
// непустой», а потому что это ЕДИНСТВЕННАЯ дренируемая очередь сервиса, и её
// молчаливая недоставка уже случалась на платформе как класс: очередь, не
// доставившая НИ ОДНОЙ строки за всю свою жизнь, выглядела исправной, потому что
// заметить это было нечем. Правило проекта прямо требует table-wide
// oldest-pending-gauge, иначе застрявший отзыв прав тихий.
//
// Доменного outbox'а (`kacho_storage.storage_outbox`) в схеме БОЛЬШЕ НЕТ — таблица
// дропнута миграцией 0011. Прежняя редакция объясняла, почему он «здесь не
// инструментирован намеренно», то есть говорила о нём как о живом. Инструментировать
// нечего; серии ниже относятся к очереди регистраций прав.
//
//	kacho_storage_outbox_backlog_depth{table}              — pending-строк
//	kacho_storage_outbox_oldest_pending_age_seconds{table} — возраст старейшей pending
//	kacho_storage_outbox_poisoned_rows{table}              — отравленных сейчас
//	kacho_storage_outbox_poisoned_total{table}             — монотонный счётчик отравлений
//
// Лейбл `table` обязателен: две очереди с разной семантикой не должны сливаться в
// одну цифру. `poisoned_total` — счётчик, а не gauge: отравление это СОБЫТИЕ, и
// после ужесточения классификации отказа в правах до терминального оно стало
// реально достижимым, поэтому обязано быть alertable, а не выводимым из разницы
// gauge'ей между скрейпами.
//
// Плюс стандартные Go/process runtime-коллекторы — они бесплатны и превращают
// утечку горутин/дескрипторов в график вместо догадки.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowmetrics"

	"github.com/PRO-Robotech/kacho/pkg/authz/authzmetrics"
	outboxmetrics "github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
)

// Metrics владеет приватным prometheus-реестром kacho-storage. Создаётся один раз в
// composition root и шарится diagnostic HTTP-listener'ом, outbox-коллекторами и
// poison-observer'ом дренажа.
type Metrics struct {
	reg *prometheus.Registry

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
	outboxDirDelivered *prometheus.GaugeVec
}

// New конструирует адаптер и регистрирует Go + process runtime-коллекторы и
// outbox-серии kacho-storage.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &Metrics{
		reg: reg,
		outboxBacklog: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_storage_outbox_backlog_depth",
			Help: "Undelivered (sent_at IS NULL) rows currently in the outbox table.",
		}, []string{"table"}),
		outboxOldest: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_storage_outbox_oldest_pending_age_seconds",
			Help: "Age of the oldest undelivered row. A rising floor means the queue is wedged, not slow.",
		}, []string{"table"}),
		outboxPoisonCur: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_storage_outbox_poisoned_rows",
			Help: "Rows that exhausted their attempts and will never be delivered without intervention.",
		}, []string{"table"}),
		outboxPoisonTot: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kacho_storage_outbox_poisoned_total",
			Help: "Poison events, monotonic. Poisoning is terminal, so every increment is an intent that was dropped.",
		}, []string{"table"}),
		outboxDirBacklog: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_storage_outbox_backlog_depth_by_direction",
			Help: "Pending register-outbox rows by table and queue direction (grant|withdrawal).",
		}, []string{"table", "direction"}),
		outboxDirOldest: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_storage_outbox_oldest_pending_age_seconds_by_direction",
			Help: "Age of the oldest pending register-outbox row by table and queue direction; " +
				"for direction=withdrawal this is how long ago revocation stopped arriving.",
		}, []string{"table", "direction"}),
		outboxDirDelivered: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_storage_outbox_delivered_by_direction",
			Help: "Register-outbox rows delivered so far by table and queue direction. " +
				"Zero for a direction means not one row of it has EVER been delivered — the " +
				"state no other series here can distinguish from a quiet queue.",
		}, []string{"table", "direction"}),
	}
	reg.MustRegister(m.outboxBacklog, m.outboxOldest, m.outboxPoisonCur, m.outboxPoisonTot,
		m.outboxDirBacklog, m.outboxDirOldest, m.outboxDirDelivered)
	return m
}

// Handler возвращает promhttp-handler приватного реестра. Монтируется ТОЛЬКО на
// выделенном cluster-internal diagnostic-listener'е.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// ---- outbox/metrics.Recorder ----

// SetBacklogDepth выставляет глубину pending-очереди по таблице.
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

// IncPoisoned инкрементит монотонный poison-счётчик (drainer poison-observer).
func (m *Metrics) IncPoisoned(table string) { m.outboxPoisonTot.WithLabelValues(table).Inc() }

// Compile-time: адаптер удовлетворяет corelib Recorder-порту.
var (
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

// SetDeliveredTotal — rows of one direction delivered so far.
func (m *Metrics) SetDeliveredTotal(table, direction string, count float64) {
	m.outboxDirDelivered.WithLabelValues(table, direction).Set(count)
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
	m.reg.MustRegister(narrowmetrics.New("storage", read))
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
	m.reg.MustRegister(authzmetrics.New("storage", lanes, decisions))
}
