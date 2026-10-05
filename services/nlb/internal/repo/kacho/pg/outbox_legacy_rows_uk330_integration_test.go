// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outbox_legacy_rows_uk330_integration_test.go — УК3-30 (замысел issue-2918,
// CX3C-04 (б), З5; полоса S1-A7): строки журнала nlb, записанные ДО перевода
// эмиттера на функцию фундамента (литеральной вставкой), читаются `Mapping`
// модуля после него тем же видом и тем же действием, и строка, записанная
// эмиттером модуля, хранится в той же форме, что и литеральная.
//
// Схема журнала nlb при переводе не меняется (З5: «второй из двух путей записи;
// переписи строк окна удержания нет»), поэтому держатель сохранности окна —
// это сравнение: (1) каждая строка литеральной формы — у обоих прежних
// писателей (`outbox_emitter.go` и `emitReconcileFinalize`), по каждой паре
// «вид × действие» ограничения базы — переводится `Mapping` в тот же род
// изменения, что объявлен словарём; (2) строка, записанная эмиттером модуля, по
// хранимым колонкам (`resource_type`, `action`, `project_id`, `payload`)
// неотличима от литеральной строки той же пары.
//
// Это регрессионный держатель: до правки эмиттера он зелёный по построению
// (писатель один и тот же), после правки обязан остаться зелёным.

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/pgtest"

	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/subscriptionjournal"
)

// uk330Want — род изменения, в который словарь владельца переводит действие
// журнала (`subscriptionjournal.Journal().Mapping.Changes`, комментарий там же:
// MOVED и FAILED отдаются правкой).
var uk330Want = map[string]subscriptionv1.SubscriptionEvent_Change{
	kachorepo.OutboxActionCreated: subscriptionv1.SubscriptionEvent_CREATED,
	kachorepo.OutboxActionUpdated: subscriptionv1.SubscriptionEvent_UPDATED,
	kachorepo.OutboxActionDeleted: subscriptionv1.SubscriptionEvent_DELETED,
	kachorepo.OutboxActionMoved:   subscriptionv1.SubscriptionEvent_UPDATED,
	kachorepo.OutboxActionFailed:  subscriptionv1.SubscriptionEvent_UPDATED,
}

var uk330Kinds = []string{
	kachorepo.OutboxResourceLoadBalancer,
	kachorepo.OutboxResourceListener,
	kachorepo.OutboxResourceTargetGroup,
}

type uk330Row struct {
	kind, action, projectID, payload string
}

func TestLB_UK330_RowsWrittenBeforeTheEmitterChangeReadTheSame(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	bg := context.Background()
	pool, err := coredb.NewPool(bg, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	userID := ids.NewHyphenID(ids.PrefixUser)
	ctx := operations.WithPrincipal(bg, operations.Principal{Type: "user", ID: userID})
	journal := subscriptionjournal.Journal()
	const projectID = "prj01ABC"
	payload := map[string]any{"name": "lb-uk330"}

	read := func(id string) uk330Row {
		t.Helper()
		var r uk330Row
		require.NoError(t, pool.QueryRow(bg, `
			SELECT resource_type, action, project_id, payload::text
			  FROM kacho_nlb.nlb_outbox WHERE resource_id = $1`, id).
			Scan(&r.kind, &r.action, &r.projectID, &r.payload))
		return r
	}

	pairs := 0
	for _, kind := range uk330Kinds {
		for action, want := range uk330Want {
			// (1) Литеральная форма прежних писателей — та же транзакция помощника,
			// тот же оператор, что стоял в `outbox_emitter.go` и в
			// `emitReconcileFinalize` до перевода.
			legacyID := ids.NewID(ids.PrefixLoadBalancer)
			tx, err := journaltx.Begin(ctx, pool, journaltx.NewOptions(false))
			require.NoError(t, err, "ФИКСТУРА: транзакция помощника")
			_, err = tx.Exec(ctx, `
				INSERT INTO kacho_nlb.nlb_outbox (resource_type, resource_id, project_id, action, payload)
				VALUES ($1, $2, $3, $4, '{"name": "lb-uk330"}'::jsonb)`, kind, legacyID, projectID, action)
			require.NoError(t, err, "ФИКСТУРА: литеральная строка (%s, %s) отвергнута базой", kind, action)
			require.NoError(t, tx.Commit(ctx))
			legacy := read(legacyID)

			_, kindKnown := journal.Mapping.Kinds[legacy.kind]
			require.True(t, kindKnown, "УК3-30: вид %q прежней строки не читается словарём владельца", legacy.kind)
			got, changeKnown := journal.Mapping.Changes[legacy.action]
			require.True(t, changeKnown, "УК3-30: действие %q прежней строки не читается словарём владельца", legacy.action)
			require.Equal(t, want, got, "УК3-30: (%s, %s) прежней строки переведено не тем родом изменения", kind, action)

			// (2) Та же пара, записанная эмиттером модуля, хранится той же формой.
			emitID := ids.NewID(ids.PrefixLoadBalancer)
			w, err := kachopg.New(pool, nil).Writer(ctx)
			require.NoError(t, err)
			require.NoError(t, w.Outbox().Emit(ctx, kind, emitID, projectID, action, payload),
				"УК3-30: эмиттер модуля отверг пару (%s, %s), которую прежний писатель записывал", kind, action)
			require.NoError(t, w.Commit())
			require.Equal(t, legacy, read(emitID),
				"УК3-30: строка эмиттера модуля (%s, %s) хранится не той формой, что прежняя литеральная", kind, action)
			pairs++
		}
	}
	require.Equal(t, len(uk330Kinds)*len(uk330Want), pairs, "УК3-30: перепись пар неполна")
	t.Logf("УК3-30: пар «вид × действие» сверено %d (видов %d × действий %d)", pairs, len(uk330Kinds), len(uk330Want))
}
