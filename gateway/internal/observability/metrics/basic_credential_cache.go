// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// basic_credential_cache.go — заполнение кэша вердиктов базовой полосы на
// диагностической поверхности (#1221).
//
// # Почему серия, а если объявление при старте и однократная запись уже есть
//
// Объявление потолка при старте и однократное предупреждение при его достижении
// отвечают на вопрос «дошли ли» и НИ ОДИН — на вопрос «насколько близко и как
// быстро растём». Между тем решение, ради которого наблюдение и заводят
// (делать ли потолок ручкой), принимается ровно по второму: потолок,
// достигнутый раз в сутки, и потолок, перемалывающий сотню записей в минуту,
// требуют разного, а однократная запись в журнале у них ОДНА И ТА ЖЕ.
//
// # Почему три величины, а не одна
//
// Занятость без потолка — число без шкалы: «сто записей» не говорит ничего, пока
// неизвестно, сто из ста двадцати это или сто из миллиона. Потолок без
// занятости — шкала без числа. Вытеснения — единственная из трёх, которая
// МОНОТОННА, поэтому только по ней берётся производная, то есть скорость;
// занятость колеблется и производной не имеет смысла.
//
// Защёлка «потолок достигался» СВОЕЙ серии не получает намеренно: она выводится
// из вытеснений (их рост означает достижение) и уже доходит до оператора
// записью в журнал. Четвёртая серия была бы вторым местом об одном предмете.

var (
	verdictCacheEntriesDesc = prometheus.NewDesc(
		"kacho_api_gateway_basic_credential_verdict_cache_entries",
		"Live verdicts held by the basic-credential lane cache. "+
			"Read against the capacity gauge: entries alone are a number without a scale.",
		nil, nil)
	verdictCacheCapacityDesc = prometheus.NewDesc(
		"kacho_api_gateway_basic_credential_verdict_cache_capacity",
		"Ceiling on the basic-credential verdict cache, in entries. "+
			"A compile-time constant today; this series is the observation a knob decision would be made on.",
		nil, nil)
	verdictCacheEvictionsDesc = prometheus.NewDesc(
		"kacho_api_gateway_basic_credential_verdict_cache_evictions_total",
		"Verdicts dropped BECAUSE THE CEILING WAS FULL, since process start. "+
			"Window turnover is NOT counted. Its derivative over time is the fill rate; "+
			"an evicted credential is re-checked against the authority, never waved through.",
		nil, nil)
)

// RegisterBasicCredentialCache провязывает читателя заполнения кэша вердиктов.
//
// Функция, а не носитель: полоса собирается композиционным корнем задолго до
// диагностической поверхности, и снимок — единственная форма, в которой её
// величины приходят сюда.
//
// nil-безопасна: сбор величин не имеет права ронять подъём края. Именно поэтому
// её пропажу не поймает компилятор, и свойство «читатель есть» держит гейт
// дерева `internal/repohygiene` `TestDeclaredAccumulatorsHaveANonTestReader`.
func (m *Metrics) RegisterBasicCredentialCache(read func() middleware.BasicCredentialCacheStats) {
	if m == nil || read == nil {
		return
	}
	m.reg.MustRegister(&verdictCacheCollector{read: read})
}

// verdictCacheCollector читает величины ИЗ ПРОЦЕССА: ни одного внешнего вызова в
// момент сбора — диагностика, которая ходит наружу за своими числами, гаснет
// ровно тогда, когда нужна.
type verdictCacheCollector struct {
	read func() middleware.BasicCredentialCacheStats
}

// Describe объявляет все три семейства ДО первого сбора: серии стоят нулями с
// первой секунды жизни процесса, поэтому «кэш пуст» и «коллектора нет»
// различимы без единого запроса.
func (c *verdictCacheCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- verdictCacheEntriesDesc
	ch <- verdictCacheCapacityDesc
	ch <- verdictCacheEvictionsDesc
}

// Collect отдаёт снимок полосы.
func (c *verdictCacheCollector) Collect(ch chan<- prometheus.Metric) {
	st := c.read()
	ch <- prometheus.MustNewConstMetric(verdictCacheEntriesDesc, prometheus.GaugeValue, float64(st.Entries))
	ch <- prometheus.MustNewConstMetric(verdictCacheCapacityDesc, prometheus.GaugeValue, float64(st.Capacity))
	ch <- prometheus.MustNewConstMetric(verdictCacheEvictionsDesc, prometheus.CounterValue, float64(st.Evictions))
}
