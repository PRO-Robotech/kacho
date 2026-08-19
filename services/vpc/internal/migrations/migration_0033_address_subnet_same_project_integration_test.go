// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// 0033 переносит «адрес и его подсеть — одного проекта» из кода в базу. Эти пробы
// про две вещи, которых сам ключ не покрывает: что миграция делает со строками,
// которые прежнее соглашение уже пропустило, и что её откат возвращает схему в
// прежнюю форму, а не в похожую.
//
// Действие ключа на живых писателях проверяется отдельно и конкурентно —
// `repo/address_subnet_project_pair_integration_test.go`.

// seedSubnetAt вписывает сеть и подсеть заданного проекта прямым INSERT'ом:
// строки, которые правит 0033, писались тогда, когда проверки не было, и путь
// use-case'а к ним отношения не имеет.
func seedSubnetAt(t *testing.T, db *sql.DB, projectID, zone string) string {
	t.Helper()
	netID := ids.NewID(ids.PrefixNetwork)
	subID := ids.NewID(ids.PrefixSubnet)
	_, err := db.Exec(
		`INSERT INTO networks (id, project_id, name) VALUES ($1, $2, $3)`,
		netID, projectID, "n33"+zone)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO subnets (id, project_id, name, network_id, zone_id, placement_type, v4_cidr_blocks)
		VALUES ($1, $2, $3, $4, $5, 'ZONAL', ARRAY['10.33.0.0/24'])`,
		subID, projectID, "s33"+zone, netID, zone)
	require.NoError(t, err)
	return subID
}

// seedInternalAddress вписывает адрес со ссылкой на подсеть прямым INSERT'ом.
// Возвращает ошибку, а не роняет пробу: одна из проб ниже утверждает, что до
// 0033 такая вставка ПРОХОДИТ, вторая — что после она отвергнута.
func seedInternalAddress(db *sql.DB, projectID, subnetID string) error {
	_, err := db.Exec(`
		INSERT INTO addresses (id, name, project_id, addr_type, ip_version, internal_ipv4)
		VALUES ($1, $1, $2, 1, 1, jsonb_build_object('address', '', 'subnet_id', $3::text))`,
		ids.NewID(ids.PrefixAddress), projectID, subnetID)
	return err
}

// TestIntegration_Migration0033_RefusesToApplyOverCrossProjectRows — строка,
// нарушающая инвариант, ОСТАНАВЛИВАЕТ применение и называет их число.
//
// Молчаливая чистка здесь была бы распоряжением чужим ресурсом: строка адреса
// принадлежит арендатору, и решить её судьбу может владелец, а не миграция.
// Поэтому исход — отказ с предикатом в тексте, по которому оператор находит эти
// строки сам.
//
// Положительный контроль — второй половиной этой же пробы: на базе, где такой
// строки нет, та же миграция применяется. Без него «отказ» был бы неотличим от
// миграции, которая не применяется никогда.
func TestIntegration_Migration0033_RefusesToApplyOverCrossProjectRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Run("cross-project row stops the migration", func(t *testing.T) {
		db, _ := openChainAt(t, 32)
		sub := seedSubnetAt(t, db, "prj-33-owner", "zone-a")
		require.NoError(t, seedInternalAddress(db, "prj-33-other", sub),
			"схема 0001..0031 принимает ссылку через границу проекта — это и есть предмет 0033")

		err := goose.UpTo(db, ".", 33)
		require.Error(t, err, "0033 обязана отказаться применяться поверх нарушающей строки")
		assert.Contains(t, err.Error(), "1 address row(s) reference a subnet of another project",
			"отказ обязан называть ЧИСЛО строк — по нему оператор понимает объём, а не факт")
	})

	t.Run("conforming rows let the migration through", func(t *testing.T) {
		db, _ := openChainAt(t, 32)
		sub := seedSubnetAt(t, db, "prj-33-owner", "zone-b")
		require.NoError(t, seedInternalAddress(db, "prj-33-owner", sub))

		require.NoError(t, goose.UpTo(db, ".", 33),
			"на согласованных строках 0033 обязана применяться — иначе отказ выше ничего не различает")

		// И ключ после применения действительно стоит: без этого утверждения
		// «применилась» означало бы только «не упала».
		var pair int
		require.NoError(t, db.QueryRow(
			`SELECT count(*) FROM pg_constraint WHERE conname = 'addresses_subnet_project_fk'`).Scan(&pair))
		assert.Equal(t, 1, pair)
		require.Error(t, seedInternalAddress(db, "prj-33-other", sub),
			"после применения ссылка через границу проекта обязана быть отвергнута базой")
	})
}

// TestIntegration_Migration0033_DownRestoresTheSingleColumnKey — откат возвращает
// схему к форме 0001, а не к похожей.
//
// 0033 СНИМАЕТ одностолбцовый ключ `addresses.internal_subnet_id → subnets(id)`
// как поглощённый парой. Откат обязан вернуть именно его — под тем же именем,
// которое Postgres породил бы сам, — иначе после Down схема отличалась бы от
// схемы, в которую Down якобы возвращает, и следующая попытка Up сорвалась бы на
// поиске ключа по каталогу.
func TestIntegration_Migration0033_DownRestoresTheSingleColumnKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	db, _ := openChainAt(t, 33)

	count := func(name string) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRow(
			`SELECT count(*) FROM pg_constraint WHERE conname = $1`, name).Scan(&n))
		return n
	}

	require.Equal(t, 1, count("addresses_subnet_project_fk"), "после Up стоит пара")
	require.Equal(t, 0, count("addresses_internal_subnet_id_fkey"), "и только она")
	require.Equal(t, 1, count("subnets_project_id_id_key"), "цель пары объявлена уникальной")

	require.NoError(t, goose.DownTo(db, ".", 32))
	assert.Equal(t, 0, count("addresses_subnet_project_fk"), "Down снимает пару")
	assert.Equal(t, 1, count("addresses_internal_subnet_id_fkey"), "Down возвращает одностолбцовый ключ")
	assert.Equal(t, 0, count("subnets_project_id_id_key"), "и снимает цель, которая была нужна только паре")

	// Повторный Up обязан пройти по той же дороге: блок снятия ищет ключ по
	// каталогу и падает, если не нашёл, — значит этот прогон доказывает, что Down
	// вернул именно то, что Up ожидает найти.
	require.NoError(t, goose.UpTo(db, ".", 33), "Up после Down обязан примениться повторно")
	assert.Equal(t, 1, count("addresses_subnet_project_fk"))
	assert.Equal(t, 0, count("addresses_internal_subnet_id_fkey"))
}
