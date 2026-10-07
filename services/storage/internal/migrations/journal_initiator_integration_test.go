// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journal_initiator_integration_test.go — полоса RED S1-A1 (issue-2918, NTF-3) для storage:
// журнал модуля несёт инициатора транзакции, а строки без него не существует.
//
// Сценарии приёмки NTF-3 «модули kacho подключаются к сервису уведомлений» (каталог `docs/specs` воркспейса)
// (отпечаток ac1f9fc9…): NTF3-62 — прямая вставка в `kacho_storage.storage_outbox` и три
// функции базы: `storage_outbox_emit` (вставка диска), `storage_outbox_emit_attachment`
// (вставка привязки диска), `storage_outbox_emit_source` (вставка диска с источником-снимком);
// УК3-28 (заказ замысла issue-2918, CX3C-01) — переиспользованное соединение.
// Обвязка — `journalprobe_helpers_test.go`.

package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/migrations"
)

// storageJournal — журнал storage (`subscriptionjournal.Table` = `kacho_storage.storage_outbox`).
var storageJournal = journalTable{
	schema: "kacho_storage", name: "storage_outbox", timeColumn: "created_at", idColumn: "resource_id",
	insert: `INSERT INTO kacho_storage.storage_outbox (resource_kind, resource_id, project_id, event_type, payload)
	         VALUES ('Volume', $1, 'prj-62', 'CREATED', '{}'::jsonb)`,
	insertNullTime: `INSERT INTO kacho_storage.storage_outbox (resource_kind, resource_id, project_id, event_type, payload, created_at)
	         VALUES ('Volume', $1, 'prj-62', 'CREATED', '{}'::jsonb, NULL)`,
}

func TestJournal_NTF362_StorageRowWithoutInitiatorIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	runJournalRowWithoutInitiatorIsRefused(t, storageJournal.open(t, migrations.FS), storageJournal)
}

func TestJournal_UK328_StorageReusedConnectionDoesNotCarryInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	runReusedConnectionDoesNotCarryInitiator(t, storageJournal.open(t, migrations.FS), storageJournal)
}

// seedDiskType — тип диска, без которого том не вписать (FK). Тип диска журнала не пишет.
func seedDiskType(t *testing.T, conn *pgx.Conn, tag string) string {
	t.Helper()
	id := "dty-ntf362" + tag
	seed(t, conn, `INSERT INTO kacho_storage.disk_types (id) VALUES ($1)`, id)
	return id
}

// insertVolume — вставка тома; source — id снимка-источника либо "".
func insertVolume(tx pgx.Tx, id, diskType, source string) error {
	_, err := tx.Exec(context.Background(), `
		INSERT INTO kacho_storage.volumes (id, project_id, name, zone_id, disk_type_id, size_bytes, source_snapshot_id)
		VALUES ($1, 'prj-62', $1, 'zone-62', $2, 1073741824, NULLIF($3, ''))`, id, diskType, source)
	return err
}

func rowExists(t *testing.T, conn *pgx.Conn, stmt, id string) bool {
	t.Helper()
	var n int
	require.NoError(t, conn.QueryRow(context.Background(), stmt, id).Scan(&n))
	return n == 1
}

// TestJournal_NTF362_StorageFunctionsWithoutInitiatorRollBack — по подпробе на каждую из
// трёх функций базы storage, каждая на собственной базе.
func TestJournal_NTF362_StorageFunctionsWithoutInitiatorRollBack(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}

	t.Run("storage_outbox_emit", func(t *testing.T) {
		conn := storageJournal.open(t, migrations.FS)
		runFunctionProbe(t, conn, storageJournal, functionProbe{
			function: "storage_outbox_emit",
			prepare: func(t *testing.T, conn *pgx.Conn, tag string) string {
				seedDiskType(t, conn, tag)
				return "epd-ntf362" + tag
			},
			change: func(tx pgx.Tx, tag string) error {
				return insertVolume(tx, "epd-ntf362"+tag, "dty-ntf362"+tag, "")
			},
			applied: func(t *testing.T, conn *pgx.Conn, tag string) bool {
				return rowExists(t, conn, `SELECT count(*) FROM kacho_storage.volumes WHERE id = $1`, "epd-ntf362"+tag)
			},
		})
	})

	t.Run("storage_outbox_emit_attachment", func(t *testing.T) {
		conn := storageJournal.open(t, migrations.FS)
		runFunctionProbe(t, conn, storageJournal, functionProbe{
			function: "storage_outbox_emit_attachment",
			prepare: func(t *testing.T, conn *pgx.Conn, tag string) string {
				dt := seedDiskType(t, conn, tag)
				vol := "epd-ntf362att" + tag
				require.NoError(t, inJournalTx(conn, initiator(probeInitiator), func(tx pgx.Tx) error {
					return insertVolume(tx, vol, dt, "")
				}), "фикстура: том под привязку")
				return vol
			},
			change: func(tx pgx.Tx, tag string) error {
				_, err := tx.Exec(context.Background(), `
					INSERT INTO kacho_storage.volume_attachments (volume_id, instance_id, project_id, zone_id, device_name)
					VALUES ($1, $2, 'prj-62', 'zone-62', 'vdb')`, "epd-ntf362att"+tag, "ins-ntf362"+tag)
				return err
			},
			applied: func(t *testing.T, conn *pgx.Conn, tag string) bool {
				return rowExists(t, conn, `SELECT count(*) FROM kacho_storage.volume_attachments WHERE volume_id = $1`, "epd-ntf362att"+tag)
			},
		})
	})

	t.Run("storage_outbox_emit_source", func(t *testing.T) {
		conn := storageJournal.open(t, migrations.FS)
		// Вставку тома слушают две функции: `storage_outbox_emit` (строка тома) и родословная,
		// зовущая `storage_outbox_emit_source` (строка источника). Чтобы отказ судил именно
		// функцию источника, строка тома в базе ЭТОЙ подпробы снята: триггер тома выключен.
		// Близнец идёт на той же базе, поэтому разница между ним и отрицанием — один факт.
		_, err := conn.Exec(context.Background(),
			`ALTER TABLE kacho_storage.volumes DISABLE TRIGGER volumes_storage_outbox_emit`)
		require.NoError(t, err, "фикстура: функция источника изолирована от строки тома")
		runFunctionProbe(t, conn, storageJournal, functionProbe{
			function: "storage_outbox_emit_source",
			prepare: func(t *testing.T, conn *pgx.Conn, tag string) string {
				seedDiskType(t, conn, tag)
				snp := "snp-ntf362" + tag
				seed(t, conn, `INSERT INTO kacho_storage.snapshots (id, project_id, name) VALUES ($1, 'prj-62', $2)`, snp, "snp62"+tag)
				return snp
			},
			change: func(tx pgx.Tx, tag string) error {
				return insertVolume(tx, "epd-ntf362src"+tag, "dty-ntf362"+tag, "snp-ntf362"+tag)
			},
			applied: func(t *testing.T, conn *pgx.Conn, tag string) bool {
				return rowExists(t, conn, `SELECT count(*) FROM kacho_storage.volumes WHERE id = $1`, "epd-ntf362src"+tag)
			},
		})
	})
}
