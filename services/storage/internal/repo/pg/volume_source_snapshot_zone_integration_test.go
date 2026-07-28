// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// A Volume is ZONAL. A Snapshot has no zone column of its own — the volume it was
// taken from is its only placement evidence, and the image capture path already
// reads it exactly that way. Seeding a Volume FROM a Snapshot, however, compared
// projects only: a snapshot of a zone-B volume could be restored into zone A, which
// silently moves block data across a placement boundary. Any link between two
// placeable resources has to be placement-coherent, and for two zonal ones that
// means the same zone.
//
// The comparison lives INSIDE the insert statement (data-integrity.md ban #10 —
// never read-then-write), so there is no window between "checked the zone" and
// "inserted the row".
//
// Wording follows the neighbouring image lane of the same repo: a snapshot of the
// caller's OWN project is already visible to them through Snapshot.Get, so the
// refusal is spoken aloud instead of being dressed up as a miss — a "not found" for
// a resource the caller can read would be a lie. A snapshot of ANOTHER project stays
// byte-identical to a genuine miss, or it becomes an existence oracle.

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// volumeRows counts the rows actually committed for a volume id — the observable
// that separates "refused" from "returned an error but wrote anyway".
func volumeRows(t *testing.T, pool *pgxpool.Pool, id string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM volumes WHERE id = $1`, id).Scan(&n))
	return n
}

// TestVolumeFromSnapshotForeignZoneRejected — the defect itself: a snapshot whose
// lineage volume lives in another zone must not seed a volume in this one.
func TestVolumeFromSnapshotForeignZoneRejected(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	srcVolID := mkVolumeRowInZone(t, pool, "prj-1", "vol-snap-src-zone-b", "region-1-b")
	snapID := mkSnapshotOfVolume(t, pool, "prj-1", "snap-from-zone-b", srcVolID)

	newID := ids.NewID(domain.PrefixVolume)
	_, err := vr.Insert(ctx, &domain.Volume{
		ID: newID, ProjectID: "prj-1", Name: "vol-restored-into-zone-a",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 20 << 30,
		SourceSnapshot: snapID,
	}, "region-1")
	require.Error(t, err, "a snapshot taken in another zone must not seed a volume here")
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "failed precondition: Volume and Snapshot must be in the same zone", err.Error(),
		"an own-project snapshot is already visible to the caller — the refusal names the reason")
	require.Equal(t, 0, volumeRows(t, pool, newID), "the refused volume must not exist")
}

// TestVolumeFromSnapshotSameZoneSeeded — the positive path stays open.
func TestVolumeFromSnapshotSameZoneSeeded(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	srcVolID := mkVolumeRowInZone(t, pool, "prj-1", "vol-snap-src-same-zone", "region-1-a")
	snapID := mkSnapshotOfVolume(t, pool, "prj-1", "snap-from-same-zone", srcVolID)

	newID := ids.NewID(domain.PrefixVolume)
	got, err := vr.Insert(ctx, &domain.Volume{
		ID: newID, ProjectID: "prj-1", Name: "vol-restored-same-zone",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 20 << 30,
		SourceSnapshot: snapID,
	}, "region-1")
	require.NoError(t, err)
	require.Equal(t, snapID, got.SourceSnapshot)
	require.Equal(t, 1, volumeRows(t, pool, newID))
}

// TestVolumeFromSnapshotWithoutLineageUnaffected — a snapshot whose source volume is
// gone (the FK is ON DELETE SET NULL) carries no placement at all. There is nothing
// to compare, and inventing a zone for it would refuse a legitimate restore. Same
// boundary the image capture path documents.
func TestVolumeFromSnapshotWithoutLineageUnaffected(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-orphan-for-volume", 20<<30)

	newID := ids.NewID(domain.PrefixVolume)
	got, err := vr.Insert(ctx, &domain.Volume{
		ID: newID, ProjectID: "prj-1", Name: "vol-from-orphan-snap",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 20 << 30,
		SourceSnapshot: snapID,
	}, "region-1")
	require.NoError(t, err, "a placement-less snapshot has no zone to contradict")
	require.Equal(t, snapID, got.SourceSnapshot)
	require.Equal(t, 1, volumeRows(t, pool, newID))
}

// TestVolumeFromSnapshotForeignProjectStaysHidden — the zone lane must not become an
// oracle: a snapshot of ANOTHER project keeps answering byte-identically to a
// snapshot that does not exist, whatever zone it was taken in.
func TestVolumeFromSnapshotForeignProjectStaysHidden(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	victimVolID := mkVolumeRowInZone(t, pool, "prj-victim-zone", "vol-victim-zone-b", "region-1-b")
	foreignSnap := mkSnapshotOfVolume(t, pool, "prj-victim-zone", "snap-victim-zone-b", victimVolID)

	_, foreignErr := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-attacker-zone", Name: "vol-steal-foreign-snap",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 20 << 30,
		SourceSnapshot: foreignSnap,
	}, "region-1")

	absentSnap := ids.NewID(domain.PrefixSnapshot)
	_, missErr := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-attacker-zone", Name: "vol-absent-snap",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 20 << 30,
		SourceSnapshot: absentSnap,
	}, "region-1")

	requireHideExistence(t, foreignErr, foreignSnap, missErr, absentSnap)
}
