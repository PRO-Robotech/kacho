// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

// system_viewer_role_id_integration_test.go — идентификаторы СИСТЕМНЫХ ролей,
// посеянных цепью миграций, проходят собственную строгую проверку формы
// (задача продукта #1808).
//
// # Почему проба по ЖИВОЙ схеме, а не по тексту миграции
//
// Утверждается ИСХОД накатанной цепи: посев переопределяет любая поздняя
// миграция, и текст одного файла об этом не знает. Здесь это не отвлечённый
// довод — предмет задачи ровно такой: строка `0001_initial.sql` править нельзя,
// исход даёт поздняя миграция, и верен только результат их сложения.
//
// # Почему обе полосы, а не одна
//
// Различие между admin и viewer НИКЕМ НЕ РЕШАЛОСЬ: у admin длина 20, у viewer
// была 21. Утверждение об одной полосе зеленело бы на проверке, не отвергающей
// ничего; здесь спрашиваются ОБЕ, и перепись печатает, сколько системных ролей
// осмотрено.
package migrations_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/iam/internal/apps/kacho/shared"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
)

func TestIntegration_SeededSystemRoleIDsPassTheServiceOwnFormCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("пропуск интеграционной пробы (нужен Docker)")
	}
	db := freshIamSchema(t)

	rows, err := db.Query(`SELECT id, name FROM kacho_iam.roles WHERE is_system ORDER BY id`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	seen, bad := 0, []string{}
	for rows.Next() {
		var id, name string
		require.NoError(t, rows.Scan(&id, &name))
		seen++
		if err := shared.ValidateResourceID(id, domain.PrefixRole, "role"); err != nil {
			bad = append(bad, id+" ("+name+"): "+err.Error())
		}
	}
	require.NoError(t, rows.Err())

	t.Logf("перепись: системных ролей осмотрено %d · негодных по форме %d", seen, len(bad))
	require.NotZero(t, seen, "системных ролей не найдено ни одной — обход беспредметен")
	require.Emptyf(t, bad,
		"посеянный идентификатор не проходит собственную проверку формы сервиса, "+
			"а она стоит первым стейтментом каждого глагола роли — значит роль "+
			"недостижима по id ни одним путём:\n%v", bad)
}

// TestIntegration_SystemViewerRoleAnswersToItsPinnedID — вторая половина: роль
// не просто «годна по форме», а лежит ПОД ТЕМ ИДЕНТИФИКАТОРОМ, который назвала
// константа домена. Без неё расхождение посева с константой прошло бы молча.
func TestIntegration_SystemViewerRoleAnswersToItsPinnedID(t *testing.T) {
	if testing.Short() {
		t.Skip("пропуск интеграционной пробы (нужен Docker)")
	}
	db := freshIamSchema(t)

	for _, lane := range []struct{ name, pinned string }{
		{"kacho-system.admin", domain.SystemAdminRoleID},
		{"kacho-system.viewer", domain.SystemViewerRoleID},
	} {
		var id string
		err := db.QueryRow(
			`SELECT id FROM kacho_iam.roles WHERE is_system AND name = $1`, lane.name).Scan(&id)
		require.NoErrorf(t, err, "системная роль %q не посеяна", lane.name)
		require.Equalf(t, lane.pinned, id,
			"посев роли %q разошёлся с константой домена: в схеме %q, в коде %q",
			lane.name, id, lane.pinned)
	}

	// Отрицательная половина: строки со СТАРЫМ негодным идентификатором в схеме
	// не осталось — иначе перецеливание детей было бы половинчатым.
	var leftovers int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM kacho_iam.roles WHERE id = 'rol000000000sysviewer'`).Scan(&leftovers))
	require.Zerof(t, leftovers, "строка со старым идентификатором пережила миграцию (%d)", leftovers)
}
