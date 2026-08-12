// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// Образ перестаёт быть строкой-о-себе и становится строкой о ЧУЖОМ объекте: он
// наследует ревизию привязки от источника, несёт своё имя объекта у бэкенда и
// рождается CREATING — готовым его объявит сверщик, увидев объект.
//
// Исключение ровно одно и оно здесь же: зарегистрированный образ (объект внесён в
// хранилище вне облака) рождается СРАЗУ готовым, а имя объекта приходит извне.
//
// Фикстуры этого файла самодостаточны: класс, бэкенд и ревизию привязки они заводят
// сами, а не опираются на посев каталога — посев снят (миграция 0016), и опора на
// него означала бы пробу, зелёную только на несуществующем состоянии базы.

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// imgFixtureDiskType — класс диска фикстур этого файла. Заводится своим id, чтобы
// пробы не зависели ни от посева (снят), ни от того, каким классом пользуются
// соседние файлы.
const imgFixtureDiskType = "block-image-fixture"

// imgFixtureRegion — регион, в котором живут образы этого файла. Зоны, его
// составляющие, — fixtureRegionZones: связь «зона → регион» приходит от владельца
// Geography и никогда не выводится разбором имени.
const imgFixtureRegion = "region-1"

// imgFixtureDiskTypeRow заводит класс диска фикстур. Отдельно от ревизии привязки:
// строке тома класс нужен ограничительной внешней связью, а ревизия — только там,
// где предмет пробы её наследование.
func imgFixtureDiskTypeRow(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO disk_types (id, name, description, zone_ids, performance_tier, lifecycle)
		 VALUES ($1,$1,'fixture','[]'::jsonb,'BALANCED','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		imgFixtureDiskType)
	require.NoError(t, err)
	return imgFixtureDiskType
}

// imgFixtureClass заводит класс диска и ДЕЙСТВУЮЩУЮ ревизию привязки на зону,
// возвращая id ревизии. Без ревизии том не создаётся вовсе, а образу нечего
// наследовать.
func imgFixtureClass(t *testing.T, pool *pgxpool.Pool, zone string) string {
	t.Helper()
	ctx := context.Background()
	imgFixtureDiskTypeRow(t, pool)

	backendID := "sb-imgfixture" + zone
	_, err := pool.Exec(ctx,
		`INSERT INTO storage_backends (id, name, kind, endpoint, credentials_ref)
		 VALUES ($1,$1,'CEPH_RBD','cfg://ceph/fixture','vault://kacho/storage/fixture')
		 ON CONFLICT (id) DO NOTHING`, backendID)
	require.NoError(t, err)

	bindingID := "dtb-imgfixture" + zone
	_, err = pool.Exec(ctx,
		`INSERT INTO disk_type_bindings
		   (id, disk_type_id, zone_id, backend_id, revision, pool, status,
		    cap_snapshots, cap_clone_from_snapshot, cap_clone_from_image)
		 VALUES ($1,$2,$3,$4,1,'kacho-block','ACTIVE',true,true,true)
		 ON CONFLICT (id) DO NOTHING`,
		bindingID, imgFixtureDiskType, zone, backendID)
	require.NoError(t, err)
	return bindingID
}

// imgFixtureVolumeRow вставляет том напрямую: с названной ревизией привязки либо
// БЕЗ неё (bindingID == "" → NULL — второй полюс наследования: наследовать нечего).
// Прямая вставка, а не путь репозитория тома: предмет проб ниже — НАСЛЕДОВАНИЕ
// политики образом, а не создание тома.
func imgFixtureVolumeRow(t *testing.T, pool *pgxpool.Pool, project, name, zone, bindingID string, size int64) string {
	t.Helper()
	imgFixtureDiskTypeRow(t, pool)
	id := ids.NewID(domain.PrefixVolume)
	var binding *string
	if bindingID != "" {
		binding = &bindingID
	}
	_, err := pool.Exec(context.Background(),
		`INSERT INTO volumes (id, project_id, name, zone_id, disk_type_id, size_bytes, state,
		                      binding_id, backend_object)
		 VALUES ($1,$2,$3,$4,$5,$6,'READY',$7,$8)`,
		id, project, name, zone, imgFixtureDiskType, size, binding, "kctest-"+id)
	require.NoError(t, err)
	return id
}

// imgFixtureSnapshotRow вставляет снимок с ревизией привязки и происхождением от
// названного тома (снимок своей зоны не несёт — его размещение это том).
func imgFixtureSnapshotRow(t *testing.T, pool *pgxpool.Pool, project, name, volumeID, bindingID string) string {
	t.Helper()
	id := ids.NewID(domain.PrefixSnapshot)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO snapshots (id, project_id, name, source_volume_id, size_bytes, state,
		                        binding_id, backend_object)
		 VALUES ($1,$2,$3,$4,$5,'READY',$6,$7)`,
		id, project, name, volumeID, int64(20)<<30, bindingID, "kctest-"+id)
	require.NoError(t, err)
	return id
}

// TestImageBornCreatingInheritsBinding — STOR-P-18 для образа: образ рождается
// CREATING, наблюдённое состояние ABSENT, ревизия привязки унаследована от
// источника, имя объекта — то, что вывел use-case.
//
// Наследование, а не собственный выбор: образ лежит там же, где его источник, и
// выбирать ему ревизию заново значило бы завести второй источник истины о том, где
// эти байты находятся.
func TestImageBornCreatingInheritsBinding(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	bindingID := imgFixtureClass(t, pool, "region-1-a")
	volID := imgFixtureVolumeRow(t, pool, "prj-1", "src-vol", "region-1-a", bindingID, 20<<30)

	imgID := ids.NewID(domain.PrefixImage)
	img, _, err := ir.Insert(ctx, &domain.Image{
		ID: imgID, ProjectID: "prj-1", Name: "from-vol", RegionID: imgFixtureRegion,
		SourceVolume: volID,
		Backend:      domain.Placement{BackendObject: "kctest-" + imgID},
	}, fixtureRegionZones)
	require.NoError(t, err)
	require.Equal(t, domain.ImageStatusCreating, img.Status,
		"образ рождается CREATING: объекта у бэкенда ещё нет, готовность объявит сверщик")

	internal, err := ir.GetInternal(ctx, imgID)
	require.NoError(t, err)
	require.Equal(t, bindingID, internal.Backend.BindingID, "ревизия унаследована от источника")
	require.Equal(t, "kctest-"+imgID, internal.Backend.BackendObject)
	require.Equal(t, domain.ObservedAbsent, internal.Observation.State,
		"наблюдения ещё не было — объекта нет")

	// Второй полюс наследования: у источника без ревизии наследовать нечего, и
	// придумывать её нельзя. Иначе «унаследовал» зеленело бы на реализации,
	// подставляющей первую попавшуюся ревизию.
	bareVol := imgFixtureVolumeRow(t, pool, "prj-1", "src-vol-bare", "region-1-a", "", 20<<30)
	bareID := ids.NewID(domain.PrefixImage)
	_, _, err = ir.Insert(ctx, &domain.Image{
		ID: bareID, ProjectID: "prj-1", Name: "from-bare-vol", RegionID: imgFixtureRegion,
		SourceVolume: bareVol,
	}, fixtureRegionZones)
	require.NoError(t, err)
	bare, err := ir.GetInternal(ctx, bareID)
	require.NoError(t, err)
	require.Empty(t, bare.Backend.BindingID, "у источника без ревизии наследовать нечего")
}

// TestImageInheritsBindingFromSnapshot — та же связь через снимок: снимок несёт
// ревизию своего тома, образ — ревизию снимка.
func TestImageInheritsBindingFromSnapshot(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	bindingID := imgFixtureClass(t, pool, "region-1-a")
	volID := imgFixtureVolumeRow(t, pool, "prj-1", "snap-src-vol", "region-1-a", bindingID, 20<<30)
	snapID := imgFixtureSnapshotRow(t, pool, "prj-1", "src-snap", volID, bindingID)

	imgID := ids.NewID(domain.PrefixImage)
	_, _, err := ir.Insert(ctx, &domain.Image{
		ID: imgID, ProjectID: "prj-1", Name: "from-snap", RegionID: imgFixtureRegion,
		SourceSnapshot: snapID,
		Backend:        domain.Placement{BackendObject: "kctest-" + imgID},
	}, fixtureRegionZones)
	require.NoError(t, err)

	internal, err := ir.GetInternal(ctx, imgID)
	require.NoError(t, err)
	require.Equal(t, bindingID, internal.Backend.BindingID, "ревизия унаследована через снимок")
}

// TestImageSourceNotReadyRejected — STOR-P-44 со стороны образа: неготовый источник
// не захватывается, и отказ называет причину вслух — источник СВОЙ, вызывающий его
// видит, скрывать нечего.
//
// Зеркальная проба рядом: тот же источник, доведённый до готовности, проходит.
// Без неё отказ зеленел бы на реализации, отвергающей любой захват.
func TestImageSourceNotReadyRejected(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	bindingID := imgFixtureClass(t, pool, "region-1-a")
	volID := imgFixtureVolumeRow(t, pool, "prj-1", "vol-creating", "region-1-a", bindingID, 20<<30)
	_, err := pool.Exec(ctx, `UPDATE volumes SET state='CREATING' WHERE id=$1`, volID)
	require.NoError(t, err)

	_, _, err = ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "img-from-creating",
		RegionID: imgFixtureRegion, SourceVolume: volID,
	}, fixtureRegionZones)
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Volume "+volID+" is not ready", err.Error()[len("failed precondition: "):])

	snapID := imgFixtureSnapshotRow(t, pool, "prj-1", "snap-creating", volID, bindingID)
	_, err = pool.Exec(ctx, `UPDATE snapshots SET state='CREATING' WHERE id=$1`, snapID)
	require.NoError(t, err)
	_, _, err = ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "img-from-creating-snap",
		RegionID: imgFixtureRegion, SourceSnapshot: snapID,
	}, fixtureRegionZones)
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Snapshot "+snapID+" is not ready", err.Error()[len("failed precondition: "):])

	// Зеркало: источники доведены до готовности — захват проходит.
	_, err = pool.Exec(ctx, `UPDATE volumes SET state='READY' WHERE id=$1`, volID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE snapshots SET state='READY' WHERE id=$1`, snapID)
	require.NoError(t, err)
	_, _, err = ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "img-from-ready-vol",
		RegionID: imgFixtureRegion, SourceVolume: volID,
	}, fixtureRegionZones)
	require.NoError(t, err, "готовый источник захватывается")
	_, _, err = ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "img-from-ready-snap",
		RegionID: imgFixtureRegion, SourceSnapshot: snapID,
	}, fixtureRegionZones)
	require.NoError(t, err, "готовый снимок захватывается")
}

// TestImageForeignProjectSourceStateStaysHidden — состояние ЧУЖОГО источника не
// просвечивает: неготовый чужой том обязан отдавать тот же "not found", что и
// несуществующий, иначе по тексту отличают «чужое есть, но не готово» от «нет».
func TestImageForeignProjectSourceStateStaysHidden(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	bindingID := imgFixtureClass(t, pool, "region-1-a")
	victim := imgFixtureVolumeRow(t, pool, "prj-victim", "victim-vol", "region-1-a", bindingID, 20<<30)
	_, err := pool.Exec(ctx, `UPDATE volumes SET state='CREATING' WHERE id=$1`, victim)
	require.NoError(t, err)

	_, _, foreignErr := ir.Insert(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-attacker", Name: "steal-img",
		RegionID: imgFixtureRegion, SourceVolume: victim,
	}, fixtureRegionZones)
	require.True(t, stderrors.Is(foreignErr, storageerr.ErrFailedPrecondition), "got %v", foreignErr)
	require.Equal(t, "Volume "+victim+" not found", foreignErr.Error()[len("failed precondition: "):],
		"состояние чужого тома не должно просвечивать отдельным текстом")
}

// TestImageStatusReasonPersisted — причина состояния и читается, и пишется.
//
// Домен несёт причину; если бы вставка её выбрасывала, получилось бы принято-и-
// проигнорировано: вызывающий уверен, что записал, а поля нет. Пара полюсов:
// названная причина возвращается, неназванная остаётся пустой.
func TestImageStatusReasonPersisted(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-reason", 1<<30)

	withReason := ids.NewID(domain.PrefixImage)
	_, _, err := ir.Insert(ctx, &domain.Image{
		ID: withReason, ProjectID: "prj-1", Name: "img-with-reason", RegionID: imgFixtureRegion,
		SourceSnapshot: snapID, StatusReason: domain.ReasonSourceNotReady,
	}, fixtureRegionZones)
	require.NoError(t, err)
	got, err := ir.Get(ctx, withReason)
	require.NoError(t, err)
	require.Equal(t, domain.ReasonSourceNotReady, got.StatusReason)

	without := ids.NewID(domain.PrefixImage)
	_, _, err = ir.Insert(ctx, &domain.Image{
		ID: without, ProjectID: "prj-1", Name: "img-without-reason", RegionID: imgFixtureRegion,
		SourceSnapshot: snapID,
	}, fixtureRegionZones)
	require.NoError(t, err)
	got, err = ir.Get(ctx, without)
	require.NoError(t, err)
	require.Equal(t, domain.ReasonNone, got.StatusReason, "причины нет — поле пусто, а не выдумано")
}

// TestImageSeededVolumesListed — чтение отдаёт тома, засеянные этим образом.
//
// Список нужен арендатору ДО удаления: удаление образа проходит и очищает
// происхождение засеянных томов, поэтому без списка он стирает связь, не видя, у
// скольких томов. Полюса: два тома — оба в списке; том удалён — остаётся один;
// чужой образ в списке не отражается.
func TestImageSeededVolumesListed(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	bindingID := imgFixtureClass(t, pool, "region-1-a")
	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-seeded", 20<<30)
	imgID := ids.NewID(domain.PrefixImage)
	_, _, err := ir.Insert(ctx, &domain.Image{
		ID: imgID, ProjectID: "prj-1", Name: "seed-img", RegionID: imgFixtureRegion,
		SourceSnapshot: snapID,
	}, fixtureRegionZones)
	require.NoError(t, err)

	got, err := ir.Get(ctx, imgID)
	require.NoError(t, err)
	require.Empty(t, got.SeededVolumeIDs, "пока никто не засеян — список пуст, а не выдуман")

	first := imgFixtureVolumeRow(t, pool, "prj-1", "seeded-1", "region-1-a", bindingID, 20<<30)
	second := imgFixtureVolumeRow(t, pool, "prj-1", "seeded-2", "region-1-a", bindingID, 20<<30)
	other := imgFixtureVolumeRow(t, pool, "prj-1", "not-seeded", "region-1-a", bindingID, 20<<30)
	_, err = pool.Exec(ctx, `UPDATE volumes SET source_image_id=$1 WHERE id = ANY($2::text[])`,
		imgID, []string{first, second})
	require.NoError(t, err)

	got, err = ir.Get(ctx, imgID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{first, second}, got.SeededVolumeIDs)
	require.NotContains(t, got.SeededVolumeIDs, other, "том без этого источника в списке не появляется")

	page, _, err := ir.List(ctx, image.Pagination{PageSize: 50, ProjectID: "prj-1"})
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.ElementsMatch(t, []string{first, second}, page[0].SeededVolumeIDs,
		"список отдаёт то же, что и чтение по идентификатору")

	_, err = pool.Exec(ctx, `DELETE FROM volumes WHERE id=$1`, second)
	require.NoError(t, err)
	got, err = ir.Get(ctx, imgID)
	require.NoError(t, err)
	require.Equal(t, []string{first}, got.SeededVolumeIDs, "удалённый том уходит из списка")
}

// TestImageRegisterBornReady — регистрация вносит образ, объект которого у бэкенда
// УЖЕ существует, поэтому он рождается готовым: заставлять сверщик «создавать»
// существующее значило бы поручить ему создать то, что есть.
//
// Полюс рядом: обычный захват из источника рождается CREATING (проба выше) — две
// формы рождения различаются, и это видно.
func TestImageRegisterBornReady(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	imgID := ids.NewID(domain.PrefixImage)
	img, regs, err := ir.Register(ctx, &domain.Image{
		ID: imgID, ProjectID: "prj-1", Name: "ubuntu-24-04", RegionID: imgFixtureRegion,
		SizeBytes: 21474836480, MinDiskBytes: 21474836480,
		Backend: domain.Placement{BackendObject: "kc7f-img-ubuntu-2404-20260812"},
	})
	require.NoError(t, err)
	require.Equal(t, domain.ImageStatusReady, img.Status)
	require.Equal(t, domain.ObservedReady, img.Observation.State)
	require.NotEmpty(t, regs, "владение регистрируется той же транзакцией, что и строка")

	got, err := ir.Get(ctx, imgID)
	require.NoError(t, err)
	require.Equal(t, domain.ImageStatusReady, got.Status)
	require.Empty(t, got.SourceSnapshot, "у зарегистрированного образа нет источника внутри облака")
	require.Empty(t, got.SourceVolume)
	require.EqualValues(t, 21474836480, got.SizeBytes, "размер называет регистрирующий")

	internal, err := ir.GetInternal(ctx, imgID)
	require.NoError(t, err)
	require.Equal(t, "kc7f-img-ubuntu-2404-20260812", internal.Backend.BackendObject,
		"имя объекта приходит извне и остаётся собой")
	require.Equal(t, domain.ObservedReady, internal.Observation.State)
	require.False(t, internal.Observation.At.IsZero(),
		"момент наблюдения назван: READY без времени наблюдения неотличим от «не смотрели»")
}

// TestImageRegisterDuplicateBackendObjectRejected — один объект хранилища — один
// образ. Повторная регистрация того же имени отвергается частичной уникальностью, а
// не проверкой в коде: две конкурентные регистрации иначе обе прошли бы чтение.
//
// Зеркало: другое имя объекта регистрируется — отказ выше про совпадение, а не про
// «второй раз вообще нельзя».
func TestImageRegisterDuplicateBackendObjectRejected(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	const object = "kc7f-img-ubuntu-2404-20260812"
	_, _, err := ir.Register(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "ubuntu-first",
		RegionID: imgFixtureRegion, SizeBytes: 1 << 30, MinDiskBytes: 1 << 30,
		Backend: domain.Placement{BackendObject: object},
	})
	require.NoError(t, err)

	_, _, err = ir.Register(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "ubuntu-second",
		RegionID: imgFixtureRegion, SizeBytes: 1 << 30, MinDiskBytes: 1 << 30,
		Backend: domain.Placement{BackendObject: object},
	})
	require.True(t, stderrors.Is(err, storageerr.ErrAlreadyExists), "got %v", err)
	require.Equal(t, "image with backend object "+object+" already exists",
		err.Error()[len("already exists: "):])

	// Тот же объект в ДРУГОМ проекте — тоже отказ: объект один, и принадлежать двум
	// образам он не может.
	_, _, err = ir.Register(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-2", Name: "ubuntu-other-project",
		RegionID: imgFixtureRegion, SizeBytes: 1 << 30, MinDiskBytes: 1 << 30,
		Backend: domain.Placement{BackendObject: object},
	})
	require.True(t, stderrors.Is(err, storageerr.ErrAlreadyExists), "got %v", err)

	// Зеркало: другое имя объекта проходит.
	_, _, err = ir.Register(ctx, &domain.Image{
		ID: ids.NewID(domain.PrefixImage), ProjectID: "prj-1", Name: "ubuntu-third",
		RegionID: imgFixtureRegion, SizeBytes: 1 << 30, MinDiskBytes: 1 << 30,
		Backend: domain.Placement{BackendObject: object + "-2"},
	})
	require.NoError(t, err)
}

// TestVolumeSeedFromNotReadyImageRejected — образ, объекта которого у бэкенда ещё
// нет, тома не засевает: иначе арендатор получил бы том, чьи байты неоткуда взять.
//
// Проба ведёт весь путь через репозиторий тома, потому что предмет — именно связь
// «образ рождается CREATING» → «засев отвергается». Зеркало рядом: тот же образ,
// доведённый сверщиком до готовности, засевает том.
func TestVolumeSeedFromNotReadyImageRejected(t *testing.T) {
	pool := newTestPool(t)
	ir := pg.NewImageRepo(pool)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	imgFixtureClass(t, pool, "region-1-a")
	snapID := mkSnapshotRow(t, pool, "prj-1", "snap-boot", 20<<30)
	imgID := ids.NewID(domain.PrefixImage)
	_, _, err := ir.Insert(ctx, &domain.Image{
		ID: imgID, ProjectID: "prj-1", Name: "boot-img", RegionID: imgFixtureRegion,
		SourceSnapshot: snapID,
		Backend:        domain.Placement{BackendObject: "kctest-" + imgID},
	}, fixtureRegionZones)
	require.NoError(t, err)

	_, _, err = vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "boot-from-creating",
		ZoneID: "region-1-a", DiskTypeID: imgFixtureDiskType, SizeBytes: 20 << 30,
		SourceImage: imgID,
	}, imgFixtureRegion)
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Image "+imgID+" is not ready", err.Error()[len("failed precondition: "):])

	// Зеркало: сверщик увидел объект и объявил образ готовым — засев проходит.
	_, err = pool.Exec(ctx, `UPDATE images SET state='READY', observed_state='READY' WHERE id=$1`, imgID)
	require.NoError(t, err)
	boot, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "boot-from-ready",
		ZoneID: "region-1-a", DiskTypeID: imgFixtureDiskType, SizeBytes: 20 << 30,
		SourceImage: imgID,
	}, imgFixtureRegion)
	require.NoError(t, err, "готовый образ засевает том")
	require.Equal(t, imgID, boot.SourceImage)
}
