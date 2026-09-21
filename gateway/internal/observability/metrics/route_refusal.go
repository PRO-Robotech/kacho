// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import "github.com/prometheus/client_golang/prometheus"

// routeRefusalDesc — отказы по маршруту на НАТИВНОЙ полосе внешнего слушателя.
//
// Нативный близнец полосы `unserved` из семейства решений. Отдельное семейство,
// а не метка в том: то семейство называется «решения о доступе», а здесь
// решения не принималось — ни каталог, ни личность, ни модель не спрашивались.
// Общая метка сделала бы перебор административной поверхности неотличимым от
// работы модели прав ровно в том месте, где на неё смотрят.
var routeRefusalDesc = prometheus.NewDesc(
	"kacho_api_gateway_route_refusals_total",
	"Internal* method calls refused by route on the external gRPC listener, before any "+
		"authorization decision. Its HTTP-lane twin is the `unserved` band of "+
		"kacho_api_gateway_authz_check_decisions_total.",
	nil, nil)

// routeRefusalCollector отдаёт величину, даже когда она нулевая: отсутствие
// серии и нулевая серия обязаны быть различимы.
type routeRefusalCollector struct{ read func() uint64 }

func (c *routeRefusalCollector) Describe(ch chan<- *prometheus.Desc) { ch <- routeRefusalDesc }

func (c *routeRefusalCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(routeRefusalDesc, prometheus.CounterValue, float64(c.read()))
}

// RegisterRouteRefusal провязывает читателя величины отказов по маршруту.
func (m *Metrics) RegisterRouteRefusal(read func() uint64) {
	if m == nil || read == nil {
		return
	}
	m.reg.MustRegister(&routeRefusalCollector{read: read})
}
