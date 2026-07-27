// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// An Image is REGIONAL; the Volume it is captured from is ZONAL. They are coherent
// only when the volume's zone belongs to the image's region. The opposite direction
// (seeding a Volume FROM an Image) already enforced exactly this; capturing an Image
// FROM a Volume did not — its insert predicate compared projects only. A caller
// could therefore publish a region-1 image whose bytes come from a region-2 volume,
// which both contradicts what migration 0007 and image.proto state and quietly moves
// data across a placement boundary.
//
// The invariant is enforced INSIDE the insert statement (data-integrity.md ban #10 —
// never read-then-write): the set of zones constituting the image's region is
// resolved from the owner of Geography and the live source row is matched against it
// in the same statement that inserts.
//
// The refusal wording is the same hide-existence text a real miss produces, so it is
// not an oracle for volumes in other regions.

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// fixtureRegionZones is what the owner of Geography answers when asked which zones
// constitute the region a fixture image is created in. It is data from geo, never
// derived from a zone's name — data-integrity.md forbids obtaining a region by
// parsing a zone string. Every fixture volume in this package lives in one of these
// zones, so passing it keeps the existing fixtures placement-coherent.
var fixtureRegionZones = []string{"region-1-a", "region-1-b"}

// mkVolumeRowInZone inserts a volume directly, in the given zone.
func mkVolumeRowInZone(t *testing.T, pool *pgxpool.Pool, project, name, zone string) string {
	t.Helper()
	id := ids.NewID(domain.PrefixVolume)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO volumes (id, project_id, name, zone_id, disk_type_id, size_bytes, state)
		 VALUES ($1,$2,$3,$4,$5,$6,'READY')`,
		id, project, name, zone, seededDiskType, int64(20)<<30)
	require.NoError(t, err)
	return id
}

// mkSnapshotOfVolume inserts a snapshot carrying its lineage to a volume. A storage
// snapshot has no zone column of its own — the volume it was taken from is its only
// placement evidence.
func mkSnapshotOfVolume(t *testing.T, pool *pgxpool.Pool, project, name, volumeID string) string {
	t.Helper()
	id := ids.NewID(domain.PrefixSnapshot)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO snapshots (id, project_id, name, source_volume_id, size_bytes, state)
		 VALUES ($1,$2,$3,$4,$5,'READY')`,
		id, project, name, volumeID, int64(20)<<30)
	require.NoError(t, err)
	return id
}

// TestImageSourceVolumeForeignRegionRejected — the defect itself: a volume outside
// the image's region must not become that image's source.
func TestImageSourceVolumeForeignRegionRejected(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	volID := mkVolumeRowInZone(t, pool, "prj-1", "vol-foreign-region", "region-2-a")

	_, err := ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "img-from-foreign-vol",
		RegionID: "region-1", SourceVolume: volID,
	}, fixtureRegionZones)
	require.Error(t, err, "an image must not be captured from a volume in another region")
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Volume "+volID+" not found", err.Error()[len("failed precondition: "):],
		"the refusal must be byte-identical to a real miss, or it is an oracle for volumes of other regions")
}

// TestImageSourceVolumeSameRegionSeeded — the positive path stays open.
func TestImageSourceVolumeSameRegionSeeded(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	volID := mkVolumeRowInZone(t, pool, "prj-1", "vol-same-region", "region-1-b")

	img, err := ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "img-from-same-vol",
		RegionID: "region-1", SourceVolume: volID,
	}, fixtureRegionZones)
	require.NoError(t, err)
	require.Equal(t, volID, img.SourceVolume)
}

// TestImageSourceSnapshotFollowsLineageRegion — the same boundary one hop away. A
// snapshot carries no zone of its own, so its placement is the volume it was taken
// from; capturing an image from such a snapshot must respect that volume's region,
// otherwise the check above is bypassed by taking a snapshot first.
func TestImageSourceSnapshotFollowsLineageRegion(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	volID := mkVolumeRowInZone(t, pool, "prj-1", "vol-lineage-foreign", "region-2-a")
	snapID := mkSnapshotOfVolume(t, pool, "prj-1", "snap-lineage-foreign", volID)

	_, err := ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "img-from-foreign-snap",
		RegionID: "region-1", SourceSnapshot: snapID,
	}, fixtureRegionZones)
	require.Error(t, err, "a snapshot whose volume is in another region must not seed the image")
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Snapshot "+snapID+" not found", err.Error()[len("failed precondition: "):])
}

// TestImageSourceSnapshotSameRegionSeeded — lineage inside the region is accepted.
func TestImageSourceSnapshotSameRegionSeeded(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	volID := mkVolumeRowInZone(t, pool, "prj-1", "vol-lineage-same", "region-1-a")
	snapID := mkSnapshotOfVolume(t, pool, "prj-1", "snap-lineage-same", volID)

	img, err := ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "img-from-same-snap",
		RegionID: "region-1", SourceSnapshot: snapID,
	}, fixtureRegionZones)
	require.NoError(t, err)
	require.Equal(t, snapID, img.SourceSnapshot)
}

// TestImageSourceSnapshotWithoutLineageUnaffected — a snapshot whose source volume is
// gone (the FK is ON DELETE SET NULL) carries no placement at all. There is nothing
// to compare, and inventing one would reject a legitimate capture. This documents the
// boundary rather than hiding it.
func TestImageSourceSnapshotWithoutLineageUnaffected(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-no-lineage", 20<<30)

	img, err := ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "img-from-orphan-snap",
		RegionID: "region-1", SourceSnapshot: snapID,
	}, fixtureRegionZones)
	require.NoError(t, err, "a placement-less snapshot has no region to contradict")
	require.Equal(t, snapID, img.SourceSnapshot)
}
