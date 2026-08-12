// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// Размещение и происхождение снимка.
//
// Предмет этого файла — то, чем снимок ОБЛАДАЕТ САМ, а не то, что он способен
// добрать через свой том. Ссылка на том обнуляется при его удалении (снимок обязан
// переживать источник), поэтому всякое свойство, добираемое через неё, у пережившего
// снимка исчезает — и проверка, на него опирающаяся, становится тождественно
// истинной ровно в том случае, ради которого писалась.

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// Зоны фикстур. Их ДВЕ намеренно: с одной зоной всякая проверка когерентности
// проходит by construction, и «зона унаследована» было бы неотличимо от «зона
// захардкожена».
const (
	snapZoneA = "ru-central1-a"
	snapZoneB = "ru-central1-b"
)

// snapClass — класс диска фикстур снимка. Свой, а не общий: каталог посевом больше
// не заводится, и проба обязана регистрировать ровно то, чем пользуется, — иначе она
// зависит от чужой фикстуры и краснеет от чужой правки.
const snapClass = "block-snapshot"

// snapNamespaceTemplate — шаблон единицы изоляции арендатора у бэкенда. Не пустой
// намеренно: на пустом шаблоне подстановка вырождается в сам идентификатор проекта,
// и «пространство выведено из шаблона привязки» зеленело бы на реализации, которая
// шаблон не читает вовсе.
const snapNamespaceTemplate = "ns-{projectId}"

// snapInstallPrefix — префикс установки проб. Совпадать с боевым не обязан: он
// свойство РАЗВЁРТЫВАНИЯ, и проба обязана видеть его в имени объекта, а не угадывать.
const snapInstallPrefix = "kctest"

// snapSeedPlacement регистрирует класс диска, бэкенд и ДЕЙСТВУЮЩУЮ ревизию привязки
// на пару (класс, зона) и возвращает идентификатор ревизии.
//
// Каталог не сеется миграцией: класс — регистрация того, что реально даёт провайдер,
// поэтому пустой каталог законен, а фикстура обязана завести его сама. Вставки
// идемпотентны: один тест регистрирует один класс в двух зонах, и повторная
// регистрация класса не должна быть ошибкой пробы.
func snapSeedPlacement(t *testing.T, pool *pgxpool.Pool, diskTypeID, zone string) string {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO disk_types (id, name, zone_ids, performance_tier, lifecycle)
		 VALUES ($1, $1, '[]'::jsonb, 'BALANCED', 'ACTIVE')
		 ON CONFLICT (id) DO NOTHING`, diskTypeID)
	require.NoError(t, err)

	backendID := "sb-" + diskTypeID
	_, err = pool.Exec(ctx,
		`INSERT INTO storage_backends (id, name, kind, endpoint, credentials_ref)
		 VALUES ($1, $1, 'CEPH_RBD', 'cfg://ceph/' || $1, 'vault://kacho/storage/' || $1)
		 ON CONFLICT (id) DO NOTHING`, backendID)
	require.NoError(t, err)

	bindingID := fmt.Sprintf("dtb-%s-%s", diskTypeID, zone)
	_, err = pool.Exec(ctx,
		`INSERT INTO disk_type_bindings
		   (id, disk_type_id, zone_id, backend_id, revision, pool, namespace_template, status,
		    cap_snapshots, cap_clone_from_snapshot, cap_clone_from_image)
		 VALUES ($1,$2,$3,$4,1,'kacho-block',$5,'ACTIVE',true,true,true)
		 ON CONFLICT DO NOTHING`,
		bindingID, diskTypeID, zone, backendID, snapNamespaceTemplate)
	require.NoError(t, err)
	return bindingID
}

// snapVolume вставляет том в НАЗВАННОЙ зоне и оставляет его таким, каким он
// рождается, — создаваемым.
//
// Имя объекта у бэкенда задаётся так же, как это делает use-case: пустое имя
// уникальный индекс считает ЗНАЧЕНИЕМ, поэтому два безымянных тома в одной базе
// столкнулись бы друг с другом, и проба падала бы «уже существует» на втором томе.
func snapVolume(t *testing.T, vr *pg.VolumeRepo, project, name, zone string, size int64) *domain.Volume {
	t.Helper()
	id := ids.NewID(domain.PrefixVolume)
	v, _, err := vr.Insert(context.Background(), &domain.Volume{
		ID:         id,
		ProjectID:  project,
		Name:       name,
		ZoneID:     zone,
		DiskTypeID: snapClass,
		SizeBytes:  size,
		Backend:    domain.Placement{BackendObject: blockbackend.ObjectName(snapInstallPrefix, id)},
	}, "")
	require.NoError(t, err)
	return v
}

// snapVolumeFrom вставляет том, засеянный названным снимком, и возвращает исход как
// есть: отрицательные пробы утверждают именно его. Имя объекта у бэкенда задаётся по
// тем же соображениям, что и в snapVolume.
func snapVolumeFrom(t *testing.T, vr *pg.VolumeRepo, project, name, zone string, size int64, snapshotID string) (*domain.Volume, error) {
	t.Helper()
	id := ids.NewID(domain.PrefixVolume)
	v, _, err := vr.Insert(context.Background(), &domain.Volume{
		ID: id, ProjectID: project, Name: name, ZoneID: zone, DiskTypeID: snapClass,
		SizeBytes: size, SourceSnapshot: snapshotID,
		Backend: domain.Placement{BackendObject: blockbackend.ObjectName(snapInstallPrefix, id)},
	}, "")
	return v, err
}

// snapReadyVolume вставляет том и объявляет его готовым.
//
// Готовность объявляет СВЕРЩИК, увидев объект у бэкенда: операция фиксирует
// намерение, а исход провижининга несёт статус ресурса. В пробе его роль исполняет
// прямое обновление строки — иначе проба про снимок ждала бы компонента, к предмету
// которого она не относится.
func snapReadyVolume(t *testing.T, pool *pgxpool.Pool, vr *pg.VolumeRepo, project, name, zone string, size int64) *domain.Volume {
	t.Helper()
	v := snapVolume(t, vr, project, name, zone, size)
	snapMarkVolumeReady(t, pool, v.ID)
	return v
}

// snapMarkVolumeReady исполняет за сверщика единственное, что нужно пробам про
// снимок: объявляет том готовым.
func snapMarkVolumeReady(t *testing.T, pool *pgxpool.Pool, volumeID string) {
	t.Helper()
	tag, err := pool.Exec(context.Background(), `UPDATE volumes SET state='READY' WHERE id=$1`, volumeID)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected(), "фикстура обязана менять ровно одну строку")
}

// snapMarkSnapshotReady — то же для снимка.
func snapMarkSnapshotReady(t *testing.T, pool *pgxpool.Pool, snapshotID string) {
	t.Helper()
	tag, err := pool.Exec(context.Background(),
		`UPDATE snapshots SET state='READY', observed_state='READY', observed_at=now() WHERE id=$1`, snapshotID)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected(), "фикстура обязана менять ровно одну строку")
}

// snapInsert снимает снимок с тома через репозиторий, выводя имя объекта так же,
// как это делает use-case: из НЕИЗМЕНЯЕМОГО идентификатора снимка.
func snapInsert(t *testing.T, sr *pg.SnapshotRepo, project, name, srcVolume string) *domain.Snapshot {
	t.Helper()
	id := ids.NewID(domain.PrefixSnapshot)
	s, _, err := sr.Insert(context.Background(), &domain.Snapshot{
		ID:             id,
		ProjectID:      project,
		Name:           name,
		SourceVolumeID: srcVolume,
		Backend:        domain.Placement{BackendObject: blockbackend.SnapshotObjectName(snapInstallPrefix, id)},
	})
	require.NoError(t, err)
	return s
}

// TestSnapshotInheritsZoneOfSourceVolume (STOR-P-36) — снимок получает СВОЮ зону от
// тома-источника тем же стейтментом, что и вставку.
//
// Зона берётся у тома ВНУТРИ вставки, а не вторым запросом: между чтением и записью
// том может смениться, и снимок унаследовал бы размещение, которого у источника уже
// нет. Две зоны в пробе — контроль: на одной «унаследована» неотличимо от «записана
// константой».
func TestSnapshotInheritsZoneOfSourceVolume(t *testing.T) {
	pool := newTestPool(t)
	vr, sr := pg.NewVolumeRepo(pool), pg.NewSnapshotRepo(pool)
	ctx := context.Background()
	snapSeedPlacement(t, pool, snapClass, snapZoneA)
	snapSeedPlacement(t, pool, snapClass, snapZoneB)

	volA := snapReadyVolume(t, pool, vr, "prj-1", "vol-a", snapZoneA, 1<<30)
	volB := snapReadyVolume(t, pool, vr, "prj-1", "vol-b", snapZoneB, 1<<30)

	snapA := snapInsert(t, sr, "prj-1", "snap-from-a", volA.ID)
	snapB := snapInsert(t, sr, "prj-1", "snap-from-b", volB.ID)
	require.Equal(t, snapZoneA, snapA.ZoneID, "зона снята с тома-источника")
	require.Equal(t, snapZoneB, snapB.ZoneID, "зона следует за источником, а не за константой")

	gotA, err := sr.Get(ctx, snapA.ID)
	require.NoError(t, err)
	require.Equal(t, snapZoneA, gotA.ZoneID, "зона читается обратно")
	gotB, err := sr.Get(ctx, snapB.ID)
	require.NoError(t, err)
	require.Equal(t, snapZoneB, gotB.ZoneID)
}

// TestSnapshotKeepsOwnZoneAfterSourceVolumeDeleted (STOR-P-37, РЕГРЕССИЯ КЛАССА) —
// снимок переживает свой том, сохраняя СОБСТВЕННОЕ размещение, и восстановление в
// чужую зону остаётся отвергнутым.
//
// Прежде зона добиралась через том, поэтому после его удаления сравнивать было не с
// чем и проверка проходила всегда — то есть тихий перенос блочных данных через
// границу размещения делался в один лишний шаг: снять снимок, удалить том,
// восстановить куда угодно.
func TestSnapshotKeepsOwnZoneAfterSourceVolumeDeleted(t *testing.T) {
	pool := newTestPool(t)
	vr, sr := pg.NewVolumeRepo(pool), pg.NewSnapshotRepo(pool)
	ctx := context.Background()
	snapSeedPlacement(t, pool, snapClass, snapZoneA)
	snapSeedPlacement(t, pool, snapClass, snapZoneB)

	src := snapReadyVolume(t, pool, vr, "prj-1", "vol-src", snapZoneB, 2<<30)
	snap := snapInsert(t, sr, "prj-1", "snap-orphan", src.ID)
	snapMarkSnapshotReady(t, pool, snap.ID)
	require.NoError(t, vr.Delete(ctx, src.ID))

	orphan, err := sr.Get(ctx, snap.ID)
	require.NoError(t, err, "снимок переживает свой том")
	require.Empty(t, orphan.SourceVolumeID, "происхождение обнулено (FK SET NULL)")
	require.Equal(t, snapZoneB, orphan.ZoneID, "размещение осталось СВОИМ, а не исчезло вместе с томом")

	// Отрицание: засев в чужую зону отвергается уже ПОСЛЕ потери источника.
	_, err = snapVolumeFrom(t, vr, "prj-1", "vol-wrong-zone", snapZoneA, 2<<30, snap.ID)
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Volume and Snapshot must be in the same zone",
		err.Error()[len("failed precondition: "):])

	// Положительный контроль: в СВОЮ зону тот же осиротевший снимок засевается.
	ok, err := snapVolumeFrom(t, vr, "prj-1", "vol-right-zone", snapZoneB, 2<<30, snap.ID)
	require.NoError(t, err, "отказ выше — про зону, а не про потерянный источник")
	require.Equal(t, snap.ID, ok.SourceSnapshot)
}

// TestSnapshotSeedingRefusedUntilReady (STOR-P-43) — засев тома из НЕГОТОВОГО снимка
// отвергается, и текст отказа — контрактный.
//
// Снимок рождается создаваемым: готовность объявляет сверщик, увидев объект у
// бэкенда. Пропустить засев из создаваемого снимка значит склонировать объект,
// которого ещё нет, и получить том, чьё содержимое не определено ничем.
func TestSnapshotSeedingRefusedUntilReady(t *testing.T) {
	pool := newTestPool(t)
	vr, sr := pg.NewVolumeRepo(pool), pg.NewSnapshotRepo(pool)
	snapSeedPlacement(t, pool, snapClass, snapZoneA)

	src := snapReadyVolume(t, pool, vr, "prj-1", "vol-src", snapZoneA, 3<<30)
	snap := snapInsert(t, sr, "prj-1", "snap-fresh", src.ID)
	require.Equal(t, domain.SnapshotStatusCreating, snap.Status,
		"снимок рождается СОЗДАВАЕМЫМ: готовность объявляет сверщик")

	_, err := snapVolumeFrom(t, vr, "prj-1", "vol-too-early", snapZoneA, 3<<30, snap.ID)
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, fmt.Sprintf("Snapshot %s is not ready", snap.ID),
		err.Error()[len("failed precondition: "):])

	// Зеркальная проба: тот же снимок в READY засевается.
	snapMarkSnapshotReady(t, pool, snap.ID)
	ok, err := snapVolumeFrom(t, vr, "prj-1", "vol-after-ready", snapZoneA, 3<<30, snap.ID)
	require.NoError(t, err, "отказ выше — про готовность, а не про засев вообще")
	require.Equal(t, snap.ID, ok.SourceSnapshot)
}

// TestSnapshotSeededVolumesAreListed (STOR-P-40) — снимок называет тома, которые из
// него засеяны.
//
// Список нужен арендатору ДО удаления: если бэкенд объявил зависимость клона от
// родителя, удаление источника с живыми детьми отвергается, и узнавать их состав
// отказом — значит узнавать его последним.
func TestSnapshotSeededVolumesAreListed(t *testing.T) {
	pool := newTestPool(t)
	vr, sr := pg.NewVolumeRepo(pool), pg.NewSnapshotRepo(pool)
	ctx := context.Background()
	snapSeedPlacement(t, pool, snapClass, snapZoneA)

	src := snapReadyVolume(t, pool, vr, "prj-1", "vol-src", snapZoneA, 4<<30)
	snap := snapInsert(t, sr, "prj-1", "snap-seed", src.ID)
	snapMarkSnapshotReady(t, pool, snap.ID)

	fresh, err := sr.Get(ctx, snap.ID)
	require.NoError(t, err)
	require.Empty(t, fresh.SeededVolumeIDs, "никто ещё не засеян — пусто, а не «что-нибудь»")

	seeded := make([]string, 0, 2)
	for _, name := range []string{"vol-child-1", "vol-child-2"} {
		v, ierr := snapVolumeFrom(t, vr, "prj-1", name, snapZoneA, 4<<30, snap.ID)
		require.NoError(t, ierr)
		seeded = append(seeded, v.ID)
	}

	got, err := sr.Get(ctx, snap.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, seeded, got.SeededVolumeIDs, "оба засеянных тома названы")

	require.NoError(t, vr.Delete(ctx, seeded[0]))
	after, err := sr.Get(ctx, snap.ID)
	require.NoError(t, err)
	require.Equal(t, []string{seeded[1]}, after.SeededVolumeIDs, "удалённый ребёнок исчезает из перечня")
}

// TestSnapshotListSeedsWithoutPerRowQuery (STOR-P-40) — перечень засеянных томов на
// СПИСКЕ стоит одну страницу, а не строку.
//
// Утверждение — на числе запросов, а не на форме кода: форма читается глазами и
// переживает любую правку, число — нет. Дополнительный запрос на строку превратил бы
// страницу в 1+N обращений, и цена росла бы с размером страницы, которая по контракту
// доходит до тысячи.
func TestSnapshotListSeedsWithoutPerRowQuery(t *testing.T) {
	pool, counter := dtTracedPool(t)
	vr, sr := pg.NewVolumeRepo(pool), pg.NewSnapshotRepo(pool)
	ctx := context.Background()
	snapSeedPlacement(t, pool, snapClass, snapZoneA)

	src := snapReadyVolume(t, pool, vr, "prj-1", "vol-src", snapZoneA, 1<<30)
	for i := 0; i < 3; i++ {
		s := snapInsert(t, sr, "prj-1", fmt.Sprintf("snap-%d", i), src.ID)
		snapMarkSnapshotReady(t, pool, s.ID)
		_, err := snapVolumeFrom(t, vr, "prj-1", fmt.Sprintf("vol-child-%d", i), snapZoneA, 1<<30, s.ID)
		require.NoError(t, err)
	}

	before := counter.n.Load()
	page, _, err := sr.List(ctx, snapshot.Pagination{PageSize: 50, ProjectID: "prj-1"})
	require.NoError(t, err)
	require.Len(t, page, 3)
	for _, s := range page {
		require.Len(t, s.SeededVolumeIDs, 1, "перечень заполнен на каждой строке страницы")
	}
	require.EqualValues(t, 1, counter.n.Load()-before, "страница стоит ОДИН запрос")
}

// TestSnapshotInheritsBindingAndTenantSpace — снимок наследует ревизию политики от
// тома-источника, а имя объекта выводит из СВОЕГО идентификатора.
//
// Ревизия неизменяема, поэтому ссылка на неё эквивалентна копии политики и не может
// «уехать»: правка класса не меняет задним числом свойства уже снятого снимка. Имя
// объекта производно от снимка, а не от тома: том удаляется раньше, и производное от
// него имя пережило бы свой источник.
//
// Единица изоляции арендатора не хранится отдельной колонкой, а ВЫВОДИТСЯ из
// унаследованной ревизии и собственного проекта — обе величины на снимке неизменяемы,
// поэтому вывод даёт ровно то же пространство, что у тома-источника, и разойтись с
// ним не может by construction.
func TestSnapshotInheritsBindingAndTenantSpace(t *testing.T) {
	pool := newTestPool(t)
	vr, sr := pg.NewVolumeRepo(pool), pg.NewSnapshotRepo(pool)
	ctx := context.Background()
	bindingA := snapSeedPlacement(t, pool, snapClass, snapZoneA)
	bindingB := snapSeedPlacement(t, pool, snapClass, snapZoneB)
	require.NotEqual(t, bindingA, bindingB, "ревизии зон различимы — иначе наследование не проверяемо")

	volB := snapReadyVolume(t, pool, vr, "prj-1", "vol-b", snapZoneB, 5<<30)
	snap := snapInsert(t, sr, "prj-1", "snap-b", volB.ID)

	got, err := sr.Get(ctx, snap.ID)
	require.NoError(t, err)
	require.Equal(t, bindingB, got.Backend.BindingID, "ревизия унаследована от тома, а не выбрана заново")
	require.Equal(t, snapInstallPrefix+"-"+snap.ID, got.Backend.BackendObject,
		"имя объекта выведено из идентификатора СНИМКА")
	require.Equal(t, "ns-prj-1", got.Backend.BackendNamespace,
		"пространство арендатора выведено из шаблона унаследованной ревизии")

	// Контроль: у тома-источника ровно та же ревизия — наследование, а не совпадение.
	var volBinding string
	require.NoError(t, pool.QueryRow(ctx, `SELECT binding_id FROM volumes WHERE id=$1`, volB.ID).Scan(&volBinding))
	require.Equal(t, volBinding, got.Backend.BindingID)
}

// TestSnapshotStatusReasonRoundTrip (STOR-P-24) — причина состояния ходит в обе
// стороны.
//
// Колонка, которую только пишут, невидима арендатору; колонка, которую только
// читают, всегда пуста. Оба вырождения выглядят как работающая причина.
func TestSnapshotStatusReasonRoundTrip(t *testing.T) {
	pool := newTestPool(t)
	vr, sr := pg.NewVolumeRepo(pool), pg.NewSnapshotRepo(pool)
	ctx := context.Background()
	snapSeedPlacement(t, pool, snapClass, snapZoneA)
	src := snapReadyVolume(t, pool, vr, "prj-1", "vol-src", snapZoneA, 1<<30)

	// Штатное создание причины не несёт — положительный контроль к паре ниже.
	plain := snapInsert(t, sr, "prj-1", "snap-plain", src.ID)
	gotPlain, err := sr.Get(ctx, plain.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ReasonNone, gotPlain.StatusReason, "у штатного снимка причины нет")

	// Причина, названная доменом, доезжает до строки и читается обратно.
	id := ids.NewID(domain.PrefixSnapshot)
	withReason, _, err := sr.Insert(ctx, &domain.Snapshot{
		ID: id, ProjectID: "prj-1", Name: "snap-reasoned", SourceVolumeID: src.ID,
		StatusReason: domain.ReasonSourceNotReady,
		Backend:      domain.Placement{BackendObject: blockbackend.SnapshotObjectName(snapInstallPrefix, id)},
	})
	require.NoError(t, err)
	require.Equal(t, domain.ReasonSourceNotReady, withReason.StatusReason)
	gotReason, err := sr.Get(ctx, withReason.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ReasonSourceNotReady, gotReason.StatusReason, "причина прочитана из строки")

	// Причина, записанная сверщиком (его роль исполняет прямое обновление), тоже видна.
	_, err = pool.Exec(ctx, `UPDATE snapshots SET state='ERROR', status_reason=$2 WHERE id=$1`,
		plain.ID, string(domain.ReasonBackendUnavailable))
	require.NoError(t, err)
	afterDrift, err := sr.Get(ctx, plain.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ReasonBackendUnavailable, afterDrift.StatusReason)
	require.Equal(t, domain.SnapshotStatusError, afterDrift.Status)
}
