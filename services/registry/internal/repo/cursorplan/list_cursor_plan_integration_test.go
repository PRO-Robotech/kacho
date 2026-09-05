// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_cursor_plan_integration_test.go — задача #708, доказательство ДЕЙСТВИЯ.
//
// Гейт `internal/repohygiene` утверждает о схеме: индекс такой-то формы
// объявлен. Здесь утверждается о поведении: план настоящего Postgres на
// настоящей цепочке миграций registry берёт порядок страницы ИЗ ЭТОГО индекса и не
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
	"github.com/PRO-Robotech/kacho/services/registry/internal/migrations"
)

func TestIntegration_Registry_CursorPagesTakeTheirOrderFromAnIndex(t *testing.T) {
	listcursorplan.Run(t, listcursorplan.Options{
		Service: "registry",
		Schema:  "kacho_registry",
		FS:      migrations.FS,
		Cases: []listcursorplan.Case{
			{
				Table: "operations", Index: "operations_cursor_idx", Order: "created_at ASC, id ASC",
				Seed: listcursorplan.SeedOperations("kacho_registry"),
			},
			// Положительный контроль формы: реестры свой курсорный индекс уже
			// несли, и он распознаётся тем же способом.
			{
				Table: "registries", Index: "registries_project_cursor_idx",
				Order: "created_at ASC, id ASC", Where: "project_id = 'prj-plan'",
			},
			// Второй положительный контроль, и он не про полноту: у настроек
			// репозитория курсор устроен ИНАЧЕ — вторым ключом идёт имя, а не
			// идентификатор. Индекс `(registry_id, created_at, name)` это
			// повторяет дословно; проба показывает, что распознаётся форма
			// обхода, а не заученная пара колонок.
			{
				Table: "repository_configs", Index: "repository_configs_cursor_idx",
				Order: "created_at ASC, name ASC", Where: "registry_id = 'reg-plan'",
			},
		},
		Control: listcursorplan.Control{Table: "operations", Order: "description ASC"},
	})
}
