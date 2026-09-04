// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	vpcrepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// Учёт числа ресурсов арендатора: списание и возврат в ТОЙ ЖЕ writer-TX, что
// вставка и удаление строки ресурса; три исхода различимы; понижение предела
// ниже потребления выразимо; системные дети не списываются.
//
// Приёмка — `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S2 п.1-4.
//
// ПОЧЕМУ ПРОБА ИДЁТ ЧЕРЕЗ РЕПОЗИТОРИЙ, А НЕ ЧЕРЕЗ ГОЛЫЙ SQL. Предмет утверждения —
// «списание происходит тем же оператором, что вставка строки ресурса». Проба,
// дёргающая счётчик отдельным запросом, утверждала бы про счётчик, а не про эту
// связь, и осталась бы зелёной ровно на том дефекте, ради которого написана:
// вставка мимо учёта.

// seedQuota заводит строку учёта — то, что на живом стенде делает материализация.
//
// Здесь она подставная НАМЕРЕННО: предмет S2 — списание и отказ, а не то, откуда
// приезжает величина. Подмена при этом не снисходительнее продукта — строка
// заводится теми же столбцами и теми же ограничениями, что и настоящая.
func seedQuota(t testing.TB, ctx context.Context, pool *pgxpool.Pool, projectID, kind string, limit int64) {
	t.Helper()
	const q = `INSERT INTO kacho_vpc.project_resource_quotas
	              (carrier_type, carrier_id, kind, used, limit_value,
	               source_scope, source_scope_id, limit_revision, account_id)
	           VALUES ('project', $1, $2, 0, $3, 'DEFAULT', '', 1, 'acc-seed')`
	_, err := pool.Exec(ctx, q, projectID, kind, limit)
	require.NoError(t, err)
}

// quotaUsed читает потребление; -1 означает «строки нет».
func quotaUsed(t testing.TB, ctx context.Context, pool *pgxpool.Pool, projectID, kind string) int64 {
	t.Helper()
	const q = `SELECT used FROM kacho_vpc.project_resource_quotas
	            WHERE carrier_type = 'project' AND carrier_id = $1 AND kind = $2`
	var used int64
	switch err := pool.QueryRow(ctx, q, projectID, kind).Scan(&used); {
	case err == pgx.ErrNoRows:
		return -1
	case err != nil:
		t.Fatalf("read used: %v", err)
	}
	return used
}

func quotaTestPool(t testing.TB, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	return pool
}

// TestQuota_ChargeOnInsert_RefundOnDelete — списание на вставке, возврат на
// удалении, обе в транзакции самой мутации.
func TestQuota_ChargeOnInsert_RefundOnDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-charge"
	seedQuota(t, ctx, pool, project, "vpc.network", 4)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	created, err := w.Networks().Insert(ctx, newNetwork(project, "net-charge"))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	assert.Equal(t, int64(1), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"вставка сети обязана списать одно место")

	// Откат: удаление возвращает место в той же транзакции.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	require.NoError(t, w2.Networks().Delete(ctx, created.ID))
	require.NoError(t, w2.Commit())

	assert.Equal(t, int64(0), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"удаление сети обязано вернуть место")
}

// TestQuota_ChargeIsRolledBackWithItsTransaction — списание принадлежит
// транзакции мутации, а не живёт отдельно.
//
// Без этого утверждения «списание в той же writer-TX» неотличимо от «списание
// рядом»: на успешном пути обе формы дают одно и то же число.
func TestQuota_ChargeIsRolledBackWithItsTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-rollback"
	seedQuota(t, ctx, pool, project, "vpc.network", 4)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Networks().Insert(ctx, newNetwork(project, "net-rollback"))
	require.NoError(t, err)
	w.Abort() // мутация сорвана

	assert.Equal(t, int64(0), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"сорванная вставка не могла списать место")
}

// TestQuota_ExhaustedIsRefused_AndDistinctFromNotProvisioned — три исхода V2-3
// различимы: место есть · место кончилось · потолок не назван.
//
// Отрицания стоят в паре с положительным контролем в ЭТОМ ЖЕ тесте: без него
// «отвергнуто» неотличимо от «отвергается всё подряд».
func TestQuota_ExhaustedIsRefused_AndDistinctFromNotProvisioned(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-exhaust"
	seedQuota(t, ctx, pool, project, "vpc.network", 1)

	// Положительный контроль: под пределом проходит.
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.Networks().Insert(ctx, newNetwork(project, "net-under"))
	require.NoError(t, err, "создание под пределом обязано проходить")
	require.NoError(t, w.Commit())

	// Отрицание: сверх предела отвергается, и отказ ИМЕНУЕТ предмет и предел.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.Networks().Insert(ctx, newNetwork(project, "net-over"))
	require.Error(t, err, "создание сверх предела обязано отвергаться")
	assert.ErrorIs(t, err, vpcrepo.ErrQuotaExceeded)
	assert.Contains(t, err.Error(),
		fmt.Sprintf("project %s has reached its limit of 1 vpc.network", project),
		"текст отказа — часть контракта: называет носителя, предел и вид")

	// Третий исход: потолок не назван ни на одной области — ОТКАЗ, и он ОТЛИЧИМ.
	const bare = "prj-quota-bare"
	w3, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w3.Abort()
	_, err = w3.Networks().Insert(ctx, newNetwork(bare, "net-bare"))
	require.Error(t, err, "«не сказано» обязано быть отказом, а не разрешением")
	assert.ErrorIs(t, err, vpcrepo.ErrQuotaNotProvisioned)
	assert.NotErrorIs(t, err, vpcrepo.ErrQuotaExceeded,
		"исчерпание и «потолок не назван» — разные исходы, а не один")
}

// TestQuota_LoweringBelowUsageIsExpressible — Р6: понижение предела ниже
// потребления законно; новые нельзя, старые живут, удаление работает.
//
// Это ровно то, что делал невыразимым `CHECK (used <= limit_value)` прецедента
// compute.
func TestQuota_LoweringBelowUsageIsExpressible(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-lower"
	seedQuota(t, ctx, pool, project, "vpc.network", 3)

	created := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		w, err := r.Writer(ctx)
		require.NoError(t, err)
		rec, err := w.Networks().Insert(ctx, newNetwork(project, fmt.Sprintf("net-low-%d", i)))
		require.NoError(t, err)
		require.NoError(t, w.Commit())
		created = append(created, rec.ID)
	}
	require.Equal(t, int64(3), quotaUsed(t, ctx, pool, project, "vpc.network"))

	// Понижение ниже потребления обязано ЗАПИСАТЬСЯ.
	_, err := pool.Exec(ctx,
		`UPDATE kacho_vpc.project_resource_quotas SET limit_value = 1
		  WHERE carrier_type = 'project' AND carrier_id = $1 AND kind = 'vpc.network'`, project)
	require.NoError(t, err, "понижение предела ниже потребления обязано быть выразимо")

	// Новые нельзя.
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.Networks().Insert(ctx, newNetwork(project, "net-after-lower"))
	require.ErrorIs(t, err, vpcrepo.ErrQuotaExceeded)

	// Старые живут и удаляются; удаление снижает потребление.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	require.NoError(t, w2.Networks().Delete(ctx, created[0]))
	require.NoError(t, w2.Commit())
	assert.Equal(t, int64(2), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"удаление работает и при потреблении выше предела")
}

// TestQuota_ConcurrentChargeAtLastSlot — за последнее место выигрывает ровно
// один, и это свойство ЕДИНСТВЕННОГО оператора, берущего блокировку строки.
//
// Парный положительный контроль (предел N, N создателей — проходят все) —
// в TestQuota_ConcurrentChargeUnderLimit ниже: без него отрицание зеленело бы
// на списании, отвергающем всё подряд.
func TestQuota_ConcurrentChargeAtLastSlot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-race"
	const writers = 8
	seedQuota(t, ctx, pool, project, "vpc.network", writers-1)

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w, err := r.Writer(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			defer w.Abort()
			if _, err := w.Networks().Insert(ctx, newNetwork(project, fmt.Sprintf("net-race-%d", i))); err != nil {
				errs[i] = err
				return
			}
			errs[i] = w.Commit()
		}(i)
	}
	wg.Wait()

	refused := 0
	for _, err := range errs {
		if err != nil {
			assert.ErrorIs(t, err, vpcrepo.ErrQuotaExceeded,
				"единственный законный отказ здесь — исчерпание предела")
			refused++
		}
	}
	assert.Equal(t, 1, refused, "ровно один обязан не получить последнее место")
	assert.Equal(t, int64(writers-1), quotaUsed(t, ctx, pool, project, "vpc.network"))
}

// TestQuota_SystemChildrenAreNotCharged — Р7: системные дети сети не тратят
// предел арендатора, а СВОИ — тратят.
//
// ПОЧЕМУ ЭТОТ ТЕСТ ТРЕБУЕТ НОВОГО ПРИЗНАКА У ТАБЛИЦ МАРШРУТИЗАЦИИ. Системность
// группы правил читается её собственным столбцом (`default_for_network`), а
// системность таблицы маршрутизации сегодня выражена только ссылкой НА РОДИТЕЛЕ
// (`networks.default_route_table_id`), которая на момент вставки ребёнка ещё не
// проставлена: `default_rt.go` сначала вставляет строку и лишь потом линкует её
// (`Insert` → `SetDefaultRouteTableID`). Триггер, спрашивающий родителя, увидел
// бы «не системная» и списал бы место — то есть предел на таблицы
// маршрутизации молча стал бы пределом на сети.
func TestQuota_SystemChildrenAreNotCharged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-system"
	seedQuota(t, ctx, pool, project, "vpc.network", 4)
	seedQuota(t, ctx, pool, project, "vpc.securityGroup", 1)
	seedQuota(t, ctx, pool, project, "vpc.routeTable", 1)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	net, err := w.Networks().Insert(ctx, newNetwork(project, "net-system"))
	require.NoError(t, err)

	// Системные дети — ровно те, что заводит Network.Create.
	_, err = w.SecurityGroups().Insert(ctx, &domain.SecurityGroup{
		ID:                ids.NewID(ids.PrefixSecurityGroup),
		ProjectID:         project,
		Name:              domain.RcNameVPC("default"),
		Description:       domain.RcDescription(""),
		Labels:            domain.LabelsFromMap(nil),
		NetworkID:         net.ID,
		DefaultForNetwork: true,
	})
	require.NoError(t, err, "системная группа правил не может упереться в предел арендатора")

	_, err = w.RouteTables().Insert(ctx, &domain.RouteTable{
		ID:          ids.NewID(ids.PrefixRouteTable),
		ProjectID:   project,
		Name:        domain.RcNameVPC("default"),
		Description: domain.RcDescription(""),
		Labels:      domain.LabelsFromMap(nil),
		NetworkID:   net.ID,
		SystemOwned: true,
	})
	require.NoError(t, err, "системная таблица маршрутизации не может упереться в предел арендатора")
	require.NoError(t, w.Commit())

	assert.Equal(t, int64(1), quotaUsed(t, ctx, pool, project, "vpc.network"))
	assert.Equal(t, int64(0), quotaUsed(t, ctx, pool, project, "vpc.securityGroup"),
		"системная группа правил не списывается")
	assert.Equal(t, int64(0), quotaUsed(t, ctx, pool, project, "vpc.routeTable"),
		"системная таблица маршрутизации не списывается")

	// Положительный контроль: СВОЯ группа арендатора списывается, и предел в 1
	// действительно действует — иначе «ноль» выше означал бы «не считается вовсе».
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.SecurityGroups().Insert(ctx, &domain.SecurityGroup{
		ID:          ids.NewID(ids.PrefixSecurityGroup),
		ProjectID:   project,
		Name:        domain.RcNameVPC("own-sg"),
		Description: domain.RcDescription(""),
		Labels:      domain.LabelsFromMap(nil),
		NetworkID:   net.ID,
	})
	require.NoError(t, err)
	require.NoError(t, w2.Commit())
	assert.Equal(t, int64(1), quotaUsed(t, ctx, pool, project, "vpc.securityGroup"),
		"своя группа правил обязана списываться")

	w3, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w3.Abort()
	_, err = w3.SecurityGroups().Insert(ctx, &domain.SecurityGroup{
		ID:          ids.NewID(ids.PrefixSecurityGroup),
		ProjectID:   project,
		Name:        domain.RcNameVPC("own-sg-2"),
		Description: domain.RcDescription(""),
		Labels:      domain.LabelsFromMap(nil),
		NetworkID:   net.ID,
	})
	require.ErrorIs(t, err, vpcrepo.ErrQuotaExceeded,
		"вторая своя группа обязана упереться в предел 1")
}

// TestQuota_ConcurrentChargeUnderLimit — положительный контроль к предыдущему.
func TestQuota_ConcurrentChargeUnderLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-race-ok"
	const writers = 8
	seedQuota(t, ctx, pool, project, "vpc.network", writers)

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w, err := r.Writer(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			defer w.Abort()
			if _, err := w.Networks().Insert(ctx, newNetwork(project, fmt.Sprintf("net-ok-%d", i))); err != nil {
				errs[i] = err
				return
			}
			errs[i] = w.Commit()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "под пределом обязаны пройти все; писатель %d", i)
	}
	assert.Equal(t, int64(writers), quotaUsed(t, ctx, pool, project, "vpc.network"))
}
