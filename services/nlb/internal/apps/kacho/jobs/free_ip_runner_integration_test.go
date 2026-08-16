// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// free_ip_runner_integration_test.go — integration tests для FreeIPRunner
// (durable-handle reconciler LoadBalancer'ов). VIP консолидирован на LoadBalancer
// (anycast active-active), поэтому reconciler сканирует load_balancers (не
// listeners) и освобождает VIP per-family. Покрывают:
//
//   - застрявший DELETING-LB → reconcile: FreeIP по address_id_v4 + DELETE строки
//   - outbox DELETED + fga-unregister;
//   - идемпотентность: уже освобождённый VIP → строка всё равно удаляется;
//   - linked-ветка: vip_origin='linked' → ClearReference (НЕ FreeIP);
//   - dualstack: раздельный release v4 (auto owned → two-step) и v6 (linked → ClearReference);
//   - create-orphan ('CREATING' с известным address_id) → FreeIP + DELETE без
//     outbox/fga (LB никогда не анонсировался);
//   - auto-only known-gap: 'CREATING' без address_id → DELETE без release;
//   - age-порог: свежий in-flight 'CREATING' НЕ трогается;
//   - multi-replica: ровно один release+DELETE (FOR UPDATE SKIP LOCKED);
//   - Run(ctx): tick освобождает stuck-строку, ctx cancel → чистый выход.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/jackc/pgx/v5/pgxpool"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// fakeReleaser — in-memory vpcclient.InternalAddressClient: считает вызовы
// снятия аренды (release-ветка reconciler'а). Alloc/SetReference не нужны.
type fakeReleaser struct {
	mu          sync.Mutex
	freeCalls   []string
	clearCalls  []string
	freeErr     error
	clearErr    error
	errByAddr   map[string]error // per-address override (poison-row tests); nil → no override
	onFirstFree func()           // coordination-hook (multi-replica): держит lock пока B тикает
	firstFired  bool
}

func (f *fakeReleaser) AllocateExternalIP(context.Context, vpcclient.AllocateExternalIPRequest) (*vpcclient.AllocateResponse, error) {
	return &vpcclient.AllocateResponse{}, nil
}
func (f *fakeReleaser) AllocateInternalIP(context.Context, vpcclient.AllocateInternalIPRequest) (*vpcclient.AllocateResponse, error) {
	return &vpcclient.AllocateResponse{}, nil
}
func (f *fakeReleaser) AllocateExternalIPv6(context.Context, vpcclient.AllocateExternalIPRequest) (*vpcclient.AllocateResponse, error) {
	return &vpcclient.AllocateResponse{}, nil
}
func (f *fakeReleaser) AllocateInternalIPv6(context.Context, vpcclient.AllocateInternalIPRequest) (*vpcclient.AllocateResponse, error) {
	return &vpcclient.AllocateResponse{}, nil
}
func (f *fakeReleaser) SetReference(context.Context, string, vpcclient.AddressOwner, bool) error {
	return nil
}
func (f *fakeReleaser) AttachExisting(context.Context, vpcclient.AttachExistingRequest) (*vpcclient.AllocateResponse, error) {
	return &vpcclient.AllocateResponse{}, nil
}

// ReleaseLease — ОДИН глагол вместо прежней пары. Дублёр отвергает
// незаполненное предъявление владения так же, как настоящий: без этого проба
// зеленела бы на вызове, который боевой владелец отверг бы синхронно.
func (f *fakeReleaser) ReleaseLease(
	_ context.Context, req vpcclient.ReleaseLeaseRequest,
) (vpcclient.LeaseOutcome, error) {
	switch {
	case req.ProjectID == "":
		return "", fmt.Errorf("%w: project_id is empty", domain.ErrInvalidArg)
	case req.AddressID == "":
		return "", fmt.Errorf("%w: address_id is empty", domain.ErrInvalidArg)
	case req.Owner.Kind == "" || req.Owner.ID == "":
		return "", fmt.Errorf("%w: owner is empty", domain.ErrInvalidArg)
	}
	addressID := req.AddressID
	f.mu.Lock()
	hook := f.onFirstFree
	if hook != nil && !f.firstFired {
		f.firstFired = true
	} else {
		hook = nil
	}
	f.clearCalls = append(f.clearCalls, addressID)
	f.freeCalls = append(f.freeCalls, addressID)
	err := f.freeErr
	if err == nil {
		err = f.clearErr
	}
	if e, ok := f.errByAddr[addressID]; ok {
		err = e
	}
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if err != nil {
		return "", err
	}
	return vpcclient.LeaseReleased, nil
}

func (f *fakeReleaser) frees() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.freeCalls))
	copy(out, f.freeCalls)
	return out
}

func (f *fakeReleaser) clears() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.clearCalls))
	copy(out, f.clearCalls)
	return out
}

// newFreeIPRunner — конструктор для тестов с тихим logger'ом.
func newFreeIPRunner(t testing.TB, pool *pgxpool.Pool, addrs vpcclient.InternalAddressClient, age time.Duration) *FreeIPRunner {
	t.Helper()
	logger := observability.NewSlogger(discardWriter{})
	return NewFreeIPRunner(pool, addrs, logger, time.Second, age)
}

// vipFixtureSeq — счётчик синтетических адресов фикстуры.
//
// Уникальность адреса держится СЧЁТЧИКОМ, а не совпадением литералов: partial
// UNIQUE (region_id, address_vN) по непустым адресам (миграция 0009) отверг бы
// две строки одного теста с одним адресом, а все строки этой фикстуры лежат в
// одном регионе. Со счётчиком новая строка фикстуры не обязана помнить про
// соседние.
var vipFixtureSeq atomic.Uint32

// vipAddrFor — синтетический адрес семейства для ключа аренды addressID.
//
// Пустой ключ → пустой адрес: схема требует ЭКВИВАЛЕНТНОСТИ «адрес пуст ⟺ ключ
// аренды пуст» (миграция 0035), то есть запрещены ОБА перекоса. Непустой адрес
// обязан вдобавок нести своё семейство в `ip_families` (миграция 0011) — это
// делает insertStuckLB.
//
// Значение берётся из диапазонов, зарезервированных под документацию (RFC 5737
// для v4, RFC 3849 для v6): оно IP-образно, но заведомо не совпадает ни с одним
// настоящим адресом, поэтому не может быть принято за живые данные.
//
// Адрес НАМЕРЕННО не равен ключу аренды: reconcile ключуется по `address_id_vN`,
// и совпадение значений сделало бы неразличимым освобождение по неверной колонке.
func vipAddrFor(t testing.TB, family domain.IPVersion, addressID string) string {
	t.Helper()
	if addressID == "" {
		return ""
	}
	n := vipFixtureSeq.Add(1)
	// Предпосылка помощника: адреса берутся из одного /24, значит их не может
	// быть больше 254 за прогон пакета. Когда предпосылка перестанет
	// выполняться, об этом скажет проба, а не молчаливая коллизия per-region
	// UNIQUE — та выглядела бы как «фикстура вдруг не вставляется».
	require.Lessf(t, n, uint32(255),
		"фикстура исчерпала документационный /24 (выдано адресов: %d) — расширь диапазон", n)
	if family == domain.IPVersionV6 {
		return fmt.Sprintf("2001:db8::%x", n)
	}
	return fmt.Sprintf("203.0.113.%d", n)
}

// insertStuckLB — durable-handle LB в нетерминальном статусе с заданным возрастом
// (updated_at = now() - age) и per-family binding. reconcile ключуется по
// address_id_v4/v6, поэтому предмет проб задают именно они.
//
// Адрес и ключ его аренды пишутся ПАРОЙ, и это не оформление. Прежняя редакция
// оставляла адрес пустым при непустом ключе, объясняя это тем, что «status-aware
// CHECK пропускает». Такого состояния продукт не производит: все три полосы
// получения VIP возвращают адрес и ключ ОДНИМ ответом vpc, а `Insert` и
// `AttachVIP` пишут их одним стейтментом. То есть фикстура была снисходительнее
// продукта — строила состояние, которого в жизни не бывает, — и с появлением
// миграции 0035 («адрес пуст ⟺ ключ аренды пуст») перестала вставляться вовсе:
// `PRO-Robotech/kacho#495`.
func insertStuckLB(t testing.TB, ctx context.Context, pool *pgxpool.Pool,
	status domain.LBStatus, originV4, addrIDV4, originV6, addrIDV6 string, age time.Duration) (id, projectID string) {
	t.Helper()
	id = ids.NewID(ids.PrefixLoadBalancer)
	projectID = "prj01" + ids.NewUID()[:15]
	// Учёт числа ресурсов: строка учёта заводится ЗДЕСЬ, потому что здесь
	// придумана идентичность проекта (см. `quota_fixture_test.go`).
	seedQuotaForProject(t, ctx, pool, projectID)

	addrV4 := vipAddrFor(t, domain.IPVersionV4, addrIDV4)
	addrV6 := vipAddrFor(t, domain.IPVersionV6, addrIDV6)
	// Семейство объявляется ровно тогда, когда у него есть адрес: непустой
	// address_vN без своего токена в ip_families отвергает миграция 0011.
	families := make([]string, 0, 2)
	if addrV4 != "" {
		families = append(families, string(domain.IPVersionV4))
	}
	if addrV6 != "" {
		families = append(families, string(domain.IPVersionV6))
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO kacho_nlb.load_balancers
			(id, project_id, region_id, type, status, placement_type, ip_families,
			 address_v4, address_id_v4, vip_origin_v4,
			 address_v6, address_id_v6, vip_origin_v6,
			 created_at, updated_at)
		VALUES ($1, $2, 'region-1', 'INTERNAL', $3, 'REGIONAL', $4,
		        $5, $6, $7,
		        $8, $9, $10,
		        now() - $11::interval, now() - $11::interval)
	`, id, projectID, string(status), families,
		addrV4, addrIDV4, originV4,
		addrV6, addrIDV6, originV6,
		age.String())
	require.NoError(t, err)
	return id, projectID
}

func countLoadBalancers(t testing.TB, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM kacho_nlb.load_balancers`).Scan(&n))
	return n
}

func countOutboxFor(t testing.TB, ctx context.Context, pool *pgxpool.Pool, resourceType, resourceID, action string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM kacho_nlb.nlb_outbox
		 WHERE resource_type=$1 AND resource_id=$2 AND action=$3
	`, resourceType, resourceID, action).Scan(&n))
	return n
}

func countFGAUnregister(t testing.TB, ctx context.Context, pool *pgxpool.Pool, resourceID string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM kacho_nlb.fga_register_outbox
		 WHERE event_type='fga.unregister' AND resource_id=$1
	`, resourceID).Scan(&n))
	return n
}

// lbExists — присутствует ли строка LB с данным id.
func lbExists(t testing.TB, ctx context.Context, pool *pgxpool.Pool, id string) bool {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_nlb.load_balancers WHERE id=$1`, id).Scan(&n))
	return n == 1
}

// lbStuckEligible — попала бы строка id под selectStuckSQL при данном пороге
// (updated_at < now()-threshold). Bump updated_at=now() выводит её из выборки.
func lbStuckEligible(t testing.TB, ctx context.Context, pool *pgxpool.Pool, id string, threshold time.Duration) bool {
	t.Helper()
	var eligible bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) = 1 FROM kacho_nlb.load_balancers
		 WHERE id = $1
		   AND status IN ('DELETING','CREATING')
		   AND updated_at < now() - make_interval(secs => $2::double precision)
	`, id, threshold.Seconds()).Scan(&eligible))
	return eligible
}

// =============================================================================
// reconcileOnce — single tick, direct call.
// =============================================================================

// TestFreeIP_ReconcileStuckDeleting — застрявший DELETING auto-LB → reconcile
// освобождает VIP (FreeIP по address_id_v4), удаляет строку, эмитит DELETED +
// fga-unregister.
func TestFreeIP_ReconcileStuckDeleting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	const addrID = "adr0000000STUCKDEL01"
	lbID, _ := insertStuckLB(t, ctx, pool, domain.LBStatusDeleting, "auto", addrID, "", "", 10*time.Minute)

	rel := &fakeReleaser{}
	r := newFreeIPRunner(t, pool, rel, time.Minute)

	n, err := r.reconcileOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "exactly one stuck LB reconciled")
	assert.Equal(t, []string{addrID}, rel.frees(), "FreeIP called once by address_id_v4")
	assert.Equal(t, []string{addrID}, rel.clears(), "owned auto → ClearReference before FreeIP (two-step)")
	assert.Equal(t, 0, countLoadBalancers(t, ctx, pool), "durable handle deleted")
	assert.Equal(t, 1, countOutboxFor(t, ctx, pool, "nlb_load_balancer", lbID, "DELETED"))
	assert.Equal(t, 1, countFGAUnregister(t, ctx, pool, lbID))

	// Повторный тик по уже удалённой строке — no-op.
	n2, err := r.reconcileOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n2, "second tick is a no-op")
}

// TestFreeIP_IdempotentAlreadyFreed — VIP уже освобождён (release no-op): строка
// всё равно удаляется, ошибки нет.
func TestFreeIP_IdempotentAlreadyFreed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	insertStuckLB(t, ctx, pool, domain.LBStatusDeleting, "auto", "adr000000ALREADYGONE", "", "", 10*time.Minute)

	rel := &fakeReleaser{} // freeErr=nil моделирует idempotent NotFound
	r := newFreeIPRunner(t, pool, rel, time.Minute)

	n, err := r.reconcileOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 0, countLoadBalancers(t, ctx, pool), "row removed despite VIP already gone")
}

// TestFreeIP_LinkedClearReference — vip_origin='linked' → ClearReference (НЕ FreeIP);
// tenant-owned Address не удаляется.
func TestFreeIP_LinkedClearReference(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	const addrID = "adr000000LINKSTUCK01"
	insertStuckLB(t, ctx, pool, domain.LBStatusDeleting, "linked", addrID, "", "", 10*time.Minute)

	rel := &fakeReleaser{}
	r := newFreeIPRunner(t, pool, rel, time.Minute)

	n, err := r.reconcileOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{addrID}, rel.clears(), "ClearReference called by address_id_v4")
	assert.Empty(t, rel.frees(), "FreeIP must NOT be called for linked (anti data-loss)")
	assert.Equal(t, 0, countLoadBalancers(t, ctx, pool))
}

// TestFreeIP_DualstackSeparateRelease — dualstack orphan: v4 (auto owned →
// two-step ClearReference→FreeIP) и v6 (linked → ClearReference) освобождаются
// РАЗДЕЛЬНО, каждый по своему дискриминатору.
func TestFreeIP_DualstackSeparateRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	const addrV4 = "adr0000000DUALV40001"
	const addrV6 = "adr0000000DUALV60001"
	insertStuckLB(t, ctx, pool, domain.LBStatusDeleting, "auto", addrV4, "linked", addrV6, 10*time.Minute)

	rel := &fakeReleaser{}
	r := newFreeIPRunner(t, pool, rel, time.Minute)

	n, err := r.reconcileOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{addrV4}, rel.frees(), "v4 auto → FreeIP (after clear)")
	assert.Equal(t, []string{addrV4, addrV6}, rel.clears(), "v4 owned two-step clear + v6 linked clear")
	assert.Equal(t, 0, countLoadBalancers(t, ctx, pool))
}

// TestFreeIP_CreateOrphanReconciled — create-path durable-handle orphan
// ('CREATING' с известным address_id) → FreeIP + DELETE, но БЕЗ outbox/fga
// (LB никогда не достиг терминального статуса и не анонсировался).
func TestFreeIP_CreateOrphanReconciled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	const addrID = "adr0000CREATEORPHAN1"
	lbID, _ := insertStuckLB(t, ctx, pool, domain.LBStatusCreating, "auto", addrID, "", "", 10*time.Minute)

	rel := &fakeReleaser{}
	r := newFreeIPRunner(t, pool, rel, time.Minute)

	n, err := r.reconcileOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{addrID}, rel.frees(), "FreeIP frees the orphan VIP")
	assert.Equal(t, 0, countLoadBalancers(t, ctx, pool), "durable handle deleted")
	assert.Equal(t, 0, countOutboxFor(t, ctx, pool, "nlb_load_balancer", lbID, "DELETED"))
	assert.Equal(t, 0, countFGAUnregister(t, ctx, pool, lbID))
}

// TestFreeIP_CreateOrphanEmptyAddress_DeletedNoRelease — auto-only known-gap:
// 'CREATING' без address_id (краш в окне «alloc-ответ ↔ persist») → reconcile
// удаляет handle БЕЗ release (нечем ключевать FreeIP).
func TestFreeIP_CreateOrphanEmptyAddress_DeletedNoRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	insertStuckLB(t, ctx, pool, domain.LBStatusCreating, "", "", "", "", 10*time.Minute)

	rel := &fakeReleaser{}
	r := newFreeIPRunner(t, pool, rel, time.Minute)

	n, err := r.reconcileOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Empty(t, rel.frees(), "no address_id → nothing to free")
	assert.Empty(t, rel.clears())
	assert.Equal(t, 0, countLoadBalancers(t, ctx, pool), "handle deleted")
}

// TestFreeIP_AgeThresholdSkipsFresh — свежий in-flight 'CREATING' (updated_at
// ~now) НЕ трогается, пока легитимный worker дорабатывает.
func TestFreeIP_AgeThresholdSkipsFresh(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	insertStuckLB(t, ctx, pool, domain.LBStatusCreating, "auto", "adr0000FRESHINFLIGHT", "", "", 0)

	rel := &fakeReleaser{}
	r := newFreeIPRunner(t, pool, rel, 5*time.Minute) // порог 5m >> age 0

	n, err := r.reconcileOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "fresh in-flight CREATING must be skipped")
	assert.Empty(t, rel.frees())
	assert.Equal(t, 1, countLoadBalancers(t, ctx, pool), "fresh row untouched")
}

// TestFreeIP_MultiReplica — две реплики тикают одновременно по одной stuck-строке:
// FOR UPDATE SKIP LOCKED гарантирует ровно один release+DELETE.
func TestFreeIP_MultiReplica(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	const addrID = "adr000MULTIREPLICA01"
	insertStuckLB(t, ctx, pool, domain.LBStatusDeleting, "auto", addrID, "", "", 10*time.Minute)

	// onFirstFree держит row-lock пока вторая реплика тикает → SKIP LOCKED путь.
	rel := &fakeReleaser{onFirstFree: func() { time.Sleep(200 * time.Millisecond) }}
	r := newFreeIPRunner(t, pool, rel, time.Minute)

	var wg sync.WaitGroup
	results := make([]int, 2)
	// Per-goroutine error slots asserted on the MAIN goroutine after Wait:
	// require.NoError inside a child goroutine only Goexits that goroutine
	// (leaving results[idx]=0), which could mask a real reconcile error as GREEN.
	errsCh := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			n, e := r.reconcileOnce(ctx)
			errsCh[idx] = e
			results[idx] = n
		}(i)
	}
	wg.Wait()
	for idx, e := range errsCh {
		require.NoErrorf(t, e, "replica %d reconcileOnce", idx)
	}

	assert.Equal(t, 1, results[0]+results[1], "exactly one replica reconciled the row")
	assert.Len(t, rel.frees(), 1, "FreeIP called exactly once (no double free)")
	assert.Equal(t, 0, countLoadBalancers(t, ctx, pool), "row deleted exactly once")
}

// =============================================================================
// Poison-row isolation — permanent release failure must not head-of-line-block.
// =============================================================================

// TestFreeIP_PoisonRowDoesNotBlockQueue — застрявший LB, чей release VIP падает
// PERMANENT-ошибкой (ErrInvalidArg — напр. stray referrer / malformed address_id),
// не должен head-of-line-блокировать весь reconciler. selectStuckSQL берёт
// старейшую строку первой; до фикса reconcileOne возвращал ошибку → reconcileOnce
// прерывал весь тик → та же ядовитая строка переизбиралась каждый тик, и НИ ОДНА
// другая застрявшая LB не реконсилилась (unbounded VIP-leak за одной строкой).
// После фикса: ядовитая строка изолируется (bump updated_at → тонет в хвост
// очереди и выпадает из age-порога), тик продолжается и реконсилит здоровую LB.
func TestFreeIP_PoisonRowDoesNotBlockQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	// Poison — старейшая (updated_at раньше) → selectStuckSQL берёт её первой.
	const poisonAddr = "adr00000POISONVIP001"
	poisonID, _ := insertStuckLB(t, ctx, pool, domain.LBStatusDeleting, "linked", poisonAddr, "", "", 20*time.Minute)
	// Healthy — младше poison, но всё ещё старше age-порога.
	const healthyAddr = "adr0000HEALTHYVIP001"
	healthyID, _ := insertStuckLB(t, ctx, pool, domain.LBStatusDeleting, "linked", healthyAddr, "", "", 10*time.Minute)

	var poisoned []string
	rel := &fakeReleaser{errByAddr: map[string]error{
		poisonAddr: fmt.Errorf("%w: address %s not found", domain.ErrInvalidArg, poisonAddr),
	}}
	r := NewFreeIPRunner(pool, rel, observability.NewSlogger(discardWriter{}), time.Second, time.Minute,
		WithPoisonObserver(func(id string) { poisoned = append(poisoned, id) }))

	n, err := r.reconcileOnce(ctx)
	require.NoError(t, err, "poison row must not surface as a tick error")
	assert.Equal(t, 1, n, "healthy LB reconciled despite poison at head of queue")

	// Healthy row released + deleted.
	assert.False(t, lbExists(t, ctx, pool, healthyID), "healthy LB deleted")
	assert.Contains(t, rel.clears(), healthyAddr, "healthy VIP released")

	// Poison row isolated: preserved, updated_at bumped out of stuck-eligibility.
	assert.True(t, lbExists(t, ctx, pool, poisonID), "poison LB preserved (VIP not lost)")
	assert.False(t, lbStuckEligible(t, ctx, pool, poisonID, time.Minute),
		"poison LB updated_at bumped → no longer re-selected first (back-off)")
	assert.Equal(t, []string{poisonID}, poisoned, "poison observer fired once for the poison LB")
}

// TestFreeIP_TransientReleaseErrorLeavesRowForRetry — транзиентная release-ошибка
// (ErrUnavailable — vpc-peer недоступен) НЕ ядовитая: строка НЕ изолируется, а
// остаётся с нетронутым updated_at → self-heal на следующем тике при
// восстановлении peer'а. reconcileOnce возвращает ошибку (abort tick).
func TestFreeIP_TransientReleaseErrorLeavesRowForRetry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	const addr = "adr00000TRANSIENT001"
	lbID, _ := insertStuckLB(t, ctx, pool, domain.LBStatusDeleting, "linked", addr, "", "", 10*time.Minute)

	var poisoned []string
	rel := &fakeReleaser{errByAddr: map[string]error{
		addr: fmt.Errorf("%w: vpc clear address reference %s", domain.ErrUnavailable, addr),
	}}
	r := NewFreeIPRunner(pool, rel, observability.NewSlogger(discardWriter{}), time.Second, time.Minute,
		WithPoisonObserver(func(id string) { poisoned = append(poisoned, id) }))

	n, err := r.reconcileOnce(ctx)
	require.Error(t, err, "transient release error aborts the tick (retry next tick)")
	assert.True(t, errors.Is(err, domain.ErrUnavailable), "error preserves transient sentinel")
	assert.Equal(t, 0, n)

	assert.True(t, lbExists(t, ctx, pool, lbID), "row preserved for retry")
	assert.True(t, lbStuckEligible(t, ctx, pool, lbID, time.Minute),
		"transient failure leaves updated_at untouched → row still stuck-eligible (fast retry)")
	assert.Empty(t, poisoned, "transient failure must NOT be treated as poison")
}

// =============================================================================
// Run — full loop with ctx cancel.
// =============================================================================

// TestFreeIP_RunTickAndCancel — Run реконсилит stuck-строку в течение тика и
// чисто выходит на ctx cancel.
func TestFreeIP_RunTickAndCancel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	insertStuckLB(t, ctx, pool, domain.LBStatusDeleting, "auto", "adr00000RUNSTUCK0001", "", "", 10*time.Minute)

	rel := &fakeReleaser{}
	r := NewFreeIPRunner(pool, rel, observability.NewSlogger(discardWriter{}), 100*time.Millisecond, time.Minute)
	runErr := make(chan error, 1)
	go func() { runErr <- r.Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if countLoadBalancers(t, ctx, pool) == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.Equal(t, 0, countLoadBalancers(t, ctx, pool), "stuck LB reconciled within 3s")

	cancel()
	select {
	case e := <-runErr:
		assert.NoError(t, e, "Run must return nil on ctx cancel")
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within 2s after ctx cancel")
	}
}
