// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package metrics — адаптер наблюдаемости края.
//
// Живёт на границе адаптера (чистая архитектура): клиент Prometheus
// импортируется ТОЛЬКО здесь и в композиционном корне — никогда в звеньях,
// клиентах и маршрутизаторе. Реестр ПРИВАТНЫЙ (`prometheus.NewRegistry`, не
// глобальный умолчательный): пробы герметичны, и повторная сборка корня в одном
// процессе не роняет регистрацию.
//
// # Величины читаются ИЗ ПРОЦЕССА и только из него
//
// Коллектор здесь не делает ни одного внешнего вызова в момент сбора: он
// вызывает функцию, которую принёс корень, а та читает атомарные счётчики.
// Диагностика, которая ходит наружу за своими числами, гаснет ровно тогда, когда
// нужна, — свойство держит проба `TestSurfaceAnswersWhenNeighboursAreDown` и
// гейт дерева `TestDiagnosticCollectorsDoNotDialOut`.
//
// # Все значения меток объявляются ПРИ РЕГИСТРАЦИИ
//
// Серии присутствуют нулями с первой секунды жизни процесса, поэтому «ноль
// отказов» и «коллектора нет» различимы без единого запроса. Это и есть предмет
// переписи накопителей: величина, объявленная и никем не прочитанная, выглядит
// как работающее наблюдение и потому надёжно скрывает собственное отсутствие.
//
// # Словарь меток ЗАКРЫТ
//
// Ни одно значение метки не берётся из данных запроса: полосы решений и окна
// вердиктов перечислены здесь константами. Число различных серий в теле не
// растёт с числом обслуженных арендаторов — потолок кардинальности и запрет
// `security.md` §«Инфра-чувствительные данные» суть один запрет с двух сторон.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// Значения меток — ЗАКРЫТЫЙ словарь. Именованные константы, а не литералы по
// месту: одно расхождение в написании завело бы пятую полосу, которую никто не
// заметит, потому что она всегда ноль.
const (
	// Решение ПРИНЯТО — модель прав ответила.
	decisionAllow        = "allow"
	decisionDeny         = "deny"
	decisionErrorRefused = "error_refused"
	decisionErrorPassed  = "error_passed"

	// Решения НЕ БЫЛО — запрос допущен механизмом, который модель не спрашивает.
	// Шесть механизмов, шесть полос: «пропущен, потому что путь публичен» и
	// «разрешён, потому что права есть» — разные факты, и первый обязан быть
	// виден, если однажды в список попадёт путь, которому там не место (#798).
	decisionPublicPath     = "public_path"
	decisionAllowlist      = "allowlist"
	decisionInternalOrigin = "internal_origin"
	decisionOverrideAllow  = "override_allow"
	decisionExempt         = "exempt"
	decisionScopeFiltered  = "scope_filtered"

	cacheHit  = "hit"
	cacheMiss = "miss"
)

// AuthzSnapshot — то, что корень отдаёт коллектору на КАЖДЫЙ сбор.
//
// Снимок, а не ссылки на носители: адаптер не должен знать, из скольких разных
// мест процесса эти величины собраны, а корень — единственное место, где они
// все видны разом.
type AuthzSnapshot struct {
	// Counts — величины накопителя решений края.
	Counts middleware.AuthzCounts
	// ClientCalls — обращения к владельцу прав ПО ПРОВОДУ, включая повторы.
	//
	// Не выводится из числа решений: их отношение и есть усиление повторов, то
	// есть ответ на вопрос, спрашивает край по разу или долбит соседа.
	ClientCalls uint64
}

// Metrics владеет приватным реестром края.
type Metrics struct {
	reg *prometheus.Registry
}

// New собирает адаптер и регистрирует коллекторы среды выполнения и сведения о
// сборке.
func New(version, commit string) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "kacho_api_gateway_build_info",
		Help:        "Build metadata of the running api-gateway binary (constant 1).",
		ConstLabels: prometheus.Labels{"version": version, "commit": commit},
	})
	buildInfo.Set(1)
	reg.MustRegister(buildInfo)
	return &Metrics{reg: reg}
}

// RegisterAuthz провязывает читателя величин решения о доступе.
//
// Функция, а не носитель: корень собирает накопитель решений и клиент владельца
// прав в разных местах, и снимок — единственная форма, в которой они приходят
// сюда вместе.
func (m *Metrics) RegisterAuthz(read func() AuthzSnapshot) {
	if m == nil || read == nil {
		return
	}
	m.reg.MustRegister(&authzCollector{read: read})
}

// Handler — обработчик экспозиции приватного реестра. Монтируется ТОЛЬКО на
// выделенную cluster-internal диагностическую поверхность.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// authzCollector — коллектор величин решения о доступе.
//
// Собственный коллектор, а не набор счётчиков, которые звенья дёргали бы
// напрямую: величины уже накоплены атомарными счётчиками на горячем пути, и
// второе место их учёта разъехалось бы с первым молча.
type authzCollector struct {
	read func() AuthzSnapshot
}

var (
	decisionsDesc = prometheus.NewDesc(
		"kacho_api_gateway_authz_check_decisions_total",
		"Authorization decisions taken at the edge, by outcome. "+
			"error_refused and error_passed are OPPOSITE outcomes of the same failed check: "+
			"the request was refused, or it was let through by the declared soft pass.",
		[]string{"decision"}, nil)
	cacheDesc = prometheus.NewDesc(
		"kacho_api_gateway_authz_cache_total",
		"Decision-cache lookups at the edge, by result.",
		[]string{"result"}, nil)
	clientCallsDesc = prometheus.NewDesc(
		"kacho_api_gateway_authz_client_calls_total",
		"Wire-level AuthorizeService.Check RPCs issued by the edge, retries included. "+
			"Its ratio to the decision count is the retry amplification.",
		nil, nil)
	durationDesc = prometheus.NewDesc(
		"kacho_api_gateway_authz_check_duration_seconds",
		"Time taken to reach an authorization decision at the edge, in seconds.",
		nil, nil)
	// enforcingDesc — включена ли проверка В ЭТОМ ПРОЦЕССЕ.
	//
	// Отдельная серия, а не вывод из нулей на полосах решений: при выключенной
	// проверке звено пропускает всё, коллектор собран, и все полосы стоят
	// нулями — ровно так же, как при отсутствии трафика. Два противоположных
	// состояния, неотличимых на поверхности, — это то же самое, что мягкий
	// проход без своего счётчика (`security.md` §Hardening-инвариант 8(б)).
	enforcingDesc = prometheus.NewDesc(
		"kacho_api_gateway_authz_enforcing",
		"1 when the edge authorization check is enabled in this process, 0 when it is off. "+
			"Zero counters alone cannot tell 'check disabled' from 'no traffic'.",
		nil, nil)
)

// Describe объявляет ВСЕ семейства до первого сбора.
func (c *authzCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- decisionsDesc
	ch <- cacheDesc
	ch <- clientCallsDesc
	ch <- durationDesc
	ch <- enforcingDesc
}

// Collect читает снимок и отдаёт все объявленные серии — включая нулевые.
//
// Ни одного внешнего вызова: `read` возвращает величины, уже лежащие в процессе.
func (c *authzCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.read()

	// Обход ЛИТЕРАЛЬНОГО набора, ключи которого — константы этого файла: словарь
	// меток закрыт по построению, и это же читает гейт дерева
	// `TestDiagnosticCollectorLabelsAreAClosedVocabulary`. Порядок обхода карты
	// не важен — формат экспозиции его не несёт.
	for decision, value := range map[string]uint64{
		decisionAllow:        s.Counts.Allowed,
		decisionDeny:         s.Counts.Denied,
		decisionErrorRefused: s.Counts.ErrorRefused,
		decisionErrorPassed:  s.Counts.ErrorPassed,

		decisionPublicPath:     s.Counts.PublicPath,
		decisionAllowlist:      s.Counts.Allowlist,
		decisionInternalOrigin: s.Counts.InternalOrigin,
		decisionOverrideAllow:  s.Counts.OverrideAllow,
		decisionExempt:         s.Counts.Exempt,
		decisionScopeFiltered:  s.Counts.ScopeFiltered,
	} {
		ch <- prometheus.MustNewConstMetric(decisionsDesc, prometheus.CounterValue,
			float64(value), decision)
	}
	for result, value := range map[string]uint64{
		cacheHit:  s.Counts.CacheHits,
		cacheMiss: s.Counts.CacheMisses,
	} {
		ch <- prometheus.MustNewConstMetric(cacheDesc, prometheus.CounterValue,
			float64(value), result)
	}
	ch <- prometheus.MustNewConstMetric(clientCallsDesc, prometheus.CounterValue, float64(s.ClientCalls))

	enforcing := 0.0
	if s.Counts.Enforcing {
		enforcing = 1
	}
	ch <- prometheus.MustNewConstMetric(enforcingDesc, prometheus.GaugeValue, enforcing)

	ch <- prometheus.MustNewConstHistogram(durationDesc,
		s.Counts.DurationCount, s.Counts.DurationSum, cumulative(s.Counts))
}

// cumulative переводит корзины накопителя (число наблюдений В КАЖДОЙ) в
// кумулятивную форму, которой требует формат экспозиции.
//
// Складывается ЗДЕСЬ, а не в накопителе: из кумулятивной формы исходную уже не
// восстановить, и накопитель, отдающий её наружу, навязывал бы формат
// экспозиции всякому своему читателю.
func cumulative(c middleware.AuthzCounts) map[float64]uint64 {
	out := make(map[float64]uint64, len(c.DurationBuckets))
	var running uint64
	for i, ub := range c.DurationBuckets {
		if i < len(c.DurationCounts) {
			running += c.DurationCounts[i]
		}
		out[ub] = running
	}
	return out
}
