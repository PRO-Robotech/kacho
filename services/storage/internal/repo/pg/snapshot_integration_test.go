// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/snapshot"
)

// mkSnapshot создаёт Snapshot из тома через репо (state=READY, size из тома) и
// возвращает его.
func mkSnapshot(t *testing.T, r *pg.SnapshotRepo, project, name, srcVolume string) *domain.Snapshot {
	t.Helper()
	s, err := r.Insert(context.Background(), &domain.Snapshot{
		ID:             ids.NewID(domain.PrefixSnapshot),
		ProjectID:      project,
		Name:           name,
		SourceVolumeID: srcVolume,
	})
	require.NoError(t, err)
	return s
}

// TestSnapshotCreateFromReadyVolume — Insert из READY-тома: state READY сразу,
// size_bytes = volumes.size_bytes на момент, status derived READY; Get совпадает
// (Snapshot Create happy, §1; task-summary Snapshot Create).
func TestSnapshotCreateFromReadyVolume(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ctx := context.Background()

	vol := mkVolume(t, vr, "prj-1", "vol-src", 7<<30)
	snap := mkSnapshot(t, sr, "prj-1", "snap-a", vol.ID)
	require.Equal(t, domain.PrefixSnapshot, snap.ID[:3])
	require.Equal(t, vol.ID, snap.SourceVolumeID)
	require.EqualValues(t, 7<<30, snap.SizeBytes, "size_bytes snapshotted from source volume")
	require.Equal(t, domain.SnapshotStatusReady, snap.Status, "state READY immediately (control-plane)")

	got, err := sr.Get(ctx, snap.ID)
	require.NoError(t, err)
	require.Equal(t, "snap-a", got.Name)
	require.EqualValues(t, 7<<30, got.SizeBytes)
	require.Equal(t, domain.SnapshotStatusReady, got.Status)
	require.False(t, got.CreatedAt.IsZero(), "created_at populated")
}

// TestSnapshotCreateSourceMissing — source volume не существует → FailedPrecondition
// "Volume <id> not found" (existence same-DB; Operation error).
func TestSnapshotCreateSourceMissing(t *testing.T) {
	sr := pg.NewSnapshotRepo(newTestPool(t))
	_, err := sr.Insert(context.Background(), &domain.Snapshot{
		ID: ids.NewID(domain.PrefixSnapshot), ProjectID: "prj-1", Name: "snap-x",
		SourceVolumeID: "vol00000000000000000",
	})
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Volume vol00000000000000000 not found", err.Error()[len("failed precondition: "):])
}

// TestSnapshotCreateSourceNotReady — source volume существует, но state != READY →
// FailedPrecondition "Volume <id> is not ready" (CAS WHERE state='READY' не сматчил).
func TestSnapshotCreateSourceNotReady(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ctx := context.Background()

	vol := mkVolume(t, vr, "prj-1", "vol-creating", 1<<30)
	_, err := pool.Exec(ctx, `UPDATE volumes SET state='CREATING' WHERE id=$1`, vol.ID)
	require.NoError(t, err)

	_, err = sr.Insert(ctx, &domain.Snapshot{
		ID: ids.NewID(domain.PrefixSnapshot), ProjectID: "prj-1", Name: "snap-y", SourceVolumeID: vol.ID,
	})
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, fmt.Sprintf("Volume %s is not ready", vol.ID), err.Error()[len("failed precondition: "):])
}

// TestSnapshotGetNotFound — well-formed-но-нет → ErrNotFound "Snapshot <id> not found".
func TestSnapshotGetNotFound(t *testing.T) {
	sr := pg.NewSnapshotRepo(newTestPool(t))
	_, err := sr.Get(context.Background(), "snp00000000000000000")
	require.True(t, stderrors.Is(err, ports.ErrNotFound), "got %v", err)
	require.Equal(t, "Snapshot snp00000000000000000 not found", err.Error()[len("not found: "):])
}

// TestSnapshotNameUniqueRace — конкурентный Insert (project,name) → ровно один OK,
// остальные AlreadyExists (partial UNIQUE 23505). Под -race.
func TestSnapshotNameUniqueRace(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	vol := mkVolume(t, vr, "prj-1", "vol-forsnap", 2<<30)

	const n = 6
	var ok, dup atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := sr.Insert(context.Background(), &domain.Snapshot{
				ID: ids.NewID(domain.PrefixSnapshot), ProjectID: "prj-1", Name: "dup-snap", SourceVolumeID: vol.ID,
			})
			switch {
			case err == nil:
				ok.Add(1)
			case stderrors.Is(err, ports.ErrAlreadyExists):
				dup.Add(1)
			default:
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, 1, ok.Load(), "exactly one snapshot insert wins")
	require.EqualValues(t, n-1, dup.Load())
}

// TestSnapshotUpdateMutable — Update name/description применяется; 0-row → NotFound.
func TestSnapshotUpdateMutable(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ctx := context.Background()
	vol := mkVolume(t, vr, "prj-1", "vol-upd", 1<<30)
	snap := mkSnapshot(t, sr, "prj-1", "snap-old", vol.ID)

	name, desc := "snap-new", "patched-desc"
	up, err := sr.Update(ctx, snap.ID, snapshot.SnapshotUpdate{Name: &name, Description: &desc})
	require.NoError(t, err)
	require.Equal(t, "snap-new", up.Name)
	require.Equal(t, "patched-desc", up.Description)

	_, err = sr.Update(ctx, "snp00000000000000000", snapshot.SnapshotUpdate{Name: &name})
	require.True(t, stderrors.Is(err, ports.ErrNotFound), "update missing → NotFound, got %v", err)
}

// TestSnapshotDeleteFKSetNull — S1-09 обе стороны SET NULL:
//   - Delete снапшота, на который ссылается том (source_snapshot_id) → OK; том цел,
//     source_snapshot_id → пусто.
//   - Delete тома-источника (source_volume_id) → OK; снапшот цел, source_volume_id → пусто.
func TestSnapshotDeleteFKSetNull(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ctx := context.Background()

	// snp-1 создан из vol-src; vol-2 создан из snp-1.
	src := mkVolume(t, vr, "prj-1", "vol-src", 3<<30)
	snap := mkSnapshot(t, sr, "prj-1", "snp-shared", src.ID)
	fromSnap, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-2",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 3 << 30, SourceSnapshot: snap.ID,
	})
	require.NoError(t, err)
	require.Equal(t, snap.ID, fromSnap.SourceSnapshot)

	// Delete снапшота (vol-2 ссылается) → не блокируется (SET NULL); vol-2 цел, ref пусто.
	require.NoError(t, sr.Delete(ctx, snap.ID))
	gotVol, err := vr.Get(ctx, fromSnap.ID)
	require.NoError(t, err)
	require.Empty(t, gotVol.SourceSnapshot, "volumes.source_snapshot_id → SET NULL on snapshot delete")

	// Другая сторона: снапшот src-snap ссылается на vol-src; удаляем vol-src.
	src2 := mkVolume(t, vr, "prj-1", "vol-src2", 4<<30)
	snap2 := mkSnapshot(t, sr, "prj-1", "snp-fromsrc", src2.ID)
	require.NoError(t, vr.Delete(ctx, src2.ID))
	gotSnap, err := sr.Get(ctx, snap2.ID)
	require.NoError(t, err, "snapshot survives source volume delete")
	require.Empty(t, gotSnap.SourceVolumeID, "snapshots.source_volume_id → SET NULL on volume delete")
}

// TestSnapshotListCursorFilter — cursor (created_at,id) ASC, project-scope, filter=name.
func TestSnapshotListCursorFilter(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ctx := context.Background()
	vol := mkVolume(t, vr, "prj-1", "vol-list", 1<<30)
	volOther := mkVolume(t, vr, "prj-2", "vol-list-other", 1<<30)
	for _, n := range []string{"snap-a", "snap-b", "snap-c"} {
		mkSnapshot(t, sr, "prj-1", n, vol.ID)
	}
	mkSnapshot(t, sr, "prj-2", "snap-other", volOther.ID)

	page1, next, err := sr.List(ctx, snapshot.Pagination{PageSize: 2, ProjectID: "prj-1"})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, next)

	page2, _, err := sr.List(ctx, snapshot.Pagination{PageSize: 2, ProjectID: "prj-1", PageToken: next})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	for _, s := range append(append([]*domain.Snapshot{}, page1...), page2...) {
		require.Equal(t, "prj-1", s.ProjectID, "project-scope excludes prj-2")
	}

	filtered, _, err := sr.List(ctx, snapshot.Pagination{PageSize: 50, ProjectID: "prj-1", Filter: "snap-b"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "snap-b", filtered[0].Name)
}
