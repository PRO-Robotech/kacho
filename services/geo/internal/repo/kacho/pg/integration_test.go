// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	stderrors "errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/migrations"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/pg"
)

// newTestPool поднимает контейнер Postgres 16, прогоняет миграции kacho-geo
// и возвращает pgxpool с search_path=kacho_geo. Пропускается под -short.
// Каталог стартует пустым (seed нет) — данные каждый тест заводит сам.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test (testcontainers Postgres) — skipped with -short")
	}
	ctx := context.Background()

	pgC, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("kacho_geo"),
		tcpostgres.WithUsername("geo"),
		tcpostgres.WithPassword("secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgC.Terminate(ctx) })

	baseDSN, err := pgC.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Миграции через goose (database/sql).
	sqlDB, err := sql.Open("pgx", baseDSN)
	require.NoError(t, err)
	defer sqlDB.Close()
	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.Up(sqlDB, "."))

	// pgxpool с search_path=kacho_geo, чтобы SQL репозитория видел таблицы без
	// схемы-префикса. Форма libpq runtime-param (URL-query `search_path=` pgx не
	// учитывает).
	poolDSN := baseDSN + "&options=-c%20search_path%3Dkacho_geo%2Cpublic"
	pool, err := coredb.NewPool(ctx, poolDSN)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func TestRegionGetNotFound(t *testing.T) {
	pool := newTestPool(t)
	rr := pg.NewRegionRepo(pool)
	_, err := rr.Get(context.Background(), "no-such-region")
	require.True(t, stderrors.Is(err, geoerrors.ErrNotFound), "got %v", err)
}

// TestZoneGetNotFound — отсутствующий zone id → ErrNotFound (parity с
// TestRegionGetNotFound; Zone.Get — отдельный SELECT + status-scan путь, не
// покрывается region-тестом транзитивно).
func TestZoneGetNotFound(t *testing.T) {
	pool := newTestPool(t)
	zr := pg.NewZoneRepo(pool)
	_, err := zr.Get(context.Background(), "no-such-zone")
	require.True(t, stderrors.Is(err, geoerrors.ErrNotFound), "got %v", err)
}

func TestRegionList(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)

	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)

	regions, _, err := rr.List(ctx, region.Pagination{PageSize: 50})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(regions), 1)
}

func TestZoneList(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)

	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)
	for _, z := range []string{"region-1-a", "region-1-b"} {
		_, zerr := zr.Insert(ctx, &domain.Zone{ID: z, RegionID: "region-1", Status: domain.ZoneStatusUp})
		require.NoError(t, zerr)
	}

	zones, _, err := zr.List(ctx, zone.Pagination{PageSize: 50})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(zones), 2)
}

func TestRegionCRUDAndOutbox(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)

	created, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)
	require.Equal(t, "region-1", created.ID)

	// Строка outbox CREATED записана атомарно.
	require.Equal(t, 1, outboxCount(t, pool, "Region", "region-1", "CREATED"))

	updated, err := rr.Update(ctx, "region-1", strPtr("Region One"))
	require.NoError(t, err)
	require.Equal(t, "Region One", updated.Name)
	require.Equal(t, 1, outboxCount(t, pool, "Region", "region-1", "UPDATED"))

	require.NoError(t, rr.Delete(ctx, "region-1"))
	require.Equal(t, 1, outboxCount(t, pool, "Region", "region-1", "DELETED"))
}

// TestRegionInsertDuplicate — повторный INSERT того же id → ErrAlreadyExists (UNIQUE PK).
func TestRegionInsertDuplicate(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)

	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)
	_, err = rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "dup"})
	require.True(t, stderrors.Is(err, geoerrors.ErrAlreadyExists), "got %v", err)
}

// TestZoneFKRestrict_DeleteRegionWithZones — удаление региона, у которого есть
// зона, упирается в FK RESTRICT zones→regions на DB-уровне (источник истины,
// без software-precheck) и поднимается как FailedPrecondition.
func TestZoneFKRestrict_DeleteRegionWithZones(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)

	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)
	_, err = zr.Insert(ctx, &domain.Zone{ID: "region-1-a", RegionID: "region-1", Status: domain.ZoneStatusUp})
	require.NoError(t, err)

	err = rr.Delete(ctx, "region-1")
	require.True(t, stderrors.Is(err, geoerrors.ErrFailedPrecondition), "FK RESTRICT must surface as FailedPrecondition, got %v", err)
}

func TestZoneCreateFKViolation_NoSuchRegion(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	zr := pg.NewZoneRepo(pool)
	_, err := zr.Insert(ctx, &domain.Zone{ID: "z-a", RegionID: "no-such-region", Status: domain.ZoneStatusUp})
	require.True(t, stderrors.Is(err, geoerrors.ErrFailedPrecondition), "FK violation must surface as FailedPrecondition, got %v", err)
}

func TestRegionDeleteThenZoneDelete_FKLifecycle(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)

	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)
	_, err = zr.Insert(ctx, &domain.Zone{ID: "region-1-a", RegionID: "region-1", Status: domain.ZoneStatusUp, Name: "Region 1 A"})
	require.NoError(t, err)

	// Удаление региона заблокировано, пока существует зона.
	require.True(t, stderrors.Is(rr.Delete(ctx, "region-1"), geoerrors.ErrFailedPrecondition))

	// Удаляем зону — затем удаление региона проходит.
	require.NoError(t, zr.Delete(ctx, "region-1-a"))
	require.NoError(t, rr.Delete(ctx, "region-1"))
}

// TestConcurrentRegionInsert_OneWins — UNIQUE PK под concurrency: ровно один
// INSERT выигрывает, остальные получают ErrAlreadyExists.
func TestConcurrentRegionInsert_OneWins(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	var mu sync.Mutex
	ok, dup := 0, 0
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := rr.Insert(ctx, &domain.Region{ID: "race-region", Name: "Race"})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case stderrors.Is(err, geoerrors.ErrAlreadyExists):
				dup++
			default:
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, ok, "exactly one INSERT must win")
	require.Equal(t, n-1, dup, "the rest must get ErrAlreadyExists")
}

// TestConcurrentZoneInsertVsRegionDelete_NoOrphan — контестный кросс-ресурсный
// FK-путь: Zone.Insert в регион, который ПАРАЛЛЕЛЬНО Region.Delete'ится
// (FK RESTRICT zones→regions). Правило #10 п.5 требует concurrent-goroutine тест
// на КАЖДЫЙ спорный within-service инвариант; PK-дубли уже покрыты
// (TestConcurrentRegionInsert_OneWins / TestConcurrentZoneInsert_OneWins), а этот
// классический attach-vs-delete путь ранее гонялся лишь последовательно
// (TestZoneFKRestrict_DeleteRegionWithZones / TestRegionDeleteThenZoneDelete_FKLifecycle).
//
// Гарантия Postgres (child-INSERT берёт FOR KEY SHARE на parent-row, parent-DELETE
// ждёт row-lock): ровно одна операция выигрывает, orphan невозможен ни при каком
// интерливинге:
//   - insert выигрывает → zone есть, region есть, delete → FailedPrecondition (RESTRICT);
//   - delete выигрывает → zone нет, region нет, insert → FailedPrecondition (FK 23503).
//
// Обе успеть не могут (orphan-запрет), обе провалиться не могут. Тест ловит
// регрессию, если кто-то ослабит FK или подменит его software-precheck'ом (TOCTOU).
func TestConcurrentZoneInsertVsRegionDelete_NoOrphan(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)

	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)

	var (
		wg        sync.WaitGroup
		start     = make(chan struct{})
		insertErr error
		deleteErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, insertErr = zr.Insert(ctx, &domain.Zone{ID: "region-1-a", RegionID: "region-1", Status: domain.ZoneStatusUp})
	}()
	go func() {
		defer wg.Done()
		<-start
		deleteErr = rr.Delete(ctx, "region-1")
	}()
	close(start) // старт обеих goroutine одновременно — максимизируем контеншн
	wg.Wait()

	switch {
	case insertErr == nil:
		// insert выиграл: region+zone на месте, delete упёрся в RESTRICT.
		require.True(t, stderrors.Is(deleteErr, geoerrors.ErrFailedPrecondition),
			"insert won → region delete must be FailedPrecondition (FK RESTRICT), got %v", deleteErr)
		_, gerr := rr.Get(ctx, "region-1")
		require.NoError(t, gerr, "region must still exist while its zone references it")
		_, zgerr := zr.Get(ctx, "region-1-a")
		require.NoError(t, zgerr, "the winning zone must be present")
	case deleteErr == nil:
		// delete выиграл: region+zone отсутствуют, insert упёрся в FK 23503.
		require.True(t, stderrors.Is(insertErr, geoerrors.ErrFailedPrecondition),
			"delete won → zone insert must be FailedPrecondition (FK 23503), got %v", insertErr)
		_, zgerr := zr.Get(ctx, "region-1-a")
		require.True(t, stderrors.Is(zgerr, geoerrors.ErrNotFound),
			"no orphan zone may reference a deleted region, got %v", zgerr)
	default:
		t.Fatalf("both ops failed (insert=%v, delete=%v) — exactly one must win", insertErr, deleteErr)
	}
}

// strPtr — указатель на строку (опциональный update-параметр).
func strPtr(s string) *string { return &s }

// TestZoneUpdateAndOutbox — атомарный partial-Update зоны (name+status) пишет
// результирующую строку, geo_outbox UPDATED, и состояние видно через re-Get.
func TestZoneUpdateAndOutbox(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)

	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)
	_, err = zr.Insert(ctx, &domain.Zone{ID: "region-1-a", RegionID: "region-1", Name: "Zone A", Status: domain.ZoneStatusUp})
	require.NoError(t, err)

	down := domain.ZoneStatusDown
	updated, err := zr.Update(ctx, "region-1-a", zone.UpdateParams{
		Name:   strPtr("Zone A renamed"),
		Status: &down,
	})
	require.NoError(t, err)
	require.Equal(t, "Zone A renamed", updated.Name)
	require.Equal(t, domain.ZoneStatusDown, updated.Status)
	require.Equal(t, "region-1", updated.RegionID, "region_id must be unchanged (not zeroed)")

	// re-Get видит обновленное состояние.
	got, err := zr.Get(ctx, "region-1-a")
	require.NoError(t, err)
	require.Equal(t, "Zone A renamed", got.Name)
	require.Equal(t, domain.ZoneStatusDown, got.Status)

	require.Equal(t, 1, outboxCount(t, pool, "Zone", "region-1-a", "UPDATED"))
}

// TestRegionListPagination — курсорная пагинация по id: PageSize=2 отдает 2 +
// непустой токен; повтор с токеном отдает остаток (1) + пустой токен.
func TestRegionListPagination(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)

	for _, id := range []string{"region-1", "region-1-a", "region-1-b"} {
		_, err := rr.Insert(ctx, &domain.Region{ID: id, Name: id})
		require.NoError(t, err)
	}

	page1, token, err := rr.List(ctx, region.Pagination{PageSize: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, token, "non-empty page_token expected when more rows remain")

	page2, token2, err := rr.List(ctx, region.Pagination{PageSize: 2, PageToken: token})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	require.Empty(t, token2, "empty page_token expected on last page")
}

// TestZoneListPagination — курсорная пагинация Zone.List по id (parity с
// TestRegionListPagination): Zone.List — отдельный код-путь (свой SELECT со
// status-колонкой + собственная next-token slice/encode логика out[pageSize-1] /
// out[:pageSize]), region-тест его транзитивно НЕ покрывает. PageSize=2 отдаёт 2 +
// непустой токен; повтор с токеном отдаёт остаток (1) + пустой токен. Ловит
// off-by-one в next-token (лишний/пропущенный курсор на границе pageSize).
func TestZoneListPagination(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)

	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)
	for _, id := range []string{"region-1-a", "region-1-b", "region-1-c"} {
		_, zerr := zr.Insert(ctx, &domain.Zone{ID: id, RegionID: "region-1", Status: domain.ZoneStatusUp})
		require.NoError(t, zerr)
	}

	page1, token, err := zr.List(ctx, zone.Pagination{PageSize: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.Equal(t, []string{"region-1-a", "region-1-b"}, []string{page1[0].ID, page1[1].ID})
	require.NotEmpty(t, token, "non-empty page_token expected when more rows remain")

	page2, token2, err := zr.List(ctx, zone.Pagination{PageSize: 2, PageToken: token})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	require.Equal(t, "region-1-c", page2[0].ID, "second page must carry the remaining zone")
	require.Empty(t, token2, "empty page_token expected on last page")
}

// TestZoneUpdateFK_NoSuchRegion — Zone.Update, перенаправляющий region_id на
// несуществующий регион, упирается в FK 23503 zones→regions → FailedPrecondition.
// Транзакция откатывается целиком, поэтому region_id остаётся прежним (partial
// re-point региона Create-путь тестируется отдельно; здесь — Update-путь).
func TestZoneUpdateFK_NoSuchRegion(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)

	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)
	_, err = zr.Insert(ctx, &domain.Zone{ID: "region-1-a", RegionID: "region-1", Name: "Zone A", Status: domain.ZoneStatusUp})
	require.NoError(t, err)

	ghost := "no-such-region"
	_, uerr := zr.Update(ctx, "region-1-a", zone.UpdateParams{RegionID: &ghost})
	require.True(t, stderrors.Is(uerr, geoerrors.ErrFailedPrecondition),
		"re-point region_id to a ghost region must surface FK 23503 as FailedPrecondition, got %v", uerr)

	// region_id не изменился — UPDATE откатился целиком.
	got, gerr := zr.Get(ctx, "region-1-a")
	require.NoError(t, gerr)
	require.Equal(t, "region-1", got.RegionID, "region_id must be unchanged after a failed re-point")
}

// TestZoneInsertDuplicate — повторный INSERT той же зоны → ErrAlreadyExists
// (UNIQUE PK). Zone Insert — отдельный от Region код-путь (свой outbox emit),
// поэтому дублируем проверку явно, а не полагаемся на region-parity.
func TestZoneInsertDuplicate(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)

	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)
	_, err = zr.Insert(ctx, &domain.Zone{ID: "region-1-a", RegionID: "region-1", Status: domain.ZoneStatusUp})
	require.NoError(t, err)
	_, err = zr.Insert(ctx, &domain.Zone{ID: "region-1-a", RegionID: "region-1", Status: domain.ZoneStatusUp})
	require.True(t, stderrors.Is(err, geoerrors.ErrAlreadyExists), "got %v", err)
}

// TestConcurrentZoneInsert_OneWins — UNIQUE PK zones под concurrency: ровно один
// INSERT одной и той же зоны выигрывает, остальные получают ErrAlreadyExists
// (DB-уровень, без software-precheck; parity с TestConcurrentRegionInsert_OneWins).
func TestConcurrentZoneInsert_OneWins(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)

	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	var mu sync.Mutex
	ok, dup := 0, 0
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, ierr := zr.Insert(ctx, &domain.Zone{ID: "race-region-a", RegionID: "region-1", Status: domain.ZoneStatusUp})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case ierr == nil:
				ok++
			case stderrors.Is(ierr, geoerrors.ErrAlreadyExists):
				dup++
			default:
				t.Errorf("unexpected err: %v", ierr)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, ok, "exactly one INSERT must win")
	require.Equal(t, n-1, dup, "the rest must get ErrAlreadyExists")
}

// TestList_malformedPageToken_invalidArgument — битый page_token (не-base64,
// битый JSON, пустой cursor id) в Region/Zone List → доменный sentinel
// geoerrors.ErrInvalidArg (repo-слой НЕ конструирует gRPC-status сам — выбор кода
// transport-concern через serviceerr.ToStatus, как и остальные repo-ошибки). Через
// serviceerr.ToStatus sentinel маппится в codes.InvalidArgument без утечки
// внутренней decode-детали как отдельного кода.
func TestList_malformedPageToken_invalidArgument(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)

	badTokens := []string{
		"!!!notbase64", // не base64
		base64.StdEncoding.EncodeToString([]byte("{")),  // base64, но битый JSON
		base64.StdEncoding.EncodeToString([]byte(`{}`)), // валидный JSON, пустой cursor id
	}
	for _, tok := range badTokens {
		_, _, rerr := rr.List(ctx, region.Pagination{PageSize: 10, PageToken: tok})
		require.ErrorIs(t, rerr, geoerrors.ErrInvalidArg, "region List(page_token=%q) sentinel", tok)
		require.Equal(t, codes.InvalidArgument, status.Code(serviceerr.ToStatus(rerr)), "region List(page_token=%q) mapped code", tok)
		_, _, zerr := zr.List(ctx, zone.Pagination{PageSize: 10, PageToken: tok})
		require.ErrorIs(t, zerr, geoerrors.ErrInvalidArg, "zone List(page_token=%q) sentinel", tok)
		require.Equal(t, codes.InvalidArgument, status.Code(serviceerr.ToStatus(zerr)), "zone List(page_token=%q) mapped code", tok)
	}
}

func outboxCount(t *testing.T, pool *pgxpool.Pool, kind, id, eventType string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM geo_outbox WHERE resource_kind=$1 AND resource_id=$2 AND event_type=$3`,
		kind, id, eventType).Scan(&n)
	require.NoError(t, err)
	return n
}
