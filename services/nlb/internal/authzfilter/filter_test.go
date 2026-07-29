// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"
	"errors"
	"fmt"
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

// Unit tests for the kacho-nlb per-object list-filter (RBAC). No network: a fake
// kacho-iam AuthorizeService answers BatchCheck from a programmed grant set.

// fakeAuthorizeClient — stub iam AuthorizeService.BatchCheck.
//
// `visible` — какие (relation, id) разрешены. Отвечает строго в порядке checks,
// как того требует контракт BatchCheck.
// Счётчики под mu — фильтр законно зовёт BatchCheck из нескольких горутин
// (батчи одной relation фанятся bounded-пулом, см. filter.go), и незащищённый
// счётчик в САМОМ стабе давал бы ложный -race-репорт про тестовый код.
// inFlight/maxInFlight — наблюдаемая конкурентность, чтобы тест мог доказать,
// что fan-out действительно ОГРАНИЧЕН.
type fakeAuthorizeClient struct {
	mu      sync.Mutex
	visible map[string]map[string]bool // relation → id → allowed
	err     error
	sleep   time.Duration

	calls       int
	checked     int
	batchSize   []int
	gotReqs     []*iamv1.AuthorizeCheckRequest
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

// Видимое подмножество страницы сохраняет порядок курсора; запрос — per-object,
// с явной relation, только по id страницы.
func TestFGAFilter_FiltersPageAndPreservesOrder(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "nlb-c", "nlb-a")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-c", "nlb-a", "nlb-b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"nlb-c", "nlb-a"}, got,
		"visible subset must keep the caller's (cursor) order — re-sorting would break pagination")

	require.NotEmpty(t, cli.gotReqs)
	assert.Equal(t, "user:usr_alice", cli.gotReqs[0].GetSubject())
	assert.Equal(t, ResourceTypeLoadBalancer, cli.gotReqs[0].GetResource().GetType())
	assert.Equal(t, ActionLoadBalancerList, cli.gotReqs[0].GetAction())
	assert.Equal(t, "viewer", cli.gotReqs[0].GetRequiredRelation())
}

// Предикат видимости — тот же союз, что делал ListObjects: viewer ∪ v_list.
// v_list-only грант («видеть в списке без содержимого») обязан оставаться видимым.
func TestFGAFilter_ViewerUnionVList(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "nlb-a").allow("v_list", "nlb-b")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeListener, ActionListenerList, []string{"nlb-a", "nlb-b", "nlb-c"})
	require.NoError(t, err)
	assert.Equal(t, []string{"nlb-a", "nlb-b"}, got, "viewer ∪ v_list")
}

// v_list спрашивается ТОЛЬКО про тех, кому отказал viewer (стоимость).
func TestFGAFilter_VListOnlyForViewerDenied(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "nlb-a", "nlb-b")
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a", "nlb-b", "nlb-c"})
	require.NoError(t, err)

	var vlist int
	for _, r := range cli.gotReqs {
		if r.GetRequiredRelation() == "v_list" {
			vlist++
		}
	}
	assert.Equal(t, 1, vlist, "only the viewer-denied id should be re-asked on v_list")
}

// Батчи режутся по контрактному пределу BatchCheck (≤100) — большая страница не
// роняет запрос в InvalidArgument.
func TestFGAFilter_BatchesRespectHardCap(t *testing.T) {
	ids := make([]string, 0, 250)
	cli := newFakeAuthorizeClient()
	for i := 0; i < 250; i++ {
		id := fmt.Sprintf("nlb-%03d", i)
		ids = append(ids, id)
		cli.allow("viewer", id)
	}
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, ids)
	require.NoError(t, err)
	assert.Len(t, got, 250)
	for _, n := range cli.batchSize {
		assert.LessOrEqual(t, n, maxBatchCheckSize, "each BatchCheck must respect the ≤100 contract")
	}
	assert.Equal(t, 3, cli.calls, "250 ids → 3 batches (100+100+50)")
}

// Стоимость пропорциональна СТРАНИЦЕ: сколько id пришло, столько и проверок
// (никакого перечисления вселенной — источник ListObjects-усечения).
func TestFGAFilter_CostProportionalToPage(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "nlb-a", "nlb-b", "nlb-c")
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeTargetGroup, ActionTargetGroupList, []string{"nlb-a", "nlb-b", "nlb-c"})
	require.NoError(t, err)
	assert.Equal(t, 3, cli.checked)
}

func TestFGAFilter_EmptyPageNoCall(t *testing.T) {
	cli := newFakeAuthorizeClient()
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, 0, cli.calls, "empty page → no authz round-trip")
}

// no-leak: без гранта не видно ничего (пустая выдача, НЕ вся страница).
func TestFGAFilter_NothingVisible(t *testing.T) {
	cli := newFakeAuthorizeClient()
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_no_grants",
		ResourceTypeListener, ActionListenerList, []string{"nlb-a", "nlb-b"})
	require.NoError(t, err)
	assert.Empty(t, got, "no grant → nothing visible (no-leak)")
}

// fail-closed (default): iam error → Unavailable, НЕ нефильтрованная страница.
func TestFGAFilter_FailClosed(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = status.Error(codes.Internal, "fga boom")
	f := NewFGAFilter(cli, DefaultConfig()) // FailOpen=false

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err), "fail-closed maps FGA error → Unavailable")
}

// upstream gRPC code (напр. PermissionDenied) тоже заворачивается в Unavailable —
// фильтр никогда не отдаёт наружу код, который List-хендлер принял бы за вердикт.
func TestFGAFilter_WrapsUpstreamCodeAsUnavailable(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = status.Error(codes.PermissionDenied, "no")
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// non-status (generic) ошибки — тоже Unavailable.
func TestFGAFilter_GenericErrWrapsUnavailable(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = errors.New("boom")
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// fail-open (configured): на iam-ошибке отдаётся нефильтрованная страница
// (degraded mode; в проде запрещён — config.Validate).
func TestFGAFilter_FailOpenReturnsPage(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = errors.New("boom")
	cfg := DefaultConfig()
	cfg.FailOpen = true
	f := NewFGAFilter(cli, cfg)

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a", "nlb-b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"nlb-a", "nlb-b"}, got, "fail-open → unfiltered page (degraded mode)")
}

// Ошибка НЕ кешируется: transient-сбой iam не отравляет кеш, следующий вызов
// перепрашивает и видит восстановившийся ответ.
func TestFGAFilter_ErrorNotCached(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = status.Error(codes.Unavailable, "iam down")
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.Error(t, err)
	require.Equal(t, 0, f.Size(), "fail-closed error must not be cached")

	cli.err = nil
	cli.allow("viewer", "nlb-a")
	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"nlb-a"}, got, "recovery: the real grant takes effect")
}

// Контракт BatchCheck: ответов столько же и в том же порядке. Расхождение —
// fail-closed ошибка, а не «считаем отказом»: сдвиг индексов приписал бы вердикт
// одного объекта другому.
func TestFGAFilter_ResponseLengthMismatchFailsClosed(t *testing.T) {
	f := NewFGAFilter(shortResponseClient{}, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a", "nlb-b"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

type shortResponseClient struct{}

func (shortResponseClient) BatchCheck(_ context.Context, _ *iamv1.BatchAuthorizeCheckRequest, _ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	return &iamv1.BatchAuthorizeCheckResponse{
		Responses: []*iamv1.AuthorizeCheckResponse{{Allowed: true}}, // 1 for 2 checks
	}, nil
}

// пустой subject при включённом фильтре → Unauthenticated (fail-closed guard,
// CWE-862: principal-less caller не перечисляет чужой проект).
func TestFGAFilter_AnonymousFailClosed(t *testing.T) {
	cli := newFakeAuthorizeClient()
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Equal(t, 0, cli.calls, "anonymous → no FGA call")
}

// отсутствие resourceType / action → ошибка (input guard), без вызова iam.
func TestFGAFilter_MissingResourceTypeOrAction(t *testing.T) {
	cli := newFakeAuthorizeClient()
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice", "", ActionLoadBalancerList, []string{"nlb-a"})
	require.Error(t, err, "empty resourceType")
	_, err = f.FilterVisibleIDs(context.Background(), "user:usr_alice", ResourceTypeLoadBalancer, "", []string{"nlb-a"})
	require.Error(t, err, "empty action")
	assert.Equal(t, 0, cli.calls, "input guard: must NOT call iam")
}

// per-request timeout энфорсится и всплывает как Unavailable (НЕ тихий bypass).
func TestFGAFilter_TimeoutEnforced(t *testing.T) {
	const clientSleep = 100 * time.Millisecond
	cli := newFakeAuthorizeClient()
	cli.sleep = clientSleep
	cfg := DefaultConfig()
	cfg.Timeout = 10 * time.Millisecond
	f := NewFGAFilter(cli, cfg)

	t0 := time.Now()
	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	elapsed := time.Since(t0)
	require.Error(t, err, "expected timeout error")
	assert.Equal(t, codes.Unavailable, status.Code(err))
	// Preemption sanity: вызов вернулся до того, как downstream договорил бы
	// (граница — sleep клиента, не жёсткий потолок от 10ms: тот флейкует под
	// -race/GC/CPU-throttle).
	assert.Less(t, elapsed, clientSleep,
		"timeout not enforced — call blocked for the full downstream sleep")
}

// Кешируются ТОЛЬКО положительные вердикты: повторный запрос про видимый id не
// стоит round-trip'а, а про невидимый — перепроверяется (иначе свежий грант не
// был бы виден до истечения TTL).
func TestFGAFilter_CachesPositiveOnly(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "nlb-a")
	f := NewFGAFilter(cli, DefaultConfig())
	ctx := context.Background()

	_, err := f.FilterVisibleIDs(ctx, "user:usr_alice", ResourceTypeLoadBalancer, ActionLoadBalancerList,
		[]string{"nlb-a", "nlb-b"})
	require.NoError(t, err)
	firstChecked := cli.checked
	require.Positive(t, firstChecked)

	_, err = f.FilterVisibleIDs(ctx, "user:usr_alice", ResourceTypeLoadBalancer, ActionLoadBalancerList,
		[]string{"nlb-a", "nlb-b"})
	require.NoError(t, err)

	var reAskedA, reAskedB bool
	for _, r := range cli.gotReqs[firstChecked:] {
		switch r.GetResource().GetId() {
		case "nlb-a":
			reAskedA = true
		case "nlb-b":
			reAskedB = true
		}
	}
	assert.False(t, reAskedA, "positive verdict is cached — no re-ask")
	assert.True(t, reAskedB, "negative verdict must NOT be cached — a fresh grant must be seen")
}

// cache TTL expiry → повторный вызов снова идёт в iam.
//
// Детерминированно через инъектированные часы (f.now) — НЕ wall-clock/time.Sleep
// (тот флейкует под -race/GC/CPU-throttle).
func TestFGAFilter_CacheTTLExpiry(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "nlb-a")
	cfg := DefaultConfig()
	cfg.CacheTTL = 25 * time.Millisecond
	f := NewFGAFilter(cli, cfg)

	base := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	cur := base
	f.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return cur
	}
	advance := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		cur = cur.Add(d)
	}
	ctx := context.Background()

	_, err := f.FilterVisibleIDs(ctx, "user:usr_alice", ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.NoError(t, err)
	require.Equal(t, 1, cli.calls)

	// Внутри TTL — попадание в кеш, iam НЕ дёргается.
	advance(10 * time.Millisecond)
	got, err := f.FilterVisibleIDs(ctx, "user:usr_alice", ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"nlb-a"}, got)
	assert.Equal(t, 1, cli.calls, "within TTL: served from cache")

	// Перешагнули TTL — запись протухла, iam зовётся заново.
	advance(30 * time.Millisecond)
	got, err = f.FilterVisibleIDs(ctx, "user:usr_alice", ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"nlb-a"}, got)
	assert.Equal(t, 2, cli.calls, "post-TTL: must ask iam again")
}


// LRU: при переполнении вытесняется least-recently-used, а не произвольная
// (Go-map-randomized, возможно горячая) запись — иначе burst distinct-List трэшил
// бы кеш и гнал лишний QPS в kacho-iam. Кеш остаётся ограничен CacheMaxEntries.
func TestFGAFilter_LRUEvictsLeastRecentlyUsed(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "hot")
	cfg := DefaultConfig()
	cfg.CacheMaxEntries = 10
	f := NewFGAFilter(cli, cfg)
	ctx := context.Background()

	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("cold_%d", i)
		cli.allow("viewer", id)
		_, err := f.FilterVisibleIDs(ctx, "user:usr_alice", ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{id})
		require.NoError(t, err)
	}
	_, err := f.FilterVisibleIDs(ctx, "user:usr_alice", ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"hot"})
	require.NoError(t, err)
	require.Equal(t, 10, f.Size())

	for i := 100; i < 200; i++ {
		before := cli.checked
		_, err := f.FilterVisibleIDs(ctx, "user:usr_alice", ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"hot"})
		require.NoError(t, err)
		require.Equal(t, before, cli.checked,
			"recently-used hot entry must stay cached across overflow (LRU, not random eviction)")

		id := fmt.Sprintf("cold_%d", i)
		cli.allow("viewer", id)
		_, err = f.FilterVisibleIDs(ctx, "user:usr_alice", ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{id})
		require.NoError(t, err)
	}
	assert.Equal(t, 10, f.Size(), "cache stays bounded at CacheMaxEntries")
}

// disabled / nil-client → passthrough без обращения к iam (dev / graceful start).
func TestFGAFilter_DisabledOrNilClientPassthrough(t *testing.T) {
	f := NewFGAFilter(nil, DefaultConfig())
	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a", "nlb-b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"nlb-a", "nlb-b"}, got)

	cli := newFakeAuthorizeClient()
	cfg := DefaultConfig()
	cfg.Enabled = false
	f2 := NewFGAFilter(cli, cfg)
	got2, err := f2.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"nlb-a"}, got2)
	assert.Equal(t, 0, cli.calls)
}

// Дубли во входе не оплачиваются дважды и не дублируются в выдаче.
func TestFGAFilter_DeduplicatesPageIDs(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "nlb-a")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a", "nlb-a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"nlb-a"}, got)
	assert.Equal(t, 1, cli.checked, "duplicate id must be asked once")
}
