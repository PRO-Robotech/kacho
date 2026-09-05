// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrations_test

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// TestMigration_ListenerTargetGroup_SameProjectFK — DB-level lock for the
// cross-project wiring BOLA (data-integrity.md ban #10: a within-service
// referential invariant must be a DB construction, not a software check).
//
// The use-case precheck runs in the handler thread while the listener row is
// INSERTed by the async worker — a concurrent TargetGroup.Move committing in that
// window would leave a durable cross-project reference. The composite FK
// (default_tg_fk, project_id) → target_groups(id, project_id) closes it atomically.
//
// RED before 0023 (FK on `id` alone): the cross-project INSERT succeeds.
// GREEN: it is rejected with SQLSTATE 23503, while the same-project INSERT and the
// unwired (empty reference) INSERT still pass, and deleting a wired TG is still
// RESTRICTed.
func TestMigration_ListenerTargetGroup_SameProjectFK(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	// Своя ПУСТАЯ база на одном контейнере пакета: тест сам проигрывает цепочку
	// (goose.Up ниже), поэтому мигрированный шаблон был бы не той точкой старта.
	db, err := sql.Open("pgx", pgtest.NewEmptyDB(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.Up(db, "."))

	// Two projects; each owns a region-coherent TargetGroup + a LoadBalancer.
	const (
		ownProject    = "prj0own00000000000001"
		victimProject = "prj0victim0000000001"
		region        = "ru-central1"
	)
	// Учёт числа ресурсов (миграция 0032): вставка строки ресурса СПИСЫВАЕТ место,
	// и списать его не с чего, пока у проекта нет строки учёта — «не сказано» это
	// отказ, а не «без предела». Проба проигрывает цепочку сама и вставляет строки
	// напрямую, поэтому строки учёта заводит тоже сама.
	//
	// Это НЕ послабление: механизм продолжает работать на каждой вставке ниже —
	// триггер списывает, отказ на исчерпании возможен. Меняется ровно то, что у
	// этих двух проектов предел заведомо больше, чем им нужно, потому что предмет
	// пробы — ссылочная целостность, а не учёт.
	for _, proj := range []string{ownProject, victimProject} {
		for _, kind := range []string{
			"loadbalancer.networkLoadBalancers",
			"loadbalancer.targetGroups",
			"loadbalancer.listeners",
		} {
			_, err = db.Exec(`
				INSERT INTO kacho_nlb.project_resource_quotas
					(carrier_type, carrier_id, kind, used, limit_value,
					 source_scope, source_scope_id, limit_revision, account_id)
				VALUES ('project', $1, $2, 0, 1000000, 'DEFAULT', '', 0, 'acc-mig')`,
				proj, kind)
			require.NoError(t, err)
		}
		// Проектный резолв ВЛОЖЕННОГО вида: из него триггер берёт снимок, заводя
		// строку учёта нового балансировщика. Без него балансировщик заводится без
		// строки учёта, и первый же слушатель получает «потолок не назван» — громко,
		// но неверно: потолок назван, просто не доехал.
		_, err = db.Exec(`
			INSERT INTO kacho_nlb.nested_quota_defaults
				(project_id, kind, limit_value, source_scope, source_scope_id,
				 limit_revision, account_id)
			VALUES ($1, 'loadbalancer.networkLoadBalancers.listeners',
				1000000, 'DEFAULT', '', 0, 'acc-mig')`, proj)
		require.NoError(t, err)
	}

	_, err = db.Exec(`
		INSERT INTO kacho_nlb.target_groups (id, project_id, region_id, name, port)
		VALUES ('tgr-own00000000000001', $1, $3, 'own-tg', 8080),
		       ('tgr-victim0000000001',  $2, $3, 'victim-tg', 8080)`,
		ownProject, victimProject, region)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO kacho_nlb.load_balancers
			(id, project_id, region_id, name, type, placement_type, status)
		VALUES ('nlb-own00000000000001', $1, $2, 'own-lb', 'INTERNAL', 'REGIONAL', 'ACTIVE')`,
		ownProject, region)
	require.NoError(t, err)

	insertListener := func(id, tgID string) error {
		_, e := db.Exec(`
			INSERT INTO kacho_nlb.listeners
				(id, load_balancer_id, project_id, region_id, name, protocol, port,
				 default_target_group_id, status)
			VALUES ($1, 'nlb-own00000000000001', $2, $3, $4, 'TCP', $5, $6, 'ACTIVE')`,
			id, ownProject, region, "l"+id[4:12], portOf(id), tgID)
		return e
	}

	// Cross-project wiring — must be rejected by the FK (23503).
	err = insertListener("lst-cross00000000001", "tgr-victim0000000001")
	require.Error(t, err, "a listener must not reference a TargetGroup of another project")
	require.Contains(t, err.Error(), "listeners_target_group_fk",
		"rejection must come from the same-project composite FK (mapped to the contract tone in pg/errors.go)")

	// Same-project wiring — unaffected.
	require.NoError(t, insertListener("lst-same000000000001", "tgr-own00000000000001"))

	// Unwired listener (empty reference → generated default_tg_fk IS NULL) — unaffected.
	require.NoError(t, insertListener("lst-unwired000000001", ""))

	// ON DELETE RESTRICT still holds for a wired TargetGroup.
	_, err = db.Exec(`DELETE FROM kacho_nlb.target_groups WHERE id = 'tgr-own00000000000001'`)
	require.Error(t, err, "deleting a wired TargetGroup must stay RESTRICTed")

	// The victim's TargetGroup stays deletable — the attacker never pinned it.
	_, err = db.Exec(`DELETE FROM kacho_nlb.target_groups WHERE id = 'tgr-victim0000000001'`)
	require.NoError(t, err)
}

// portOf derives a per-listener port from the id suffix so the
// (load_balancer_id, port, protocol) UNIQUE index never masks an FK assertion.
func portOf(id string) int {
	sum := 0
	for _, c := range id {
		sum += int(c)
	}
	return 1024 + sum%40000
}
