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

	"github.com/PRO-Robotech/kacho/pkg/operations"
	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/pg"
)

func geoZoneStatusUp() domain.ZoneStatus   { return domain.ZoneStatusUp }
func geoZoneStatusDown() domain.ZoneStatus { return domain.ZoneStatusDown }

// seedRegion создаёт регион через async use-case и ждёт успешного завершения
// операции (setup-шаг — фейлит тест, если операция упала).
func seedRegion(t *testing.T, ops operations.Repo, uc *region.UseCase, id, name string) {
	t.Helper()
	op, err := uc.Create(context.Background(), id, name)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID).Error)
}

// seedZone создаёт зону через async use-case и ждёт успешного завершения.
func seedZone(t *testing.T, ops operations.Repo, uc *zone.UseCase, id, regionID, name string, st domain.ZoneStatus) {
	t.Helper()
	op, err := uc.Create(context.Background(), id, regionID, name, st)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID).Error)
}

// awaitOpDone детерминированно ждет завершения LRO-операции вместо time.Sleep:
// поллит operations.Repo.Get (тот же контракт, что OperationService.Get у клиента)
// до done=true. Таймаут 3s — admin-мутации каталога мгновенны.
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

// unmarshalRegion распаковывает Operation.response в geov1.Region.
func unmarshalRegion(t *testing.T, op *operations.Operation) *geov1.Region {
	t.Helper()
	require.NotNil(t, op.Response, "response payload expected")
	msg, err := op.Response.UnmarshalNew()
	require.NoError(t, err)
	r, ok := msg.(*geov1.Region)
	require.True(t, ok, "response must be geov1.Region, got %T", msg)
	return r
}

// assertEmptyResponse проверяет, что Operation.response — google.protobuf.Empty
// (Delete не несёт тела ресурса; payload — пустой Empty, не nil-Any).
func assertEmptyResponse(t *testing.T, op *operations.Operation) {
	t.Helper()
	require.NotNil(t, op.Response, "delete response is google.protobuf.Empty (set, not nil)")
	msg, err := op.Response.UnmarshalNew()
	require.NoError(t, err)
	_, ok := msg.(*emptypb.Empty)
	require.True(t, ok, "delete response must be google.protobuf.Empty, got %T", msg)
}

// unmarshalZone распаковывает Operation.response в geov1.Zone.
func unmarshalZone(t *testing.T, op *operations.Operation) *geov1.Zone {
	t.Helper()
	require.NotNil(t, op.Response, "response payload expected")
	msg, err := op.Response.UnmarshalNew()
	require.NoError(t, err)
	z, ok := msg.(*geov1.Zone)
	require.True(t, ok, "response must be geov1.Zone, got %T", msg)
	return z
}

// geo-async-01: Region.Create → Operation(done=false) → poll → done=true,
// response=Region; затем repo.Get отдает тот же ресурс.
func TestAsyncRegionCreate_Operation(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	uc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)

	op, err := uc.Create(ctx, "region-async-1", "Region Async One")
	require.NoError(t, err, "Create returns Operation synchronously (no error on accept)")
	require.NotEmpty(t, op.ID)
	require.False(t, op.Done, "operation must start done=false")
	require.Nil(t, op.Error)

	done := awaitOpDone(t, ops, op.ID)
	require.True(t, done.Done)
	require.Nil(t, done.Error, "happy create must not carry error")
	r := unmarshalRegion(t, done)
	require.Equal(t, "region-async-1", r.GetId())
	require.Equal(t, "Region Async One", r.GetName())
	require.NotNil(t, r.GetCreatedAt())

	got, err := pg.NewRegionRepo(pool).Get(ctx, "region-async-1")
	require.NoError(t, err)
	require.Equal(t, "Region Async One", got.Name)
}

// geo-async-02: Region.Update(name) → Operation → done → обновленный Region.
func TestAsyncRegionUpdate_Operation(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	uc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)

	op, err := uc.Create(ctx, "region-async-1", "Region Async One")
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID).Error)

	op2, err := uc.Update(ctx, "region-async-1", "Region Async One Renamed")
	require.NoError(t, err)
	require.False(t, op2.Done)
	done := awaitOpDone(t, ops, op2.ID)
	require.Nil(t, done.Error)
	require.Equal(t, "Region Async One Renamed", unmarshalRegion(t, done).GetName())
}

// geo-async-03: Region.Delete → Operation → done → response=Empty; затем Get → NotFound.
func TestAsyncRegionDelete_Operation(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	uc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)

	op, err := uc.Create(ctx, "region-async-del", "to-be-deleted")
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID).Error)

	op2, err := uc.Delete(ctx, "region-async-del")
	require.NoError(t, err)
	require.False(t, op2.Done)
	done := awaitOpDone(t, ops, op2.ID)
	require.Nil(t, done.Error, "happy delete must not carry error")
	assertEmptyResponse(t, done)

	_, gerr := pg.NewRegionRepo(pool).Get(ctx, "region-async-del")
	require.Error(t, gerr, "region must be gone after delete")
}

// geo-async-04: Zone.Create → Operation → done → response=Zone.
func TestAsyncZoneCreate_Operation(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	rop, err := ruc.Create(ctx, "region-async-1", "Region Async One")
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, rop.ID).Error)

	zop, err := zuc.Create(ctx, "region-async-1-a", "region-async-1", "Zone Async One A", geoZoneStatusUp())
	require.NoError(t, err)
	require.False(t, zop.Done)
	done := awaitOpDone(t, ops, zop.ID)
	require.Nil(t, done.Error)
	z := unmarshalZone(t, done)
	require.Equal(t, "region-async-1-a", z.GetId())
	require.Equal(t, "region-async-1", z.GetRegionId())
	require.Equal(t, geov1.Zone_UP, z.GetStatus())
	require.NotNil(t, z.GetCreatedAt())
}

// geo-async-05: Zone.Update + Zone.Delete async parity.
func TestAsyncZoneUpdateDelete_Operation(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	seedRegion(t, ops, ruc, "region-async-1", "R")
	seedZone(t, ops, zuc, "region-async-1-a", "region-async-1", "Zone Async One A", geoZoneStatusUp())

	uop, err := zuc.Update(ctx, "region-async-1-a", "region-async-1", "Zone Async One A Renamed", geoZoneStatusDown())
	require.NoError(t, err)
	udone := awaitOpDone(t, ops, uop.ID)
	require.Nil(t, udone.Error)
	z := unmarshalZone(t, udone)
	require.Equal(t, "Zone Async One A Renamed", z.GetName())
	require.Equal(t, geov1.Zone_DOWN, z.GetStatus())

	dop, err := zuc.Delete(ctx, "region-async-1-a")
	require.NoError(t, err)
	ddone := awaitOpDone(t, ops, dop.ID)
	require.Nil(t, ddone.Error)
	assertEmptyResponse(t, ddone)

	_, gerr := pg.NewZoneRepo(pool).Get(ctx, "region-async-1-a")
	require.Error(t, gerr)
}

// geo-async-06: malformed (empty) id → sync InvalidArgument, NO operation created.
func TestAsyncMalformedID_SyncInvalidArgument(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	_, err := ruc.Update(ctx, "", "x")
	require.Error(t, err, "empty region_id must fail synchronously")
	_, err = ruc.Delete(ctx, "")
	require.Error(t, err)
	_, err = ruc.Create(ctx, "", "x")
	require.Error(t, err)
	_, err = zuc.Create(ctx, "", "region-1", "z", geoZoneStatusUp())
	require.Error(t, err)
}

// geo-async-07: not-found → Operation with error.code=NOT_FOUND (well-formed-but-absent).
func TestAsyncNotFound_OperationError(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	uop, err := ruc.Update(ctx, "region-absent", "x")
	require.NoError(t, err, "well-formed id accepted; failure arrives async")
	udone := awaitOpDone(t, ops, uop.ID)
	require.NotNil(t, udone.Error)
	require.Equal(t, int32(codes.NotFound), udone.Error.GetCode())

	dop, err := ruc.Delete(ctx, "region-absent")
	require.NoError(t, err)
	require.Equal(t, int32(codes.NotFound), awaitOpDone(t, ops, dop.ID).Error.GetCode())

	zop, err := zuc.Update(ctx, "zone-absent", "", "x", geoZoneStatusUp())
	require.NoError(t, err)
	require.Equal(t, int32(codes.NotFound), awaitOpDone(t, ops, zop.ID).Error.GetCode())
}

// geo-async-08: Region.Delete with zones → Operation.error FAILED_PRECONDITION (FK RESTRICT).
func TestAsyncRegionDeleteWithZones_FailedPrecondition(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	seedRegion(t, ops, ruc, "region-async-busy", "Busy")
	seedZone(t, ops, zuc, "region-async-busy-a", "region-async-busy", "Z", geoZoneStatusUp())

	dop, err := ruc.Delete(ctx, "region-async-busy")
	require.NoError(t, err)
	done := awaitOpDone(t, ops, dop.ID)
	require.NotNil(t, done.Error)
	require.Equal(t, int32(codes.FailedPrecondition), done.Error.GetCode())

	// Регион остается.
	_, gerr := pg.NewRegionRepo(pool).Get(ctx, "region-async-busy")
	require.NoError(t, gerr)
}

// geo-async-09: Zone.Create on absent region → Operation.error FAILED_PRECONDITION (FK violation).
func TestAsyncZoneCreateBadRegion_FailedPrecondition(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	zuc := zone.New(pg.NewZoneRepo(pool), pg.NewZoneRepo(pool), ops, serviceerr.ToStatus)

	zop, err := zuc.Create(ctx, "region-ghost-a", "region-ghost", "Ghost Zone", geoZoneStatusUp())
	require.NoError(t, err)
	done := awaitOpDone(t, ops, zop.ID)
	require.NotNil(t, done.Error)
	require.Equal(t, int32(codes.FailedPrecondition), done.Error.GetCode())

	_, gerr := pg.NewZoneRepo(pool).Get(ctx, "region-ghost-a")
	require.Error(t, gerr, "zone must not be created")
}

// geo-async-10: idempotent re-poll + second Delete on already-deleted region.
func TestAsyncIdempotentRePollAndSecondDelete(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	ruc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)

	seedRegion(t, ops, ruc, "region-async-idem", "Idem")
	op1, err := ruc.Delete(ctx, "region-async-idem")
	require.NoError(t, err)
	d1 := awaitOpDone(t, ops, op1.ID)
	require.Nil(t, d1.Error)

	// re-poll стабилен.
	again, err := ops.Get(ctx, op1.ID)
	require.NoError(t, err)
	require.True(t, again.Done)
	require.Nil(t, again.Error)

	// Второй Delete: ресурса уже нет → Operation.error NOT_FOUND.
	op2, err := ruc.Delete(ctx, "region-async-idem")
	require.NoError(t, err)
	d2 := awaitOpDone(t, ops, op2.ID)
	require.NotNil(t, d2.Error)
	require.Equal(t, int32(codes.NotFound), d2.Error.GetCode())
}

// geo-async-11: concurrent Create same id → exactly one winner, others ALREADY_EXISTS.
func TestAsyncConcurrentRegionCreate_OneWinner(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ops := operations.NewRepo(pool, "kacho_geo")
	uc := region.New(pg.NewRegionRepo(pool), pg.NewRegionRepo(pool), ops, serviceerr.ToStatus)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	opIDs := make([]string, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			op, err := uc.Create(ctx, "region-async-race", "Race")
			require.NoError(t, err)
			opIDs[i] = op.ID
		}(i)
	}
	wg.Wait()

	winners, conflicts := 0, 0
	for _, id := range opIDs {
		done := awaitOpDone(t, ops, id)
		switch {
		case done.Error == nil:
			winners++
		case done.Error.GetCode() == int32(codes.AlreadyExists):
			conflicts++
		default:
			t.Fatalf("unexpected op error code: %d (%s)", done.Error.GetCode(), done.Error.GetMessage())
		}
	}
	require.Equal(t, 1, winners, "exactly one Create must win")
	require.Equal(t, n-1, conflicts, "the rest must report ALREADY_EXISTS")

	// Ровно один region-async-race в каталоге.
	got, err := pg.NewRegionRepo(pool).Get(ctx, "region-async-race")
	require.NoError(t, err)
	require.Equal(t, "region-async-race", got.ID)
}
