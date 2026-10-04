// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// free_ip_runner_initiator_integration_test.go — полоса RED S1-A2 (issue-2918,
// NTF-3) для фонового пути nlb: строка журнала, которую пишет задание
// освобождения адресов, несёт инициатора компонента `system:nlb-free-ip-runner`.
//
// Основание — замысел issue-2918: З13 (таблица фоновых путей: `emitReconcileFinalize`
// → `NetworkLoadBalancer DELETED`, инициатор — принципал прохода от
// `journaltx.AsComponent(ctx, "nlb", "free-ip-runner")`), З4 и CX3B-25 (2) (три
// исхода на входе `AsComponent`: принципала нет → принципал компонента; иной
// принципал → `ErrComponentOverPrincipal`, у вызывающего nlb — ошибка
// `reconcileOne` до `journaltx.Begin`). Сценарий приёмки, которому служит
// инициатор, — NTF3-58 (изменение, сделанное компонентом, несёт системного
// инициатора); исход фонового пути до и после под-фазы одинаков (CX3G-05) держат
// `TestFreeIP_ReconcileStuckDeleting` и `TestFreeIP_CreateOrphanReconciled` без
// правки утверждений.

package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

func freeIPJournalInitiators(t *testing.T, pool *pgxpool.Pool, lbID string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT initiator FROM kacho_nlb.nlb_outbox
		 WHERE resource_type = 'nlb_load_balancer' AND resource_id = $1 AND action = 'DELETED'`, lbID)
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

func freeIPStand(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	var n int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'kacho_nlb' AND table_name = 'nlb_outbox' AND column_name = 'initiator'`).Scan(&n))
	require.Equal(t, 1, n, "ФИКСТУРА: у kacho_nlb.nlb_outbox нет колонки initiator — миграция S1-A1 не применена")
	// Строка балансировщика — не журналируемая запись: журнал nlb пишется
	// функцией фундамента из Go, триггера на таблице нет.
	lbID, _ := insertStuckLB(t, ctx, pool, domain.LBStatusDeleting, "auto", "adr0000000INITIATOR1", "", "", 10*time.Minute)
	require.Equal(t, 1, countLoadBalancers(t, ctx, pool), "ФИКСТУРА: застрявший балансировщик заведён")
	return pool, lbID
}

// TestFreeIP_S1A2_FinalizeRowCarriesComponentInitiator — проход без принципала
// в контексте (так его зовёт `Run`) пишет `DELETED` с `system:nlb-free-ip-runner`.
func TestFreeIP_S1A2_FinalizeRowCarriesComponentInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	pool, lbID := freeIPStand(t)
	r := newFreeIPRunner(t, pool, &fakeReleaser{}, time.Minute)

	_, err := r.reconcileOnce(context.Background())
	require.NoError(t, err, "S1-A2 (З13): проход задания освобождения адресов отвергнут")
	require.Equal(t, []string{"system:nlb-free-ip-runner"}, freeIPJournalInitiators(t, pool, lbID),
		"S1-A2 (З13): DELETED фонового пути nlb несёт инициатора компонента")
}

// TestFreeIP_S1A2_ForeignPrincipalIsRefusedBeforeTheTransaction — CX3B-25 (2):
// контекст с принципалом пользователя — `AsComponent` отказывает
// `ErrComponentOverPrincipal`, проход возвращает ошибку до открытия транзакции,
// строка балансировщика на месте, строк журнала нет.
func TestFreeIP_S1A2_ForeignPrincipalIsRefusedBeforeTheTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	pool, lbID := freeIPStand(t)
	rel := &fakeReleaser{}
	r := newFreeIPRunner(t, pool, rel, time.Minute)
	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: ids.NewHyphenID(ids.PrefixUser)})

	_, err := r.reconcileOnce(ctx)
	require.Error(t, err, "CX3B-25 (2): проход под чужим принципалом обязан отказать")
	require.True(t, errors.Is(err, journaltx.ErrComponentOverPrincipal),
		"CX3B-25 (2): отказ — ErrComponentOverPrincipal, получено: %v", err)
	require.Empty(t, rel.frees(), "CX3B-25 (2): отказ до прохода — адрес не освобождался")
	require.Equal(t, 1, countLoadBalancers(t, context.Background(), pool), "CX3B-25 (2): строка балансировщика на месте")
	require.Empty(t, freeIPJournalInitiators(t, pool, lbID), "CX3B-25 (2): строк журнала нет")
}
