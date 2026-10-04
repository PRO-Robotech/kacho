// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journal_initiator_integration_test.go — полоса RED S1-A1 (issue-2918, NTF-3) для compute:
// журнал модуля несёт инициатора транзакции, а строки без него не существует.
//
// Сценарии приёмки `docs/specs/sub-phase-NTF-3-kacho-modules-notifications-acceptance.md`
// (отпечаток ac1f9fc9…): NTF3-62 — прямая вставка в `public.compute_outbox` (функций базы,
// пишущих журнал, у compute нет — §1.13 приёмки);
// УК3-28 (заказ замысла issue-2918, CX3C-01) — переиспользованное соединение.
// Обвязка — `journalprobe_helpers_test.go`.

package migrations_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/services/compute/internal/migrations"
)

// computeJournal — журнал compute (`subscriptionjournal.Table` = `compute_outbox`, схема public).
var computeJournal = journalTable{
	schema: "public", name: "compute_outbox", timeColumn: "created_at", idColumn: "resource_id",
	insert: `INSERT INTO public.compute_outbox (resource_kind, resource_id, event_type, payload, project_id)
	         VALUES ('Instance', $1, 'CREATED', '{}'::jsonb, 'prj-62')`,
	insertNullTime: `INSERT INTO public.compute_outbox (resource_kind, resource_id, event_type, payload, project_id, created_at)
	         VALUES ('Instance', $1, 'CREATED', '{}'::jsonb, 'prj-62', NULL)`,
}

func TestJournal_NTF362_ComputeRowWithoutInitiatorIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	runJournalRowWithoutInitiatorIsRefused(t, computeJournal.open(t, migrations.FS), computeJournal)
}

func TestJournal_UK328_ComputeReusedConnectionDoesNotCarryInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	runReusedConnectionDoesNotCarryInitiator(t, computeJournal.open(t, migrations.FS), computeJournal)
}
