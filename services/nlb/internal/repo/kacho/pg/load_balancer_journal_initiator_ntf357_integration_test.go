// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// load_balancer_journal_initiator_ntf357_integration_test.go — полоса RED S1-A2
// (issue-2918, NTF-3) для nlb: пишущая транзакция репозитория несёт инициатора
// принципала контекста в строку журнала `kacho_nlb.nlb_outbox`.
//
// Сценарий приёмки NTF-3 (отпечаток ac1f9fc9…) NTF3-57, ветка nlb
// (`NetworkLoadBalancerService.Create` от имени пользователя). Источник
// инициатора — ОДИН: принципал контекста (замысел issue-2918, З4).

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
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

func ntf357Initiators(t *testing.T, pool *pgxpool.Pool, resourceID string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT initiator FROM kacho_nlb.nlb_outbox
		 WHERE resource_type = 'nlb_load_balancer' AND resource_id = $1 AND action = 'CREATED'`, resourceID)
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

// TestLB_NTF357_CreatedByUserCarriesUserInitiator — NTF3-57 (nlb): CREATED
// балансировщика, созданного пользователем, несёт `user:<id>`.
func TestLB_NTF357_CreatedByUserCarriesUserInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	pool, err := coredb.NewPool(context.Background(), setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'kacho_nlb' AND table_name = 'nlb_outbox' AND column_name = 'initiator'`).Scan(&n))
	require.Equal(t, 1, n, "ФИКСТУРА: у kacho_nlb.nlb_outbox нет колонки initiator — миграция S1-A1 не применена")

	userID := ids.NewHyphenID(ids.PrefixUser)
	want := "user:" + userID
	ctx := operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: userID})

	repo := kachopg.New(pool, nil)
	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	lb := newLB("prj01ABC", "")
	rec, err := w.LoadBalancers().Insert(ctx, lb)
	require.NoError(t, err, "ФИКСТУРА: вставка балансировщика")
	require.NoError(t,
		w.Outbox().Emit(ctx, "nlb_load_balancer", string(rec.ID), string(rec.ProjectID), "CREATED", map[string]any{"id": string(rec.ID)}),
		"NTF3-57: строка журнала CREATED, записанная транзакцией репозитория под принципалом %s, отвергнута", want)
	require.NoError(t, w.Commit())
	require.Equal(t, []string{want}, ntf357Initiators(t, pool, string(rec.ID)),
		"NTF3-57: CREATED несёт инициатора принципала контекста")
}
