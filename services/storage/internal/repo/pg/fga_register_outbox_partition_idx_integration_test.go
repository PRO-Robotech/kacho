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

// TestFGARegisterOutboxStorage_PendingIndexSetIsExactlyTwo — предмет: НАБОР частичных индексов по
// неотправленным строкам, а не наличие двух нужных.
//
// Проверка «эти два индекса есть» утверждает наличие и никогда — отсутствие,
// поэтому третий индекс по тем же строкам она пропускает. А третий индекс здесь
// не безобиден: очередь почти всё время пуста, последний сбор статистики почти
// всегда пришёлся на пустой бэклог, и во всплеск планировщик входит с оценкой в
// одну строку. На такой оценке сортировка бесплатна, поэтому любой более узкий
// частичный индекс по тем же строкам выглядит дешевле упорядоченного — и план
// перестаёт останавливаться рано: анти-соединение по партиции прогоняется по
// разу на КАЖДУЮ неотправленную строку, то есть выборка читает всю очередь,
// которую разгребает.
//
// Поэтому утверждается равенство множества: перечисляем определения всех
// частичных индексов по `sent_at IS NULL` и требуем ровно два — по ключу
// партиции и по порядку выборки.
func TestFGARegisterOutboxStorage_PendingIndexSetIsExactlyTwo(t *testing.T) {
	pool := newTestPool(t)

	var defs []string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT coalesce(array_agg(indexdef ORDER BY indexname), '{}')
		   FROM pg_indexes
		  WHERE schemaname = 'kacho_storage'
		    AND tablename  = 'fga_register_outbox'
		    AND indexdef LIKE '%WHERE (sent_at IS NULL)%'`).Scan(&defs))

	assert.Lenf(t, defs, 2,
		"kacho_storage.fga_register_outbox обязана нести РОВНО два частичных индекса по неотправленным строкам — "+
			"(resource_id, id) для поиска головы партиции и (attempt_count, id) для "+
			"упорядоченного внешнего прохода. Любой третий порядок по тем же строкам планировщик "+
			"берёт при статистике пустой очереди, и выборка теряет раннюю остановку по LIMIT. "+
			"Найдено: %v", defs)

	joined := ""
	for _, d := range defs {
		joined += d + "\n"
	}
	assert.Contains(t, joined, "(resource_id, id)")
	assert.Contains(t, joined, "(attempt_count, id)")
}
