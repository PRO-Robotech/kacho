// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Заморозка координаты изоляции тенант-домена (`networks.vrf_id`, миграция 0031).
//
// Предмет здесь — свойство БАЗЫ, а не пути use-case, поэтому пробы ходят СЫРЫМ
// SQL. Отдельная проба на репозиторий (`network_vrf_id_integration_test.go`,
// `TestNetwork_CIL0_03_VrfIdStableAcrossUpdate`) утверждает, что перечень столбцов
// в `UPDATE networks SET …` координату не упоминает, — то есть закрепляет
// СОГЛАШЕНИЕ одного писателя. Она останется зелёной у любого второго писателя:
// воркера, inline-пути в чужой writer-TX, административной консоли,
// восстановления из дампа. Инвариант обязан отвечать каждому из них, и именно
// это здесь и проверяется — вставкой и обновлением в обход репозитория.
//
// Почему координата обязана быть неизменной и невыдаваемой второй раз: её
// получает исполнитель датаплейна как имя изоляции. Если значение сменит
// владельца — во времени или после удаления сети, — исполнитель с отставшей
// записью применит правило к ЧУЖОЙ сети. Смена и переиспользование дают один и
// тот же исход, поэтому закрыты одной миграцией.

// assertNamesVrfSubject — отказ базы обязан НАЗЫВАТЬ предмет: следующий читатель,
// наткнувшийся на этот отказ, должен понять запрет, иначе снимет его как непонятный.
// Проверяется и класс ошибки (23514 — нарушение ограничения области значений,
// который маппер репозитория уже узнаёт, см. helpers.IsCheckViolation), и текст.
func assertNamesVrfSubject(t *testing.T, err error, what string) {
	t.Helper()
	require.Error(t, err, "%s: база обязана отвергнуть", what)

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr, "%s: ожидается ошибка PG, а не транспортная", what)
	assert.Equal(t, "23514", pgErr.Code,
		"%s: класс ошибки обязан быть 23514 (нарушение области значений), получено %s", what, pgErr.Code)
	assert.Contains(t, strings.ToLower(pgErr.Message), "vrf_id",
		"%s: сообщение обязано называть предмет запрета, получено %q", what, pgErr.Message)
}

// Смена значения у существующей строки отвергается базой — в обеих формах, в
// которых её вообще можно записать: присваиванием литерала и `= DEFAULT`.
//
// Вторая форма — не теоретическая: она проходит даже у столбца, объявленного
// `GENERATED ALWAYS AS IDENTITY` (проверено на PG 16.14 до написания миграции:
// значение сменилось со 101 на 104), поэтому одного лишь identity для заморозки
// НЕ достаточно и запрет обязан быть триггером.
//
// Рядом — два положительных контроля. Без них проба зеленела бы на триггере,
// запрещающем ЛЮБОЕ обновление сети: отрицание «отвергнуто» неотличимо от
// «отвергается всё».
func TestNetworkVrfFrozen_UpdateOfVrfIdRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)

	n := insertNetwork(t, r, "project-vrf-frozen", "net-frozen")

	var before int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT vrf_id FROM kacho_vpc.networks WHERE id = $1`, n.ID).Scan(&before))
	require.Greater(t, before, int64(0), "координата обязана быть выдана при создании")

	// Отрицание 1 — присваивание другого значения.
	_, err = pool.Exec(ctx,
		`UPDATE kacho_vpc.networks SET vrf_id = $2 WHERE id = $1`, n.ID, before+1000)
	assertNamesVrfSubject(t, err, "смена координаты на другое значение")

	// Отрицание 2 — `= DEFAULT`: выдало бы НОВОЕ значение из последовательности,
	// то есть та же смена владельца имени, только записанная иначе.
	_, err = pool.Exec(ctx,
		`UPDATE kacho_vpc.networks SET vrf_id = DEFAULT WHERE id = $1`, n.ID)
	assertNamesVrfSubject(t, err, "смена координаты через DEFAULT")

	// Ни одна из попыток не изменила значение.
	var after int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT vrf_id FROM kacho_vpc.networks WHERE id = $1`, n.ID).Scan(&after))
	assert.Equal(t, before, after, "координата обязана остаться прежней после отвергнутых попыток")

	// Положительный контроль 1 — правка ДРУГИХ полей сети проходит.
	_, err = pool.Exec(ctx,
		`UPDATE kacho_vpc.networks SET description = $2, name = $3 WHERE id = $1`,
		n.ID, "переименована", "net-frozen-renamed")
	require.NoError(t, err,
		"запрет обязан касаться смены vrf_id, а не любого обновления сети")

	// Положительный контроль 2 — упоминание столбца БЕЗ смены значения проходит.
	// Запрет ключуется на изменение, а не на присутствие столбца в SET.
	_, err = pool.Exec(ctx,
		`UPDATE kacho_vpc.networks SET vrf_id = vrf_id, description = $2 WHERE id = $1`,
		n.ID, "тот же vrf_id")
	require.NoError(t, err,
		"присваивание того же значения ничего не меняет и обязано проходить")

	require.NoError(t, pool.QueryRow(ctx,
		`SELECT vrf_id FROM kacho_vpc.networks WHERE id = $1`, n.ID).Scan(&after))
	assert.Equal(t, before, after, "положительные контроли не должны были сменить координату")
}

// Конкурентные вставки получают РАЗНЫЕ значения.
//
// Свойство держится последовательностью, а не «максимум плюс один»: второе под
// конкуренцией выдаёт одно значение дважды. Проба остаётся осмысленной и после
// 0031: выдача переезжает из DEFAULT в триггер, и она сторожит, что новый
// механизм по-прежнему выдаёт различные значения каждому писателю.
func TestNetworkVrfFrozen_ConcurrentInsertsGetDistinctValues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	const N = 16
	var (
		mu   sync.Mutex
		got  = make([]int64, 0, N)
		errs = make([]error, N)
		wg   sync.WaitGroup
	)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var v int64
			// Вставка НЕ называет vrf_id — ровно как прод-путь
			// (`repo/kacho/pg/network.go`, INSERT INTO networks (…)).
			if err := pool.QueryRow(ctx,
				`INSERT INTO kacho_vpc.networks (id, project_id, name)
				 VALUES ($1, $2, $3) RETURNING vrf_id`,
				newNetID(), "project-vrf-conc", "net-conc-"+itoa(i)).Scan(&v); err != nil {
				errs[i] = err
				return
			}
			mu.Lock()
			got = append(got, v)
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		require.NoErrorf(t, e, "конкурентная вставка %d", i)
	}

	seen := make(map[int64]bool, N)
	for _, v := range got {
		require.False(t, seen[v], "значение %d выдано дважды", v)
		seen[v] = true
	}
	require.Len(t, seen, N, "осмотрено различных значений: %d из %d вставок", len(seen), N)
}

// После удаления сети её значение больше не выдаётся — ни последовательностью,
// ни явной вставкой.
//
// Явная вставка — не гипотетический путь: до 0031 она проходила (проверено на
// PG 16.14: значение удалённой сети принималось повторно), и уникальность её не
// ловит by construction — строка, которой значение принадлежало, уже удалена.
func TestNetworkVrfFrozen_ValueNotReissuedAfterDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)

	const proj = "project-vrf-reuse"
	gone := insertNetwork(t, r, proj, "net-to-delete")
	var goneVRF int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT vrf_id FROM kacho_vpc.networks WHERE id = $1`, gone.ID).Scan(&goneVRF))

	_, err = pool.Exec(ctx, `DELETE FROM kacho_vpc.networks WHERE id = $1`, gone.ID)
	require.NoError(t, err)

	// Последовательность не возвращается назад: ни одна следующая сеть не
	// получает освободившееся значение.
	for i := 0; i < 5; i++ {
		var v int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO kacho_vpc.networks (id, project_id, name)
			 VALUES ($1, $2, $3) RETURNING vrf_id`,
			newNetID(), proj, "net-after-delete-"+itoa(i)).Scan(&v))
		assert.NotEqual(t, goneVRF, v, "значение удалённой сети выдано повторно")
		assert.Greater(t, v, goneVRF, "выдача обязана идти только вперёд")
	}

	// Явное присваивание значения удалённой сети отвергается базой.
	_, err = pool.Exec(ctx,
		`INSERT INTO kacho_vpc.networks (id, project_id, name, vrf_id)
		 VALUES ($1, $2, $3, $4)`,
		newNetID(), proj, "net-reusing-vrf", goneVRF)
	assertNamesVrfSubject(t, err, "явное переиспользование координаты удалённой сети")

	// Положительный контроль — та же вставка без vrf_id проходит. Без него
	// отрицание выше зеленело бы на триггере, запрещающем всякую вставку сети.
	var fresh int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO kacho_vpc.networks (id, project_id, name)
		 VALUES ($1, $2, $3) RETURNING vrf_id`,
		newNetID(), proj, "net-reusing-vrf-ok").Scan(&fresh))
	assert.Greater(t, fresh, goneVRF, "выдача обязана продолжиться вперёд")
}

// Значение, которое нельзя доставить исполнителю без искажения, отвергается.
//
// У этой проверки есть РЕАЛЬНЫЙ производитель входа, и проба идёт именно через
// него — исчерпание последовательности, а не подставное значение: путь чтения
// (`repo/helpers/scans.go`, ScanNetwork) приводит значение к uint32 через
// safeconv.IntToUint32, который вне диапазона НЕ падает, а ЗАЖИМАЕТ до
// 4294967295. Без границы в базе две разные сети с номерами выше диапазона
// доехали бы до исполнителя датаплейна под ОДНИМ именем — то есть ровно то
// слияние координат, против которого написана вся эта миграция, только
// произведённое чтением, а не записью.
func TestNetworkVrfFrozen_ValueBeyondDeliverableRangeRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	const maxUint32 = int64(4294967295)

	// Ставим последовательность так, чтобы СЛЕДУЮЩЕЕ значение было ровно
	// границей диапазона.
	_, err = pool.Exec(ctx,
		`SELECT setval('kacho_vpc.networks_vrf_seq', $1)`, maxUint32-1)
	require.NoError(t, err, "подготовка производителя входа")

	// Положительный контроль — граница диапазона ПРИНИМАЕТСЯ. Без него проба
	// ниже зеленела бы на границе, поставленной где угодно ниже.
	var atBoundary int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO kacho_vpc.networks (id, project_id, name)
		 VALUES ($1, $2, $3) RETURNING vrf_id`,
		newNetID(), "project-vrf-range", "net-at-boundary").Scan(&atBoundary))
	assert.Equal(t, maxUint32, atBoundary, "граница диапазона обязана приниматься")

	// Отрицание — первое значение ЗА границей отвергается, а не зажимается.
	_, err = pool.Exec(ctx,
		`INSERT INTO kacho_vpc.networks (id, project_id, name) VALUES ($1, $2, $3)`,
		newNetID(), "project-vrf-range", "net-beyond-boundary")
	assertNamesVrfSubject(t, err, "значение за границей доставимого диапазона")
}
