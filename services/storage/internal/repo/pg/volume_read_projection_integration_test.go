// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// volume_read_projection_integration_test.go — проекция ЧТЕНИЯ тома: что `Get` и
// `List` отдают арендатору.
//
// Предмет обеих задач один — НЕДОСТАТОЧНАЯ ПРОЕКЦИЯ: чтение отдаёт меньше, чем
// хранит строка, и молчит об этом. У задачи `#1559` недобирается перечень привязок
// (соединение без агрегации), у задачи `#1557` — две колонки самой строки.
//
// Пробы лежат в одном файле намеренно: они утверждают одно свойство с разных
// сторон — «ответ чтения равен состоянию строки», — и разводить их по файлам
// значило бы прятать, что чинятся они одной проекцией.

// attachTo вставляет строку привязки НАПРЯМУЮ, минуя CAS.
//
// Предмет здесь — ЧТЕНИЕ, а не запись, и оно обязано отвечать по строкам, которые
// схема допускает: ключ привязки — пара `(volume_id, instance_id)` (миграция 0018),
// поэтому вторая строка законна независимо от того, какой путь её положил.
// Достижимость БОЕВЫМ путём утверждается отдельно —
// `TestSecondAttachmentIsReachableThroughTheProductPath` ниже.
func attachTo(t *testing.T, pool *pgxpool.Pool, volumeID, instanceID, instanceName, device string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO volume_attachments
		   (volume_id, instance_id, instance_name, project_id, zone_id, device_name, auto_delete)
		 VALUES ($1,$2,$3,'prj-1','region-1-a',$4,false)`,
		volumeID, instanceID, instanceName, device)
	require.NoError(t, err)
}

// instanceIDsOf — состав перечня привязок ПО СОСТАВУ, а не по индексу.
//
// Контракт объявляет `attachments` НАБОРОМ (`storage/v1/volume.proto`), поэтому
// проба обязана сверять его тем же способом, каким это предписано клиенту. Сверка
// по индексу закрепила бы порядок, которого контракт не обещает.
func instanceIDsOf(v *domain.Volume) map[string]bool {
	got := make(map[string]bool, len(v.Attachments))
	for i := range v.Attachments {
		got[v.Attachments[i].InstanceID] = true
	}
	return got
}

// TestGetReturnsEveryAttachmentOfTheVolume — `Get` отдаёт ВСЕ привязки тома.
//
// До починки запрос чтения соединял привязки без агрегации, а `Get` брал строку
// через `QueryRow` — тот читает ПЕРВУЮ строку соединения и остальные отбрасывает.
// Порядка в запросе не было вовсе, поэтому какая именно привязка доедет до
// арендатора, не было определено ничем.
//
// Утверждается ПАРА: и число привязок, и их состав. Одно число прошло бы на любых
// двух строках, включая две копии одной.
func TestGetReturnsEveryAttachmentOfTheVolume(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v := mkVolume(t, pool, r, "prj-1", "vol-multi-get", 10<<30)

	attachTo(t, pool, v.ID, "epd00000000000000011", "web-1", "sdb")
	attachTo(t, pool, v.ID, "epd00000000000000012", "web-2", "sdc")

	got, err := r.Get(ctx, v.ID)
	require.NoError(t, err)
	require.Len(t, got.Attachments, 2,
		"том привязан к двум инстансам, а чтение отдало %d: соединение без агрегации "+
			"даёт по строке на привязку, и QueryRow берёт первую попавшуюся",
		len(got.Attachments))
	require.Equal(t,
		map[string]bool{"epd00000000000000011": true, "epd00000000000000012": true},
		instanceIDsOf(got), "состав привязок разошёлся с тем, что лежит в базе")
	require.Equal(t, domain.VolumeStatusInUse, got.Status)

	// Положительный контроль в обратную сторону: том без привязок отдаётся пустым,
	// а не с выдуманной строкой. Без него утверждение выше зеленело бы на
	// реализации, которая приписывает привязки каждому тому.
	clean := mkVolume(t, pool, r, "prj-1", "vol-multi-get-clean", 10<<30)
	empty, err := r.Get(ctx, clean.ID)
	require.NoError(t, err)
	require.Empty(t, empty.Attachments)
	require.Equal(t, domain.VolumeStatusAvailable, empty.Status)
}

// TestListDoesNotDuplicateAMultiplyAttachedVolume — `List` кладёт том в страницу
// ОДИН раз, а предел считает по ТОМАМ, а не по строкам соединения.
//
// До починки том с двумя привязками попадал в страницу ДВАЖДЫ, `LIMIT` резал
// строки соединения, и курсор `(created_at, id)` указывал не на следующий том, а на
// следующую строку соединения — то есть на тот же том. Клиент, ведущий состояние,
// прочитал бы дубль как два разных тома.
//
// Проба идёт по СТРАНИЦАМ размера 1: именно так дефект и проявляется — при
// достаточно большой странице дубль виден, но курсор ещё нет.
func TestListDoesNotDuplicateAMultiplyAttachedVolume(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	// Первый том — многопривязочный, второй — без привязок. Порядок обхода задан
	// курсором `(created_at, id)`, поэтому многопривязочный обходится первым.
	multi := mkVolume(t, pool, r, "prj-list-dup", "vol-dup-1", 10<<30)
	attachTo(t, pool, multi.ID, "epd00000000000000021", "web-1", "sdb")
	attachTo(t, pool, multi.ID, "epd00000000000000022", "web-2", "sdc")
	plain := mkVolume(t, pool, r, "prj-list-dup", "vol-dup-2", 10<<30)

	// Первая страница: РОВНО один том, и это многопривязочный — со всеми привязками.
	page, next, err := r.List(ctx, volume.Pagination{PageSize: 1, ProjectID: "prj-list-dup"})
	require.NoError(t, err)
	require.Len(t, page, 1,
		"страница размера 1 отдала %d элементов: предел режет строки соединения, а не тома",
		len(page))
	require.Equal(t, multi.ID, page[0].ID)
	require.Len(t, page[0].Attachments, 2,
		"список отдал том с %d привязками вместо двух", len(page[0].Attachments))
	require.NotEmpty(t, next, "второй том остался за страницей — курсор обязан быть")

	// Вторая страница: курсор указывает на СЛЕДУЮЩИЙ ТОМ, а не на ту же строку.
	page2, next2, err := r.List(ctx,
		volume.Pagination{PageSize: 1, ProjectID: "prj-list-dup", PageToken: next})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	require.Equal(t, plain.ID, page2[0].ID,
		"курсор первой страницы указал не на следующий ТОМ: он был посчитан по строке "+
			"соединения, поэтому многопривязочный том пришёл бы второй раз")
	require.Empty(t, next2, "томов было два — третьей страницы быть не должно")

	// Перепись всего листинга одной страницей: дублей нет ни одного.
	all, _, err := r.List(ctx, volume.Pagination{PageSize: 50, ProjectID: "prj-list-dup"})
	require.NoError(t, err)
	seen := map[string]int{}
	for _, v := range all {
		seen[v.ID]++
	}
	require.Equal(t, map[string]int{multi.ID: 1, plain.ID: 1}, seen,
		"перепись листинга: том обязан встретиться РОВНО один раз")
}

// TestSecondAttachmentIsReachableThroughTheProductPath — вторая привязка достижима
// БОЕВЫМ путём, а не только прямой вставкой.
//
// Проба существует затем, чтобы дефект чтения не читался как теоретический. Путь
// открывает не гонка, а штатная настройка каталога: предикат вставки привязки
// разрешает вторую строку, когда действующая ревизия привязки класса объявляет
// способность множественной привязки (миграция 0018 объявляет это свойством
// БЭКЕНДА, а не нашей схемы).
//
// Второй полюс утверждается тут же: без способности вторая привязка отвергается.
// Без него зелёное было бы неотличимо от реализации, которая не смотрит на
// способность вовсе.
func TestSecondAttachmentIsReachableThroughTheProductPath(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	// ── полюс «способность объявлена»: вторая привязка проходит ────────────────
	multiType := seedTypeWithMultiAttach(t, pool, true)
	vMulti := mkVolumeOnType(t, pool, r, "prj-1", "vol-cap-yes", multiType)
	require.NoError(t, r.Attach(ctx, mkAttach(vMulti.ID, "epd00000000000000031", "sdb", false)))
	second := mkAttach(vMulti.ID, "epd00000000000000032", "sdc", false)
	second.InstanceName = "web-2"
	require.NoError(t, r.Attach(ctx, second),
		"ревизия привязки объявляет множественную привязку — вторая обязана пройти")
	require.Equal(t, 2, attachRowCount(t, pool, vMulti.ID))

	got, err := r.Get(ctx, vMulti.ID)
	require.NoError(t, err)
	require.Len(t, got.Attachments, 2, "боевой путь дал две привязки, чтение отдало %d",
		len(got.Attachments))

	// ── полюс «способность НЕ объявлена»: вторая привязка отвергается ──────────
	soloType := seedTypeWithMultiAttach(t, pool, false)
	vSolo := mkVolumeOnType(t, pool, r, "prj-1", "vol-cap-no", soloType)
	require.NoError(t, r.Attach(ctx, mkAttach(vSolo.ID, "epd00000000000000041", "sdb", false)))
	require.Error(t, r.Attach(ctx, mkAttach(vSolo.ID, "epd00000000000000042", "sdc", false)),
		"способность не объявлена — вторая привязка обязана быть отвергнута")
	require.Equal(t, 1, attachRowCount(t, pool, vSolo.ID))
}

// TestReadPathAnswersStatusReasonAndUsedBytes — `Get` и `List` отдают
// `status_reason` и `used_bytes`, записанные сверщиком.
//
// До починки проекция чтения (`volumeSelectCols`) этих колонок не выбирала вовсе:
// они писались сверщиком и не читались НИ ОДНИМ путём. Арендатор получал пустую
// причину отказа и отсутствующее потребление ВСЕГДА — при живых значениях в базе, и
// отличить это от «бэкенд не сказал» было нечем.
//
// Обе колонки утверждаются на ОБОИХ чтениях: расхождение `Get` и `List` — отдельный
// класс («проекция, которая поле не заполняет, обязана это сказать»), и проба,
// спросившая одно чтение, его бы не заметила.
func TestReadPathAnswersStatusReasonAndUsedBytes(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	store := reconciler.NewStore(pool)
	ctx := context.Background()

	// ── потребление: пишется БОЕВЫМ путём подтверждения ───────────────────────
	used := mkVolumeCreating(t, r, "prj-observed", "vol-used", 10<<30)
	applied, err := store.Confirm(ctx, reconciler.KindVolume, used.ID, blockbackend.Observed{
		State: blockbackend.ObservedReady, SizeBytes: 10 << 30,
		UsedBytes: 4 << 30, HasUsedBytes: true,
	})
	require.NoError(t, err)
	require.True(t, applied)

	gotUsed, err := r.Get(ctx, used.ID)
	require.NoError(t, err)
	require.True(t, gotUsed.Observation.HasUsedBytes,
		"бэкенд сообщил потребление, а чтение отдало «не сказал»: колонки нет в проекции")
	require.EqualValues(t, 4<<30, gotUsed.Observation.UsedBytes)

	// ── причина отказа: пишется БОЕВЫМ путём объявления ошибки ────────────────
	failed := mkVolumeCreating(t, r, "prj-observed", "vol-reason", 10<<30)
	require.NoError(t, store.MarkError(ctx, reconciler.KindVolume, failed.ID,
		domain.ReasonBackendCapacityExhausted,
		blockbackend.Observed{State: blockbackend.ObservedError}))

	gotFailed, err := r.Get(ctx, failed.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ReasonBackendCapacityExhausted, gotFailed.StatusReason,
		"том объявлен ошибочным с названной причиной, а чтение отдало %q — "+
			"единственное, чем том объясняет своё ERROR, до арендатора не доезжает",
		gotFailed.StatusReason)

	// ── отрицательный полюс: бэкенд потребления НЕ сообщил ────────────────────
	//
	// Без него утверждение выше зеленело бы на реализации, которая подставляет
	// ноль всегда: «не сказали» и «пусто» обязаны остаться различимыми, и поле
	// контракта объявлено необязательным именно ради этого.
	silent := mkVolume(t, pool, r, "prj-observed", "vol-silent", 10<<30)
	gotSilent, err := r.Get(ctx, silent.ID)
	require.NoError(t, err)
	require.False(t, gotSilent.Observation.HasUsedBytes,
		"бэкенд потребления не сообщил, а чтение отдало значение — «не сказали» стало «пусто»")
	require.Equal(t, domain.ReasonNone, gotSilent.StatusReason)

	// ── ТО ЖЕ через список: проекции обязаны совпадать ────────────────────────
	page, _, err := r.List(ctx, volume.Pagination{PageSize: 50, ProjectID: "prj-observed"})
	require.NoError(t, err)
	byID := map[string]*domain.Volume{}
	for _, v := range page {
		byID[v.ID] = v
	}
	require.Len(t, byID, 3, "список не отдал все три тома проекта — сравнивать не с чем")
	require.True(t, byID[used.ID].Observation.HasUsedBytes,
		"Get отдаёт потребление, а List — нет: две проекции одного предмета")
	require.EqualValues(t, 4<<30, byID[used.ID].Observation.UsedBytes)
	require.Equal(t, domain.ReasonBackendCapacityExhausted, byID[failed.ID].StatusReason,
		"Get отдаёт причину отказа, а List — нет: две проекции одного предмета")
	require.False(t, byID[silent.ID].Observation.HasUsedBytes)
}

// seedTypeWithMultiAttach заводит класс диска со СВОИМ бэкендом и действующей
// ревизией привязки в зоне фикстур, объявляющей (либо не объявляющей) способность
// множественной привязки.
func seedTypeWithMultiAttach(t *testing.T, pool *pgxpool.Pool, multi bool) string {
	t.Helper()
	ctx := context.Background()
	// Идентификатор класса диска — человекочитаемый СЛАГ, назначаемый
	// администратором (`block-standard`, `block-fast`; миграция 0003 объявляет это
	// прямо), а НЕ чеканимый id. Прежде фикстура звала `NewHyphenID("dt")` и
	// производила форму, которой продукт для этого ресурса не выпускает нигде:
	// префикса `dt` нет ни в одном вызове чеканки прод-кода. Фикстура, минтящая
	// невозможное значение, отличается от продукта ровно там, где проба должна
	// его повторять, — и вдобавок этот `dt` всплывал третьим «недостающим
	// префиксом» в каждой переписи дефисной чеканки, будучи артефактом пробы.
	// Бэкенд и привязка ниже дефисную чеканку зовут ЗАКОННО: `sb` и `dtb` —
	// настоящие префиксы этих ресурсов.
	typeID := "block-fixture-" + ids.NewUID()
	backendID := ids.NewHyphenID("sb")
	_, err := pool.Exec(ctx,
		`INSERT INTO disk_types (id, name, lifecycle) VALUES ($1, $1, 'ACTIVE')`, typeID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO storage_backends (id, name, kind, zone_ids, endpoint, credentials_ref)
		VALUES ($1, $1, 'CEPH_RBD', '[]'::jsonb, 'cfg://fixture', 'vault://fixture')`, backendID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO disk_type_bindings
		  (id, disk_type_id, zone_id, backend_id, revision, pool, status, cap_multi_attach)
		VALUES ($1,$2,'region-1-a',$3,1,'kacho-block','ACTIVE',$4)`,
		ids.NewHyphenID("dtb"), typeID, backendID, multi)
	require.NoError(t, err)
	return typeID
}

// mkVolumeOnType заводит готовый том НА НАЗВАННОМ классе и провязывает его с
// действующей ревизией привязки — той самой, чью способность читает предикат
// вставки привязки.
func mkVolumeOnType(t *testing.T, pool *pgxpool.Pool, r *pg.VolumeRepo,
	project, name, diskTypeID string,
) *domain.Volume {
	t.Helper()
	ctx := context.Background()
	v, _, err := r.Insert(ctx, &domain.Volume{
		ID:         ids.NewID(domain.PrefixVolume),
		ProjectID:  project,
		Name:       name,
		ZoneID:     "region-1-a",
		DiskTypeID: diskTypeID,
		SizeBytes:  10 << 30,
	}, "")
	require.NoError(t, err)
	// Привязка ревизии — то, что на живом стенде проставляет сверщик, выбрав
	// действующую ревизию класса в зоне тома.
	tag, err := pool.Exec(ctx, `
		UPDATE volumes SET binding_id = b.id
		  FROM disk_type_bindings b
		 WHERE volumes.id = $1 AND b.disk_type_id = $2
		   AND b.zone_id = volumes.zone_id AND b.status = 'ACTIVE'`, v.ID, diskTypeID)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected(),
		"ревизия привязки не проставилась — предикат вставки привязки читал бы её как отсутствующую")
	confirmReady(t, pool, reconciler.KindVolume, v.ID, 10<<30)
	return v
}
