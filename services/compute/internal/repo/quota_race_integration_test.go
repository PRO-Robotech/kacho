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
