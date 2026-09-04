// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/nameformdb"
)

// TestIntegration_Storage_NameFormConstraintIsEnforced — задача #721.
//
// Миграция 715001 ставит форму имени трём тенантным таблицам storage;
// доказательство того, что форма ДЕЙСТВУЕТ, было только у vpc. Разбор класса,
// перечень утверждений и почему положительный контроль обязателен —
// `pkg/nameformdb`.
func TestIntegration_Storage_NameFormConstraintIsEnforced(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t) // сам пропускается под -short; сеет каталог классов и учёт

	// Класс диска берётся из каталога, а не выписывается литералом: у тома это
	// внешний ключ, и выдуманное значение отвергалось бы 23503 — то есть
	// положительный контроль падал бы по чужой причине.
	var diskType string
	require.NoError(t,
		pool.QueryRow(ctx, `SELECT id FROM kacho_storage.disk_types ORDER BY id LIMIT 1`).Scan(&diskType),
		"фикстура: каталог классов диска пуст — вставка тома недостижима")

	// `project` — идентичность из перечня фикстуры учёта (quota_fixture_test.go).
	const project = "project"

	nameformdb.Probe{
		Schema: "kacho_storage",
		Tables: []nameformdb.Table{
			{
				Name: "volumes",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_storage.volumes
					            (id, project_id, zone_id, disk_type_id, size_bytes, name)
					        VALUES ($1, $2, 'zone-nameform', $3, 1073741824, $4)`,
						[]any{fmt.Sprintf("vol-%017d", seq), project, diskType, name}
				},
			},
			{
				Name: "snapshots",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_storage.snapshots (id, project_id, name)
					        VALUES ($1, $2, $3)`,
						[]any{fmt.Sprintf("snp-%017d", seq), project, name}
				},
			},
			{
				Name: "images",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_storage.images (id, project_id, region_id, name)
					        VALUES ($1, $2, 'region-nameform', $3)`,
						[]any{fmt.Sprintf("img-%017d", seq), project, name}
				},
			},
		},
		// Исключения объявлены самой миграцией 715001 и повторены здесь не как
		// пожелание, а как утверждение: перечень сверяется с базой в обе
		// стороны, поэтому запись, которой больше нечего исключать (форму
		// поставили), станет находкой и не переживёт свой предмет.
		Excluded: map[string]string{
			"disk_types":       "административный ресурс внутреннего листенера: имя — установочная величина, уникальность устроена глобально по установке",
			"storage_backends": "то же основание, что у класса диска: имя не косметическая метка арендатора",
		},
	}.Run(ctx, t, pool)
}
