// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// instance_journal_initiator_ntf357_integration_test.go — полоса RED S1-A2
// (issue-2918, NTF-3) для compute: транзакция вставки машины несёт инициатора
// принципала контекста в строку журнала `public.compute_outbox`.
//
// Сценарий приёмки NTF-3 (отпечаток ac1f9fc9…) NTF3-57, ветка compute
// (`InstanceService.Create` от имени пользователя). Источник инициатора — ОДИН:
// принципал контекста (замысел issue-2918, З4).
//
// Репозиторий отдаёт отказ записи журнала классом `ErrInternal`, текст базы
// наружу не идёт. Поэтому рядом стоит КОНТРОЛЬ: тот же вызов на соединении, где
// инициатор уже выставлен параметром старта сессии, проходит. Близнец отличается
// ровно одним фактом — есть ли у транзакции инициатор, — и красное основной
// пробы читается как «транзакция репозитория инициатора не выставила», а не как
// поломка фикстуры.

package repo_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

// ntf357UserCtx — контекст с принципалом пользователя и ожидаемый инициатор.
func ntf357UserCtx() (context.Context, string) {
	id := ids.NewHyphenID(ids.PrefixUser)
	return operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: id}), "user:" + id
}

// ntf357Pool — пул на базе пробы. Непустой sessionInitiator выставляет
// инициатора параметром старта каждого соединения (только для контроля).
func ntf357Pool(t *testing.T, dsn, sessionInitiator string) *pgxpool.Pool {
	t.Helper()
	if sessionInitiator != "" {
		dsn = ntf357WithStartOption(t, dsn, "kacho_journal.initiator="+sessionInitiator)
	}
	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'compute_outbox' AND column_name = 'initiator'`).Scan(&n))
	require.Equal(t, 1, n, "ФИКСТУРА: у public.compute_outbox нет колонки initiator — миграция S1-A1 не применена")
	return pool
}

func ntf357Initiators(t *testing.T, pool *pgxpool.Pool, resourceID string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT initiator FROM public.compute_outbox
		 WHERE resource_kind = 'Instance' AND resource_id = $1 AND event_type = 'CREATED'`, resourceID)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestInstance_NTF357_ControlSessionInitiatorAdmitsTheInsert — КОНТРОЛЬ: на
// соединении с инициатором вставка машины проходит и строка журнала его несёт.
// Зелёный здесь — условие того, что красное соседней пробы говорит о предмете.
func TestInstance_NTF357_ControlSessionInitiatorAdmitsTheInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	ctx, want := ntf357UserCtx()
	pool := ntf357Pool(t, setupTestDB(t), want)

	in := comp1Instance(ids.NewHyphenID(ids.PrefixInstanceHyphen), "prj-acme", "ntf357-control")
	_, _, err := repo.NewInstanceRepo(pool).Insert(ctx, in)
	require.NoError(t, err, "КОНТРОЛЬ: вставка машины при выставленном инициаторе")
	require.Equal(t, []string{want}, ntf357Initiators(t, pool, in.ID))
}

// TestInstance_NTF357_CreatedByUserCarriesUserInitiator — NTF3-57 (compute):
// вставка машины под принципалом пользователя пишет CREATED с `user:<id>`.
func TestInstance_NTF357_CreatedByUserCarriesUserInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	ctx, want := ntf357UserCtx()
	pool := ntf357Pool(t, setupTestDB(t), "")

	in := comp1Instance(ids.NewHyphenID(ids.PrefixInstanceHyphen), "prj-acme", "ntf357")
	_, _, err := repo.NewInstanceRepo(pool).Insert(ctx, in)
	require.NoError(t, err,
		"NTF3-57: вставка машины под принципалом %s отвергнута (контроль с тем же вызовом и выставленным инициатором — зелёный)", want)
	require.Equal(t, []string{want}, ntf357Initiators(t, pool, in.ID),
		"NTF3-57: CREATED несёт инициатора принципала контекста")
}

// ntf357WithStartOption дописывает `-c <настройка>` к параметру старта `options`
// строки подключения, сохраняя уже стоящие там (`search_path` выдающего базу):
// второй параметр `options` заменил бы первый, и контроль молча шёл бы без
// инициатора.
func ntf357WithStartOption(t *testing.T, dsn, setting string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	q := u.Query()
	opts := strings.TrimSpace(q.Get("options") + " -c " + setting)
	q.Set("options", opts)
	u.RawQuery = q.Encode()
	return u.String()
}
