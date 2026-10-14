// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// logWindowEventsDesc — клетки окон доклада края (kacho#2740). Строка журнала
// раз в окно называет ПЕРВОЕ событие окна и потому не отличает «мигнуло
// однажды» от «держится час»; клетка — итог окна с запуска, и рост её между
// двумя сборами показывает, держится ли состояние.
var logWindowEventsDesc = prometheus.NewDesc(
	"kacho_api_gateway_log_window_events_total",
	"Events counted by the edge's rate-limited log reports since start, by window: "+
		"revocation_record_failure, revocation_no_identifier, revocation_authority_failure, "+
		"session_service_failure, own_assurance_off_axis, basic_assurance_unknown, "+
		"auth_methods_unusable (the authentication layer), auth_methods_unusable_dpop (the DPoP surface). "+
		"The log line names the first event of a window; the growth of this cell shows whether the state persists.",
	[]string{"window"}, nil)

// LogWindowSources — окна доклада, которые читает коллектор: слой
// аутентификации и поверхность DPoP (nil — поверхность не смонтирована).
type LogWindowSources struct {
	Auth middleware.LogWindows
	DPoP middleware.LogWindowTotal
}

// RegisterLogWindows провязывает читателя окон доклада края.
func (m *Metrics) RegisterLogWindows(read func() LogWindowSources) {
	if m == nil || read == nil {
		return
	}
	m.reg.MustRegister(&logWindowCollector{read: read})
}

type logWindowCollector struct {
	read func() LogWindowSources
}

func (c *logWindowCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- logWindowEventsDesc
}

// Метки — закрытый словарь: окно — ключ литерального набора констант.
const (
	windowRecordRevocationFailure    = "revocation_record_failure"
	windowRevocationNoIdentifier     = "revocation_no_identifier"
	windowAuthorityRevocationFailure = "revocation_authority_failure"
	windowSessionServiceFailure      = "session_service_failure"
	windowOwnAssuranceOffAxis        = "own_assurance_off_axis"
	windowBasicAssuranceUnknown      = "basic_assurance_unknown"
	windowAuthMethodsUnusable        = "auth_methods_unusable"
	windowAuthMethodsUnusableDPoP    = "auth_methods_unusable_dpop"
)

// windowTotal — итог окна; окно непровязанной полосы стоит нулём, чтобы ноль
// отличался от «клетки нет».
func windowTotal(w middleware.LogWindowTotal) uint64 {
	if w == nil {
		return 0
	}
	return w.Total()
}

// Collect — ни одного внешнего вызова: итоги окон лежат в процессе.
func (c *logWindowCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.read()
	for window, total := range map[string]middleware.LogWindowTotal{
		windowRecordRevocationFailure:    s.Auth.RecordRevocationFailure,
		windowRevocationNoIdentifier:     s.Auth.RevocationNoIdentifier,
		windowAuthorityRevocationFailure: s.Auth.AuthorityRevocationFailure,
		windowSessionServiceFailure:      s.Auth.SessionServiceFailure,
		windowOwnAssuranceOffAxis:        s.Auth.OwnAssuranceOffAxis,
		windowBasicAssuranceUnknown:      s.Auth.BasicAssuranceUnknown,
		windowAuthMethodsUnusable:        s.Auth.AuthMethodsUnusable,
		windowAuthMethodsUnusableDPoP:    s.DPoP,
	} {
		ch <- prometheus.MustNewConstMetric(logWindowEventsDesc, prometheus.CounterValue,
			float64(windowTotal(total)), window)
	}
}
