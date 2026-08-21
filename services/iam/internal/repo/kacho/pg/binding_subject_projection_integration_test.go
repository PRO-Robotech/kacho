// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// binding_subject_projection_integration_test.go — строка субъекта обязана
// появляться ВМЕСТЕ с выдачей, каким бы путём выдача ни была записана.
//
// Предмет. Форма вердикта заходит в выдачи с пары «субъект + область» через
// kacho_iam.access_binding_subjects. Выдача, у которой дочерней строки нет,
// невидима вердикту целиком: право записано, а не действует — и не действует
// молча, потому что сама выдача на месте и читается всеми списками.
//
// Почему это не лечится дисциплиной вызывающего. Пара операторов «вставить
// выдачу» + «вставить субъектов» разнесена по двум методам репозитория, и
// второй зовёт лишь путь AccessBindingService.Create. Служебные пути, которые
// пишут выдачу внутри собственной транзакции (создание проекта — выдача
// администратора создателю; приглашение пользователя с ролью на проект), зовут
// только первый. Замер на живом стенде 2026-08-21: выдач 450, дочерних строк
// 339, выдач без субъекта 111 — из них 110 административных на проект (то есть
// создатели проектов) и одна от приглашения. Все 111 несли пару субъекта в
// самой выдаче, поэтому проекция восстановима без потери.
//
// Инвариант держится ТРИГГЕРОМ (ban #10), а не памятью: любой путь записи —
// включая те, которых ещё нет, — проецирует пару в дочернюю таблицу.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
)

// TestBindingSubjectProjection_InsertWithoutChildRow_StillVisible — выдача,
// записанная ОДНИМ оператором (как это делают служебные пути), обязана нести
// дочернюю строку субъекта сразу после вставки.
func TestBindingSubjectProjection_InsertWithoutChildRow_StillVisible(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires Postgres container")
	}
	ctx, pool := kac127Setup(t)
	// Помощник посева пишет выдачу ОДНИМ оператором — ровно так, как это делают
	// служебные пути продукта (выдача администратора создателю проекта,
	// приглашение с ролью). Он и есть предмет пробы.
	_, bindingID, _, roleID := kac127SeedABRow(t, ctx, pool, "absp1", domain.AccessBindingStatusActive)

	var prjID string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT resource_id FROM kacho_iam.access_bindings WHERE role_id=$1 LIMIT 1`, roleID).Scan(&prjID))

	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_iam.access_binding_subjects WHERE binding_id=$1`,
		bindingID).Scan(&n))
	require.Equal(t, 1, n,
		"строка субъекта обязана появиться вместе с выдачей: без неё выдача "+
			"невидима форме вердикта — право записано и не действует")

	// Область дочерней строки обязана совпасть с областью выдачи: ветвь выдач
	// сравнивает ОБЕ координаты, и расхождение сделало бы право невидимым тем
	// же способом, что и отсутствие строки.
	var rt, ri string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT resource_type, resource_id FROM kacho_iam.access_binding_subjects
		  WHERE binding_id=$1`, bindingID).Scan(&rt, &ri))
	require.Equal(t, "project", rt)
	require.Equal(t, prjID, ri)
}

// TestBindingSubjectProjection_ExplicitChildRow_NotDuplicated — законный
// близнец: путь, который субъектов пишет САМ, не должен получить второй
// экземпляр строки. Без этой пробы триггер, дублирующий строки, остался бы
// незамеченным — а дубль означает лишний проход по ветви выдач на каждом
// вопросе о правах.
func TestBindingSubjectProjection_ExplicitChildRow_NotDuplicated(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires Postgres container")
	}
	ctx, pool := kac127Setup(t)
	_, bindingID, userID, _ := kac127SeedABRow(t, ctx, pool, "absp2", domain.AccessBindingStatusActive)

	_, err := pool.Exec(ctx,
		`INSERT INTO kacho_iam.access_binding_subjects (binding_id, subject_type, subject_id, ordinal)
		 VALUES ($1, $2, $3, 0)
		 ON CONFLICT (binding_id, subject_type, subject_id) DO NOTHING`,
		bindingID, "user", userID)
	require.NoError(t, err, "повторная запись того же субъекта — идемпотентна")

	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_iam.access_binding_subjects WHERE binding_id=$1`,
		bindingID).Scan(&n))
	require.Equal(t, 1, n, "строка субъекта одна, а не две")
}

var _ = context.Background
