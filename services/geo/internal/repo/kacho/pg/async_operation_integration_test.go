// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/emptypb"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/pg"
)

func geoZoneStatusUp() domain.GeoStatus   { return domain.GeoStatusUp }
func geoZoneStatusDown() domain.GeoStatus { return domain.GeoStatusDown }

// seedRegion создаёт OPEN (status=UP) регион через use-case и проверяет успех.
// Каталог-мутации синхронно-завершены (Operation{done:true} сразу).
func seedRegion(t *testing.T, _ operations.Repo, uc *region.UseCase, id, name string) {
	t.Helper()
	op, err := uc.Create(context.Background(), region.CreateInput{ID: id, Name: name, Status: domain.GeoStatusUp})
	require.NoError(t, err)
	require.True(t, op.Done)
	require.Nil(t, op.Error)
}

// seedZone создаёт зону через use-case с явным статусом и проверяет успех.
func seedZone(t *testing.T, _ operations.Repo, uc *zone.UseCase, id, regionID, name string, st domain.GeoStatus) {
	t.Helper()
	op, err := uc.Create(context.Background(), zone.CreateInput{ID: id, RegionID: regionID, Name: name, Status: st})
	require.NoError(t, err)
	require.True(t, op.Done)
	require.Nil(t, op.Error)
}

// awaitOpDone — для sync-каталога возвращает уже-done строку немедленно (persisted
// в operations-таблице; тот же контракт, что OperationService.Get у клиента).
func awaitOpDone(t *testing.T, ops operations.Repo, opID string) *operations.Operation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		op, err := ops.Get(context.Background(), opID)
		if err == nil && op.Done {
			return op
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation %s did not finish within 3s", opID)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func unmarshalRegion(t *testing.T, op *operations.Operation) *geov1.Region {
	t.Helper()
	require.NotNil(t, op.Response, "response payload expected")
	msg, err := op.Response.UnmarshalNew()
	require.NoError(t, err)
	r, ok := msg.(*geov1.Region)
	require.True(t, ok, "response must be geov1.Region, got %T", msg)
	return r
}

func assertEmptyResponse(t *testing.T, op *operations.Operation) {
	t.Helper()
	require.NotNil(t, op.Response, "delete response is google.protobuf.Empty (set, not nil)")
	msg, err := op.Response.UnmarshalNew()
	require.NoError(t, err)
	_, ok := msg.(*emptypb.Empty)
	require.True(t, ok, "delete response must be google.protobuf.Empty, got %T", msg)
}

func unmarshalZone(t *testing.T, op *operations.Operation) *geov1.Zone {
	t.Helper()
	require.NotNil(t, op.Response, "response payload expected")
	msg, err := op.Response.UnmarshalNew()
	require.NoError(t, err)
	z, ok := msg.(*geov1.Zone)
	require.True(t, ok, "response must be geov1.Zone, got %T", msg)
	return z
}

// geo-sync-01: Region.Create → Operation{done:true} немедленно, response=public Region;
// persisted в operations-таблице; repo.Get отдаёт тот же ресурс. (GEO-1-16)
func TestSyncRegionCreate_Operation(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	uc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)

	op, err := uc.Create(ctx, region.CreateInput{ID: "region-sync-1", Name: "Region Sync One", CountryCode: "RU", Status: domain.GeoStatusUp})
	require.NoError(t, err)
	require.NotEmpty(t, op.ID)
	require.True(t, op.Done, "catalog Create returns done=true synchronously")
	require.Nil(t, op.Error)

	r := unmarshalRegion(t, op)
	require.Equal(t, "region-sync-1", r.GetId())
	require.Equal(t, "RU", r.GetCountryCode())
	require.True(t, r.GetOpenForPlacement())

	// persisted → OperationService.Get отдаёт тот же done:true.
	persisted := awaitOpDone(t, ops, op.ID)
	require.True(t, persisted.Done)

	got, err := pg.NewRegionRepo(pool).Get(ctx, "region-sync-1")
	require.NoError(t, err)
	require.Equal(t, "Region Sync One", got.Name)
}

// geo-sync-02: Region.Update(name) → done:true → обновлённый Region.
func TestSyncRegionUpdate_Operation(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	uc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)

	seedRegion(t, ops, uc, "region-sync-1", "Region Sync One")
	op2, err := uc.Update(ctx, region.UpdateInput{ID: "region-sync-1", Mask: []string{"name"}, Name: "Region Sync One Renamed"})
	require.NoError(t, err)
	require.True(t, op2.Done)
	require.Nil(t, op2.Error)
	require.Equal(t, "Region Sync One Renamed", unmarshalRegion(t, op2).GetName())
}

// geo-sync-03: Region.Delete → done:true, response=Empty; затем Get → NotFound.
func TestSyncRegionDelete_Operation(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	uc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)

	seedRegion(t, ops, uc, "region-sync-del", "to-be-deleted")
	op2, err := uc.Delete(ctx, "region-sync-del")
	require.NoError(t, err)
	require.True(t, op2.Done)
	require.Nil(t, op2.Error)
	assertEmptyResponse(t, op2)

	_, gerr := pg.NewRegionRepo(pool).Get(ctx, "region-sync-del")
	require.Error(t, gerr, "region must be gone after delete")
}

// geo-sync-04: Zone.Create → done:true, response=public Zone (openForPlacement derived).
func TestSyncZoneCreate_Operation(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	seedRegion(t, ops, ruc, "region-sync-1", "Region Sync One")
	zop, err := zuc.Create(ctx, zone.CreateInput{ID: "region-sync-1-a", RegionID: "region-sync-1", Name: "Zone Sync One A", Status: geoZoneStatusUp()})
	require.NoError(t, err)
	require.True(t, zop.Done)
	require.Nil(t, zop.Error)
	z := unmarshalZone(t, zop)
	require.Equal(t, "region-sync-1-a", z.GetId())
	require.Equal(t, "region-sync-1", z.GetRegionId())
	require.True(t, z.GetOpenForPlacement(), "zone UP under region UP → openForPlacement")
	require.NotNil(t, z.GetCreatedAt())
}

// geo-sync-05: Zone.Update + Zone.Delete sync parity.
func TestSyncZoneUpdateDelete_Operation(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	seedRegion(t, ops, ruc, "region-sync-1", "R")
	seedZone(t, ops, zuc, "region-sync-1-a", "region-sync-1", "Zone Sync One A", geoZoneStatusUp())

	uop, err := zuc.Update(ctx, zone.UpdateInput{ID: "region-sync-1-a", Mask: []string{"name", "status"}, Name: "Zone Sync One A Renamed", Status: geoZoneStatusDown()})
	require.NoError(t, err)
	require.True(t, uop.Done)
	require.Nil(t, uop.Error)
	z := unmarshalZone(t, uop)
	require.Equal(t, "Zone Sync One A Renamed", z.GetName())
	require.False(t, z.GetOpenForPlacement(), "zone DOWN → not open")

	dop, err := zuc.Delete(ctx, "region-sync-1-a")
	require.NoError(t, err)
	require.True(t, dop.Done)
	require.Nil(t, dop.Error)
	assertEmptyResponse(t, dop)

	_, gerr := pg.NewZoneRepo(pool).Get(ctx, "region-sync-1-a")
	require.Error(t, gerr)
}

// geo-sync-06: malformed (empty) id → sync InvalidArgument, NO operation created.
func TestSyncMalformedID_SyncInvalidArgument(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	_, err := ruc.Update(ctx, region.UpdateInput{ID: "", Name: "x", Mask: []string{"name"}})
	require.Error(t, err, "empty region id must fail synchronously")
	_, err = ruc.Delete(ctx, "")
	require.Error(t, err)
	_, err = ruc.Create(ctx, region.CreateInput{ID: "", Name: "x"})
	require.Error(t, err)
	_, err = zuc.Create(ctx, zone.CreateInput{ID: "", RegionID: "region-1", Name: "z"})
	require.Error(t, err)
}

// geo-sync-07: not-found → Operation.error NOT_FOUND (well-formed-but-absent).
func TestSyncNotFound_OperationError(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	uop, err := ruc.Update(ctx, region.UpdateInput{ID: "region-absent", Name: "x", Mask: []string{"name"}})
	require.NoError(t, err, "well-formed id accepted; failure in op.error")
	require.True(t, uop.Done)
	require.NotNil(t, uop.Error)
	require.Equal(t, int32(codes.NotFound), uop.Error.GetCode())

	dop, err := ruc.Delete(ctx, "region-absent")
	require.NoError(t, err)
	require.Equal(t, int32(codes.NotFound), dop.Error.GetCode())

	zop, err := zuc.Update(ctx, zone.UpdateInput{ID: "region-1-x", Name: "x", Mask: []string{"name"}})
	require.NoError(t, err)
	require.Equal(t, int32(codes.NotFound), zop.Error.GetCode())
}

// geo-sync-08: Region.Delete with zones → Operation.error FAILED_PRECONDITION
// "region <id> is not empty" (FK RESTRICT, GEO-1-18).
func TestSyncRegionDeleteWithZones_FailedPrecondition(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	seedRegion(t, ops, ruc, "region-sync-busy", "Busy")
	seedZone(t, ops, zuc, "region-sync-busy-a", "region-sync-busy", "Z", geoZoneStatusUp())

	dop, err := ruc.Delete(ctx, "region-sync-busy")
	require.NoError(t, err)
	require.True(t, dop.Done)
	require.NotNil(t, dop.Error)
	require.Equal(t, int32(codes.FailedPrecondition), dop.Error.GetCode())
	require.Equal(t, "region region-sync-busy is not empty", dop.Error.GetMessage())

	_, gerr := pg.NewRegionRepo(pool).Get(ctx, "region-sync-busy")
	require.NoError(t, gerr, "region stays while it has zones")
}

// geo-sync-09: Zone.Create on absent region → Operation.error FAILED_PRECONDITION
// (FK 23503; [PHASE-0-GATED] — by-lane NOT_FOUND deferred). (GEO-1-34)
func TestSyncZoneCreateBadRegion_FailedPrecondition(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	zop, err := zuc.Create(ctx, zone.CreateInput{ID: "region-ghost-a", RegionID: "region-ghost", Name: "Ghost Zone", Status: geoZoneStatusUp()})
	require.NoError(t, err)
	require.True(t, zop.Done)
	require.NotNil(t, zop.Error)
	require.Equal(t, int32(codes.FailedPrecondition), zop.Error.GetCode())

	_, gerr := pg.NewZoneRepo(pool).Get(ctx, "region-ghost-a")
	require.Error(t, gerr, "zone must not be created")
}

// geo-sync-10: concurrent Create same id → exactly one winner, others ALREADY_EXISTS.
func TestSyncConcurrentRegionCreate_OneWinner(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	uc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	opsOut := make([]*operations.Operation, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			// одинаковый id, но уникальные name (иначе UNIQUE(name) вместо PK-гонки).
			op, err := uc.Create(ctx, region.CreateInput{ID: "region-sync-race", Name: sprintfName(i), Status: domain.GeoStatusUp})
			require.NoError(t, err)
			opsOut[i] = op
		}(i)
	}
	wg.Wait()

	winners, conflicts := 0, 0
	for _, op := range opsOut {
		switch {
		case op.Error == nil:
			winners++
		case op.Error.GetCode() == int32(codes.AlreadyExists):
			conflicts++
		default:
			t.Fatalf("unexpected op error code: %d (%s)", op.Error.GetCode(), op.Error.GetMessage())
		}
	}
	require.Equal(t, 1, winners, "exactly one Create must win")
	require.Equal(t, n-1, conflicts, "the rest must report ALREADY_EXISTS")

	got, err := pg.NewRegionRepo(pool).Get(ctx, "region-sync-race")
	require.NoError(t, err)
	require.Equal(t, "region-sync-race", got.ID)
}

func sprintfName(i int) string { return "Race Region " + string(rune('A'+i)) }
