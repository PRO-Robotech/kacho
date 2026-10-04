// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// Проба NTF3-162 (issue-2918, полоса S1-A8): посев стенда
// deploy/scripts/vpc-address-pool-baseline.sql пишет строку журнала vpc с
// инициатором `system:stand-seed`, и повтор посева строк журнала не пишет.
//
// Приёмка NTF-3 «модули kacho подключаются к сервису уведомлений»
// (каталог `docs/specs` воркспейса, отпечаток ac1f9fc9…), сценарий NTF3-162 и
// его близнец (§10: «пул уже засеян / не засеян»). Половина сценария о строке
// ленты `resource-event` здесь не утверждается: таблицу ленты и функцию базы,
// ставящую её строку, заводит полоса S2-B1, и её проба идёт там же.
//
// Посев исполняется ТЕМ ЖЕ файлом, что исполняет рецепт подъёма стенда, и тем же
// способом — одним запросом, то есть одной транзакцией (скрипт доставки зовёт
// psql с `--single-transaction`). Копия SQL в пробе разошлась бы с посевом молча.
//
// Порядок проверок несущий: сперва предпосылка обвязки (журнал vpc несёт колонку
// `initiator` — её заводит миграция полосы S1-A1), затем предмет. Обратный порядок
// дал бы базе без миграции выдать себя за посев без инициатора.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// standSeedInitiator — инициатор, под которым посев стенда пишет журнал (Р3,
// NTF3-162). Значение утверждается дословно: это наблюдаемое сценария.
const standSeedInitiator = "system:stand-seed"

// journalSeedRow — строка журнала vpc после позиции P0.
type journalSeedRow struct {
	Seq        int64
	Kind       string
	ResourceID string
	Event      string
	Initiator  string
	HasTime    bool
}

// vpcJournalPosition — последняя позиция журнала vpc (P0 сценария).
func vpcJournalPosition(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var p int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT coalesce(max(sequence_no), 0) FROM vpc_outbox`).Scan(&p))
	return p
}

// vpcJournalRowsAfter — строки журнала vpc строго после позиции p.
func vpcJournalRowsAfter(t *testing.T, pool *pgxpool.Pool, p int64) []journalSeedRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT sequence_no, resource_kind, resource_id, event_type, initiator, created_at IS NOT NULL
		  FROM vpc_outbox WHERE sequence_no > $1 ORDER BY sequence_no`, p)
	require.NoError(t, err)
	defer rows.Close()
	var out []journalSeedRow
	for rows.Next() {
		var r journalSeedRow
		require.NoError(t, rows.Scan(&r.Seq, &r.Kind, &r.ResourceID, &r.Event, &r.Initiator, &r.HasTime))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// requireJournalCarriesInitiatorColumn — предпосылка обвязки, не предмет: журнал
// vpc после миграций под-фазы несёт колонку `initiator` (полоса S1-A1). Без неё
// исход пробы — «условие не создано», а не красный.
func requireJournalCarriesInitiatorColumn(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'kacho_vpc' AND table_name = 'vpc_outbox' AND column_name = 'initiator'`).Scan(&n))
	if n != 1 {
		t.Fatalf("УСЛОВИЕ НЕ СОЗДАНО: у kacho_vpc.vpc_outbox нет колонки initiator (найдено %d) — "+
			"миграция журнала под-фазы (полоса S1-A1) не применена; это не красный посева", n)
	}
}

// TestAddressPoolBaseline_NTF3162_StandSeedWritesJournalWithInitiator — посев
// стенда на базе без пула полосы пишет ровно одну строку журнала `AddressPool
// CREATED` по засеянному пулу, с инициатором `system:stand-seed` и временем; второе
// исполнение того же файла (пул уже есть) строк журнала не пишет.
func TestAddressPoolBaseline_NTF3162_StandSeedWritesJournalWithInitiator(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	pool, b := newBaselinePool(t)
	ctx := context.Background()

	// ── предпосылка обвязки ────────────────────────────────────────────────
	requireJournalCarriesInitiatorColumn(t, pool)
	require.Zero(t, laneCount(t, pool), "Given: пул EXTERNAL_PUBLIC «по умолчанию» без зоны обязан отсутствовать")
	var mine int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM address_pools WHERE id = $1`, b.ID).Scan(&mine))
	require.Zero(t, mine, "Given: засеваемого пула до посева быть не должно")

	// ── When: посев стенда, не засеян ──────────────────────────────────────
	p0 := vpcJournalPosition(t, pool)
	_, err := pool.Exec(ctx, b.SQL)
	require.NoError(t, err, "посев стенда %s не исполнился одной транзакцией против базы vpc после миграций под-фазы",
		vpcPoolBaselineSQLRel)

	// ── Then ───────────────────────────────────────────────────────────────
	after := vpcJournalRowsAfter(t, pool, p0)
	require.Len(t, after, 1, "после P0=%d в журнале vpc не ровно одна строка: %+v", p0, after)
	got := after[0]
	require.Equal(t, "AddressPool", got.Kind, "вид строки журнала посева")
	require.Equal(t, b.ID, got.ResourceID, "строка журнала посева не по засеянному пулу")
	require.Equal(t, "CREATED", got.Event, "событие строки журнала посева")
	require.Equal(t, standSeedInitiator, got.Initiator, "инициатор строки журнала посева")
	require.True(t, got.HasTime, "строка журнала посева без времени")
	require.Equal(t, 1, laneCount(t, pool), "после посева в полосе не ровно один пул")

	// ── близнец: пул уже засеян — тот же файл, ровно один изменённый факт ──
	p1 := vpcJournalPosition(t, pool)
	_, err = pool.Exec(ctx, b.SQL)
	require.NoError(t, err, "повторный посев стенда не исполнился")
	require.Empty(t, vpcJournalRowsAfter(t, pool, p1),
		"повторный посев (пул уже есть) написал строки журнала после позиции %d", p1)
	require.Equal(t, 1, laneCount(t, pool), "повторный посев изменил полосу")
	t.Logf("NTF3-162: P0=%d, строка %d (%s %s %s, initiator=%q); близнец: P1=%d, строк после — 0",
		p0, got.Seq, got.Kind, got.ResourceID, got.Event, got.Initiator, p1)
}
