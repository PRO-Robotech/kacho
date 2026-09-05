// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_cursor_plan_integration_test.go — задача #708, доказательство ДЕЙСТВИЯ.
//
// Гейт `internal/repohygiene` утверждает о схеме: индекс такой-то формы
// объявлен. Здесь утверждается о поведении: план настоящего Postgres на
// настоящей цепочке миграций vpc берёт порядок страницы ИЗ ЭТОГО индекса и не
// содержит узла сортировки. Разбор этих двух вопросов и контроль в обратную
// сторону — в шапке `pkg/listcursorplan`.
//
// Проба красна на состоянии ДО фикса: без `708001` план каждой из девяти таблиц
// несёт узел сортировки, потому что порядок брать неоткуда.
package cursorplan_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/listcursorplan"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/migrations"
)

func TestIntegration_VPC_CursorPagesTakeTheirOrderFromAnIndex(t *testing.T) {
	// Равенство по проекту — не «один из фильтров», а требование всех восьми
	// списков vpc (`project_id required`), поэтому оно стоит в каждом запросе,
	// как и в живом обходе.
	const byProject = "project_id = 'prj-plan'"

	listcursorplan.Run(t, listcursorplan.Options{
		Service: "vpc",
		Schema:  "kacho_vpc",
		FS:      migrations.FS,
		Cases: []listcursorplan.Case{
			{Table: "subnets", Index: "subnets_project_cursor_idx", Order: "created_at ASC, id ASC", Where: byProject},
			{Table: "networks", Index: "networks_project_cursor_idx", Order: "created_at ASC, id ASC", Where: byProject},
			{Table: "security_groups", Index: "security_groups_project_cursor_idx", Order: "created_at ASC, id ASC", Where: byProject},
			{Table: "route_tables", Index: "route_tables_project_cursor_idx", Order: "created_at ASC, id ASC", Where: byProject},
			{Table: "addresses", Index: "addresses_project_cursor_idx", Order: "created_at ASC, id ASC", Where: byProject},
			{Table: "gateways", Index: "gateways_project_cursor_idx", Order: "created_at ASC, id ASC", Where: byProject},
			{Table: "network_interfaces", Index: "network_interfaces_project_cursor_idx", Order: "created_at ASC, id ASC", Where: byProject},

			// Страницу адресов берут ТРИ чтения, и обязательное равенство у них
			// разное. Строка выше проверяет общий список проекта; две ниже —
			// чтения по подсети и по пулу, у которых ведущее равенство своё
			// (#912). Без них порядок из индекса получало одно чтение из трёх,
			// а остальные два сортировали весь набор под равенством: на
			// подсети с тысячей адресов это видно сразу и растёт линейно.
			{Table: "addresses", Index: "addresses_subnet_cursor_idx", Order: "created_at ASC, id ASC",
				Where: "internal_subnet_id = 'sub-plan'"},
			{Table: "addresses", Index: "addresses_pool_cursor_idx", Order: "created_at ASC, id ASC",
				Where: "external_ipv4 ->> 'address_pool_id' = 'apl-plan'"},

			// Пул адресов — админский ресурс, колонки проекта у него нет, а оба
			// фильтра списка необязательны: ведущего равенства не существует.
			{Table: "address_pools", Index: "address_pools_cursor_idx", Order: "created_at ASC, id ASC"},

			// Общий список операций проверяется ВДОБАВОК реалистично: строки
			// насыпаны, статистика собрана, ручек нет — план выбирает сам
			// планировщик. Таблица годится для этого потому, что не тянет за
			// собой ни внешних ключей, ни триггеров учёта.
			{
				Table: "operations", Index: "operations_cursor_idx", Order: "created_at ASC, id ASC",
				Seed: listcursorplan.SeedOperations("kacho_vpc"),
			},
		},
		// Контроль: у `description` индекса нет и не должно быть, поэтому план
		// этого обхода обязан нести сортировку. Без него утверждение «сортировки
		// нет» ничего не различает.
		Control: listcursorplan.Control{Table: "operations", Order: "description ASC"},
	})
}
