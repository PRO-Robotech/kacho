// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// bindingSample — законная ревизия целиком, со ВСЕМИ полями непустыми: поле,
// оставленное нулевым, прошло бы круг «записали ноль — прочитали ноль» и ничего не
// доказало бы о переносе.
func bindingSample(diskTypeID, zoneID, backendID string) *domain.DiskTypeBinding {
	return &domain.DiskTypeBinding{
		ID:         ids.NewHyphenID(domain.PrefixDiskTypeBinding),
		DiskTypeID: diskTypeID,
		ZoneID:     zoneID,
		BackendID:  backendID,
		Locator: domain.BindingLocator{
			Pool:              "kacho-block-balanced",
			NamespaceTemplate: "prj-{projectId}",
		},
		Capabilities: domain.BindingCapabilities{
			Snapshots: true, CloneFromSnapshot: true, CloneFromImage: true,
			CloneKeepsParent: true, OnlineGrow: true, MultiAttach: false,
			EncryptionAtRest: true, TrashTTLSeconds: 86400,
		},
		QoS: domain.BindingQoS{
			BaselineIOPS: 3000, IOPSPerGiB: 30, MaxIOPS: 80000,
			BaselineThroughputMiBps: 125, ThroughputPerGiBMiBps: 0.5, MaxThroughputMiBps: 1000,
		},
	}
}

// bindSeedDiskType заводит класс: ревизия ссылается на него внешней связью, без
// строки класса её не вставить вовсе.
func bindSeedDiskType(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	_, err := pg.NewDiskTypeRepo(pool).Insert(context.Background(), &domain.DiskType{
		ID: id, Name: id, PerformanceTier: domain.TierBalanced, Lifecycle: domain.LifecycleActive,
	})
	require.NoError(t, err)
	return id
}

// bindSeedBackend регистрирует бэкенд — вторую сторону ссылки ревизии.
func bindSeedBackend(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	b, err := pg.NewStorageBackendRepo(pool).Insert(context.Background(), sbSample(name))
	require.NoError(t, err)
	return b.ID
}

// bindActiveCount — сколько ДЕЙСТВУЮЩИХ ревизий у пары (класс, зона). Утверждение
// про таблицу, а не про то, что вернул репозиторий: ровно одна действующая — это
// свойство данных, и проверять его надо в данных.
func bindActiveCount(t *testing.T, pool *pgxpool.Pool, diskTypeID, zoneID string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM disk_type_bindings
		  WHERE disk_type_id = $1 AND zone_id = $2 AND status = 'ACTIVE'`,
		diskTypeID, zoneID).Scan(&n))
	return n
}

// bindRevisions — номера ревизий пары в порядке возрастания.
func bindRevisions(t *testing.T, pool *pgxpool.Pool, diskTypeID, zoneID string) []int32 {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT revision FROM disk_type_bindings
		  WHERE disk_type_id = $1 AND zone_id = $2 ORDER BY revision`, diskTypeID, zoneID)
	require.NoError(t, err)
	defer rows.Close()
	var out []int32
	for rows.Next() {
		var rev int32
		require.NoError(t, rows.Scan(&rev))
		out = append(out, rev)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestDiskTypeBindingRoundTrip — ревизия читается ОБОИМИ путями побайтово той же,
// какой записана: координата, все семь способностей, срок корзины и числа
// производительности, включая единственное дробное.
func TestDiskTypeBindingRoundTrip(t *testing.T) {
	pool := newBareTestPool(t)
	r := pg.NewDiskTypeBindingRepo(pool)
	ctx := context.Background()

	dt := bindSeedDiskType(t, pool, "block-round")
	be := bindSeedBackend(t, pool, "ceph-round")

	want := bindingSample(dt, "ru-central1-a", be)
	created, err := r.Register(ctx, want)
	require.NoError(t, err)
	require.Equal(t, want.ID, created.ID)
	require.EqualValues(t, 1, created.Revision, "первая ревизия пары — первая")
	require.Equal(t, domain.BindingStatusActive, created.Status)
	require.True(t, created.IsActive())
	require.False(t, created.CreatedAt.IsZero())
	require.Equal(t, want.Locator, created.Locator)
	require.Equal(t, want.Capabilities, created.Capabilities)
	require.Equal(t, want.QoS, created.QoS)

	got, err := r.Get(ctx, want.ID)
	require.NoError(t, err)
	require.Equal(t, created, got, "чтение по id отдаёт ровно то, что вернула запись")

	page, next, err := r.List(ctx, 50, "")
	require.NoError(t, err)
	require.Empty(t, next)
	require.Len(t, page, 1)
	require.Equal(t, created, page[0], "список несёт то же, что и чтение по id")

	missing := ids.NewHyphenID(domain.PrefixDiskTypeBinding)
	_, err = r.Get(ctx, missing)
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound), "got %v", err)
	require.Equal(t, "DiskTypeBinding "+missing+" not found", err.Error()[len("not found: "):])
}

// TestDiskTypeBindingRegisterSupersedesPrevious (STOR-P-13) — новая ревизия пары
// вытесняет прежнюю, и прежняя остаётся В ТАБЛИЦЕ неизменной во ВСЕХ полях, кроме
// состояния. Это несущая проба всей модели: ссылка тома на ревизию равна копии
// политики ровно потому, что цель неизменяема.
func TestDiskTypeBindingRegisterSupersedesPrevious(t *testing.T) {
	pool := newBareTestPool(t)
	r := pg.NewDiskTypeBindingRepo(pool)
	ctx := context.Background()

	dt := bindSeedDiskType(t, pool, "block-sup")
	be := bindSeedBackend(t, pool, "ceph-sup")

	rev1, err := r.Register(ctx, bindingSample(dt, "ru-central1-a", be))
	require.NoError(t, err)
	require.EqualValues(t, 1, rev1.Revision)

	// Соседняя пара — положительный контроль вытеснения: она обязана пережить его
	// нетронутой, иначе «вытеснили прежнюю» зеленело бы на реализации, снимающей
	// действующие ревизии всех пар подряд.
	otherZone, err := r.Register(ctx, bindingSample(dt, "ru-central1-b", be))
	require.NoError(t, err)
	otherClass := bindSeedDiskType(t, pool, "block-sup-2")
	otherType, err := r.Register(ctx, bindingSample(otherClass, "ru-central1-a", be))
	require.NoError(t, err)

	next := bindingSample(dt, "ru-central1-a", be)
	next.Locator.Pool = "kacho-block-fast"
	next.Capabilities.MultiAttach = true
	next.QoS.MaxIOPS = 120000
	rev2, err := r.Register(ctx, next)
	require.NoError(t, err)
	require.EqualValues(t, 2, rev2.Revision, "номер ревизии — следующий в пределах пары")
	require.Equal(t, domain.BindingStatusActive, rev2.Status)
	require.Equal(t, "kacho-block-fast", rev2.Locator.Pool)

	superseded, err := r.Get(ctx, rev1.ID)
	require.NoError(t, err)
	wantPrev := *rev1
	wantPrev.Status = domain.BindingStatusSuperseded
	require.Equal(t, &wantPrev, superseded,
		"вытеснение меняет ТОЛЬКО состояние: обещанное прежним ресурсам остаётся дословно")
	require.False(t, superseded.IsActive())

	require.Equal(t, 1, bindActiveCount(t, pool, dt, "ru-central1-a"))
	require.Equal(t, []int32{1, 2}, bindRevisions(t, pool, dt, "ru-central1-a"),
		"история append-only: прежняя строка не удалена")

	stillActive, err := r.Get(ctx, otherZone.ID)
	require.NoError(t, err)
	require.True(t, stillActive.IsActive(), "ревизия соседней ЗОНЫ вытеснением не тронута")
	stillActive, err = r.Get(ctx, otherType.ID)
	require.NoError(t, err)
	require.True(t, stillActive.IsActive(), "ревизия соседнего КЛАССА вытеснением не тронута")

	third := bindingSample(dt, "ru-central1-a", be)
	rev3, err := r.Register(ctx, third)
	require.NoError(t, err)
	require.EqualValues(t, 3, rev3.Revision)
	prev2, err := r.Get(ctx, rev2.ID)
	require.NoError(t, err)
	require.Equal(t, domain.BindingStatusSuperseded, prev2.Status)
	prev1, err := r.Get(ctx, rev1.ID)
	require.NoError(t, err)
	require.Equal(t, domain.BindingStatusSuperseded, prev1.Status, "уже вытесненная так и остаётся")
	require.Equal(t, 1, bindActiveCount(t, pool, dt, "ru-central1-a"))
}

// bindWaitBlocked ждёт, пока в ЭТОЙ базе не окажется ровно want заблокированных на
// замке серверных процессов.
//
// Ожидание по УСЛОВИЮ, а не по времени: пауза фиксированной длины сделала бы пробу
// зелёной на машине помедленнее и красной на загруженной, ничего не утверждая о
// продукте. Не дождавшись — проба падает, а не тихо переходит в неперекрывающийся
// прогон, в котором обе регистрации прошли бы по очереди и «ровно одна» ничего бы
// не значило.
func bindWaitBlocked(t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var n int
		if err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity
			  WHERE datname = current_database()
			    AND wait_event_type = 'Lock'
			    AND pid <> pg_backend_pid()`).Scan(&n); err != nil {
			return false
		}
		return n == want
	}, 30*time.Second, 2*time.Millisecond,
		"регистрации не встали на замок: предпосылка пробы не выполнена, перекрытия нет")
}

// TestDiskTypeBindingRegisterConcurrentExactlyOneWins (STOR-P-14) — две регистрации
// на ОДНУ пару, перекрытые детерминированно: ровно одна получает действующую
// ревизию, вторая — ALREADY_EXISTS.
//
// Перекрытие создаётся ДЕРЖАТЕЛЕМ ЗАМКА, а не паузой: отдельная транзакция берёт
// строку действующей ревизии на замок, обе регистрации встают в очередь за ней (обе
// уже со своим снимком данных), и только после снятия замка продолжают. Без этого
// перекрытия «ровно одна» — утверждение о расписании, а не о продукте: разошедшиеся
// во времени регистрации ЗАКОННО проходят обе, последовательно повышая номер.
func TestDiskTypeBindingRegisterConcurrentExactlyOneWins(t *testing.T) {
	pool := newBareTestPool(t)
	r := pg.NewDiskTypeBindingRepo(pool)
	ctx := context.Background()

	dt := bindSeedDiskType(t, pool, "block-race")
	be := bindSeedBackend(t, pool, "ceph-race")
	seed, err := r.Register(ctx, bindingSample(dt, "ru-central1-a", be))
	require.NoError(t, err)
	require.EqualValues(t, 1, seed.Revision)

	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)
	var lockedID string
	require.NoError(t, blocker.QueryRow(ctx,
		`SELECT id FROM disk_type_bindings
		  WHERE disk_type_id = $1 AND zone_id = $2 AND status = 'ACTIVE' FOR UPDATE`,
		dt, "ru-central1-a").Scan(&lockedID))
	require.Equal(t, seed.ID, lockedID, "на замке именно действующая ревизия пары")

	const contenders = 2
	results := make(chan error, contenders)
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, rerr := r.Register(context.Background(), bindingSample(dt, "ru-central1-a", be))
			results <- rerr
		}()
	}
	bindWaitBlocked(t, pool, contenders)
	require.NoError(t, blocker.Rollback(ctx))
	wg.Wait()
	close(results)

	var won, lost int
	for rerr := range results {
		switch {
		case rerr == nil:
			won++
		case stderrors.Is(rerr, storageerr.ErrAlreadyExists):
			lost++
		default:
			t.Fatalf("неожиданный исход конкурентной регистрации: %v", rerr)
		}
	}
	require.Equal(t, 1, won, "ровно одна регистрация заводит действующую ревизию")
	require.Equal(t, contenders-1, lost, "остальные получают ALREADY_EXISTS")
	require.Equal(t, 1, bindActiveCount(t, pool, dt, "ru-central1-a"))
	require.Equal(t, []int32{1, 2}, bindRevisions(t, pool, dt, "ru-central1-a"),
		"проигравшая не оставила ни строки, ни дырки в нумерации")

	// Положительный контроль: одновременные регистрации на РАЗНЫЕ пары проходят обе.
	// Без него «ровно одна» зеленело бы на реализации, отвергающей любую вторую
	// регистрацию вообще.
	var free sync.WaitGroup
	freeErrs := make(chan error, 2)
	for _, zone := range []string{"ru-central1-b", "ru-central1-c"} {
		free.Add(1)
		go func(z string) {
			defer free.Done()
			_, ferr := r.Register(context.Background(), bindingSample(dt, z, be))
			freeErrs <- ferr
		}(zone)
	}
	free.Wait()
	close(freeErrs)
	for ferr := range freeErrs {
		require.NoError(t, ferr, "регистрации на разные пары не конкурируют")
	}
}

// TestDiskTypeBindingRegisterRaceHoldsUnderAnyInterleaving — то же свойство без
// принудительного перекрытия: как бы ни легло расписание, действующая ревизия пары
// остаётся ровно одна, номера не повторяются и не имеют дырок, а всякий отказ —
// ALREADY_EXISTS. Под -race.
func TestDiskTypeBindingRegisterRaceHoldsUnderAnyInterleaving(t *testing.T) {
	pool := newBareTestPool(t)
	r := pg.NewDiskTypeBindingRepo(pool)
	ctx := context.Background()

	dt := bindSeedDiskType(t, pool, "block-any")
	be := bindSeedBackend(t, pool, "ceph-any")

	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, rerr := r.Register(ctx, bindingSample(dt, "ru-central1-a", be))
			results <- rerr
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var won int
	for rerr := range results {
		switch {
		case rerr == nil:
			won++
		case stderrors.Is(rerr, storageerr.ErrAlreadyExists):
		default:
			t.Fatalf("неожиданный исход конкурентной регистрации: %v", rerr)
		}
	}
	require.GreaterOrEqual(t, won, 1, "хотя бы одна регистрация проходит")
	require.Equal(t, 1, bindActiveCount(t, pool, dt, "ru-central1-a"),
		"сколько бы ни прошло, действующая остаётся ровно одна")

	revs := bindRevisions(t, pool, dt, "ru-central1-a")
	require.Len(t, revs, won, "в таблице ровно столько строк, сколько регистраций прошло")
	sort.Slice(revs, func(i, j int) bool { return revs[i] < revs[j] })
	for i, rev := range revs {
		require.EqualValues(t, i+1, rev, "номера идут подряд с единицы: %v", revs)
	}
}

// TestDiskTypeBindingRepoHasNoMutatingPath — у репозитория ревизий НЕТ пути правки.
// Отсутствие обновления и есть механизм, ради которого ревизии заведены, поэтому оно
// держится ГЕЙТОМ, а не комментарием: комментарий переживёт первый же добавленный
// метод. Гейт падает в обе стороны — и на появившемся мутаторе, и на исчезнувшем
// пути регистрации.
func TestDiskTypeBindingRepoHasNoMutatingPath(t *testing.T) {
	want := map[string]struct{}{"Get": {}, "List": {}, "Register": {}}
	rt := reflect.TypeOf((*pg.DiskTypeBindingRepo)(nil))

	got := map[string]struct{}{}
	for i := 0; i < rt.NumMethod(); i++ {
		got[rt.Method(i).Name] = struct{}{}
	}
	for name := range got {
		_, known := want[name]
		require.True(t, known,
			"у ревизии появился метод %s: append-only держится отсутствием правки, а не дисциплиной", name)
	}
	for name := range want {
		_, present := got[name]
		require.True(t, present, "пропал метод %s — гейт остался бы зелёным на пустом репозитории", name)
	}
}

// TestDiskTypeBindingNumberAndStatusAssignedByRegistry — номер ревизии и её
// состояние назначает РЕГИСТРАЦИЯ, а не вызывающий. Названное вызывающим отвергается
// ЯВНО: принять и выбросить значило бы вернуть успех на параметр, который не
// применён.
func TestDiskTypeBindingNumberAndStatusAssignedByRegistry(t *testing.T) {
	pool := newBareTestPool(t)
	r := pg.NewDiskTypeBindingRepo(pool)
	ctx := context.Background()

	dt := bindSeedDiskType(t, pool, "block-assign")
	be := bindSeedBackend(t, pool, "ceph-assign")

	withRevision := bindingSample(dt, "ru-central1-a", be)
	withRevision.Revision = 7
	_, err := r.Register(ctx, withRevision)
	require.Equal(t, "disk_type_binding revision is assigned on registration and must not be supplied",
		iaText(t, err))

	withStatus := bindingSample(dt, "ru-central1-a", be)
	withStatus.Status = domain.BindingStatusSuperseded
	_, err = r.Register(ctx, withStatus)
	require.Equal(t, `disk_type_binding is registered ACTIVE: status "SUPERSEDED" must not be supplied`,
		iaText(t, err))

	// Пары: неназванные номер и состояние проходят, названное ACTIVE — тоже, потому
	// что оно совпадает с тем, чем регистрация и является.
	created, err := r.Register(ctx, bindingSample(dt, "ru-central1-a", be))
	require.NoError(t, err)
	require.EqualValues(t, 1, created.Revision)

	explicit := bindingSample(dt, "ru-central1-b", be)
	explicit.Status = domain.BindingStatusActive
	created, err = r.Register(ctx, explicit)
	require.NoError(t, err)
	require.Equal(t, domain.BindingStatusActive, created.Status)
}

// TestDiskTypeBindingReferencedRevisionIsNotDeletable (STOR-P-15) — ревизия, на
// которую ссылается ресурс, не удаляется. Держит ОГРАНИЧИТЕЛЬНАЯ внешняя связь
// (0017), поэтому и проверяется она прямым удалением из таблицы: репозиторий пути
// удаления не имеет, а свойство принадлежит БД и обязано выполняться независимо от
// того, кто пришёл со стейтментом.
//
// Пара обязательна: ревизия без ссылающихся удаляется тем же стейтментом — значит
// отказ выше про ССЫЛКУ, а не про запрет удаления вообще.
func TestDiskTypeBindingReferencedRevisionIsNotDeletable(t *testing.T) {
	pool := newBareTestPool(t)
	r := pg.NewDiskTypeBindingRepo(pool)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	dt := bindSeedDiskType(t, pool, "block-fk")
	be := bindSeedBackend(t, pool, "ceph-fk")
	bound, err := r.Register(ctx, bindingSample(dt, "region-1-a", be))
	require.NoError(t, err)

	vol, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-fk", Name: "vol-fk",
		ZoneID: "region-1-a", DiskTypeID: dt, SizeBytes: 1 << 30,
		Backend: domain.Placement{BackendObject: "kc7f-vol-fk"},
	}, "")
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM disk_type_bindings WHERE id = $1`, bound.ID)
	requireFKRestrict(t, err, "volumes_binding_id_fkey")
	alive, err := r.Get(ctx, bound.ID)
	require.NoError(t, err, "ревизия жива, пока на неё ссылается ресурс")
	require.Equal(t, bound, alive, "отказавшее удаление не тронуло ни одного поля")

	// Вторая колонка ссылки — своя связь и свой отказ: ревизия, в которую ресурс
	// ПЕРЕЕЗЖАЕТ, обязана дожить до конца переезда.
	target, err := r.Register(ctx, bindingSample(dt, "region-1-b", be))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE volumes SET desired_binding_id = $1 WHERE id = $2`, target.ID, vol.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM disk_type_bindings WHERE id = $1`, target.ID)
	requireFKRestrict(t, err, "volumes_desired_binding_id_fkey")

	free, err := r.Register(ctx, bindingSample(dt, "region-1-c", be))
	require.NoError(t, err)
	tag, err := pool.Exec(ctx, `DELETE FROM disk_type_bindings WHERE id = $1`, free.ID)
	require.NoError(t, err, "ревизия без ссылающихся удаляется")
	require.EqualValues(t, 1, tag.RowsAffected())
}

// requireFKRestrict утверждает отказ ограничительной внешней связи ПОИМЁННО: код
// 23503 сам по себе сказал бы лишь «какая-то ссылка», и проба зеленела бы на
// нарушении другой связи.
func requireFKRestrict(t *testing.T, err error, constraint string) {
	t.Helper()
	require.Error(t, err, "удаление ревизии, на которую ссылаются, обязано отказать")
	var pgErr *pgconn.PgError
	require.True(t, stderrors.As(err, &pgErr), "ожидался отказ БД, got %v", err)
	require.Equal(t, "23503", pgErr.Code)
	require.Equal(t, constraint, pgErr.ConstraintName)
}

// TestDiskTypeBindingUnknownClassOrBackendRejected — ревизия ссылается на класс и
// бэкенд внешними связями, поэтому несуществующая сторона отвергается БД. Тон отказа
// — контрактный, чтобы администратор видел, ЧЕГО не хватает.
func TestDiskTypeBindingUnknownClassOrBackendRejected(t *testing.T) {
	pool := newBareTestPool(t)
	r := pg.NewDiskTypeBindingRepo(pool)
	ctx := context.Background()

	dt := bindSeedDiskType(t, pool, "block-ref")
	be := bindSeedBackend(t, pool, "ceph-ref")

	_, err := r.Register(ctx, bindingSample("block-absent", "ru-central1-a", be))
	require.Equal(t, "DiskType block-absent not found", fpText(t, err))

	missingBackend := ids.NewHyphenID(domain.PrefixStorageBackend)
	_, err = r.Register(ctx, bindingSample(dt, "ru-central1-a", missingBackend))
	require.Equal(t, "StorageBackend "+missingBackend+" not found", fpText(t, err))

	_, err = r.Register(ctx, bindingSample(dt, "ru-central1-a", be))
	require.NoError(t, err, "обе стороны существуют — отказы выше про ссылки, а не про запись вообще")
}

// TestDiskTypeBindingInvariantsHeldByDB — форма ревизии энфорсится ОГРАНИЧЕНИЯМИ БД
// (0015), а не разбором в репозитории. Пары обязательны.
func TestDiskTypeBindingInvariantsHeldByDB(t *testing.T) {
	pool := newBareTestPool(t)
	r := pg.NewDiskTypeBindingRepo(pool)
	ctx := context.Background()

	dt := bindSeedDiskType(t, pool, "block-inv")
	be := bindSeedBackend(t, pool, "ceph-inv")

	noPool := bindingSample(dt, "ru-central1-a", be)
	noPool.Locator.Pool = ""
	_, err := r.Register(ctx, noPool)
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "пустое пространство размещения, got %v", err)

	noZone := bindingSample(dt, "", be)
	_, err = r.Register(ctx, noZone)
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "пустая зона, got %v", err)

	negativeTTL := bindingSample(dt, "ru-central1-a", be)
	negativeTTL.Capabilities.TrashTTLSeconds = -1
	_, err = r.Register(ctx, negativeTTL)
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "отрицательный срок корзины, got %v", err)

	_, err = r.Register(ctx, bindingSample(dt, "ru-central1-a", be))
	require.NoError(t, err, "законная ревизия той же формы проходит")

	dup := bindingSample(dt, "ru-central1-b", be)
	first, err := r.Register(ctx, dup)
	require.NoError(t, err)
	sameID := bindingSample(dt, "ru-central1-c", be)
	sameID.ID = first.ID
	_, err = r.Register(ctx, sameID)
	require.True(t, stderrors.Is(err, storageerr.ErrAlreadyExists), "дубль id, got %v", err)
	require.Equal(t, "DiskTypeBinding "+first.ID+" already exists", err.Error()[len("already exists: "):])
}

// TestDiskTypeBindingListCursor — курсорная пагинация по (created_at, id) ASC:
// строка не пропускается и не повторяется; мусорный курсор → InvalidArgument, и это
// в паре с законным курсором.
func TestDiskTypeBindingListCursor(t *testing.T) {
	pool := newBareTestPool(t)
	r := pg.NewDiskTypeBindingRepo(pool)
	ctx := context.Background()

	dt := bindSeedDiskType(t, pool, "block-page")
	be := bindSeedBackend(t, pool, "ceph-page")

	const total = 5
	for i := 0; i < total; i++ {
		_, err := r.Register(ctx, bindingSample(dt, fmt.Sprintf("ru-central1-%d", i), be))
		require.NoError(t, err)
	}

	page, next, err := r.List(ctx, 2, "")
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.NotEmpty(t, next)

	seen := map[string]struct{}{}
	for _, b := range page {
		seen[b.ID] = struct{}{}
	}
	token := next
	for token != "" {
		var chunk []*domain.DiskTypeBinding
		chunk, token, err = r.List(ctx, 2, token)
		require.NoError(t, err)
		for _, b := range chunk {
			_, dup := seen[b.ID]
			require.False(t, dup, "курсор не повторяет строку: %s", b.ID)
			seen[b.ID] = struct{}{}
		}
	}
	require.Len(t, seen, total, "курсор прошёл все ревизии")

	_, _, err = r.List(ctx, 2, "не-курсор")
	require.Equal(t, "invalid page_token", iaText(t, err))
}
