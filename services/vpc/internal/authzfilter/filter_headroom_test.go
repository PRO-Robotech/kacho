// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/validate"
)

// loadedPeerLatency — латентность ОДНОГО BatchCheck (100 checks) у ЗАГРУЖЕННОГО
// kacho-iam. Значение выбрано выше прежнего per-call deadline (500ms) намеренно:
// именно этот режим ронял `RT-LST-PAGESIZE-EXACTLY-1000` в параллельном newman'е
// (`503 list filter: AuthorizeService.BatchCheck DeadlineExceeded`), тогда как на
// холостом стенде тот же кейс проходил. Регрессия — не логическая, а по запасу
// латентности, поэтому тест ЛОЧИТ ИМЕННО ЗАПАС, а не форму запроса.
const loadedPeerLatency = 600 * time.Millisecond

// TestFGAFilter_MaxPageWithinBudget_UnderLoadedPeerLatency — REGRESSION-lock.
//
// Страница ДОКУМЕНТИРОВАННОГО максимума (validate.MaxPageSize = 1000) обязана
// отфильтроваться БЕЗ 503, когда пир отвечает за loadedPeerLatency на батч, и
// уложиться в бюджет операции с реальным запасом.
//
// Арифметика (см. filter.go): страница 1000 → ceil(1000/100)=10 батчей.
// Relation теперь ОДНА (предикат страницы равен отношению чтения), поэтому
// последовательных фаз больше нет и worst-case глубина вдвое меньше прежней. До
// фикса параллелизма батчи шли ПОСЛЕДОВАТЕЛЬНО, каждый под своим 500ms-дедлайном
// → 10..20 round-trip'ов подряд, и ЛЮБОЙ из них дольше 500ms ронял весь List в
// Unavailable. Бюджет обязан держаться и на этой, уменьшенной, глубине.
func TestFGAFilter_MaxPageWithinBudget_UnderLoadedPeerLatency(t *testing.T) {
	pageSize := int(validate.MaxPageSize)

	cli := newFakeAuthorizeClient()
	ids := make([]string, 0, pageSize)
	want := make([]string, 0, pageSize)
	for i := 0; i < pageSize; i++ {
		id := fmt.Sprintf("net%017d", i)
		ids = append(ids, id)
		switch i % 3 {
		case 0, 1: // читаемый вызывающим → обязан быть в странице
			cli.allow("v_get", id)
			want = append(want, id)
		default: // не читаемый → в страницу не попадает
		}
	}
	cli.sleep = loadedPeerLatency

	cfg := DefaultConfig()
	f := NewFGAFilter(cli, cfg)

	t0 := time.Now()
	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeNetwork, ActionNetworkList, ids)
	elapsed := time.Since(t0)

	// (1) САМА регрессия: страница максимума не имеет права стать 503, пока пир
	//     отвечает за loadedPeerLatency.
	require.NoError(t, err,
		"a %d-id page (documented max) must NOT fail when the peer answers each ≤%d-id batch in %s — "+
			"this is exactly the RT-LST-PAGESIZE-EXACTLY-1000 regression",
		pageSize, maxBatchCheckSize, loadedPeerLatency)

	// (2) Разбиение allowed/denied НЕ портится параллельными батчами: тот же
	//     предикат (отношение чтения), тот же порядок курсора.
	assert.Equal(t, want, got,
		"parallel batches must not corrupt the allowed/denied partitioning nor the cursor order")

	calls, checked, maxInFlight, batchSizes := cli.snapshot()

	// (3) Контракт BatchCheck ≤100 держится (не ослаблен ради параллелизма).
	require.NotEmpty(t, batchSizes)
	for _, n := range batchSizes {
		assert.LessOrEqual(t, n, maxBatchCheckSize, "each BatchCheck must respect the ≤%d contract", maxBatchCheckSize)
	}

	// (4) Fan-out ОГРАНИЧЕН: не «горутина на батч».
	assert.LessOrEqual(t, maxInFlight, f.Parallelism(),
		"fan-out must be bounded by a worker pool, never unbounded goroutine-per-batch")

	// (5) АРИФМЕТИКА (без таймингов, поэтому не флейкует): worst-case глубина ×
	//     per-call deadline обязана помещаться в бюджет операции с запасом ≥25%.
	depth := f.WorstCaseDepth()
	worstWall := time.Duration(depth) * cfg.Timeout
	budget := f.Budget()
	require.Less(t, worstWall, budget,
		"worst-case wall (depth %d × per-call %s = %s) must fit the operation budget %s",
		depth, cfg.Timeout, worstWall, budget)
	assert.LessOrEqual(t, worstWall+budget/4, budget,
		"budget must keep ≥25%% headroom over the worst-case wall (%s vs %s)", worstWall, budget)

	// (6) Наблюдаемый параллелизм: последовательный обход дал бы calls×latency.
	//     Допускаем depth+1 волну на планировщик/GC.
	assert.Less(t, elapsed, time.Duration(depth+1)*loadedPeerLatency,
		"page must be checked in bounded-parallel waves, not %d sequential round-trips", calls)
	assert.Less(t, elapsed, budget, "operation must finish inside its own budget")

	t.Logf("page=%d ids | batches=%d (checks=%d) | parallelism=%d | observed max in-flight=%d | "+
		"per-call timeout=%s | worst-case depth=%d waves | worst-case wall=%s | budget=%s | "+
		"peer latency=%s | MEASURED elapsed=%s | headroom vs budget=%s",
		pageSize, calls, checked, f.Parallelism(), maxInFlight,
		cfg.Timeout, depth, worstWall, budget,
		loadedPeerLatency, elapsed.Round(time.Millisecond), (budget - elapsed).Round(time.Millisecond))
}

// Первая ошибка пробрасывается как есть (fail-closed), соседние батчи
// отменяются — а не «дорабатывают» страницу, которую всё равно не отдадут.
func TestFGAFilter_FirstErrorWinsAndCancelsSiblings(t *testing.T) {
	pageSize := int(validate.MaxPageSize)

	cli := newFakeAuthorizeClient()
	ids := make([]string, 0, pageSize)
	for i := 0; i < pageSize; i++ {
		ids = append(ids, fmt.Sprintf("net%017d", i))
	}
	cli.err = status.Error(codes.ResourceExhausted, "iam overloaded")
	cli.sleep = 20 * time.Millisecond

	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeNetwork, ActionNetworkList, ids)
	require.Error(t, err, "fail-closed: an iam error must never yield a page")
	assert.Nil(t, got)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "ResourceExhausted",
		"the FIRST upstream error must be reported, not a sibling's Canceled")

	calls, _, _, _ := cli.snapshot()
	assert.LessOrEqual(t, calls, f.Parallelism(),
		"after the first error no further wave may start (siblings cancelled); got %d calls", calls)
}

// Одиночный батч (страница ≤100) не платит за пул и не меняет наблюдаемое
// поведение — путь маленькой страницы остаётся прежним.
func TestFGAFilter_SingleBatchStaysSerial(t *testing.T) {
	cli := newFakeAuthorizeClient()
	ids := make([]string, 0, maxBatchCheckSize)
	for i := 0; i < maxBatchCheckSize; i++ {
		id := fmt.Sprintf("net%017d", i)
		ids = append(ids, id)
		cli.allow("v_get", id)
	}
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeNetwork, ActionNetworkList, ids)
	require.NoError(t, err)
	assert.Equal(t, ids, got)

	calls, _, maxInFlight, _ := cli.snapshot()
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, maxInFlight, "a single-batch page must not fan out")
}

// Бюджет операции — жёсткий потолок: висящий пир не может держать List дольше
// бюджета, сколько бы волн ни оставалось.
func TestFGAFilter_OperationBudgetCapsHangingPeer(t *testing.T) {
	cli := newFakeAuthorizeClient()
	ids := make([]string, 0, int(validate.MaxPageSize))
	for i := 0; i < int(validate.MaxPageSize); i++ {
		ids = append(ids, fmt.Sprintf("net%017d", i))
	}
	cli.sleep = time.Hour // пир не отвечает никогда

	cfg := DefaultConfig()
	cfg.Timeout = 30 * time.Second // per-call дедлайн заведомо больше бюджета
	cfg.OverallTimeout = 300 * time.Millisecond
	f := NewFGAFilter(cli, cfg)

	t0 := time.Now()
	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice",
		ResourceTypeNetwork, ActionNetworkList, ids)
	elapsed := time.Since(t0)

	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err), "fail-closed on budget exhaustion")
	assert.Less(t, elapsed, 5*time.Second,
		"the operation budget must cap the whole filter, not just one call; took %s", elapsed)
}
