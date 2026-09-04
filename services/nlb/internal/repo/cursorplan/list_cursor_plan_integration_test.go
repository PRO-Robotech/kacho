// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_cursor_plan_integration_test.go — задача #708, доказательство ДЕЙСТВИЯ.
//
// Гейт `internal/repohygiene` утверждает о схеме: индекс такой-то формы
// объявлен. Здесь утверждается о поведении: план настоящего Postgres на
// настоящей цепочке миграций nlb берёт порядок страницы ИЗ ЭТОГО индекса и не
// содержит узла сортировки. Разбор обоих вопросов, довод в пользу
// детерминированной постановки и требование контроля — в шапке
// `pkg/listcursorplan`.
//
// Проба красна на состоянии ДО фикса: без `708001` план несёт узел сортировки,
// потому что порядок брать неоткуда.
package cursorplan_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/listcursorplan"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

func TestIntegration_NLB_CursorPagesTakeTheirOrderFromAnIndex(t *testing.T) {
	// Равенство по проекту требуют все три списка nlb (`project_id required`).
	const byProject = "project_id = 'prj-plan'"

	listcursorplan.Run(t, listcursorplan.Options{
		Service: "nlb",
		Schema:  "kacho_nlb",
		FS:      migrations.FS,
		Cases: []listcursorplan.Case{
			// Три таблицы, у которых индекс СТОЯЛ и был неприменим из-за
			// направления второго ключа. Именно здесь проба стоит дороже гейта:
			// прежний индекс формой почти неотличим от нового, и различает их
			// только план.
			{Table: "load_balancers", Index: "load_balancers_project_cursor_idx", Order: "created_at ASC, id ASC", Where: byProject},
			{Table: "listeners", Index: "listeners_project_cursor_idx", Order: "created_at ASC, id ASC", Where: byProject},
			{Table: "target_groups", Index: "target_groups_project_cursor_idx", Order: "created_at ASC, id ASC", Where: byProject},

			// Дочерний список целей: равенство по группе стоит прямо в тексте
			// запроса репозитория, поэтому оно и ведёт индекс.
			{Table: "targets", Index: "targets_group_cursor_idx", Order: "created_at ASC, id ASC", Where: "target_group_id = 'tg-plan'"},

			{
				Table: "operations", Index: "operations_cursor_idx", Order: "created_at ASC, id ASC",
				Seed: listcursorplan.SeedOperations("kacho_nlb"),
			},
		},
		Control: listcursorplan.Control{Table: "operations", Order: "description ASC"},
	})
}
