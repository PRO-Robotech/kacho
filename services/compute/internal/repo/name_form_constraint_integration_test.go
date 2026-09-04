// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/nameformdb"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// TestIntegration_Compute_NameFormConstraintIsEnforced — действие ограничения
// формы имени на живой базе.
//
// Ссылки на задачу здесь нет намеренно: гейт `internal/commentlint` этого сервиса
// запрещает её в комментариях, и предмет называется словами. Предупреждение об
// этом уже стоило одного круга проб — образец у vpc переносится в compute не
// дословно.
//
// Миграция 715001 ставит форму имени четырём таблицам compute; доказательство
// того, что форма ДЕЙСТВУЕТ, было только у vpc. Разбор класса, перечень
// утверждений и почему положительный контроль обязателен — `pkg/nameformdb`.
//
// Строки вставляются НАПРЯМУЮ в таблицу, минуя домен и use-case: предмет — то,
// что отвергнет сервер, когда слоя над ним не окажется.
func TestIntegration_Compute_NameFormConstraintIsEnforced(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (testcontainers Postgres) — skipped with -short")
	}
	ctx := context.Background()

	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	// `project` — идентичность из перечня фикстуры учёта (quota_fixture_test.go).
	// Без строки учёта КАЖДАЯ вставка ресурса отвергалась бы «потолок не назван»,
	// и положительный контроль перестал бы быть контролем.
	const project = "project"

	nameformdb.Probe{
		Schema: "public",
		Tables: []nameformdb.Table{
			{
				Name: "instances",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO public.instances (id, project_id, zone_id, name)
					        VALUES ($1, $2, 'zone-nameform', $3)`,
						[]any{fmt.Sprintf("ins-%017d", seq), project, name}
				},
			},
			{
				Name: "machine_types",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO public.machine_types (id, name, family, v_cpu, memory_mib, status)
					        VALUES ($1, $2, 0, 1, 1024, 1)`,
						[]any{fmt.Sprintf("mt-%017d", seq), name}
				},
			},
			{
				Name: "guest_access_keys",
				Row: func(name string, seq int) (string, []any) {
					// id обязан отвечать своей форме (`gak-` + 17 знаков
					// крокфорда) — иначе строку отвергнет ограничение ключа, а не
					// формы имени, и отрицание докажет не то.
					return `INSERT INTO public.guest_access_keys (id, project_id, name, public_key, fingerprint)
					        VALUES ($1, $2, $3, 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5', $4)`,
						[]any{fmt.Sprintf("gak-%017d", seq), project, name, fmt.Sprintf("fp-%d", seq)}
				},
			},
			{
				Name: "placement_groups",
				Row: func(name string, seq int) (string, []any) {
					// Якорь размещения взаимоисключающий: ZONAL требует непустой
					// зоны и пустого региона (см. placement_groups_anchor_check).
					return `INSERT INTO public.placement_groups
					            (id, project_id, name, strategy, placement_type, zone_id, region_id)
					        VALUES ($1, $2, $3, 'SPREAD', 'ZONAL', 'zone-nameform', '')`,
						[]any{fmt.Sprintf("plg-%017d", seq), project, name}
				},
			},
		},
	}.Run(ctx, t, pool)
}
