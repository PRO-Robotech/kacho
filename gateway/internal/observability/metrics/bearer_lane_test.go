// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
)

// kacho#2740 — на КАЖДОЕ состояние полосы предъявителя есть клетка на приборе,
// с нулём до первого события. Перечень состояний выводится из кода
// (middleware.BearerLaneStates — те же исходы, что ветки revocationCheck), а не
// выписывается здесь.
func TestBearerLane_EveryStateHasACellWithZeroBeforeTheFirstEvent(t *testing.T) {
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterBearerLane(func() middleware.BearerLaneSnapshot { return middleware.BearerLaneSnapshot{} })
	body := expose(t, m)
	states := middleware.BearerLaneStates()
	if len(states) == 0 {
		t.Fatal("состояний полосы предъявителя 0 — перечень не выведен")
	}
	present := 0
	for _, s := range states {
		line := `kacho_api_gateway_bearer_lane_revocation_total{outcome="` + s.Outcome + `",source="` + s.Source + `"} 0`
		if assert.Contains(t, body, line, "клетка состояния обязана существовать с нулём") {
			present++
		}
	}
	t.Logf("перепись: состояний полосы %d · клеток на поверхности %d", len(states), present)
}

// Величина клетки — величина накопителя полосы: событие, записанное полосой,
// видно на приборе (а не только строкой журнала раз в окно).
func TestBearerLane_ACellCarriesTheLaneValue(t *testing.T) {
	m := gwmetrics.New("test", "deadbeef")
	snap := middleware.BearerLaneSnapshot{}
	states := middleware.BearerLaneStates()
	snap.Set(states[0], 7)
	m.RegisterBearerLane(func() middleware.BearerLaneSnapshot { return snap })
	body := expose(t, m)
	want := `kacho_api_gateway_bearer_lane_revocation_total{outcome="` + states[0].Outcome + `",source="` + states[0].Source + `"} 7`
	if !strings.Contains(body, want) {
		t.Fatalf("величина полосы не дошла до прибора: ожидалась строка %q", want)
	}
}
