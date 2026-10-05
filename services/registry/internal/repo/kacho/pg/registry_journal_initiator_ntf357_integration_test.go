// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// registry_journal_initiator_ntf357_integration_test.go — полоса RED S1-A2
// (issue-2918, NTF-3) для registry: транзакция вставки реестра несёт инициатора
// принципала контекста; строку журнала `kacho_registry.registry_resource_journal`
// пишет функция базы на таблице реестров, и колонку инициатора она заполняет
// умолчанием из настройки транзакции.
//
// Сценарий приёмки NTF-3 (отпечаток ac1f9fc9…) NTF3-57, ветка registry
// (`RegistryService.Create` от имени пользователя). Источник инициатора — ОДИН:
// принципал контекста (замысел issue-2918, З4).
//
// КОНТРОЛЬ рядом: тот же вызов на соединении с инициатором, выставленным
// параметром старта сессии, проходит. Близнец отличается ровно одним фактом, и
// красное основной пробы читается как «транзакция репозитория инициатора не
// выставила», а не как поломка фикстуры: репозиторий отдаёт отказ классом, без
// текста базы.

package pg_test

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
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

func ntf357UserCtx() (context.Context, string, string) {
	id := ids.NewHyphenID(ids.PrefixUser)
	return operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: id}), id, "user:" + id
}

// ntf357Pool — пул на собственной базе пробы; непустой sessionInitiator
// выставляет инициатора параметром старта каждого соединения (только контроль).
func ntf357Pool(t *testing.T, sessionInitiator string) *pgxpool.Pool {
	t.Helper()
	dsn := pgtest.NewDB(t)
	if sessionInitiator != "" {
		dsn = ntf357WithStartOption(t, dsn, "kacho_journal.initiator="+sessionInitiator)
	}
	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	seedFixtureQuotas(t, pool)
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'kacho_registry' AND table_name = 'registry_resource_journal'
		   AND column_name = 'initiator'`).Scan(&n))
	require.Equal(t, 1, n, "ФИКСТУРА: у registry_resource_journal нет колонки initiator — миграция S1-A1 не применена")
	return pool
}

func ntf357Initiators(t *testing.T, pool *pgxpool.Pool, resourceID string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT initiator FROM kacho_registry.registry_resource_journal
		 WHERE resource_id = $1 AND event_type = 'CREATED'`, resourceID)
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

func ntf357Insert(ctx context.Context, pool *pgxpool.Pool, userID, name string) (*domain.Registry, error) {
	reg := newReg("prj-P", name, nil)
	_, _, err := kachopg.NewRegistryRepo(pool).Insert(ctx, reg, domain.RegisterIntentForCreate(reg, "user", userID))
	return reg, err
}

// TestRegistry_NTF357_ControlSessionInitiatorAdmitsTheInsert — КОНТРОЛЬ.
func TestRegistry_NTF357_ControlSessionInitiatorAdmitsTheInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	ctx, userID, want := ntf357UserCtx()
	pool := ntf357Pool(t, want)

	reg, err := ntf357Insert(ctx, pool, userID, "ntf357-control")
	require.NoError(t, err, "КОНТРОЛЬ: вставка реестра при выставленном инициаторе")
	require.Equal(t, []string{want}, ntf357Initiators(t, pool, reg.ID))
}

// TestRegistry_NTF357_CreatedByUserCarriesUserInitiator — NTF3-57 (registry).
func TestRegistry_NTF357_CreatedByUserCarriesUserInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	ctx, userID, want := ntf357UserCtx()
	pool := ntf357Pool(t, "")

	reg, err := ntf357Insert(ctx, pool, userID, "ntf357")
	require.NoError(t, err,
		"NTF3-57: вставка реестра под принципалом %s отвергнута (контроль с тем же вызовом и выставленным инициатором — зелёный)", want)
	require.Equal(t, []string{want}, ntf357Initiators(t, pool, reg.ID),
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
