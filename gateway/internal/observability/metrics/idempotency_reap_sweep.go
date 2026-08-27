// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
)

// idempotency_reap_sweep.go — отставание уборки записей однократности (#1302).
//
// # Зачем серия, если уборка уже пишет в журнал
//
// Запись в журнале отвечает «не догнали В ЭТОТ РАЗ». Решение, ради которого
// наблюдение и заводят — хватает ли уборке ёмкости под фактическим темпом, —
// принимается по другому: растёт отставание или колеблется, и как давно. По
// одиночным строкам журнала этого не увидеть, а по ряду — видно сразу.
//
// # Почему три серии, а не одно отставание
//
// Нулевое отставание отвечает на два разных вопроса одинаково: «уборка
// догоняет» и «уборка не исполнялась ни разу». Различает их счётчик заходов, и
// без него ноль означал бы тишину, а не благополучие — ровно то, что
// `security.md` §Hardening-инвариант 8 велит делать заметным.
//
// Унесённое монотонно, поэтому по нему берётся производная — темп уборки;
// отставание колеблется и производной не имеет смысла.
//
// # Почему величины читаются ИЗ ПРОЦЕССА
//
// Сбор не ходит в базу: диагностика, которая ходит наружу за своими числами,
// гаснет ровно тогда, когда нужна. Отставание измеряет САМА уборка, в тот
// момент, когда она уже держит соединение, и кладёт в снимок.
var (
	idempReapSweepsDesc = prometheus.NewDesc(
		"kacho_api_gateway_idempotency_reap_sweeps_total",
		"Sweeps of the idempotency records table performed since process start. "+
			"Read it before the lag gauge: a zero lag means 'caught up' only if this counter moves.",
		nil, nil)
	idempReapRemovedDesc = prometheus.NewDesc(
		"kacho_api_gateway_idempotency_reap_removed_total",
		"Expired idempotency record rows removed since process start. "+
			"Its derivative over time is the cleanup rate; compare it against the rate of mutations carrying an Idempotency-Key.",
		nil, nil)
	idempReapLagDesc = prometheus.NewDesc(
		"kacho_api_gateway_idempotency_reap_lag_seconds",
		"Age of the oldest expired idempotency record LEFT BEHIND by the last sweep. "+
			"Zero while the sweep drains the tail; grows without bound once the write rate outruns it.",
		nil, nil)
)

// RegisterIdempotencyReap провязывает читателя величин уборки записей
// однократности.
//
// Функция и снимок, а не носитель: хранилище собирается композиционным корнем
// задолго до диагностической поверхности, и живёт оно не всегда — флот в одну
// реплику обходится памятью процесса, и тогда таблицы нет вовсе.
//
// nil-безопасна: сбор величин не имеет права ронять подъём края. Именно поэтому
// её пропажу не поймает компилятор, и свойство «читатель есть» держит гейт
// дерева `internal/repohygiene` `TestDeclaredAccumulatorsHaveANonTestReader`.
func (m *Metrics) RegisterIdempotencyReap(read func() idempotencypg.ReapSweepStats) {
	if m == nil || read == nil {
		return
	}
	m.reg.MustRegister(&idempReapCollector{read: read})
}

type idempReapCollector struct {
	read func() idempotencypg.ReapSweepStats
}

// Describe объявляет все три семейства ДО первого сбора: серии стоят нулями с
// первой секунды жизни процесса, поэтому «уборок ещё не было» и «коллектора
// нет» различимы без единого запроса.
func (c *idempReapCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- idempReapSweepsDesc
	ch <- idempReapRemovedDesc
	ch <- idempReapLagDesc
}

// Collect отдаёт снимок уборки.
func (c *idempReapCollector) Collect(ch chan<- prometheus.Metric) {
	st := c.read()
	ch <- prometheus.MustNewConstMetric(idempReapSweepsDesc, prometheus.CounterValue, float64(st.Sweeps))
	ch <- prometheus.MustNewConstMetric(idempReapRemovedDesc, prometheus.CounterValue, float64(st.RemovedTotal))
	ch <- prometheus.MustNewConstMetric(idempReapLagDesc, prometheus.GaugeValue, st.Lag.Seconds())
}
