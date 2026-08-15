// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package jobs

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
// # Почему здесь ПЕРЕЧНЯ НЕТ, а есть вызов у производителя
//
// Пробы этого пакета не пишут идентичности литералами — они их ПОРОЖДАЮТ
// (`"proj-" + ids.NewUID()[:8]`). Перечень, снятый с дерева, такую идентичность
// не видит by construction: до прогона её не существует. Поэтому строку учёта
// заводит тот же помощник, который порождает проект, — сразу и ровно на него.
//
// Это не послабление: механизм продолжает работать на каждой вставке (триггер
// списывает, удаление возвращает, отказ на исчерпании возможен). Меняется
// только величина, заведомо большая нужной, потому что предмет проб пакета —
// фоновые проходы, а не учёт. Поведение самого учёта проверяют пробы
// репозитория, и они заводят строки сами, чтобы состояние «потолка нет»
// осталось выразимым.

// fixtureQuotaLimit — предел служебных проектов проб.
const fixtureQuotaLimit = 1_000_000

// fixtureQuotaKinds — три тенантных вида домена, на которых висят триггеры учёта
// (миграция 0032). Выписаны токенами закрытой таблицы грантуемых пар.
var fixtureQuotaKinds = []string{
	"loadbalancer.networkLoadBalancers",
	"loadbalancer.targetGroups",
	"loadbalancer.listeners",
}

// fixtureNestedKind — ось вложенности домена: слушателей в одном балансировщике.
const fixtureNestedKind = "loadbalancer.networkLoadBalancers.listeners"

// seedQuotaForProject заводит строки учёта и проектный резолв вложенного вида
// для ОДНОГО, только что порождённого проекта.
//
// Зовётся помощником, который этот проект и придумал, — иначе идентичность
// осталась бы известна только ему.
func seedQuotaForProject(t testing.TB, ctx context.Context, pool *pgxpool.Pool, projectID string) {
	t.Helper()
	for _, k := range fixtureQuotaKinds {
		_, err := pool.Exec(ctx, `
			INSERT INTO kacho_nlb.project_resource_quotas
				(carrier_type, carrier_id, kind, used, limit_value,
				 source_scope, source_scope_id, limit_revision, account_id)
			VALUES ('project', $1, $2, 0, $3, 'DEFAULT', '', 0, 'acc-fixture')
			ON CONFLICT (carrier_type, carrier_id, kind) DO NOTHING`,
			projectID, k, int64(fixtureQuotaLimit))
		require.NoError(t, err, "фикстура учёта: строка проекта")
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO kacho_nlb.nested_quota_defaults
			(project_id, kind, limit_value, source_scope, source_scope_id,
			 limit_revision, account_id)
		VALUES ($1, $2, $3, 'DEFAULT', '', 0, 'acc-fixture')
		ON CONFLICT (project_id, kind) DO NOTHING`,
		projectID, fixtureNestedKind, int64(fixtureQuotaLimit))
	require.NoError(t, err, "фикстура учёта: резолв вложенного вида")
}
