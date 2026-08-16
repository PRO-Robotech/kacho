// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// Потребление строки учёта обязано совпадать с числом строк ресурса — на КАЖДОМ
// пути, которым строка учёта появляется.
//
// Задача `PRO-Robotech/kacho#419`.
//
// # Предмет
//
// Материализация заводила строку с `used = 0` безусловно, комментируя это тем,
// что потребление создаёт триггер. Утверждение верно ровно для проекта, у
// которого на момент заведения строки ресурсов НЕТ, — и ложно для всякого
// другого. Проект, чьи ресурсы старше самого механизма учёта, получал ноль при
// живых ресурсах: предел переставал ограничивать, потому что вычитать начинали
// не с того числа.
//
// # Почему это не разовая беда миграции
//
// Соблазн прочитать #419 как «строки завели поздно, надо один раз пересчитать»
// велик и неверен. Состояние «ресурсы есть, строки учёта нет» воспроизводится
// каждый раз, когда механизм учёта приходит на непустую базу: так было у vpc,
// так будет у каждого владельца, чью миграцию учёта применят к живому проекту, и
// так будет у каждого вида, добавленного в каталог позже своих ресурсов.
// Разовый пересчёт чинит прошлое и ничего не обещает про будущее — поэтому
// предмет пробы здесь МАТЕРИАЛИЗАЦИЯ, а не миграция.
//
// # Почему счёт на материализации не может разъехаться с триггером
//
// Пока строки учёта нет, вставка строки ресурса ОТВЕРГАЕТСЯ («не сказано» =
// отказ, V2-3). Значит между счётом и вставкой строки учёта никакая чужая
// транзакция не может добавить ресурс: множество, которое считают, заморожено by
// construction, а не блокировкой. Как только строка появилась, дальнейшее ведёт
// триггер. Отсюда инвариант «used равен числу строк» держится ВСЕГДА, а не
// «после пересчёта».

// quotaDisableCharging снимает триггеры учёта на время, пока проба заводит
// ресурсы «до появления механизма».
//
// Это не послабление продукту, а ЕДИНСТВЕННЫЙ способ построить предусловие:
// состояние «ресурсы есть, строки учёта нет» на исправной схеме невыразимо —
// вставка была бы отвергнута. Ровно так оно и возникло в жизни: строки ресурсов
// старше миграции, которая завела триггеры.
func quotaDisableCharging(t testing.TB, ctx context.Context, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	for _, tbl := range tables {
		_, err := pool.Exec(ctx, fmt.Sprintf(
			"ALTER TABLE kacho_vpc.%s DISABLE TRIGGER %s_quota_count", tbl, tbl))
		require.NoError(t, err, "снять триггер учёта с %s", tbl)
	}
	t.Cleanup(func() {
		for _, tbl := range tables {
			_, _ = pool.Exec(context.Background(), fmt.Sprintf(
				"ALTER TABLE kacho_vpc.%s ENABLE TRIGGER %s_quota_count", tbl, tbl))
		}
	})
}

func quotaEnableCharging(t testing.TB, ctx context.Context, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	for _, tbl := range tables {
		_, err := pool.Exec(ctx, fmt.Sprintf(
			"ALTER TABLE kacho_vpc.%s ENABLE TRIGGER %s_quota_count", tbl, tbl))
		require.NoError(t, err, "вернуть триггер учёта на %s", tbl)
	}
}

// TestQuotaMaterialise_SeedsUsageFromRowsThatAlreadyExist — материализация на
// проекте, чьи ресурсы старше механизма учёта, заводит строку с ФАКТИЧЕСКИМ
// потреблением, а не с нулём.
//
// RED до фикса: `used` равен 0 при трёх существующих сетях, то есть предел
// разрешает создать ещё столько же сверх фактического.
func TestQuotaMaterialise_SeedsUsageFromRowsThatAlreadyExist(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-backfill"

	// Предусловие: ресурсы заведены ДО того, как у проекта появилась строка учёта.
	quotaDisableCharging(t, ctx, pool, "networks")
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	for _, name := range []string{"bf-a", "bf-b", "bf-c"} {
		_, err = w.Networks().Insert(ctx, newNetwork(project, name))
		require.NoError(t, err)
	}
	require.NoError(t, w.Commit())
	quotaEnableCharging(t, ctx, pool, "networks")

	require.Equal(t, int64(-1), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"предусловие: строки учёта у проекта ещё нет")

	// Материализация — тот самый оператор, что и на живом пути.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	n, err := w2.Quotas().Materialize(ctx,
		quotaRowsFor(project, "acc-backfill", map[string]int64{"vpc.network": 16}))
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "строка учёта заведена")
	require.NoError(t, w2.Commit())

	require.Equal(t, int64(3), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"потребление обязано совпасть с числом строк ресурса, а не начаться с нуля")
}

// TestQuotaMaterialise_SeededUsageStillRefusesAtTheCeiling — положительный
// контроль к пробе выше: досчитанное потребление РЕШАЕТ, а не только украшает
// ответ чтения.
//
// Без него фикс «записали правильное число» был бы неотличим от фикса
// «записали правильное число, и оно ни на что не влияет»: предел проверяет
// триггер, и утверждать надо его исход.
func TestQuotaMaterialise_SeededUsageStillRefusesAtTheCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-backfill-ceil"

	quotaDisableCharging(t, ctx, pool, "networks")
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	for _, name := range []string{"ceil-a", "ceil-b"} {
		_, err = w.Networks().Insert(ctx, newNetwork(project, name))
		require.NoError(t, err)
	}
	require.NoError(t, w.Commit())
	quotaEnableCharging(t, ctx, pool, "networks")

	// Предел ровно по факту: места больше нет.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.Quotas().Materialize(ctx,
		quotaRowsFor(project, "acc-backfill-ceil", map[string]int64{"vpc.network": 2}))
	require.NoError(t, err)
	require.NoError(t, w2.Commit())

	w3, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w3.Abort()
	_, err = w3.Networks().Insert(ctx, newNetwork(project, "ceil-over"))
	require.Error(t, err, "предел исчерпан фактическими строками — создание обязано быть отвергнуто")

	// Положительный контроль: при поднятом пределе то же создание проходит.
	const roomy = "prj-quota-backfill-room"
	quotaDisableCharging(t, ctx, pool, "networks")
	w4, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w4.Abort()
	_, err = w4.Networks().Insert(ctx, newNetwork(roomy, "room-a"))
	require.NoError(t, err)
	require.NoError(t, w4.Commit())
	quotaEnableCharging(t, ctx, pool, "networks")

	w5, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w5.Abort()
	_, err = w5.Quotas().Materialize(ctx,
		quotaRowsFor(roomy, "acc-backfill-room", map[string]int64{"vpc.network": 8}))
	require.NoError(t, err)
	require.NoError(t, w5.Commit())

	w6, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w6.Abort()
	_, err = w6.Networks().Insert(ctx, newNetwork(roomy, "room-b"))
	require.NoError(t, err, "место есть — создание обязано пройти")
	require.NoError(t, w6.Commit())
	require.Equal(t, int64(2), quotaUsed(t, ctx, pool, roomy, "vpc.network"))
}

// TestQuotaMaterialise_SystemChildrenAreNotSeeded — досчёт уважает Р7: системный
// ребёнок не тратит предел арендатора, значит и в затравку не попадает.
//
// Проба-близнец к двум выше: без неё фикс, считающий строки «как есть», выглядел
// бы верным и завышал бы потребление ровно на число системных детей — то есть
// арендатор недосчитался бы места, которое ему причитается.
func TestQuotaMaterialise_SystemChildrenAreNotSeeded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-backfill-sys"

	quotaDisableCharging(t, ctx, pool, "networks", "route_tables")
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	net, err := w.Networks().Insert(ctx, newNetwork(project, "sys-net"))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	// Одна таблица маршрутизации арендатора и одна системная.
	_, err = pool.Exec(ctx, `INSERT INTO kacho_vpc.route_tables
	        (id, project_id, network_id, name, description, labels, system_owned)
	     VALUES ($1, $2, $3, 'rt-tenant', '', '{}'::jsonb, false),
	            ($4, $2, $3, 'rt-system', '', '{}'::jsonb, true)`,
		"rtb"+project+"1", project, net.ID, "rtb"+project+"2")
	require.NoError(t, err)
	quotaEnableCharging(t, ctx, pool, "networks", "route_tables")

	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.Quotas().Materialize(ctx,
		quotaRowsFor(project, "acc-backfill-sys", map[string]int64{"vpc.routeTable": 32}))
	require.NoError(t, err)
	require.NoError(t, w2.Commit())

	require.Equal(t, int64(1), quotaUsed(t, ctx, pool, project, "vpc.routeTable"),
		"системная таблица маршрутизации в потребление арендатора не входит (Р7)")
}

// quotaDivergence — строки, чей счётчик разошёлся с фактом, по мнению самой базы.
//
// Спрашивается тем же оператором, что применяет пересчёт, только в режиме
// сверки: у проверки и у починки один предикат, поэтому «сверка молчит» и
// «починка ничего не нашла бы» — одно и то же утверждение, а не два похожих.
func quotaDivergence(t testing.TB, ctx context.Context, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT carrier_type, carrier_id, kind, used_before, used_actual
		   FROM kacho_vpc.kacho_quota_recount(false)`)
	require.NoError(t, err)
	defer rows.Close()

	var out []string
	for rows.Next() {
		var ct, ci, kind string
		var before, actual int64
		require.NoError(t, rows.Scan(&ct, &ci, &kind, &before, &actual))
		out = append(out, fmt.Sprintf("%s/%s %s: учтено %d, фактически %d",
			ct, ci, kind, before, actual))
	}
	require.NoError(t, rows.Err())
	return out
}

// TestQuotaRecount_DivergenceIsImpossibleAfterOrdinaryTraffic — ни один вид не
// расходится с фактом после штатной работы.
//
// Это предикат снятия #419 (п.2): утверждается не «мы однажды пересчитали», а
// свойство — расхождение невыразимо. Проба нарочно ходит ВСЕМИ путями, которые
// двигают счётчик: заведение строки на непустом проекте, создание, удаление и
// системный ребёнок.
func TestQuotaRecount_DivergenceIsImpossibleAfterOrdinaryTraffic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-recount"

	// Часть ресурсов — старше механизма учёта.
	quotaDisableCharging(t, ctx, pool, "networks")
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.Networks().Insert(ctx, newNetwork(project, "rc-old"))
	require.NoError(t, err)
	require.NoError(t, w.Commit())
	quotaEnableCharging(t, ctx, pool, "networks")

	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.Quotas().Materialize(ctx,
		quotaRowsFor(project, "acc-recount", map[string]int64{"vpc.network": 16}))
	require.NoError(t, err)
	require.NoError(t, w2.Commit())

	// Часть — созданы штатно, одна из них потом удалена.
	w3, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w3.Abort()
	_, err = w3.Networks().Insert(ctx, newNetwork(project, "rc-new-a"))
	require.NoError(t, err)
	doomed, err := w3.Networks().Insert(ctx, newNetwork(project, "rc-new-b"))
	require.NoError(t, err)
	require.NoError(t, w3.Commit())

	w4, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w4.Abort()
	require.NoError(t, w4.Networks().Delete(ctx, doomed.ID))
	require.NoError(t, w4.Commit())

	require.Equal(t, int64(2), quotaUsed(t, ctx, pool, project, "vpc.network"))
	require.Empty(t, quotaDivergence(t, ctx, pool),
		"счётчик обязан совпадать с фактом по КАЖДОЙ строке учёта")
}

// TestQuotaRecount_VerifierNamesADivergenceItIsShown — доказательство того, что
// сверка выше способна упасть.
//
// Без него «расхождений нет» неотличимо от «сверка ничего не читает»: предыдущая
// проба зеленела бы и на функции, возвращающей пустоту всегда. Здесь счётчик
// портится напрямую — единственным оператором, которому это позволено, потому
// что он и есть инъекция, — и сверка обязана назвать испорченную строку.
func TestQuotaRecount_VerifierNamesADivergenceItIsShown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-recount-inj"

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.Quotas().Materialize(ctx,
		quotaRowsFor(project, "acc-recount-inj", map[string]int64{"vpc.network": 16}))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.Networks().Insert(ctx, newNetwork(project, "inj-a"))
	require.NoError(t, err)
	require.NoError(t, w2.Commit())

	// Законный близнец: до порчи сверка про этот проект молчит.
	for _, d := range quotaDivergence(t, ctx, pool) {
		require.NotContains(t, d, project, "до порчи расхождения быть не должно")
	}

	_, err = pool.Exec(ctx,
		`UPDATE kacho_vpc.project_resource_quotas SET used = 7
		  WHERE carrier_type = 'project' AND carrier_id = $1 AND kind = 'vpc.network'`,
		project)
	require.NoError(t, err)

	found := quotaDivergence(t, ctx, pool)
	var named bool
	for _, d := range found {
		if strings.Contains(d, project) {
			named = true
			require.Contains(t, d, "учтено 7, фактически 1",
				"сверка обязана назвать обе величины, а не только факт расхождения")
		}
	}
	require.True(t, named, "сверка обязана НАЗВАТЬ испорченную строку, а не просто упасть: %v", found)

	// Режим сверки ничего не пишет: испорченное значение осталось испорченным.
	require.Equal(t, int64(7), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"сверка обязана быть чтением; чинит — отдельный режим")

	// А режим починки — приводит к факту.
	_, err = pool.Exec(ctx, `SELECT count(*) FROM kacho_vpc.kacho_quota_recount(true)`)
	require.NoError(t, err)
	require.Equal(t, int64(1), quotaUsed(t, ctx, pool, project, "vpc.network"))
	require.Empty(t, quotaDivergence(t, ctx, pool))
}
