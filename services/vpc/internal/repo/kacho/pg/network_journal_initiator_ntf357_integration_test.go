// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// network_journal_initiator_ntf357_integration_test.go — полоса RED S1-A2
// (issue-2918, NTF-3) для vpc: пишущая транзакция репозитория несёт инициатора
// принципала контекста в строку журнала.
//
// Сценарий приёмки NTF-3 (отпечаток ac1f9fc9…) NTF3-57: создание сети от имени
// пользователя и правка её описания от имени сервисного аккаунта; строки журнала
// `kacho_vpc.vpc_outbox` несут `user:<id>` и `service_account:<id>`. Источник
// инициатора — ОДИН: принципал контекста (замысел issue-2918, З4); транзакцию
// открывает репозиторий, и выставить настройку инициатора больше некому.
//
// Утверждается строка журнала — то, что меняет полоса S1-A2. Доставка события
// подписчику и поле `occurredAt` контракта — предметы других полос.

package pg_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// ntf357Principal — контекст с принципалом и инициатор, которого ждёт журнал.
func ntf357Principal(typ, prefix, form string) (context.Context, string) {
	id := ids.NewHyphenID(prefix)
	ctx := operations.WithPrincipal(context.Background(), operations.Principal{Type: typ, ID: id})
	return ctx, form + ":" + id
}

// ntf357RequireInitiatorColumn — предпосылка: колонка инициатора у журнала есть
// (полоса S1-A1). Без неё красное означало бы отсутствие чужого предмета.
func ntf357RequireInitiatorColumn(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'kacho_vpc' AND table_name = 'vpc_outbox' AND column_name = 'initiator'`).Scan(&n))
	require.Equal(t, 1, n, "ФИКСТУРА: у kacho_vpc.vpc_outbox нет колонки initiator — миграция S1-A1 не применена")
}

// ntf357Initiators — инициаторы строк журнала ресурса по виду события.
func ntf357Initiators(t *testing.T, pool *pgxpool.Pool, resourceID, eventType string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT initiator FROM kacho_vpc.vpc_outbox
		 WHERE resource_id = $1 AND event_type = $2 ORDER BY sequence_no`, resourceID, eventType)
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

// ntf357Repo — пул на собственной базе пробы и репозиторий над ним.
func ntf357Repo(t *testing.T) (*pgxpool.Pool, *kachopg.Repository) {
	t.Helper()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	ntf357RequireInitiatorColumn(t, pool)
	return pool, mustJournalWriter(kachopg.New(pool, nil, probeJournalOptions))
}

// TestNetwork_NTF357_CreatedByUserCarriesUserInitiator — NTF3-57 (vpc): CREATED
// сети, созданной пользователем, несёт `user:<id>`.
func TestNetwork_NTF357_CreatedByUserCarriesUserInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	pool, r := ntf357Repo(t)

	userCtx, wantUser := ntf357Principal("user", ids.PrefixUser, "user")
	w, err := r.Writer(userCtx)
	require.NoError(t, err)
	defer w.Abort()
	created, err := w.Networks().Insert(userCtx, newNetwork("prj-1", "net-a"))
	require.NoError(t, err, "ФИКСТУРА: вставка сети")
	require.NoError(t,
		w.Outbox().Emit(userCtx, "Network", created.ID, created.ProjectID, "CREATED", map[string]any{"id": created.ID}),
		"NTF3-57: строка журнала CREATED, записанная транзакцией репозитория под принципалом %s, отвергнута", wantUser)
	require.NoError(t, w.Commit())
	require.Equal(t, []string{wantUser}, ntf357Initiators(t, pool, created.ID, "CREATED"),
		"NTF3-57: CREATED несёт инициатора принципала контекста")
}

// TestNetwork_NTF357_UpdatedByServiceAccountCarriesItsInitiator — NTF3-57 (vpc):
// UPDATED описания сети, сделанный сервисным аккаунтом, несёт
// `service_account:<id>`. Сеть заводится транзакцией без строки журнала —
// фикстура не зависит от предмета.
func TestNetwork_NTF357_UpdatedByServiceAccountCarriesItsInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	pool, r := ntf357Repo(t)

	userCtx, _ := ntf357Principal("user", ids.PrefixUser, "user")
	w, err := r.Writer(userCtx)
	require.NoError(t, err)
	n := newNetwork("prj-1", "net-a")
	created, err := w.Networks().Insert(userCtx, n)
	require.NoError(t, err, "ФИКСТУРА: вставка сети")
	require.NoError(t, w.Commit(), "ФИКСТУРА: фиксация сети")

	saCtx, wantSA := ntf357Principal("service_account", ids.PrefixServiceAccount, "service_account")
	w2, err := r.Writer(saCtx)
	require.NoError(t, err)
	defer w2.Abort()
	upd := *n
	upd.Description = domain.RcDescription("ntf3-57")
	_, err = w2.Networks().Update(saCtx, &upd)
	require.NoError(t, err, "ФИКСТУРА: правка сети")
	require.NoError(t,
		w2.Outbox().Emit(saCtx, "Network", created.ID, created.ProjectID, "UPDATED", map[string]any{"id": created.ID}),
		"NTF3-57: строка журнала UPDATED под принципалом %s отвергнута", wantSA)
	require.NoError(t, w2.Commit())
	require.Equal(t, []string{wantSA}, ntf357Initiators(t, pool, created.ID, "UPDATED"),
		"NTF3-57: UPDATED несёт инициатора сервисного аккаунта")
}
