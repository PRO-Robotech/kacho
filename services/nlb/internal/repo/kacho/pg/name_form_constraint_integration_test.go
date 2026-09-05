// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/nameformdb"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// TestIntegration_NLB_NameFormConstraintIsEnforced — задача #721.
//
// Миграция 715001 ставит форму имени трём таблицам nlb; доказательство того, что
// форма ДЕЙСТВУЕТ, было только у vpc. Разбор класса, перечень утверждений и
// почему положительный контроль обязателен — `pkg/nameformdb`.
func TestIntegration_NLB_NameFormConstraintIsEnforced(t *testing.T) {
	ctx := context.Background()
	dsn := setupTestDB(t) // сам пропускается под -short

	// Своя идентичность проекта, а не одна из общих: перечень общих снят с
	// дерева литералами и разошёлся бы с ним молча, стоит пробе переехать.
	const (
		project = "prj0NAMEFORM000000001"
		region  = "region-nameform"
	)
	seedQuotasForProjects(t, dsn, []string{project})

	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	// Родитель для слушателя: слушатель ссылается на балансировщик внешним
	// ключом, и без него КАЖДАЯ его вставка отвергалась бы 23503 — то есть
	// положительный контроль падал бы по чужой причине, а отрицание зеленело бы
	// на чужом отказе.
	const parentLB = "lb-nameform-parent"
	_, err = pool.Exec(ctx, `INSERT INTO kacho_nlb.load_balancers
	        (id, project_id, region_id, type, placement_type, name)
	    VALUES ($1, $2, $3, 'EXTERNAL', '', 'nameform-parent-lb')`, parentLB, project, region)
	require.NoError(t, err, "фикстура: балансировщик-родитель для слушателя")

	nameformdb.Probe{
		Schema: "kacho_nlb",
		Tables: []nameformdb.Table{
			{
				Name: "load_balancers",
				Row: func(name string, seq int) (string, []any) {
					// Тип и вид размещения связаны ограничением: у внешнего
					// балансировщика вид размещения обязан быть пустым.
					return `INSERT INTO kacho_nlb.load_balancers
					            (id, project_id, region_id, type, placement_type, name)
					        VALUES ($1, $2, $3, 'EXTERNAL', '', $4)`,
						[]any{fmt.Sprintf("lb-%017d", seq), project, region, name}
				},
			},
			{
				Name: "listeners",
				Row: func(name string, seq int) (string, []any) {
					// Порт разный на каждую строку: тройка «балансировщик, порт,
					// протокол» уникальна, и повтор порта отвергся бы
					// уникальностью раньше, чем формой имени.
					return `INSERT INTO kacho_nlb.listeners
					            (id, load_balancer_id, project_id, region_id, protocol, port, name)
					        VALUES ($1, $2, $3, $4, 'TCP', $5, $6)`,
						[]any{fmt.Sprintf("lsn-%017d", seq), parentLB, project, region, 10000 + seq, name}
				},
			},
			{
				Name: "target_groups",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_nlb.target_groups
					            (id, project_id, region_id, port, name)
					        VALUES ($1, $2, $3, $4, $5)`,
						[]any{fmt.Sprintf("tg-%017d", seq), project, region, 20000 + seq, name}
				},
			},
		},
	}.Run(ctx, t, pool)
}
