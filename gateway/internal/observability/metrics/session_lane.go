// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_lane.go — клетки полосы сессии человека и ретрансляции полосы формы
// (приёмка Ф3, Ф3-48) и клетка уровня вне оси сессии (приёмка Ф11, Ф11-19).
//
// Клетки существуют с нулём с первой секунды жизни процесса — «отказов по
// отсечке не было» и «полосы нет» обязаны различаться без единого запроса.
// Словарь меток закрыт константами этого файла; словарь глаголов сверяется с
// объявлением путей `middleware.LoginLaneRoutes` пробой, а не читается из него
// в момент сбора.
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

// Глаголы формы — закрытый словарь МЕТОК. Это словарь поверхности, а не второе
// объявление путей: пути и их имена объявляет `middleware.LoginLaneRoutes`, а
// сходимость двух словарей держит проба `TestSessionLane_F3_48_VerbLabelsMatchTheDeclaredRoutes`
// — тем же порядком, что у полос решений (`decision*` выше и `AuthzCounts`).
// Коллектор в момент сбора не зовёт НИЧЕГО вне пакета: гейт дерева
// `TestDiagnosticCollectorsDoNotDialOut` судит любой вызов как поход наружу.
const (
	loginLaneVerbLogin    = "login"
	loginLaneVerbLogout   = "logout"
	loginLaneVerbPassword = "password"
	loginLaneVerbCSRF     = "csrf"
	// Регистрация (Ф4, kacho#2699) и восстановление доступа (Ф5, kacho#2701) —
	// те же клетки, что у четырёх глаголов Ф3: словарь поверхности растёт
	// вместе с объявлением путей, и проба сходимости это держит.
	loginLaneVerbRegister         = "register"
	loginLaneVerbRecovery         = "recovery"
	loginLaneVerbRecoveryComplete = "recovery-complete"
	// Второй фактор (Ф12 Р4): шесть глаголов той же полосы.
	loginLaneVerbSecondFactorStatus      = "second-factor-status"
	loginLaneVerbSecondFactorEnroll      = "second-factor-enroll"
	loginLaneVerbSecondFactorConfirm     = "second-factor-confirm"
	loginLaneVerbSecondFactorRemove      = "second-factor-remove"
	loginLaneVerbSecondFactorBackupCodes = "second-factor-backup-codes"
	loginLaneVerbStepUp                  = "step-up"
)

// LoginLaneVerbLabels — значения метки `verb` в порядке объявления; для пробы
// сходимости со словарём путей.
func LoginLaneVerbLabels() []string {
	return []string{
		loginLaneVerbLogin, loginLaneVerbLogout, loginLaneVerbPassword, loginLaneVerbCSRF,
		loginLaneVerbRegister, loginLaneVerbRecovery, loginLaneVerbRecoveryComplete,
		loginLaneVerbSecondFactorStatus, loginLaneVerbSecondFactorEnroll, loginLaneVerbSecondFactorConfirm,
		loginLaneVerbSecondFactorRemove, loginLaneVerbSecondFactorBackupCodes, loginLaneVerbStepUp,
	}
}

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
	sessionAssuranceOffAxisDesc = prometheus.NewDesc(
		"kacho_api_gateway_session_lane_assurance_off_axis_total",
		"Answers about a live session from our identity service whose assurance level is off the "+
			"session axis (absent, \"0\" or a foreign vocabulary such as aal2). Every positive "+
			"authentication floor refuses such a session; a floorless verb passes. Non-zero means a "+
			"service/edge version skew or a direct write into the session store.",
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
	ch <- sessionAssuranceOffAxisDesc
	ch <- loginLaneRelayedDesc
	ch <- loginLaneUnreachableDesc
}

// Collect — ни одного внешнего вызова: `read` возвращает величины, уже лежащие
// в процессе. Глаголы обходятся по закрытому словарю меток, а не по ключам
// снимка: клетка глагола, по которому ретрансляций не было, обязана стоять
// нулём.
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
	ch <- prometheus.MustNewConstMetric(sessionAssuranceOffAxisDesc, prometheus.CounterValue, float64(s.Lane.AssuranceOffAxis))
	for verb, value := range map[string]uint64{
		loginLaneVerbLogin:            s.Relay.Relayed[loginLaneVerbLogin],
		loginLaneVerbLogout:           s.Relay.Relayed[loginLaneVerbLogout],
		loginLaneVerbPassword:         s.Relay.Relayed[loginLaneVerbPassword],
		loginLaneVerbCSRF:             s.Relay.Relayed[loginLaneVerbCSRF],
		loginLaneVerbRegister:         s.Relay.Relayed[loginLaneVerbRegister],
		loginLaneVerbRecovery:         s.Relay.Relayed[loginLaneVerbRecovery],
		loginLaneVerbRecoveryComplete: s.Relay.Relayed[loginLaneVerbRecoveryComplete],
	} {
		ch <- prometheus.MustNewConstMetric(loginLaneRelayedDesc, prometheus.CounterValue, float64(value), verb)
	}
	ch <- prometheus.MustNewConstMetric(loginLaneUnreachableDesc, prometheus.CounterValue, float64(s.Relay.Unreachable))
}
