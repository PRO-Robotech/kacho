// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journal_initiator_integration_test.go — полоса RED S1-A1 (issue-2918, NTF-3) для nlb:
// журнал модуля несёт инициатора транзакции, строки без него не существует, а вид журнала
// принимает слово сигнала ленты `notification`.
//
// Сценарии приёмки `docs/specs/sub-phase-NTF-3-kacho-modules-notifications-acceptance.md`
// (отпечаток ac1f9fc9…): NTF3-62 — прямая вставка в `kacho_nlb.nlb_outbox` и функция базы
// `lb_status_recompute` (вставка приёмника с группой целей у балансировщика `INACTIVE`);
// ключ журнала `notification` (§1.9, Д3, §3 шаг (3)); УК3-28 (заказ замысла issue-2918,
// CX3C-01) — переиспользованное соединение. Обвязка — `journalprobe_helpers_test.go`.

package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// nlbJournal — журнал nlb (`subscriptionjournal.Table` = `kacho_nlb.nlb_outbox`; вид —
// `resource_type`, действие — `action`, время — `emitted_at`).
var nlbJournal = journalTable{
	schema: "kacho_nlb", name: "nlb_outbox", timeColumn: "emitted_at", idColumn: "resource_id",
	insert: `INSERT INTO kacho_nlb.nlb_outbox (resource_type, resource_id, project_id, action, payload)
	         VALUES ('nlb_load_balancer', $1, 'prj-62', 'CREATED', '{}'::jsonb)`,
	insertNullTime: `INSERT INTO kacho_nlb.nlb_outbox (resource_type, resource_id, project_id, action, payload, emitted_at)
	         VALUES ('nlb_load_balancer', $1, 'prj-62', 'CREATED', '{}'::jsonb, NULL)`,
}

func TestJournal_NTF362_NLBRowWithoutInitiatorIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	runJournalRowWithoutInitiatorIsRefused(t, nlbJournal.open(t, migrations.FS), nlbJournal)
}

func TestJournal_UK328_NLBReusedConnectionDoesNotCarryInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	runReusedConnectionDoesNotCarryInitiator(t, nlbJournal.open(t, migrations.FS), nlbJournal)
}

// TestJournal_NTF362_NLBFunctionWithoutInitiatorRollsBack — вставка приёмника с группой
// целей у балансировщика `INACTIVE`: функция переводит балансировщик в `ACTIVE` CAS-ом и
// пишет `nlb_load_balancer UPDATED` в журнал.
func TestJournal_NTF362_NLBFunctionWithoutInitiatorRollsBack(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	conn := nlbJournal.open(t, migrations.FS)
	runFunctionProbe(t, conn, nlbJournal, functionProbe{
		function: "lb_status_recompute",
		prepare: func(t *testing.T, conn *pgx.Conn, tag string) string {
			lb, tg := "nlb-ntf362"+tag, "ntg-ntf362"+tag
			seed(t, conn, `
				INSERT INTO kacho_nlb.load_balancers (id, project_id, region_id, name, type, status)
				VALUES ($1, 'prj-62', 'region-62', $2, 'EXTERNAL', 'INACTIVE')`, lb, "lb62"+tag)
			seed(t, conn, `
				INSERT INTO kacho_nlb.target_groups (id, project_id, region_id, name, port)
				VALUES ($1, 'prj-62', 'region-62', $2, 8080)`, tg, "tg62"+tag)
			return lb
		},
		change: func(tx pgx.Tx, tag string) error {
			_, err := tx.Exec(context.Background(), `
				INSERT INTO kacho_nlb.listeners (id, load_balancer_id, project_id, region_id, name, protocol, port, default_target_group_id, status)
				VALUES ($1, $2, 'prj-62', 'region-62', $3, 'TCP', 80, $4, 'ACTIVE')`,
				"nls-ntf362"+tag, "nlb-ntf362"+tag, "ls62"+tag, "ntg-ntf362"+tag)
			return err
		},
		applied: func(t *testing.T, conn *pgx.Conn, tag string) bool {
			var n int
			require.NoError(t, conn.QueryRow(context.Background(),
				`SELECT count(*) FROM kacho_nlb.listeners WHERE id = $1`, "nls-ntf362"+tag).Scan(&n))
			return n == 1
		},
	})
}

// TestJournal_S1A1_NLBKindCheckAdmitsNotificationSignal — ограничение вида журнала nlb
// принимает ключ журнала сигнала ленты `notification` (без этого строка сигнала nlb
// отвергнута базой) и остаётся закрытым для прочих слов.
//
// Три строки отличаются ровно видом: известный вид (положительный контроль) — принят;
// `notification` — принят; постороннее слово — отвергнуто 23514 тем же ограничением.
func TestJournal_S1A1_NLBKindCheckAdmitsNotificationSignal(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	conn := nlbJournal.open(t, migrations.FS)
	insertKind := func(kind, id string) error {
		return execJournalTx(conn, initiator(probeInitiator), `
			INSERT INTO kacho_nlb.nlb_outbox (resource_type, resource_id, project_id, action, payload)
			VALUES ($1, $2, 'prj-62', 'UPDATED', '{}'::jsonb)`, kind, id)
	}

	require.NoError(t, insertKind("nlb_load_balancer", "kind-control"),
		"положительный контроль: известный вид принят — иначе проба судила бы не вид")

	assert.NoError(t, insertKind("notification", "kind-notification"),
		"ключ журнала сигнала ленты `notification` обязан приниматься ограничением вида nlb")

	err := insertKind("not_a_kind", "kind-foreign")
	if assert.Error(t, err, "ограничение вида остаётся закрытым: постороннее слово отвергнуто") {
		pgErr := pgErrorOf(err)
		if assert.NotNil(t, pgErr) {
			assert.Equal(t, sqlStateCheck, pgErr.Code, pgErr.Message)
			assert.Contains(t, pgErr.ConstraintName, "resource_type", pgErr.Message)
		}
	}
}
