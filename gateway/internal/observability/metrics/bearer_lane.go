// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// bearerLaneRevocationDesc — клетки полосы предъявителя (kacho#2740): исход
// вопроса об отзыве по источнику. Строка журнала раз в окно называет ПЕРВОЕ
// событие окна; клетка — сколько их было и сколько длится состояние.
var bearerLaneRevocationDesc = prometheus.NewDesc(
	"kacho_api_gateway_bearer_lane_revocation_total",
	"Bearer-lane revocation questions at the edge, by source (authority — our own minting; "+
		"record — any other accepted issuer record) and outcome: live, revoked (refused 401), "+
		"unanswered (the source did not answer; 503), misconfigured (something that is not a revocation "+
		"source answered; 503), no_identifier (the token carries no jti to ask by; 503), not_wired "+
		"(the reader is not assembled: authority — 503, record — not asked).",
	[]string{"source", "outcome"}, nil)

// RegisterBearerLane провязывает читателя клеток полосы предъявителя.
func (m *Metrics) RegisterBearerLane(read func() middleware.BearerLaneSnapshot) {
	if m == nil || read == nil {
		return
	}
	m.reg.MustRegister(&bearerLaneCollector{read: read})
}

type bearerLaneCollector struct {
	read func() middleware.BearerLaneSnapshot
}

func (c *bearerLaneCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- bearerLaneRevocationDesc
}

// Collect обходит ЗАКРЫТЫЙ перечень состояний полосы, а не ключи слепка:
// клетка состояния, которого ещё не было, обязана стоять нулём.
func (c *bearerLaneCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.read()
	for _, st := range middleware.BearerLaneStates() {
		ch <- prometheus.MustNewConstMetric(bearerLaneRevocationDesc, prometheus.CounterValue,
			float64(s.Value(st)), st.Source, st.Outcome)
	}
}
