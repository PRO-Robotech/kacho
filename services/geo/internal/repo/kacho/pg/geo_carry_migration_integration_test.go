// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// Пробы посева каталога размещения geo — базового (подъём стенда) и переносного
// (одноразовый Job из legacy-compute). Обе читают ПОСТАВЛЯЕМЫЙ артефакт и
// исполняют его SQL против РЕАЛЬНОЙ схемы kacho_geo, а не против его копии в
// тесте: копия SQL в пробе разъехалась бы с чартом молча и зеленела бы на
// сломанном.
//
// ЧТО ЗДЕСЬ УТВЕРЖДАЕТСЯ ПРО ПРОДУКТ. Зона пригодна для размещения ⟺
// zone.status='UP' И region.status='UP' (см. openZoneCountExpr в region.go и
// фильтр openForPlacement в zone.go). Умолчание обеих колонок — 'DOWN'
// (миграция 0004, fail-safe). Значит вставка, не назвавшая статус региона,
// закрывает ВСЕ его зоны — при том что существование региона и зоны
// потребители по-прежнему подтверждают, то есть отказ приходит не оттуда, где
// причина.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// repoRoot поднимается от каталога теста до go.mod. Артефакты посева живут в
// deploy/ и tests/, то есть вне пакета; путь ищется, а не выписывается
// относительными «..» — их количество ломается при любом переносе пакета.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "не нашёл go.mod, поднимаясь от %s", dir)
		dir = parent
	}
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), rel)
	b, err := os.ReadFile(p) // #nosec G304 -- путь собран из константы пробы и корня репозитория, извне не приходит
	require.NoError(t, err, "нет артефакта %s — проба читает поставляемый файл, а не свою копию", rel)
	require.NotEmpty(t, strings.TrimSpace(string(b)), "%s пуст", rel)
	return string(b)
}

// ─── перенос из legacy-compute: geo-data-migration-job.yaml ─────────────────

// jobUpsertSQL достаёт из шаблона Job'а те операторы, которые он реально
// исполняет: создание staging-таблиц и два upsert'а в regions/zones. Внутри
// этих операторов шаблонизации нет, поэтому helm не нужен — но отсутствие
// `{{` проверяется, иначе проба однажды исполнила бы не то, что поедет.
//
// Печатает объём осмотренного: «ноль находок» обязано отличаться от «ноль
// прочитанного».
func jobUpsertSQL(t *testing.T) (create []string, regionUpsert, zoneUpsert string) {
	t.Helper()
	const rel = "deploy/helm/umbrella/charts/kacho-geo/templates/geo-data-migration-job.yaml"
	body := readRepoFile(t, rel)

	starts := []string{"CREATE UNLOGGED TABLE", "INSERT INTO regions", "INSERT INTO zones"}
	var stmts []string
	lines := strings.Split(body, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		matched := false
		for _, s := range starts {
			if strings.HasPrefix(trimmed, s) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		var buf []string
		for ; i < len(lines); i++ {
			cur := strings.TrimSpace(lines[i])
			buf = append(buf, cur)
			if strings.HasSuffix(cur, ";") {
				break
			}
		}
		stmts = append(stmts, strings.Join(buf, "\n"))
	}

	t.Logf("осмотрено: %s — %d строк, извлечено операторов: %d", rel, len(lines), len(stmts))

	for _, s := range stmts {
		require.NotContains(t, s, "{{",
			"оператор Job'а стал шаблонизированным — проба обязана рендерить чарт, а не читать файл:\n%s", s)
		switch {
		case strings.HasPrefix(s, "CREATE UNLOGGED TABLE"):
			create = append(create, s)
		case strings.HasPrefix(s, "INSERT INTO regions"):
			require.Empty(t, regionUpsert, "в шаблоне больше одного upsert'а в regions")
			regionUpsert = s
		case strings.HasPrefix(s, "INSERT INTO zones"):
			require.Empty(t, zoneUpsert, "в шаблоне больше одного upsert'а в zones")
			zoneUpsert = s
		}
	}

	require.Len(t, create, 2, "ожидались две staging-таблицы (_geo_stg_regions, _geo_stg_zones) в %s", rel)
	require.NotEmpty(t, regionUpsert, "в %s не найден `INSERT INTO regions …`", rel)
	require.NotEmpty(t, zoneUpsert, "в %s не найден `INSERT INTO zones …`", rel)
	return create, regionUpsert, zoneUpsert
}

// carryFixture — перенос одной географии: регион с открытой зоной, регион без
// открытых зон, и зона в третьем состоянии, которого у kacho_geo нет.
type carriedZone struct{ id, regionID, status, name string }

func runCarry(t *testing.T, pool *pgxpool.Pool, regions [][2]string, zones []carriedZone) {
	t.Helper()
	ctx := context.Background()
	create, regionUpsert, zoneUpsert := jobUpsertSQL(t)

	for _, c := range create {
		_, err := pool.Exec(ctx, c)
		require.NoError(t, err, "staging-таблица не создалась:\n%s", c)
	}
	for _, r := range regions {
		_, err := pool.Exec(ctx,
			`INSERT INTO _geo_stg_regions (id, name, created_at) VALUES ($1,$2,now())`, r[0], r[1])
		require.NoError(t, err)
	}
	for _, z := range zones {
		_, err := pool.Exec(ctx,
			`INSERT INTO _geo_stg_zones (id, region_id, status, name, created_at) VALUES ($1,$2,$3,$4,now())`,
			z.id, z.regionID, z.status, z.name)
		require.NoError(t, err)
	}

	// Порядок обязателен: FK RESTRICT zones.region_id → regions(id).
	_, err := pool.Exec(ctx, regionUpsert)
	require.NoError(t, err, "upsert регионов из шаблона Job'а не исполнился:\n%s", regionUpsert)
	_, err = pool.Exec(ctx, zoneUpsert)
	require.NoError(t, err, "upsert зон из шаблона Job'а не исполнился (CHECK status IN ('UP','DOWN')):\n%s", zoneUpsert)
}

func statusOf(t *testing.T, pool *pgxpool.Pool, table, id string) string {
	t.Helper()
	var s string
	q := "SELECT status FROM " + table + " WHERE id = $1"
	require.NoError(t, pool.QueryRow(context.Background(), q, id).Scan(&s), "нет строки %s.%s", table, id)
	return s
}

func openZoneIDs(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT z.id FROM zones z JOIN regions r ON r.id = z.region_id
		  WHERE z.status = 'UP' AND r.status = 'UP' ORDER BY z.id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		out = append(out, id)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestGeoCarryPreservesThePlaceableZoneSet — перенос обязан сохранить НАБОР зон,
// пригодных для размещения. У источника колонки статуса региона не было вовсе,
// поэтому до переноса зона была пригодна ⟺ zone.status='UP'; после переноса —
// ⟺ zone.status='UP' И region.status='UP'. Значит статус региона обязан быть
// выведен из перенесённых зон, а не взят из fail-safe умолчания.
//
// Красная форма (вернуть в шаблон `INSERT INTO regions (id, name, created_at)`):
// регион садится 'DOWN' → набор открытых зон ПУСТ вместо {r-open-a}.
func TestGeoCarryPreservesThePlaceableZoneSet(t *testing.T) {
	pool := newTestPool(t)

	runCarry(t, pool,
		[][2]string{{"r-open", "region-open"}, {"r-closed", "region-closed"}},
		[]carriedZone{
			{"r-open-a", "r-open", "UP", "zone-open-a"},
			{"r-open-b", "r-open", "STATUS_UNSPECIFIED", "zone-unspecified-b"},
			{"r-closed-a", "r-closed", "DOWN", "zone-closed-a"},
		})

	require.Equal(t, "UP", statusOf(t, pool, "regions", "r-open"),
		"регион с перенесённой 'UP'-зоной обязан открыться: иначе перенос закрывает целую географию")
	require.Equal(t, "UP", statusOf(t, pool, "zones", "r-open-a"))

	// Третье состояние источника ('STATUS_UNSPECIFIED') пригодным никогда не было
	// и CHECK'ом 0004 запрещено → приводится к 'DOWN', а не роняет Job на 23514.
	require.Equal(t, "DOWN", statusOf(t, pool, "zones", "r-open-b"))

	require.Equal(t, []string{"r-open-a"}, openZoneIDs(t, pool),
		"набор пригодных зон после переноса обязан совпасть с набором до переноса (zone.status='UP')")
}

// TestGeoCarryLeavesARegionWithNothingOpenClosed — законный близнец той же
// формы: гейт обязан МОЛЧАТЬ, то есть не открывать регион, у которого не было
// ни одной пригодной зоны. Ловит «починку» вида «просто впишем 'UP'»: она
// прошла бы предыдущую пробу и открыла бы закрытую географию.
func TestGeoCarryLeavesARegionWithNothingOpenClosed(t *testing.T) {
	pool := newTestPool(t)

	runCarry(t, pool,
		[][2]string{{"r-dark", "region-dark"}},
		[]carriedZone{
			{"r-dark-a", "r-dark", "DOWN", "zone-dark-a"},
			{"r-dark-b", "r-dark", "STATUS_UNSPECIFIED", "zone-dark-b"},
		})

	require.Equal(t, "DOWN", statusOf(t, pool, "regions", "r-dark"),
		"регион без ни одной 'UP'-зоны открывать нечем — fail-safe 'DOWN'")
	require.Empty(t, openZoneIDs(t, pool))
}

// TestGeoCarryIsIdempotent — повторный прогон переноса не дублирует и не падает
// (заявление шапки шаблона: «Idempotent: re-runs never duplicate / never fail»).
func TestGeoCarryIsIdempotent(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	regions := [][2]string{{"r-open", "region-open"}}
	zones := []carriedZone{{"r-open-a", "r-open", "UP", "zone-open-a"}}

	runCarry(t, pool, regions, zones)
	first := openZoneIDs(t, pool)

	// Второй прогон: staging пересоздаётся тем же оператором (IF NOT EXISTS) и
	// наполняется тем же содержимым — как при повторе Job'а.
	_, err := pool.Exec(ctx, `TRUNCATE _geo_stg_regions; TRUNCATE _geo_stg_zones;`)
	require.NoError(t, err)
	runCarry(t, pool, regions, zones)

	require.Equal(t, first, openZoneIDs(t, pool))
	var nr, nz int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM regions`).Scan(&nr))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM zones`).Scan(&nz))
	require.Equal(t, 1, nr)
	require.Equal(t, 1, nz)
}
