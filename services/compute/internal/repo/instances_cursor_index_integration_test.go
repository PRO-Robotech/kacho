// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Тот же предмет, что у списков блочного хранения: курсорная пагинация продаётся
// клиенту как ПОСТОЯННАЯ стоимость страницы, а индекса под пару «проект +
// (создано, id)» у главной тенантной таблицы сервиса не было — только раздельные
// однополевые. Планировщику оставались два плана, и оба не по странице: скан по
// проекту с сортировкой всех его строк либо скан по времени создания с
// отбрасыванием чужих проектов.
//
// Соседний каталог размеров машин композит под курсор уже нёс — то есть о пути
// доступа думали, но не здесь.
//
// Проверяется ПЛАН и число прочитанных строк: индекс, который планировщик не
// выбирает, не стоит ничего.

const (
	// insCursorRows — строк на проект.
	//
	// Было 200. Тогда проба зеленела, но НЕ потому, что свойство держится: все
	// имена в ней были пустой строкой, строка таблицы была узкой, и курсорный
	// индекс выигрывал по стоимости с небольшим запасом. Имя перестало быть
	// пустым — единая форма имени пустой строки не допускает, — и строка стала
	// шире, и планировщик на этом объёме стал предпочитать скан по проекту с
	// последующей сортировкой — то есть проба покраснела на РАЗМЕРЕ ФИКСТУРЫ,
	// а не на продукте.
	//
	// Замерено на этом дереве: 200 строк на проект — план сортирует; 2000 —
	// берёт instances_project_cursor_idx и не сортирует. Снятие нового
	// уникального индекса (project_id, name) плана НЕ меняет, то есть дело в
	// объёме, а не в индексе.
	//
	// Объём поднят до 2000, чтобы выбор индекса перестал зависеть от ширины
	// строки: проба обязана утверждать свойство продукта, а не удачу оценки.
	insCursorRows     = 2000
	insCursorProjects = 8 // проектов в таблице (доля целевого — 1/8)
	insCursorPageSize = 20
	insCursorSlack    = 5
	insCursorOffset   = 120 // курсор в середине проекта
)

const insCursorStartAt = "2020-01-01T00:02:00Z" // база + 120 секунд

var insPlanRowsRe = regexp.MustCompile(`actual time=[0-9.]+\.\.[0-9.]+ rows=(\d+)`)

// TestIntegration_InstancesCursorIndex_PageDoesNotReadTheWholeProject — замок.
func TestIntegration_InstancesCursorIndex_PageDoesNotReadTheWholeProject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	// Предпосылка пробы: форма запроса в ней совпадает с формой репозитория.
	body, rerr := os.ReadFile("instance_repo.go")
	require.NoError(t, rerr)
	for _, frag := range []string{"(created_at, id) > ($%d, $%d)", "ORDER BY created_at ASC, id ASC"} {
		require.Contains(t, string(body), frag,
			"форма запроса списка машин разошлась с пробой (%q): проба доказывала бы что-то о себе", frag)
	}

	var projects []string
	for p := 0; p < insCursorProjects; p++ {
		prj := fmt.Sprintf("prj-cursor-%d", p)
		projects = append(projects, prj)
		_, err = pool.Exec(ctx, `
			INSERT INTO instances (id, project_id, zone_id, name, machine_type_id, status, created_at)
			SELECT 'ins-' || $1 || lpad(g::text, 8, '0'), $1, 'ru-central1-a',
			       'ins-' || $1 || lpad(g::text, 8, '0'), 'mt-std2', 1,
			       TIMESTAMPTZ '2020-01-01 00:00:00Z' + (g || ' seconds')::interval
			  FROM generate_series(0, $2::int - 1) g`, prj, insCursorRows)
		require.NoError(t, err)
	}
	_, err = pool.Exec(ctx, `ANALYZE instances`)
	require.NoError(t, err)

	rows, err := pool.Query(ctx, `
		EXPLAIN (ANALYZE, FORMAT TEXT)
		SELECT id FROM instances
		 WHERE project_id = $1 AND (created_at, id) > ($2, $3)
		 ORDER BY created_at ASC, id ASC
		 LIMIT $4`, projects[0], insCursorStartAt, "", insCursorPageSize+1)
	require.NoError(t, err)
	var b strings.Builder
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		b.WriteString(line)
		b.WriteString("\n")
	}
	require.NoError(t, rows.Err())
	rows.Close()
	plan := b.String()

	read := 0
	for _, m := range insPlanRowsRe.FindAllStringSubmatch(plan, -1) {
		n, cerr := strconv.Atoi(m[1])
		require.NoError(t, cerr)
		if n > read {
			read = n
		}
	}
	require.NotZero(t, read, "план не содержит ни одного узла с actual rows — проба ничего не измерила:\n%s", plan)
	t.Logf("instances: страница %d строк с позиции %d — узел плана прочитал %d строк "+
		"(в таблице %d проектов × %d строк)", insCursorPageSize, insCursorOffset, read, insCursorProjects, insCursorRows)

	require.NotContains(t, plan, "Sort Method",
		"страница списка машин сортируется: планировщику пришлось упорядочить строки проекта, "+
			"то есть стоимость страницы растёт с размером проекта.\nПлан:\n%s", plan)
	require.LessOrEqual(t, read, insCursorPageSize+1+insCursorSlack,
		"страница из %d строк прочитала %d строк (в проекте %d, проектов в таблице %d): стоимость "+
			"страницы обязана определяться размером страницы.\nПлан:\n%s",
		insCursorPageSize, read, insCursorRows, insCursorProjects, plan)
}
