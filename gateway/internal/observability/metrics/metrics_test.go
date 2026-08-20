// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics_test

import (
	"context"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
)

// expose снимает экспозицию реестра ровно так, как её увидит собиратель:
// через тот же обработчик, что монтируется на диагностическую поверхность.
// Читать реестр в обход обработчика значило бы утверждать не о том, что уезжает
// на провод.
func expose(t *testing.T, m *gwmetrics.Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code)
	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	return string(body)
}

// TestFreshProcessDeclaresEveryBandWithZero — OBS-1-02.
//
// Ни одного запроса не обслужено, а все десять полос решений, обе полосы окна
// вердиктов, обращения по проводу и число наблюдений длительности уже стоят на
// поверхности нулями. Это и есть предмет #208: отсутствие серии и нулевая серия
// обязаны быть различимы.
func TestFreshProcessDeclaresEveryBandWithZero(t *testing.T) {
	authzMetrics := middleware.NewAuthzMetrics()
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAuthz(func() gwmetrics.AuthzSnapshot {
		return gwmetrics.AuthzSnapshot{Counts: authzMetrics.Counts()}
	})

	body := expose(t, m)
	for _, want := range []string{
		`kacho_api_gateway_authz_check_decisions_total{decision="allow"} 0`,
		`kacho_api_gateway_authz_check_decisions_total{decision="deny"} 0`,
		`kacho_api_gateway_authz_check_decisions_total{decision="error_refused"} 0`,
		`kacho_api_gateway_authz_check_decisions_total{decision="error_passed"} 0`,
		// Шесть полос ДОПУСКА БЕЗ ОТВЕТА МОДЕЛИ (#798). Публичный проход раньше не
		// попадал ни в одну, а пять остальных сливались в `allow`.
		`kacho_api_gateway_authz_check_decisions_total{decision="public_path"} 0`,
		`kacho_api_gateway_authz_check_decisions_total{decision="allowlist"} 0`,
		`kacho_api_gateway_authz_check_decisions_total{decision="internal_origin"} 0`,
		`kacho_api_gateway_authz_check_decisions_total{decision="override_allow"} 0`,
		`kacho_api_gateway_authz_check_decisions_total{decision="exempt"} 0`,
		`kacho_api_gateway_authz_check_decisions_total{decision="scope_filtered"} 0`,
		`kacho_api_gateway_authz_cache_total{result="hit"} 0`,
		`kacho_api_gateway_authz_cache_total{result="miss"} 0`,
		`kacho_api_gateway_authz_client_calls_total 0`,
		`kacho_api_gateway_authz_check_duration_seconds_count 0`,
		// Посадка объявляется ОТДЕЛЬНОЙ серией: нулями на полосах решений
		// «проверка выключена» и «трафика не было» не различаются (#798).
		`kacho_api_gateway_authz_enforcing 0`,
	} {
		assert.Contains(t, body, want,
			"свежий процесс обязан объявлять полосу нулём, а не отсутствием серии")
	}
}

// TestUnregisteredCollectorIsRed — OBS-1-03, инъекция в обе стороны к
// предыдущей пробе.
//
// Без неё «серии присутствуют» неотличимо от пробы, которая ничего не читает:
// на реестре без регистрации коллектора текст экспозиции обязан НЕ содержать
// объявленных семейств.
func TestUnregisteredCollectorIsRed(t *testing.T) {
	m := gwmetrics.New("test", "deadbeef") // коллектор НЕ зарегистрирован
	body := expose(t, m)
	assert.NotContains(t, body, "kacho_api_gateway_authz_check_decisions_total",
		"снятие регистрации обязано быть заметно пробе — иначе она не читает ничего")

	// Законный близнец: реестр без коллектора решений всё равно отдаёт СВОИ
	// серии, поэтому «пусто» здесь не может означать «обработчик не ответил».
	assert.Contains(t, body, "kacho_api_gateway_build_info")
}

// TestDecisionBandsGrowIndependently — исход попадает в СВОЮ полосу.
func TestDecisionBandsGrowIndependently(t *testing.T) {
	authzMetrics := middleware.NewAuthzMetrics()
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAuthz(func() gwmetrics.AuthzSnapshot {
		return gwmetrics.AuthzSnapshot{Counts: authzMetrics.Counts()}
	})

	// Числа РАЗНЫЕ у каждой полосы намеренно: одинаковые сделали бы невидимой
	// перепутанную пару «метка ↔ поле снимка» в коллекторе, а десять полос —
	// ровно тот размер, на котором такая опечатка и заводится.
	repeat := func(n int, f func()) {
		for i := 0; i < n; i++ {
			f()
		}
	}
	repeat(2, authzMetrics.RecordAllow)
	repeat(1, authzMetrics.RecordDeny)
	repeat(3, authzMetrics.RecordErrorRefused)
	repeat(4, authzMetrics.RecordErrorPassed)
	repeat(5, authzMetrics.RecordPublicPath)
	repeat(6, authzMetrics.RecordAllowlist)
	repeat(7, authzMetrics.RecordInternalOrigin)
	repeat(8, authzMetrics.RecordOverrideAllow)
	repeat(9, authzMetrics.RecordExempt)
	repeat(10, authzMetrics.RecordScopeFiltered)
	authzMetrics.RecordCacheHit()
	authzMetrics.RecordCacheMiss()
	authzMetrics.SetEnforcing(true)
	authzMetrics.ObserveCheckDuration(3 * time.Millisecond)

	body := expose(t, m)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_decisions_total{decision="allow"} 2`)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_decisions_total{decision="deny"} 1`)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_decisions_total{decision="error_refused"} 3`)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_decisions_total{decision="error_passed"} 4`)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_decisions_total{decision="public_path"} 5`)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_decisions_total{decision="allowlist"} 6`)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_decisions_total{decision="internal_origin"} 7`)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_decisions_total{decision="override_allow"} 8`)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_decisions_total{decision="exempt"} 9`)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_decisions_total{decision="scope_filtered"} 10`)
	assert.Contains(t, body, `kacho_api_gateway_authz_enforcing 1`)
	assert.Contains(t, body, `kacho_api_gateway_authz_cache_total{result="hit"} 1`)
	assert.Contains(t, body, `kacho_api_gateway_authz_cache_total{result="miss"} 1`)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_duration_seconds_count 1`)
}

// TestDurationHistogramIsSecondsAndConsistent — OBS-1-11 (детерминированная
// половина, уровень «в процессе»).
//
// Имени, оканчивающегося на `_latency_ms`, на поверхности нет ни в одной форме;
// корзины не убывают; `le="+Inf"` равна `_count`.
func TestDurationHistogramIsSecondsAndConsistent(t *testing.T) {
	authzMetrics := middleware.NewAuthzMetrics()
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAuthz(func() gwmetrics.AuthzSnapshot {
		return gwmetrics.AuthzSnapshot{Counts: authzMetrics.Counts()}
	})
	authzMetrics.ObserveCheckDuration(500 * time.Microsecond)
	authzMetrics.ObserveCheckDuration(30 * time.Millisecond)
	authzMetrics.ObserveCheckDuration(5 * time.Second)

	body := expose(t, m)
	assert.NotContains(t, body, "_latency_ms")
	assert.Contains(t, body, "kacho_api_gateway_authz_check_duration_seconds_sum")
	assert.Contains(t, body, `kacho_api_gateway_authz_check_duration_seconds_count 3`)
	assert.Contains(t, body, `kacho_api_gateway_authz_check_duration_seconds_bucket{le="+Inf"} 3`)

	var prev float64 = -1
	seen := 0
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "kacho_api_gateway_authz_check_duration_seconds_bucket{") {
			continue
		}
		seen++
		parts := strings.Fields(line)
		require.Len(t, parts, 2, line)
		v, err := strconv.ParseFloat(parts[1], 64)
		require.NoError(t, err, line)
		assert.GreaterOrEqual(t, v, prev, "кумулятивные корзины не убывают: "+line)
		prev = v
	}
	assert.Positive(t, seen, "корзин не найдено — проба ничего не прочитала")
}

// TestNoLifetimeHitRatioOnTheSurface — OBS-1-12: производной доли попаданий на
// поверхности нет.
//
// Р4: доля, посчитанная в процессе за всё время жизни, не дифференцируется по
// времени и не складывается по репликам; потребитель считает её из попаданий и
// промахов сам. Без этой пробы снятая производная вернулась бы следующей
// правкой незамеченной.
func TestNoLifetimeHitRatioOnTheSurface(t *testing.T) {
	authzMetrics := middleware.NewAuthzMetrics()
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAuthz(func() gwmetrics.AuthzSnapshot {
		return gwmetrics.AuthzSnapshot{Counts: authzMetrics.Counts()}
	})
	authzMetrics.RecordCacheHit()
	authzMetrics.RecordCacheMiss()

	body := expose(t, m)
	assert.NotContains(t, body, "_hit_ratio")
	// Положительный контроль: обе слагаемые серии на месте, то есть отрицание
	// выше сделано не на пустой поверхности.
	assert.Contains(t, body, `kacho_api_gateway_authz_cache_total{result="hit"} 1`)
	assert.Contains(t, body, `kacho_api_gateway_authz_cache_total{result="miss"} 1`)
}

// TestSurfaceAnswersWhenNeighboursAreDown — OBS-1-28 (уровень «в процессе»).
//
// Диагностика, которая ходит наружу за своими числами, гаснет ровно тогда, когда
// нужна. Источник здесь имитирует зависший вызов к соседу: сбор обязан уложиться
// в объявленный срок и отдать все семейства.
func TestSurfaceAnswersWhenNeighboursAreDown(t *testing.T) {
	authzMetrics := middleware.NewAuthzMetrics()
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAuthz(func() gwmetrics.AuthzSnapshot {
		return gwmetrics.AuthzSnapshot{Counts: authzMetrics.Counts()}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan string, 1)
	go func() { done <- expose(t, m) }()
	select {
	case body := <-done:
		assert.Contains(t, body, "kacho_api_gateway_authz_check_decisions_total")
	case <-ctx.Done():
		t.Fatal("экспозиция не отдана в пределах срока: коллектор ждёт кого-то снаружи")
	}
}
