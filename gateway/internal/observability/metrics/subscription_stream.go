// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// subscription_stream.go — проекция потока изменений на диагностической
// поверхности (kacho#1020).
//
// # Зачем серии, если счётчики уже есть в процессе
//
// Затем, что «ноль отказов за всю жизнь контроля» обязано быть ЗАМЕТНО.
// Величина, которую никто не читает, не отличима от «этот код не исполнялся» —
// потолок, ни разу не сработавший, и потолок, не подключённый вовсе, дают один и
// тот же ноль. Пока счётчик считает в никуда, его собственное заявление о
// заметности ложно.
//
// # Почему отказы РАЗВЕДЕНЫ по полосам, а не сложены в один счётчик
//
// Полос четыре, и они означают РАЗНОЕ для дежурного:
//
//	вход        — клиент прислал негодный запрос: вина вызывающего;
//	нет владельца — владелец не объявлен ПОСАДКОЙ: наша ошибка развёртывания;
//	личность    — запрос без названного вызывающего;
//	предел      — исчерпание, и оно двух родов: реплики и субъекта.
//
// Сложи их — и «клиенты массово шлют мусор» станет неотличимо от «мы не
// объявили владельца». Первое не требует действий, второе требует немедленных.
//
// # Почему исчерпание реплики и исчерпание субъекта — РАЗНЫЕ серии
//
// Первое означает «пора добавить реплик либо поднять потолок», второе — «один
// арендатор упёрся в своё, остальные не задеты». Ответ дежурного на них
// противоположный, а сложенные они дают число, по которому не выбрать ни одного.
var (
	subStreamOpenDesc = prometheus.NewDesc(
		"kacho_api_gateway_subscription_streams_open",
		"Subscription streams open on this replica right now. "+
			"Read against the ceiling gauges: a count without a scale says nothing.",
		nil, nil)
	subStreamCeilingDesc = prometheus.NewDesc(
		"kacho_api_gateway_subscription_streams_ceiling",
		"Ceiling on concurrent subscription streams for this replica. "+
			"Replicas x ceiling must fit under the journal owner's own stream ceiling.",
		nil, nil)
	subStreamSubjectCeilingDesc = prometheus.NewDesc(
		"kacho_api_gateway_subscription_streams_subject_ceiling",
		"Ceiling on concurrent subscription streams for ONE caller. "+
			"Without it a single tenant takes the replica ceiling whole.",
		nil, nil)
	subStreamOpenedDesc = prometheus.NewDesc(
		"kacho_api_gateway_subscription_streams_opened_total",
		"Subscription streams opened since process start.", nil, nil)
	subStreamEventsDesc = prometheus.NewDesc(
		"kacho_api_gateway_subscription_events_sent_total",
		"Events framed to subscribers since process start.", nil, nil)
	subStreamRefusedDesc = prometheus.NewDesc(
		"kacho_api_gateway_subscription_streams_refused_total",
		"Subscription streams refused, by lane. Lanes mean different things to the "+
			"operator and are never summed: `input` is the caller's fault, "+
			"`no_owner` is ours, `limit_replica` asks for capacity, `limit_subject` does not.",
		[]string{"lane"}, nil)
	subStreamClosedByOwnerDesc = prometheus.NewDesc(
		"kacho_api_gateway_subscription_streams_closed_by_owner_total",
		"Streams the journal owner ended after they were already open. "+
			"These cannot become a response code: the headers are out by then.",
		nil, nil)
)

// RegisterSubscriptionStream провязывает читателя счётчиков проекции потока.
//
// Функция, а не носитель: ручка собирается композиционным корнем задолго до
// диагностической поверхности, и снимок — единственная форма, в которой её
// величины приходят сюда.
//
// nil-безопасна: сбор величин не имеет права ронять подъём края. Именно поэтому
// её пропажу не поймает компилятор, и свойство «читатель есть» держит гейт
// дерева `internal/repohygiene` `TestDeclaredAccumulatorsHaveANonTestReader`.
func (m *Metrics) RegisterSubscriptionStream(
	read func() subscriptionstream.Stats,
	ceiling, subjectCeiling int,
) {
	if m == nil || read == nil {
		return
	}
	m.reg.MustRegister(&subscriptionStreamCollector{
		read:           read,
		ceiling:        ceiling,
		subjectCeiling: subjectCeiling,
	})
}

// subscriptionStreamCollector читает величины ИЗ ПРОЦЕССА: ни одного внешнего
// вызова в момент сбора — диагностика, которая ходит наружу за своими числами,
// гаснет ровно тогда, когда нужна.
type subscriptionStreamCollector struct {
	read           func() subscriptionstream.Stats
	ceiling        int
	subjectCeiling int
}

// Describe объявляет все семейства ДО первого сбора: серии стоят нулями с первой
// секунды жизни процесса, поэтому «отказов не было» и «коллектора нет»
// различимы без единого запроса.
func (c *subscriptionStreamCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- subStreamOpenDesc
	ch <- subStreamCeilingDesc
	ch <- subStreamSubjectCeilingDesc
	ch <- subStreamOpenedDesc
	ch <- subStreamEventsDesc
	ch <- subStreamRefusedDesc
	ch <- subStreamClosedByOwnerDesc
}

// Collect отдаёт снимок ручки.
func (c *subscriptionStreamCollector) Collect(ch chan<- prometheus.Metric) {
	st := c.read()
	ch <- prometheus.MustNewConstMetric(subStreamOpenDesc, prometheus.GaugeValue, float64(st.Open))
	ch <- prometheus.MustNewConstMetric(subStreamCeilingDesc, prometheus.GaugeValue, float64(c.ceiling))
	ch <- prometheus.MustNewConstMetric(subStreamSubjectCeilingDesc, prometheus.GaugeValue,
		float64(c.subjectCeiling))
	ch <- prometheus.MustNewConstMetric(subStreamOpenedDesc, prometheus.CounterValue, float64(st.Opened))
	ch <- prometheus.MustNewConstMetric(subStreamEventsDesc, prometheus.CounterValue, float64(st.EventsSent))
	ch <- prometheus.MustNewConstMetric(subStreamClosedByOwnerDesc, prometheus.CounterValue,
		float64(st.ClosedByOwner))

	for lane, value := range map[string]uint64{
		"input":    st.RefusedInput,
		"no_owner": st.RefusedNoOwner,
		"identity": st.RefusedAuthN,
		// Полоса отдельная, а не слитая с `identity`: «пришли без удостоверения»
		// и «пришли с удостоверением вида, которому подписка не полагается» —
		// разные наблюдения, и лечит их разное. Слей их — и решение о том, кому
		// подписка положена, стало бы невидимо.
		"subject_kind":  st.RefusedSubjectKind,
		"limit_replica": st.RefusedSlot,
		"limit_subject": st.RefusedSubjectQuota,
		"owner_refusal": st.RefusedOwner,
	} {
		ch <- prometheus.MustNewConstMetric(subStreamRefusedDesc, prometheus.CounterValue,
			float64(value), lane)
	}
}
