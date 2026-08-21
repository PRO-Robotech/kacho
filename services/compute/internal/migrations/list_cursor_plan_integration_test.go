// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_cursor_plan_integration_test.go — задача #708, доказательство ДЕЙСТВИЯ.
//
// Гейт `internal/repohygiene` утверждает о схеме: индекс такой-то формы
// объявлен. Здесь утверждается о поведении: план настоящего Postgres на
// настоящей цепочке миграций compute берёт порядок страницы ИЗ ЭТОГО индекса и не
// содержит узла сортировки. Разбор обоих вопросов, довод в пользу
// детерминированной постановки и требование контроля — в шапке
// `internal/listcursorplan`.
//
// Проба красна на состоянии ДО фикса: без `708001` план несёт узел сортировки,
// потому что порядок брать неоткуда.
package migrations_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/internal/listcursorplan"
	"github.com/PRO-Robotech/kacho/services/compute/internal/migrations"
)

func TestIntegration_Compute_CursorPagesTakeTheirOrderFromAnIndex(t *testing.T) {
	listcursorplan.Run(t, listcursorplan.Options{
		Service: "compute",
		Schema:  "public",
		FS:      migrations.FS,
		Cases: []listcursorplan.Case{
			// Тенантские таблицы compute свои курсорные индексы уже несут
			// (`instances_project_cursor_idx` и соседние). Без порядка оставался
			// общий список операций: у него все фильтры необязательны, поэтому
			// ведущего равенства нет и индекс несёт только ключи курсора.
			//
			// Случай реалистичный: строки насыпаны, статистика собрана, ручек
			// нет — план выбирает сам планировщик по своей стоимости.
			{
				Table: "operations", Index: "operations_cursor_idx", Order: "created_at ASC, id ASC",
				Seed: listcursorplan.SeedOperations("public"),
			},
			// Положительный контроль формы: уже существовавший индекс
			// `instances_project_cursor_idx` распознаётся тем же способом, что и
			// новые. Без него проба говорила бы только о том, что добавлено в
			// этой правке, и не отличала бы «покрыто» от «не смотрели».
			{
				Table: "instances", Index: "instances_project_cursor_idx",
				Order: "created_at ASC, id ASC", Where: "project_id = 'prj-plan'",
			},
		},
		Control: listcursorplan.Control{Table: "operations", Order: "description ASC"},
	})
}
