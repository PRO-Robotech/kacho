// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subject_change_repo_integration_test.go — integration tests for SubjectChangeRepo.
// Verifies PollSubjectChanges returns ascending rows, honours limit, and
// reports headID correctly. Uses testcontainers Postgres (same pattern as
// sibling integration tests). Skipped under testing.Short().
//
// .3.
package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	"github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/access_binding"
	kachopg "github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg"
)

// TestSubjectChangeRepo_PollSubjectChanges verifies:
//  1. Returns rows with id > since_id, ascending order.
//  2. Honours limit (requests 2 of 3 → receives 2).
//  3. headID НЕ ПЕРЕПРЫГИВАЕТ непрочитанное: на ПОЛНОЙ странице это последняя
//     отданная строка, на неполной — граница устоявшегося.
//  4. Continuing cursor returns the remaining row.
//
// # Пункт 3 ПЕРЕПИСАН, и прежняя редакция была неверна (kacho#1374)
//
// Он требовал `headID = MAX(id)` «независимо от положения курсора» — то есть
// закреплял ровно ту потерю, ради которой заведена задача: потребитель двигает
// курсор на отданную позицию, а `MAX(id)` лежит ЗА непрочитанным хвостом полной
// страницы и за номером всякого писателя в полёте. Утверждение усилено, а не
// ослаблено: теперь проба требует, чтобы позиция НЕ обгоняла отданное.
func TestSubjectChangeRepo_PollSubjectChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires Docker)")
	}

	ctx := context.Background()
	dsn := kachopg.NewTestPostgres(t)

	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	repo := kachopg.NewSubjectChangeRepo(pool)

	// Посев идёт ТЕМ ЖЕ путём, каким пишет прод (`EmitSubjectChangeEvent`), а не
	// собственным INSERT'ом: фикстура не вправе быть снисходительнее продукта.
	// Прод кладёт в строку `payload` — тело, которое единственное и получает
	// декодер дренажа (его godoc это оговаривает), — а прежний посев называл
	// только `(subject_id, op)`. Такую строку прод не производит ни при каком
	// входе, и разобрать её нельзя: проба стерегла форму, которой в очереди не
	// бывает. Миграция 0097 закрепила это схемой, и посев через писателя
	// продукта означает, что расходиться с ним больше нечему.
	abRepo := kachopg.New(pool, nil)
	seed := func(subjectID, op string) int64 {
		t.Helper()
		w, err := abRepo.Writer(ctx)
		require.NoError(t, err)
		require.NoError(t, w.AccessBindingsW().EmitSubjectChangeEvent(ctx,
			access_binding.SubjectChangeEvent{SubjectID: subjectID, Op: op}))
		require.NoError(t, w.Commit(ctx))

		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT id FROM kacho_iam.subject_change_outbox WHERE subject_id = $1`,
			subjectID).Scan(&id))
		return id
	}

	// Seed 3 rows.
	id1 := seed("usr_a", "binding_upsert")
	id2 := seed("usr_b", "binding_delete")
	id3 := seed("usr_c", "binding_upsert")

	// ── Poll 1: since=0, limit=2 → first 2 rows; headID=id2 ─────────────────
	changes, headID, err := repo.PollSubjectChanges(ctx, 0, 2)
	require.NoError(t, err)
	require.Len(t, changes, 2, "expected 2 changes (limit=2)")
	require.Equal(t, id1, changes[0].ID)
	require.Equal(t, "usr_a", changes[0].SubjectID)
	require.Equal(t, "binding_upsert", changes[0].Op)
	require.Equal(t, id2, changes[1].ID)
	require.Equal(t, "usr_b", changes[1].SubjectID)
	require.Equal(t, "binding_delete", changes[1].Op)
	require.Equal(t, id2, headID,
		"страница полна, значит за id2 остались непрочитанные: позиция обязана остановиться на id2, а не уйти на MAX(id)=id3")
	require.Less(t, headID, id3, "позиция не вправе обгонять непрочитанное")

	// ── Poll 2: since=id2, limit=256 → only third row; headID=id3 ────────────
	//
	// Страница НЕПОЛНАЯ — окно вычитано целиком, и позиция вправе назвать границу.
	changes2, headID2, err := repo.PollSubjectChanges(ctx, id2, 256)
	require.NoError(t, err)
	require.Len(t, changes2, 1, "expected 1 remaining change")
	require.Equal(t, id3, changes2[0].ID)
	require.Equal(t, "usr_c", changes2[0].SubjectID)
	require.Equal(t, "binding_upsert", changes2[0].Op)
	require.Equal(t, id3, headID2)
}
