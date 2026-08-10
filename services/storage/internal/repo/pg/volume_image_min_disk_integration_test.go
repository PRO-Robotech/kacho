// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// A storage Image carries min_disk_bytes — the smallest receiving boot volume the
// image can be written into. It was derived on Image.Create and echoed back, and
// nothing ever compared a boot volume against it: a 1 GiB volume could be seeded
// from a 20 GiB image. The comment on storage/v1/image.proto claimed the receiving
// side enforced it; the receiving side enforced it only for the block-storage
// duplicate that lives in compute, over compute's own Disk/Image pair — never over
// a storage Volume.
//
// Both rows live in the same database, so the comparison belongs inside the insert
// CAS itself, not in a read-then-write check (data-integrity.md ban #10).
//
// The refusal must stay distinguishable from "the image does not resolve": an image
// that failed the project/region lane is one the caller may not see at all, and its
// min_disk_bytes must never appear in the answer.

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

// A boot volume smaller than the image's min_disk_bytes must not be seeded, and the
// answer must name the shortfall (the image already resolved in the caller's own
// project and region, so its minimum is not secret at this point).
func TestVolumeSourceImageBelowMinDiskRejected(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	// min_disk_bytes is derived from the source snapshot: 20 GiB.
	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-min-disk", 20<<30)
	img := mkImageFromSnapshot(t, ir, "prj-1", "img-min-disk", imageRegionFixture, snapID)
	require.EqualValues(t, 20<<30, img.MinDiskBytes, "fixture must carry a non-zero minimum")

	_, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "boot-too-small",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
		SourceImage: img.ID,
	}, imageRegionFixture)
	require.Error(t, err, "a boot volume below the image minimum must not be seeded")
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "got %v", err)
	require.Equal(t,
		"Volume size 1073741824 is less than image min_disk_bytes 21474836480",
		err.Error()[len("invalid argument: "):])
}

// Exactly at the minimum is allowed — the bound is inclusive.
func TestVolumeSourceImageAtMinDiskSeeded(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-at-min", 20<<30)
	img := mkImageFromSnapshot(t, ir, "prj-1", "img-at-min", imageRegionFixture, snapID)

	boot, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "boot-at-min",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 20 << 30,
		SourceImage: img.ID,
	}, imageRegionFixture)
	require.NoError(t, err, "a volume exactly at the image minimum must be seeded")
	require.Equal(t, img.ID, boot.SourceImage)
}

// An image the caller may not reach must keep answering with the hide-existence
// wording even when the volume is also below its minimum: the size lane must never
// become an oracle that confirms a foreign image exists (or how large it is).
func TestVolumeSourceImageCrossProjectStillHidesMinDisk(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-victim", "snap-victim-min", 20<<30)
	img := mkImageFromSnapshot(t, ir, "prj-victim", "img-victim-min", imageRegionFixture, snapID)

	_, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "boot-victim-min",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
		SourceImage: img.ID,
	}, imageRegionFixture)
	require.Error(t, err)
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Image "+img.ID+" not found", err.Error()[len("failed precondition: "):],
		"a foreign image must not leak its minimum through a size refusal")
}

// A source-less volume has no image to compare against, so the minimum must not
// block it at any size.
func TestVolumeWithoutSourceUnaffectedByMinDisk(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	v, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "plain-vol-min-disk",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 20,
	}, "")
	require.NoError(t, err)
	require.Empty(t, v.SourceImage)
}

// A snapshot-seeded volume goes down the snapshot lane, which carries no minimum —
// the image lane must not spill onto it.
func TestVolumeSourceSnapshotUnaffectedByMinDisk(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-lane-min", 20<<30)

	v, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "boot-from-snap-min",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
		SourceSnapshot: snapID,
	}, "")
	require.NoError(t, err, "the snapshot lane carries no image minimum")
	require.Equal(t, snapID, v.SourceSnapshot)
}
