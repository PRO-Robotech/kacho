// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrations_test

// RED-фаза (ban #12) под задачу продукта #1473 — площадка ZONAL-балансировщика
// как ДИСКРИМИНАТОР, а не как ещё одна колонка.
//
// Инвариант держит база, а не use-case (ban #10): непустая зона допустима РОВНО
// при placement_type='ZONAL'. Обе стороны проверяются отдельно, потому что
// ломаются они по-разному:
//
//   · зона у REGIONAL/EXTERNAL — УТЕЧКА РАЗМЕЩЕНИЯ: наружу уехала бы координата,
//     которую платформа выбирает сама (внешний public-VIP) либо которой не
//     существует (anycast);
//   · ZONAL без зоны — то самое состояние, ради снятия которого поле заведено:
//     «размещение зональное» сказано, а КАКОЕ — нет.
//
// Положительные контроли стоят рядом с каждым отрицанием: без них «вставка
// отвергнута» было бы истинно и на схеме, отвергающей всё.

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// insertLB — минимальная строка балансировщика с заданным размещением и зоной.
// Возвращает ошибку вставки (nil — принято).
func insertLB(db *sql.DB, id, typ, placementType, zoneID string) error {
	_, err := db.Exec(`
		INSERT INTO kacho_nlb.load_balancers
			(id, project_id, region_id, name, type, status, placement_type, zone_id)
		VALUES ($1, 'prj-a', 'ru-central1', $2, $3, 'INACTIVE', $4, $5)`,
		id, id, typ, placementType, zoneID)
	return err
}

// TestMigration_LoadBalancerZone_IsExclusiveToZonalPlacement — дискриминатор
// зоны: непустая зона РОВНО при ZONAL, обе стороны на каждой оси.
func TestMigration_LoadBalancerZone_IsExclusiveToZonalPlacement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	// Своя ПУСТАЯ база на общем контейнере пакета: его TestMain намеренно не
	// мигрирует шаблон (страж удалений сам ведёт цепочку), поэтому цепочку
	// прогоняет тест.
	db, err := sql.Open("pgx", pgtest.NewEmptyDB(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.Up(db, "."))

	// Потолок учёта для проекта: без него триггер учёта отвергает вставку раньше,
	// чем до неё доберётся проверяемое здесь ограничение, и проба судила бы о
	// чужом отказе.
	_, err = db.Exec(`
		INSERT INTO kacho_nlb.project_resource_quotas
			(carrier_type, carrier_id, kind, used, limit_value,
			 source_scope, source_scope_id, limit_revision, account_id)
		VALUES ('project', 'prj-a', 'loadbalancer.networkLoadBalancers', 0, 1000000,
		        'DEFAULT', '', 0, 'acc-mig')`)
	require.NoError(t, err)

	// Положительный: ZONAL со своей площадкой — принимается.
	require.NoError(t, insertLB(db, "lb-zonal-ok", "INTERNAL", "ZONAL", "ru-central1-a"),
		"зональный балансировщик со своей зоной обязан вставляться")

	// Отрицание: ZONAL без площадки — та самая неназванная зона, ради которой
	// поле и заведено.
	require.Error(t, insertLB(db, "lb-zonal-nozone", "INTERNAL", "ZONAL", ""),
		"«размещение зональное» без указания какое — состояние, которое поле снимает")

	// Положительный: REGIONAL без зоны — anycast, зональной координаты нет.
	require.NoError(t, insertLB(db, "lb-regional-ok", "INTERNAL", "REGIONAL", ""),
		"anycast зоны не несёт by construction")

	// Отрицание: REGIONAL с зоной — утечка координаты, которой у него нет.
	require.Error(t, insertLB(db, "lb-regional-zone", "INTERNAL", "REGIONAL", "ru-central1-a"),
		"у anycast зональной координаты нет — записать её значит выдумать")

	// Положительный: EXTERNAL без зоны — подлежащую зону выбирает платформа.
	require.NoError(t, insertLB(db, "lb-external-ok", "EXTERNAL", "", ""),
		"внешний балансировщик зоны не несёт")

	// Отрицание: EXTERNAL с зоной — placement-leak, ровно тот, из-за которого
	// сняты прежние per-zone-поля контракта.
	require.Error(t, insertLB(db, "lb-external-zone", "EXTERNAL", "", "ru-central1-a"),
		"зона внешнего VIP деривится платформой и наружу не выходит")
}
