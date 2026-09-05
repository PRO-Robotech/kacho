// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stuck_delete_sweep_claim_integration_test.go — проход добивателя берёт ОДНА
// реплика.
//
// # Предмет
//
// Выборка добивателя детерминирована: `WHERE status='DELETING' AND deleting_since
// < … ORDER BY deleting_since LIMIT 50`. Без развода N реплик берут ТЕ ЖЕ
// пятьдесят машин и зовут по каждой двух соседей — предел партии, заведённый ради
// бережности к соседям, молча умножается на число реплик, а компонент
// масштабируется по загрузке, то есть умножение включается само и под нагрузкой.
//
// # Почему интеграционная, а не юнит
//
// Разводит БАЗА: замок прохода живёт в её пространстве ключей и действует между
// процессами. Юнит с подставным репозиторием доказал бы работу собственного
// дублёра — того, чего в бою нет.
//
// Реплики выражены НЕЗАВИСИМЫМИ ПУЛАМИ над ОДНОЙ базой: сессионный замок
// принадлежит соединению, и проба на одном пуле молча проверяла бы
// взаимоисключение внутри процесса.
//
// # Почему детерминированно и без ожиданий
//
// Заявители выстраиваются барьером: каждый пробует взять проход и НЕ отпускает,
// пока не попробовали все. «Взял ровно один» становится свойством, а не
// расписанием.
package repo_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

// sweepReplicas поднимает n независимых пулов над ОДНОЙ базой — модель n реплик.
func sweepReplicas(t *testing.T, n int) []*repo.InstanceRepo {
	t.Helper()
	dsn := setupTestDB(t)
	out := make([]*repo.InstanceRepo, 0, n)
	for i := 0; i < n; i++ {
		var pool *pgxpool.Pool
		pool, err := coredb.NewPool(context.Background(), dsn)
		require.NoError(t, err)
		pgtest.ClosePoolAtEnd(t, pool)
		out = append(out, repo.NewInstanceRepo(pool))
	}
	return out
}

// TestIntegration_StuckDeleteSweep_TakenByExactlyOneReplica — несущее утверждение.
func TestIntegration_StuckDeleteSweep_TakenByExactlyOneReplica(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	const replicas = 4
	repos := sweepReplicas(t, replicas)

	var (
		taken    atomic.Int64
		releases = make([]func(context.Context), replicas)
		errs     = make([]error, replicas)
		start    sync.WaitGroup
		tried    sync.WaitGroup
		wg       sync.WaitGroup
	)
	start.Add(1)
	tried.Add(replicas)

	for i := 0; i < replicas; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			release, ok, err := repos[i].TryClaimStuckDeleteSweep(ctx)
			// Отказ базы не роняется здесь: require.FailNow из чужой горутины
			// оставил бы барьер незакрытым, и проба зависла бы вместо падения.
			errs[i] = err
			if err == nil && ok {
				taken.Add(1)
				releases[i] = release
			}
			tried.Done()
			tried.Wait()
			if releases[i] != nil {
				releases[i](ctx)
			}
		}(i)
	}
	start.Done()
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "реплика %d: отказ базы на заявке", i)
	}
	require.Equal(t, int64(1), taken.Load(),
		"проход добивателя обязан достаться ровно одной реплике из %d", replicas)
}

// TestIntegration_StuckDeleteSweep_ReleasedClaimIsTakenAgain — положительный
// контроль к утверждению выше.
//
// Без него «взял ровно один» зеленело бы и на замке, который не отдаётся никому и
// никогда: добиватель молча перестал бы работать, а снаружи это выглядело бы
// точно так же, как исправный развод.
func TestIntegration_StuckDeleteSweep_ReleasedClaimIsTakenAgain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	repos := sweepReplicas(t, 2)
	a, b := repos[0], repos[1]

	release, ok, err := a.TryClaimStuckDeleteSweep(ctx)
	require.NoError(t, err)
	require.True(t, ok, "первый заявитель обязан получить проход")

	_, ok, err = b.TryClaimStuckDeleteSweep(ctx)
	require.NoError(t, err)
	require.False(t, ok, "пока держит первый — второму отказ")

	release(ctx)

	release2, ok, err := b.TryClaimStuckDeleteSweep(ctx)
	require.NoError(t, err)
	require.True(t, ok, "после снятия проход обязан достаться следующему")
	release2(ctx)
}
