// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
// Батчи внутри relation НЕЗАВИСИМЫ (per-object вопросы). Обходить их
// ПОСЛЕДОВАТЕЛЬНО — та самая регрессия, что поймали у соседей: на
// документированном максимуме страницы (validate.MaxPageSize = 1000) это
// ceil(1000/100) = 10 round-trip'ов подряд, каждый под своим per-call дедлайном.
// Стоило латентности пира перешагнуть per-call дедлайн, как ПЕРВЫЙ же батч ронял
// весь позитивный List в `Unavailable`; под параллельной нагрузкой это и
// происходило, на холостом стенде — нет.
//
// 5 выбрано из арифметики контракта, а не «на глаз»:
//   - 5 РОВНО делит максимум батчей (MaxPageSize/maxBatchCheckSize = 10) ⇒ на
//     предельной странице это ровно 2 полные волны, без рваного хвоста;
//   - глубина worst-case падает 10 → 2 волн (5×), поэтому per-call дедлайн можно
//     сделать РЕАЛИСТИЧНЫМ (см. DefaultConfig.Timeout), а не пилить его под число
//     последовательных хопов;
//   - всплеск на пира ограничен 5 одновременными BatchCheck (≤500 checks in-flight)
//     на запрос — предсказуемый малый множитель.
//
// Это НЕ ослабление проверки: спрашивается тот же предикат, теми же батчами ≤100,
// тот же fail-closed — меняется только порядок ожидания ответов.
const defaultParallelism = 5

// budgetHeadroom{Num,Den} — множитель бюджета операции над worst-case стеной
// (×3/2 ⇒ запас 33%). Бюджет обязан быть ПОТОЛКОМ, который в здоровом режиме не
// срабатывает: срабатывать должны per-call дедлайны.
const (
	budgetHeadroomNum = 3
	budgetHeadroomDen = 2
)

// visibilityRelations — отношения, союз которых и есть «видимость» страницы.
//
// Это РОВНО то отношение, на котором permission-catalog гейтит per-object чтение
// (`<Resource>.Get` → `viewer` на самом объекте; каталог — авторитет, его же
// зеркалит internal/check). Список storage отдаёт не идентификаторы, а ПОЛНЫЕ
// строки, поэтому «видно в списке» и «читается Get'ом» — один и тот же вопрос, и
// задаваться он обязан один раз.
//
// Прежде здесь стоял союз `viewer ∪ v_list`, и это был не расширенный доступ, а
// РАСХОЖДЕНИЕ: объект, на который у субъекта есть только `v_list`, попадал на
// страницу, а его же `Get` отвечал отказом — вызывающий узнавал идентификатор
// ресурса, который прочитать не может (оракул существования, выданный самим
// read-path'ом). Ничего при этом не терялось и по грантам: реконсайлер IAM на
// КАЖДЫЙ материализованный объект пишет, помимо `v_*`, back-compat tier-tuple
// (`viewer`/`editor`/`admin` — reconcile/tuples.go, «Always emitted»), а любое из
// трёх резолвит `viewer` по модели (`viewer: [...] or editor`, `editor: [...] or
// admin`). То есть `v_list` без резолвящегося `viewer` — не выданный доступ, а
// недоматериализованное/недоотозванное состояние, и показывать по нему нечего.
//
// Расширять этот набор — значит показывать в списке то, что Get не отдаст. Если
// когда-нибудь понадобится «видно в перечне, но без содержимого», это отдельная
// проекция ответа (усечённая строка), а не более широкий предикат видимости на
// полных строках.
var visibilityRelations = [...]string{"viewer"}

// Filter — port фильтра видимости. Реализация — *FGAFilter (через
// AuthorizeService.BatchCheck) либо nil (list-filter disabled / dev).
type Filter interface {
	// FilterVisibleIDs возвращает подмножество ids, видимое subject'у, СОХРАНЯЯ
	// порядок входа (страница уже отсортирована курсором — переупорядочивание
	// сломало бы пагинацию).
	//
	//   resourceType — FGA object type ("storage_volume", "storage_snapshot", …).
	//   action       — semantic permission ("storage.volumes.list"); передаётся в
	//                  kacho-iam для аудита/трассировки, решение принимает явный
	//                  required_relation (см. visibilityRelations).
	//   subject      — FGA subject string ("user:usr_alice" / "service_account:sva_x").
	//
	// err != nil → fail-closed: caller ОБЯЗАН пробросить ошибку, а не отдать
	// нефильтрованную страницу.
	FilterVisibleIDs(ctx context.Context, subject, resourceType, action string, ids []string) ([]string, error)
}

// ObjectGate — вопрос модели прав про ОДИН НАЗВАННЫЙ объект с ЯВНО указанным
// набором отношений (union: разрешено, если резолвится ЛЮБОЕ из них).
//
// # Зачем отдельный порт, а не Filter
//
// Filter отвечает на вопрос ВИДИМОСТИ страницы и пинит отношение сам
// (visibilityRelations — `viewer`, ровно то, что энфорсит Get). Здесь предмет
// другой: мутация называет объект ЧУЖОГО домена, и требуемое отношение задаёт
// вызывающий use-case (`v_update` на привязке, `v_update ∪ v_delete` на отвязке —
// см. actions.go). Подмешивать это в предикат видимости нельзя: он специально сужен
// до одного отношения, и его расширение показывало бы в списках то, чего Get не
// отдаст.
//
//   - err != nil → fail-closed: вызывающий ОБЯЗАН отказать, а не продолжить мутацию.
//     Недоступный ответ модели не есть ответ «да».
//   - Вердикт НЕ кешируется (в отличие от видимости): кеш Filter'а ключуется без
//     отношения — он хранит итог «видим», и положить туда вердикт про `v_update`
//     значило бы выдать один вопрос за другой. Плюс мутаций на порядки меньше, чем
//     строк списков, поэтому платить нечем, а окно залипания отзыва на мутации не
//     нужно вовсе.
type ObjectGate interface {
	AllowedOnObject(ctx context.Context, subject, resourceType, action string, relations []string, id string) (bool, error)
}

// Config — параметры FGAFilter.
type Config struct {
	// Enabled — master-switch. false → FilterVisibleIDs возвращает ids как есть
	// (нефильтрованный passthrough; per-RPC interceptor всё равно гейтит project-tier).
	Enabled bool
	// Timeout — per-call deadline ОДНОГО BatchCheck-вызова (architecture.md:
	// per-call deadline на КАЖДОМ внешнем вызове). НЕ бюджет операции — см.
	// OverallTimeout.
	Timeout time.Duration
	// Parallelism — сколько ≤100-id батчей ОДНОЙ relation летят одновременно.
	// 0 → defaultParallelism. Ограничивает fan-out worker-пулом; выше количества
	// батчей страницы не поднимается.
	Parallelism int
	// OverallTimeout — бюджет ВСЕЙ операции FilterVisibleIDs (все relations, все
	// волны батчей). 0 → выводится из Timeout и Parallelism так, чтобы worst-case
	// стена на предельной странице помещалась внутрь с запасом (см.
	// deriveOverallTimeout). Потолок нужен, потому что per-call дедлайн ограничивает
	// ОДИН хоп, а не их последовательность: без него деградировавший пир держал бы
	// List столько, сколько волн осталось.
	OverallTimeout time.Duration
	// CacheTTL — TTL одной положительной записи visibility-cache.
	CacheTTL time.Duration
	// CacheMaxEntries — bound для cache size (LRU-вытеснение).
	CacheMaxEntries int
	// FailOpen — на iam-ошибке: true → страница отдаётся нефильтрованной +
	// audit-warn; false → Unavailable (fail-closed, default — security.md).
	FailOpen bool
}

// DefaultConfig — sane defaults: фильтр включён, 1s per-call timeout, fan-out 5,
// выведенный бюджет операции, 5s TTL, 10000 entries, fail-closed.
//
// # Почему per-call дедлайн 1s, а не 500ms
//
// Это НЕ «подняли таймаут». Последовательная форма требовала, чтобы per-call
// дедлайн делился на ЧИСЛО ПОСЛЕДОВАТЕЛЬНЫХ ХОПОВ — 10 на предельной странице.
// Отсюда и брались 500ms: не потому, что столько отвечает здоровый пир, а потому,
// что больше «не помещалось». В итоге было худшее из двух: хрупкий хоп (пир под
// нагрузкой перешагивает 500ms → весь позитивный List в 503) И длинная
// НЕОГРАНИЧЕННАЯ агрегата.
//
// Bounded fan-out срезает глубину 10 → 2 волн, и это ОСВОБОЖДАЕТ бюджет под
// реалистичный per-call дедлайн: 2 × 1s = 2s worst-case стены против прежних
// 10 × 500ms = 5s. Допуск на ОДИН хоп вырос вдвое, а агрегатный worst-case УПАЛ и
// стал ОГРАНИЧЕННЫМ (бюджет 3s). Числа следуют из worstCaseDepth — при изменении
// предиката видимости пересчитываются там, а не здесь.
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
// (а) revoke залипал бы на TTL, (б) свежесозданный ресурс, чей owner-tuple ещё не
// материализовался, оставался бы невидимым весь TTL (read-your-writes). Промах по
// кешу стоит один элемент батча, а не отдельный round-trip.
//
// Eviction — LRU: при переполнении CacheMaxEntries вытесняется least-recently-used
// запись, а не произвольная (Go-map-randomized, возможно горячая).
type FGAFilter struct {
	cli AuthorizeClient
	cfg Config

	// logger — sink для audit-warn при fail-open degraded-mode. Инъектируется
	// (default slog.Default(), выставленный в composition root).
	logger *slog.Logger

	// now — источник времени для TTL-логики кеша. Инъектируется (default time.Now),
	// чтобы TTL-expiry проверялся детерминированно продвижением фейковых часов, а
	// не time.Sleep + wall-clock (флейк под нагрузкой CI).
	now func() time.Time

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
// Нормализует Parallelism и ВЫВОДИТ OverallTimeout, если тот не задан явно: бюджет
// операции — не независимая ручка, а ФУНКЦИЯ контракта (максимум страницы, предел
// батча) и per-call дедлайна. Так арифметика «сумма worst-case ожиданий помещается
// в бюджет» держится ПО ПОСТРОЕНИЮ, а не по надежде на когерентный config.
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

// WithLogger подменяет sink audit-warn'ов (composition root / white-box тесты).
func (f *FGAFilter) WithLogger(l *slog.Logger) *FGAFilter {
	if l != nil {
		f.logger = l
	}
	return f
}

// ceilDiv — целочисленное деление вверх (b > 0).
func ceilDiv(a, b int) int { return (a + b - 1) / b }

// worstCaseDepth — максимальное число ПОСЛЕДОВАТЕЛЬНЫХ round-trip'ов, которые
// FilterVisibleIDs может потратить на самой большой допустимой странице.
//
// Страница ≤ validate.MaxPageSize ⇒ ≤ ceil(MaxPageSize/maxBatchCheckSize) батчей на
// relation; батчи внутри relation идут волнами по Parallelism. Множитель
// len(visibilityRelations) остаётся в формуле намеренно: предикат видимости —
// одно отношение (см. visibilityRelations), и глубина обязана следовать за ним, а
// не за переписанной от руки константой.
func worstCaseDepth(parallelism int) int {
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}
	batches := ceilDiv(int(validate.MaxPageSize), maxBatchCheckSize)
	return len(visibilityRelations) * ceilDiv(batches, parallelism)
}

// deriveOverallTimeout — бюджет операции: worst-case стена (глубина × per-call
// дедлайн) плюс запас budgetHeadroom. Запас гарантирует, что в здоровом (пусть и
// медленном) режиме первым срабатывает per-call дедлайн, а бюджет остаётся потолком
// против деградировавшего пира.
func deriveOverallTimeout(perCall time.Duration, parallelism int) time.Duration {
	return time.Duration(worstCaseDepth(parallelism)) * perCall * budgetHeadroomNum / budgetHeadroomDen
}

// Parallelism — фактический bound fan-out'а (observability/tests).
func (f *FGAFilter) Parallelism() int { return f.cfg.Parallelism }

// Budget — фактический бюджет одной операции FilterVisibleIDs (observability/tests).
func (f *FGAFilter) Budget() time.Duration { return f.cfg.OverallTimeout }

// WorstCaseDepth — worst-case число последовательных round-trip'ов на предельной
// странице при текущем Parallelism (observability/tests).
func (f *FGAFilter) WorstCaseDepth() int { return worstCaseDepth(f.cfg.Parallelism) }

// FilterVisibleIDs — основной entry-point. См. Filter.
func (f *FGAFilter) FilterVisibleIDs(ctx context.Context, subject, resourceType, action string, ids []string) ([]string, error) {
	if f == nil || !f.cfg.Enabled || f.cli == nil {
		return ids, nil
	}
	if subject == "" {
		// Anonymous caller — fail-closed (use-case передаёт subject из Principal).
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

// AllowedOnObject — см. ObjectGate. Один round-trip: все отношения спрашиваются
// одним BatchCheck по одному и тому же объекту, разрешено при первом «да».
//
// Fail-open здесь НЕ применяется, и это не упущение. Ручка fail-open — осознанный
// operator-размен на ЧТЕНИИ (страница отдаётся нефильтрованной + audit-WARN). На
// мутации, которая пишет строку в набор ЧУЖОГО ресурса, «продолжить, потому что
// модель не ответила» защитимого прочтения не имеет: недоступность iam не делает
// вызывающего владельцем названной машины.
func (f *FGAFilter) AllowedOnObject(
	ctx context.Context,
	subject, resourceType, action string,
	relations []string,
	id string,
) (bool, error) {
	if f == nil || !f.cfg.Enabled || f.cli == nil {
		// Порт есть, спросить негде. Это состояние посадки, а не ответ модели, —
		// поэтому отказ, а не «да» (production boot-guard такую посадку запрещает:
		// config.Validate требует ListFilterEnabled=true и адрес authorize-эндпоинта).
		return false, status.Error(codes.PermissionDenied,
			"instance gate: the rights model is not configured for this deployment")
	}
	if subject == "" {
		return false, status.Error(codes.Unauthenticated, "instance gate: subject required")
	}
	if resourceType == "" || action == "" || id == "" || len(relations) == 0 {
		return false, fmt.Errorf("authzfilter: resourceType, action, id and at least one relation required")
	}

	checks := make([]*iamv1.AuthorizeCheckRequest, 0, len(relations))
	for _, relation := range relations {
		checks = append(checks, &iamv1.AuthorizeCheckRequest{
			Subject:          subject,
			Resource:         &iamv1.ResourceRef{Type: resourceType, Id: id},
			Action:           action,
			RequiredRelation: relation,
		})
	}
	resp, cerr := f.batchCheckOnce(ctx, &iamv1.BatchAuthorizeCheckRequest{Checks: checks})
	if cerr != nil {
		return false, gateErr(cerr, f.cfg.Timeout)
	}
	// Тот же контракт длины, что у страничного пути: расхождение — fail-closed
	// ошибка, а не «считаем отказом», иначе вердикт одного отношения выдавался бы
	// за другой.
	if len(resp.GetResponses()) != len(checks) {
		return false, status.Errorf(codes.Unavailable,
			"instance gate: BatchCheck returned %d responses for %d checks",
			len(resp.GetResponses()), len(checks))
	}
	for _, r := range resp.GetResponses() {
		if r.GetAllowed() {
			return true, nil
		}
	}
	return false, nil
}

// gateErr — peer-ошибка вопроса про объект → Unavailable с названным числом
// (по ошибке ctx не различить per-call дедлайн от дедлайна вызывающего).
// pgx/SQL-текста здесь нет by construction: источник — gRPC-status iam.
func gateErr(err error, perCall time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Errorf(codes.Unavailable,
			"instance gate: AuthorizeService.BatchCheck deadline exceeded (per-call %s)", perCall)
	}
	if s, ok := status.FromError(err); ok && s.Code() != codes.OK && s.Code() != codes.Unknown {
		return status.Errorf(codes.Unavailable, "instance gate: AuthorizeService.BatchCheck %s: %s", s.Code(), s.Message())
	}
	return status.Errorf(codes.Unavailable, "instance gate: AuthorizeService.BatchCheck: %v", err)
}

// batchVerdicts — вердикты ОДНОГО батча. Каждая горутина пишет в СВОЙ элемент среза
// (разные ячейки — не гонка), а склейка идёт по индексу батча, а не по порядку
// завершения: разбиение allowed/denied обязано быть ДЕТЕРМИНИРОВАННЫМ независимо от
// того, кто ответил первым.
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
// ≤ maxBatchCheckSize. Возвращает разрешённые и отказанные (для следующего отношения).
//
// Батчи НЕЗАВИСИМЫ (per-object вопросы), поэтому идут ОГРАНИЧЕННЫМ fan-out'ом
// (worker-пул на Parallelism), а не по одному: последовательный обход давал на
// предельной странице до 20 round-trip'ов подряд и вынуждал держать per-call дедлайн
// неестественно узким (см. defaultParallelism / DefaultConfig).
//
// Каждый батч по-прежнему идёт под СОБСТВЕННЫМ per-call deadline (batchCheckOnce,
// architecture.md), а вся операция — внутри бюджета FilterVisibleIDs.
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

	// Одиночный батч (страница ≤100) не платит за пул — общий случай остаётся ровно
	// тем же одним round-trip'ом.
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
	// зафиксированной ошибки означает, что истёк РОДИТЕЛЬСКИЙ ctx (бюджет операции
	// или дедлайн вызывающего) посреди страницы. Сообщаем об этом, а не отдаём
	// недорешённый набор за ответ.
	if cerr := ctx.Err(); cerr != nil {
		return nil, nil, cerr
	}
	return joinVerdicts(results, len(ids))
}

// joinVerdicts склеивает вердикты в порядке БАТЧЕЙ (не завершения) — результат не
// зависит от того, какая горутина ответила первой.
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
	// Контракт BatchCheck: responses в порядке checks и той же длины. Расхождение —
	// не «считаем отказом», а fail-closed ошибка: молчаливое смещение индексов
	// выдало бы вердикт одного объекта за другой.
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
		// Degraded mode: iam недоступен, но оператор явно выбрал fail-open, поэтому
		// per-object проверка обходится (вся страница становится видимой). Это ровно
		// тот класс over-show, от которого защищает list-filter — он ОБЯЗАН быть
		// громким, а не тихой authz-деградацией. err — peer gRPC status (без
		// pgx/SQL-текста), пишется только server-side, наружу не возвращается.
		f.logger.Warn("list filter fail-open: iam.BatchCheck unreachable, bypassing per-object authz (returning the UNFILTERED page)",
			"error", err)
		return ids, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		// Оба числа названы явно: сработать мог как per-call дедлайн, так и бюджет
		// операции, и по ошибке ctx их не различить.
		return nil, status.Errorf(codes.Unavailable,
			"list filter: AuthorizeService.BatchCheck deadline exceeded (per-call %s, operation budget %s)",
			f.cfg.Timeout, f.cfg.OverallTimeout)
	}
	if s, ok := status.FromError(err); ok && s.Code() != codes.OK && s.Code() != codes.Unknown {
		return nil, status.Errorf(codes.Unavailable, "list filter: AuthorizeService.BatchCheck %s: %s", s.Code(), s.Message())
	}
	return nil, status.Errorf(codes.Unavailable, "list filter: AuthorizeService.BatchCheck: %v", err)
}

// cacheKey — ключ положительного вердикта видимости. Отношение в ключ НЕ входит:
// кешируется итоговый вердикт «видим», а не отдельная ветка предиката.
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
	if f.now().After(e.expires) {
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
	exp := f.now().Add(f.cfg.CacheTTL)
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
