// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// DiskType.zone_ids is documented — in storage/v1/disk_type.proto and in migration
// 0003 — as "the availability zones where this disk type is offered", with an empty
// list meaning "offered everywhere". InternalDiskTypeService.Create/Update let an
// administrator set it. Volume.Create never looked at it, so the list restricted
// nothing: a volume could be provisioned on a type its own catalogue entry says is
// not offered in that zone.
//
// Volume and DiskType are rows of the same database, so the invariant belongs in
// the insert CAS itself, not in a read-then-write check before it
// (data-integrity.md ban #10). The empty list is the anycast-shaped exception from
// the placement-coherence rule: a type that is not zone-scoped has nothing to
// compare against, so the zonal lane is satisfied by construction.

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// TestVolumeRejectedOnDiskTypeNotOfferedInZone — the volume's zone is absent from
// the type's zone list, so the insert must not happen, and the refusal must name
// both sides in the contract's wording.
func TestVolumeRejectedOnDiskTypeNotOfferedInZone(t *testing.T) {
	pool := newTestPool(t)
	dr := pg.NewDiskTypeRepo(pool)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	_, err := dr.Insert(ctx, &domain.DiskType{
		ID: "block-zoned-a", Name: "block-zoned-a", ZoneIDs: []string{"region-1-a"},
		PerformanceTier: "balanced",
	})
	require.NoError(t, err)

	_, _, err = vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-wrong-zone",
		ZoneID: "region-1-b", DiskTypeID: "block-zoned-a", SizeBytes: 1 << 30,
	}, "")
	require.Error(t, err, "a volume must not be provisioned on a disk type not offered in its zone")
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "DiskType block-zoned-a is not offered in zone region-1-b",
		err.Error()[len("failed precondition: "):])
}

// TestVolumeAcceptedOnDiskTypeOfferedInZone — the volume's zone is listed, so the
// zonal lane is satisfied and the row is inserted.
func TestVolumeAcceptedOnDiskTypeOfferedInZone(t *testing.T) {
	pool := newTestPool(t)
	dr := pg.NewDiskTypeRepo(pool)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	_, err := dr.Insert(ctx, &domain.DiskType{
		ID: "block-zoned-b", Name: "block-zoned-b", ZoneIDs: []string{"region-1-a", "region-1-b"},
		PerformanceTier: "balanced",
	})
	require.NoError(t, err)

	v, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-right-zone",
		ZoneID: "region-1-b", DiskTypeID: "block-zoned-b", SizeBytes: 1 << 30,
	}, "")
	require.NoError(t, err)
	require.Equal(t, "block-zoned-b", v.DiskTypeID)
}

// TestVolumeAcceptedOnUnscopedDiskType — пустой список зон у класса означает
// «класс сам себя зонами не ограничивает», а НЕ «предлагается везде».
//
// Прежняя редакция утверждала второе и обосновывала это тем, что иначе «перестал
// бы работать весь посеянный каталог, где у каждой записи пустой список». Посева
// больше нет: класс предлагается там, где объявлено, ЧЕМ он обслуживается, —
// то есть где у него есть ДЕЙСТВУЮЩАЯ ревизия привязки. Утверждение не ослаблено,
// а приведено к предмету: проверяются ОБЕ половины, и вторая — та, ради которой
// привязка и заведена.
func TestVolumeAcceptedOnUnscopedDiskType(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	// Половина первая: зона, где класс привязан, — принимается.
	v, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-unscoped-type",
		ZoneID: fixtureZone, DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
	}, "")
	require.NoError(t, err)
	require.Equal(t, seededDiskType, v.DiskTypeID)

	// Половина вторая: зона, где привязки нет, — отвергается, и отказ говорит
	// именно об отсутствии обслуживания, а не о запрете класса.
	_, _, err = vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-unbound-zone",
		ZoneID: "any-zone-at-all", DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
	}, "")
	require.Error(t, err, "класс без действующей ревизии в зоне не обслуживается")
	require.Contains(t, err.Error(), "has no active binding in zone any-zone-at-all")
}

// TestVolumeOnMissingDiskTypeStillReportsMissing — the new lane must not swallow
// the pre-existing answer for a type that does not exist at all. Absent row and
// wrong zone are different facts and keep different wordings.
func TestVolumeOnMissingDiskTypeStillReportsMissing(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	_, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-no-such-type",
		ZoneID: "region-1-a", DiskTypeID: "no-such-disk-type", SizeBytes: 1 << 30,
	}, "")
	require.Error(t, err)
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "DiskType no-such-disk-type not found",
		err.Error()[len("failed precondition: "):])
}
