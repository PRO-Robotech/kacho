// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package metrics — Prometheus observability adapter kacho-registry.
//
// Живёт на adapter-границе (Clean Architecture): prometheus-клиент импортируется
// ТОЛЬКО здесь и в composition root (cmd/kacho-registry) — никогда в domain/ или
// в use-case. Метрики снимаются с отдельного cluster-internal diagnostic-порта:
// ни на публичной gRPC-поверхности, ни на data-plane они не публикуются —
// внутренняя кардинальность не tenant-facing (security.md, инфра-чувствительные
// данные только на internal-поверхности). Реестр ПРИВАТНЫЙ (prometheus.NewRegistry,
// не global default): тесты герметичны и нет duplicate-register panic при
// рестартах composition root в одном процессе.
//
// # Почему этот пакет появился и почему именно с этими сериями
//
// У kacho-registry не было НИ ОДНОЙ метрики — ни этого пакета, ни эндпоинта.
// Между тем его очередь регистраций (`kacho_registry.registry_outbox`) — та
// самая, на которой класс «очередь, не доставившая ни одной строки за всю свою
// жизнь» был найден вживую: все её строки отвергались владельцем прав, дренаж
// классифицировал отказ как временный, партиция заклинивала — и всё это выглядело
// исправно, потому что заметить было нечем. Синхронный регистратор при этом
// работал, поэтому наблюдаемое поведение сервиса ошибку скрывало.
//
// Сводные серии по таблице:
//
//	kacho_registry_outbox_backlog_depth{table}              — недоставленных строк
//	kacho_registry_outbox_oldest_pending_age_seconds{table} — возраст головы очереди
//	kacho_registry_outbox_poisoned_rows{table}              — отравленных сейчас
//	kacho_registry_outbox_poisoned_total{table}             — монотонный счётчик отравлений
//
// Разложение той же очереди по направлению:
//
//	kacho_registry_outbox_backlog_depth_by_direction{table,direction}
//	kacho_registry_outbox_oldest_pending_age_by_direction_seconds{table,direction}
//	kacho_registry_outbox_delivered_total{table,direction}
//
// Разложение обязательно, потому что эта очередь несёт ОБЕ половины — постановку
// и снятие регистрации, — а сводные серии на ней остаются здоровыми при полностью
// мёртвом снятии: реестры и репозитории создаются непрерывно, поэтому глубина
// мала и голова молода независимо от того, доехал ли хоть один отзыв.
// `delivered_total{direction="withdrawal"} == 0` — единственная величина,
// отличающая «снимать было нечего» от «снятие не доезжает».
//
// `poisoned_total` — счётчик, а не gauge: отравление это СОБЫТИЕ, и после
// ужесточения классификации отказа в правах до терминального оно реально
// достижимо, поэтому обязано быть alertable, а не выводимым из разницы gauge'ей
// между скрейпами.
//
// Плюс стандартные Go/process runtime-коллекторы — они бесплатны и превращают
// утечку горутин/дескрипторов в график вместо догадки.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowmetrics"

	"github.com/PRO-Robotech/kacho/pkg/authz/authzmetrics"
	outboxmetrics "github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
)

// Metrics владеет приватным prometheus-реестром kacho-registry. Создаётся один
// раз в composition root и шарится diagnostic HTTP-listener'ом, outbox-коллектором
// и poison-observer'ом дренажа.
type Metrics struct {
	reg *prometheus.Registry

	outboxBacklog   *prometheus.GaugeVec
	outboxOldest    *prometheus.GaugeVec
	outboxPoisonCur *prometheus.GaugeVec
	outboxPoisonTot *prometheus.CounterVec

	outboxDirBacklog   *prometheus.GaugeVec
	outboxDirOldest    *prometheus.GaugeVec
	outboxDirDelivered *prometheus.CounterVec
}

// New конструирует адаптер и регистрирует Go + process runtime-коллекторы и
// outbox-серии kacho-registry.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &Metrics{
		reg: reg,
		outboxBacklog: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_registry_outbox_backlog_depth",
			Help: "Недоставленные строки outbox-таблицы.",
		}, []string{"table"}),
		outboxOldest: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_registry_outbox_oldest_pending_age_seconds",
			Help: "Возраст самой старой недоставленной строки outbox-таблицы. " +
				"Отвечает на «висит ли строка дольше N».",
		}, []string{"table"}),
		outboxPoisonCur: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_registry_outbox_poisoned_rows",
			Help: "Отравленные (исчерпавшие попытки) строки outbox-таблицы сейчас.",
		}, []string{"table"}),
		outboxPoisonTot: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kacho_registry_outbox_poisoned_total",
			Help: "Монотонный счётчик отравлений: отравление — событие, а не состояние.",
		}, []string{"table"}),
		outboxDirBacklog: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_registry_outbox_backlog_depth_by_direction",
			Help: "Недоставленные строки очереди по направлению (постановка / снятие регистрации).",
		}, []string{"table", "direction"}),
		outboxDirOldest: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kacho_registry_outbox_oldest_pending_age_by_direction_seconds",
			Help: "Возраст самой старой недоставленной строки одного направления — " +
				"отвечает на «как давно это направление перестало доезжать».",
		}, []string{"table", "direction"}),
		outboxDirDelivered: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kacho_registry_outbox_delivered_total",
			Help: "Доставленные строки очереди по направлению, считаются В МОМЕНТ " +
				"доставки. Единственная величина, отличающая «их не было» от «они не " +
				"доезжают». Счётчик, а не измеритель: значение не зависит от числа " +
				"хранимых строк, поэтому уборка доставленных его не снижает. Ноль означает " +
				"«ни одной с момента старта процесса»; порог ставить на increase() за окно.",
		}, []string{"table", "direction"}),
	}
	reg.MustRegister(
		m.outboxBacklog, m.outboxOldest, m.outboxPoisonCur, m.outboxPoisonTot,
		m.outboxDirBacklog, m.outboxDirOldest, m.outboxDirDelivered,
	)
	return m
}

// Handler возвращает promhttp-handler приватного реестра. Монтируется ТОЛЬКО на
// выделенном cluster-internal diagnostic-listener'е.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// Registerer отдаёт реестр этого сервиса как ПРИЁМНИК регистрации.
//
// # Зачем окно наружу, если рядом есть именованные
//
// Соседние окна (`RegisterAuthzCache`, `RegisterListNarrow`, `RegisterPoolStats`)
// принимают ЧИТАТЕЛЯ готовых величин: они существуют, чтобы этот пакет не
// импортировал доменные. Здесь предмет обратный — носителю входящего пути надо
// ЗАВЕСТИ своё семейство серий, а не отдать читателя, и заводит он его СВОИМИ
// руками. Тогда несогласованное объявление (то же имя с другой размерностью)
// становится отказом подъёма, а не молчаливой пропажей семейства со скрейпа.
//
// Разбор решения — у поля, ради которого окно открыто:
// `pkg/servicecontract.Spec.Metrics` (отказ старта О13). Здесь он не
// пересказывается: два места об одном предмете расходятся на первом уточнении.
//
// Отдаётся именно `Registerer`, а не сам реестр: собирать величины через это
// окно нельзя, и сузить его до одной операции дешевле, чем потом выяснять, кто
// ещё им воспользовался.
func (m *Metrics) Registerer() prometheus.Registerer { return m.reg }

// ---- outbox/metrics.Recorder ----

// SetBacklogDepth выставляет глубину pending-очереди по таблице.
func (m *Metrics) SetBacklogDepth(table string, depth float64) {
	m.outboxBacklog.WithLabelValues(table).Set(depth)
}

// SetOldestPendingAgeSeconds выставляет возраст старейшей pending-строки.
func (m *Metrics) SetOldestPendingAgeSeconds(table string, age float64) {
	m.outboxOldest.WithLabelValues(table).Set(age)
}

// SetPoisonedCount выставляет текущее число отравленных строк (Collector scan).
func (m *Metrics) SetPoisonedCount(table string, count float64) {
	m.outboxPoisonCur.WithLabelValues(table).Set(count)
}

// IncPoisoned инкрементит монотонный poison-счётчик (drainer poison-observer).
func (m *Metrics) IncPoisoned(table string) { m.outboxPoisonTot.WithLabelValues(table).Inc() }

// ---- outbox/metrics.DirectionRecorder ----
//
// Разложение по направлению Collector спрашивает у получателя ПРИВЕДЕНИЕМ ТИПА в
// рантайме: потеря этих трёх методов в рефакторинге сборку бы не сломала, а молча
// прекратила бы публиковать единственную серию, отвечающую на «доезжает ли снятие
// вообще». Утверждение ниже — то, что не даёт этому случиться тихо.

// SetBacklogDepthByDirection — pending-строки одного направления очереди.
func (m *Metrics) SetBacklogDepthByDirection(table, direction string, depth float64) {
	m.outboxDirBacklog.WithLabelValues(table, direction).Set(depth)
}

// SetOldestPendingAgeByDirection — возраст старейшей pending-строки направления.
func (m *Metrics) SetOldestPendingAgeByDirection(table, direction string, age float64) {
	m.outboxDirOldest.WithLabelValues(table, direction).Set(age)
}

// IncDeliveredByDirection — ОДНА доставленная строка направления.
//
// СЧЁТЧИК, инкрементируемый наблюдателем дренажа, а не измеритель, ставящийся
// сканом (#1714): величина объявлена «за всё время», и счёт по живым строкам
// совпадал с этим ровно до появления уборки доставленных строк.
func (m *Metrics) IncDeliveredByDirection(table, direction string) {
	m.outboxDirDelivered.WithLabelValues(table, direction).Inc()
}

// InitDeliveredByDirection заводит серию направления с нулём, не увеличивая её:
// дочерняя серия счётчика иначе появилась бы только после ПЕРВОЙ доставки, и
// «ни одного отзыва не доставлено» выражалось бы отсутствием ряда вместо нуля.
func (m *Metrics) InitDeliveredByDirection(table, direction string) {
	m.outboxDirDelivered.WithLabelValues(table, direction)
}

// Compile-time: адаптер удовлетворяет оба corelib-порта.
var (
	_ outboxmetrics.Recorder          = (*Metrics)(nil)
	_ outboxmetrics.DirectionRecorder = (*Metrics)(nil)
)

// RegisterListNarrow провязывает читателя величин СУЖАТЕЛЯ СПИСКОВ.
//
// Коллектор — ОДНА реализация на все сервисы (`pkg/listnarrow/narrowmetrics`),
// потому что пять копий одинаковых на вид полос разъезжаются молча — ровно по
// той же причине, по которой единствен и сам сужатель. Здесь только имя
// сервиса и читатель.
//
// `read == nil` не отменяет серий: четыре полосы объявляются нулями и на
// посадке без сужателя, иначе «сужений не было» стало бы неотличимо от
// «коллектора нет».
func (m *Metrics) RegisterListNarrow(read func() listnarrow.Counts) {
	m.reg.MustRegister(narrowmetrics.New("registry", read))
}

// RegisterAuthzCache провязывает читателей величин КЕША ПОЛОЖИТЕЛЬНЫХ ВЕРДИКТОВ.
//
// Коллектор — ОДНА реализация на все сервисы (`pkg/authz/authzmetrics`) и одно
// правило имени серии, однородное с краем
// (`kacho_api_gateway_authz_cache_total`): собиратель, у которого уже есть
// правило на край, читает сервисы тем же выражением.
//
// Полосы объявляет ВЫЗЫВАЮЩИЙ: кешей вердиктов в процессе бывает больше одного,
// и полоса, которой в этом процессе нет, не рисуется нулями — иначе экспозиция
// утверждала бы существование окна, которого нет.
//
// Решения звена — величина ПРОЦЕССА, а не полосы: звено у сервиса одно, и оба
// слушателя проходят через него.
func (m *Metrics) RegisterAuthzCache(lanes map[string]authzmetrics.Reader,
	decisions authzmetrics.DecisionReader) {
	m.reg.MustRegister(authzmetrics.New("registry", lanes, decisions))
}
