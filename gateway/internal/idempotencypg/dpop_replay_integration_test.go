// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package idempotencypg_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// TestDPoPReplay_SecondReplicaRejectsTheSameProof — ПРЕДИКАТ ЗАДАЧИ #909:
// доказательство, предъявленное одной реплике, отвергается ДРУГОЙ.
//
// Пока запись жила в памяти процесса, обещание однократности было верно ровно
// до второй реплики, и различие наблюдалось только на ней. Проба на одном
// экземпляре зеленела бы и на памяти — то есть закрепляла бы дефект.
func TestDPoPReplay_SecondReplicaRejectsTheSameProof(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()

	// ДВЕ НЕЗАВИСИМЫЕ реплики: свои пулы, своя жизнь, общая база — ровно то,
	// чем флот отличается от процесса.
	first := replica(t, dsn, idempotencypg.Config{})
	second := replica(t, dsn, idempotencypg.Config{})

	const jti = "jti-909-cross-replica"
	if err := first.AddDPoPProof(ctx, jti, time.Minute); err != nil {
		t.Fatalf("первое предъявление обязано пройти: %v", err)
	}

	err := second.AddDPoPProof(ctx, jti, time.Minute)
	if !errors.Is(err, idempotencypg.ErrDPoPReplay) {
		t.Fatalf("повтор на ДРУГОЙ реплике обязан быть отвергнут, получено: %v", err)
	}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: отвергается повтор, а не всё подряд. Без него
	// проба зеленела бы на хранилище, которое не пропускает ничего.
	if err := second.AddDPoPProof(ctx, jti+"-другое", time.Minute); err != nil {
		t.Fatalf("другое доказательство на той же реплике обязано пройти: %v", err)
	}
}

// TestDPoPReplay_ConcurrentReplicasAdmitExactlyOne — под конкуренцией допуск
// получает РОВНО ОДИН.
//
// Программная пара «посмотреть — записать» прошла бы обоих: между вопросом и
// записью открыто окно, и это ровно та конкуренция, ради которой хранилище
// вынесено из процесса. Здесь допуск — один оператор, и проба это утверждает.
func TestDPoPReplay_ConcurrentReplicasAdmitExactlyOne(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()

	const replicas = 8
	stores := make([]*idempotencypg.Store, replicas)
	for i := range stores {
		stores[i] = replica(t, dsn, idempotencypg.Config{})
	}

	const jti = "jti-909-concurrent"
	var (
		admitted int64
		rejected int64
		start    = make(chan struct{})
		wg       sync.WaitGroup
	)
	for _, s := range stores {
		wg.Add(1)
		go func(s *idempotencypg.Store) {
			defer wg.Done()
			<-start // одновременный старт: без него реплики идут по очереди
			switch err := s.AddDPoPProof(ctx, jti, time.Minute); {
			case err == nil:
				atomic.AddInt64(&admitted, 1)
			case errors.Is(err, idempotencypg.ErrDPoPReplay):
				atomic.AddInt64(&rejected, 1)
			default:
				t.Errorf("непредвиденный отказ хранилища: %v", err)
			}
		}(s)
	}
	close(start)
	wg.Wait()

	t.Logf("перепись: реплик %d; допущено %d; отвергнуто %d", replicas, admitted, rejected)
	if admitted != 1 {
		t.Fatalf("допущено %d предъявлений одного доказательства, ожидалось ровно одно", admitted)
	}
	if rejected != replicas-1 {
		t.Fatalf("отвергнуто %d, ожидалось %d — остальные исходы потерялись", rejected, replicas-1)
	}
}

// TestDPoPReplay_ExpiredProofDoesNotBanTheValueForever — просроченная запись не
// становится вечным запретом.
//
// Иначе уникальный ключ, заведённый ради однократности, превратился бы в
// запрет на повторное значение навсегда — а за пределами окна свежести повтор
// отвергается уже проверкой времени, и держать запись незачем.
func TestDPoPReplay_ExpiredProofDoesNotBanTheValueForever(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()
	s := replica(t, dsn, idempotencypg.Config{})

	const jti = "jti-909-expired"
	// Окно свежести в прошлом: запись рождается уже просроченной.
	if err := s.AddDPoPProof(ctx, jti, time.Millisecond); err != nil {
		t.Fatalf("первое предъявление: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	if err := s.AddDPoPProof(ctx, jti, time.Minute); err != nil {
		t.Fatalf("за пределами окна свежести значение обязано освободиться: %v", err)
	}

	// И снова однократно — освобождение не отменяет самой гарантии.
	if err := s.AddDPoPProof(ctx, jti, time.Minute); !errors.Is(err, idempotencypg.ErrDPoPReplay) {
		t.Fatalf("после переоткрытия окна повтор обязан отвергаться, получено: %v", err)
	}
}

// TestDPoPReplay_PurgeRemovesOnlyExpired — уборка не трогает живые записи.
func TestDPoPReplay_PurgeRemovesOnlyExpired(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()
	s := replica(t, dsn, idempotencypg.Config{})

	if err := s.AddDPoPProof(ctx, "jti-909-live", time.Hour); err != nil {
		t.Fatalf("живое предъявление: %v", err)
	}
	if err := s.AddDPoPProof(ctx, "jti-909-stale", time.Millisecond); err != nil {
		t.Fatalf("просроченное предъявление: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	sw, err := s.PurgeExpiredDPoPProofs(ctx)
	if err != nil {
		t.Fatalf("уборка: %v", err)
	}
	if sw.Removed != 1 {
		t.Fatalf("убрано %d записей, ожидалась одна просроченная", sw.Removed)
	}
	// Живая запись уцелела: повтор по ней по-прежнему отвергается.
	if err := s.AddDPoPProof(ctx, "jti-909-live", time.Hour); !errors.Is(err, idempotencypg.ErrDPoPReplay) {
		t.Fatalf("уборка снесла живую запись — гарантия однократности потеряна: %v", err)
	}
}
