// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package authzfilter реализует per-object фильтрацию видимости для kacho-nlb.
//
// Каждый публичный List use-case (NetworkLoadBalancer / Listener / TargetGroup)
// читает СТРАНИЦУ строк из своей БД курсором и затем спрашивает kacho-iam, какие
// id этой страницы видимы вызывающему subject'у (`AuthorizeService.BatchCheck`,
// батчи ≤100). Это даёт настоящую per-object видимость: видимый набор равен
// Check-allow набору (read==enforce), а стоимость пропорциональна СТРАНИЦЕ, а не
// популяции типа в сторе.
//
// # Почему не ListObjects (важно — это был баг)
//
// Раньше фильтр спрашивал `AuthorizeService.ListObjects` — «перечисли ВСЕ объекты
// этого типа, которые subject'у можно» — и сужал SQL до полученного набора
// (`WHERE id = ANY`). У OpenFGA ListObjects есть ЖЁСТКИЙ server-side предел
// (`OPENFGA_LIST_OBJECTS_MAX_RESULTS`, default 1000) и НЕТ continuation-token'а:
// перечисление молча возвращает произвольный 1000-id префикс. Предел действует на
// ТИП В СТОРЕ (cluster-wide), а не на тенанта, поэтому на долгоживущем сторе
// (>1000 объектов типа) собственный ресурс тенанта выпадал за префикс и исчезал из
// List навсегда: строка есть, грант есть, Get/Update/Delete (они задают ПРЯМОЙ
// per-object вопрос через per-RPC interceptor) работают — а List пуст. Замер на
// стенде: `nlb_network_load_balancer` = 20739 tuples, `nlb_target_group` = 10447,
// `nlb_listener` = 5208 — все далеко за пределом. Просьба `max_results=10000`
// предел не поднимает — это лишь client-side trim уже усечённого ответа.
//
// Лечится не поднятием предела (он внешний и всё равно конечен), а формой вопроса:
// вместо «перечисли вселенную и найди в ней» — «можно ли этому subject'у ЭТОТ
// объект», батчем на страницу.
//
// # Предикат видимости не изменился
//
// `ListObjects` по определению возвращает объекты, на которых `Check` сказал бы
// «да», а kacho-iam объединял два отношения: `viewer ∪ v_list` (см.
// AuthorizeService.ListObjects). Здесь вычисляется РОВНО тот же предикат, только
// per-object: сначала батч на `viewer`, затем батч на `v_list` для тех, кому
// `viewer` отказал. Никакого ослабления авторизации — тот же вопрос, другая форма
// запроса.
//
// subject извлекается из ctx через operations.PrincipalFromContext (nlb-конвенция,
// см. subject.go), не из raw-metadata.
package authzfilter

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/validate"
)

// maxBatchCheckSize — контрактный предел AuthorizeService.BatchCheck
// (>100 → InvalidArgument). Батчи режутся по нему.
const maxBatchCheckSize = 100

// defaultParallelism — сколько ≤100-id батчей ОДНОЙ relation летят в kacho-iam
// ОДНОВРЕМЕННО (bounded worker-pool, не «горутина на батч»).
//
// # Почему 5, и почему это не косметика
//
// Батчи внутри relation НЕЗАВИСИМЫ (per-object вопросы), но обходились
// ПОСЛЕДОВАТЕЛЬНО. На документированном максимуме страницы (validate.MaxPageSize
// = 1000) это ceil(1000/100) = 10 round-trip'ов на relation, а relations две и
// они последовательны по построению (`v_list` спрашивается только у отказанных
// `viewer`) — до 20 подряд, каждый под своим per-call дедлайном. Замер на стабе с
// латентностью пира 400ms: 17 round-trip'ов, 6.8s стены — и НИ ОДНОГО потолка на
// операцию. А стоило латентности пира перешагнуть per-call дедлайн (500ms), как
// ПЕРВЫЙ же батч ронял весь позитивный List в `Unavailable`. Под параллельным
// e2e ровно это и происходило, на холостом стенде — нет.
//
// 5 выбрано из арифметики контракта, а не «на глаз»:
//   - 5 РОВНО делит максимум батчей (MaxPageSize/maxBatchCheckSize = 10) ⇒ на
//     предельной странице каждая relation это ровно 2 полные волны, без рваного
//     хвоста (у 4 волны были бы 4+4+2 — третья волна ради двух батчей);
//   - глубина worst-case падает 20 → 4 волн (5×), поэтому per-call дедлайн можно
//     сделать РЕАЛИСТИЧНЫМ (см. DefaultConfig.Timeout), а не пилить его под
//     число последовательных хопов;
//   - всплеск на пира ограничен 5 одновременными BatchCheck (≤500 checks
//     in-flight) на запрос — предсказуемый малый множитель: даже десятки
//     параллельных List на реплике держатся в пределах одного HTTP/2-соединения
//     и не выедают конкурентные стримы.
//
// Это НЕ ослабление проверки: спрашивается тот же предикат, теми же батчами
// ≤100, тот же fail-closed — меняется только порядок ожидания ответов.
const defaultParallelism = 5

// budgetHeadroom{Num,Den} — множитель бюджета операции над worst-case стеной
// (×3/2 ⇒ запас 33%). Бюджет обязан быть ПОТОЛКОМ, который в здоровом режиме
// не срабатывает: срабатывать должны per-call дедлайны.
const (
	budgetHeadroomNum = 3
	budgetHeadroomDen = 2
)

// visibilityRelations — отношения, объединение которых и есть «видимость» в
// List/Get: `viewer` (read-tier: direct usersets ∪ editor ∪ admin) и `v_list`
// (object-only selector-грант «видеть в списке без содержимого»). Тот же союз,
// что делал AuthorizeService.ListObjects — порядок значим только для стоимости
// (`viewer` покрывает подавляющее большинство, `v_list` добирает остаток).
var visibilityRelations = [...]string{"viewer", "v_list"}

// Filter — port фильтра видимости. Реализация — *FGAFilter (через
// AuthorizeService.BatchCheck) либо nil (list-filter disabled / dev).
//
// Порт НЕ умеет «перечислить всё, что subject'у можно»: такого вопроса больше нет
// ни на одном пути (он был источником усечения на пределе OpenFGA ListObjects —
// см. package-doc). Единичное чтение по id (`Get`) авторизуется ПРЯМЫМ per-object
// Check в per-RPC interceptor'е — так же, как Update/Delete, — и в этот порт не
// заходит вовсе.
type Filter interface {
	// FilterVisibleIDs возвращает подмножество ids, видимое subject'у, СОХРАНЯЯ
	// порядок входа (страница уже отсортирована курсором — переупорядочивание
	// сломало бы пагинацию).
	//
	//   resourceType — FGA object type ("nlb_network_load_balancer", …).
	//   action       — semantic permission ("loadbalancer.networkLoadBalancers.list");
	//                  передаётся в kacho-iam для аудита/трассировки, решение
	//                  принимает явный required_relation (см. visibilityRelations).
	//   subject      — FGA subject ("user:usr_alice" / "service_account:sa_x").
	//
	// err != nil → fail-closed: caller ОБЯЗАН пробросить ошибку, а не отдать
	// нефильтрованную страницу.
	FilterVisibleIDs(ctx context.Context, subject, resourceType, action string, ids []string) ([]string, error)
}

// Config — параметры FGAFilter.
type Config struct {
	// Enabled — master-switch. false → FilterVisibleIDs возвращает ids как есть
	// (нефильтрованный passthrough; для dev / graceful start без iam).
	Enabled bool
	// Timeout — per-call deadline ОДНОГО BatchCheck-вызова (architecture.md:
	// per-call deadline на КАЖДОМ внешнем вызове). НЕ бюджет операции — см.
	// OverallTimeout.
	Timeout time.Duration
	// Parallelism — сколько ≤100-id батчей ОДНОЙ relation летят одновременно.
	// 0 → defaultParallelism. Ограничивает fan-out worker-пулом; выше
	// количества батчей страницы не поднимается.
	Parallelism int
	// OverallTimeout — бюджет ВСЕЙ операции FilterVisibleIDs (все relations, все
	// волны батчей). 0 → выводится из Timeout и Parallelism так, чтобы
	// worst-case стена на предельной странице помещалась внутрь с запасом (см.
	// deriveOverallTimeout). Потолок нужен, потому что per-call дедлайн
	// ограничивает ОДИН хоп, а не их последовательность: без него деградировавший
	// пир держал бы List столько, сколько волн осталось.
	OverallTimeout time.Duration
	// CacheTTL — TTL одной положительной записи visibility-cache.
	CacheTTL time.Duration
	// CacheMaxEntries — bound размера cache (в записях «(subject,type,id) видим»).
	// Это ИСКЛЮЧИТЕЛЬНО размер кеша: раньше та же величина уезжала ещё и в
	// `ListObjects.max_results` («cap видимых объектов»), связывая два
	// несвязанных смысла; per-object BatchCheck никакого cap'а не имеет.
	CacheMaxEntries int
	// FailOpen — на iam-ошибке: true → страница отдаётся нефильтрованной + warn;
	// false → Unavailable (fail-closed, default — security.md).
	FailOpen bool
}

// DefaultConfig — sane defaults: фильтр включён, 1s per-call timeout, fan-out 5,
// выведенный бюджет операции, 5s TTL, 10000 entries, fail-closed.
//
// # Почему per-call дедлайн 1s, а не прежние 500ms
//
// Это НЕ «подняли таймаут». Прежняя форма (последовательные батчи) требовала,
// чтобы per-call дедлайн делился на ЧИСЛО ПОСЛЕДОВАТЕЛЬНЫХ ХОПОВ — до 20 на
// предельной странице. Отсюда и брались 500ms: не потому, что столько отвечает
// здоровый пир, а потому, что больше «не помещалось». В итоге было худшее из
// двух: хрупкий хоп (пир под нагрузкой перешагивает 500ms → весь позитивный List
// в 503) И длинная НЕОГРАНИЧЕННАЯ агрегата (замер: 17 хопов, 6.8s).
//
// Bounded fan-out срезает глубину 20 → 4 волн, и это ОСВОБОЖДАЕТ бюджет под
// реалистичный per-call дедлайн: 4 × 1s = 4s worst-case стены против прежних
// 10-20 × 500ms = 5..10s. То есть допуск на ОДИН хоп вырос вдвое, а агрегатный
// worst-case при этом УПАЛ (6.8s замерено → 4s) и стал ОГРАНИЧЕННЫМ (бюджет 6s).
func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		Timeout:         time.Second,
		Parallelism:     defaultParallelism,
		CacheTTL:        5 * time.Second,
		CacheMaxEntries: 10000,
		FailOpen:        false,
	}
}

// AuthorizeClient — узкий интерфейс к kacho-iam AuthorizeService (тестируемость).
// Сигнатура совпадает со сгенерированным AuthorizeServiceClient.BatchCheck —
// NewIAMAuthorizeClient это thin pass-through.
type AuthorizeClient interface {
	BatchCheck(ctx context.Context, in *iamv1.BatchAuthorizeCheckRequest, opts ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error)
}

// FGAFilter — продакшен-реализация Filter поверх AuthorizeService.BatchCheck с
// in-memory TTL+LRU-кешем ПОЛОЖИТЕЛЬНЫХ вердиктов.
//
// Кешируются только «видим»: отрицательный вердикт никогда не кешируется, иначе
// свежий грант не был бы виден до истечения TTL. Промах по кешу стоит один элемент
// батча, а не отдельный round-trip.
//
// Eviction — LRU: при переполнении CacheMaxEntries вытесняется least-recently-used
// запись, а не произвольная (Go-map-randomized, возможно горячая).
//
// УСТАРЕВАНИЕ ЗАПИСИ ОГРАНИЧЕНО ТОЛЬКО СРОКОМ ЖИЗНИ (CacheTTL, по умолчанию 5s), и
// иного пути её снятия НЕТ. Здесь лежал метод снятия записей субъекта с пометкой
// «LISTEN/NOTIFY-driven» — механизма, которого у ЭТОГО кеша не существует: его никто,
// кроме теста, не звал, и в композиционном корне он не провязан. Пометка описывала
// соседний кеш — у per-RPC проверки (internal/check) действительно есть общий
// pg_notify-инвалидатор, — и создавала впечатление, что отзыв права снимается сразу и
// тут. Метод удалён вместе со своим тестом (мёртвый код), а граница названа: отзыв
// права перестаёт быть виден списочной фильтрации в пределах CacheTTL. Отрицательный
// вердикт не кешируется вовсе, поэтому свежая ВЫДАЧА видна сразу — ограничение
// касается только отзыва.
type FGAFilter struct {
	cli AuthorizeClient
	cfg Config

	// logger — sink записей о мягком проходе. Инъектируется (по умолчанию
	// slog.Default(), который композиционный корень выставляет на старте).
	logger *slog.Logger

	// now — источник времени для TTL-логики кеша. Инъектируется (по умолчанию
	// time.Now), чтобы TTL-тесты были детерминированными и не зависели от
	// wall-clock/time.Sleep (flaky под -race/GC/CPU-throttle).
	now func() time.Time

	// openPassMisconfigured / openPassTransient — сколько раз страница ушла
	// НЕотфильтрованной, раздельно по причине. Раздельно — потому что это разные
	// вещи по смыслу: временный отказ соседа проходит сам, неверная настройка не
	// пройдёт никогда. Читаются OpenPassCounts.
	openPassMisconfigured atomic.Uint64
	openPassTransient     atomic.Uint64

	mu     sync.Mutex
	cache  map[string]*list.Element
	lruLst *list.List
}

type cacheEntry struct {
	key     string
	expires time.Time
}

// NewFGAFilter создаёт фильтр. cli == nil → passthrough (graceful start без iam).
//
// Нормализует Parallelism и ВЫВОДИТ OverallTimeout, если тот не задан явно:
// бюджет операции — не независимая ручка, а ФУНКЦИЯ контракта (максимум
// страницы, предел батча) и per-call дедлайна. Так арифметика «сумма worst-case
// ожиданий помещается в бюджет» держится ПО ПОСТРОЕНИЮ, а не по надежде на
// когерентный config.
func NewFGAFilter(cli AuthorizeClient, cfg Config) *FGAFilter {
	if cfg.Timeout <= 0 {
		cfg.Timeout = time.Second
	}
	if cfg.Parallelism <= 0 {
		cfg.Parallelism = defaultParallelism
	}
	if cfg.OverallTimeout <= 0 {
		cfg.OverallTimeout = deriveOverallTimeout(cfg.Timeout, cfg.Parallelism)
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 5 * time.Second
	}
	if cfg.CacheMaxEntries <= 0 {
		cfg.CacheMaxEntries = 10000
	}
	return &FGAFilter{
		cli:    cli,
		cfg:    cfg,
		logger: slog.Default(),
		now:    time.Now,
		cache:  make(map[string]*list.Element, cfg.CacheMaxEntries),
		lruLst: list.New(),
	}
}

// WithLogger подменяет sink записей о мягком проходе (композиционный корень /
// white-box тесты).
func (f *FGAFilter) WithLogger(l *slog.Logger) *FGAFilter {
	if l != nil {
		f.logger = l
	}
	return f
}

// OpenPassCounts — сколько раз фильтр отдал страницу НЕотфильтрованной, раздельно
// по причине: misconfigured — сосед ответил так, что это доказывает неверную
// настройку (сама не пройдёт); transient — отказ, который может пройти сам.
//
// Значения читаются, а не выводятся из наличия записей в логе: «ноль проходов за всю
// жизнь фильтра» обязано быть отличимо от «счётчика нет», а по отсутствию строк в
// логе эти два состояния неразличимы.
func (f *FGAFilter) OpenPassCounts() (misconfigured, transient uint64) {
	if f == nil {
		return 0, 0
	}
	return f.openPassMisconfigured.Load(), f.openPassTransient.Load()
}

// ceilDiv — целочисленное деление вверх (b > 0).
func ceilDiv(a, b int) int { return (a + b - 1) / b }

// worstCaseDepth — максимальное число ПОСЛЕДОВАТЕЛЬНЫХ round-trip'ов, которые
// FilterVisibleIDs может потратить на самой большой допустимой странице.
//
// Страница ≤ validate.MaxPageSize ⇒ ≤ ceil(MaxPageSize/maxBatchCheckSize)
// батчей на relation; батчи внутри relation идут волнами по Parallelism;
// relations последовательны по построению (`v_list` только для отказанных
// `viewer`), и в худшем случае ВСЯ страница доезжает до второй relation.
func worstCaseDepth(parallelism int) int {
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}
	batches := ceilDiv(int(validate.MaxPageSize), maxBatchCheckSize)
	return len(visibilityRelations) * ceilDiv(batches, parallelism)
}

// deriveOverallTimeout — бюджет операции: worst-case стена (глубина × per-call
// дедлайн) плюс запас budgetHeadroom. Запас гарантирует, что в здоровом (пусть и
// медленном) режиме первым срабатывает per-call дедлайн, а бюджет остаётся
// потолком против деградировавшего пира.
func deriveOverallTimeout(perCall time.Duration, parallelism int) time.Duration {
	return time.Duration(worstCaseDepth(parallelism)) * perCall * budgetHeadroomNum / budgetHeadroomDen
}

// Parallelism — фактический bound fan-out'а (observability/tests).
func (f *FGAFilter) Parallelism() int { return f.cfg.Parallelism }

// Budget — фактический бюджет одной операции FilterVisibleIDs
// (observability/tests).
func (f *FGAFilter) Budget() time.Duration { return f.cfg.OverallTimeout }

// WorstCaseDepth — worst-case число последовательных round-trip'ов на предельной
// странице при текущем Parallelism (observability/tests).
func (f *FGAFilter) WorstCaseDepth() int { return worstCaseDepth(f.cfg.Parallelism) }

// nowFn — safe accessor для инъектируемых часов (fallback на time.Now, если
// FGAFilter сконструирован не через NewFGAFilter).
func (f *FGAFilter) nowFn() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// FilterVisibleIDs — основной entry-point. См. Filter.
func (f *FGAFilter) FilterVisibleIDs(ctx context.Context, subject, resourceType, action string, ids []string) ([]string, error) {
	if f == nil || !f.cfg.Enabled || f.cli == nil {
		return ids, nil
	}
	if subject == "" {
		// Principal не извлечён (anonymous / потерянный forwarded-principal) —
		// fail-closed. Пустой subject НЕ означает «system, можно всё»: это была бы
		// cross-tenant enumeration дыра (CWE-862).
		return nil, status.Error(codes.Unauthenticated, "list filter: subject required")
	}
	if resourceType == "" || action == "" {
		return nil, fmt.Errorf("authzfilter: resourceType and action required")
	}
	if len(ids) == 0 {
		return nil, nil
	}

	// Бюджет ВСЕЙ операции. Per-call дедлайны остаются (batchCheckOnce), но
	// выводятся уже из этого ctx — раньший дедлайн выигрывает автоматически,
	// поэтому последовательность волн не может суммарно пережить бюджет.
	if f.cfg.OverallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.cfg.OverallTimeout)
		defer cancel()
	}

	visible := make(map[string]struct{}, len(ids))
	// pending — ещё не признанные видимыми (дедуплицированы: одна страница не
	// должна платить дважды за повторяющийся id).
	pending := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if f.getCache(subject, resourceType, id) {
			visible[id] = struct{}{}
			continue
		}
		pending = append(pending, id)
	}

	for _, relation := range visibilityRelations {
		if len(pending) == 0 {
			break
		}
		allowed, denied, err := f.checkRelation(ctx, subject, resourceType, action, relation, pending)
		if err != nil {
			return f.handleErr(ids, err)
		}
		for _, id := range allowed {
			visible[id] = struct{}{}
			f.putCache(subject, resourceType, id)
		}
		pending = denied
	}

	// Порядок входа сохраняется — страница уже упорядочена курсором.
	out := make([]string, 0, len(visible))
	for _, id := range ids {
		if _, ok := visible[id]; ok {
			delete(visible, id) // защита от дублей во входе
			out = append(out, id)
		}
	}
	return out, nil
}

// batchVerdicts — вердикты ОДНОГО батча. Каждая горутина пишет в СВОЙ элемент
// среза (разные ячейки — не гонка), а склейка идёт по индексу батча, а не по
// порядку завершения: разбиение allowed/denied обязано быть ДЕТЕРМИНИРОВАННЫМ
// независимо от того, кто ответил первым.
type batchVerdicts struct {
	allowed []string
	denied  []string
}

// splitBatches режет ids на батчи ≤ maxBatchCheckSize (контракт BatchCheck).
func splitBatches(ids []string) [][]string {
	out := make([][]string, 0, ceilDiv(len(ids), maxBatchCheckSize))
	for start := 0; start < len(ids); start += maxBatchCheckSize {
		end := start + maxBatchCheckSize
		if end > len(ids) {
			end = len(ids)
		}
		out = append(out, ids[start:end])
	}
	return out
}

// checkRelation спрашивает kacho-iam об ОДНОМ отношении для набора ids батчами
// ≤ maxBatchCheckSize. Возвращает разрешённые и отказанные (для следующего
// отношения).
//
// Батчи НЕЗАВИСИМЫ (per-object вопросы), поэтому идут ОГРАНИЧЕННЫМ fan-out'ом
// (worker-пул на Parallelism), а не по одному: последовательный обход давал на
// предельной странице до 20 round-trip'ов подряд и вынуждал держать per-call
// дедлайн неестественно узким (см. defaultParallelism / DefaultConfig).
//
// Каждый батч по-прежнему идёт под СОБСТВЕННЫМ per-call deadline
// (batchCheckOnce, architecture.md), а вся операция — внутри бюджета
// FilterVisibleIDs.
//
// Fail-closed: ПЕРВАЯ ошибка отменяет соседей и возвращается как есть; частично
// разрешённый набор не отдаётся никогда.
func (f *FGAFilter) checkRelation(
	ctx context.Context,
	subject, resourceType, action, relation string,
	ids []string,
) (allowed, denied []string, err error) {
	batches := splitBatches(ids)
	results := make([]batchVerdicts, len(batches))

	// Одиночный батч (страница ≤100) не платит за пул — общий случай остаётся
	// ровно тем же одним round-trip'ом.
	if len(batches) == 1 {
		if cerr := f.checkBatch(ctx, subject, resourceType, action, relation, batches[0], &results[0]); cerr != nil {
			return nil, nil, cerr
		}
		return joinVerdicts(results, len(ids))
	}

	workers := f.cfg.Parallelism
	if workers > len(batches) {
		workers = len(batches)
	}

	// cctx отменяется ПЕРВОЙ ошибкой — соседние батчи не дорабатывают страницу,
	// которую всё равно не отдадут.
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)
	queue := make(chan int)
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for b := range queue {
				cerr := f.checkBatch(cctx, subject, resourceType, action, relation, batches[b], &results[b])
				if cerr == nil {
					continue
				}
				mu.Lock()
				if firstErr == nil {
					firstErr = cerr
					cancel()
				}
				mu.Unlock()
				return
			}
		}()
	}
feed:
	for b := range batches {
		select {
		case queue <- b:
		case <-cctx.Done():
			break feed // кто-то уже упал fail-closed либо истёк бюджет
		}
	}
	close(queue)
	wg.Wait()

	if firstErr != nil {
		return nil, nil, firstErr
	}
	// cancel() зовётся только после установки firstErr, поэтому done-cctx без
	// зафиксированной ошибки означает, что истёк РОДИТЕЛЬСКИЙ ctx (бюджет
	// операции или дедлайн вызывающего) посреди страницы. Сообщаем об этом, а не
	// отдаём недорешённый набор за ответ.
	if cerr := ctx.Err(); cerr != nil {
		return nil, nil, cerr
	}
	return joinVerdicts(results, len(ids))
}

// joinVerdicts склеивает вердикты в порядке БАТЧЕЙ (не завершения) — результат
// не зависит от того, какая горутина ответила первой.
func joinVerdicts(results []batchVerdicts, total int) (allowed, denied []string, err error) {
	allowed = make([]string, 0, total)
	denied = make([]string, 0, total)
	for i := range results {
		allowed = append(allowed, results[i].allowed...)
		denied = append(denied, results[i].denied...)
	}
	return allowed, denied, nil
}

// checkBatch — один BatchCheck на один батч; вердикты кладутся в out.
func (f *FGAFilter) checkBatch(
	ctx context.Context,
	subject, resourceType, action, relation string,
	batch []string,
	out *batchVerdicts,
) error {
	checks := make([]*iamv1.AuthorizeCheckRequest, 0, len(batch))
	for _, id := range batch {
		checks = append(checks, &iamv1.AuthorizeCheckRequest{
			Subject:          subject,
			Resource:         &iamv1.ResourceRef{Type: resourceType, Id: id},
			Action:           action,
			RequiredRelation: relation,
		})
	}

	resp, cerr := f.batchCheckOnce(ctx, &iamv1.BatchAuthorizeCheckRequest{Checks: checks})
	if cerr != nil {
		return cerr
	}
	// Контракт BatchCheck: responses в порядке checks и той же длины.
	// Расхождение — не «считаем отказом», а fail-closed ошибка: молчаливое
	// смещение индексов выдало бы вердикт одного объекта за другой.
	if len(resp.GetResponses()) != len(batch) {
		return fmt.Errorf("authzfilter: BatchCheck returned %d responses for %d checks",
			len(resp.GetResponses()), len(batch))
	}
	out.allowed = make([]string, 0, len(batch))
	out.denied = make([]string, 0, len(batch))
	for i, r := range resp.GetResponses() {
		if r.GetAllowed() {
			out.allowed = append(out.allowed, batch[i])
		} else {
			out.denied = append(out.denied, batch[i])
		}
	}
	return nil
}

// batchCheckOnce делает ровно один BatchCheck под собственным per-call deadline
// (architecture.md: per-call deadline на КАЖДОМ внешнем вызове). ctx уже несёт
// бюджет операции, поэтому фактическим дедлайном становится РАНЬШИЙ из двух.
func (f *FGAFilter) batchCheckOnce(ctx context.Context, req *iamv1.BatchAuthorizeCheckRequest) (*iamv1.BatchAuthorizeCheckResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, f.cfg.Timeout)
	defer cancel()
	return f.cli.BatchCheck(callCtx, req)
}

// handleErr — реакция по fail-open / fail-closed.
func (f *FGAFilter) handleErr(ids []string, err error) ([]string, error) {
	if f.cfg.FailOpen {
		return f.openPass(ids, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		// Оба числа названы явно: сработать мог как per-call дедлайн, так и
		// бюджет операции, и по ошибке ctx их не различить. Называть только
		// per-call (как было) — вводит в заблуждение, когда истёк бюджет.
		return nil, status.Errorf(codes.Unavailable,
			"list filter: AuthorizeService.BatchCheck deadline exceeded (per-call %s, operation budget %s)",
			f.cfg.Timeout, f.cfg.OverallTimeout)
	}
	if s, ok := status.FromError(err); ok && s.Code() != codes.OK && s.Code() != codes.Unknown {
		return nil, status.Errorf(codes.Unavailable, "list filter: AuthorizeService.BatchCheck %s: %s", s.Code(), s.Message())
	}
	return nil, status.Errorf(codes.Unavailable, "list filter: AuthorizeService.BatchCheck: %v", err)
}

// openPass — задокументированный мягкий проход: страница уходит вызывающему
// НЕотфильтрованной. Он защитим ровно пока отказ действительно временный, поэтому
// здесь он (а) классифицируется и (б) считается.
//
// Классификация решает, чем это НАЗВАНО. Ответ соседа, доказывающий, что мы
// обращаемся не туда (нет такого метода, учётные данные не приняты, запрос не той
// формы), — это НАСТРОЙКА: она не «пройдёт сама», и пока её не поправят, фильтр
// исполняется на каждом запросе и не сужает ни одной страницы. Постоянное состояние
// не может называться предупреждением о временном.
//
// Счётчик нужен ОТДЕЛЬНО от записи: по логу видно, что проход БЫЛ, и не видно, что
// его НЕ БЫЛО. Прочитанный ноль (OpenPassCounts) эти два состояния различает.
//
// err — статус ответа соседа (без pgx/SQL-текста), пишется только на своей стороне,
// вызывающему не возвращается.
func (f *FGAFilter) openPass(ids []string, err error) ([]string, error) {
	if provesMisconfiguration(err) {
		total := f.openPassMisconfigured.Add(1)
		f.logger.Error("list filter fail-open: iam.BatchCheck answered in a way that proves a MISCONFIGURED edge, not an outage — "+
			"this will not clear on its own; the per-object filter decides nothing and the page is returned UNFILTERED",
			"error", err, "misconfigured_open_passes_total", total)
		return ids, nil
	}
	total := f.openPassTransient.Add(1)
	f.logger.Warn("list filter fail-open: iam.BatchCheck unreachable, bypassing per-object authz (returning the UNFILTERED page)",
		"error", err, "transient_open_passes_total", total)
	return ids, nil
}

// provesMisconfiguration — доказывает ли ответ, что дело не в доступности соседа.
//
// Различие проводится по одному вопросу: может ли повтор ТОГО ЖЕ запроса когда-нибудь
// пройти?
//
//   - истёкший срок / отменённый контекст — может (медленный, но живой сосед).
//     Проверяется ПЕРВЫМ: голый context.DeadlineExceeded не является gRPC-статусом, и
//     без этой ветки истёкший бюджет операции читался бы как доказательство неверной
//     настройки — ложная тревога на здоровом, но нагруженном стенде;
//   - ответ не gRPC-статусом означает, что до разбора мы дошли, а разобрать не смогли
//     (например вердиктов пришло не столько, сколько задано вопросов): сосед отвечает
//     по другому контракту — повтор не поможет;
//   - метода нет, учётные данные не приняты, звать не разрешено, форма запроса
//     отвергнута — повтор не поможет ни при какой доступности.
//
// Остальные коды оставлены временными намеренно: ошибиться в эту сторону значит
// недосказать тревогу, ошибиться в другую — поднимать её на здоровом стенде и
// приучить оператора её игнорировать.
func provesMisconfiguration(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	s, ok := status.FromError(err)
	if !ok {
		return true
	}
	switch s.Code() {
	case codes.Unimplemented, codes.Unauthenticated, codes.PermissionDenied, codes.InvalidArgument:
		return true
	default:
		return false
	}
}

// cacheKey — ключ положительного вердикта видимости. Отношение в ключ НЕ входит:
// кешируется итоговое «видим» (viewer ∪ v_list), а не отдельная ветка союза.
func cacheKey(subject, resourceType, id string) string {
	return subject + "|" + resourceType + "|" + id
}

func (f *FGAFilter) getCache(subject, resourceType, id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	el, ok := f.cache[cacheKey(subject, resourceType, id)]
	if !ok {
		return false
	}
	e := el.Value.(*cacheEntry)
	if f.nowFn().After(e.expires) {
		f.lruLst.Remove(el)
		delete(f.cache, e.key)
		return false
	}
	f.lruLst.MoveToFront(el) // LRU touch
	return true
}

func (f *FGAFilter) putCache(subject, resourceType, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := cacheKey(subject, resourceType, id)
	exp := f.nowFn().Add(f.cfg.CacheTTL)
	if el, ok := f.cache[key]; ok {
		el.Value.(*cacheEntry).expires = exp
		f.lruLst.MoveToFront(el)
		return
	}
	el := f.lruLst.PushFront(&cacheEntry{key: key, expires: exp})
	f.cache[key] = el
	// Вытеснить LRU-tail пока перешагиваем bound.
	for f.lruLst.Len() > f.cfg.CacheMaxEntries {
		tail := f.lruLst.Back()
		if tail == nil {
			break
		}
		te := tail.Value.(*cacheEntry)
		f.lruLst.Remove(tail)
		delete(f.cache, te.key)
	}
}

// Size — текущий размер cache (observability/tests).
func (f *FGAFilter) Size() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lruLst.Len()
}
