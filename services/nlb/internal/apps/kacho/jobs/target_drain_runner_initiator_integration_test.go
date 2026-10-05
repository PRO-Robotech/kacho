// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// target_drain_runner_initiator_integration_test.go — третья пара личности
// компонента nlb (решение Д116): проход снятия истёкших целей — фоновый путь,
// пишущий журнал модуля (`nlb_target_group UPDATED`), и его инициатор —
// принципал компонента `(nlb, target-drain-runner)`, а не принципал запроса,
// которого у фонового пути нет.
//
// Пара проб — законный близнец и отрицательный кейс, отличающиеся ровно
// принципалом входного контекста: без принципала (так джобу зовёт `Run`) строка
// журнала несёт `system:nlb-target-drain-runner`; с принципалом пользователя —
// `ErrComponentOverPrincipal` до транзакции, цели на месте, строк журнала нет.
// Объявление пары в таблице гейта УК3-27 (д) держит
// `internal/repohygiene/journaledwrites.go`.

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
	"github.com/PRO-Robotech/corelib/pgtest"
)

func drainJournalInitiators(t *testing.T, pool *pgxpool.Pool, tgID string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT initiator FROM kacho_nlb.nlb_outbox
		 WHERE resource_type = 'nlb_target_group' AND resource_id = $1 AND action = 'UPDATED'`, tgID)
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

func drainInitiatorStand(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	tgID, _ := insertTargetGroup(t, ctx, pool, 30)
	insertDrainingTarget(t, ctx, pool, tgID, "inst-expired", 60*time.Second)
	require.Equal(t, 1, countTargets(t, ctx, pool), "ФИКСТУРА: истёкшая цель заведена")
	return pool, tgID
}

// TestDrainOnce_D116_JournalRowCarriesTheComponentInitiator — проход без
// принципала в контексте пишет `UPDATED` группы с инициатором компонента.
func TestDrainOnce_D116_JournalRowCarriesTheComponentInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	pool, tgID := drainInitiatorStand(t)
	r := newRunner(t, pool, time.Hour)

	deleted, tgs, err := r.drainOnce(context.Background())
	require.NoError(t, err, "Д116: проход снятия истёкших целей отвергнут")
	require.Equal(t, int64(1), deleted)
	require.Equal(t, 1, tgs)
	require.Equal(t, []string{"system:nlb-target-drain-runner"}, drainJournalInitiators(t, pool, tgID),
		"Д116: UPDATED фонового пути снятия целей несёт инициатора компонента")
}

// TestDrainOnce_D116_ForeignPrincipalIsRefusedBeforeTheTransaction — контекст с
// принципалом пользователя: отказ `ErrComponentOverPrincipal` до транзакции.
func TestDrainOnce_D116_ForeignPrincipalIsRefusedBeforeTheTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	pool, tgID := drainInitiatorStand(t)
	r := newRunner(t, pool, time.Hour)
	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: ids.NewHyphenID(ids.PrefixUser)})

	_, _, err := r.drainOnce(ctx)
	require.Error(t, err, "Д116: проход под чужим принципалом обязан отказать")
	require.True(t, errors.Is(err, journaltx.ErrComponentOverPrincipal),
		"Д116: отказ — ErrComponentOverPrincipal, получено: %v", err)
	require.Equal(t, 1, countTargets(t, context.Background(), pool), "Д116: отказ до прохода — цель на месте")
	require.Empty(t, drainJournalInitiators(t, pool, tgID), "Д116: строк журнала нет")
}
