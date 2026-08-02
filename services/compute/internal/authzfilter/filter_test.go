// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
)

// fakeAuthorizeClient — stub kacho-iam AuthorizeService.BatchCheck.
//
// `visible` — какие (relation, id) разрешены. Отвечает строго в порядке checks,
// как того требует контракт BatchCheck.
// Счётчики под mu — фильтр законно зовёт BatchCheck из нескольких горутин
// (конкурентные страницы), и незащищённый счётчик в САМОМ стабе давал бы
// ложный -race-репорт про тестовый код.
type fakeAuthorizeClient struct {
	mu      sync.Mutex
	visible map[string]map[string]bool // relation → id → allowed
	err     error
	sleep   time.Duration

	calls     int
	checked   int
	batchSize []int
	gotReqs   []*iamv1.AuthorizeCheckRequest

	// inFlight/maxInFlight — наблюдаемая конкурентность, чтобы тест мог
	// доказать, что fan-out батчей действительно ОГРАНИЧЕН пулом.
	inFlight    int
	maxInFlight int
}

func newFakeAuthorizeClient() *fakeAuthorizeClient {
	return &fakeAuthorizeClient{visible: map[string]map[string]bool{}}
}

// snapshot — согласованный слепок счётчиков (стаб зовётся конкурентно).
func (f *fakeAuthorizeClient) snapshot() (calls, checked, maxInFlight int, batchSize []int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.checked, f.maxInFlight, append([]int(nil), f.batchSize...)
}

func (f *fakeAuthorizeClient) allow(relation string, ids ...string) *fakeAuthorizeClient {
	if f.visible[relation] == nil {
		f.visible[relation] = map[string]bool{}
	}
	for _, id := range ids {
		f.visible[relation][id] = true
	}
	return f
}

func (f *fakeAuthorizeClient) BatchCheck(ctx context.Context, in *iamv1.BatchAuthorizeCheckRequest, _ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	f.mu.Lock()
	f.calls++
	f.batchSize = append(f.batchSize, len(in.GetChecks()))
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	sleep, cerr := f.sleep, f.err
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()

	if sleep > 0 {
		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if cerr != nil {
		return nil, cerr
	}
	if n := len(in.GetChecks()); n > maxBatchCheckSize {
		return nil, status.Errorf(codes.InvalidArgument, "Illegal argument checks: batch size %d > %d", n, maxBatchCheckSize)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	out := &iamv1.BatchAuthorizeCheckResponse{
		Responses: make([]*iamv1.AuthorizeCheckResponse, 0, len(in.GetChecks())),
	}
	for _, c := range in.GetChecks() {
		f.checked++
		f.gotReqs = append(f.gotReqs, c)
		out.Responses = append(out.Responses, &iamv1.AuthorizeCheckResponse{
			Allowed: f.visible[c.GetRequiredRelation()][c.GetResource().GetId()],
		})
	}
	return out, nil
}

// Страница фильтруется per-object, порядок входа (курсора) сохраняется, а запрос
// несёт явный required_relation и только id страницы.
func TestFGAFilter_FiltersPageAndPreservesOrder(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("v_get", "c", "a")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"c", "a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"c", "a"}, got,
		"visible subset must keep the caller's (cursor) order — re-sorting would break pagination")

	require.NotEmpty(t, cli.gotReqs)
	assert.Equal(t, "user:usr_x", cli.gotReqs[0].GetSubject())
	assert.Equal(t, ResourceTypeInstance, cli.gotReqs[0].GetResource().GetType())
	assert.Equal(t, ActionInstanceRead, cli.gotReqs[0].GetAction())
	assert.Equal(t, "v_get", cli.gotReqs[0].GetRequiredRelation())
}

// Предикат видимости — ОДНО отношение, то же, которым гейтится Get.
// Прежде здесь стояли два теста, закреплявшие союз `viewer ∪ v_list` и
// стоимость его второй фазы. Оба утверждали предикат, который расходился с
// чтением, — их предмет снят вместе с союзом. Обе половины (v_list-only НЕ
// впускает объект; спрашивается ровно одно отношение, второй фазы нет)
// проверяются в read_parity_test.go.

// Батчи режутся по контрактному пределу BatchCheck (≤100) — большая страница не
// роняет запрос в InvalidArgument.
func TestFGAFilter_BatchesRespectHardCap(t *testing.T) {
	ids := make([]string, 0, 250)
	cli := newFakeAuthorizeClient()
	for i := 0; i < 250; i++ {
		id := fmt.Sprintf("epd_%03d", i)
		ids = append(ids, id)
		cli.allow("v_get", id)
	}
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead, ids)
	require.NoError(t, err)
	assert.Len(t, got, 250)
	for _, n := range cli.batchSize {
		assert.LessOrEqual(t, n, maxBatchCheckSize, "each BatchCheck must respect the ≤100 contract")
	}
	assert.Equal(t, 3, cli.calls, "250 ids → 3 batches (100+100+50)")
}

// Стоимость пропорциональна СТРАНИЦЕ: сколько id пришло, столько и проверок
// (никакого перечисления вселенной — источник cap-дефекта).
func TestFGAFilter_CostProportionalToPage(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("v_get", "a", "b", "c")
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a", "b", "c"})
	require.NoError(t, err)
	assert.Equal(t, 3, cli.checked)
}

// Дубликаты во входе не оплачиваются дважды и не дублируются в выдаче.
func TestFGAFilter_DeduplicatesInput(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("v_get", "a")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a", "a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, got, "a duplicated id appears once in the visible subset")
	// Одна relation (предикат страницы = отношение чтения): {a,b} = 2 проверки.
	// Дублированный "a" оплачивается ОДИН раз (3 означало бы, что дедупа нет).
	assert.Equal(t, 2, cli.checked, "duplicate id must be checked once")
}

func TestFGAFilter_EmptyPageNoCall(t *testing.T) {
	cli := newFakeAuthorizeClient()
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, 0, cli.calls, "empty page → no authz round-trip")
}

// Empty grant → nothing visible (NOT error, NOT the whole page).
func TestFGAFilter_NothingVisible(t *testing.T) {
	cli := newFakeAuthorizeClient()
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a", "b"})
	require.NoError(t, err)
	assert.Empty(t, got, "no grant → nothing visible (no-leak)")
}

func TestFGAFilter_FailClosed(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = status.Error(codes.Unavailable, "iam down")
	f := NewFGAFilter(cli, DefaultConfig()) // FailOpen=false

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err), "fail-closed maps an iam error → Unavailable")
}

// Sanity: upstream PermissionDenied is wrapped as Unavailable (a filter-side
// transport failure is never surfaced as the caller's own 403).
func TestFGAFilter_PreservesCodes(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = status.Error(codes.PermissionDenied, "no")
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{"a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// Generic (non-status) errors → Unavailable wrap.
func TestFGAFilter_GenericErrWrapsUnavailable(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = errors.New("boom")
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{"a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestFGAFilter_FailOpenReturnsPage(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = errors.New("boom")
	cfg := DefaultConfig()
	cfg.FailOpen = true
	f := NewFGAFilter(cli, cfg)

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got, "fail-open → unfiltered page (degraded mode)")
}

// Regression: fail-open degraded-mode MUST emit an audit WARN. handleErr returns
// the UNFILTERED page (every row of the page becomes visible, bypassing the
// per-object check) — doc.go and the Config.FailOpen godoc both promise an
// audit-warn. Without an emitted WARN an operator who enabled fail-open gets a
// silently authz-degraded mode with zero observability. Lock the observable (a
// WARN is produced), not just the returned page.
func TestFGAFilter_FailOpenEmitsAuditWarn(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = status.Error(codes.Unavailable, "iam down")
	cfg := DefaultConfig()
	cfg.FailOpen = true
	f := NewFGAFilter(cli, cfg)

	// White-box capture of the audit sink, mirroring the f.now injection used by
	// the TTL tests. Level threshold WARN so an accidental Info would not pass.
	var buf bytes.Buffer
	f.logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a"})
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, got)

	logged := buf.String()
	require.NotEmpty(t, logged, "fail-open: expected an audit WARN, got no log output (degraded authz mode is silent)")
	assert.Contains(t, logged, "level=WARN", "fail-open: audit log must be WARN level")
	assert.Contains(t, logged, "fail-open", "fail-open: audit log must identify the fail-open bypass")
}

// Контракт BatchCheck: ответов столько же и в том же порядке. Расхождение —
// fail-closed ошибка, а не «считаем отказом»: сдвиг индексов приписал бы вердикт
// одного объекта другому.
func TestFGAFilter_ResponseLengthMismatchFailsClosed(t *testing.T) {
	f := NewFGAFilter(shortResponseClient{}, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a", "b"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

type shortResponseClient struct{}

func (shortResponseClient) BatchCheck(_ context.Context, _ *iamv1.BatchAuthorizeCheckRequest, _ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	return &iamv1.BatchAuthorizeCheckResponse{
		Responses: []*iamv1.AuthorizeCheckResponse{{Allowed: true}}, // 1 for 2 checks
	}, nil
}

func TestFGAFilter_AnonymousFailClosed(t *testing.T) {
	cli := newFakeAuthorizeClient()
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "", ResourceTypeInstance, ActionInstanceRead, []string{"a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Equal(t, 0, cli.calls, "anonymous → no FGA call")
}

// Per-call deadline is enforced on EVERY BatchCheck: the fake honours ctx.Done(),
// so if the 10ms deadline fires before the 100ms sleep completes it returns
// ctx.Err() → Unavailable. Were the timeout NOT applied, the fake would sleep the
// full 100ms and answer successfully — proving enforcement deterministically,
// without a flaky wall-clock bound.
func TestFGAFilter_TimeoutEnforced(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("v_get", "a")
	cli.sleep = 100 * time.Millisecond
	cfg := DefaultConfig()
	cfg.Timeout = 10 * time.Millisecond
	f := NewFGAFilter(cli, cfg)

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{"a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// Кешируются ТОЛЬКО положительные вердикты: повторный запрос про видимый id не
// стоит round-trip'а, а про невидимый — перепроверяется. Отрицательный кеш
// залипал бы и на revoke (грант снят, а доступ жив), и на read-your-writes
// (owner-tuple свежесозданного ресурса ещё не материализовался → ресурс был бы
// невидим весь TTL).
func TestFGAFilter_CachesPositiveOnly(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("v_get", "a")
	f := NewFGAFilter(cli, DefaultConfig())
	ctx := context.Background()

	_, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{"a", "b"})
	require.NoError(t, err)
	firstChecked := cli.checked
	require.Positive(t, firstChecked)

	_, err = f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{"a", "b"})
	require.NoError(t, err)

	var reAskedA, reAskedB bool
	for _, r := range cli.gotReqs[firstChecked:] {
		switch r.GetResource().GetId() {
		case "a":
			reAskedA = true
		case "b":
			reAskedB = true
		}
	}
	assert.False(t, reAskedA, "positive verdict is cached — no re-ask")
	assert.True(t, reAskedB, "negative verdict must NOT be cached — a fresh grant must be seen")
}

// Кеш ключуется по (subject, resourceType, id): вердикт одного subject'а НЕ
// подставляется другому (иначе — cross-tenant over-show).
func TestFGAFilter_CacheKeyedBySubjectAndType(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("v_get", "a")
	f := NewFGAFilter(cli, DefaultConfig())
	ctx := context.Background()

	got, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{"a"})
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, got)

	// Другой subject на тот же id — своё решение (fake разрешает "a" любому, но
	// запрос ОБЯЗАН уйти: кеш чужого вердикта переиспользовать нельзя).
	before := cli.checked
	_, err = f.FilterVisibleIDs(ctx, "user:usr_y", ResourceTypeInstance, ActionInstanceRead, []string{"a"})
	require.NoError(t, err)
	assert.Greater(t, cli.checked, before, "another subject must not reuse the first subject's verdict")

	// Тот же subject, другой resourceType — тоже отдельный вердикт. Фильтр
	// обращается с resourceType как с непрозрачной строкой (кладёт её в ключ кеша
	// и в запрос, ничего о ней не утверждая), поэтому ключ проверяется ЛЮБЫМ
	// другим типом. compute своих других типов больше не имеет — Disk/Image/
	// Snapshot сняты вместе с дублем блочного хранения, — так что здесь взят
	// реальный тип домена-владельца.
	before = cli.checked
	_, err = f.FilterVisibleIDs(ctx, "user:usr_x", "storage_volume", ActionInstanceRead, []string{"a"})
	require.NoError(t, err)
	assert.Greater(t, cli.checked, before, "another resource type must not reuse the verdict")
}

// TTL-expiry: положительный вердикт живёт CacheTTL, затем перепроверяется (revoke
// становится виден ≤ CacheTTL). Часы фейковые — детерминированно, без time.Sleep.
func TestFGAFilter_CacheTTLExpiry(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("v_get", "a")
	cfg := DefaultConfig()
	cfg.CacheTTL = 25 * time.Millisecond
	f := NewFGAFilter(cli, cfg)
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	f.now = clk.now
	ctx := context.Background()

	_, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{"a"})
	require.NoError(t, err)
	require.Equal(t, 1, cli.calls)

	// Внутри TTL — cache hit, round-trip'а нет.
	clk.advance(10 * time.Millisecond)
	_, err = f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{"a"})
	require.NoError(t, err)
	require.Equal(t, 1, cli.calls, "within TTL the positive verdict is served from cache")

	// За TTL — перепроверка. Грант к этому моменту отозван → id пропадает.
	// Relation одна, поэтому переспрос — ровно один вызов.
	clk.advance(40 * time.Millisecond)
	cli.visible["v_get"]["a"] = false
	got, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{"a"})
	require.NoError(t, err)
	assert.Equal(t, 2, cli.calls, "past TTL the verdict must be re-asked")
	assert.Empty(t, got, "a revoked grant must stop being visible once the TTL elapses")
}

// LRU: при переполнении вытесняется least-recently-used, а не произвольная
// (Go-map-randomized, возможно горячая) запись — иначе burst distinct-List
// трэшил бы кеш и гнал лишний QPS в kacho-iam.
func TestFGAFilter_LRUEvictsLeastRecentlyUsed(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("v_get", "hot")
	cfg := DefaultConfig()
	cfg.CacheMaxEntries = 10
	f := NewFGAFilter(cli, cfg)
	ctx := context.Background()

	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("cold_%d", i)
		cli.allow("v_get", id)
		_, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{id})
		require.NoError(t, err)
	}
	_, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{"hot"})
	require.NoError(t, err)
	require.Equal(t, 10, f.Size())

	for i := 100; i < 200; i++ {
		before := cli.checked
		_, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{"hot"})
		require.NoError(t, err)
		require.Equal(t, before, cli.checked,
			"recently-used hot entry must stay cached across overflow (LRU, not random eviction)")

		id := fmt.Sprintf("cold_%d", i)
		cli.allow("v_get", id)
		_, err = f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeInstance, ActionInstanceRead, []string{id})
		require.NoError(t, err)
	}
	assert.Equal(t, 10, f.Size(), "cache stays bounded at CacheMaxEntries")
}

// Cache-size тюнинг НЕ усекает видимость: страница любой длины проверяется целиком
// даже при крошечном кеше (прежний ListObjects-путь имел отдельный result-cap,
// который при связывании с размером кеша молча резал хвост allow-list'а).
func TestFGAFilter_CacheSizeDoesNotTruncateVisibility(t *testing.T) {
	ids := make([]string, 0, 120)
	cli := newFakeAuthorizeClient()
	for i := 0; i < 120; i++ {
		id := fmt.Sprintf("epd_%03d", i)
		ids = append(ids, id)
		cli.allow("v_get", id)
	}
	cfg := DefaultConfig()
	cfg.CacheMaxEntries = 3 // operator tuned cache down for memory
	f := NewFGAFilter(cli, cfg)

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead, ids)
	require.NoError(t, err)
	assert.Len(t, got, 120, "every granted id on the page must stay visible regardless of cache sizing")
	assert.LessOrEqual(t, f.Size(), cfg.CacheMaxEntries, "cache still respects its bound")
}

// Passthrough: nil-client (graceful start без iam) и Enabled=false.
func TestFGAFilter_DisabledOrNilClientPassthrough(t *testing.T) {
	f := NewFGAFilter(nil, DefaultConfig())
	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got)

	cli := newFakeAuthorizeClient()
	cfg := DefaultConfig()
	cfg.Enabled = false
	f2 := NewFGAFilter(cli, cfg)
	got2, err := f2.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, got2)
	assert.Equal(t, 0, cli.calls)
}

// Конкурентные страницы одного subject'а не гоняют кеш в race (детект под -race).
func TestFGAFilter_ConcurrentPagesRaceFree(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("v_get", "a", "b", "c")
	f := NewFGAFilter(cli, DefaultConfig())

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
				[]string{"a", "b", "c"})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
}

// fakeClock — детерминированный источник времени для TTL-тестов кеша.
// Заменяет f.now, чтобы TTL-expiry продвигался логически (advance), а не через
// time.Sleep + wall-clock (flaky под нагрузкой CI).
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
