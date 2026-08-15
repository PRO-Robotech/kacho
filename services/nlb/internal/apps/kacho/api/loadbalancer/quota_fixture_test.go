// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// Фикстура учёта числа ресурсов для интеграционных проб этого пакета.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S4 п.1.
//
// # Зачем
//
// С появлением учёта вставка строки ресурса СПИСЫВАЕТ место, а списать его не с
// чего, пока у проекта нет строки учёта: «не сказано» — отказ, а не «без
// предела» (V2-3). На живом пути строку заводит совещательная полоса ПЕРЕД
// writer-транзакцией; пробы этого пакета собирают use-case БЕЗ полосы (соседа с
// величинами в них нет), поэтому базу в то же состояние приводит фикстура.
//
// # Почему это НЕ послабление
//
// Механизм продолжает работать на каждой вставке: триггер списывает, удаление
// возвращает, отказ на исчерпании возможен. Меняется ровно одно — величина у
// этих проектов заведомо больше, чем им нужно, потому что предмет проб пакета
// лежит в другом месте. Поведение САМОГО учёта проверяют пробы репозитория
// (`internal/repo/kacho/pg/quota_integration_test.go`), и они заводят строки
// сами, чтобы состояние «потолка нет» осталось выразимым.
//
// # Перечень снят с дерева
//
// Предикат читает ОБЕ формы литерала — Go-строку и SQL-строку внутри неё:
//
//	grep -rhoE '"prj[^"]*"' *_test.go      # Go
//	grep -rhoE "'prj[^']*'" *_test.go      # SQL внутри Go-строки
//
// Вторая форма добавлена не из осторожности, а по находке: проба сливающего
// прохода вставляет строку сырым SQL, и предикат по Go-литералам её проект не
// видел. Радиус берётся по имени механизма, а не по форме, в которой его
// заметили. Идентичность,
// сюда не попавшая, получит громкий отказ, называющий предмет и проект, — то
// есть расхождение перечня с деревом наблюдаемо в тот же прогон, а не молча.
const fixtureQuotaLimit = 1_000_000

var fixtureQuotaKinds = []string{
	"loadbalancer.networkLoadBalancers",
	"loadbalancer.targetGroups",
	"loadbalancer.listeners",
}

const fixtureNestedKind = "loadbalancer.networkLoadBalancers.listeners"

var fixtureProjects = []string{
	"prj", "prj[^", "prj-a",
	"prj-abc", "prj-acme-test", "prj-b",
	"prj-dst", "prj-fanout", "prj-move-dst",
	"prj-move-src", "prj-ops", "prj-other",
	"prj-pool-release", "prj-q", "prj-sa",
	"prj-src", "prj-u", "prj-victim",
	"prj-x",
}

// seedQuotaFixture заводит строки учёта и проектный резолв вложенного вида.
func seedQuotaFixture(t testing.TB, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	for _, p := range fixtureProjects {
		for _, k := range fixtureQuotaKinds {
			_, err := pool.Exec(ctx, `
				INSERT INTO kacho_nlb.project_resource_quotas
					(carrier_type, carrier_id, kind, used, limit_value,
					 source_scope, source_scope_id, limit_revision, account_id)
				VALUES ('project', $1, $2, 0, $3, 'DEFAULT', '', 0, 'acc-fixture')
				ON CONFLICT (carrier_type, carrier_id, kind) DO NOTHING`,
				p, k, int64(fixtureQuotaLimit))
			require.NoError(t, err, "фикстура учёта: строка проекта")
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO kacho_nlb.nested_quota_defaults
				(project_id, kind, limit_value, source_scope, source_scope_id,
				 limit_revision, account_id)
			VALUES ($1, $2, $3, 'DEFAULT', '', 0, 'acc-fixture')
			ON CONFLICT (project_id, kind) DO NOTHING`,
			p, fixtureNestedKind, int64(fixtureQuotaLimit))
		require.NoError(t, err, "фикстура учёта: резолв вложенного вида")
	}
}
