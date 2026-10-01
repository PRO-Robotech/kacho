// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware/anonmail"
)

// anon_mail.go — наблюдаемость ограничителя анонимной почты края (приёмка
// NTF-2, Р5; замысел issue-2917, З8): ответы 503 «хранилище ограничителя
// недоступно», из них — отказы насыщения (Д66), опережающая серия насыщения —
// удержание строки ведра, и смещение часов реплики от часов базы. Своей метрики у остатка
// «исход фиксации не разрешён» нет — он входит в общую серию и называется
// журналом WARN с decision_id.
var anonMailStoreUnavailableDesc = prometheus.NewDesc(
	"kacho_api_gateway_anon_mail_store_unavailable_total",
	"Anonymous mail limiter answers 503 'request limiter is unavailable' since process start: "+
		"the store did not answer within the limiter's wait, or a commit outcome could not be resolved. "+
		"The request never reached the identity service.",
	nil, nil)

// anonMailBucketWaitTimeoutsDesc — из ответов 503 ограничителя те, где решение
// не получило строку ведра общего потока за предел ожидания (решение Д66): поток
// выше пропускной способности строки ведра флота кончается закрытым отказом,
// и это принятая цена замысла — серия и правило тревоги по ней делают её
// видимой. Ожидание блокировки ключа, пометки вызова и захват соединения пула
// сюда не входят (CX2-93 (а)): они растят только серию недоступности.
var anonMailBucketWaitTimeoutsDesc = prometheus.NewDesc(
	"kacho_api_gateway_anon_mail_bucket_wait_timeouts_total",
	"Anonymous mail limiter answers 503 since process start because the decision did not obtain "+
		"the global bucket row within the limiter's wait: the fleet's decision throughput is saturated. "+
		"Key lock, challenge mark and connection acquire waits are not counted here. "+
		"A subset of kacho_api_gateway_anon_mail_store_unavailable_total.",
	nil, nil)

// anonMailBucketHoldDesc — опережающая серия насыщения (решение Д66; ревью
// system-design CRIT-1): секунды, пока решения реплики держали строку ведра
// общего потока — от получения строки до конца её транзакции (у memory — до
// отпускания ведра). Строка одна на установку, поэтому
// sum(rate(...[5m])) по флоту — доля времени, когда она занята (0…1), и растёт
// она ДО первых отказов 503: правило уровня warning по ней звонит, пока запас
// ещё есть.
var anonMailBucketHoldDesc = prometheus.NewDesc(
	"kacho_api_gateway_anon_mail_bucket_hold_seconds_total",
	"Seconds the anonymous mail limiter's decisions of this process held the fleet-wide global bucket row "+
		"(from obtaining the row to the end of its transaction). The row is one per installation, so "+
		"sum(rate()) over the fleet is the fraction of time the row is busy: it rises before the first 503.",
	nil, nil)

// anonMailClockOffsetDesc — смещение часов реплики от часов базы ограничителя
// (ревью system-design I-2, решение Д71): момент решения минус clock_timestamp()
// базы в последнем решении, измерившем его. Моменты пропуска, окна и момент
// ведра судят часы реплики; реплика впереди базы пишет моменты в будущее.
// Измерение смещено вниз не больше чем на срок решения (момент решения берётся
// до захвата соединения), поэтому плюс — всегда сдвиг часов. Пока не измерено
// (и у memory, где часов базы нет) серии нет: ноль значил бы «часы сверены».
var anonMailClockOffsetDesc = prometheus.NewDesc(
	"kacho_api_gateway_anon_mail_clock_offset_seconds",
	"Offset of this replica's clock from the limiter database clock in the last decision that measured it "+
		"(decision moment minus the database clock_timestamp()); positive means the replica is ahead. "+
		"Absent until measured and on the memory store, which has no database clock.",
	nil, nil)

// RegisterAnonMail провязывает читателя величин звена-ограничителя. Функция и
// снимок, а не носитель: сбор не ходит в хранилище. nil-безопасна; свойство
// «читатель есть» держит гейт дерева TestDeclaredAccumulatorsHaveANonTestReader.
func (m *Metrics) RegisterAnonMail(read func() anonmail.Stats) {
	if m == nil || read == nil {
		return
	}
	m.reg.MustRegister(&anonMailCollector{read: read})
}

type anonMailCollector struct {
	read func() anonmail.Stats
}

// Describe объявляет серии до первого сбора: ноль отказов и «коллектора нет»
// различимы без единого запроса. Серия смещения часов объявлена, но
// отдаётся только измеренной.
func (c *anonMailCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- anonMailStoreUnavailableDesc
	ch <- anonMailBucketWaitTimeoutsDesc
	ch <- anonMailBucketHoldDesc
	ch <- anonMailClockOffsetDesc
}

// Collect отдаёт снимок звена.
func (c *anonMailCollector) Collect(ch chan<- prometheus.Metric) {
	st := c.read()
	ch <- prometheus.MustNewConstMetric(anonMailStoreUnavailableDesc, prometheus.CounterValue, float64(st.StoreUnavailable))
	ch <- prometheus.MustNewConstMetric(anonMailBucketWaitTimeoutsDesc, prometheus.CounterValue, float64(st.BucketWaitTimeouts))
	ch <- prometheus.MustNewConstMetric(anonMailBucketHoldDesc, prometheus.CounterValue, st.BucketHold.Seconds())
	if st.ClockOffsetMeasured {
		ch <- prometheus.MustNewConstMetric(anonMailClockOffsetDesc, prometheus.GaugeValue, st.ClockOffset.Seconds())
	}
}
