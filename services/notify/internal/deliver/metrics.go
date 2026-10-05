// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// metrics — счётчики исполнителей строк (З22, З21, З27).
type metrics struct {
	templateSkew     *prometheus.CounterVec
	leaseBudgetShort *prometheus.CounterVec
}

// newMetrics регистрирует счётчики и заводит нулём серии каждого источника:
// «ноль за всю жизнь» отличим от «серии нет».
func newMetrics(reg prometheus.Registerer, modules []string) (*metrics, error) {
	m := &metrics{
		templateSkew: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "notify_template_skew_total",
			Help: "Строки, отложенные template_skew (клетка 1): шаблона нет в сборке либо ревизия строки " +
				"старше или новее ревизии сборки.",
		}, []string{"source", "direction"}),
		leaseBudgetShort: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "notify_lease_budget_short_total",
			Help: "Строки, не начатые из-за остатка аренды меньше суммы сроков обработки (клетка 8): " +
				"Ack не послан, строку выдаст следующий Claim.",
		}, []string{"source"}),
	}
	for _, c := range []prometheus.Collector{m.templateSkew, m.leaseBudgetShort} {
		if err := reg.Register(c); err != nil {
			return nil, fmt.Errorf("регистрация счётчиков исполнителей: %w", err)
		}
	}
	for _, s := range modules {
		for _, d := range directions() {
			m.templateSkew.WithLabelValues(s, d)
		}
		m.leaseBudgetShort.WithLabelValues(s)
	}
	return m, nil
}
