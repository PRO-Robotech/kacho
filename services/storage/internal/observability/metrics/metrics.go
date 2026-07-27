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
// `kacho_storage.storage_outbox` (доменные события) здесь НЕ инструментирован
// намеренно: у него другая форма строки (`sequence_no`/`processed_at` вместо
// `sent_at`/`attempt_count`), corelib-коллектор её не сканирует, и дренажа у него
// нет. Придумывать ему серии значило бы отдавать цифру, за которой ничего не
// происходит.
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
	}
	reg.MustRegister(m.outboxBacklog, m.outboxOldest, m.outboxPoisonCur, m.outboxPoisonTot)
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
var _ outboxmetrics.Recorder = (*Metrics)(nil)
