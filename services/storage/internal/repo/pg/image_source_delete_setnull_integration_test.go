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

// TestImageSourceSnapshotDeleteSetNull — STOR-1-28 / F5 parity (source-provenance
// SET NULL, снимок→образ): удаление Snapshot, засевшего Image (images.source_snapshot_id
// set), ПРОХОДИТ (FK ON DELETE SET NULL — provenance, НЕ live-dependency); Image
// выживает source-less (lineage-clear), а НЕ падает 23514 из-за CHECK exactly-one.
//
// RED до фикса: FK SET NULL зануляет source_snapshot_id → CHECK images_source_exactly_one
// (требует ровно один source non-null) нарушается на строке images → DELETE snapshots
// abort'ится 23514 → mapSnapshotErr → InvalidArgument "Illegal argument" (не то поведение).
// GREEN после фикса: CHECK ослаблен до mutual-exclusion (at-most-one) → source-less Image
// валиден → snapshot-delete чист, Image цел.
func TestImageSourceSnapshotDeleteSetNull(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-provenance", 20<<30)
	img := mkImageFromSnapshot(t, ir, "prj-1", "prov-from-snap", "ru-central1", snapID)
	require.Equal(t, snapID, img.SourceSnapshot, "image seeded from snapshot")

	// Delete снапшота, засевшего образ → должно ПРОЙТИ (provenance SET NULL, не RESTRICT):
	// блочные данные образа уже материализованы и независимы от источника (STOR-1-28).
	require.NoError(t, sr.Delete(ctx, snapID),
		"deleting a snapshot that seeded an image must succeed (SET NULL, not 23514 abort)")

	// Снапшот ушёл.
	_, serr := sr.Get(ctx, snapID)
	require.True(t, stderrors.Is(serr, ports.ErrNotFound), "snapshot hard-deleted, got %v", serr)

	// Image цел: source_snapshot_id очищен (lineage-clear), остальное неизменно, всё ещё READY.
	got, err := ir.Get(ctx, img.ID)
	require.NoError(t, err, "image must survive deletion of its source snapshot")
	require.Empty(t, got.SourceSnapshot, "source_snapshot_id SET NULL (provenance cleared)")
	require.Empty(t, got.SourceVolume, "no volume source appears")
	require.Equal(t, domain.ImageStatusReady, got.Status, "image state untouched by source delete")
	require.EqualValues(t, 20<<30, got.SizeBytes, "block data (size) untouched by source delete")
}

// TestImageSourceVolumeDeleteSetNull — симметрично для images.source_volume_id: удаление
// Volume, напрямую засевшего Image (source_volume_id set), ПРОХОДИТ (FK ON DELETE SET NULL);
// Image выживает source-less. RED до фикса — 23514 через CHECK exactly-one (mapVolumeErr →
// InvalidArgument). GREEN после — at-most-one CHECK допускает source-less Image.
func TestImageSourceVolumeDeleteSetNull(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	srcVol := mkVolume(t, vr, "prj-1", "golden-src-vol", 32<<30)
	img, err := ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "prov-from-vol",
		RegionID: "ru-central1", SourceVolume: srcVol.ID,
	})
	require.NoError(t, err)
	require.Equal(t, srcVol.ID, img.SourceVolume, "image seeded directly from volume")

	// Delete тома, засевшего образ → ПРОХОДИТ (provenance SET NULL, не 23514 abort).
	require.NoError(t, vr.Delete(ctx, srcVol.ID),
		"deleting a volume that seeded an image must succeed (SET NULL, not 23514 abort)")

	// Том ушёл.
	_, verr := vr.Get(ctx, srcVol.ID)
	require.True(t, stderrors.Is(verr, ports.ErrNotFound), "volume hard-deleted, got %v", verr)

	// Image цел: source_volume_id очищен, всё ещё READY.
	got, err := ir.Get(ctx, img.ID)
	require.NoError(t, err, "image must survive deletion of its source volume")
	require.Empty(t, got.SourceVolume, "source_volume_id SET NULL (provenance cleared)")
	require.Empty(t, got.SourceSnapshot, "no snapshot source appears")
	require.Equal(t, domain.ImageStatusReady, got.Status, "image state untouched by source delete")
	require.EqualValues(t, 32<<30, got.SizeBytes, "block data (size) untouched by source delete")
}
