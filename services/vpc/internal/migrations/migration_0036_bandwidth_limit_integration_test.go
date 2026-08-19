// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrations_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// 0036 переносит нижний край арендаторского ограничения полосы с кода на
// конструкцию базы.
//
// # Почему граница берётся ИЗ КОНСТАНТЫ, а не выписана числом
//
// Число 1000 живёт в двух местах: в `CHECK` схемы и в
// `domain.GuaranteedInterfaceBandwidthFloorMbps`. Два места об одном предмете
// расходятся молча — если только их не сверить. Эти пробы предъявляют базе
// величины, ВЫЧИСЛЕННЫЕ из константы, поэтому правка константы без правки схемы
// краснеет здесь, а не проявляется на стенде.

// seedNICSubnetFor — родитель интерфейса (сеть + подсеть) прямым INSERT'ом.
func seedNICSubnetFor(t *testing.T, db *sql.DB, tag string) string {
	t.Helper()
	netID := ids.NewID(ids.PrefixNetwork)
	subID := ids.NewID(ids.PrefixSubnet)
	_, err := db.Exec(
		`INSERT INTO networks (id, project_id, name) VALUES ($1, $2, $3)`,
		netID, "prj-36", "n36"+tag)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO subnets (id, project_id, name, network_id, zone_id, placement_type, v4_cidr_blocks)
		VALUES ($1, $2, $3, $4, $5, 'ZONAL', ARRAY['10.36.0.0/24'])`,
		subID, "prj-36", "s36"+tag, netID, "zone-36")
	require.NoError(t, err)
	return subID
}

// insertNICWithLimit вписывает интерфейс с заданной величиной ограничения.
// Возвращает ошибку, а не роняет пробу: часть проб ниже утверждает отказ базы.
func insertNICWithLimit(db *sql.DB, subnetID string, limit int64, macTail int) error {
	_, err := db.Exec(`
		INSERT INTO network_interfaces (id, name, project_id, subnet_id, mac_address, bandwidth_limit_mbps)
		VALUES ($1, $1, $2, $3, $4, $5)`,
		ids.NewID(ids.PrefixNetworkInterface), "prj-36", subnetID,
		fmt.Sprintf("0e:36:00:00:00:%02x", macTail), limit)
	return err
}

// TestIntegration_Migration0036_FloorIsEnforcedByTheSchema — база сама отвергает
// величину на уровне опубликованного пола и ниже, и принимает первую осмысленную.
//
// Отрицания стоят В ПАРЕ с положительными: проверка, отвергающая всё, прошла бы
// каждую отрицательную половину и была бы полностью сломанной.
func TestIntegration_Migration0036_FloorIsEnforcedByTheSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	db, _ := openChainAt(t, 36)
	sub := seedNICSubnetFor(t, db, "a")

	const floor = int64(domain.GuaranteedInterfaceBandwidthFloorMbps)

	for _, tc := range []struct {
		name    string
		limit   int64
		accepts bool
		why     string
	}{
		{"не задано", 0, true,
			"ноль — единственное представление отсутствия и обязан проходить всегда"},
		{"на единицу ниже пола", floor - 1, false,
			"ниже гарантированного пола строка противоречит обещанию платформы самой себе"},
		{"ровно пол", floor, false,
			"граница строгая: ограничить ровно тем, что и так гарантировано, нечем"},
		{"на единицу выше пола", floor + 1, true,
			"первая осмысленная величина обязана проходить, иначе отрицания выше беспредметны"},
		{"заведомо большая величина", floor * 50, true,
			"верхний край — объявление посадки, и базе он не известен: она его не судит"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := insertNICWithLimit(db, sub, tc.limit, int(tc.limit%251)+1)
			if tc.accepts {
				require.NoError(t, err, tc.why)
				return
			}
			require.Error(t, err, tc.why)
			assert.Contains(t, err.Error(), "network_interfaces_bandwidth_limit_check",
				"отказ обязан прийти от ИМЕНОВАННОЙ проверки — по имени её находят в схеме")
		})
	}
}

// TestIntegration_Migration0036_DownRestoresTheShape — откат снимает и колонку, и
// проверку, а повторное применение проходит по той же дороге.
func TestIntegration_Migration0036_DownRestoresTheShape(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	db, _ := openChainAt(t, 36)

	count := func(q string, arg string) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRow(q, arg).Scan(&n))
		return n
	}
	const constraintQ = `SELECT count(*) FROM pg_constraint WHERE conname = $1`
	const columnQ = `SELECT count(*) FROM information_schema.columns
	                  WHERE table_schema = 'kacho_vpc' AND table_name = 'network_interfaces'
	                    AND column_name = $1`

	require.Equal(t, 1, count(constraintQ, "network_interfaces_bandwidth_limit_check"))
	require.Equal(t, 1, count(columnQ, "bandwidth_limit_mbps"))

	require.NoError(t, goose.DownTo(db, ".", 35))
	assert.Equal(t, 0, count(constraintQ, "network_interfaces_bandwidth_limit_check"))
	assert.Equal(t, 0, count(columnQ, "bandwidth_limit_mbps"))

	require.NoError(t, goose.UpTo(db, ".", 36), "Up после Down обязан примениться повторно")
	assert.Equal(t, 1, count(constraintQ, "network_interfaces_bandwidth_limit_check"))
	assert.Equal(t, 1, count(columnQ, "bandwidth_limit_mbps"))
}
