// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// TestAuthzMetrics_FourDecisionBands — полосы ПРИНЯТОГО решения считаются
// РАЗДЕЛЬНО. Полосы допуска без ответа модели (#798) проверяет
// authz_admission_lane_test.go — там их предмет.
//
// Полоса «проверка не состоялась» была одна на два противоположных исхода, и по
// её значению нельзя было сказать, отклонил контроль запрос или пропустил.
func TestAuthzMetrics_FourDecisionBands(t *testing.T) {
	m := middleware.NewAuthzMetrics()
	m.RecordAllow()
	m.RecordAllow()
	m.RecordDeny()
	m.RecordErrorRefused()
	m.RecordErrorPassed()
	m.RecordErrorPassed()

	c := m.Counts()
	assert.Equal(t, uint64(2), c.Allowed)
	assert.Equal(t, uint64(1), c.Denied)
	assert.Equal(t, uint64(1), c.ErrorRefused)
	assert.Equal(t, uint64(2), c.ErrorPassed)
}

// TestAuthzMetrics_FreshCarrierReadsZeros — свежий накопитель отдаёт ЧИТАЕМЫЕ
// нули по всем полосам, а не отсутствие величин.
func TestAuthzMetrics_FreshCarrierReadsZeros(t *testing.T) {
	c := middleware.NewAuthzMetrics().Counts()
	assert.Equal(t, uint64(0), c.Allowed)
	assert.Equal(t, uint64(0), c.Denied)
	assert.Equal(t, uint64(0), c.ErrorRefused)
	assert.Equal(t, uint64(0), c.ErrorPassed)
	assert.Equal(t, uint64(0), c.CacheHits)
	assert.Equal(t, uint64(0), c.CacheMisses)
	assert.Equal(t, uint64(0), c.DurationCount)
	require.NotEmpty(t, c.DurationBuckets, "границы корзин обязаны быть объявлены и на свежем накопителе")
	assert.Len(t, c.DurationCounts, len(c.DurationBuckets)+1, "последняя корзина — +Inf")
}

// TestAuthzMetrics_NilCarrierStillDeclaresBands — накопителя нет, а корзины и
// нули есть.
//
// Коллектор, собранный на посадке без накопителя, обязан отдать те же серии
// нулями: иначе «поверхность есть, серий нет» снова стало бы неотличимо от
// «событий не было».
func TestAuthzMetrics_NilCarrierStillDeclaresBands(t *testing.T) {
	var m *middleware.AuthzMetrics
	c := m.Counts()
	assert.Equal(t, uint64(0), c.DurationCount)
	assert.Equal(t, middleware.DefaultCheckDurationBuckets, c.DurationBuckets)
	assert.Len(t, c.DurationCounts, len(middleware.DefaultCheckDurationBuckets)+1)
}

// TestAuthzMetrics_DurationIsSeconds — длительность наблюдается в БАЗОВЫХ
// единицах, и корзины попадают туда, куда положено.
func TestAuthzMetrics_DurationIsSeconds(t *testing.T) {
	m := middleware.NewAuthzMetricsWithBuckets([]float64{0.001, 0.01, 0.1})
	m.ObserveCheckDuration(500 * time.Microsecond) // 0.0005 s → корзина ≤0.001
	m.ObserveCheckDuration(5 * time.Millisecond)   // 0.005  s → корзина ≤0.01
	m.ObserveCheckDuration(50 * time.Millisecond)  // 0.05   s → корзина ≤0.1
	m.ObserveCheckDuration(2 * time.Second)        // → +Inf

	c := m.Counts()
	require.Len(t, c.DurationCounts, 4)
	assert.Equal(t, uint64(1), c.DurationCounts[0])
	assert.Equal(t, uint64(1), c.DurationCounts[1])
	assert.Equal(t, uint64(1), c.DurationCounts[2])
	assert.Equal(t, uint64(1), c.DurationCounts[3], "наблюдение сверх последней границы — в +Inf")
	assert.Equal(t, uint64(4), c.DurationCount)
	assert.InDelta(t, 0.0005+0.005+0.05+2.0, c.DurationSum, 1e-9)
}

// TestAuthzMetrics_BadBucketsFallBackToDefault — невозрастающие границы
// отвергаются целиком, а не применяются частично.
func TestAuthzMetrics_BadBucketsFallBackToDefault(t *testing.T) {
	m := middleware.NewAuthzMetricsWithBuckets([]float64{0.005, 0.002, 0.01})
	assert.Equal(t, middleware.DefaultCheckDurationBuckets, m.Counts().DurationBuckets)
}

// TestAuthzMetrics_NegativeDurationClamped — отрицательная длительность
// (часы прыгнули назад) не уезжает в чужую корзину.
func TestAuthzMetrics_NegativeDurationClamped(t *testing.T) {
	m := middleware.NewAuthzMetrics()
	m.ObserveCheckDuration(-5 * time.Second)
	c := m.Counts()
	assert.Equal(t, uint64(1), c.DurationCounts[0])
	assert.Equal(t, 0.0, c.DurationSum)
}

// TestAuthzMetrics_ConcurrentSafe — накопитель считает под конкуренцией.
func TestAuthzMetrics_ConcurrentSafe(t *testing.T) {
	m := middleware.NewAuthzMetrics()
	const goroutines = 16
	const each = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				m.RecordAllow()
				m.ObserveCheckDuration(time.Duration(i) * time.Millisecond)
			}
		}()
	}
	wg.Wait()
	c := m.Counts()
	assert.Equal(t, uint64(goroutines*each), c.Allowed)
	assert.Equal(t, uint64(goroutines*each), c.DurationCount)
}
