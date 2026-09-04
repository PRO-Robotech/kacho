// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migration_placement_integration_test.go — upgrade-регрессия миграции 0006
// (REG-1 F4 regional placement) на НЕПУСТОМ каталоге. Миграция вводит
// registries.region_id NOT NULL + placement-anchor CHECK «region_id непуст», поэтому
// обязана НЕСТИ BACKFILL: ADD COLUMN nullable → backfill → SET NOT NULL. Без backfill
// `ADD COLUMN region_id TEXT NOT NULL` падает (SQLSTATE 23502, «contains null values»)
// на любом стенде, где реестры уже созданы — апгрейд невозможен, а починить это
// последующей миграцией НЕЛЬЗЯ (goose обрывается ровно на 0006, 0007+ не выполняются).
//
// Тест воспроизводит апгрейд: миграции до 0005 → INSERT легаси-реестра → остаток Up.
package pg_test

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/registry/internal/migrations"
)

// prePlacementVersion — последняя миграция ДО ввода regional placement (0005).
const prePlacementVersion int64 = 5

// startPG выдаёт тесту СОБСТВЕННУЮ пустую базу на одном Postgres пакета (см.
// testmain_pgtest_test.go) — вместо контейнера на каждый вызов.
//
// Пустую — это предпосылка, а не деталь: тесты этого файла и соседнего
// migration_lifecycle_normalize идут по цепочке САМИ, останавливаясь ниже её вершины,
// чтобы посеять легаси-строку и только потом доиграть апгрейд. База, уже
// мигрированная до head, не оставила бы им того, что они проверяют. Поэтому здесь
// NewEmptyDB, а не NewDB (шаблон пакета мигрирован — им он не годится).
//
// Отсутствие базы — ОТКАЗ, никогда не пропуск: pgtest.NewEmptyDB роняет t сам.
func startPG(t *testing.T) string {
	t.Helper()
	return pgtest.NewEmptyDB(t)
}

// openGoose открывает соединение с накатчиком goose на встроенных миграциях registry.
func openGoose(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	return db
}

// REG-1-F4-upgrade — миграция 0006 применяется на НЕПУСТОМ каталоге реестров:
// существующая строка получает непустой region_id (backfill) и placement_type
// REGIONAL, placement-anchor CHECK выполняется. RED до фикса: goose.Up падает
// «column "region_id" of relation "registries" contains null values».
func TestMigration0006_RegionPlacement_AppliesOnNonEmptyRegistries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	dsn := startPG(t)
	db := openGoose(t, dsn)

	// Стенд «до апгрейда»: схема на 0005, каталог НЕ пуст.
	require.NoError(t, goose.UpTo(db, ".", prePlacementVersion))
	_, err := db.Exec(`INSERT INTO kacho_registry.registries (id, project_id, name)
	                   VALUES ('regLEGACY0000000000A', 'prj-legacy', 'legacy-reg')`)
	require.NoError(t, err, "легаси-реестр создаётся на схеме 0005 (region_id ещё нет)")

	// Апгрейд: 0006 обязан пройти, а не упасть на NOT NULL без backfill.
	require.NoError(t, goose.Up(db, "."), "миграция обязана апгрейдить непустой каталог")

	var regionID, placementType string
	require.NoError(t, db.QueryRow(
		`SELECT region_id, placement_type FROM kacho_registry.registries WHERE id = 'regLEGACY0000000000A'`,
	).Scan(&regionID, &placementType))
	require.NotEmpty(t, regionID, "backfill обязан дать непустой region_id (placement-anchor CHECK)")
	require.Equal(t, "ru-central1", regionID, "дефолт backfill — baseline-регион платформы")
	require.Equal(t, "REGIONAL", placementType)

	// Колонка обязана остаться NOT NULL (backfill не ослабляет инвариант).
	_, err = db.Exec(`INSERT INTO kacho_registry.registries (id, project_id, name)
	                  VALUES ('regNULLREGION000000A', 'prj-x', 'no-region')`)
	require.Error(t, err, "region_id обязан остаться NOT NULL после backfill")
	require.Contains(t, err.Error(), "region_id")
}

// REG-1-F4-upgrade-override — backfill параметризуем: оператор мультирегионального
// стенда задаёт GUC kacho.registry_backfill_region (через DSN options / ALTER DATABASE),
// и легаси-строки получают ЕГО регион вместо baseline-дефолта.
func TestMigration0006_RegionPlacement_BackfillRegionOverridable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	dsn := startPG(t)
	db := openGoose(t, dsn)

	require.NoError(t, goose.UpTo(db, ".", prePlacementVersion))
	_, err := db.Exec(`INSERT INTO kacho_registry.registries (id, project_id, name)
	                   VALUES ('regLEGACY0000000000B', 'prj-legacy', 'legacy-reg')`)
	require.NoError(t, err)
	// Имя базы больше не константа — её выдаёт пакету pgtest, по одной на тест, — а
	// ALTER DATABASE принимает ИДЕНТИФИКАТОР, не выражение: current_database() на его
	// место не подставить. Отсюда динамический SQL. Предмет утверждения не меняется:
	// GUC уровня БД ставится ровно на ту базу, где идёт апгрейд, как и раньше.
	_, err = db.Exec(`DO $$ BEGIN
		EXECUTE format('ALTER DATABASE %I SET kacho.registry_backfill_region = %L',
		               current_database(), 'eu-north-1');
	END $$`)
	require.NoError(t, err)
	_ = db.Close() // GUC уровня БД подхватывают НОВЫЕ сессии

	db2 := openGoose(t, dsn)
	require.NoError(t, goose.Up(db2, "."))

	var regionID string
	require.NoError(t, db2.QueryRow(
		`SELECT region_id FROM kacho_registry.registries WHERE id = 'regLEGACY0000000000B'`,
	).Scan(&regionID))
	require.Equal(t, "eu-north-1", regionID, "backfill обязан уважать kacho.registry_backfill_region")
}
