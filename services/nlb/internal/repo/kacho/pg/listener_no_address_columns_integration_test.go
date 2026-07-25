// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// listener_no_address_columns_integration_test.go — сжатие мёртвой
// address-модели листенера.
//
// VIP консолидирован на LoadBalancer (`load_balancers.address_v4/_v6` +
// `address_id_v4/_v6` + `vip_origin_v4/_v6`). У листенера собственного адреса
// нет ни в контракте (proto: `ip_version`/`address_id`/`allocated_address`/
// `subnet_id` — reserved 12-15), ни в коде: `Listener.Create` — plain INSERT,
// который НИКОГДА не заполняет эти поля, а единственные их писатели
// (`SetVIP`/`SetAllocatedAddress`) не имеют ни одного production-вызывающего.
// Оставшиеся артефакты (колонки, индекс `listeners_address_idx`, ветка release
// VIP в Listener.Delete, boot-backfill `vip_origin`) описывают инвариант,
// которого сервис не реализует, — следующий контрибьютор «чинит» код под них
// (architecture.md doc-truthfulness + LEAN; продолжение миграции 0025, которая
// сняла мёртвый UNIQUE и оставила колонки как code-change).
//
// Гейт держит компрессию: колонки не должны вернуться (вместе с ними вернулась
// бы и вся мёртвая машинерия вокруг).
func TestListeners_AddressColumnsAreGone(t *testing.T) {
	dsn := setupTestDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	for _, col := range []string{"address_id", "allocated_address", "subnet_id", "ip_version", "vip_origin"} {
		var exists bool
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				 WHERE table_schema = 'kacho_nlb' AND table_name = 'listeners'
				   AND column_name = $1)`, col).Scan(&exists))
		require.Falsef(t, exists,
			"kacho_nlb.listeners.%s must be dropped: the VIP lives on the LoadBalancer and "+
				"nothing ever writes this column — keeping it documents an invariant the service "+
				"does not implement", col)
	}

	// Индекс по мёртвой колонке (частичный `WHERE address_id <> ''` — предикат,
	// которому не соответствует ни одна строка, которую сервис способен создать).
	var idxExists bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			 WHERE schemaname = 'kacho_nlb' AND indexname = 'listeners_address_idx')`).Scan(&idxExists))
	require.False(t, idxExists,
		"listeners_address_idx indexes a dropped column and matched no row the service could produce")
}
