// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware/anonmail"
)

// anon_mail.go — наблюдаемость ограничителя анонимной почты края (приёмка
// NTF-2, Р5; замысел issue-2917, З8): ответы 503 «хранилище ограничителя
// недоступно» и из них — отказы насыщения (Д66). Своей метрики у остатка
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

// Describe объявляет серию до первого сбора: ноль отказов и «коллектора нет»
// различимы без единого запроса.
func (c *anonMailCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- anonMailStoreUnavailableDesc
	ch <- anonMailBucketWaitTimeoutsDesc
}

// Collect отдаёт снимок звена.
func (c *anonMailCollector) Collect(ch chan<- prometheus.Metric) {
	st := c.read()
	ch <- prometheus.MustNewConstMetric(anonMailStoreUnavailableDesc, prometheus.CounterValue, float64(st.StoreUnavailable))
	ch <- prometheus.MustNewConstMetric(anonMailBucketWaitTimeoutsDesc, prometheus.CounterValue, float64(st.BucketWaitTimeouts))
}
