// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// Вложенный потолок: сколько детей помещается в ОДНОЙ сети.
//
// # Что здесь утверждается и в каком порядке
//
// Две пробы, и вторая важнее первой. Первая говорит, что предел РАБОТАЕТ; вторая
// — что он не сломал живой путь. Порядок именно такой, потому что сломать здесь
// легче, чем ограничить: строку учёта родителя заводит триггер жизненного цикла,
// и у сети, созданной ДО появления этого механизма, её нет. Списание, отвечающее
// на её отсутствие отказом, отвергло бы создание подсети в каждой уже живущей
// сети — то есть предел, введённый ради защиты, отнял бы работающую функцию.

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	vpcrepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// seedNestedDefault заводит ПРОЕКТНЫЙ резолв вложенной величины — то, что на
// живом стенде делает материализация полосы учёта.
func seedNestedDefault(
	t testing.TB, ctx context.Context, pool *pgxpool.Pool, projectID, kind string, limit int64,
) {
	t.Helper()
	const q = `INSERT INTO kacho_vpc.nested_quota_defaults
	              (project_id, kind, limit_value, source_scope, source_scope_id,
	               limit_revision, account_id)
	           VALUES ($1, $2, $3, 'DEFAULT', '', 1, 'acc-seed')
	           ON CONFLICT (project_id, kind) DO UPDATE SET limit_value = EXCLUDED.limit_value`
	_, err := pool.Exec(ctx, q, projectID, kind, limit)
	require.NoError(t, err)
}

// nestedUsed читает потребление строки учёта РОДИТЕЛЯ; -1 означает «строки нет».
func nestedUsed(
	t testing.TB, ctx context.Context, pool *pgxpool.Pool, parentID, kind string,
) int64 {
	t.Helper()
	const q = `SELECT used FROM kacho_vpc.project_resource_quotas
	            WHERE carrier_type = $1 AND carrier_id = $2 AND kind = $1`
	var used int64
	switch err := pool.QueryRow(ctx, q, kind, parentID).Scan(&used); {
	case err != nil && err.Error() == "no rows in result set":
		return -1
	case err != nil:
		t.Fatalf("read nested used: %v", err)
	}
	return used
}

// TestQuotaNested_SubnetsAreCountedInTheirNetwork — предел, который
// ОГРАНИЧИВАЕТ.
//
// Проектный потолок здесь заведомо щедрый: утверждение — про ось «в одной сети»,
// и без её отделения проба зеленела бы на проектном пределе.
func TestQuotaNested_SubnetsAreCountedInTheirNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-nested-subnet"
	seedQuota(t, ctx, pool, project, "vpc.network", 8)
	seedQuota(t, ctx, pool, project, "vpc.subnet", 100) // проектный предел не должен мешать
	seedNestedDefault(t, ctx, pool, project, "vpc.network.subnet", 2)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	net, err := w.Networks().Insert(ctx, newNetwork(project, "net-nested"))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	require.EqualValues(t, 0, nestedUsed(t, ctx, pool, net.ID, "vpc.network.subnet"),
		"строка учёта сети обязана заводиться ВМЕСТЕ с сетью: иначе списывать нечего, "+
			"и предел не наступит никогда")

	for i := 1; i <= 2; i++ {
		wi, werr := r.Writer(ctx)
		require.NoError(t, werr)
		_, werr = wi.Subnets().Insert(ctx,
			newSubnet(project, fmt.Sprintf("sub-nested-%d", i), net.ID, "ru-central1-a",
				[]string{fmt.Sprintf("10.60.%d.0/24", i)}))
		require.NoErrorf(t, werr, "подсеть %d из двух обязана пройти", i)
		require.NoError(t, wi.Commit())
		wi.Abort()
	}

	require.EqualValues(t, 2, nestedUsed(t, ctx, pool, net.ID, "vpc.network.subnet"))

	w3, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w3.Abort()
	_, err = w3.Subnets().Insert(ctx,
		newSubnet(project, "sub-nested-3", net.ID, "ru-central1-a", []string{"10.60.3.0/24"}))
	require.Error(t, err,
		"третья подсеть в сети с пределом два прошла: ось «в одной сети» не ограничена, "+
			"и одна сеть способна вобрать весь проектный потолок")

	// Утверждается то, что получает ВЫЗЫВАЮЩИЙ, а не то, что произвела база:
	// репозиторий переводит отказ единственного производителя в свой sentinel, и
	// проба, ждущая ошибку драйвера, утверждала бы про слой, до которого
	// вызывающий не доходит.
	require.ErrorIs(t, err, vpcrepo.ErrQuotaExceeded,
		"отказ вложенной оси обязан приходить тем же признаком, что и проектной: "+
			"второй sentinel заставил бы вызывающего различать их вручную")
	require.Contains(t, err.Error(), "vpc.network.subnet",
		"отказ обязан называть НОСИТЕЛЯ и вид: арендатор, упёршийся в предел сети, "+
			"иначе пойдёт поднимать проектный")
	require.Contains(t, err.Error(), net.ID,
		"отказ обязан называть КОНКРЕТНУЮ сеть: у арендатора их много, и «какая-то "+
			"из них полна» не говорит, что делать")
}

// TestQuotaNested_SubnetInAPreexistingNetworkStillPasses — живой путь, который
// вложенная ось не вправе сломать.
//
// Сеть, созданная ДО появления механизма, строки учёта не имеет и получить её
// задним числом не может: триггер жизненного цикла срабатывает на вставке сети,
// а она уже случилась. Списание, отвечающее на отсутствие строки ОТКАЗОМ,
// отвергло бы создание подсети в каждой такой сети — то есть у всех арендаторов
// сразу, и ровно в тот момент, когда предел вводился ради их защиты.
//
// Проба воспроизводит это состояние буквально: строка учёта сети снимается после
// её создания, и подсеть обязана пройти.
func TestQuotaNested_SubnetInAPreexistingNetworkStillPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-nested-legacy"
	seedQuota(t, ctx, pool, project, "vpc.network", 8)
	seedQuota(t, ctx, pool, project, "vpc.subnet", 100)
	seedNestedDefault(t, ctx, pool, project, "vpc.network.subnet", 2)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	net, err := w.Networks().Insert(ctx, newNetwork(project, "net-legacy"))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	// Приводим сеть к состоянию «создана до механизма»: строки учёта у неё нет.
	_, err = pool.Exec(ctx,
		`DELETE FROM kacho_vpc.project_resource_quotas
		  WHERE carrier_type = 'vpc.network.subnet' AND carrier_id = $1`, net.ID)
	require.NoError(t, err)
	require.EqualValues(t, -1, nestedUsed(t, ctx, pool, net.ID, "vpc.network.subnet"),
		"предпосылка пробы сломана: строка учёта сети всё ещё на месте, "+
			"и проба утверждала бы не о том состоянии")

	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.Subnets().Insert(ctx,
		newSubnet(project, "sub-legacy", net.ID, "ru-central1-a", []string{"10.61.1.0/24"}))
	require.NoError(t, err,
		"подсеть в сети, созданной до механизма, отвергнута: вложенный предел отнял "+
			"работающую функцию у каждого существующего арендатора")
	require.NoError(t, w2.Commit())

	// И проектная ось при этом продолжает считать: пропуск касается ТОЛЬКО
	// вложенной оси, а не учёта вообще.
	require.EqualValues(t, 1, quotaUsed(t, ctx, pool, project, "vpc.subnet"),
		"проектное списание пропало вместе с вложенным: пропущена не одна ось, а обе")
}
