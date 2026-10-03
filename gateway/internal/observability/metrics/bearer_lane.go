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

// Метки — закрытый словарь: источник — константа, исход — ключ литерального
// набора констант. Клетка исхода, которого ещё не было, стоит нулём.
const (
	bearerSourceAuthority      = "authority"
	bearerSourceRecord         = "record"
	bearerOutcomeLive          = "live"
	bearerOutcomeRevoked       = "revoked"
	bearerOutcomeUnanswered    = "unanswered"
	bearerOutcomeMisconfigured = "misconfigured"
	bearerOutcomeNoIdentifier  = "no_identifier" // #nosec G101 -- значение метки исхода, а не удостоверение
	bearerOutcomeNotWired      = "not_wired"
)

// Collect — ни одного внешнего вызова: `read` возвращает величины, уже лежащие
// в процессе.
func (c *bearerLaneCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.read()
	for outcome, value := range map[string]uint64{
		bearerOutcomeLive:          s.Authority.Live,
		bearerOutcomeRevoked:       s.Authority.Revoked,
		bearerOutcomeUnanswered:    s.Authority.Unanswered,
		bearerOutcomeMisconfigured: s.Authority.Misconfigured,
		bearerOutcomeNoIdentifier:  s.Authority.NoIdentifier,
		bearerOutcomeNotWired:      s.Authority.NotWired,
	} {
		ch <- prometheus.MustNewConstMetric(bearerLaneRevocationDesc, prometheus.CounterValue,
			float64(value), bearerSourceAuthority, outcome)
	}
	for outcome, value := range map[string]uint64{
		bearerOutcomeLive:          s.Record.Live,
		bearerOutcomeRevoked:       s.Record.Revoked,
		bearerOutcomeUnanswered:    s.Record.Unanswered,
		bearerOutcomeMisconfigured: s.Record.Misconfigured,
		bearerOutcomeNoIdentifier:  s.Record.NoIdentifier,
		bearerOutcomeNotWired:      s.Record.NotWired,
	} {
		ch <- prometheus.MustNewConstMetric(bearerLaneRevocationDesc, prometheus.CounterValue,
			float64(value), bearerSourceRecord, outcome)
	}
}
