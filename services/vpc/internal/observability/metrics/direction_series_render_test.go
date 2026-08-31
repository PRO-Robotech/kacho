// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics_test

// direction_series_render_test.go — ряды по направлению обязаны РЕНДЕРИТЬСЯ, а не просто
// компилироваться.
//
// Collector спрашивает способность приёмника во время работы (`rec.(DirectionRecorder)`),
// поэтому потеря этих методов при рефакторинге не сломала бы сборку — она молча перестала
// бы публиковать единственные ряды, по которым видно, доезжает ли снятие вообще. Проба
// проверяет обе половины сразу: способность признаётся И имена появляются в выдаче.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	outboxmetrics "github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/observability/metrics"
)

func TestDirectionSeries_AreAcceptedAndRendered(t *testing.T) {
	m := metrics.New("test", "abc")

	// Способность, которую Collector спрашивает во время работы.
	rec, ok := any(m).(outboxmetrics.DirectionRecorder)
	require.True(t, ok, "адаптер обязан удовлетворять DirectionRecorder — иначе Collector "+
		"молча не опубликует ни одного ряда по направлению")

	const tbl = "kacho_vpc.fga_register_outbox"
	rec.SetBacklogDepthByDirection(tbl, outboxmetrics.DirectionWithdrawal, 3)
	rec.SetOldestPendingAgeByDirection(tbl, outboxmetrics.DirectionWithdrawal, 42)
	// Ноль доставленных — то самое состояние, ради видимости которого ряд заведён.
	//
	// Величина стала СЧЁТЧИКОМ (#1714), и дочерняя серия счётчика появляется лишь
	// после первого инкремента. Поэтому ряд заводится ЯВНО, и утверждение ниже —
	// про то, что «ни одного отзыва не доставлено» видно ЧИСЛОМ, а не отсутствием
	// ряда. Инкремента здесь нет намеренно: проверяется именно нулевое состояние.
	rec.InitDeliveredByDirection(tbl, outboxmetrics.DirectionWithdrawal)

	rr := httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rr.Body.String()

	for _, want := range []string{
		`kacho_vpc_outbox_backlog_depth_by_direction{direction="withdrawal",table="` + tbl + `"} 3`,
		`kacho_vpc_outbox_oldest_pending_age_seconds_by_direction{direction="withdrawal",table="` + tbl + `"} 42`,
		`kacho_vpc_outbox_delivered_by_direction{direction="withdrawal",table="` + tbl + `"} 0`,
	} {
		require.Contains(t, body, want,
			"ряд обязан присутствовать в выдаче /metrics; «ноль доставленных снятий» "+
				"наблюдаем только если ряд существует")
	}
}
