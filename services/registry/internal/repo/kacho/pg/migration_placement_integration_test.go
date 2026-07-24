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
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/PRO-Robotech/kacho/services/registry/internal/migrations"
)

// prePlacementVersion — последняя миграция ДО ввода regional placement (0005).
const prePlacementVersion int64 = 5

// startPG поднимает изолированный Postgres 16 и возвращает DSN (без применённых
// миграций — их накатывает сам тест по шагам).
func startPG(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pgc, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("kacho_registry_upgrade"),
		postgres.WithUsername("registry"),
		postgres.WithPassword("registry"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgc.Terminate(context.Background()) })

	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	return dsn
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
	_, err = db.Exec(`ALTER DATABASE kacho_registry_upgrade SET kacho.registry_backfill_region = 'eu-north-1'`)
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
