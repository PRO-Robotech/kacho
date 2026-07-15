// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/migrations"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/volume"
)

// seededDiskType — id из seed-миграции 0004 (block-balanced существует; volumes.
// disk_type_id RESTRICT требует существующий тип).
const seededDiskType = "block-balanced"

// newTestPool поднимает контейнер Postgres 16, прогоняет миграции kacho-storage
// (включая seed disk_types) и возвращает pgxpool с search_path=kacho_storage.
// Пропускается под -short. Каждый тест заводит данные сам.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test (testcontainers Postgres) — skipped with -short")
	}
	ctx := context.Background()

	pgC, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("kacho_storage"),
		tcpostgres.WithUsername("storage"),
		tcpostgres.WithPassword("secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgC.Terminate(ctx) })

	baseDSN, err := pgC.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	sqlDB, err := sql.Open("pgx", baseDSN)
	require.NoError(t, err)
	defer sqlDB.Close()
	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.Up(sqlDB, "."))

	// pool_max_conns=16 — даём race-тестам достаточно соединений, чтобы горутины
	// реально исполнялись параллельно (contended CAS / auto-device-name), а не
	// сериализовались на пуле (иначе гонка не воспроизводится).
	poolDSN := baseDSN + "&options=-c%20search_path%3Dkacho_storage%2Cpublic&pool_max_conns=16"
	pool, err := coredb.NewPool(ctx, poolDSN)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// mkVolume вставляет том через репо (state=READY) и возвращает его.
func mkVolume(t *testing.T, r *pg.VolumeRepo, project, name string, size int64) *domain.Volume {
	t.Helper()
	v, err := r.Insert(context.Background(), &domain.Volume{
		ID:         ids.NewID(domain.PrefixVolume),
		ProjectID:  project,
		Name:       name,
		ZoneID:     "region-1-a",
		DiskTypeID: seededDiskType,
		SizeBytes:  size,
	})
	require.NoError(t, err)
	return v
}

// attach вставляет строку volume_attachments напрямую (attach-CAS — S2; здесь тест
// FK-инвариантов delete/derived-status независим от attach-пути).
func attach(t *testing.T, pool *pgxpool.Pool, volumeID, instanceID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO volume_attachments (volume_id, instance_id, instance_name, project_id, zone_id, device_name)
		 VALUES ($1,$2,'web-1','prj-1','region-1-a','sdb')`, volumeID, instanceID)
	require.NoError(t, err)
}

// TestVolumeCreateGetDerivedStatus — Insert (state READY, block_size default) → Get
// (AVAILABLE, поля); привязка → derived IN_USE + attachments/usedBy (S1-01, §1.3/1.5).
func TestVolumeCreateGetDerivedStatus(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	v := mkVolume(t, r, "prj-1", "vol-data-1", 10<<30)
	require.Equal(t, domain.PrefixVolume, v.ID[:3])
	require.EqualValues(t, 4096, v.BlockSize, "block_size default")
	require.Equal(t, domain.VolumeStatusAvailable, v.Status)

	got, err := r.Get(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, "vol-data-1", got.Name)
	require.EqualValues(t, 10<<30, got.SizeBytes)
	require.Equal(t, domain.VolumeStatusAvailable, got.Status)
	require.Empty(t, got.Attachments)

	attach(t, pool, v.ID, "epd00000000000000001")
	got, err = r.Get(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, domain.VolumeStatusInUse, got.Status, "READY + attachment → IN_USE (derived)")
	require.Len(t, got.Attachments, 1)
	require.Equal(t, "epd00000000000000001", got.Attachments[0].InstanceID)
}

// TestVolumeGetNotFound — well-formed-но-нет → ErrNotFound "Volume <id> not found".
func TestVolumeGetNotFound(t *testing.T) {
	r := pg.NewVolumeRepo(newTestPool(t))
	_, err := r.Get(context.Background(), "vol00000000000000000")
	require.True(t, stderrors.Is(err, ports.ErrNotFound), "got %v", err)
	require.Equal(t, "Volume vol00000000000000000 not found", err.Error()[len("not found: "):])
}

// TestVolumeNameUniqueRace — конкурентный Insert (project,name) → ровно один OK,
// остальные AlreadyExists "volume with name <n> already exists in project"
// (partial UNIQUE 23505, data-integrity.md чек-лист п.5). Под -race.
func TestVolumeNameUniqueRace(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	const n = 6
	var ok, dup atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := r.Insert(context.Background(), &domain.Volume{
				ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "dup-name",
				ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
			})
			switch {
			case err == nil:
				ok.Add(1)
			case stderrors.Is(err, ports.ErrAlreadyExists):
				dup.Add(1)
				require.Equal(t, "volume with name dup-name already exists in project", err.Error()[len("already exists: "):])
			default:
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, 1, ok.Load(), "exactly one insert wins")
	require.EqualValues(t, n-1, dup.Load())
}

// TestVolumeSizeIncreaseOnly — increase ok; shrink/equal → InvalidArg "Volume size
// can only be increased" (DB-CAS increase-only, S1-04/A8); concurrent identical
// increase → ровно один OK (size-CAS race, под -race).
func TestVolumeSizeIncreaseOnly(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v := mkVolume(t, r, "prj-1", "vol-resize", 10<<30)

	big := int64(20 << 30)
	up, err := r.Update(ctx, v.ID, volume.VolumeUpdate{SizeBytes: &big})
	require.NoError(t, err)
	require.EqualValues(t, 20<<30, up.SizeBytes)

	for _, shrink := range []int64{5 << 30, 20 << 30} { // меньше и равно — оба отвергаются
		s := shrink
		_, err := r.Update(ctx, v.ID, volume.VolumeUpdate{SizeBytes: &s})
		require.True(t, stderrors.Is(err, ports.ErrInvalidArg), "shrink %d: %v", shrink, err)
		require.Equal(t, "Volume size can only be increased", err.Error()[len("invalid argument: "):])
	}

	// size-CAS race: N goroutines одинаково 20→40; ровно одна выигрывает.
	v2 := mkVolume(t, r, "prj-1", "vol-race", 20<<30)
	const n = 6
	var ok, rej atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s := int64(40 << 30)
			_, err := r.Update(context.Background(), v2.ID, volume.VolumeUpdate{SizeBytes: &s})
			switch {
			case err == nil:
				ok.Add(1)
			case stderrors.Is(err, ports.ErrInvalidArg):
				rej.Add(1)
			default:
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, 1, ok.Load(), "exactly one size-CAS wins")
	require.EqualValues(t, n-1, rej.Load())
}

// TestVolumeDeleteFKRestrict — привязанный том → FailedPrecondition "Volume <id> is
// in use" (FK RESTRICT 23503, S1-07/A3); после detach delete проходит → NotFound.
func TestVolumeDeleteFKRestrict(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v := mkVolume(t, r, "prj-1", "vol-attached", 10<<30)
	attach(t, pool, v.ID, "epd00000000000000009")

	err := r.Delete(ctx, v.ID)
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, fmt.Sprintf("Volume %s is in use", v.ID), err.Error()[len("failed precondition: "):])
	_, gerr := r.Get(ctx, v.ID)
	require.NoError(t, gerr, "attached volume still present")

	_, err = pool.Exec(ctx, `DELETE FROM volume_attachments WHERE volume_id=$1`, v.ID)
	require.NoError(t, err)
	require.NoError(t, r.Delete(ctx, v.ID))
	_, gerr = r.Get(ctx, v.ID)
	require.True(t, stderrors.Is(gerr, ports.ErrNotFound))
}

// TestVolumeDiskTypeAndSnapshotFK — несуществующий disk_type → "DiskType <id> not
// found" (S1-08/Q4); несуществующий source_snapshot → "Snapshot <id> not found"
// (S1-12); из существующего снапшота → OK (same-DB FK).
func TestVolumeDiskTypeAndSnapshotFK(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	_, err := r.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "v-badtype",
		ZoneID: "region-1-a", DiskTypeID: "dtp-nonexistent", SizeBytes: 1 << 30,
	})
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "DiskType dtp-nonexistent not found", err.Error()[len("failed precondition: "):])

	_, err = r.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "v-badsnap",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
		SourceSnapshot: "snp00000000000000000",
	})
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Snapshot snp00000000000000000 not found", err.Error()[len("failed precondition: "):])

	snapID := ids.NewID(domain.PrefixSnapshot)
	_, err = pool.Exec(ctx,
		`INSERT INTO snapshots (id, project_id, name, size_bytes, state) VALUES ($1,'prj-1','snap-a',0,'READY')`, snapID)
	require.NoError(t, err)
	fromSnap, err := r.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "v-fromsnap",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 30, SourceSnapshot: snapID,
	})
	require.NoError(t, err)
	require.Equal(t, snapID, fromSnap.SourceSnapshot)
}

// TestVolumeListCursorFilter — cursor (created_at,id) ASC, project-scope, filter=name,
// garbage token → InvalidArg (S1-03).
func TestVolumeListCursorFilter(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	for _, n := range []string{"vol-a", "vol-b", "vol-c"} {
		mkVolume(t, r, "prj-1", n, 1<<30)
	}
	mkVolume(t, r, "prj-2", "vol-other", 1<<30)

	page1, next, err := r.List(ctx, volume.Pagination{PageSize: 2, ProjectID: "prj-1"})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, next)

	page2, _, err := r.List(ctx, volume.Pagination{PageSize: 2, ProjectID: "prj-1", PageToken: next})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	// project-scope: том prj-2 не встречается.
	for _, v := range append(append([]*domain.Volume{}, page1...), page2...) {
		require.Equal(t, "prj-1", v.ProjectID)
	}

	filtered, _, err := r.List(ctx, volume.Pagination{PageSize: 50, ProjectID: "prj-1", Filter: "vol-b"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "vol-b", filtered[0].Name)

	_, _, err = r.List(ctx, volume.Pagination{PageSize: 50, PageToken: "%%%garbage%%%"})
	require.True(t, stderrors.Is(err, ports.ErrInvalidArg), "garbage token → InvalidArg, got %v", err)
}

// TestVolumeUpdateMutableAndNameCollision — Update name→existing → AlreadyExists
// (partial UNIQUE); name→"" ок (partial UNIQUE не действует на пустое, два безымянных
// легальны); mutable description применяется (S1-05/S1-06).
func TestVolumeUpdateMutableAndNameCollision(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	_ = mkVolume(t, r, "prj-1", "alpha", 1<<30)
	vb := mkVolume(t, r, "prj-1", "beta", 1<<30)

	name := "alpha"
	_, err := r.Update(ctx, vb.ID, volume.VolumeUpdate{Name: &name})
	require.True(t, stderrors.Is(err, ports.ErrAlreadyExists), "got %v", err)

	empty := ""
	_, err = r.Update(ctx, vb.ID, volume.VolumeUpdate{Name: &empty})
	require.NoError(t, err, "clearing name allowed (partial UNIQUE ignores '')")

	desc := "patched"
	_, err = r.Update(ctx, vb.ID, volume.VolumeUpdate{Description: &desc})
	require.NoError(t, err)
	got, err := r.Get(ctx, vb.ID)
	require.NoError(t, err)
	require.Equal(t, "patched", got.Description)
	require.Empty(t, got.Name)
}
