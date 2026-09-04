// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/nameformdb"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// TestIntegration_VPC_NameFormConstraintIsEnforced — задача #721, довесок к её
// предмету.
//
// Задача пришла с утверждением «у vpc проба есть, у четырёх других нет». Замер
// уточнил его: миграция 715001 ставит форму имени ДЕВЯТИ таблицам vpc, а
// `TestIntegration_NetworkRepo_CheckConstraints` вставляет строку в ОДНУ из них
// (сеть). То есть у сервиса, считавшегося покрытым, действие ограничения
// доказано для одной таблицы из девяти.
//
// Соседняя проба не снимается и не переписывается: её предмет — ОТОБРАЖЕНИЕ
// отказа базы в сигнальную ошибку репозитория (23514 → helpers.ErrInvalidArg),
// и он остаётся её. Здесь предмет другой — действие ограничения на каждой
// таблице, которой миграция его поставила.
func TestIntegration_VPC_NameFormConstraintIsEnforced(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (testcontainers Postgres) — skipped with -short")
	}
	ctx := context.Background()

	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	// Идентичность проекта фикстура учёта соберёт из исходников пакета сама
	// (quota_fixture_integration_test.go): без строки учёта КАЖДАЯ вставка
	// ресурса отвергалась бы «потолок не назван», и положительный контроль
	// перестал бы быть контролем.
	const project = "project-nameform"

	// Цепочка родителей. Подсеть ссылается на сеть, шлюз и интерфейс — на
	// подсеть; без них вставки отвергались бы внешним ключом, то есть по чужой
	// причине.
	const (
		parentNetwork = "net-nameform-parent"
		parentSubnet  = "sub-nameform-parent"
	)
	_, err = pool.Exec(ctx, `INSERT INTO kacho_vpc.networks (id, project_id, name)
	                         VALUES ($1, $2, 'nameform-parent-net')`, parentNetwork, project)
	require.NoError(t, err, "фикстура: сеть-родитель")
	_, err = pool.Exec(ctx, `INSERT INTO kacho_vpc.subnets
	            (id, project_id, network_id, name, placement_type, zone_id, region_id)
	        VALUES ($1, $2, $3, 'nameform-parent-sub', 'ZONAL', 'zone-nameform', '')`,
		parentSubnet, project, parentNetwork)
	require.NoError(t, err, "фикстура: подсеть-родитель")

	nameformdb.Probe{
		Schema: "kacho_vpc",
		Tables: []nameformdb.Table{
			{
				Name: "networks",
				Row: func(name string, seq int) (string, []any) {
					// vrf_id НЕ подаётся: его выдаёт платформа, и присланное
					// значение отвергает страж таблицы — то есть строка не дошла
					// бы до проверки формы имени вовсе.
					return `INSERT INTO kacho_vpc.networks (id, project_id, name) VALUES ($1, $2, $3)`,
						[]any{fmt.Sprintf("net-%017d", seq), project, name}
				},
			},
			{
				Name: "route_tables",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_vpc.route_tables (id, project_id, network_id, name) VALUES ($1, $2, $3, $4)`,
						[]any{fmt.Sprintf("rtb-%017d", seq), project, parentNetwork, name}
				},
			},
			{
				Name: "subnets",
				Row: func(name string, seq int) (string, []any) {
					// Якорь размещения взаимоисключающий: ZONAL требует непустой
					// зоны и пустого региона (subnets_placement_payload_chk).
					return `INSERT INTO kacho_vpc.subnets
					            (id, project_id, network_id, name, placement_type, zone_id, region_id)
					        VALUES ($1, $2, $3, $4, 'ZONAL', 'zone-nameform', '')`,
						[]any{fmt.Sprintf("sub-%017d", seq), project, parentNetwork, name}
				},
			},
			{
				Name: "addresses",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_vpc.addresses (id, project_id, name) VALUES ($1, $2, $3)`,
						[]any{fmt.Sprintf("adr-%017d", seq), project, name}
				},
			},
			{
				Name: "security_groups",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_vpc.security_groups (id, project_id, network_id, name) VALUES ($1, $2, $3, $4)`,
						[]any{fmt.Sprintf("sg-%017d", seq), project, parentNetwork, name}
				},
			},
			{
				Name: "gateways",
				Row: func(name string, seq int) (string, []any) {
					// EGRESS_ONLY, а не NAT: у NAT-шлюза внешний адрес обязателен
					// (gateways_nat_has_address_chk), и его отсутствие отвергло бы
					// строку раньше формы имени.
					return `INSERT INTO kacho_vpc.gateways (id, project_id, subnet_id, gateway_type, name)
					        VALUES ($1, $2, $3, 'EGRESS_ONLY', $4)`,
						[]any{fmt.Sprintf("gw-%017d", seq), project, parentSubnet, name}
				},
			},
			{
				Name: "network_interfaces",
				Row: func(name string, seq int) (string, []any) {
					// Аппаратный адрес — своей формы и свой на каждую строку.
					return `INSERT INTO kacho_vpc.network_interfaces (id, project_id, subnet_id, mac_address, name)
					        VALUES ($1, $2, $3, $4, $5)`,
						[]any{
							fmt.Sprintf("nic-%017d", seq), project, parentSubnet,
							fmt.Sprintf("02:00:00:00:%02x:%02x", seq/256, seq%256), name,
						}
				},
			},
			{
				Name: "cidr_groups",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_vpc.cidr_groups (id, project_id, name) VALUES ($1, $2, $3)`,
						[]any{fmt.Sprintf("cg-%017d", seq), project, name}
				},
			},
			{
				Name: "address_pools",
				Row: func(name string, seq int) (string, []any) {
					// Вид пула — единственный допускаемый проверкой значений.
					return `INSERT INTO kacho_vpc.address_pools (id, kind, name) VALUES ($1, 1, $2)`,
						[]any{fmt.Sprintf("apl-%017d", seq), name}
				},
			},
		},
	}.Run(ctx, t, pool)
}
