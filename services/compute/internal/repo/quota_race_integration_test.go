// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build integration

package repo

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestQuota_ConcurrentCreatesRespectTheLimit — при лимите N и 2N одновременных
// списаний проходит РОВНО N.
//
// # Почему проба обязана быть конкурентной
//
// Последовательная проверка «при исчерпанном пределе следующее списание
// отвергается» зеленеет и на коде, где предел читается отдельным запросом, а
// списывается вторым. Между ними помещается чужая запись: два создателя увидят
// одно и то же свободное место и оба пройдут — и потолок выродится в частоту,
// умноженную на число параллельных пар.
//
// Проба без конкуренции этого не ловит вовсе, поэтому по правилу продукта она
// НЕ ПРИНИМАЕТСЯ как проверка этого свойства.
func TestQuota_ConcurrentCreatesRespectTheLimit(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	const (
		projectID = "prj-quota-race"
		limit     = 5
		attempts  = 10 // 2N: половина обязана не пройти
	)

	_, err := pool.Exec(ctx,
		`INSERT INTO project_instance_quotas (project_id, limit_value, used) VALUES ($1, $2, 0)`,
		projectID, limit)
	require.NoError(t, err)

	var wg sync.WaitGroup
	results := make([]error, attempts)
	start := make(chan struct{})

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // все стартуют разом, иначе это не гонка

			tx, err := pool.Begin(ctx)
			if err != nil {
				results[idx] = err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()

			if err := chargeProjectQuota(ctx, tx, projectID); err != nil {
				results[idx] = err
				return
			}
			results[idx] = tx.Commit(ctx)
		}(i)
	}
	close(start)
	wg.Wait()

	var passed, refused int
	for _, err := range results {
		switch {
		case err == nil:
			passed++
		case errors.Is(err, ErrProjectInstanceLimit):
			refused++
		default:
			// Отказ хранилища, не отображённый в понятную ошибку, — отдельный
			// дефект: арендатор увидел бы «что-то сломалось» вместо «место
			// кончилось».
			require.Failf(t, "непонятный отказ", "списание отказало не пределом: %v", err)
		}
	}

	require.Equal(t, limit, passed,
		"прошло не ровно столько, сколько разрешено: потолок не держит под конкуренцией")
	require.Equal(t, attempts-limit, refused,
		"остальные обязаны получить именно исчерпание предела")

	// Счётчик в базе совпадает с числом прошедших: иначе успех означал бы не то,
	// что записано.
	var used int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT used FROM project_instance_quotas WHERE project_id = $1`, projectID).Scan(&used))
	require.Equal(t, limit, used)
}

// TestQuota_RefundReturnsTheSlot — удаление возвращает место, и оно снова
// используется.
//
// Отрицание в паре: без положительной половины «списание отвергается» зеленело
// бы на проекте, где место кончилось навсегда, — то есть на невозврате, который
// эта проба и должна ловить.
func TestQuota_RefundReturnsTheSlot(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	const projectID = "prj-quota-refund"
	_, err := pool.Exec(ctx,
		`INSERT INTO project_instance_quotas (project_id, limit_value, used) VALUES ($1, 1, 0)`,
		projectID)
	require.NoError(t, err)

	charge := func() error {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		if err := chargeProjectQuota(ctx, tx, projectID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	require.NoError(t, charge(), "первое списание при лимите 1 обязано пройти")

	// (−) место кончилось
	require.ErrorIs(t, charge(), ErrProjectInstanceLimit)

	// возврат
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, refundProjectQuota(ctx, tx, projectID))
	require.NoError(t, tx.Commit(ctx))

	// (+) освободившееся место снова используется
	require.NoError(t, charge(), "возвращённое место обязано быть доступно снова")
}

// TestQuota_LoweringBelowUsageIsExpressibleAndFreezes — администратор понижает
// предел ниже текущего потребления; старые машины живут, новые не создаются,
// удаление работает.
//
// # Что здесь утверждается и почему это НЕ «перерасход»
//
// `used > limit_value` — законное состояние, а не поломка: оно означает ровно
// то, что нужно после понижения — новые нельзя, старые живут. Запретить его
// схемой значит запретить САМО ПОНИЖЕНИЕ, потому что записать новый предел
// можно было бы только после того, как арендатор сам удалит машины. То есть
// административное действие становится заложником того, кого оно ограничивает.
//
// # Почему отрицание идёт в паре с положительным контролем
//
// «Списание отвергнуто» само по себе зеленеет и на проекте, где сломано всё:
// на исчерпанном пределе, на потерянной строке, на отказе хранилища. Поэтому
// сначала утверждается, что до понижения списание ПРОХОДИТ, а в конце — что
// освободившееся место снова используется, когда потребление опускается под
// новый предел.
func TestQuota_LoweringBelowUsageIsExpressibleAndFreezes(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	const projectID = "prj-quota-lower"

	_, err := pool.Exec(ctx,
		`INSERT INTO project_instance_quotas (project_id, limit_value, used) VALUES ($1, 3, 0)`,
		projectID)
	require.NoError(t, err)

	charge := func() error {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		if err := chargeProjectQuota(ctx, tx, projectID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	refund := func() {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		require.NoError(t, refundProjectQuota(ctx, tx, projectID))
		require.NoError(t, tx.Commit(ctx))
	}
	usedNow := func() int {
		var used int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT used FROM project_instance_quotas WHERE project_id = $1`, projectID).Scan(&used))
		return used
	}

	// (+) до понижения предел работает как обычно
	for i := 0; i < 3; i++ {
		require.NoError(t, charge(), "списание в пределах предела обязано проходить")
	}
	require.Equal(t, 3, usedNow())

	// Само понижение — то, ради чего проба написана. Сегодня падает на 23514.
	_, err = pool.Exec(ctx,
		`UPDATE project_instance_quotas SET limit_value = 1 WHERE project_id = $1`, projectID)
	require.NoError(t, err,
		"понижение предела ниже потребления обязано быть выразимо: иначе администратор "+
			"не может ограничить проект, пока проект сам не освободит место")

	// Заморозка: новые нельзя…
	require.ErrorIs(t, charge(), ErrProjectInstanceLimit,
		"после понижения создание обязано отвергаться именно пределом")

	// …старые живут, и удаление работает.
	refund()
	refund()
	require.Equal(t, 1, usedNow(), "возврат обязан работать и на замороженном проекте")

	// Потребление всё ещё не ниже нового предела — по-прежнему заморожено.
	require.ErrorIs(t, charge(), ErrProjectInstanceLimit)

	// (+) опустились под новый предел — место снова доступно.
	refund()
	require.Equal(t, 0, usedNow())
	require.NoError(t, charge(),
		"под новым пределом создание обязано снова проходить: иначе заморозка вечна")
}

// TestQuota_ProjectWithoutLimitIsNotBlocked — проект без назначенного предела не
// блокируется.
//
// Отсутствие строки означает «предел не назначен» — платформенный домен ещё не
// проецировал сюда значение. Трактовать это как «предел ноль» значило бы
// остановить весь продукт в момент, когда счётчик только заводится.
func TestQuota_ProjectWithoutLimitIsNotBlocked(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	require.NoError(t, chargeProjectQuota(ctx, tx, "prj-no-limit-row"),
		"проект без назначенного предела не должен отвергаться")
}
