// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware/anonmail"
)

// anon_mail.go — наблюдаемость ограничителя анонимной почты края (приёмка
// NTF-2, Р5; замысел issue-2917, З8): ответы 503 «хранилище ограничителя
// недоступно». Своей метрики у остатка «исход фиксации не разрешён» нет — он
// входит в эту же серию и называется журналом WARN с decision_id.
var anonMailStoreUnavailableDesc = prometheus.NewDesc(
	"kacho_api_gateway_anon_mail_store_unavailable_total",
	"Anonymous mail limiter answers 503 'request limiter is unavailable' since process start: "+
		"the store did not answer within the limiter's wait, or a commit outcome could not be resolved. "+
		"The request never reached the identity service.",
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
func (c *anonMailCollector) Describe(ch chan<- *prometheus.Desc) { ch <- anonMailStoreUnavailableDesc }

// Collect отдаёт снимок звена.
func (c *anonMailCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(anonMailStoreUnavailableDesc, prometheus.CounterValue, float64(c.read().StoreUnavailable))
}
