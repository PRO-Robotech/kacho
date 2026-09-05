// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// pass_claim_integration_test.go — проход сверщика берёт ОДНА реплика.
//
// # Предмет
//
// Фоновая работа поднимается в каждой реплике: у процесса нет способа узнать, что
// он не один. Выборка расхождений детерминирована, поэтому без развода N реплик
// берут ТЕ ЖЕ строки и зовут по каждой плоскость данных.
//
// # Почему проба интеграционная, а не юнит
//
// Разводит здесь БАЗА, а не код: замок прохода живёт в её пространстве ключей и
// действует между процессами. Юнит с подставным хранилищем доказал бы работу
// собственного дублёра — то есть ровно того, чего в бою нет.
//
// Две реплики выражены ДВУМЯ ПУЛАМИ над ОДНОЙ базой. Это не декорация: сессионный
// замок принадлежит соединению, и проба на одном пуле молча проверяла бы
// взаимоисключение внутри процесса.
//
// # Почему детерминированно и без ожиданий
//
// Заявители выстраиваются барьером: каждый пытается взять проход и НЕ отпускает,
// пока не попробовали все. Тогда «взял ровно один» есть свойство, а не расписание,
// и проба не зависит ни от таймингов, ни от порядка планировщика.
package reconciler_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/singlepass"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
)

// replicaPools поднимает n независимых пулов над ОДНОЙ базой — модель n реплик
// сервиса.
func replicaPools(t *testing.T, n int) []*pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test (testcontainers Postgres) — skipped with -short")
	}
	dsn := pgtest.NewDB(t) + "&pool_max_conns=8"
	pools := make([]*pgxpool.Pool, 0, n)
	for i := 0; i < n; i++ {
		pool, err := coredb.NewPool(context.Background(), dsn)
		require.NoError(t, err)
		pgtest.ClosePoolAtEnd(t, pool)
		pools = append(pools, pool)
	}
	return pools
}

// TestIntegration_ReconcilePass_TakenByExactlyOneReplica — несущее утверждение.
//
// Четыре реплики одновременно заявляются на проход по ОДНОМУ виду ресурса и
// держат заявку до конца раунда. Взять его обязан ровно один.
func TestIntegration_ReconcilePass_TakenByExactlyOneReplica(t *testing.T) {
	ctx := context.Background()
	const replicas = 4
	pools := replicaPools(t, replicas)

	var (
		taken    atomic.Int64
		releases = make([]singlepass.Release, replicas)
		errs     = make([]error, replicas)
		start    sync.WaitGroup // все заявляются одновременно
		tried    sync.WaitGroup // никто не отпускает, пока не попробовали все
		wg       sync.WaitGroup
	)
	start.Add(1)
	tried.Add(replicas)

	for i := 0; i < replicas; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := reconciler.NewStore(pools[i])
			start.Wait()
			release, ok, err := store.TryClaimKindPass(ctx, reconciler.KindVolume)
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
		"проход по одному виду обязан достаться ровно одной реплике из %d", replicas)
}

// TestIntegration_ReconcilePass_KindsAreIndependent — положительный контроль.
//
// Без него утверждение выше зеленело бы и на замке, который не отдаётся НИКОМУ и
// НИКОГДА: «взял ровно один» выполняется и тогда, когда взявший — единственный
// возможный. Здесь проверяется обратное: вид — ключ разбиения, поэтому занятый
// том не мешает взять снимок и образ.
func TestIntegration_ReconcilePass_KindsAreIndependent(t *testing.T) {
	ctx := context.Background()
	pools := replicaPools(t, 2)
	a, b := reconciler.NewStore(pools[0]), reconciler.NewStore(pools[1])

	relVolume, ok, err := a.TryClaimKindPass(ctx, reconciler.KindVolume)
	require.NoError(t, err)
	require.True(t, ok, "первый заявитель обязан получить проход")
	defer relVolume(ctx)

	_, ok, err = b.TryClaimKindPass(ctx, reconciler.KindVolume)
	require.NoError(t, err)
	require.False(t, ok, "второй заявитель на ТОТ ЖЕ вид обязан получить отказ")

	relSnapshot, ok, err := b.TryClaimKindPass(ctx, reconciler.KindSnapshot)
	require.NoError(t, err)
	require.True(t, ok, "другой вид — другой ключ разбиения, он обязан быть свободен")
	relSnapshot(ctx)

	relImage, ok, err := b.TryClaimKindPass(ctx, reconciler.KindImage)
	require.NoError(t, err)
	require.True(t, ok, "и третий вид тоже")
	relImage(ctx)
}

// TestIntegration_ReconcilePass_ReleasedClaimIsTakenAgain — замок отпускается.
//
// Проба существует ради того, чтобы «развели» не превратилось в «заклинило»:
// замок, который берётся один раз и не отдаётся, останавливает сверку навсегда, и
// снаружи это выглядит точно так же, как исправный развод.
func TestIntegration_ReconcilePass_ReleasedClaimIsTakenAgain(t *testing.T) {
	ctx := context.Background()
	pools := replicaPools(t, 2)
	a, b := reconciler.NewStore(pools[0]), reconciler.NewStore(pools[1])

	release, ok, err := a.TryClaimKindPass(ctx, reconciler.KindVolume)
	require.NoError(t, err)
	require.True(t, ok)

	_, ok, err = b.TryClaimKindPass(ctx, reconciler.KindVolume)
	require.NoError(t, err)
	require.False(t, ok, "пока держит первый — второму отказ")

	release(ctx)

	release2, ok, err := b.TryClaimKindPass(ctx, reconciler.KindVolume)
	require.NoError(t, err)
	require.True(t, ok, "после снятия проход обязан достаться следующему")
	release2(ctx)
}

// TestIntegration_ReconcileOnce_SkipsTheKindAnotherReplicaHolds — исход прохода.
//
// Утверждает не сам замок, а то, ЧТО ВИДИТ вызывающий: вид, занятый другой
// репликой, считается пропущенным (Skipped), а не отказавшим (Failed). Различие
// несущее — до этого проигрыш гонки записывался ошибкой, то есть N−1 реплик
// писали отказ на каждом УСПЕШНОМ проходе.
func TestIntegration_ReconcileOnce_SkipsTheKindAnotherReplicaHolds(t *testing.T) {
	ctx := context.Background()
	pools := replicaPools(t, 2)

	// Реплика-«сосед» держит проход по всем трём видам.
	held := reconciler.NewStore(pools[0])
	for _, kind := range reconciler.AllKinds() {
		release, ok, err := held.TryClaimKindPass(ctx, kind)
		require.NoError(t, err)
		require.True(t, ok)
		defer release(ctx)
	}

	rec := reconciler.New(reconciler.NewStore(pools[1]), refusingOpener{}, reconciler.Config{})
	counts := rec.Once(ctx)

	require.Equal(t, len(reconciler.AllKinds()), counts.Skipped,
		"каждый занятый вид обязан быть посчитан пропуском")
	require.Zero(t, counts.Failed, "пропуск НЕ является отказом")
	require.Zero(t, counts.Scanned, "занятый вид не читается вовсе")
}

// refusingOpener — открыватель бэкенда, который НЕ ДОЛЖЕН быть позван.
//
// Дублёр, молча отдающий рабочий бэкенд, сделал бы пробу выше зелёной и на
// сверщике, который занятый вид всё-таки читает: строк в базе нет, счётчики
// сошлись бы сами собой. Отказ превращает «позвали» в наблюдаемое событие.
type refusingOpener struct{}

func (refusingOpener) Open(context.Context, reconciler.Binding) (blockbackend.Backend, error) {
	return nil, errors.New("проход занятого вида не имеет права открывать бэкенд")
}
