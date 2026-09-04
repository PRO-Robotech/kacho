// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_cursor_plan_integration_test.go — задача #708, доказательство ДЕЙСТВИЯ.
//
// Гейт `internal/repohygiene` утверждает о схеме: индекс такой-то формы
// объявлен. Здесь утверждается о поведении: план настоящего Postgres на
// настоящей цепочке миграций storage берёт порядок страницы ИЗ ЭТОГО индекса и не
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
	"github.com/PRO-Robotech/kacho/services/storage/internal/migrations"
)

func TestIntegration_Storage_CursorPagesTakeTheirOrderFromAnIndex(t *testing.T) {
	listcursorplan.Run(t, listcursorplan.Options{
		Service: "storage",
		Schema:  "kacho_storage",
		FS:      migrations.FS,
		Cases: []listcursorplan.Case{
			// Три каталога: тенантского измерения у них нет, поэтому ведущего
			// равенства не существует и индекс несёт только ключи курсора.
			// У классов диска до этой правки не было ни одного индекса, кроме
			// первичного ключа.
			{Table: "disk_types", Index: "disk_types_cursor_idx", Order: "created_at ASC, id ASC"},
			{Table: "disk_type_bindings", Index: "disk_type_bindings_cursor_idx", Order: "created_at ASC, id ASC"},
			{Table: "storage_backends", Index: "storage_backends_cursor_idx", Order: "created_at ASC, id ASC"},
			{
				Table: "operations", Index: "operations_cursor_idx", Order: "created_at ASC, id ASC",
				Seed: listcursorplan.SeedOperations("kacho_storage"),
			},
			// Положительный контроль формы: тома свой курсорный индекс уже
			// несли (`0013_tenant_cursor_indexes`), и он распознаётся тем же
			// способом, что и новые.
			{
				Table: "volumes", Index: "volumes_project_cursor_idx",
				Order: "created_at ASC, id ASC", Where: "project_id = 'prj-plan'",
			},
		},
		Control: listcursorplan.Control{Table: "operations", Order: "description ASC"},
	})
}
