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

	"github.com/PRO-Robotech/kacho/pkg/filter"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// mkSnapshot создаёт Snapshot из тома через репо (size из тома, состояние —
// СОЗДАВАЕМЫЙ: готовность объявляет сверщик) и возвращает его. Имя объекта у бэкенда
// выводится так же, как это делает use-case, — из неизменяемого идентификатора
// снимка: без него частичная уникальность имени объекта не проверяется ничем.
func mkSnapshot(t *testing.T, r *pg.SnapshotRepo, project, name, srcVolume string) *domain.Snapshot {
	t.Helper()
	return snapInsert(t, r, project, name, srcVolume)
}

// TestSnapshotCreateFromReadyVolume — Insert из READY-тома: size_bytes =
// volumes.size_bytes на момент, состояние — СОЗДАВАЕМЫЙ; Get совпадает.
//
// Готовым снимок объявляет сверщик, увидев объект у бэкенда: операция фиксирует
// намерение, а исход провижининга несёт статус ресурса. Прежде вставка объявляла
// снимок готовым сама — то есть утверждала о плоскости данных то, чего никто не
// проверял.
func TestSnapshotCreateFromReadyVolume(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ctx := context.Background()
	snapSeedPlacement(t, pool, snapClass, snapZoneA)

	vol := snapReadyVolume(t, pool, vr, "prj-1", "vol-src", snapZoneA, 7<<30)
	snap := mkSnapshot(t, sr, "prj-1", "snap-a", vol.ID)
	require.Equal(t, domain.PrefixSnapshot, snap.ID[:3])
	require.Equal(t, vol.ID, snap.SourceVolumeID)
	require.EqualValues(t, 7<<30, snap.SizeBytes, "size_bytes snapshotted from source volume")
	require.Equal(t, domain.SnapshotStatusCreating, snap.Status, "готовность объявляет сверщик")

	got, err := sr.Get(ctx, snap.ID)
	require.NoError(t, err)
	require.Equal(t, "snap-a", got.Name)
	require.EqualValues(t, 7<<30, got.SizeBytes)
	require.Equal(t, domain.SnapshotStatusCreating, got.Status)
	require.False(t, got.CreatedAt.IsZero(), "created_at populated")

	// Положительный контроль: тот же снимок, объявленный готовым, читается готовым —
	// «создаваемый» выше про рождение, а не про то, что чтение состояния сломано.
	snapMarkSnapshotReady(t, pool, snap.ID)
	ready, err := sr.Get(ctx, snap.ID)
	require.NoError(t, err)
	require.Equal(t, domain.SnapshotStatusReady, ready.Status)
}

// TestSnapshotCreateSourceMissing — source volume не существует → FailedPrecondition
// "Volume <id> not found" (existence same-DB; Operation error).
func TestSnapshotCreateSourceMissing(t *testing.T) {
	sr := pg.NewSnapshotRepo(newTestPool(t))
	_, _, err := sr.Insert(context.Background(), &domain.Snapshot{
		ID: ids.NewID(domain.PrefixSnapshot), ProjectID: "prj-1", Name: "snap-x",
		SourceVolumeID: "vol00000000000000000",
	})
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Volume vol00000000000000000 not found", err.Error()[len("failed precondition: "):])
}

// TestSnapshotCreateSourceNotReady — source volume существует, но state != READY →
// FailedPrecondition "Volume <id> is not ready" (CAS WHERE state='READY' не сматчил).
func TestSnapshotCreateSourceNotReady(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ctx := context.Background()

	snapSeedPlacement(t, pool, snapClass, snapZoneA)
	// Том рождается создаваемым — готовность объявляет сверщик; здесь она нарочно
	// не объявляется.
	vol := snapVolume(t, vr, "prj-1", "vol-creating", snapZoneA, 1<<30)

	_, _, err := sr.Insert(ctx, &domain.Snapshot{
		ID: ids.NewID(domain.PrefixSnapshot), ProjectID: "prj-1", Name: "snap-y", SourceVolumeID: vol.ID,
	})
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, fmt.Sprintf("Volume %s is not ready", vol.ID), err.Error()[len("failed precondition: "):])
}

// TestSnapshotGetNotFound — well-formed-но-нет → ErrNotFound "Snapshot <id> not found".
func TestSnapshotGetNotFound(t *testing.T) {
	sr := pg.NewSnapshotRepo(newTestPool(t))
	_, err := sr.Get(context.Background(), "snp00000000000000000")
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound), "got %v", err)
	require.Equal(t, "Snapshot snp00000000000000000 not found", err.Error()[len("not found: "):])
}

// TestSnapshotNameUniqueRace — конкурентный Insert (project,name) → ровно один OK,
// остальные AlreadyExists (partial UNIQUE 23505). Под -race.
func TestSnapshotNameUniqueRace(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	snapSeedPlacement(t, pool, snapClass, snapZoneA)
	vol := snapReadyVolume(t, pool, vr, "prj-1", "vol-forsnap", snapZoneA, 2<<30)

	const n = 6
	var ok, dup atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := sr.Insert(context.Background(), &domain.Snapshot{
				ID: ids.NewID(domain.PrefixSnapshot), ProjectID: "prj-1", Name: "dup-snap", SourceVolumeID: vol.ID,
			})
			switch {
			case err == nil:
				ok.Add(1)
			case stderrors.Is(err, storageerr.ErrAlreadyExists):
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
//
// Отдельно проверяется время правки: колонка updated_at в схеме была, Update её
// двигал, а чтение её не брало — поэтому объявленное контрактом поле оставалось
// пустым ВСЕГДА, и «снимок не менялся» было неотличимо от «мы не смотрим».
func TestSnapshotUpdateMutable(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ctx := context.Background()
	snapSeedPlacement(t, pool, snapClass, snapZoneA)
	vol := snapReadyVolume(t, pool, vr, "prj-1", "vol-upd", snapZoneA, 1<<30)
	snap := mkSnapshot(t, sr, "prj-1", "snap-old", vol.ID)

	require.False(t, snap.UpdatedAt.IsZero(), "время правки заполнено с рождения")

	name, desc := "snap-new", "patched-desc"
	up, _, err := sr.Update(ctx, snap.ID, snapshot.SnapshotUpdate{Name: &name, Description: &desc})
	require.NoError(t, err)
	require.Equal(t, "snap-new", up.Name)
	require.Equal(t, "patched-desc", up.Description)
	require.True(t, up.UpdatedAt.After(up.CreatedAt), "правка двигает время правки")

	_, _, err = sr.Update(ctx, "snp00000000000000000", snapshot.SnapshotUpdate{Name: &name})
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound), "update missing → NotFound, got %v", err)
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
	snapSeedPlacement(t, pool, snapClass, snapZoneA)
	src := snapReadyVolume(t, pool, vr, "prj-1", "vol-src", snapZoneA, 3<<30)
	snap := mkSnapshot(t, sr, "prj-1", "snp-shared", src.ID)
	snapMarkSnapshotReady(t, pool, snap.ID)
	fromSnap, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-2",
		ZoneID: snapZoneA, DiskTypeID: snapClass, SizeBytes: 3 << 30, SourceSnapshot: snap.ID,
	}, "")
	require.NoError(t, err)
	require.Equal(t, snap.ID, fromSnap.SourceSnapshot)

	// Delete снапшота (vol-2 ссылается) → не блокируется (SET NULL); vol-2 цел, ref пусто.
	require.NoError(t, sr.Delete(ctx, snap.ID))
	gotVol, err := vr.Get(ctx, fromSnap.ID)
	require.NoError(t, err)
	require.Empty(t, gotVol.SourceSnapshot, "volumes.source_snapshot_id → SET NULL on snapshot delete")

	// Другая сторона: снапшот src-snap ссылается на vol-src; удаляем vol-src.
	src2 := snapReadyVolume(t, pool, vr, "prj-1", "vol-src2", snapZoneA, 4<<30)
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
	snapSeedPlacement(t, pool, snapClass, snapZoneA)
	vol := snapReadyVolume(t, pool, vr, "prj-1", "vol-list", snapZoneA, 1<<30)
	volOther := snapReadyVolume(t, pool, vr, "prj-2", "vol-list-other", snapZoneA, 1<<30)
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

	// Репозиторий получает РАЗОБРАННЫЙ узел, а не голое значение: оператор — часть
	// выражения (#460). Узел строится тем же Parse, что зовёт use-case, — фикстура
	// не вправе быть снисходительнее продукта.
	nameEq, perr := filter.Parse(`name="snap-b"`, []string{"name"})
	require.NoError(t, perr)
	filtered, _, err := sr.List(ctx, snapshot.Pagination{PageSize: 50, ProjectID: "prj-1", FilterAST: nameEq})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "snap-b", filtered[0].Name)

	// CONTAINS — подстрока. Равенство выше сузило до ОДНОЙ строки, поэтому три
	// строки здесь пришли от оператора, а не от предиката, которого нет.
	nameSub, perr := filter.Parse(`name CONTAINS "snap-"`, []string{"name"})
	require.NoError(t, perr)
	subset, _, err := sr.List(ctx, snapshot.Pagination{PageSize: 50, ProjectID: "prj-1", FilterAST: nameSub})
	require.NoError(t, err)
	require.Len(t, subset, 3, "подстрока обязана вернуть все три снимка проекта")
	for _, s := range subset {
		// snap-other из prj-2 подстроке ТОЖЕ отвечает — и не приходит.
		require.Equal(t, "prj-1", s.ProjectID)
	}

	// Совпадение ВНУТРИ имени, а не префикс.
	nameMid, perr := filter.Parse(`name CONTAINS "p-b"`, []string{"name"})
	require.NoError(t, perr)
	mid, _, err := sr.List(ctx, snapshot.Pagination{PageSize: 50, ProjectID: "prj-1", FilterAST: nameMid})
	require.NoError(t, err)
	require.Len(t, mid, 1)
	require.Equal(t, "snap-b", mid[0].Name)
}
