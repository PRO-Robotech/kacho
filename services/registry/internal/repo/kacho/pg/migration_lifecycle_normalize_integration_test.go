// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migration_lifecycle_normalize_integration_test.go — upgrade-регрессия миграции
// 0012 на НЕПУСТОМ каталоге overlay-строк.
//
// 0007 добавила колонку lifecycle и CreateRepository принимала EPHEMERAL на вход.
// Значение никто не читал: видимость overlay решает НАЛИЧИЕ строки, а не колонка,
// поэтому строка с EPHEMERAL переживала пустой репозиторий, попадала в List и
// отдавалась Get ровно как DURABLE — расходилось только эхо в ответе. Вход теперь
// отвергается; 0012 снимает уже сохранённое утверждение, которое сервис никогда
// не исполнял. Чтение таких строк ломаться не должно ни до, ни после.
package pg_test

import (
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

// preLifecycleNormalizeVersion — последняя миграция ДО нормализации (0011).
const preLifecycleNormalizeVersion int64 = 11

// Апгрейд на стенде, где overlay-строка уже несёт EPHEMERAL: после Up значение
// приведено к тому, чем строка является на деле — DURABLE. Строка обязана уцелеть
// (нормализация — не удаление) со всеми своими полями.
func TestMigration0012_LifecycleNormalize_AppliesOnStoredEphemeral(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	dsn := startPG(t)
	db := openGoose(t, dsn)

	// Стенд «до апгрейда»: схема на 0011, overlay-строка заявляет EPHEMERAL.
	require.NoError(t, goose.UpTo(db, ".", preLifecycleNormalizeVersion))
	_, err := db.Exec(`INSERT INTO kacho_registry.registries (id, project_id, name, region_id)
	                   VALUES ('regLIFECYCLE00000001', 'prj-legacy', 'legacy-reg', 'ru-central1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO kacho_registry.repository_configs
	                     (registry_id, name, description, visibility, lifecycle)
	                   VALUES ('regLIFECYCLE00000001', 'legacy/ephemeral', 'stored claim', 'PRIVATE', 'EPHEMERAL')`)
	require.NoError(t, err, "схема 0007..0011 принимает EPHEMERAL — это и есть строка под нормализацию")
	_, err = db.Exec(`INSERT INTO kacho_registry.repository_configs
	                     (registry_id, name, visibility, lifecycle)
	                   VALUES ('regLIFECYCLE00000001', 'legacy/durable', 'PUBLIC', 'DURABLE')`)
	require.NoError(t, err)

	require.NoError(t, goose.Up(db, "."), "апгрейд обязан пройти на непустом overlay-каталоге")

	var lifecycle, description, visibility string
	require.NoError(t, db.QueryRow(
		`SELECT lifecycle, description, visibility FROM kacho_registry.repository_configs
		  WHERE registry_id = 'regLIFECYCLE00000001' AND name = 'legacy/ephemeral'`,
	).Scan(&lifecycle, &description, &visibility))
	require.Equal(t, "DURABLE", lifecycle, "сохранённое EPHEMERAL приведено к реальному поведению строки")
	require.Equal(t, "stored claim", description, "нормализация не трогает остальные поля")
	require.Equal(t, "PRIVATE", visibility)

	// Соседняя durable-строка не задета.
	require.NoError(t, db.QueryRow(
		`SELECT lifecycle, visibility FROM kacho_registry.repository_configs
		  WHERE registry_id = 'regLIFECYCLE00000001' AND name = 'legacy/durable'`,
	).Scan(&lifecycle, &visibility))
	require.Equal(t, "DURABLE", lifecycle)
	require.Equal(t, "PUBLIC", visibility)

	var total int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM kacho_registry.repository_configs WHERE registry_id = 'regLIFECYCLE00000001'`,
	).Scan(&total))
	require.Equal(t, 2, total, "нормализация не удаляет строки")
}

// Пустой каталог (текущее состояние стенда) — миграция обязана быть no-op, а не
// падать: гейт должен пройти там, где нормализовывать нечего. Её Down тоже обязан
// исполняться: нормализация ничего не создаёт, но неисполнимый Down заклинил бы
// откат всей цепочки.
func TestMigration0012_LifecycleNormalize_NoopOnEmptyCatalogue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	db := openGoose(t, startPG(t))
	require.NoError(t, goose.Up(db, "."))

	var rows int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM kacho_registry.repository_configs`).Scan(&rows))
	require.Equal(t, 0, rows)

	require.NoError(t, goose.Down(db, "."), "Down миграции нормализации обязан исполняться")
	version, err := goose.GetDBVersion(db)
	require.NoError(t, err)
	require.Equal(t, preLifecycleNormalizeVersion, version, "откат снимает ровно 0012")
}
