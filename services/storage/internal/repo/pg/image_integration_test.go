// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/image"
)

// mkSnapshotRow вставляет строку snapshots напрямую (source для Image) с заданным
// размером и возвращает id.
func mkSnapshotRow(t *testing.T, pool *pgxpool.Pool, project, name string, size int64) string {
	t.Helper()
	id := ids.NewID(domain.PrefixSnapshot)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO snapshots (id, project_id, name, size_bytes, state) VALUES ($1,$2,$3,$4,'READY')`,
		id, project, name, size)
	require.NoError(t, err)
	return id
}

// mkImageFromSnapshot создаёт Image из снапшота через ImageRepo (state READY).
func mkImageFromSnapshot(t *testing.T, r *pg.ImageRepo, project, name, region, snapID string) *domain.Image {
	t.Helper()
	i, err := r.Insert(context.Background(), &domain.Image{
		ID:             ids.NewID(domain.PrefixImage),
		ProjectID:      project,
		Name:           name,
		RegionID:       region,
		SourceSnapshot: snapID,
	})
	require.NoError(t, err)
	return i
}

// TestImageCreateGetReady — STOR-1-20 (NET-NEW): Insert (READY, derived size/min_disk,
// format STANDARD, placement REGIONAL) → Get отдаёт те же поля.
func TestImageCreateGetReady(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewImageRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "golden-snap", 21474836480)
	img := mkImageFromSnapshot(t, r, "prj-1", "ubuntu-24-04", "ru-central1", snapID)

	require.Equal(t, domain.PrefixImage, img.ID[:3], "img- prefix")
	require.Equal(t, domain.ImageStatusReady, img.Status, "state READY → Status READY")
	require.Equal(t, domain.ImagePlacementRegional, img.Placement, "Image is REGIONAL const")
	require.Equal(t, domain.ImageFormatStandard, img.Format, "native Kachō format STANDARD")
	require.EqualValues(t, 21474836480, img.SizeBytes, "size derived from source snapshot")
	require.EqualValues(t, 21474836480, img.MinDiskBytes, "min_disk derived from source")

	got, err := r.Get(ctx, img.ID)
	require.NoError(t, err)
	require.Equal(t, "ubuntu-24-04", got.Name)
	require.Equal(t, "ru-central1", got.RegionID)
	require.Equal(t, snapID, got.SourceSnapshot)
	require.Empty(t, got.SourceVolume)
	require.Equal(t, domain.ImageStatusReady, got.Status)
	require.Equal(t, domain.ImagePlacementRegional, got.Placement)
}

// TestImageCreateFromVolume — STOR-1-24: Image из тома напрямую (source_volume_id) →
// READY, size derived из тома.
func TestImageCreateFromVolume(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	v := mkVolume(t, vr, "prj-1", "src-vol", 32<<30)
	img, err := ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "from-vol",
		RegionID: "ru-central1", SourceVolume: v.ID,
	})
	require.NoError(t, err)
	require.Equal(t, v.ID, img.SourceVolume)
	require.Empty(t, img.SourceSnapshot)
	require.EqualValues(t, 32<<30, img.SizeBytes, "size derived from source volume")
}

// TestImageGetNotFound — STOR-1-21: well-formed-но-нет → ErrNotFound "Image <id> not found".
func TestImageGetNotFound(t *testing.T) {
	r := pg.NewImageRepo(newTestPool(t))
	_, err := r.Get(context.Background(), "img00000000000000000")
	require.True(t, stderrors.Is(err, ports.ErrNotFound), "got %v", err)
	require.Equal(t, "Image img00000000000000000 not found", err.Error()[len("not found: "):])
}

// TestImageNameUniqueRace — STOR-1-21/04 (CONCURRENCY, -race): конкурентный Insert
// (project,name) → ровно один OK, остальные AlreadyExists "image with name <n>
// already exists in project" (partial UNIQUE 23505, data-integrity §5). Под -race.
func TestImageNameUniqueRace(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewImageRepo(pool)
	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-for-race", 1<<30)

	const n = 6
	var ok, dup atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := r.Insert(context.Background(), &domain.Image{
				ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "dup-img",
				RegionID: "ru-central1", SourceSnapshot: snapID,
			})
			switch {
			case err == nil:
				ok.Add(1)
			case stderrors.Is(err, ports.ErrAlreadyExists):
				dup.Add(1)
				require.Equal(t, "image with name dup-img already exists in project", err.Error()[len("already exists: "):])
			default:
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, 1, ok.Load(), "exactly one insert wins")
	require.EqualValues(t, n-1, dup.Load())

	// то же имя в другом проекте → OK (UNIQUE scoped проектом).
	snap2 := mkSnapshotRow(t, pool, "prj-2", "snap-beta", 1<<30)
	_, err := r.Insert(context.Background(), &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-2", Name: "dup-img",
		RegionID: "ru-central1", SourceSnapshot: snap2,
	})
	require.NoError(t, err, "same name in another project is allowed")
}

// TestImageSourceFKNotFound — STOR-1-24: неизвестный source snapshot/volume → same-DB
// FK 23503 → FailedPrecondition "<Resource> <id> not found" (контрактный тон).
func TestImageSourceFKNotFound(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewImageRepo(pool)
	ctx := context.Background()

	_, err := r.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "bad-snap-src",
		RegionID: "ru-central1", SourceSnapshot: "snp00000000000000000",
	})
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Snapshot snp00000000000000000 not found", err.Error()[len("failed precondition: "):])

	_, err = r.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "bad-vol-src",
		RegionID: "ru-central1", SourceVolume: "vol00000000000000000",
	})
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Volume vol00000000000000000 not found", err.Error()[len("failed precondition: "):])
}

// TestImageSourceExactlyOneDBCheck — F12 backstop: DB CHECK images_source_exactly_one
// отвергает оба-непусты И ни-одного (23514). Прямой SQL-insert (обходит sync-validate),
// чтобы лочить именно DB-инвариант.
func TestImageSourceExactlyOneDBCheck(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-x", 1<<30)
	v := mkVolume(t, pg.NewVolumeRepo(pool), "prj-1", "vol-x", 1<<30)

	// оба источника → CHECK 23514.
	_, err := pool.Exec(ctx, `INSERT INTO images
		(id, project_id, region_id, source_snapshot_id, source_volume_id, format, state)
		VALUES ($1,'prj-1','ru-central1',$2,$3,'STANDARD','READY')`,
		ids.NewID(domain.PrefixImage), snapID, v.ID)
	require.Error(t, err, "both sources must violate images_source_exactly_one CHECK")
	require.Contains(t, err.Error(), "images_source_exactly_one")

	// ни одного источника → CHECK 23514.
	_, err = pool.Exec(ctx, `INSERT INTO images
		(id, project_id, region_id, format, state)
		VALUES ($1,'prj-1','ru-central1','STANDARD','READY')`,
		ids.NewID(domain.PrefixImage))
	require.Error(t, err, "no source must violate images_source_exactly_one CHECK")
	require.Contains(t, err.Error(), "images_source_exactly_one")
}

// TestImageListCursorFilter — STOR-1-33/32: cursor (created_at,id) ASC, project-scope,
// filter=name, garbage token → InvalidArg.
func TestImageListCursorFilter(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewImageRepo(pool)
	ctx := context.Background()
	snap := mkSnapshotRow(t, pool, "prj-1", "snap-list", 1<<30)
	for _, n := range []string{"img-a", "img-b", "img-c"} {
		mkImageFromSnapshot(t, r, "prj-1", n, "ru-central1", snap)
	}
	snapOther := mkSnapshotRow(t, pool, "prj-2", "snap-other", 1<<30)
	mkImageFromSnapshot(t, r, "prj-2", "img-other", "ru-central1", snapOther)

	page1, next, err := r.List(ctx, image.Pagination{PageSize: 2, ProjectID: "prj-1"})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, next)

	page2, _, err := r.List(ctx, image.Pagination{PageSize: 2, ProjectID: "prj-1", PageToken: next})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	for _, i := range append(append([]*domain.Image{}, page1...), page2...) {
		require.Equal(t, "prj-1", i.ProjectID, "project-scope: prj-2 image never leaks")
	}

	filtered, _, err := r.List(ctx, image.Pagination{PageSize: 50, ProjectID: "prj-1", Filter: "img-b"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "img-b", filtered[0].Name)

	_, _, err = r.List(ctx, image.Pagination{PageSize: 50, ProjectID: "prj-1", PageToken: "%%%garbage%%%"})
	require.True(t, stderrors.Is(err, ports.ErrInvalidArg), "garbage token → InvalidArg, got %v", err)
}

// TestImageUpdateMutableAndNameCollision — STOR-1-22: Update name→existing →
// AlreadyExists; mutable description применяется.
func TestImageUpdateMutableAndNameCollision(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewImageRepo(pool)
	ctx := context.Background()
	snap := mkSnapshotRow(t, pool, "prj-1", "snap-upd", 1<<30)
	_ = mkImageFromSnapshot(t, r, "prj-1", "alpha", "ru-central1", snap)
	ib := mkImageFromSnapshot(t, r, "prj-1", "beta", "ru-central1", snap)

	name := "alpha"
	_, err := r.Update(ctx, ib.ID, image.ImageUpdate{Name: &name})
	require.True(t, stderrors.Is(err, ports.ErrAlreadyExists), "got %v", err)

	desc := "patched-desc"
	_, err = r.Update(ctx, ib.ID, image.ImageUpdate{Description: &desc})
	require.NoError(t, err)
	got, err := r.Get(ctx, ib.ID)
	require.NoError(t, err)
	require.Equal(t, "patched-desc", got.Description)
}

// TestImageNameBVADBCheck — STOR-1-30 backstop: имя длиной 64 (>63) отвергается
// DB-CHECK images_name_check (23514). Прямой SQL (domain-валидатор — отдельный уровень).
func TestImageNameBVADBCheck(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	snap := mkSnapshotRow(t, pool, "prj-1", "snap-bva", 1<<30)
	longName := ""
	for i := 0; i < 64; i++ {
		longName += "a"
	}
	_, err := pool.Exec(ctx, `INSERT INTO images
		(id, project_id, name, region_id, source_snapshot_id, format, state)
		VALUES ($1,'prj-1',$2,'ru-central1',$3,'STANDARD','READY')`,
		ids.NewID(domain.PrefixImage), longName, snap)
	require.Error(t, err, "64-char name must violate images_name_check")
	require.Contains(t, err.Error(), "images_name_check")

	// 63 символа → OK.
	name63 := longName[:63]
	_, err = pool.Exec(ctx, `INSERT INTO images
		(id, project_id, name, region_id, source_snapshot_id, format, state)
		VALUES ($1,'prj-1',$2,'ru-central1',$3,'STANDARD','READY')`,
		ids.NewID(domain.PrefixImage), name63, snap)
	require.NoError(t, err, "63-char name accepted")
}
