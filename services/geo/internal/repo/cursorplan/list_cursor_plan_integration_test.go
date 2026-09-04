// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_cursor_plan_integration_test.go — задача #708, доказательство ДЕЙСТВИЯ.
//
// Гейт `internal/repohygiene` утверждает о схеме: индекс такой-то формы
// объявлен. Здесь утверждается о поведении: план настоящего Postgres на
// настоящей цепочке миграций geo берёт порядок страницы ИЗ ЭТОГО индекса и не
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
	"github.com/PRO-Robotech/kacho/services/geo/internal/migrations"
)

func TestIntegration_Geo_CursorPagesTakeTheirOrderFromAnIndex(t *testing.T) {
	listcursorplan.Run(t, listcursorplan.Options{
		Service: "geo",
		Schema:  "kacho_geo",
		FS:      migrations.FS,
		Cases: []listcursorplan.Case{
			// Каталоги geo (регионы, зоны) листаются курсором по `id` и
			// обслуживаются первичным ключом — предметом #708 они не являются.
			// Курсор по времени создания у geo один: общий список операций.
			{
				Table: "operations", Index: "operations_cursor_idx", Order: "created_at ASC, id ASC",
				Seed: listcursorplan.SeedOperations("kacho_geo"),
			},
		},
		Control: listcursorplan.Control{Table: "operations", Order: "description ASC"},
	})
}
