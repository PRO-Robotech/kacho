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
//
// One predicate, but TWO refusals that must not share an answer:
//
//   - the image belongs to ANOTHER project — the caller cannot see it at all, so
//     the refusal stays byte-identical to a genuine miss (security.md §6);
//   - the image is the caller's OWN and its region does not contain the volume's
//     zone — nothing is hidden (Image.Get already returns it to this caller), so
//     hiding turns the answer into a false statement about a resource the caller
//     is looking straight at. Placement coherence names it out loud
//     (data-integrity.md §Placement-coherence).

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

// bootFromImage seeds a boot volume from an image in the given project/zone and
// reports the volume id it tried to use together with the refusal (if any).
func bootFromImage(ctx context.Context, vr *pg.VolumeRepo, project, name, zone, zoneRegion, imageID string) (string, error) {
	id := ids.NewID(domain.PrefixVolume)
	_, err := vr.Insert(ctx, &domain.Volume{
		ID: id, ProjectID: project, Name: name,
		ZoneID: zone, DiskTypeID: seededDiskType, SizeBytes: 21474836480,
		SourceImage: imageID,
	}, zoneRegion)
	return id, err
}

// The caller's OWN image, in a region that does not contain the volume's zone, is
// refused BY NAME. Before the split this answered "Image <id> not found" — the
// wording reserved for a resource the caller may not know exists — about an image
// that same caller can Get. Nothing was hidden and the diagnosis was false.
func TestVolumeSourceImageOwnProjectForeignRegionNamed(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-foreign-region", 20<<30)
	img := mkImageFromSnapshot(t, ir, "prj-1", "img-region-1", "region-1", snapID)

	volID, err := bootFromImage(ctx, vr, "prj-1", "boot-foreign-region",
		"region-2-a", "region-2", // the zone's region, resolved from the owner of Geography
		img.ID)
	require.Error(t, err, "a boot volume outside the image's region must not be seeded")
	require.Equal(t, "Volume and Image must be in the same region", fpText(t, err))

	// A refusal is a refusal: nothing half-materialised.
	_, gerr := vr.Get(ctx, volID)
	require.True(t, stderrors.Is(gerr, storageerr.ErrNotFound), "no volume row materialised, got %v", gerr)

	// And the image the caller was just told about is still readable by them —
	// which is precisely why hiding it would have been a lie.
	got, ierr := ir.Get(ctx, img.ID)
	require.NoError(t, ierr)
	require.Equal(t, "region-1", got.RegionID)
}

// Splitting the lanes must not open an existence oracle. An image of ANOTHER
// project keeps the hide-existence wording — byte-identical to a genuine miss —
// whether or not its region would have matched. Had the region lane been consulted
// first, the second sub-case below would answer "must be in the same region" and
// thereby confirm that a foreign image exists.
func TestVolumeSourceImageForeignProjectStaysHidden(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	victimSnap := mkSnapshotRow(t, pool, projVictim, "snap-hidden-region", 20<<30)
	// Two victim images: one whose region matches the attacker's zone region, one
	// whose region does not. Both must answer identically.
	sameRegion := mkImageFromSnapshot(t, ir, projVictim, "victim-img-same-region", "region-1", victimSnap)
	otherRegion := mkImageFromSnapshot(t, ir, projVictim, "victim-img-other-region", "region-2", victimSnap)

	missID := ids.NewID(domain.PrefixImage)
	_, missErr := bootFromImage(ctx, vr, projAttacker, "boot-miss", "region-1-a", "region-1", missID)

	for _, tc := range []struct {
		name    string
		imageID string
	}{
		{"region-would-match", sameRegion.ID},
		{"region-would-not-match", otherRegion.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			volID, err := bootFromImage(ctx, vr, projAttacker, "boot-hidden-"+tc.name,
				"region-1-a", "region-1", tc.imageID)
			require.NotContains(t, fpText(t, err), "same region",
				"a foreign image must never be named — naming it confirms it exists")
			requireHideExistence(t, err, tc.imageID, missErr, missID)

			_, gerr := vr.Get(ctx, volID)
			require.True(t, stderrors.Is(gerr, storageerr.ErrNotFound), "no volume row materialised, got %v", gerr)
		})
	}
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

// An image the caller owns, whose zone-region the service failed to resolve, is
// NOT a region mismatch — there is nothing to compare it against, and calling it
// one would be a second false diagnosis. The use-case fail-closes long before this
// (a source image forces a geo resolve), so this pins the port contract.
func TestVolumeSourceImageUnresolvedRegionStaysFailClosed(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-unresolved-region", 20<<30)
	img := mkImageFromSnapshot(t, ir, "prj-1", "img-unresolved-region", "region-1", snapID)

	_, err := bootFromImage(ctx, vr, "prj-1", "boot-unresolved-region", "region-1-a", "", img.ID)
	require.Equal(t, "Image "+img.ID+" not found", fpText(t, err),
		"an unresolved region is not a mismatch — it stays fail-closed")
}
