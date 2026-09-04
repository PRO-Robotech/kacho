// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrations_test

import (
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// Выданный VIP обязан нести ключ к своей аренде — на уровне СХЕМЫ (#467).
//
// ПРЕДМЕТ. Строка с непустым `address_v6` и пустым `address_id_v6` была законной.
// Смысла у неё нет: адрес выдан, а вернуть его нечем. Освобождение при удалении
// принимало такую строку за «у этого семейства аренды нет», возвращало успех,
// и строка удалялась — после чего аренду не видел уже никто (реконсайлер выбирает
// только DELETING/CREATING), а подсеть не удалялась никогда.
//
// Инвариант внутри одного сервиса держится конструкцией схемы, а не проверкой в
// коде (`data-integrity.md`, ban #10): проверка ловит уже записанное,
// ограничение не даёт записать.
//
// Проверяется ЭКВИВАЛЕНТНОСТЬ, поэтому запрещены оба перекоса, и у отрицаний
// есть положительный контроль — иначе «отвергает» было бы неотличимо от
// «отвергает всё».
func TestMigration_VIPLeaseIDRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	db, err := sql.Open("pgx", pgtest.NewEmptyDB(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.Up(db, "."))

	// Перепись: оба ограничения существуют. Без этого «ничего не вставилось»
	// было бы неотличимо от «ограничения нет, а мешает что-то другое».
	for _, name := range []string{
		"load_balancers_v4_lease_id_present_check",
		"load_balancers_v6_lease_id_present_check",
	} {
		var n int
		require.NoError(t, db.QueryRow(`
			SELECT count(*) FROM pg_constraint c
			  JOIN pg_class t ON t.oid = c.conrelid
			  JOIN pg_namespace ns ON ns.oid = t.relnamespace
			 WHERE ns.nspname = 'kacho_nlb' AND t.relname = 'load_balancers' AND c.conname = $1`,
			name).Scan(&n))
		require.Equalf(t, 1, n, "ограничение %s обязано существовать", name)
	}

	// Потолок учёта для проекта: без него триггер учёта отвергает вставку
	// балансировщика раньше, чем до неё доберётся проверяемое здесь ограничение,
	// и проба судила бы о чужом отказе.
	_, err = db.Exec(`
		INSERT INTO kacho_nlb.project_resource_quotas
			(carrier_type, carrier_id, kind, used, limit_value,
			 source_scope, source_scope_id, limit_revision, account_id)
		VALUES ('project', 'prj-a', 'loadbalancer.networkLoadBalancers', 0, 1000000,
		        'DEFAULT', '', 0, 'acc-mig')`)
	require.NoError(t, err)

	insert := func(t *testing.T, suffix, addrV4, addrIDV4, addrV6, addrIDV6 string) error {
		t.Helper()
		_, err := db.Exec(`
			INSERT INTO kacho_nlb.load_balancers
			  (id, project_id, region_id, name, type, status,
			   address_v4, address_id_v4, address_v6, address_id_v6, ip_families)
			VALUES ($1, 'prj-a', 'ru-central1', $2, 'EXTERNAL', 'INACTIVE',
			        $3, $4, $5, $6, '{IPV4,IPV6}')`,
			"lb-"+suffix, "edge-"+suffix, addrV4, addrIDV4, addrV6, addrIDV6)
		return err
	}

	t.Run("положительный контроль: обе пары согласованы", func(t *testing.T) {
		require.NoError(t, insert(t, "ok-both", "10.0.0.7", "adr-v4", "2a02::7", "adr-v6"))
	})

	t.Run("положительный контроль: семейства v6 нет вовсе", func(t *testing.T) {
		require.NoError(t, insert(t, "ok-v4only", "10.0.0.8", "adr-v4b", "", ""))
	})

	// Отрицания: оба перекоса, на обоих семействах.
	for _, tc := range []struct {
		name                               string
		addrV4, addrIDV4, addrV6, addrIDV6 string
		wantConstraint                     string
	}{
		{"адрес v6 без ключа аренды — предмет #467", "10.0.0.9", "adr-v4c", "2a02::9", "",
			"load_balancers_v6_lease_id_present_check"},
		{"адрес v4 без ключа аренды", "10.0.0.10", "", "", "",
			"load_balancers_v4_lease_id_present_check"},
		{"ключ аренды v6 без адреса — аренда, которую не показывают", "", "", "", "adr-v6c",
			"load_balancers_v6_lease_id_present_check"},
		{"ключ аренды v4 без адреса", "", "adr-v4d", "", "",
			"load_balancers_v4_lease_id_present_check"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := insert(t, fmt.Sprintf("bad-%p", &tc), tc.addrV4, tc.addrIDV4, tc.addrV6, tc.addrIDV6)
			require.Error(t, err, "перекошенная пара обязана быть отвергнута схемой")
			require.Contains(t, err.Error(), tc.wantConstraint,
				"отказ обязан называть то ограничение, которое сработало")
		})
	}
}
