// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journal_initiator_integration_test.go — полоса RED S1-A1 (issue-2918, NTF-3) для vpc:
// журнал модуля несёт инициатора транзакции, а строки без него не существует.
//
// Сценарии приёмки `docs/specs/sub-phase-NTF-3-kacho-modules-notifications-acceptance.md`
// (отпечаток ac1f9fc9…): NTF3-62 — прямая вставка в `kacho_vpc.vpc_outbox` и функция базы
// `subnets_outbox_emit_route_table_change` (снятие таблицы маршрутов, к которой привязана
// подсеть); УК3-28 (заказ замысла issue-2918, CX3C-01) — переиспользованное соединение.
// Обвязка — `journalprobe_helpers_test.go`.

package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/migrations"
)

// vpcJournal — журнал vpc (`subscriptionjournal.Table` = `kacho_vpc.vpc_outbox`).
var vpcJournal = journalTable{
	schema: "kacho_vpc", name: "vpc_outbox", timeColumn: "created_at", idColumn: "resource_id",
	insert: `INSERT INTO kacho_vpc.vpc_outbox (resource_kind, resource_id, event_type, payload, project_id)
	         VALUES ('Network', $1, 'CREATED', '{}'::jsonb, 'prj-62')`,
	insertNullTime: `INSERT INTO kacho_vpc.vpc_outbox (resource_kind, resource_id, event_type, payload, project_id, created_at)
	         VALUES ('Network', $1, 'CREATED', '{}'::jsonb, 'prj-62', NULL)`,
}

func TestJournal_NTF362_VPCRowWithoutInitiatorIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	runJournalRowWithoutInitiatorIsRefused(t, vpcJournal.open(t, migrations.FS), vpcJournal)
}

func TestJournal_UK328_VPCReusedConnectionDoesNotCarryInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	runReusedConnectionDoesNotCarryInitiator(t, vpcJournal.open(t, migrations.FS), vpcJournal)
}

// TestJournal_NTF362_VPCFunctionWithoutInitiatorRollsBack — снятие таблицы маршрутов,
// к которой привязана подсеть: внешний ключ `ON DELETE SET NULL` правит
// `subnets.route_table_id`, и функция пишет `Subnet UPDATED` в журнал.
func TestJournal_NTF362_VPCFunctionWithoutInitiatorRollsBack(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	conn := vpcJournal.open(t, migrations.FS)
	runFunctionProbe(t, conn, vpcJournal, functionProbe{
		function: "subnets_outbox_emit_route_table_change",
		prepare: func(t *testing.T, conn *pgx.Conn, tag string) string {
			net, rt, sub := "enp-ntf362"+tag, "enr-ntf362"+tag, "e9b-ntf362"+tag
			seed(t, conn, `INSERT INTO kacho_vpc.networks (id, project_id, name) VALUES ($1, 'prj-62', $2)`, net, "n62"+tag)
			seed(t, conn, `INSERT INTO kacho_vpc.route_tables (id, project_id, name, network_id) VALUES ($1, 'prj-62', $2, $3)`, rt, "r62"+tag, net)
			seed(t, conn, `
				INSERT INTO kacho_vpc.subnets (id, project_id, name, network_id, zone_id, placement_type, route_table_id)
				VALUES ($1, 'prj-62', $2, $3, 'zone-62', 'ZONAL', $4)`, sub, "s62"+tag, net, rt)
			return sub
		},
		change: func(tx pgx.Tx, tag string) error {
			_, err := tx.Exec(context.Background(), `DELETE FROM kacho_vpc.route_tables WHERE id = $1`, "enr-ntf362"+tag)
			return err
		},
		applied: func(t *testing.T, conn *pgx.Conn, tag string) bool {
			var n int
			require.NoError(t, conn.QueryRow(context.Background(),
				`SELECT count(*) FROM kacho_vpc.route_tables WHERE id = $1`, "enr-ntf362"+tag).Scan(&n))
			return n == 0
		},
	})
}
