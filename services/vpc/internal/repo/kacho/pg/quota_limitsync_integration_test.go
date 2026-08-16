// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/quota"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// Смена величины ДОЕЗЖАЕТ до живого проекта.
//
// Задача `PRO-Robotech/kacho#410`.
//
// # Предмет
//
// Строка снимка заводилась один раз — на первой мутации в проекте — и дальше не
// обновлялась ничем: промах больше не случался, запись шла с
// `ON CONFLICT DO NOTHING`, дельту не тянул никто, ревизия писалась константой.
// Администратор менял потолок, ответ был успешным, значение у владельца величин
// менялось — и до проекта не доезжало ни при каких условиях. Ни поднятие, ни
// понижение.
//
// # Что утверждается здесь, а что уровнем выше
//
// Здесь — ОПЕРАТОРЫ: кого касается изменение, какое старшинство побеждает и что
// происходит с потреблением. Порядок прохода (курсор двигается после
// применения) утверждается на уровне синхронизатора подставной проекцией: она
// про базу не знает, а эти пробы не знают про курсор.

func quotaProjection(t testing.TB, pool *pgxpool.Pool) *quota.PgProjection {
	t.Helper()
	p, err := quota.NewPgProjection(pool, "kacho_vpc")
	require.NoError(t, err)
	return p
}

// quotaSnapshot читает снимок величины строки учёта.
func quotaSnapshot(t testing.TB, ctx context.Context, pool *pgxpool.Pool, project, kind string) (
	limit int64, scope, scopeID string, revision int64, syncedAt time.Time, exists bool,
) {
	t.Helper()
	const q = `SELECT limit_value, source_scope, source_scope_id, limit_revision, synced_at
	             FROM kacho_vpc.project_resource_quotas
	            WHERE carrier_type = 'project' AND carrier_id = $1 AND kind = $2`
	err := pool.QueryRow(ctx, q, project, kind).Scan(&limit, &scope, &scopeID, &revision, &syncedAt)
	if err != nil {
		return 0, "", "", 0, time.Time{}, false
	}
	return limit, scope, scopeID, revision, syncedAt, true
}

// TestLimitSync_ProjectChangeReachesTheLivingRow — предикат снятия задачи:
// изменение доезжает до проекта, у которого строка учёта УЖЕ есть.
func TestLimitSync_ProjectChangeReachesTheLivingRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)
	proj := quotaProjection(t, pool)

	const project = "prj-limitsync-project"

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.Quotas().Materialize(ctx,
		quotaRowsFor(project, "acc-limitsync", map[string]int64{"vpc.network": 16}))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	_, _, _, revBefore, syncedBefore, ok := quotaSnapshot(t, ctx, pool, project, "vpc.network")
	require.True(t, ok)
	require.Zero(t, revBefore, "предусловие: ревизия снимка ещё не назначена")

	n, err := proj.ApplyChange(ctx, quota.Change{
		Kind: "vpc.network", Scope: quota.ScopeProject, ScopeID: project, Value: 64, Revision: 12,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "изменение обязано тронуть ровно строку этого проекта")

	limit, scope, scopeID, rev, syncedAfter, ok := quotaSnapshot(t, ctx, pool, project, "vpc.network")
	require.True(t, ok)
	require.Equal(t, int64(64), limit, "величина доехала")
	require.Equal(t, "PROJECT", scope)
	require.Equal(t, project, scopeID)
	require.Equal(t, int64(12), rev, "ревизия снимка равна ревизии применённой величины, а не нулю")
	require.True(t, syncedAfter.After(syncedBefore), "отметка синхронизации обязана двигаться")

	// Поднятый предел РЕШАЕТ, а не только показывается: создаём сверх прежнего.
	// Семнадцать — на единицу больше прежней величины 16, то есть последнее
	// создание проходит ТОЛЬКО потому, что дельта доехала.
	for i := 0; i < 17; i++ {
		w2, err := r.Writer(ctx)
		require.NoError(t, err)
		_, err = w2.Networks().Insert(ctx, newNetwork(project, fmt.Sprintf("ls-%02d", i)))
		require.NoError(t, err, "создание №%d в пределах поднятой величины обязано проходить", i+1)
		require.NoError(t, w2.Commit())
	}
	require.Equal(t, int64(17), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"семнадцать больше прежних шестнадцати — предел действительно поднят")
}

// TestLimitSync_AccountChangeReachesEveryProjectOfThatAccount — аккаунтная
// дельта адресуется зеркалом аккаунта.
//
// Ради этого зеркало и заведено: без него аккаунтное изменение адресовалось бы
// пересчётом ВСЕХ строк вида, то есть всплеском вызовов, пропорциональным числу
// проектов, на каждое административное действие.
func TestLimitSync_AccountChangeReachesEveryProjectOfThatAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)
	proj := quotaProjection(t, pool)

	const ours, theirs = "acc-ls-ours", "acc-ls-theirs"
	mine := []string{"prj-ls-acc-a", "prj-ls-acc-b"}

	for _, p := range mine {
		w, err := r.Writer(ctx)
		require.NoError(t, err)
		_, err = w.Quotas().Materialize(ctx, quotaRowsFor(p, ours, map[string]int64{"vpc.subnet": 8}))
		require.NoError(t, err)
		require.NoError(t, w.Commit())
	}
	// Законный близнец: чужой аккаунт, тот же вид — его трогать нельзя.
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Quotas().Materialize(ctx,
		quotaRowsFor("prj-ls-acc-alien", theirs, map[string]int64{"vpc.subnet": 8}))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	n, err := proj.ApplyChange(ctx, quota.Change{
		Kind: "vpc.subnet", Scope: quota.ScopeAccount, ScopeID: ours, Value: 40, Revision: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), n, "оба проекта аккаунта — и ни одного чужого")

	for _, p := range mine {
		limit, scope, _, _, _, ok := quotaSnapshot(t, ctx, pool, p, "vpc.subnet")
		require.True(t, ok)
		require.Equal(t, int64(40), limit)
		require.Equal(t, "ACCOUNT", scope)
	}
	limit, _, _, _, _, ok := quotaSnapshot(t, ctx, pool, "prj-ls-acc-alien", "vpc.subnet")
	require.True(t, ok)
	require.Equal(t, int64(8), limit, "проект чужого аккаунта не тронут")
}

// TestLimitSync_LessSpecificScopeNeverOverridesAMoreSpecificOne — старшинство
// областей соблюдается предикатом оператора.
//
// Самое тихое из возможных нарушений: перезапись личного перекрытия общим
// правилом выглядит как исправная синхронизация, а арендатор молча теряет
// выданную ему величину.
func TestLimitSync_LessSpecificScopeNeverOverridesAMoreSpecificOne(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)
	proj := quotaProjection(t, pool)

	const account = "acc-ls-prec"
	const overridden = "prj-ls-prec-own" // живёт из личного перекрытия
	const plain = "prj-ls-prec-default"  // живёт из умолчания

	var err error
	for _, p := range []string{overridden, plain} {
		w, err := r.Writer(ctx)
		require.NoError(t, err)
		_, err = w.Quotas().Materialize(ctx, quotaRowsFor(p, account, map[string]int64{"vpc.address": 16}))
		require.NoError(t, err)
		require.NoError(t, w.Commit())
	}

	// У первого — личное перекрытие.
	_, err = proj.ApplyChange(ctx, quota.Change{
		Kind: "vpc.address", Scope: quota.ScopeProject, ScopeID: overridden, Value: 100, Revision: 30,
	})
	require.NoError(t, err)

	// Умолчание меняется ПОЗЖЕ и с большей ревизией — и всё равно не побеждает.
	//
	// Число затронутых строк здесь НЕ утверждается: база пакета общая, и строк,
	// живущих из умолчания, в ней столько, сколько завели соседние пробы. Предмет
	// утверждения — исход по КАЖДОЙ из двух названных строк, а не счёт по всей
	// таблице; счёт был бы утверждением о чужих фикстурах.
	_, err = proj.ApplyChange(ctx, quota.Change{
		Kind: "vpc.address", Scope: quota.ScopeDefault, Value: 7, Revision: 31,
	})
	require.NoError(t, err)

	limit, scope, _, _, _, _ := quotaSnapshot(t, ctx, pool, overridden, "vpc.address")
	require.Equal(t, int64(100), limit, "личное перекрытие обязано пережить смену умолчания")
	require.Equal(t, "PROJECT", scope)

	limit, scope, _, _, _, _ = quotaSnapshot(t, ctx, pool, plain, "vpc.address")
	require.Equal(t, int64(7), limit, "положительный контроль: строка из умолчания изменилась")
	require.Equal(t, "DEFAULT", scope)

	// Аккаунтная область тоже не перебивает личную, но перебивает умолчание.
	n, err := proj.ApplyChange(ctx, quota.Change{
		Kind: "vpc.address", Scope: quota.ScopeAccount, ScopeID: account, Value: 55, Revision: 32,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "аккаунт свой у этой пробы — счёт здесь утверждать можно")

	limit, _, _, _, _, _ = quotaSnapshot(t, ctx, pool, overridden, "vpc.address")
	require.Equal(t, int64(100), limit, "личное перекрытие переживает и аккаунтное правило")
	limit, scope, _, _, _, _ = quotaSnapshot(t, ctx, pool, plain, "vpc.address")
	require.Equal(t, int64(55), limit)
	require.Equal(t, "ACCOUNT", scope)
}

// TestLimitSync_StaleRevisionIsNotApplied — изменение старше снимка не
// применяется.
//
// Дельта доставляется как минимум однажды, поэтому повтор страницы — штатное
// течение, а не сбой. Без этого условия повтор возвращал бы строке уже
// устаревшую величину.
func TestLimitSync_StaleRevisionIsNotApplied(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)
	proj := quotaProjection(t, pool)

	const project = "prj-ls-stale"
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Quotas().Materialize(ctx,
		quotaRowsFor(project, "acc-ls-stale", map[string]int64{"vpc.gateway": 4}))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	newer := quota.Change{Kind: "vpc.gateway", Scope: quota.ScopeProject, ScopeID: project, Value: 40, Revision: 50}
	_, err = proj.ApplyChange(ctx, newer)
	require.NoError(t, err)

	older := quota.Change{Kind: "vpc.gateway", Scope: quota.ScopeProject, ScopeID: project, Value: 1, Revision: 49}
	n, err := proj.ApplyChange(ctx, older)
	require.NoError(t, err)
	require.Zero(t, n, "изменение старше снимка не применяется")

	limit, _, _, rev, _, _ := quotaSnapshot(t, ctx, pool, project, "vpc.gateway")
	require.Equal(t, int64(40), limit)
	require.Equal(t, int64(50), rev)

	// Положительный контроль: повторное применение ТОЙ ЖЕ ревизии тоже не
	// трогает строку — оператор идемпотентен, поэтому повтор страницы безопасен.
	n, err = proj.ApplyChange(ctx, newer)
	require.NoError(t, err)
	require.Zero(t, n)
}

// TestLimitSync_WithdrawalRemovesTheRowAndUsageComesBackCounted — отзыв снимает
// строку, а восстановление НЕ теряет потребление.
//
// Связка с фиксом затравки: до него удаление строки означало бы потерю
// потребления — восстановленная строка вернулась бы с нулём и выдала место,
// которого нет. Считающая затравка делает отзыв безопасным.
func TestLimitSync_WithdrawalRemovesTheRowAndUsageComesBackCounted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)
	proj := quotaProjection(t, pool)

	const project = "prj-ls-withdraw"
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Quotas().Materialize(ctx,
		quotaRowsFor(project, "acc-ls-withdraw", map[string]int64{"vpc.network": 16}))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	_, err = proj.ApplyChange(ctx, quota.Change{
		Kind: "vpc.network", Scope: quota.ScopeProject, ScopeID: project, Value: 16, Revision: 60,
	})
	require.NoError(t, err)

	for _, name := range []string{"wd-a", "wd-b"} {
		w2, err := r.Writer(ctx)
		require.NoError(t, err)
		_, err = w2.Networks().Insert(ctx, newNetwork(project, name))
		require.NoError(t, err)
		require.NoError(t, w2.Commit())
	}
	require.Equal(t, int64(2), quotaUsed(t, ctx, pool, project, "vpc.network"))

	n, err := proj.ApplyChange(ctx, quota.Change{
		Kind: "vpc.network", Scope: quota.ScopeProject, ScopeID: project, Revision: 61, Withdrawn: true,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.Equal(t, int64(-1), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"строка снята: следующее списание разрешит потолок заново")

	// Материализация восстанавливает строку — и потребление возвращается СЧЁТОМ.
	w3, err := r.Writer(ctx)
	require.NoError(t, err)
	_, err = w3.Quotas().Materialize(ctx,
		quotaRowsFor(project, "acc-ls-withdraw", map[string]int64{"vpc.network": 16}))
	require.NoError(t, err)
	require.NoError(t, w3.Commit())

	require.Equal(t, int64(2), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"потребление не потеряно отзывом: затравка считает по строкам ресурса")
}

// TestLimitSync_CursorAndCountersAreObservable — «ни одна строка не
// синхронизирована за всё время» обязано быть заметно.
func TestLimitSync_CursorAndCountersAreObservable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	proj := quotaProjection(t, pool)

	cursor, err := proj.LoadCursor(ctx)
	require.NoError(t, err)
	require.Empty(t, cursor, "проекция, ни разу не тянувшая дельту, стоит на начале времён")

	require.NoError(t, proj.SaveCursor(ctx, "rev|42", 3))
	cursor, err = proj.LoadCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, "rev|42", cursor)

	require.NoError(t, proj.Heartbeat(ctx))

	var applied, pulls int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT applied_rows_total, pulls_total FROM kacho_vpc.quota_sync_cursor WHERE id = 'limits'`).
		Scan(&applied, &pulls))
	require.Equal(t, int64(3), applied, "работа считается накопительно")
	require.Equal(t, int64(1), pulls, "проходы считаются отдельно от работы")
}
