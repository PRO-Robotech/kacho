// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// fakeAuthorizeClient — stub kacho-iam AuthorizeService.BatchCheck.
//
// `visible` — какие (relation, id) разрешены. Отвечает строго в порядке checks, как
// того требует контракт BatchCheck. Счётчики под mu: фильтр законно зовёт BatchCheck
// из нескольких горутин, и незащищённый счётчик в САМОМ стабе давал бы ложный
// -race-репорт про тестовый код.
type fakeAuthorizeClient struct {
	mu      sync.Mutex
	visible map[string]map[string]bool // relation → id → allowed
	err     error
	sleep   time.Duration

	calls     int
	checked   int
	batchSize []int
	gotReqs   []*iamv1.AuthorizeCheckRequest

	// inFlight/maxInFlight — наблюдаемая конкурентность, чтобы тест мог доказать,
	// что fan-out батчей действительно ОГРАНИЧЕН пулом.
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
	f.mu.Lock()
	defer f.mu.Unlock()
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
	cli := newFakeAuthorizeClient().allow("viewer", "c", "a")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x",
		ResourceTypeVolume, ActionVolumeList, []string{"c", "a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"c", "a"}, got,
		"visible subset must keep the caller's (cursor) order — re-sorting would break pagination")

	require.NotEmpty(t, cli.gotReqs)
	assert.Equal(t, "user:usr_x", cli.gotReqs[0].GetSubject())
	assert.Equal(t, ResourceTypeVolume, cli.gotReqs[0].GetResource().GetType())
	assert.Equal(t, ActionVolumeList, cli.gotReqs[0].GetAction())
	assert.Equal(t, "viewer", cli.gotReqs[0].GetRequiredRelation(),
		"the decision must ride an explicit required_relation, not a server-side verb derivation")
}

// Отказ viewer'а — окончательный: страница не добирается вторым, более широким
// отношением. `v_list` без резолвящегося `viewer` — это не выданный доступ, а
// недоматериализованный/недоотозванный грант, и показывать по нему нечего: Get на
// этот же объект гейтится `viewer`'ом и ответил бы отказом (см.
// filter_get_parity_test.go).
func TestFGAFilter_ViewerDenialIsFinal(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "a").allow("v_list", "b")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x",
		ResourceTypeSnapshot, ActionSnapshotList, []string{"a", "b", "c"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, got)
}

// Пустой subject — fail-closed Unauthenticated, НИКОГДА не passthrough.
func TestFGAFilter_EmptySubjectFailsClosed(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "a")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "",
		ResourceTypeImage, ActionImageList, []string{"a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Nil(t, got, "an anonymous caller must never receive an unfiltered page")
}

// Ошибка iam при fail-closed (default) → Unavailable и НИ ОДНОЙ строки.
func TestFGAFilter_IAMErrorFailsClosed(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = status.Error(codes.Internal, "boom")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x",
		ResourceTypeVolume, ActionVolumeList, []string{"a", "b"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Nil(t, got)
}

// fail-open — осознанный break-glass: страница отдаётся как есть, но ГРОМКО.
func TestFGAFilter_FailOpenReturnsUnfilteredPage(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = status.Error(codes.Unavailable, "iam down")
	cfg := DefaultConfig()
	cfg.FailOpen = true
	f := NewFGAFilter(cli, cfg)

	ids := []string{"a", "b"}
	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x",
		ResourceTypeVolume, ActionVolumeList, ids)
	require.NoError(t, err)
	assert.Equal(t, ids, got)
}

// Положительный вердикт кешируется, отрицательный — никогда (иначе revoke залипал
// бы на TTL, а свежесозданный ресурс был бы невидим весь TTL).
func TestFGAFilter_CachesOnlyPositiveVerdicts(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "a")
	f := NewFGAFilter(cli, DefaultConfig())

	for i := 0; i < 3; i++ {
		got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x",
			ResourceTypeVolume, ActionVolumeList, []string{"a", "b"})
		require.NoError(t, err)
		assert.Equal(t, []string{"a"}, got)
	}
	assert.Equal(t, 1, f.Size(), "only the positive verdict may be cached")

	_, checked, _, _ := cli.snapshot()
	// «a» спрашивается один раз (потом из кеша); «b» — на КАЖДОМ вызове и по обеим
	// relations, потому что отказ не кешируется.
	assert.Equal(t, 1+3*len(visibilityRelations), checked,
		"a denied id must be re-asked every time; a granted id must be asked once")
}

// TTL истекает по инъектированным часам (детерминированно, без time.Sleep).
func TestFGAFilter_CacheEntryExpires(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "a")
	f := NewFGAFilter(cli, DefaultConfig())
	clock := time.Now()
	f.now = func() time.Time { return clock }

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x",
		ResourceTypeVolume, ActionVolumeList, []string{"a"})
	require.NoError(t, err)
	calls1, _, _, _ := cli.snapshot()

	clock = clock.Add(2 * DefaultConfig().CacheTTL)
	_, err = f.FilterVisibleIDs(context.Background(), "user:usr_x",
		ResourceTypeVolume, ActionVolumeList, []string{"a"})
	require.NoError(t, err)
	calls2, _, _, _ := cli.snapshot()
	assert.Greater(t, calls2, calls1, "an expired entry must be re-asked, not served stale")
}

// Кеш ограничен: LRU вытесняет хвост, а не произвольную (возможно горячую) запись.
func TestFGAFilter_CacheIsBoundedLRU(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cfg := DefaultConfig()
	cfg.CacheMaxEntries = 2
	f := NewFGAFilter(cli, cfg)

	cli.allow("viewer", "a", "b", "c")
	for _, id := range []string{"a", "b", "c"} {
		_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x",
			ResourceTypeVolume, ActionVolumeList, []string{id})
		require.NoError(t, err)
	}
	assert.Equal(t, 2, f.Size(), "cache must respect CacheMaxEntries")
}

// Расхождение длины ответа BatchCheck с длиной батча — fail-closed ошибка, а не
// «считаем отказом»: молчаливое смещение индексов выдало бы вердикт одного объекта
// за другой.
type shortRespClient struct{}

func (shortRespClient) BatchCheck(context.Context, *iamv1.BatchAuthorizeCheckRequest, ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	return &iamv1.BatchAuthorizeCheckResponse{
		Responses: []*iamv1.AuthorizeCheckResponse{{Allowed: true}},
	}, nil
}

func TestFGAFilter_ResponseLengthMismatchFailsClosed(t *testing.T) {
	f := NewFGAFilter(shortRespClient{}, DefaultConfig())
	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x",
		ResourceTypeVolume, ActionVolumeList, []string{"a", "b"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Nil(t, got)
}

// SubjectFromPrincipal — единый источник caller-identity; system / неполный
// principal → "" (caller обязан трактовать это как fail-closed, не как bypass).
func TestSubjectFromPrincipal(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"user", operations.WithPrincipal(context.Background(),
			operations.Principal{Type: "user", ID: "usr_a"}), "user:usr_a"},
		{"service account", operations.WithPrincipal(context.Background(),
			operations.Principal{Type: "service_account", ID: "sva_a"}), "service_account:sva_a"},
		{"system principal", operations.WithPrincipal(context.Background(),
			operations.SystemPrincipal()), ""},
		{"no principal at all", context.Background(), ""},
		{"incomplete principal", operations.WithPrincipal(context.Background(),
			operations.Principal{Type: "user"}), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SubjectFromPrincipal(tc.ctx))
		})
	}
}

// FilterVisiblePage без caller-identity отдаёт ПУСТУЮ страницу и не ходит в iam:
// bypass на пустом subject'е — это и есть over-show-утечка.
func TestFilterVisiblePage_NoSubjectYieldsEmptyPageWithoutIAM(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "a")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := FilterVisiblePage(context.Background(), f,
		ResourceTypeVolume, ActionVolumeList, []string{"a"}, func(s string) string { return s })
	require.NoError(t, err)
	assert.Empty(t, got)
	calls, _, _, _ := cli.snapshot()
	assert.Zero(t, calls, "no caller identity ⇒ no page ⇒ no round-trip to iam")
}

// nil-фильтр — passthrough (dev / list-filter disabled).
func TestFilterVisiblePage_NilFilterIsPassthrough(t *testing.T) {
	page := []string{"a", "b"}
	got, err := FilterVisiblePage(context.Background(), nil,
		ResourceTypeVolume, ActionVolumeList, page, func(s string) string { return s })
	require.NoError(t, err)
	assert.Equal(t, page, got)
}
