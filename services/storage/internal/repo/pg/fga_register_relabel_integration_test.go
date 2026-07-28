// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// fga_register_relabel_integration_test.go — a label change must refresh the
// mirror the label-selector reads.
//
// Access granted through a label selector is revoked by REMOVING the label. That
// only works if the authority holding the selector sees the new labels, and it
// sees them through the mirror row the owner feeds it. Storage emitted a register
// intent on Create and an unregister intent on Delete, and nothing in between —
// so the mirror froze at creation time, the selector kept matching the labels the
// resource no longer carries, and the grant outlived the label it was granted
// through. Observed black-box: after clearing the labels the resource comes back
// with none, and the access check still answers allowed, minutes past the poll
// budget. The same case on kacho-compute passes on the same stand under the same
// identity — the difference is the resource family, not the environment.
//
// kacho-vpc already does this (network update re-emits when labels are in the
// mask, with the same reasoning written next to it), so these three are the ones
// that were missed rather than a new contract.
//
// Full label removal is an UPSERT WITH EMPTY LABELS, never an unregister: the
// resource still exists and keeps its owner tuple; it simply stops matching a
// label selector. Unregistering here would revoke the owner's own access.

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// registerRowsFor filters the outbox down to register intents naming one resource.
func registerRowsFor(t *testing.T, rows []fgaOutboxRow, kind, id string) []fgaOutboxRow {
	t.Helper()
	var out []fgaOutboxRow
	for _, r := range rows {
		if r.eventType == "fga.register" && r.resourceKind == kind && r.resourceID == id {
			out = append(out, r)
		}
	}
	return out
}

// A label change on a Volume must re-emit the register intent carrying the NEW
// labels, in the same writer transaction as the row update.
func TestVolumeUpdate_LabelChange_ReEmitsRegisterIntentWithNewLabels(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)

	v := mkVolume(t, r, "prj-relabel-v", "vol-relabel", 10<<30)

	newLabels := map[string]string{"team": "storage"}
	_, err := r.Update(t.Context(), v.ID, volume.VolumeUpdate{LabelsSet: true, Labels: newLabels})
	require.NoError(t, err)

	rows := registerRowsFor(t, selectFGARows(t, pool), "storage_volume", v.ID)
	require.Len(t, rows, 2,
		"one register intent from Create and one from the label change; without the second "+
			"the mirror keeps the labels the volume no longer carries and the grant outlives them")

	p, err := fgaregister.Decode(rows[1].payload)
	require.NoError(t, err)
	require.Equal(t, newLabels, p.Labels, "the re-emitted intent must carry the labels as they are NOW")
	require.Equal(t, "project:prj-relabel-v", p.SubjectID, "the owner tuple is unchanged by a relabel")
	require.Equal(t, "storage_volume:"+v.ID, p.Object)
	require.True(t, rows[1].sentAtNull, "a fresh intent is not applied yet")
}

// Clearing every label is the revoke path, and it is an upsert with an empty map
// — NOT an unregister. Unregistering would take the owner's own access away along
// with the selector match.
func TestVolumeUpdate_LabelsCleared_UpsertsEmptyNotUnregister(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)

	v := mkVolume(t, r, "prj-relabel-c", "vol-clear", 10<<30)
	_, err := r.Update(t.Context(), v.ID, volume.VolumeUpdate{
		LabelsSet: true, Labels: map[string]string{"team": "storage"},
	})
	require.NoError(t, err)

	_, err = r.Update(t.Context(), v.ID, volume.VolumeUpdate{LabelsSet: true, Labels: map[string]string{}})
	require.NoError(t, err)

	all := selectFGARows(t, pool)
	for _, row := range all {
		require.NotEqual(t, "fga.unregister", row.eventType,
			"clearing labels must not unregister the volume — it still exists and keeps its owner tuple")
	}

	rows := registerRowsFor(t, all, "storage_volume", v.ID)
	require.Len(t, rows, 3, "Create + two label changes")

	p, err := fgaregister.Decode(rows[2].payload)
	require.NoError(t, err)
	require.Empty(t, p.Labels, "the mirror must be told the labels are gone, not left at the old set")
}

// An update that does not touch labels must not emit anything: the mirror has
// nothing new to learn, and an intent per unrelated update is drain traffic the
// partition head has to clear before any real one.
func TestVolumeUpdate_WithoutLabels_EmitsNothing(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)

	v := mkVolume(t, r, "prj-relabel-n", "vol-noop", 10<<30)
	before := len(selectFGARows(t, pool))

	newName := "vol-noop-renamed"
	_, err := r.Update(t.Context(), v.ID, volume.VolumeUpdate{Name: &newName})
	require.NoError(t, err)

	require.Len(t, selectFGARows(t, pool), before,
		"a rename tells the label selector nothing; emitting here is pure drain traffic")
}

// The other two resources carry labels and are label-selectable in exactly the
// same way, so they were missed in exactly the same way.
func TestSnapshotUpdate_LabelChange_ReEmitsRegisterIntentWithNewLabels(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)

	v := mkVolume(t, vr, "prj-relabel-s", "vol-for-snap", 10<<30)
	s := mkSnapshot(t, sr, "prj-relabel-s", "snap-relabel", v.ID)

	newLabels := map[string]string{"tier": "cold"}
	_, err := sr.Update(t.Context(), s.ID, snapshot.SnapshotUpdate{LabelsSet: true, Labels: newLabels})
	require.NoError(t, err)

	rows := registerRowsFor(t, selectFGARows(t, pool), "storage_snapshot", s.ID)
	require.Len(t, rows, 2, "Create + the label change")

	p, err := fgaregister.Decode(rows[1].payload)
	require.NoError(t, err)
	require.Equal(t, newLabels, p.Labels)
	require.Equal(t, "storage_snapshot:"+s.ID, p.Object)
}

func TestImageUpdate_LabelChange_ReEmitsRegisterIntentWithNewLabels(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)

	snapID := mkSnapshotRow(t, pool, "prj-relabel-i", "snap-for-img", 10<<30)
	img := mkImageFromSnapshot(t, ir, "prj-relabel-i", "img-relabel", "reg-1", snapID)

	newLabels := map[string]string{"os": "linux"}
	_, err := ir.Update(t.Context(), img.ID, image.ImageUpdate{LabelsSet: true, Labels: newLabels})
	require.NoError(t, err)

	rows := registerRowsFor(t, selectFGARows(t, pool), "storage_image", img.ID)
	require.Len(t, rows, 2, "Create + the label change")

	p, err := fgaregister.Decode(rows[1].payload)
	require.NoError(t, err)
	require.Equal(t, newLabels, p.Labels)
	require.Equal(t, "storage_image:"+img.ID, p.Object)
}

// The CLEARING path is the revoke path, and it is the one that has to hold on
// every label-carrying resource — not just on the volume. Clearing is worth its
// own case per resource because "upsert an empty set" and "unregister" both make
// the selector stop matching, so a wrong implementation still looks like a
// working revoke from the outside; what separates them is that unregistering
// also drops the owner tuple and takes the OWNER's own access away with it. The
// volume has covered this since the emit-on-Update fix; snapshot and image had
// only the relabel case, so the one wrong implementation they could grow was the
// one no test would have caught.
func TestSnapshotUpdate_LabelsCleared_UpsertsEmptyNotUnregister(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)

	v := mkVolume(t, vr, "prj-relabel-sc", "vol-for-snap-clear", 10<<30)
	s := mkSnapshot(t, sr, "prj-relabel-sc", "snap-clear", v.ID)

	_, err := sr.Update(t.Context(), s.ID, snapshot.SnapshotUpdate{
		LabelsSet: true, Labels: map[string]string{"tier": "treska"},
	})
	require.NoError(t, err)

	_, err = sr.Update(t.Context(), s.ID, snapshot.SnapshotUpdate{LabelsSet: true, Labels: map[string]string{}})
	require.NoError(t, err)

	all := selectFGARows(t, pool)
	for _, row := range all {
		require.NotEqual(t, "fga.unregister", row.eventType,
			"clearing labels must not unregister the snapshot — it still exists and keeps its owner tuple")
	}

	rows := registerRowsFor(t, all, "storage_snapshot", s.ID)
	require.Len(t, rows, 3, "Create + two label changes")

	p, err := fgaregister.Decode(rows[2].payload)
	require.NoError(t, err)
	require.Empty(t, p.Labels, "the mirror must be told the labels are gone, not left at the old set")
}

func TestImageUpdate_LabelsCleared_UpsertsEmptyNotUnregister(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)

	snapID := mkSnapshotRow(t, pool, "prj-relabel-ic", "snap-for-img-clear", 10<<30)
	img := mkImageFromSnapshot(t, ir, "prj-relabel-ic", "img-clear", "reg-1", snapID)

	_, err := ir.Update(t.Context(), img.ID, image.ImageUpdate{
		LabelsSet: true, Labels: map[string]string{"tier": "treska"},
	})
	require.NoError(t, err)

	_, err = ir.Update(t.Context(), img.ID, image.ImageUpdate{LabelsSet: true, Labels: map[string]string{}})
	require.NoError(t, err)

	all := selectFGARows(t, pool)
	for _, row := range all {
		require.NotEqual(t, "fga.unregister", row.eventType,
			"clearing labels must not unregister the image — it still exists and keeps its owner tuple")
	}

	rows := registerRowsFor(t, all, "storage_image", img.ID)
	require.Len(t, rows, 3, "Create + two label changes")

	p, err := fgaregister.Decode(rows[2].payload)
	require.NoError(t, err)
	require.Empty(t, p.Labels, "the mirror must be told the labels are gone, not left at the old set")
}
