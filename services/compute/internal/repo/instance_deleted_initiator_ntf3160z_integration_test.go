// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// instance_deleted_initiator_ntf3160z_integration_test.go — полоса RED S2-B3
// (issue-2918, NTF-3) для compute: строка `Instance DELETED`, которую пишет
// последний шаг delete-саги (`InstanceRepo.Delete`, общий для публичного
// `Delete` и прохода добивателя), берёт инициатора ТОЛЬКО из принципала
// контекста; второго источника нет.
//
// Сценарии и заказы (приёмка NTF-3 ред. 42, замысел issue-2918):
//   - NTF3-160 (з), часть compute: строка журнала compute, записанная снятием
//     машины `usr-A`, несёт `user:usr-A` и заполненный `occurred_at`; строк с
//     инициатором `service:`/`system:` — 0;
//   - близнец (з): то же снятие делает `usr-B` — `user:usr-B`, строк `user:usr-A` — 0;
//   - CX3H-02 (б): без принципала транзакция отказывает ДО оператора — строка
//     машины на месте, строк журнала 0. Положительный близнец — первая проба
//     (тот же вызов, отличие — есть ли принципал).

package repo_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

type ntf3160zRow struct {
	Initiator  string
	OccurredAt bool
}

// ntf3160zDeletedRows — строки журнала `Instance DELETED` машины.
func ntf3160zDeletedRows(t *testing.T, pool *pgxpool.Pool, instanceID string) []ntf3160zRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT initiator, created_at IS NOT NULL FROM public.compute_outbox
		 WHERE resource_kind = 'Instance' AND resource_id = $1 AND event_type = 'DELETED'
		 ORDER BY sequence_no`, instanceID)
	require.NoError(t, err)
	defer rows.Close()
	var out []ntf3160zRow
	for rows.Next() {
		var r ntf3160zRow
		require.NoError(t, rows.Scan(&r.Initiator, &r.OccurredAt))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func ntf3160zForeignAfter(t *testing.T, pool *pgxpool.Pool, p0 int64) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*) FROM public.compute_outbox
		 WHERE sequence_no > $1 AND (initiator LIKE 'service:%' OR initiator LIKE 'system:%')`, p0).Scan(&n))
	return n
}

func ntf3160zP0(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var p0 int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COALESCE(max(sequence_no), 0) FROM public.compute_outbox`).Scan(&p0))
	return p0
}

// ntf3160zDeleteAs — машина заведена фикстурой до P0, снятие строки — под ctx.
func ntf3160zDeleteAs(t *testing.T, ctx context.Context, name string) (*pgxpool.Pool, string, int64, error) {
	t.Helper()
	fixtureCtx, fixtureWho := ntf357UserCtx()
	dsn := setupTestDB(t)
	fixture := ntf357Pool(t, dsn, fixtureWho)
	subject := ntf357Pool(t, dsn, "")

	in := comp1Instance(ids.NewHyphenID(ids.PrefixInstanceHyphen), "prj-acme", name)
	_, _, err := mustJournalWriter(repo.NewInstanceRepo(fixture, probeJournalOptions)).Insert(fixtureCtx, in)
	require.NoError(t, err, "ФИКСТУРА: вставка машины на пуле с инициатором")

	p0 := ntf3160zP0(t, subject)
	derr := mustJournalWriter(repo.NewInstanceRepo(subject, probeJournalOptions)).Delete(ctx, in.ID)
	return subject, in.ID, p0, derr
}

// TestInstance_NTF3160z_DeletedRowCarriesStarterInitiator — NTF3-160 (з), compute.
func TestInstance_NTF3160z_DeletedRowCarriesStarterInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	ctx, want := ntf357UserCtx()
	pool, id, p0, err := ntf3160zDeleteAs(t, ctx, "ntf3160z-a")
	require.NoError(t, err, "NTF3-160 (з): снятие строки машины под принципалом %s отвергнуто", want)
	require.Equal(t, []ntf3160zRow{{Initiator: want, OccurredAt: true}}, ntf3160zDeletedRows(t, pool, id),
		"NTF3-160 (з): Instance DELETED несёт инициатора начавшего снятие и заполненный occurred_at")
	require.Equal(t, 0, ntf3160zForeignAfter(t, pool, p0),
		"NTF3-160 (з): строк журнала compute после P0 с инициатором service: или system: — 0")
}

// TestInstance_NTF3160zTwin_OtherStarterOwnInitiator — близнец (з): начавший — другой пользователь.
func TestInstance_NTF3160zTwin_OtherStarterOwnInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	_, wantA := ntf357UserCtx()
	ctxB, wantB := ntf357UserCtx()
	pool, id, p0, err := ntf3160zDeleteAs(t, ctxB, "ntf3160z-b")
	require.NoError(t, err, "NTF3-160 (з) близнец: снятие под принципалом %s отвергнуто", wantB)
	got := ntf3160zDeletedRows(t, pool, id)
	require.Equal(t, []ntf3160zRow{{Initiator: wantB, OccurredAt: true}}, got,
		"NTF3-160 (з) близнец: Instance DELETED несёт %s", wantB)
	for _, r := range got {
		require.NotEqual(t, wantA, r.Initiator, "NTF3-160 (з) близнец: строк с инициатором usr-A — 0")
	}
	require.Equal(t, 0, ntf3160zForeignAfter(t, pool, p0), "NTF3-160 (з) близнец: строк service:/system: — 0")
}

// TestInstance_CX3H02b_DeleteWithoutPrincipalRefusedBeforeStatement — CX3H-02 (б):
// без принципала — отказ до оператора; строка машины на месте, строк журнала 0.
// Положительный близнец — TestInstance_NTF3160z_DeletedRowCarriesStarterInitiator
// (тот же вызов; единственный изменённый факт — принципал в контексте).
func TestInstance_CX3H02b_DeleteWithoutPrincipalRefusedBeforeStatement(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	pool, id, p0, err := ntf3160zDeleteAs(t, operations.WithoutPrincipal(context.Background()), "cx3h02b")
	require.Error(t, err, "CX3H-02 (б): снятие строки машины без принципала обязано отказать")
	var alive int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM instances WHERE id = $1`, id).Scan(&alive))
	require.Equal(t, 1, alive, "CX3H-02 (б): строка машины на месте после отказа")
	require.Empty(t, ntf3160zDeletedRows(t, pool, id), "CX3H-02 (б): строк Instance DELETED — 0")
	var after int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM public.compute_outbox WHERE sequence_no > $1`, p0).Scan(&after))
	require.Equal(t, 0, after, "CX3H-02 (б): строк журнала compute после P0 — 0")
}
