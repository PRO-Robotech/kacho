// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// TestVolumeSourceImageSeed — STOR-1-18 (NET-NEW): Create volume с source_image_id из
// существующего Image → boot-Volume засеян; Get показывает sourceImageId.
func TestVolumeSourceImageSeed(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-seed", 20<<30)
	img := mkImageFromSnapshot(t, ir, "prj-1", "ubuntu-boot", "ru-central1", snapID)

	boot, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "boot-vol",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 21474836480,
		SourceImage: img.ID,
	})
	require.NoError(t, err)
	require.Equal(t, img.ID, boot.SourceImage)

	got, err := vr.Get(ctx, boot.ID)
	require.NoError(t, err)
	require.Equal(t, img.ID, got.SourceImage, "sourceImageId persisted on the boot volume")
	require.Empty(t, got.SourceSnapshot, "boot volume seeded from image, not snapshot")
	require.Equal(t, domain.VolumeStatusAvailable, got.Status)
}

// TestVolumeSourceImageFKNotFound — STOR-1-19: неизвестный source_image → same-DB FK
// 23503 → FailedPrecondition "Image <id> not found" (контрактный тон).
func TestVolumeSourceImageFKNotFound(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	_, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "boot-badimg",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
		SourceImage: "img00000000000000000",
	})
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Image img00000000000000000 not found", err.Error()[len("failed precondition: "):])
}

// TestImageDeleteSetsVolumeSourceImageNull — STOR-1-28 (NET-NEW, behaviour): Image.Delete
// засевшего образа ПРОХОДИТ (FK ON DELETE SET NULL, НЕ RESTRICT); boot-Volume не затронут
// (AVAILABLE, size неизменен), sourceImageId очищен; Image → NotFound.
func TestImageDeleteSetsVolumeSourceImageNull(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-prov", 20<<30)
	img := mkImageFromSnapshot(t, ir, "prj-1", "prov-img", "ru-central1", snapID)
	boot, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "prov-boot",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 21474836480,
		SourceImage: img.ID,
	})
	require.NoError(t, err)

	// Delete Image, засевшего в томе → проходит (provenance SET NULL, не RESTRICT).
	require.NoError(t, ir.Delete(ctx, img.ID), "deleting a seeded image must succeed (SET NULL, not RESTRICT)")

	// Image ушёл.
	_, gerr := ir.Get(ctx, img.ID)
	require.True(t, stderrors.Is(gerr, ports.ErrNotFound), "image hard-deleted, got %v", gerr)

	// boot-Volume цел: sourceImageId очищен (lineage-clear), остальное неизменно.
	gotV, err := vr.Get(ctx, boot.ID)
	require.NoError(t, err, "boot volume must remain after image delete")
	require.Empty(t, gotV.SourceImage, "source_image_id SET NULL (lineage cleared)")
	require.EqualValues(t, 21474836480, gotV.SizeBytes, "block data untouched by image delete")
	require.Equal(t, domain.VolumeStatusAvailable, gotV.Status)
}
