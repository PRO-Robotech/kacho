// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/nameformdb"
)

// TestIntegration_Geo_NameFormConstraintIsEnforced — задача #721.
//
// Миграция 715001 ставит форму имени в пяти схемах; доказательство того, что
// форма ДЕЙСТВУЕТ, было у одной. Здесь — доказательство для geo. Разбор класса,
// перечень утверждений и почему положительный контроль обязателен —
// `internal/nameform`.
func TestIntegration_Geo_NameFormConstraintIsEnforced(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t) // сам пропускается под -short

	// Родитель для зоны. Зона ссылается на регион внешним ключом, поэтому без
	// него КАЖДАЯ вставка зоны отвергалась бы 23503 — и положительный контроль
	// перестал бы быть контролем, а отрицание зеленело бы на чужом отказе.
	const parentRegion = "reg-nameform-parent"
	_, err := pool.Exec(ctx,
		`INSERT INTO kacho_geo.regions (id, name) VALUES ($1, $2)`,
		parentRegion, "nameform-parent-region")
	require.NoError(t, err, "фикстура: регион-родитель для зоны")

	nameformdb.Probe{
		Schema: "kacho_geo",
		Tables: []nameformdb.Table{
			{
				Name: "regions",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_geo.regions (id, name) VALUES ($1, $2)`,
						[]any{fmt.Sprintf("reg-nf-%017d", seq), name}
				},
			},
			{
				Name: "zones",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_geo.zones (id, region_id, name) VALUES ($1, $2, $3)`,
						[]any{fmt.Sprintf("zone-nf-%017d", seq), parentRegion, name}
				},
			},
		},
	}.Run(ctx, t, pool)
}
