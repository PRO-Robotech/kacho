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
)

// maxBatchCheckSize — контрактный предел AuthorizeService.BatchCheck
// (>100 → InvalidArgument). Батчи режутся по нему.
const maxBatchCheckSize = 100

// visibilityRelations — отношения, объединение которых и есть «видимость» в
// List/Get: `viewer` (read-tier: direct usersets ∪ editor ∪ admin) и `v_list`
// (object-only selector-грант «видеть в списке без содержимого»). Тот же союз,
// что делал AuthorizeService.ListObjects — порядок значим только для стоимости
// (`viewer` покрывает подавляющее большинство, `v_list` добирает остаток).
var visibilityRelations = [...]string{"viewer", "v_list"}

// Filter — port фильтра видимости. Реализация — *FGAFilter (через
// AuthorizeService.BatchCheck) либо nil (list-filter disabled / dev).
type Filter interface {
	// FilterVisibleIDs возвращает подмножество ids, видимое subject'у, СОХРАНЯЯ
	// порядок входа (страница уже отсортирована курсором — переупорядочивание
	// сломало бы пагинацию).
	//
	//   resourceType — FGA object type ("compute_instance", "compute_disk", …).
	//   action       — semantic permission ("compute.disks.list"); передаётся в
	//                  kacho-iam для аудита/трассировки, решение принимает
	//                  явный required_relation (см. visibilityRelations).
	//   subject      — FGA subject string ("user:usr_alice" / "service_account:sva_x").
	//
	// err != nil → fail-closed: caller ОБЯЗАН пробросить ошибку, а не отдать
	// нефильтрованную страницу.
	FilterVisibleIDs(ctx context.Context, subject, resourceType, action string, ids []string) ([]string, error)
}

// Config — параметры FGAFilter.
type Config struct {
	// Enabled — master-switch. false → FilterVisibleIDs возвращает ids как есть
	// (нефильтрованный passthrough; per-RPC interceptor всё равно гейтит).
	Enabled bool
	// Timeout — per-request deadline ОДНОГО BatchCheck-вызова.
	Timeout time.Duration
	// CacheTTL — TTL одной положительной записи visibility-cache.
	CacheTTL time.Duration
	// CacheMaxEntries — bound для cache size (LRU-вытеснение).
	CacheMaxEntries int
	// FailOpen — на iam-ошибке: true → страница отдаётся нефильтрованной +
	// audit-warn; false → Unavailable (fail-closed, default — security.md).
	FailOpen bool
}

// DefaultConfig — sane defaults: фильтр включён, 500ms timeout, 5s TTL, 10000
// entries, fail-closed.
func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		Timeout:         500 * time.Millisecond,
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

// FGAFilter — продакшен-реализация Filter поверх AuthorizeService.BatchCheck
// с in-memory TTL+LRU-кешем ПОЛОЖИТЕЛЬНЫХ вердиктов.
//
// Кешируются только «видим» (как в pkg/authz/cache.go): отрицательный вердикт
// никогда не кешируется, иначе (а) revoke залипал бы на TTL, (б) свежесозданный
// ресурс, чей owner-tuple ещё не материализовался, оставался бы невидимым весь
// TTL (read-your-writes). Промах по кешу стоит один элемент батча, а не
// отдельный round-trip.
//
// Eviction — LRU: при переполнении CacheMaxEntries вытесняется
// least-recently-used entry, а не произвольная (Go-map-randomized, возможно
// горячая) запись.
type FGAFilter struct {
	cli AuthorizeClient
	cfg Config

	// logger — sink для audit-warn при fail-open degraded-mode. Инъектируется
	// (default slog.Default(), выставленный в cmd composition root) —
	// перекрывается в white-box тестах для перехвата WARN, как и now.
	logger *slog.Logger

	// now — источник времени для TTL-логики кеша. Инъектируется (default
	// time.Now) чтобы TTL-expiry можно было проверять детерминированно, продвигая
	// фейковые часы, а не через time.Sleep + wall-clock (flaky под нагрузкой CI).
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
func NewFGAFilter(cli AuthorizeClient, cfg Config) *FGAFilter {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 500 * time.Millisecond
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

// FilterVisibleIDs — основной entry-point. См. Filter.
func (f *FGAFilter) FilterVisibleIDs(ctx context.Context, subject, resourceType, action string, ids []string) ([]string, error) {
	if f == nil || !f.cfg.Enabled || f.cli == nil {
		return ids, nil
	}
	if subject == "" {
		// Anonymous caller — fail-closed (handler передаёт subject из Principal).
		return nil, status.Error(codes.Unauthenticated, "list filter: subject required")
	}
	if resourceType == "" || action == "" {
		return nil, fmt.Errorf("authzfilter: resourceType and action required")
	}
	if len(ids) == 0 {
		return nil, nil
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

// checkRelation спрашивает kacho-iam об ОДНОМ отношении для набора ids батчами
// ≤ maxBatchCheckSize. Возвращает разрешённые и отказанные (для следующего
// отношения). Каждый батч идёт под собственным per-call deadline
// (architecture.md: per-call deadline на КАЖДОМ внешнем вызове) — единый таймаут
// на всю серию батчей спуриозно резал бы здоровый multi-batch peer.
func (f *FGAFilter) checkRelation(
	ctx context.Context,
	subject, resourceType, action, relation string,
	ids []string,
) (allowed, denied []string, err error) {
	allowed = make([]string, 0, len(ids))
	denied = make([]string, 0)
	for start := 0; start < len(ids); start += maxBatchCheckSize {
		end := start + maxBatchCheckSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]

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
			return nil, nil, cerr
		}
		// Контракт BatchCheck: responses в порядке checks и той же длины.
		// Расхождение — не «считаем отказом», а fail-closed ошибка: молчаливое
		// смещение индексов выдало бы вердикт одного объекта за другой.
		if len(resp.GetResponses()) != len(batch) {
			return nil, nil, fmt.Errorf("authzfilter: BatchCheck returned %d responses for %d checks",
				len(resp.GetResponses()), len(batch))
		}
		for i, r := range resp.GetResponses() {
			if r.GetAllowed() {
				allowed = append(allowed, batch[i])
			} else {
				denied = append(denied, batch[i])
			}
		}
	}
	return allowed, denied, nil
}

// batchCheckOnce делает ровно один BatchCheck под собственным per-call deadline.
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
		return nil, status.Errorf(codes.Unavailable, "list filter: AuthorizeService.BatchCheck deadline exceeded after %s", f.cfg.Timeout)
	}
	if s, ok := status.FromError(err); ok && s.Code() != codes.OK && s.Code() != codes.Unknown {
		return nil, status.Errorf(codes.Unavailable, "list filter: AuthorizeService.BatchCheck %s: %s", s.Code(), s.Message())
	}
	return nil, status.Errorf(codes.Unavailable, "list filter: AuthorizeService.BatchCheck: %v", err)
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
