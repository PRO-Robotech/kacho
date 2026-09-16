// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_lane.go — клетки полосы сессии человека и ретрансляции полосы формы
// (приёмка Ф3, Ф3-48).
//
// Клетки существуют с нулём с первой секунды жизни процесса — «отказов по
// отсечке не было» и «полосы нет» обязаны различаться без единого запроса.
// Словарь меток закрыт: исходы полосы — константы этого файла, глаголы формы —
// объявление `middleware.LoginLaneRoutes` (единственное; второе здесь было бы
// вторым местом об одном предмете).
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// Исходы полосы сессии, закрытый словарь.
const (
	sessionOutcomeCutoffDenied = "cutoff_denied"
	sessionOutcomeNoSession    = "no_session"
	sessionOutcomeUnavailable  = "unavailable"
)

// SessionLaneSnapshot — то, что корень отдаёт коллектору на каждый сбор: клетки
// полосы личности и клетки ретранслятора вместе — они собраны в разных местах
// процесса, и снимок — единственная форма, в которой они приходят сюда разом.
type SessionLaneSnapshot struct {
	Lane  middleware.SessionLaneSnapshot
	Relay handler.LoginLaneRelaySnapshot
}

var (
	sessionRefusalsDesc = prometheus.NewDesc(
		"kacho_api_gateway_session_lane_refusals_total",
		"Browser-session refusals at the edge, by outcome: cutoff_denied (ended by our revocation, "+
			"carrier ended), no_session (a carrier the service does not know: unknown, logged out, "+
			"expired or blocked — one answer, carrier ended), unavailable (the service did not answer; "+
			"carrier intact).",
		[]string{"outcome"}, nil)
	sessionRolloutWindowDesc = prometheus.NewDesc(
		"kacho_api_gateway_session_lane_rollout_window_total",
		"Sessions let through LOUDLY because the authority does not yet offer the cutoff question "+
			"(image skew during a rollout). Non-zero after a rollout settled means the check is not enforced.",
		nil, nil)
	loginLaneRelayedDesc = prometheus.NewDesc(
		"kacho_api_gateway_login_lane_relayed_total",
		"Login-lane requests relayed to the identity service's form listener, by verb.",
		[]string{"verb"}, nil)
	loginLaneUnreachableDesc = prometheus.NewDesc(
		"kacho_api_gateway_login_lane_unreachable_total",
		"Login-lane requests answered 503 by the edge because the form listener could not be reached.",
		nil, nil)
)

// RegisterSessionLane провязывает читателя клеток полосы сессии и ретрансляции.
func (m *Metrics) RegisterSessionLane(read func() SessionLaneSnapshot) {
	if m == nil || read == nil {
		return
	}
	m.reg.MustRegister(&sessionLaneCollector{read: read})
}

type sessionLaneCollector struct {
	read func() SessionLaneSnapshot
}

func (c *sessionLaneCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- sessionRefusalsDesc
	ch <- sessionRolloutWindowDesc
	ch <- loginLaneRelayedDesc
	ch <- loginLaneUnreachableDesc
}

// Collect — ни одного внешнего вызова: `read` возвращает величины, уже лежащие
// в процессе. Глаголы обходятся по ОБЪЯВЛЕНИЮ, а не по ключам снимка: клетка
// глагола, по которому ретрансляций не было, обязана стоять нулём.
func (c *sessionLaneCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.read()
	for outcome, value := range map[string]uint64{
		sessionOutcomeCutoffDenied: s.Lane.CutoffDenied,
		sessionOutcomeNoSession:    s.Lane.NoSession,
		sessionOutcomeUnavailable:  s.Lane.Unavailable,
	} {
		ch <- prometheus.MustNewConstMetric(sessionRefusalsDesc, prometheus.CounterValue, float64(value), outcome)
	}
	ch <- prometheus.MustNewConstMetric(sessionRolloutWindowDesc, prometheus.CounterValue, float64(s.Lane.RolloutWindow))
	for _, rt := range middleware.LoginLaneRoutes() {
		ch <- prometheus.MustNewConstMetric(loginLaneRelayedDesc, prometheus.CounterValue, float64(s.Relay.Relayed[rt.Verb]), rt.Verb)
	}
	ch <- prometheus.MustNewConstMetric(loginLaneUnreachableDesc, prometheus.CounterValue, float64(s.Relay.Unreachable))
}
