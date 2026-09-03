// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package reconciler_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/quota"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend/fake"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/migrations"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// Полный цикл «создаваемый → готовый» на дублёре плоскости данных.
//
// Это несущая проба всей задачи: она доказывает, что готовность ресурса производит
// НАБЛЮДЕНИЕ, а не наша запись. Без неё утверждение «сверщик доводит ресурс до
// готовности» держалось бы разбором кода.
//
// Дублёр здесь не ослабляет пробу: он гоняется той же контрактной суитой, что и
// настоящий адаптер, и отвергает тот же ввод.
//
// Приёмка STOR-P-23, 24, 25, 28, 31, 32, 35.

func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		// Приведение схемы — ОДИН раз на пакет, у выдающего базу.
		// Прежде его приписывал каждый вызывающий своей копией; забывший
		// получал `relation … does not exist` — отказ, читающийся как дефект
		// продукта. Довод целиком — `internal/pgtest` §WithSearchPath.
		SearchPath: "kacho_storage,public",
		Name:       "storage",
		User:       "storage",
		Password:   "secret",
		Migrate:    pgtest.Goose(migrations.FS),
	}))
}

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test (testcontainers Postgres) — skipped with -short")
	}
	dsn := pgtest.NewDB(t) + "&pool_max_conns=8"
	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	seedQuotaFor(t, pool, "prj-cycle")
	return pool
}

// seedQuotaFor приводит базу пробы в состояние «проект материализован».
//
// Пробы этого пакета заводят тома напрямую, а вставка строки ресурса СПИСЫВАЕТ
// место (миграция 0023): «потолок не назван» — отказ, а не «без предела». На
// живом пути строку заводит материализация; здесь её нет, поэтому проба обязана
// привести базу в то же состояние, в каком её видит репозиторий в бою.
//
// Идёт через `pg.MaterializeQuotas` — тот же и ЕДИНСТВЕННЫЙ оператор заведения
// строк учёта, а не через свой INSERT: копия разошлась бы с настоящим молча, и
// разошлась бы на составе столбцов, то есть там, где расхождение не видно
// глазом. Величина заведомо больше, чем нужно пробам: предел здесь не предмет
// утверждения, а условие достижимости предмета.
func seedQuotaFor(t *testing.T, pool *pgxpool.Pool, project string) {
	t.Helper()
	rows := make([]quota.Row, 0, 3)
	for _, kind := range []string{"storage.volumes", "storage.snapshots", "storage.images"} {
		rows = append(rows, quota.Row{
			CarrierType:   quota.CarrierProject,
			CarrierID:     project,
			Kind:          kind,
			Limit:         1_000_000,
			SourceScope:   "DEFAULT",
			LimitRevision: 0,
			// Зеркало аккаунта непусто: схема отвергает пустое, и отвергает
			// правильно — строка без зеркала невидима аккаунтной дельте.
			AccountID: "acc-fixture",
		})
	}
	n, err := pg.MaterializeQuotas(context.Background(), pool, rows)
	require.NoError(t, err, "фикстура учёта: заведение строк")
	require.Equal(t, int64(len(rows)), n,
		"перепись: заведено строк — столько же, сколько объявлено")
}

// openerFor отдаёт один и тот же дублёр на любую ревизию привязки.
type openerFor struct{ b blockbackend.Backend }

func (o openerFor) Open(context.Context, reconciler.Binding) (blockbackend.Backend, error) {
	return o.b, nil
}

// seedBinding заводит класс, бэкенд и действующую ревизию привязки, возвращая её id
// и локатор. Без действующей ревизии тома не создаются вовсе — этим и замкнут
// инвариант «создаваемый навсегда невозможен».
func seedBinding(t *testing.T, pool *pgxpool.Pool) (bindingID string, loc blockbackend.Locator) {
	t.Helper()
	ctx := context.Background()
	const (
		diskType = "block-cycle"
		zone     = "ru-central1-a"
		pool_    = "kacho-cycle"
	)
	backendID := ids.NewHyphenID("sb")
	bindingID = ids.NewHyphenID("dtb")

	_, err := pool.Exec(ctx, `INSERT INTO disk_types (id, name, lifecycle) VALUES ($1,$1,'ACTIVE')`, diskType)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO storage_backends (id, name, kind, zone_ids, endpoint, credentials_ref)
		VALUES ($1, $1, 'CEPH_RBD', $2::jsonb, 'cfg://cycle', 'vault://cycle')`,
		backendID, `["`+zone+`"]`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO disk_type_bindings
			(id, disk_type_id, zone_id, backend_id, revision, pool, namespace_template,
			 cap_snapshots, cap_clone_from_snapshot, cap_online_grow, status)
		VALUES ($1, $2, $3, $4, 1, $5, '{projectId}', true, true, true, 'ACTIVE')`,
		bindingID, diskType, zone, backendID, pool_)
	require.NoError(t, err)

	return bindingID, blockbackend.Locator{Pool: pool_, Namespace: "prj-cycle"}
}

// insertVolume кладёт строку тома в СОЗДАВАЕМОМ состоянии — ровно так, как её
// оставляет вставка после фиксации намерения.
func insertVolume(t *testing.T, pool *pgxpool.Pool, bindingID string, loc blockbackend.Locator, size int64) (id, object string) {
	t.Helper()
	id = ids.NewID(domain.PrefixVolume)
	object = blockbackend.ObjectName("kctest", id)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO volumes (id, project_id, zone_id, name, disk_type_id, size_bytes, state,
		                     binding_id, backend_object, backend_namespace, observed_state)
		VALUES ($1, 'prj-cycle', 'ru-central1-a', $1, 'block-cycle', $2, 'CREATING',
		        $3, $4, $5, 'ABSENT')`,
		id, size, bindingID, object, loc.Namespace)
	require.NoError(t, err)
	return id, object
}

func volumeState(t *testing.T, pool *pgxpool.Pool, id string) (state, observed, reason string) {
	t.Helper()
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT state, observed_state, status_reason FROM volumes WHERE id = $1`, id).
		Scan(&state, &observed, &reason))
	return
}

func TestCycle_CreatingBecomesReadyOnlyAfterTheObjectExists(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	bindingID, loc := seedBinding(t, pool)
	id, object := insertVolume(t, pool, bindingID, loc, 4<<30)

	be := fake.New(blockbackend.Capabilities{
		Snapshots: true, CloneFromSnapshot: true, OnlineGrow: true,
	})
	rec := reconciler.New(reconciler.NewStore(pool), openerFor{be}, reconciler.Config{
		Interval: 0, Batch: 10, CallTimeout: 0,
	})

	// До прохода готовности нет: строка есть, объекта нет.
	state, observed, _ := volumeState(t, pool, id)
	require.Equal(t, "CREATING", state)
	require.Equal(t, "ABSENT", observed)

	c := rec.Once(ctx)
	require.Equal(t, 1, c.Scanned, "проход обязан увидеть расхождение")
	require.Equal(t, 1, c.Provision, "и создать объект")

	// Объект создан у бэкенда — и ТОЛЬКО после этого ресурс объявлен готовым.
	obs, err := be.Observe(ctx, blockbackend.ObjectRef{Locator: loc, Name: object})
	require.NoError(t, err)
	require.Equal(t, blockbackend.ObservedReady, obs.State)
	require.EqualValues(t, 4<<30, obs.SizeBytes)

	state, observed, reason := volumeState(t, pool, id)
	require.Equal(t, "READY", state)
	require.Equal(t, "READY", observed)
	require.Empty(t, reason, "у готового ресурса причины быть не должно")

	// Второй проход не находит работы: расхождения больше нет.
	require.Zero(t, rec.Once(ctx).Scanned, "устоявшееся состояние обходом не берётся")
}

func TestCycle_BackendRefusalMarksTheResourceWithANamedReason(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	bindingID, loc := seedBinding(t, pool)
	id, _ := insertVolume(t, pool, bindingID, loc, 1<<30)

	be := fake.New(blockbackend.Capabilities{Snapshots: true, OnlineGrow: true})
	be.FailVerb("CreateVolume", blockbackend.OutcomeCapacityExhausted)
	rec := reconciler.New(reconciler.NewStore(pool), openerFor{be}, reconciler.Config{Batch: 10})

	rec.Once(ctx)

	state, _, reason := volumeState(t, pool, id)
	require.Equal(t, "ERROR", state)
	require.Equal(t, string(domain.ReasonBackendCapacityExhausted), reason,
		"причина обязана быть из закрытого словаря наших полос")
}

func TestCycle_UnavailableBackendDoesNotCondemnTheResource(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	bindingID, loc := seedBinding(t, pool)
	id, _ := insertVolume(t, pool, bindingID, loc, 1<<30)

	be := fake.New(blockbackend.Capabilities{Snapshots: true, OnlineGrow: true})
	be.FailVerb("Observe", blockbackend.OutcomeUnavailable)
	rec := reconciler.New(reconciler.NewStore(pool), openerFor{be}, reconciler.Config{Batch: 10})

	c := rec.Once(ctx)
	require.Equal(t, 1, c.Waited, "недоступность обязана давать ожидание, а не действие")

	state, observed, reason := volumeState(t, pool, id)
	require.Equal(t, "CREATING", state, "намерение не тронуто")
	require.Equal(t, "UNKNOWN", observed, "молчание бэкенда — не утверждение об отсутствии объекта")
	require.Empty(t, reason, "временная недоступность не есть приговор ресурсу")

	// Бэкенд ожил — тот же ресурс доходит до готовности без вмешательства.
	be.ClearFailures()
	rec.Once(ctx)
	state, observed, _ = volumeState(t, pool, id)
	require.Equal(t, "READY", state)
	require.Equal(t, "READY", observed)
}

func TestCycle_DeletionRemovesTheObjectBeforeTheRow(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	bindingID, loc := seedBinding(t, pool)
	id, object := insertVolume(t, pool, bindingID, loc, 1<<30)

	be := fake.New(blockbackend.Capabilities{Snapshots: true, OnlineGrow: true})
	rec := reconciler.New(reconciler.NewStore(pool), openerFor{be}, reconciler.Config{Batch: 10})
	rec.Once(ctx) // довели до готовности

	_, err := pool.Exec(ctx, `UPDATE volumes SET state = 'DELETING' WHERE id = $1`, id)
	require.NoError(t, err)

	// Первый проход снимает ОБЪЕКТ, строка ещё жива: иначе крах между шагами
	// оставил бы ёмкость, о которой в системе не осталось записи.
	c := rec.Once(ctx)
	require.Equal(t, 1, c.Remove)
	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM volumes WHERE id = $1`, id).Scan(&rows))
	require.Equal(t, 1, rows, "строка обязана пережить объект")

	obs, err := be.Observe(ctx, blockbackend.ObjectRef{Locator: loc, Name: object})
	require.NoError(t, err)
	require.Equal(t, blockbackend.ObservedAbsent, obs.State)

	// Второй проход забывает строку.
	c = rec.Once(ctx)
	require.Equal(t, 1, c.Forget)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM volumes WHERE id = $1`, id).Scan(&rows))
	require.Zero(t, rows)
}

func TestCycle_VanishedObjectIsReportedNotRecreated(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	bindingID, loc := seedBinding(t, pool)
	id, object := insertVolume(t, pool, bindingID, loc, 1<<30)

	be := fake.New(blockbackend.Capabilities{Snapshots: true, OnlineGrow: true})
	rec := reconciler.New(reconciler.NewStore(pool), openerFor{be}, reconciler.Config{Batch: 10})
	rec.Once(ctx)

	// Объект снят мимо нас.
	require.NoError(t, be.DeleteVolume(ctx, blockbackend.ObjectRef{Locator: loc, Name: object}))
	// Расхождение обязано попасть в обход: наблюдаемое уже не совпадает с намерением.
	_, err := pool.Exec(ctx, `UPDATE volumes SET observed_state = 'ABSENT' WHERE id = $1`, id)
	require.NoError(t, err)

	c := rec.Once(ctx)
	require.Equal(t, 1, c.Vanished)
	require.Zero(t, c.Provision, "пересоздание НЕ является починкой: данные не вернутся, "+
		"а пустой объект того же имени выглядел бы здоровым ресурсом")

	state, _, reason := volumeState(t, pool, id)
	require.Equal(t, "ERROR", state)
	require.Equal(t, string(domain.ReasonPreconditionFailed), reason)
}

func TestCycle_LeakScanCountsButNeverDeletes(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	bindingID, loc := seedBinding(t, pool)
	_, mine := insertVolume(t, pool, bindingID, loc, 1<<30)

	be := fake.New(blockbackend.Capabilities{Snapshots: true, OnlineGrow: true})
	rec := reconciler.New(reconciler.NewStore(pool), openerFor{be}, reconciler.Config{Batch: 10})
	rec.Once(ctx)

	// Чужой объект в том же локаторе: строки под ним нет.
	stray := blockbackend.ObjectRef{Locator: loc, Name: "kcother-vol00000000000000001"}
	require.NoError(t, be.CreateVolume(ctx, blockbackend.VolumeSpec{Ref: stray, SizeBytes: 1 << 20}))

	c, err := rec.ScanLeaks(ctx, be, loc)
	require.NoError(t, err)
	require.Equal(t, 2, c.Scanned, "осмотрены оба объекта — ноль находок отличимо от нуля прочитанного")
	require.Equal(t, 1, c.Leaked)

	// Чужой объект НЕ удалён: снос по собственному выводу необратим.
	obs, err := be.Observe(ctx, stray)
	require.NoError(t, err)
	require.Equal(t, blockbackend.ObservedReady, obs.State, "сверщик не сносит то, чего не заводил")

	// И свой объект тоже на месте — проба не зеленеет на пустом бэкенде.
	obs, err = be.Observe(ctx, blockbackend.ObjectRef{Locator: loc, Name: mine})
	require.NoError(t, err)
	require.Equal(t, blockbackend.ObservedReady, obs.State)
}
