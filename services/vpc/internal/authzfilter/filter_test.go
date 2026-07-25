// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
)

// fakeAuthorizeClient — stub iam AuthorizeService.BatchCheck.
//
// `visible` — какие (relation, id) разрешены. Отвечает строго в порядке checks,
// как того требует контракт BatchCheck.
type fakeAuthorizeClient struct {
	visible map[string]map[string]bool // relation → id → allowed
	err     error

	calls     int
	checked   int
	batchSize []int
	gotReqs   []*iamv1.AuthorizeCheckRequest
}

func newFakeAuthorizeClient() *fakeAuthorizeClient {
	return &fakeAuthorizeClient{visible: map[string]map[string]bool{}}
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

func (f *fakeAuthorizeClient) BatchCheck(_ context.Context, in *iamv1.BatchAuthorizeCheckRequest, _ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	f.calls++
	f.batchSize = append(f.batchSize, len(in.GetChecks()))
	if f.err != nil {
		return nil, f.err
	}
	if n := len(in.GetChecks()); n > 100 {
		return nil, status.Errorf(codes.InvalidArgument, "Illegal argument checks: batch size %d > 100", n)
	}
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

func TestFGAFilter_FiltersPageAndPreservesOrder(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "c", "a")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList,
		[]string{"c", "a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"c", "a"}, got,
		"visible subset must keep the caller's (cursor) order — re-sorting would break pagination")

	// request shape: per-object, explicit relation, page ids only.
	require.NotEmpty(t, cli.gotReqs)
	assert.Equal(t, "user:usr_x", cli.gotReqs[0].GetSubject())
	assert.Equal(t, ResourceTypeSubnet, cli.gotReqs[0].GetResource().GetType())
	assert.Equal(t, ActionSubnetList, cli.gotReqs[0].GetAction())
	assert.Equal(t, "viewer", cli.gotReqs[0].GetRequiredRelation())
}

// Предикат видимости — тот же союз, что делал ListObjects: viewer ∪ v_list.
// v_list-only грант («видеть в списке без содержимого») обязан оставаться видимым.
func TestFGAFilter_ViewerUnionVList(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "a").allow("v_list", "b")
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList,
		[]string{"a", "b", "c"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got, "viewer ∪ v_list")
}

// v_list спрашивается ТОЛЬКО про тех, кому отказал viewer (стоимость).
func TestFGAFilter_VListOnlyForViewerDenied(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "a", "b")
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList,
		[]string{"a", "b", "c"})
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
		id := fmt.Sprintf("e9b_%03d", i)
		ids = append(ids, id)
		cli.allow("viewer", id)
	}
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList, ids)
	require.NoError(t, err)
	assert.Len(t, got, 250)
	for _, n := range cli.batchSize {
		assert.LessOrEqual(t, n, 100, "each BatchCheck must respect the ≤100 contract")
	}
	assert.Equal(t, 3, cli.calls, "250 ids → 3 batches (100+100+50)")
}

// Стоимость пропорциональна СТРАНИЦЕ: сколько id пришло, столько и проверок
// (никакого перечисления вселенной).
func TestFGAFilter_CostProportionalToPage(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "a", "b", "c")
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList,
		[]string{"a", "b", "c"})
	require.NoError(t, err)
	assert.Equal(t, 3, cli.checked)
}

func TestFGAFilter_EmptyPageNoCall(t *testing.T) {
	cli := newFakeAuthorizeClient()
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, 0, cli.calls, "empty page → no authz round-trip")
}

func TestFGAFilter_NothingVisible(t *testing.T) {
	cli := newFakeAuthorizeClient()
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList,
		[]string{"a", "b"})
	require.NoError(t, err)
	assert.Empty(t, got, "no grant → nothing visible (no-leak)")
}

func TestFGAFilter_FailClosed(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = status.Error(codes.Internal, "fga boom")
	f := NewFGAFilter(cli, DefaultConfig()) // FailOpen=false

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList,
		[]string{"a"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code(), "fail-closed maps FGA error → Unavailable")
}

func TestFGAFilter_FailOpenReturnsPage(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = errors.New("boom")
	cfg := DefaultConfig()
	cfg.FailOpen = true
	f := NewFGAFilter(cli, cfg)

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList,
		[]string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got, "fail-open → unfiltered page (degraded mode)")
}

// Контракт BatchCheck: ответов столько же и в том же порядке. Расхождение —
// fail-closed ошибка, а не «считаем отказом»: сдвиг индексов приписал бы вердикт
// одного объекта другому.
func TestFGAFilter_ResponseLengthMismatchFailsClosed(t *testing.T) {
	f := NewFGAFilter(shortResponseClient{}, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList,
		[]string{"a", "b"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code())
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

	_, err := f.FilterVisibleIDs(context.Background(), "", ResourceTypeSubnet, ActionSubnetList, []string{"a"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Equal(t, 0, cli.calls, "anonymous → no FGA call")
}

// Кешируются ТОЛЬКО положительные вердикты: повторный запрос про видимый id не
// стоит round-trip'а, а про невидимый — перепроверяется (иначе revoke залипал бы
// на TTL).
func TestFGAFilter_CachesPositiveOnly(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "a")
	f := NewFGAFilter(cli, DefaultConfig())
	ctx := context.Background()

	_, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeSubnet, ActionSubnetList, []string{"a", "b"})
	require.NoError(t, err)
	firstChecked := cli.checked
	require.Positive(t, firstChecked)

	_, err = f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeSubnet, ActionSubnetList, []string{"a", "b"})
	require.NoError(t, err)

	var reAskedA bool
	for _, r := range cli.gotReqs[firstChecked:] {
		if r.GetResource().GetId() == "a" {
			reAskedA = true
		}
	}
	assert.False(t, reAskedA, "positive verdict is cached — no re-ask")

	var reAskedB bool
	for _, r := range cli.gotReqs[firstChecked:] {
		if r.GetResource().GetId() == "b" {
			reAskedB = true
		}
	}
	assert.True(t, reAskedB, "negative verdict must NOT be cached — a fresh grant must be seen")
}

func TestFGAFilter_InvalidateDropsSubject(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "a")
	f := NewFGAFilter(cli, DefaultConfig())
	ctx := context.Background()

	_, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeSubnet, ActionSubnetList, []string{"a"})
	require.NoError(t, err)
	require.Equal(t, 1, f.Size())

	f.Invalidate("user:usr_x")
	assert.Equal(t, 0, f.Size(), "revoke-driven invalidation must drop the subject's verdicts")
}

// LRU: при переполнении вытесняется least-recently-used, а не произвольная
// (Go-map-randomized, возможно горячая) запись — иначе burst distinct-List
// трэшил бы кеш и гнал лишний QPS в kacho-iam.
func TestFGAFilter_LRUEvictsLeastRecentlyUsed(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.allow("viewer", "hot")
	cfg := DefaultConfig()
	cfg.CacheMaxEntries = 10
	f := NewFGAFilter(cli, cfg)
	ctx := context.Background()

	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("cold_%d", i)
		cli.allow("viewer", id)
		_, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeSubnet, ActionSubnetList, []string{id})
		require.NoError(t, err)
	}
	_, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeSubnet, ActionSubnetList, []string{"hot"})
	require.NoError(t, err)
	require.Equal(t, 10, f.Size())

	for i := 100; i < 200; i++ {
		before := cli.checked
		_, err := f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeSubnet, ActionSubnetList, []string{"hot"})
		require.NoError(t, err)
		require.Equal(t, before, cli.checked,
			"recently-used hot entry must stay cached across overflow (LRU, not random eviction)")

		id := fmt.Sprintf("cold_%d", i)
		cli.allow("viewer", id)
		_, err = f.FilterVisibleIDs(ctx, "user:usr_x", ResourceTypeSubnet, ActionSubnetList, []string{id})
		require.NoError(t, err)
	}
	assert.Equal(t, 10, f.Size(), "cache stays bounded at CacheMaxEntries")
}

func TestFGAFilter_DisabledOrNilClientPassthrough(t *testing.T) {
	// nil client → passthrough (graceful start без iam).
	f := NewFGAFilter(nil, DefaultConfig())
	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList,
		[]string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got)

	// Enabled=false → passthrough.
	cli := newFakeAuthorizeClient()
	cfg := DefaultConfig()
	cfg.Enabled = false
	f2 := NewFGAFilter(cli, cfg)
	got2, err := f2.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList,
		[]string{"a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, got2)
	assert.Equal(t, 0, cli.calls)
}

func TestAsPort_NilFilterReturnsNil(t *testing.T) {
	assert.Nil(t, AsPort(nil))
	var typedNil *FGAFilter
	assert.Nil(t, AsPort(typedNil), "typed-nil *FGAFilter → nil UseCasePort (passthrough)")
}

func TestAsPort_DelegatesToFilter(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "e9b_a")
	port := AsPort(NewFGAFilter(cli, DefaultConfig()))
	require.NotNil(t, port)

	got, err := port.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeSubnet, ActionSubnetList,
		[]string{"e9b_a", "e9b_z"})
	require.NoError(t, err)
	assert.Equal(t, []string{"e9b_a"}, got)
}
