// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authz_metrics.go — накопитель решений края: четыре полосы исхода, окно
// вердиктов и длительность решения.
//
// # Что здесь считается и почему именно так
//
// Решение о доступе принимается на крае один раз за запрос, и у него ровно
// четыре исхода. Три из них раньше сливались в один счётчик «ошибка», а
// четвёртого не было вовсе:
//
//   - разрешено;
//   - отказано;
//   - проверка не состоялась и запрос ОТКЛОНЁН (fail-closed);
//   - проверка не состоялась и запрос ПРОПУЩЕН (объявленный мягкий проход).
//
// Последние две — противоположные вещи, и по одному числу их не различить.
// `security.md` §Hardening-инвариант 8(б) требует, чтобы мягкий проход нёс
// СВОЙ счётчик: иначе он невидим, и «контроль ни разу не отказал» читается
// одинаково при исправном контроле и при контроле, который всех пропускает.
//
// # Единицы — базовые
//
// Длительность наблюдается в СЕКУНДАХ, а не в миллисекундах: платформенный
// словарь измеряет время в базовых единицах, и отвечающая сторона того же ребра
// (`kacho_iam_authz_check_duration_seconds`) уже так и делает. Переименование
// сегодня бесплатно — серии края ещё не читал никто; завтра оно стало бы
// ломающим изменением для панелей и правил тревог.
//
// # Здесь НЕТ производной доли попаданий
//
// Доля, посчитанная в процессе за всё время жизни, на поверхности сбора
// бесполезна: её нельзя ни продифференцировать по времени, ни сложить по
// репликам. Потребитель считает её из попаданий и промахов сам, а второе место
// об одном предмете разъезжается молча.
//
// # Читатель
//
// Величины читает коллектор `gateway/internal/observability/metrics`,
// зарегистрированный композиционным корнем на диагностической поверхности.
// Свойство «читатель есть» держит гейт дерева `internal/repohygiene`
// `TestDeclaredAccumulatorsHaveANonTestReader`: провязка наблюдаемости
// nil-безопасна по построению, поэтому её пропажу не поймает ни компилятор, ни
// проба самого накопителя.
package middleware

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultCheckDurationBuckets — верхние границы корзин длительности решения, В
// СЕКУНДАХ и по возрастанию.
//
// Диапазон выбран под предмет: решение края — это один вызов к владельцу прав
// под сроком в сотни миллисекунд, поэтому интерес лежит между миллисекундой и
// секундой. Верхняя корзина здесь не «потолок», а граница, за которой всё равно
// сработает срок вызова.
var DefaultCheckDurationBuckets = []float64{0.001, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1}

// AuthzCounts — ПРОЧИТАННЫЕ величины накопителя.
//
// Именно прочитанные: по отсутствию строки на поверхности «события не было» и
// «счётчика нет» неразличимы, а прочитанный ноль их различает. Тип, а не карта
// заранее собранных имён: имена серий — предмет коллектора, и накопителю о них
// знать нечего.
type AuthzCounts struct {
	// Allowed — решений «разрешено».
	Allowed uint64
	// Denied — решений «отказано» (включая отказ, пришедший от владельца прав
	// кодом отказа в правах).
	Denied uint64
	// ErrorRefused — проверка не состоялась, и запрос ОТКЛОНЁН.
	ErrorRefused uint64
	// ErrorPassed — проверка не состоялась, и запрос ПРОПУЩЕН объявленным мягким
	// проходом. Отдельная полоса ровно потому, что это противоположный исход.
	ErrorPassed uint64

	// CacheHits / CacheMisses — окно вердиктов. Их отношение к числу обращений к
	// владельцу прав и есть ответ на вопрос, работает ли окно.
	CacheHits   uint64
	CacheMisses uint64

	// DurationBuckets — верхние границы корзин (секунды, по возрастанию).
	DurationBuckets []float64
	// DurationCounts — число наблюдений В КАЖДОЙ корзине; длина на единицу
	// больше числа границ (последний элемент — корзина «+Inf»). Кумулятивную
	// форму, которой требует формат экспозиции, собирает коллектор: складывать
	// здесь значило бы отдавать наружу величину, из которой исходную уже не
	// восстановить.
	DurationCounts []uint64
	// DurationSum — сумма наблюдений (секунды).
	DurationSum float64
	// DurationCount — число наблюдений. Отличает «решений не было» от «не
	// считали».
	DurationCount uint64
}

// AuthzMetrics — накопитель решений края. Безопасен для конкурентного
// использования.
type AuthzMetrics struct {
	allowedTotal      atomic.Uint64
	deniedTotal       atomic.Uint64
	errorRefusedTotal atomic.Uint64
	errorPassedTotal  atomic.Uint64

	cacheHitTotal  atomic.Uint64
	cacheMissTotal atomic.Uint64

	durationMu      sync.Mutex
	durationBuckets []float64 // верхние границы, секунды, по возрастанию
	durationCounts  []uint64  // по корзинам; последняя — +Inf
	durationSum     float64
	durationCount   uint64
}

// NewAuthzMetrics собирает накопитель с корзинами по умолчанию.
func NewAuthzMetrics() *AuthzMetrics {
	return NewAuthzMetricsWithBuckets(DefaultCheckDurationBuckets)
}

// NewAuthzMetricsWithBuckets позволяет задать свои границы корзин (секунды).
// Границы обязаны строго возрастать и быть положительными; иное — умолчание.
func NewAuthzMetricsWithBuckets(buckets []float64) *AuthzMetrics {
	if !ascendingPositive(buckets) {
		buckets = DefaultCheckDurationBuckets
	}
	cp := make([]float64, len(buckets))
	copy(cp, buckets)
	return &AuthzMetrics{
		durationBuckets: cp,
		durationCounts:  make([]uint64, len(cp)+1),
	}
}

// RecordAllow — решение «разрешено».
func (m *AuthzMetrics) RecordAllow() { m.allowedTotal.Add(1) }

// RecordDeny — решение «отказано».
func (m *AuthzMetrics) RecordDeny() { m.deniedTotal.Add(1) }

// RecordErrorRefused — проверка не состоялась, запрос ОТКЛОНЁН.
//
// Зовётся ТАМ, ГДЕ ИСХОД ИЗВЕСТЕН, — в звене, которое отклоняет, а не в
// функции, принимающей решение: последняя не знает, чем кончится для запроса
// несостоявшаяся проверка, и одна общая запись оттуда снова слила бы два
// противоположных исхода в одно число.
func (m *AuthzMetrics) RecordErrorRefused() { m.errorRefusedTotal.Add(1) }

// RecordErrorPassed — проверка не состоялась, запрос ПРОПУЩЕН объявленным
// мягким проходом. См. [AuthzMetrics.RecordErrorRefused] о месте вызова.
func (m *AuthzMetrics) RecordErrorPassed() { m.errorPassedTotal.Add(1) }

// RecordCacheHit / RecordCacheMiss — окно вердиктов.
func (m *AuthzMetrics) RecordCacheHit()  { m.cacheHitTotal.Add(1) }
func (m *AuthzMetrics) RecordCacheMiss() { m.cacheMissTotal.Add(1) }

// ObserveCheckDuration добавляет наблюдение длительности решения.
//
// Принимает [time.Duration], а не число: единица измерения тогда не бывает
// перепутана вызывающим — а именно так и появилось прежнее имя серии,
// оканчивавшееся на `_latency_ms`.
func (m *AuthzMetrics) ObserveCheckDuration(d time.Duration) {
	seconds := d.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	m.durationMu.Lock()
	defer m.durationMu.Unlock()
	idx := sort.SearchFloat64s(m.durationBuckets, seconds)
	if idx < len(m.durationBuckets) {
		m.durationCounts[idx]++
	} else {
		m.durationCounts[len(m.durationBuckets)]++
	}
	m.durationSum += seconds
	m.durationCount++
}

// Counts — прочитанные величины (см. [AuthzCounts]).
//
// Единственный способ достать их наружу. Nil-получатель отдаёт нулевые
// величины С ОБЪЯВЛЕННЫМИ корзинами: коллектор, собранный на посадке без
// накопителя, обязан отдать те же серии нулями, а не исчезнуть — иначе
// «поверхность есть, серий нет» снова стало бы неотличимо от «событий не было».
func (m *AuthzMetrics) Counts() AuthzCounts {
	if m == nil {
		return AuthzCounts{
			DurationBuckets: append([]float64(nil), DefaultCheckDurationBuckets...),
			DurationCounts:  make([]uint64, len(DefaultCheckDurationBuckets)+1),
		}
	}
	out := AuthzCounts{
		Allowed:      m.allowedTotal.Load(),
		Denied:       m.deniedTotal.Load(),
		ErrorRefused: m.errorRefusedTotal.Load(),
		ErrorPassed:  m.errorPassedTotal.Load(),
		CacheHits:    m.cacheHitTotal.Load(),
		CacheMisses:  m.cacheMissTotal.Load(),
	}
	m.durationMu.Lock()
	out.DurationBuckets = append([]float64(nil), m.durationBuckets...)
	out.DurationCounts = append([]uint64(nil), m.durationCounts...)
	out.DurationSum = m.durationSum
	out.DurationCount = m.durationCount
	m.durationMu.Unlock()
	return out
}

// ascendingPositive — строго возрастающие положительные границы.
func ascendingPositive(b []float64) bool {
	if len(b) == 0 {
		return false
	}
	prev := -1.0
	for _, v := range b {
		if v <= prev || v <= 0 {
			return false
		}
		prev = v
	}
	return true
}
