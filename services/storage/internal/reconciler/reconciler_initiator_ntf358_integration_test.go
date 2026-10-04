// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// reconciler_initiator_ntf358_integration_test.go — полоса RED S1-A2 (issue-2918,
// NTF-3) для фонового пути storage: запись перехода тома в `ERROR`, сделанная
// сверщиком, несёт системного инициатора компонента.
//
// Сценарий приёмки NTF-3 (отпечаток ac1f9fc9…) NTF3-58: `usr-A` создал `vol-1`
// (`CREATING`); запись перехода `vol-1` в `ERROR`, сделанная компонентом
// `storage-reconciler` (С21), коммитится — строка журнала `UPDATED` несёт
// `system:storage-reconciler`. Пара компонента — `(storage, reconciler)` §8
// замысла issue-2918 (З4, CX3B-25): личность ставит `journaltx.AsComponent` на
// фоновом пути, транзакцию открывает помощник.
//
// Близнец (то же изменение глаголом арендатора несёт `user:`) —
// `repo/pg/volume_journal_initiator_ntf357_integration_test.go`.
//
// ДВА ПУЛА НА ОДНОЙ БАЗЕ. Пул фикстуры выставляет инициатора `usr-A` параметром
// старта сессии: им заводится том (`CREATED` от `usr-A`). Сверщик работает на
// пуле предмета, где инициатора не выставляет никто, кроме самого пути записи.

package reconciler_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend/fake"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
)

func ntf358Initiators(t *testing.T, pool *pgxpool.Pool, volumeID, eventType string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT initiator FROM kacho_storage.storage_outbox
		 WHERE resource_kind = 'Volume' AND resource_id = $1 AND event_type = $2 ORDER BY sequence_no`, volumeID, eventType)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		require.NoError(t, rows.Scan(&v))
		out = append(out, v)
	}
	require.NoError(t, rows.Err())
	return out
}

// ntf358Stand — база пробы, два пула и `vol-1` в CREATING, созданный `usr-A`.
func ntf358Stand(t *testing.T) (fixture, subject *pgxpool.Pool, volumeID string) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	ctx := context.Background()
	userA := "user:" + ids.NewHyphenID(ids.PrefixUser)

	dsn := pgtest.NewDB(t) + "&pool_max_conns=8"
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	q := u.Query()
	q.Set("options", strings.TrimSpace(q.Get("options")+" -c kacho_journal.initiator="+userA))
	u.RawQuery = q.Encode()
	fixture, err = coredb.NewPool(ctx, u.String())
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, fixture)
	subject, err = coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, subject)

	var n int
	require.NoError(t, subject.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'kacho_storage' AND table_name = 'storage_outbox' AND column_name = 'initiator'`).Scan(&n))
	require.Equal(t, 1, n, "ФИКСТУРА: у kacho_storage.storage_outbox нет колонки initiator — миграция S1-A1 не применена")

	seedQuotaFor(t, fixture, "prj-cycle")
	bindingID, loc := seedBinding(t, fixture)
	volumeID, _ = insertVolume(t, fixture, bindingID, loc, 1<<30)
	require.Equal(t, []string{userA}, ntf358Initiators(t, subject, volumeID, "CREATED"),
		"ФИКСТУРА: CREATED vol-1 от usr-A")
	state, _, _ := volumeState(t, subject, volumeID)
	require.Equal(t, "CREATING", state, "ФИКСТУРА: vol-1 в CREATING")
	return fixture, subject, volumeID
}

// ntf358FailingPass — один проход сверщика на пуле pool, где бэкенд отказывает
// создать объект окончательно: сверщик пишет переход в ERROR (С21). Возвращает
// журнал процесса сверщика.
func ntf358FailingPass(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	be := fake.New(blockbackend.Capabilities{Snapshots: true, OnlineGrow: true})
	be.FailVerb("CreateVolume", blockbackend.OutcomeCapacityExhausted)
	var log bytes.Buffer
	rec := reconciler.New(reconciler.NewStore(pool), openerFor{be}, reconciler.Config{
		Batch: 10, Logger: slog.New(slog.NewTextHandler(&log, nil)),
	})
	rec.Once(context.Background())
	return log.String()
}

// TestReconciler_NTF358_ControlSessionInitiatorAdmitsTheTransition — КОНТРОЛЬ:
// тот же проход на соединении с инициатором записывает переход в ERROR.
// Сверщик отбрасывает ошибку записи перехода, поэтому текста отказа базы у
// основной пробы нет; близнец отличается ровно одним фактом — есть ли у
// транзакции инициатор, — и красное основной пробы читается как «путь записи
// перехода инициатора не выставил».
func TestReconciler_NTF358_ControlSessionInitiatorAdmitsTheTransition(t *testing.T) {
	fixture, subject, id := ntf358Stand(t)
	log := ntf358FailingPass(t, fixture)
	state, _, reason := volumeState(t, subject, id)
	require.Equal(t, "ERROR", state, "КОНТРОЛЬ: переход в ERROR при выставленном инициаторе; журнал сверщика:\n%s", log)
	require.Equal(t, string(domain.ReasonBackendCapacityExhausted), reason)
}

// TestReconciler_NTF358_ErrorTransitionCarriesComponentInitiator — NTF3-58.
func TestReconciler_NTF358_ErrorTransitionCarriesComponentInitiator(t *testing.T) {
	_, subject, id := ntf358Stand(t)
	log := ntf358FailingPass(t, subject)

	state, _, reason := volumeState(t, subject, id)
	require.Equal(t, "ERROR", state,
		"NTF3-58: запись перехода vol-1 в ERROR сверщиком не закоммичена (причина в строке: %q; контроль с тем же проходом и выставленным инициатором — зелёный); журнал сверщика:\n%s", reason, log)
	require.Equal(t, []string{"system:storage-reconciler"}, ntf358Initiators(t, subject, id, "UPDATED"),
		"NTF3-58: UPDATED перехода, записанного компонентом, несёт system:storage-reconciler")
}
