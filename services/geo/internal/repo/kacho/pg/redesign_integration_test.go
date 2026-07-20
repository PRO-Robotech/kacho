// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// GEO-1 redesign integration tests (testcontainers Postgres 16): two-projection
// at the SQL layer, fresh-DOWN persistence, cross-region openForPlacement°
// derivation, openZoneCount° rollup, the open_for_placement filter, and the
// mandatory UNIQUE(name) concurrent-race (data-integrity.md §checklist п.5).
package pg_test

import (
	"context"
	stderrors "errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/pg"
)

// TestGEO1_UniqueName_ConcurrentRace — GEO-1-36 + data-integrity.md п.5: global
// UNIQUE(name) под concurrency. N goroutine вставляют регионы с ОДИНАКОВЫМ name и
// РАЗНЫМИ id → ровно один INSERT проходит, остальные ErrAlreadyExists (23505,
// DB-backstop, без software-precheck). Без этого теста инвариант не мёржим.
func TestGEO1_UniqueName_ConcurrentRace(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	var mu sync.Mutex
	ok, dup := 0, 0
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := rr.Insert(ctx, &domain.Region{
				ID:     "region-name-race-" + string(rune('a'+i)), // разные id
				Name:   "Colliding Name",                          // одинаковое name
				Status: domain.GeoStatusUp,
			})
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
		}(i)
	}
	wg.Wait()
	require.Equal(t, 1, ok, "exactly one INSERT must win the UNIQUE(name) race")
	require.Equal(t, n-1, dup, "the rest must get ErrAlreadyExists")
}

// TestGEO1_ZoneUniqueName_ConcurrentRace — тот же инвариант на zones (отдельный
// код-путь: свой outbox-emit + region-status подтягивание).
func TestGEO1_ZoneUniqueName_ConcurrentRace(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)
	_, err := rr.Insert(ctx, &domain.Region{ID: "ru-central1", Name: "RU Central 1", Status: domain.GeoStatusUp})
	require.NoError(t, err)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	var mu sync.Mutex
	ok, dup := 0, 0
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, ierr := zr.Insert(ctx, &domain.Zone{
				ID: "ru-central1-" + string(rune('a'+i)), RegionID: "ru-central1",
				Name: "Colliding Zone Name", Status: domain.GeoStatusUp,
			})
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
		}(i)
	}
	wg.Wait()
	require.Equal(t, 1, ok)
	require.Equal(t, n-1, dup)
}

// TestGEO1_TwoProjection_InfraOnlyInternal — GEO-1-01/02: infra принимается на
// Create и читается GetInternal (full); публичный repo.Get НЕ читает infra-колонки
// (two-projection на SQL-уровне) но несёт derived region-status.
func TestGEO1_TwoProjection_InfraOnlyInternal(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ruc, zuc, _ := newUseCases(pool)

	seedRegion(t, nil, ruc, "ru-central1", "RU Central 1") // UP
	_, err := zuc.Create(ctx, zone.CreateInput{
		ID: "ru-central1-a", RegionID: "ru-central1", Name: "Zone A", Status: domain.GeoStatusUp,
		Infra: domain.ZoneInfra{NumericInfraID: 10402, HostClasses: []string{"std-v3", "mem-v2"}, FailureDomainCount: 3, UnderlayAnchor: "fd00:ru1a::/48", CapacityHint: "AMPLE"},
	})
	require.NoError(t, err)

	zr := pg.NewZoneRepo(pool)
	// GetInternal — full infra + status.
	iz, err := zr.GetInternal(ctx, "ru-central1-a")
	require.NoError(t, err)
	require.Equal(t, domain.GeoStatusUp, iz.Status)
	require.Equal(t, int64(10402), iz.Infra.NumericInfraID)
	require.Equal(t, []string{"std-v3", "mem-v2"}, iz.Infra.HostClasses)
	require.Equal(t, int32(3), iz.Infra.FailureDomainCount)
	require.Equal(t, "fd00:ru1a::/48", iz.Infra.UnderlayAnchor)
	require.Equal(t, "AMPLE", iz.Infra.CapacityHint)

	// Public Get — infra НЕ читается (zero) + region-status для деривации.
	pub, err := zr.Get(ctx, "ru-central1-a")
	require.NoError(t, err)
	require.Equal(t, int64(0), pub.Infra.NumericInfraID, "public read path must not fetch infra (two-projection)")
	require.Empty(t, pub.Infra.HostClasses, "host-classes must not leak via public read")
	require.Equal(t, domain.GeoStatusUp, pub.RegionStatus)
	require.True(t, pub.OpenForPlacement(), "zone UP under region UP → open")
}

// TestGEO1_FreshDOWN_Persisted — GEO-1-13: create region без status → persisted
// DOWN (fail-safe); GetInternal.status==DOWN; public openForPlacement=false;
// metadata.warnings несёт громкий no-op.
func TestGEO1_FreshDOWN_Persisted(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ruc, _, _ := newUseCases(pool)

	op, err := ruc.Create(ctx, region.CreateInput{ID: "eu-west1", Name: "EU West 1", CountryCode: "NL"}) // no status
	require.NoError(t, err)
	require.True(t, op.Done)
	require.Nil(t, op.Error)

	meta, err := operations.MetadataFor[*geov1.CreateRegionMetadata](op)
	require.NoError(t, err)
	require.Len(t, meta.GetWarnings(), 1)

	ir, err := pg.NewRegionRepo(pool).GetInternal(ctx, "eu-west1")
	require.NoError(t, err)
	require.Equal(t, domain.GeoStatusDown, ir.Status, "fresh region must persist DOWN (fail-safe), not UP")

	pub, err := pg.NewRegionRepo(pool).Get(ctx, "eu-west1")
	require.NoError(t, err)
	require.False(t, pub.OpenForPlacement())
}

// TestGEO1_ZoneOpenForPlacement_DependsOnRegion — GEO-1-07/26: zone UP под DOWN-регионом
// → derived openForPlacement=false, placementBlockedReason=REGION_DOWN (JOIN region.status).
func TestGEO1_ZoneOpenForPlacement_DependsOnRegion(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)
	// Регион DOWN, зона UP.
	_, err := rr.Insert(ctx, &domain.Region{ID: "ru-central1", Name: "RU Central 1", Status: domain.GeoStatusDown})
	require.NoError(t, err)
	_, err = zr.Insert(ctx, &domain.Zone{ID: "ru-central1-a", RegionID: "ru-central1", Name: "Zone A", Status: domain.GeoStatusUp})
	require.NoError(t, err)

	z, err := zr.Get(ctx, "ru-central1-a")
	require.NoError(t, err)
	require.False(t, z.OpenForPlacement(), "zone UP but region DOWN → not open")
	require.Equal(t, domain.PlacementBlockedReasonRegionDown, z.PlacementBlockedReason())

	// openForPlacement=true фильтр исключает зону под DOWN-регионом (GEO-1-26).
	open, _, err := zr.List(ctx, zone.Pagination{PageSize: 50, RegionID: "ru-central1", OpenForPlacement: true})
	require.NoError(t, err)
	require.Empty(t, open, "openForPlacement filter must exclude zones under a DOWN region")
}

// TestGEO1_OpenZoneCountHint_Rollup — GEO-1-25: region.openZoneCountHint° = read-time
// COUNT зон с openForPlacement°=true (2 UP + 1 DOWN → 2).
func TestGEO1_OpenZoneCountHint_Rollup(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)
	_, err := rr.Insert(ctx, &domain.Region{ID: "ru-central1", Name: "RU Central 1", Status: domain.GeoStatusUp})
	require.NoError(t, err)
	for _, z := range []struct {
		id string
		st domain.GeoStatus
	}{{"ru-central1-a", domain.GeoStatusUp}, {"ru-central1-b", domain.GeoStatusUp}, {"ru-central1-d", domain.GeoStatusDown}} {
		_, zerr := zr.Insert(ctx, &domain.Zone{ID: z.id, RegionID: "ru-central1", Name: "Zone " + z.id, Status: z.st})
		require.NoError(t, zerr)
	}

	r, err := rr.Get(ctx, "ru-central1")
	require.NoError(t, err)
	require.Equal(t, int64(2), r.OpenZoneCount, "openZoneCountHint = count of UP zones under an UP region")

	// Регион DOWN ⇒ hint=0 by construction (все зоны closed).
	_, err = rr.Update(ctx, "ru-central1", region.UpdateParams{Status: ptrStatus(domain.GeoStatusDown)})
	require.NoError(t, err)
	r2, err := rr.Get(ctx, "ru-central1")
	require.NoError(t, err)
	require.Equal(t, int64(0), r2.OpenZoneCount, "DOWN region → openZoneCountHint=0")
}

// TestGEO1_InfraMutable_NumericImmutable — GEO-1-04: Internal Update меняет
// infra.capacityHint/hostClasses; numericInfraId остаётся неизменным (в UpdateParams
// его нет — immutable путь удалён на уровне порта).
func TestGEO1_InfraMutable_NumericImmutable(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ruc, zuc, _ := newUseCases(pool)
	seedRegion(t, nil, ruc, "ru-central1", "RU Central 1")
	_, err := zuc.Create(ctx, zone.CreateInput{ID: "ru-central1-a", RegionID: "ru-central1", Name: "Zone A", Status: domain.GeoStatusUp,
		Infra: domain.ZoneInfra{NumericInfraID: 10402, CapacityHint: "AMPLE", HostClasses: []string{"std-v3", "mem-v2"}}})
	require.NoError(t, err)

	_, err = zuc.Update(ctx, zone.UpdateInput{ID: "ru-central1-a", Mask: []string{"infra.capacityHint", "infra.hostClasses"},
		Infra: domain.ZoneInfra{CapacityHint: "CONSTRAINED", HostClasses: []string{"std-v3"}}})
	require.NoError(t, err)

	iz, err := pg.NewZoneRepo(pool).GetInternal(ctx, "ru-central1-a")
	require.NoError(t, err)
	require.Equal(t, "CONSTRAINED", iz.Infra.CapacityHint)
	require.Equal(t, []string{"std-v3"}, iz.Infra.HostClasses)
	require.Equal(t, int64(10402), iz.Infra.NumericInfraID, "numericInfraId immutable — unchanged by infra-subset Update")
}

// newUseCases собирает region+zone use-cases поверх реальных pg-репо и corelib
// operations-таблицы (end-to-end через use-case, минус transport).
func newUseCases(pool *pgxpool.Pool) (*region.UseCase, *zone.UseCase, operations.Repo) {
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)
	return ruc, zuc, ops
}

func ptrStatus(s domain.GeoStatus) *domain.GeoStatus { return &s }
