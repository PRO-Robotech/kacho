// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
)

// TestSessionLane_F3_48_EveryCellExistsWithZeroBeforeTheFirstEvent — клетки
// счётчиков края (Ф3-48): отказ по отсечке · «сессии нет» · недоступность ·
// окно раската · ретрансляция по глаголу · служба недостижима — все существуют
// с нулём в только что поднятом процессе; после одного события ровно одна
// клетка выросла на единицу.
func TestSessionLane_F3_48_EveryCellExistsWithZeroBeforeTheFirstEvent(t *testing.T) {
	lane := middleware.SessionLaneSnapshot{}
	relay := handler.LoginLaneRelaySnapshot{Relayed: map[string]uint64{}}
	for _, rt := range middleware.LoginLaneRoutes() {
		relay.Relayed[rt.Verb] = 0
	}
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterSessionLane(func() gwmetrics.SessionLaneSnapshot {
		return gwmetrics.SessionLaneSnapshot{Lane: lane, Relay: relay}
	})
	body := expose(t, m)

	zeroes := []string{
		`kacho_api_gateway_session_lane_refusals_total{outcome="cutoff_denied"} 0`,
		`kacho_api_gateway_session_lane_refusals_total{outcome="no_session"} 0`,
		`kacho_api_gateway_session_lane_refusals_total{outcome="unavailable"} 0`,
		`kacho_api_gateway_session_lane_rollout_window_total 0`,
		`kacho_api_gateway_login_lane_relayed_total{verb="login"} 0`,
		`kacho_api_gateway_login_lane_relayed_total{verb="logout"} 0`,
		`kacho_api_gateway_login_lane_relayed_total{verb="password"} 0`,
		`kacho_api_gateway_login_lane_relayed_total{verb="csrf"} 0`,
		`kacho_api_gateway_login_lane_unreachable_total 0`,
	}
	for _, line := range zeroes {
		assert.Contains(t, body, line, "клетка обязана существовать с нулём до первого события")
	}
	t.Logf("перепись: клеток объявлено %d · на поверхности с нулём %d", len(zeroes), countPresent(body, zeroes))

	// Положительный контроль: ровно одна клетка выросла на единицу.
	lane.CutoffDenied = 1
	body = expose(t, m)
	require.Contains(t, body, `kacho_api_gateway_session_lane_refusals_total{outcome="cutoff_denied"} 1`)
	for _, line := range zeroes[1:] {
		assert.Contains(t, body, line, "соседняя клетка не должна была вырасти")
	}
	relay.Relayed["logout"] = 1
	body = expose(t, m)
	require.Contains(t, body, `kacho_api_gateway_login_lane_relayed_total{verb="logout"} 1`)
	require.Contains(t, body, `kacho_api_gateway_login_lane_relayed_total{verb="login"} 0`)
}

func countPresent(body string, lines []string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(body, l) {
			n++
		}
	}
	return n
}

// Словарь меток `verb` сходится с объявлением путей формы — в обе стороны.
// Второе объявление глаголов здесь есть словарь ПОВЕРХНОСТИ (тот же порядок,
// что у полос решений), и расхождение с объявлением путей обязано краснеть.
func TestSessionLane_F3_48_VerbLabelsMatchTheDeclaredRoutes(t *testing.T) {
	labels := gwmetrics.LoginLaneVerbLabels()
	routes := middleware.LoginLaneRoutes()
	require.Equal(t, len(routes), len(labels), "число меток и число глаголов формы")
	for i, rt := range routes {
		require.Equal(t, rt.Verb, labels[i], "метка %d расходится с объявлением пути %s", i, rt.Path)
	}
	t.Logf("перепись: глаголов объявлено %d · меток %d · сошлись %d", len(routes), len(labels), len(routes))
}

// Клетка отказа по требованию смены пароля — в семействе решений (Ф3-23, Ф3-48).
func TestSessionLane_F3_48_PasswordChangeRequiredIsADecisionCell(t *testing.T) {
	authz := middleware.NewAuthzMetrics()
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAuthz(func() gwmetrics.AuthzSnapshot { return gwmetrics.AuthzSnapshot{Counts: authz.Counts()} })
	require.Contains(t, expose(t, m), `kacho_api_gateway_authz_check_decisions_total{decision="password_change_required"} 0`)
	authz.RecordPasswordChangeRequired()
	require.Contains(t, expose(t, m), `kacho_api_gateway_authz_check_decisions_total{decision="password_change_required"} 1`)
}

// Клетка «уровень вне оси сессии» (Ф11-19): существует с нулём до первого
// ответа службы без уровня и растёт на единицу с каждым таким ответом.
// Отдельная клетка, а не исход отказа: на глаголе без пола такой ответ
// ПРОХОДИТ, и отказом он не является — состояние докладывается само по себе.
func TestSessionLane_F11_19_OffAxisAssuranceCellExistsWithZeroAndGrows(t *testing.T) {
	lane := middleware.SessionLaneSnapshot{}
	relay := handler.LoginLaneRelaySnapshot{Relayed: map[string]uint64{}}
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterSessionLane(func() gwmetrics.SessionLaneSnapshot {
		return gwmetrics.SessionLaneSnapshot{Lane: lane, Relay: relay}
	})
	require.Contains(t, expose(t, m), `kacho_api_gateway_session_lane_assurance_off_axis_total 0`)
	lane.AssuranceOffAxis = 1
	body := expose(t, m)
	require.Contains(t, body, `kacho_api_gateway_session_lane_assurance_off_axis_total 1`)
	require.Contains(t, body, `kacho_api_gateway_session_lane_refusals_total{outcome="no_session"} 0`,
		"соседняя клетка не должна была вырасти")
}
