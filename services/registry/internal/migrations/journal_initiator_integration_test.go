// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journal_initiator_integration_test.go — полоса RED S1-A1 (issue-2918, NTF-3) для registry:
// журнал модуля несёт инициатора транзакции, а строки без него не существует.
//
// Сценарии приёмки `docs/specs/sub-phase-NTF-3-kacho-modules-notifications-acceptance.md`
// (отпечаток ac1f9fc9…): NTF3-62 — прямая вставка в `kacho_registry.registry_resource_journal`
// и функция базы `registries_journal_emit` (вставка реестра); УК3-28 (заказ замысла
// issue-2918, CX3C-01) — переиспользованное соединение. Обвязка — `journalprobe_helpers_test.go`.

package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/registry/internal/migrations"
)

// registryJournal — журнал registry (`subscriptionjournal.Table` = `kacho_registry.registry_resource_journal`).
var registryJournal = journalTable{
	schema: "kacho_registry", name: "registry_resource_journal", timeColumn: "created_at", idColumn: "resource_id",
	insert: `INSERT INTO kacho_registry.registry_resource_journal (resource_kind, resource_id, project_id, event_type, payload)
	         VALUES ('Registry', $1, 'prj-62', 'CREATED', '{}'::jsonb)`,
	insertNullTime: `INSERT INTO kacho_registry.registry_resource_journal (resource_kind, resource_id, project_id, event_type, payload, created_at)
	         VALUES ('Registry', $1, 'prj-62', 'CREATED', '{}'::jsonb, NULL)`,
}

func TestJournal_NTF362_RegistryRowWithoutInitiatorIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	runJournalRowWithoutInitiatorIsRefused(t, registryJournal.open(t, migrations.FS), registryJournal)
}

func TestJournal_UK328_RegistryReusedConnectionDoesNotCarryInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	runReusedConnectionDoesNotCarryInitiator(t, registryJournal.open(t, migrations.FS), registryJournal)
}

// TestJournal_NTF362_RegistryFunctionWithoutInitiatorRollsBack — вставка реестра: функция
// пишет `Registry CREATED` в журнал.
func TestJournal_NTF362_RegistryFunctionWithoutInitiatorRollsBack(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	conn := registryJournal.open(t, migrations.FS)
	runFunctionProbe(t, conn, registryJournal, functionProbe{
		function: "registries_journal_emit",
		prepare: func(_ *testing.T, _ *pgx.Conn, tag string) string {
			return "crp-ntf362" + tag
		},
		change: func(tx pgx.Tx, tag string) error {
			_, err := tx.Exec(context.Background(), `
				INSERT INTO kacho_registry.registries (id, project_id, name, region_id)
				VALUES ($1, 'prj-62', $2, 'region-62')`, "crp-ntf362"+tag, "reg62"+tag)
			return err
		},
		applied: func(t *testing.T, conn *pgx.Conn, tag string) bool {
			var n int
			require.NoError(t, conn.QueryRow(context.Background(),
				`SELECT count(*) FROM kacho_registry.registries WHERE id = $1`, "crp-ntf362"+tag).Scan(&n))
			return n == 1
		},
	})
}
