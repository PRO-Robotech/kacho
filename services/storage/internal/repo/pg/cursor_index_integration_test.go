// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// Курсорная пагинация продаётся клиенту как ПОСТОЯННАЯ стоимость страницы: запрос
// везде одной формы — сузить по проекту, взять строки строго после курсора,
// упорядочить по (создано, id), взять размер страницы.
//
// Индексов под эту пару не было ни у одной тенантной таблицы — только раздельные
// однополевые (по проекту и по времени создания). Планировщику оставались два
// плана, и ни один не по странице: либо скан по проекту с сортировкой ВСЕХ строк
// проекта на КАЖДУЮ страницу, либо скан по времени создания с отбрасыванием чужих
// проектов — то есть тем дороже, чем больше проект и чем меньше его доля в
// таблице. Полный обход проекта постранично становится квадратичным.
//
// Что это упущение, а не решение: в тех же деревьях композит под курсор выставлен
// везде, где о нём подумали (таблица операций, каталог размеров, все тенантные
// таблицы балансировщика и реестров).
//
// Проверяется НАБЛЮДАЕМОЕ — план и число прочитанных строк, а не наличие строки в
// каталоге индексов: индекс, который планировщик не выбирает, не стоит ничего.

const (
	// cursorProbeRows — строк на проект в пробе.
	//
	// Было 200 при пустых именах. Имя перестало быть пустым (#715), строка
	// стала шире, и на этом объёме планировщик перестал выбирать курсорный
	// индекс для volumes — при том что для images и snapshots продолжал.
	// Такая разница между тремя одинаковыми по смыслу пробами и есть признак
	// того, что решение принималось на грани, а не по существу.
	//
	// Замерено: 2000 строк на проект возвращают выбор курсорного индекса.
	cursorProbeRows = 2000
	// cursorProbeProjects — сколько проектов в таблице. Доля целевого проекта
	// обязана быть заметно меньше единицы: под старым планом (скан по времени
	// создания + отбрасывание чужих проектов) стоимость страницы растёт именно
	// обратно этой доле. При восьми проектах на страницу в 20 строк старый план
	// читает порядка 160 строк, новый — 21.
	cursorProbeProjects = 8
	// cursorPageSize — размер страницы в пробе.
	cursorPageSize = 20
	// cursorPlanSlack — допуск сверх страницы: планировщик вправе прочитать
	// несколько лишних строк (LIMIT+1, внутренние узлы). Предмет утверждения — что
	// число НЕ порядка размера проекта.
	cursorPlanSlack = 5
	// cursorStartOffset — с какой позиции проекта начинается страница. Курсор в
	// НАЧАЛЕ не различал бы планы: отбрасывать чужие строки приходится ровно
	// столько, сколько их лежит ДО курсора.
	cursorStartOffset = 120
)

// cursorStartAt — момент, с которого проба берёт страницу (курсор в середине
// проекта). Совпадает с посевом: created_at = базовая метка + i секунд.
const cursorStartAt = "2020-01-01T00:02:00Z" // база + 120 секунд

// planRowsRe — «rows=N» из строк плана EXPLAIN ANALYZE (actual rows).
var planRowsRe = regexp.MustCompile(`actual time=[0-9.]+\.\.[0-9.]+ rows=(\d+)`)

// explainPlan выполняет EXPLAIN (ANALYZE) и склеивает план в одну строку.
func explainPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	rows, err := pool.Query(ctx, sql, args...)
	require.NoError(t, err)
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		b.WriteString(line)
		b.WriteString("\n")
	}
	require.NoError(t, rows.Err())
	return b.String()
}

// maxActualRows — наибольшее число строк, фактически выданных каким-либо узлом
// плана. Это и есть «сколько прочитали, чтобы отдать страницу».
func maxActualRows(t *testing.T, plan string) int {
	t.Helper()
	max := 0
	for _, m := range planRowsRe.FindAllStringSubmatch(plan, -1) {
		n, err := strconv.Atoi(m[1])
		require.NoError(t, err)
		if n > max {
			max = n
		}
	}
	require.NotZero(t, max, "план не содержит ни одного узла с actual rows — проба ничего не измерила:\n%s", plan)
	return max
}

// requireRepoShape — предпосылка пробы: форма запроса в ней совпадает с формой,
// которую строит репозиторий. Разойдясь, проба стала бы утверждением о самой себе.
func requireRepoShape(t *testing.T, repoFile string, fragments ...string) {
	t.Helper()
	body, err := os.ReadFile(repoFile)
	require.NoError(t, err, "исходник репозитория не читается — предпосылка пробы не проверяема")
	src := string(body)
	for _, f := range fragments {
		require.Contains(t, src, f,
			"форма запроса в %s разошлась с пробой (%q): проба доказывала бы что-то о себе, а не о продукте",
			repoFile, f)
	}
}

// seedTenantRows засевает по cursorProbeRows строк в cursorProbeProjects
// проектов одним стейтментом на проект. Доля целевого проекта в таблице обязана
// быть заметно меньше единицы, иначе скан по времени создания выглядел бы дешёвым
// по построению.
//
// insert — шаблон с $1 (проект) и $2 (номер строки); строки одного индекса из
// разных проектов получают ОДИН И ТОТ ЖЕ момент создания, поэтому под старым планом
// (скан по времени) чужие строки лежат вперемешку с целевыми — как и в реальной
// таблице.
func seedTenantRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, insert string) []string {
	t.Helper()
	projects := make([]string, 0, cursorProbeProjects)
	for p := 0; p < cursorProbeProjects; p++ {
		prj := fmt.Sprintf("prj-cursor-%d", p)
		projects = append(projects, prj)
		_, err := pool.Exec(ctx, insert, prj, cursorProbeRows)
		require.NoError(t, err)
	}
	return projects
}

// TestIntegration_VolumesCursorIndex_PageDoesNotReadTheWholeProject — том.
func TestIntegration_VolumesCursorIndex_PageDoesNotReadTheWholeProject(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (testcontainers Postgres) — skipped with -short")
	}
	ctx := context.Background()
	pool := newTestPool(t)

	requireRepoShape(t, "volume_repo.go",
		"(v.created_at, v.id) > ($%d, $%d)", "ORDER BY v.created_at ASC, v.id ASC")

	projects := seedTenantRows(t, ctx, pool, `
		INSERT INTO volumes (id, project_id, zone_id, name, disk_type_id, size_bytes, state, created_at)
		SELECT 'vol' || $1 || lpad(g::text, 8, '0'), $1, 'ru-central1-a', 'vol' || $1 || lpad(g::text, 8, '0'), '`+seededDiskType+`',
		       1073741824, 'READY', TIMESTAMPTZ '2020-01-01 00:00:00Z' + (g || ' seconds')::interval
		  FROM generate_series(0, $2::int - 1) g`)
	_, err := pool.Exec(ctx, `ANALYZE volumes`)
	require.NoError(t, err)

	plan := explainPlan(t, ctx, pool, `
		EXPLAIN (ANALYZE, FORMAT TEXT)
		SELECT v.id
		  FROM volumes v LEFT JOIN volume_attachments va ON va.volume_id = v.id
		 WHERE v.project_id = $1 AND (v.created_at, v.id) > ($2, $3)
		 ORDER BY v.created_at ASC, v.id ASC
		 LIMIT $4`,
		projects[0], cursorStartAt, "", cursorPageSize+1)

	assertPageBoundPlan(t, plan, "volumes")
}

// TestIntegration_ImagesCursorIndex_PageDoesNotReadTheWholeProject — образ.
func TestIntegration_ImagesCursorIndex_PageDoesNotReadTheWholeProject(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (testcontainers Postgres) — skipped with -short")
	}
	ctx := context.Background()
	pool := newTestPool(t)

	requireRepoShape(t, "image_repo.go",
		"(i.created_at, i.id) > ($%d, $%d)", "ORDER BY i.created_at ASC, i.id ASC")

	projects := seedTenantRows(t, ctx, pool, `
		INSERT INTO images (id, project_id, name, region_id, size_bytes, state, created_at)
		SELECT 'img' || $1 || lpad(g::text, 8, '0'), $1, 'img' || $1 || lpad(g::text, 8, '0'), 'ru-central1', 1073741824, 'READY',
		       TIMESTAMPTZ '2020-01-01 00:00:00Z' + (g || ' seconds')::interval
		  FROM generate_series(0, $2::int - 1) g`)
	_, err := pool.Exec(ctx, `ANALYZE images`)
	require.NoError(t, err)

	plan := explainPlan(t, ctx, pool, `
		EXPLAIN (ANALYZE, FORMAT TEXT)
		SELECT i.id FROM images i
		 WHERE i.project_id = $1 AND (i.created_at, i.id) > ($2, $3)
		 ORDER BY i.created_at ASC, i.id ASC
		 LIMIT $4`,
		projects[0], cursorStartAt, "", cursorPageSize+1)

	assertPageBoundPlan(t, plan, "images")
}

// TestIntegration_SnapshotsCursorIndex_PageDoesNotReadTheWholeProject — снимок.
func TestIntegration_SnapshotsCursorIndex_PageDoesNotReadTheWholeProject(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (testcontainers Postgres) — skipped with -short")
	}
	ctx := context.Background()
	pool := newTestPool(t)

	// Форма пинится С АЛИАСОМ: у снимка FROM перестал быть одной таблицей
	// (зеркало томов-потребителей приезжает подзапросом), и без алиаса ссылка на
	// created_at стала бы неоднозначной. Пин ведёт за продуктом, а не наоборот —
	// иначе проба доказывала бы что-то о себе.
	requireRepoShape(t, "snapshot_repo.go",
		"(s.created_at, s.id) > ($%d, $%d)", "ORDER BY s.created_at ASC, s.id ASC")

	projects := seedTenantRows(t, ctx, pool, `
		INSERT INTO snapshots (id, project_id, name, source_volume_id, size_bytes, state, created_at)
		SELECT 'snp' || $1 || lpad(g::text, 8, '0'), $1, 'snp' || $1 || lpad(g::text, 8, '0'), NULL, 1073741824, 'READY',
		       TIMESTAMPTZ '2020-01-01 00:00:00Z' + (g || ' seconds')::interval
		  FROM generate_series(0, $2::int - 1) g`)
	_, err := pool.Exec(ctx, `ANALYZE snapshots`)
	require.NoError(t, err)

	plan := explainPlan(t, ctx, pool, `
		EXPLAIN (ANALYZE, FORMAT TEXT)
		SELECT id FROM snapshots
		 WHERE project_id = $1 AND (created_at, id) > ($2, $3)
		 ORDER BY created_at ASC, id ASC
		 LIMIT $4`,
		projects[0], cursorStartAt, "", cursorPageSize+1)

	assertPageBoundPlan(t, plan, "snapshots")
}

// assertPageBoundPlan — общее утверждение всех трёх проб: страница не сортируется
// и не читает проект целиком.
func assertPageBoundPlan(t *testing.T, plan, table string) {
	t.Helper()
	read := maxActualRows(t, plan)
	t.Logf("%s: страница %d строк с позиции %d — узел плана прочитал %d строк "+
		"(в таблице %d проектов × %d строк)",
		table, cursorPageSize, cursorStartOffset, read, cursorProbeProjects, cursorProbeRows)
	require.NotContains(t, plan, "Sort Method",
		"страница списка %s сортируется: планировщику пришлось упорядочить строки проекта, "+
			"то есть стоимость страницы растёт с размером проекта.\nПлан:\n%s", table, plan)
	require.LessOrEqual(t, read, cursorPageSize+1+cursorPlanSlack,
		"страница из %d строк прочитала %d строк (в проекте %d, проектов в таблице %d): "+
			"стоимость страницы обязана определяться размером страницы, а не размером проекта "+
			"и не его долей в таблице.\nПлан:\n%s",
		cursorPageSize, read, cursorProbeRows, cursorProbeProjects, plan)
}
