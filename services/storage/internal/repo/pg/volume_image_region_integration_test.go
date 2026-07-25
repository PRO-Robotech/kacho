// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// A storage Image is REGIONAL (anycast); a boot Volume is ZONAL. The two are
// coherent only when the volume's zone belongs to the image's region — which is
// what migration 0007 and storage/v1/image.proto both state. It was stated and
// never enforced: the insert predicate only compared projects.
//
// The invariant lives inside the same database, so it is enforced inside the
// insert CAS itself (data-integrity.md ban #10 — not a read-then-write check).

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

// A boot volume whose zone belongs to ANOTHER region than the image must not be
// seeded. The answer is the same hide-existence wording a real miss produces, so
// the refusal is not an oracle for images of other regions.
func TestVolumeSourceImageForeignRegionRejected(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-foreign-region", 20<<30)
	img := mkImageFromSnapshot(t, ir, "prj-1", "img-region-1", "region-1", snapID)

	_, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "boot-foreign-region",
		ZoneID: "region-2-a", DiskTypeID: seededDiskType, SizeBytes: 21474836480,
		SourceImage: img.ID,
	}, "region-2") // the zone's region, resolved from the owner of Geography
	require.Error(t, err, "a boot volume outside the image's region must not be seeded")
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Image "+img.ID+" not found", err.Error()[len("failed precondition: "):])
}

// Same region → seeded.
func TestVolumeSourceImageSameRegionSeeded(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-same-region", 20<<30)
	img := mkImageFromSnapshot(t, ir, "prj-1", "img-same-region", "region-1", snapID)

	boot, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "boot-same-region",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 21474836480,
		SourceImage: img.ID,
	}, "region-1")
	require.NoError(t, err)
	require.Equal(t, img.ID, boot.SourceImage)
}

// A source-less volume carries no image, so there is no region to compare and the
// unresolved region must not block it.
func TestVolumeWithoutSourceUnaffectedByRegion(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	v, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "plain-vol-region",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
	}, "")
	require.NoError(t, err)
	require.Empty(t, v.SourceImage)
}
