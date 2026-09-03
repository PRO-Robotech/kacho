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

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/filter"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// seededDiskType — класс, который фикстуры заводят САМИ.
//
// Прежде он приходил из посева миграции. Посев снят: класс — регистрация того, что
// реально даёт провайдер, и выдуманного каталога заранее быть не должно. Фикстуры
// теперь сеют свой класс, свой бэкенд и действующую ревизию привязки — ровно то, что
// требуется от арендатора на живом стенде, поэтому проба заодно проверяет достижимость
// этого пути.
const seededDiskType = "block-fixture"

// seedFixtureCatalog заводит класс, бэкенд и ДЕЙСТВУЮЩУЮ ревизию привязки на каждую
// зону, которой пользуются фикстуры пакета.
//
// Без действующей ревизии том не создаётся вовсе: вставка требует её, потому что
// исполнять создание иначе некому. Это не ужесточение ради проб — это тот же
// инвариант, которым закрыт «создаваемый навсегда».
func seedFixtureCatalog(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	// Перечень выведен из ЗОН, которыми пользуются фикстуры пакета, а не выписан
	// на глаз: класс без действующей ревизии в зоне не обслуживает её вовсе, и
	// недостающая зона читается как дефект продукта («нет действующей привязки»),
	// хотя это пробел подготовки.
	zones := []string{
		"region-1-a", "region-1-b",
		"region-2-a", "region-2-b",
		"ru-central1-a", "ru-central1-b",
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO disk_types (id, name, lifecycle) VALUES ($1, $1, 'ACTIVE')
		ON CONFLICT (id) DO NOTHING`, seededDiskType)
	require.NoError(t, err)

	backendID := ids.NewHyphenID("sb")
	_, err = pool.Exec(ctx, `
		INSERT INTO storage_backends (id, name, kind, zone_ids, endpoint, credentials_ref)
		VALUES ($1, $1, 'CEPH_RBD', '[]'::jsonb, 'cfg://fixture', 'vault://fixture')`, backendID)
	require.NoError(t, err)

	for _, zone := range zones {
		_, err = pool.Exec(ctx, `
			INSERT INTO disk_type_bindings
				(id, disk_type_id, zone_id, backend_id, revision, pool, namespace_template,
				 cap_snapshots, cap_clone_from_snapshot, cap_clone_from_image, cap_online_grow, status)
			VALUES ($1, $2, $3, $4, 1, 'kacho-fixture', '{projectId}', true, true, true, true, 'ACTIVE')`,
			ids.NewHyphenID("dtb"), seededDiskType, zone, backendID)
		require.NoError(t, err)
	}
}

// offerDiskTypeInZone заводит ДЕЙСТВУЮЩУЮ ревизию привязки для класса в зоне.
//
// В продукте класс не предлагается, пока не объявлено, ЧЕМ он обслуживается:
// без действующей ревизии вставка тома отвергается. Проба, заводящая свой класс,
// обязана пройти тот же путь — иначе она падает отказом «нет действующей
// привязки» и читается как дефект продукта, хотя это пробел подготовки.
func offerDiskTypeInZone(t *testing.T, pool *pgxpool.Pool, diskTypeID string, zones ...string) {
	t.Helper()
	ctx := context.Background()
	backendID := ids.NewHyphenID("sb")
	_, err := pool.Exec(ctx, `
		INSERT INTO storage_backends (id, name, kind, zone_ids, endpoint, credentials_ref)
		VALUES ($1, $1, 'CEPH_RBD', '[]'::jsonb, 'cfg://fixture', 'vault://fixture')`, backendID)
	require.NoError(t, err)
	for _, zone := range zones {
		_, err = pool.Exec(ctx, `
			INSERT INTO disk_type_bindings
				(id, disk_type_id, zone_id, backend_id, revision, pool, namespace_template,
				 cap_snapshots, cap_clone_from_snapshot, cap_clone_from_image, cap_online_grow, status)
			VALUES ($1, $2, $3, $4, 1, 'kacho-fixture', '{projectId}', true, true, true, true, 'ACTIVE')`,
			ids.NewHyphenID("dtb"), diskTypeID, zone, backendID)
		require.NoError(t, err)
	}
}

// imageRegionFixture — регион, который фикстуры образов объявляют явно
// (mkImageFromSnapshot(..., "ru-central1", ...)). Он же передаётся в
// VolumeRepo.Insert как регион ЗОНЫ тома: зона фикстур называется "region-1-a" и
// с этим регионом никак не связана по имени — регион приходит от владельца
// Geography, а не выводится из строки зоны.
const imageRegionFixture = "ru-central1"

// fixtureZone — зона, в которой живут фикстуры пакета. Названа один раз: та же
// строка, выписанная в каждой пробе, разъезжается с перечнем зон посева молча.
const fixtureZone = "region-1-a"

// newBareTestPool — база БЕЗ посева фикстурного каталога.
//
// Пробе, чей предмет и есть каталог (ревизии привязки, кластеры данных, политика
// класса), посев мешает по существу: она считает строки и обязана считать СВОИ.
// Общий посев делал её утверждение о содержимом таблицы утверждением о чужой
// подготовке — «должно быть 1, а есть 5» ровно на число посеянных зон.
//
// Правильный разрез не «сузить счёт», а «владеть своим предметом»: тест каталога
// заводит каталог сам, тест тома получает готовый.
func newBareTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newPoolWithCatalog(t, false)
}

// newTestPool выдаёт тесту СОБСТВЕННУЮ базу на одном контейнере пакета — клон
// шаблона, в который миграции kacho-storage (включая seed disk_types) накатаны
// один раз (см. TestMain и internal/pgtest). Возвращает pgxpool с
// search_path=kacho_storage. Пропускается под -short. Каждый тест заводит данные
// сам.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newPoolWithCatalog(t, true)
}

func newPoolWithCatalog(t *testing.T, seed bool) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test (testcontainers Postgres) — skipped with -short")
	}
	ctx := context.Background()

	baseDSN := pgtest.NewDB(t)

	// pool_max_conns=16 — даём race-тестам достаточно соединений, чтобы горутины
	// реально исполнялись параллельно (contended CAS / auto-device-name), а не
	// сериализовались на пуле (иначе гонка не воспроизводится).
	poolDSN := baseDSN + "&pool_max_conns=16"
	pool, err := coredb.NewPool(ctx, poolDSN)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	// Строки учёта заводятся ОБОИМ видам пула, а не только «с каталогом»:
	// каталог классов и учёт числа ресурсов — разные условия. Вставка строки
	// ресурса списывает место у ЛЮБОГО пула, поэтому пул без учёта отвергал бы
	// каждую вставку «потолок не назван» независимо от того, нужен ли пробе
	// каталог классов.
	seedFixtureQuotas(t, pool)
	if seed {
		seedFixtureCatalog(t, pool)
	}
	return pool
}

// mkVolume заводит том и доводит его до готовности ТЕМ ЖЕ путём, что и бой.
//
// Вставка фиксирует НАМЕРЕНИЕ (state=CREATING) — готовность производит сверщик,
// увидевший объект у бэкенда. Поэтому фикстура не пишет 'READY' руками: она
// зовёт то же подтверждение, которое зовёт сверщик. Рукописное состояние сделало
// бы фикстуру снисходительнее продукта — том оказывался бы готов там, где
// продукт этого не умеет, и проба привязки зеленела бы на состоянии, которого в
// бою не бывает.
func mkVolume(t *testing.T, pool *pgxpool.Pool, r *pg.VolumeRepo, project, name string, size int64) *domain.Volume {
	t.Helper()
	v := mkVolumeCreating(t, r, project, name, size)
	confirmReady(t, pool, reconciler.KindVolume, v.ID, size)
	v.Status = domain.VolumeStatusAvailable
	return v
}

// mkVolumeCreating заводит том и ОСТАВЛЯЕТ его в намерении — для проб, чей
// предмет и есть неподтверждённое состояние.
func mkVolumeCreating(t *testing.T, r *pg.VolumeRepo, project, name string, size int64) *domain.Volume {
	t.Helper()
	v, _, err := r.Insert(context.Background(), &domain.Volume{
		ID:         ids.NewID(domain.PrefixVolume),
		ProjectID:  project,
		Name:       name,
		ZoneID:     "region-1-a",
		DiskTypeID: seededDiskType,
		SizeBytes:  size,
	}, "")
	require.NoError(t, err)
	return v
}

// confirmReady — подтверждение ресурса тем же вызовом, которым его подтверждает
// сверщик, увидев объект у бэкенда.
func confirmReady(t *testing.T, pool *pgxpool.Pool, kind reconciler.Kind, id string, size int64) {
	t.Helper()
	applied, err := reconciler.NewStore(pool).Confirm(
		context.Background(), kind, id,
		blockbackend.Observed{State: blockbackend.ObservedReady, SizeBytes: size})
	require.NoError(t, err)
	// Применение утверждается явно: подтверждение, не тронувшее ни одной строки,
	// оставило бы ресурс в намерении, а фикстура выглядела бы исполненной.
	require.True(t, applied, "подтверждение не применилось ни к одной строке")
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

// TestVolumeCreateGetDerivedStatus — Insert (state READY) → Get (AVAILABLE, поля);
// привязка → derived IN_USE + attachments/usedBy (S1-01, §1.3/1.5).
//
// Утверждение про block_size снято вместе с полем: у него не было читателя,
// меняющего поведение, поэтому проба закрепляла умолчание схемы, а не свойство
// продукта. Колонка живёт в схеме со своим умолчанием и контрактом не адресуется.
func TestVolumeCreateGetDerivedStatus(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	v := mkVolume(t, pool, r, "prj-1", "vol-data-1", 10<<30)
	require.Equal(t, domain.PrefixVolume, v.ID[:3])
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
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound), "got %v", err)
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
			_, _, err := r.Insert(context.Background(), &domain.Volume{
				ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "dup-name",
				ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
			}, "")
			switch {
			case err == nil:
				ok.Add(1)
			case stderrors.Is(err, storageerr.ErrAlreadyExists):
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
	v := mkVolume(t, pool, r, "prj-1", "vol-resize", 10<<30)

	big := int64(20 << 30)
	up, _, err := r.Update(ctx, v.ID, volume.VolumeUpdate{SizeBytes: &big})
	require.NoError(t, err)
	require.EqualValues(t, 20<<30, up.SizeBytes)

	for _, shrink := range []int64{5 << 30, 20 << 30} { // меньше и равно — оба отвергаются
		s := shrink
		_, _, err := r.Update(ctx, v.ID, volume.VolumeUpdate{SizeBytes: &s})
		require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "shrink %d: %v", shrink, err)
		require.Equal(t, "Volume size can only be increased", err.Error()[len("invalid argument: "):])
	}

	// size-CAS race: N goroutines одинаково 20→40; ровно одна выигрывает.
	v2 := mkVolume(t, pool, r, "prj-1", "vol-race", 20<<30)
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
			_, _, err := r.Update(context.Background(), v2.ID, volume.VolumeUpdate{SizeBytes: &s})
			switch {
			case err == nil:
				ok.Add(1)
			case stderrors.Is(err, storageerr.ErrInvalidArg):
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
	v := mkVolume(t, pool, r, "prj-1", "vol-attached", 10<<30)
	attach(t, pool, v.ID, "epd00000000000000009")

	err := r.Delete(ctx, v.ID)
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, fmt.Sprintf("Volume %s is in use", v.ID), err.Error()[len("failed precondition: "):])
	_, gerr := r.Get(ctx, v.ID)
	require.NoError(t, gerr, "attached volume still present")

	_, err = pool.Exec(ctx, `DELETE FROM volume_attachments WHERE volume_id=$1`, v.ID)
	require.NoError(t, err)
	require.NoError(t, r.Delete(ctx, v.ID))
	_, gerr = r.Get(ctx, v.ID)
	require.True(t, stderrors.Is(gerr, storageerr.ErrNotFound))
}

// TestVolumeDiskTypeAndSnapshotFK — несуществующий disk_type → "DiskType <id> not
// found" (S1-08/Q4); несуществующий source_snapshot → "Snapshot <id> not found"
// (S1-12); из существующего снапшота → OK (same-DB FK).
func TestVolumeDiskTypeAndSnapshotFK(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	_, _, err := r.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "v-badtype",
		ZoneID: "region-1-a", DiskTypeID: "dtp-nonexistent", SizeBytes: 1 << 30,
	}, "")
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "DiskType dtp-nonexistent not found", err.Error()[len("failed precondition: "):])

	_, _, err = r.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "v-badsnap",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
		SourceSnapshot: "snp00000000000000000",
	}, "")
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Snapshot snp00000000000000000 not found", err.Error()[len("failed precondition: "):])

	snapID := ids.NewID(domain.PrefixSnapshot)
	_, err = pool.Exec(ctx,
		`INSERT INTO snapshots (id, project_id, name, size_bytes, state, zone_id)
		 VALUES ($1,'prj-1','snap-a',0,'READY','region-1-a')`, snapID)
	require.NoError(t, err)
	fromSnap, _, err := r.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "v-fromsnap",
		ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 1 << 30, SourceSnapshot: snapID,
	}, "")
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
		mkVolume(t, pool, r, "prj-1", n, 1<<30)
	}
	mkVolume(t, pool, r, "prj-2", "vol-other", 1<<30)

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

	// Репозиторий получает РАЗОБРАННЫЙ узел, а не голое значение: оператор — часть
	// выражения (#460). Узел строится тем же Parse, что зовёт use-case, — фикстура
	// не вправе быть снисходительнее продукта.
	nameEq, perr := filter.Parse(`name="vol-b"`, []string{"name"})
	require.NoError(t, perr)
	filtered, _, err := r.List(ctx, volume.Pagination{PageSize: 50, ProjectID: "prj-1", FilterAST: nameEq})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "vol-b", filtered[0].Name)

	// CONTAINS — подстрока. Равенство выше сузило до ОДНОЙ строки, поэтому три
	// строки здесь пришли от оператора, а не от предиката, которого нет: без
	// парного контроля «вернулось всё» неотличимо от «фильтр не применился».
	nameSub, perr := filter.Parse(`name CONTAINS "vol-"`, []string{"name"})
	require.NoError(t, perr)
	subset, _, err := r.List(ctx, volume.Pagination{PageSize: 50, ProjectID: "prj-1", FilterAST: nameSub})
	require.NoError(t, err)
	require.Len(t, subset, 3, "подстрока обязана вернуть все три тома проекта")
	for _, v := range subset {
		// vol-other из prj-2 подстроке ТОЖЕ отвечает — и не приходит: подстрочный
		// фильтр не отменяет project-scope.
		require.Equal(t, "prj-1", v.ProjectID)
	}

	// Совпадение ВНУТРИ имени, а не префикс: `l-b` начинает ни одно из имён.
	nameMid, perr := filter.Parse(`name CONTAINS "l-b"`, []string{"name"})
	require.NoError(t, perr)
	mid, _, err := r.List(ctx, volume.Pagination{PageSize: 50, ProjectID: "prj-1", FilterAST: nameMid})
	require.NoError(t, err)
	require.Len(t, mid, 1)
	require.Equal(t, "vol-b", mid[0].Name)

	_, _, err = r.List(ctx, volume.Pagination{PageSize: 50, PageToken: "%%%garbage%%%"})
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "garbage token → InvalidArg, got %v", err)
}

// TestVolumeUpdateMutableAndNameCollision — Update name→existing → AlreadyExists
// (partial UNIQUE); name→"" ок (partial UNIQUE не действует на пустое, два безымянных
// легальны); mutable description применяется (S1-05/S1-06).
func TestVolumeUpdateMutableAndNameCollision(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	_ = mkVolume(t, pool, r, "prj-1", "alpha", 1<<30)
	vb := mkVolume(t, pool, r, "prj-1", "beta", 1<<30)

	name := "alpha"
	_, _, err := r.Update(ctx, vb.ID, volume.VolumeUpdate{Name: &name})
	require.True(t, stderrors.Is(err, storageerr.ErrAlreadyExists), "got %v", err)

	// Здесь стояло «очистка имени разрешена (частичный UNIQUE игнорирует '')».
	// Ни очистки, ни частичного индекса больше нет: форма (#715) пустую строку
	// не принимает, а миграция 715001 заменила частичный индекс полным.
	// Утверждение перевёрнуто, а не снято, — иначе пропал бы контроль на то,
	// что правкой имя нельзя опустошить.
	empty := ""
	_, _, err = r.Update(ctx, vb.ID, volume.VolumeUpdate{Name: &empty})
	require.Error(t, err, "очистка имени правкой больше не допускается")

	desc := "patched"
	_, _, err = r.Update(ctx, vb.ID, volume.VolumeUpdate{Description: &desc})
	require.NoError(t, err)
	got, err := r.Get(ctx, vb.ID)
	require.NoError(t, err)
	require.Equal(t, "patched", got.Description)
	// Имя осталось прежним: очистить его правкой больше нельзя (см. выше).
	require.Equal(t, "beta", got.Name)
}
