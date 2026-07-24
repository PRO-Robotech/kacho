// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFGARegisterOutbox_PartitionHeadIndexCreated — миграция 0008 применяется и
// создаёт partial-index, на который опирается partition-head CLAIM
// register-drainer'а (`drainer.Config.PartitionColumn = "resource_id"`, wired в
// cmd/storage/register_drainer.go).
//
// Индекс — часть контракта, а не оптимизация: без него коррелированный
// `NOT EXISTS (… p.resource_id = t.resource_id AND p.id < t.id)` в claim-запросе
// вырождается в seq-scan на каждую кандидат-строку (квадратично на бэклоге).
// Сам порядок-инвариант (unregister → stale register НЕ воскрешает mirror-строку
// удалённого Volume/Snapshot) зафиксирован corelib-тестом
// drainer.Test_1_4_45_RegisterOutbox_UnregisterThenStaleRegister.
func TestFGARegisterOutbox_PartitionHeadIndexCreated(t *testing.T) {
	pool := newTestPool(t)

	var indexDef string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT indexdef FROM pg_indexes
		  WHERE schemaname = 'kacho_storage'
		    AND tablename  = 'fga_register_outbox'
		    AND indexname  = 'fga_register_outbox_partition_head_idx'`).Scan(&indexDef))

	// Ведущий ключ обязан совпадать с PartitionColumn; хвостовой id обслуживает
	// `p.id < t.id`; partial-предикат держит размер индекса на уровне бэклога.
	assert.Contains(t, indexDef, "(resource_id, id)")
	assert.Contains(t, indexDef, "WHERE (sent_at IS NULL)")
}
