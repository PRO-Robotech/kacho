// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

func aMessage(rev int64) *vpcv1.WatchIntentResponse {
	return &vpcv1.WatchIntentResponse{
		Event: &vpcv1.WatchIntentResponse_Intent{
			Intent: &vpcv1.DataplaneIntent{Revision: rev},
		},
	}
}

// Очередь незавершённых сообщений НЕ растёт за свой предел.
//
// Предмет — исчерпание памяти процесса, обслуживающего ВСЕХ: медленный
// получатель, которому нечем возразить, копит здесь столько, сколько успеет
// произвести источник. Предел обязан быть ПРЕДЕЛОМ, а не пожеланием, поэтому
// проба кладёт вдвое больше и требует двух свойств сразу: длина не переросла
// предел И превышение НАЗВАНО (иначе отбрасывание было бы молчаливым, а
// потерянное сообщение неотличимо от непришедшего).
func TestPendingNeverGrowsPastItsLimit(t *testing.T) {
	const limit = 8
	q := newPending(limit, 20*time.Millisecond)

	accepted := 0
	for i := 1; i <= 2*limit; i++ {
		if q.Offer(t.Context(), aMessage(int64(i))) == offerAccepted {
			accepted++
		}
	}

	assert.LessOrEqual(t, q.Len(), limit,
		"очередь выросла до %d при пределе %d — предел не является пределом", q.Len(), limit)
	assert.Equal(t, limit, accepted,
		"принято %d сообщений при пределе %d", accepted, limit)
	assert.True(t, q.Overflowed(),
		"предел исчерпан, а очередь об этом не сообщает: молчаливое отбрасывание "+
			"неотличимо от отсутствия изменений")
}

// Положительный контроль: пока предел не достигнут, очередь принимает всё и
// переполнения НЕ объявляет.
//
// Без этой половины проба выше зеленела бы на очереди, которая отвергает вообще
// всё и всегда кричит о переполнении, — то есть на сломанном потоке.
func TestPendingAcceptsUpToItsLimitAndStaysQuiet(t *testing.T) {
	const limit = 8
	q := newPending(limit, 20*time.Millisecond)

	for i := 1; i <= limit; i++ {
		require.Equal(t, offerAccepted, q.Offer(t.Context(), aMessage(int64(i))),
			"сообщение %d отвергнуто, хотя предел %d не достигнут", i, limit)
	}
	assert.Equal(t, limit, q.Len())
	assert.False(t, q.Overflowed(),
		"переполнение объявлено на заполненной, но не переполненной очереди")
}

// Получатель, ОТСТАВШИЙ, но живой, переполнения не вызывает.
//
// Это различение — весь предмет предела. Мгновенно полная очередь означает лишь
// то, что источник опередил получателя на планировщике; объявив это
// переполнением, мы отправляли бы исправного исполнителя на полную
// пересинхронизацию по расписанию операционной системы. Переполнение обязано
// означать «получатель не забирает», а не «получатель не успел в эту
// миллисекунду».
func TestPendingWaitsForASlowButLiveReceiver(t *testing.T) {
	const limit = 4
	q := newPending(limit, time.Second)

	for i := 1; i <= limit; i++ {
		require.Equal(t, offerAccepted, q.Offer(t.Context(), aMessage(int64(i))))
	}
	// Очередь полна. Получатель заберёт одно сообщение — но не сразу.
	go func() {
		time.Sleep(50 * time.Millisecond)
		<-q.C()
	}()

	assert.Equal(t, offerAccepted, q.Offer(t.Context(), aMessage(int64(limit+1))),
		"источник сдался на получателе, который отстал, но забирает")
	assert.False(t, q.Overflowed(), "отставание объявлено переполнением")
}

// Отмена контекста переполнением НЕ является.
//
// Гашение процесса и медленный исполнитель — разные события; смешав их, мы
// получили бы «переполнений за жизнь: столько же, сколько перезапусков».
func TestPendingCancellationIsNotOverflow(t *testing.T) {
	q := newPending(1, time.Minute)
	require.Equal(t, offerAccepted, q.Offer(t.Context(), aMessage(1)))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.Equal(t, offerAborted, q.Offer(ctx, aMessage(2)))
	assert.False(t, q.Overflowed(), "отмена контекста засчитана за переполнение")
}

// Пока в очереди ЕСТЬ место, исход постановки не зависит от состояния контекста.
//
// Проба гоняет сотню постановок при отменённом контексте и свободной очереди.
// Предмет — недетерминированность: `select` с двумя готовыми ветвями выбирает
// случайную, поэтому реализация, спрашивающая отмену на быстром пути, ведёт себя
// по-разному от запуска к запуску. Такой поток то доставляет страницу, то
// объявляет переполнение — и оба исхода выглядят как «работает», пока кто-нибудь
// не прочитает журнал.
func TestPendingWithRoomIsDeterministicRegardlessOfContext(t *testing.T) {
	const n = 100
	q := newPending(n, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for i := 1; i <= n; i++ {
		require.Equal(t, offerAccepted, q.Offer(ctx, aMessage(int64(i))),
			"постановка №%d отвергнута при свободной очереди: исход зависит от гонки, а не от предела", i)
	}
	assert.False(t, q.Overflowed())
}

// Три исхода постановки РАЗЛИЧАЮТСЯ, и различаются они по существу.
//
// «Отменён контекст» и «получатель не забирает» — разные события: первое
// означает гашение, второе обязано вылиться в полную пересинхронизацию. Сведи их
// в один булев исход, и наблюдаемость предела станет счётчиком перезапусков.
func TestPendingDistinguishesStallFromShutdown(t *testing.T) {
	stalled := newPending(1, 20*time.Millisecond)
	require.Equal(t, offerAccepted, stalled.Offer(context.Background(), aMessage(1)))
	assert.Equal(t, offerStalled, stalled.Offer(context.Background(), aMessage(2)))
	assert.True(t, stalled.Overflowed())

	aborted := newPending(1, time.Minute)
	require.Equal(t, offerAccepted, aborted.Offer(context.Background(), aMessage(1)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Equal(t, offerAborted, aborted.Offer(ctx, aMessage(2)))
	assert.False(t, aborted.Overflowed())
}
