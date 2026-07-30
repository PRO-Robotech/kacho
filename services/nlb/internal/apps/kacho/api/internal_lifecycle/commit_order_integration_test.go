// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package internal_lifecycle

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
)

// TestSubscribe_InverseCommitOrder_DeliversBothEvents — контракт подписчика при
// ПАРАЛЛЕЛЬНЫХ писателях `nlb_outbox`.
//
// `sequence_no` выдаётся на INSERT, а строка видна на COMMIT: транзакция A
// может взять номер раньше B и закоммититься позже. Подписчик двигает курсор на
// номер последнего отданного события, поэтому уйти за номер A, пока A в полёте,
// значит потерять её событие навсегда — перечитывание идёт «больше курсора», и
// resume_from_event_id воспроизводит ту же дыру.
//
// Форма теста: пока A в полёте, стриму даётся окно на доставку. Окно — не
// синхронизация: в зелёном исходе доставка происходит ПОСЛЕ коммита A и
// проверяется отдельным дедлайном, а в красном (курсор ушёл за дыру) стрим
// отдаёт в это окно событие B — его NOTIFY будит подписчика сразу — и второго
// события не приходит уже никогда.
func TestSubscribe_InverseCommitOrder_DeliversBothEvents(t *testing.T) {
	env := setupIntTestEnv(t, 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Событие до подписки: его catchup доказывает, что стрим жив и вычерпан.
	seqWarmup := insertOutbox(t, env.pool, "nlb_load_balancer", "nlb-WARMUP", "prj-1", "CREATED", `{}`)

	stream, err := env.client.Subscribe(ctx, &lbv1.SubscribeRequest{})
	require.NoError(t, err)

	// Единственный читатель стрима: два параллельных Recv растащили бы события
	// между собой (grpc отдаёт каждое ровно одному Recv).
	events := pumpStream(ctx, stream)
	require.Equal(t, strconv.FormatInt(seqWarmup, 10),
		mustRecvEvent(t, events, 10*time.Second).GetEventId())

	// A берёт номер первой и остаётся в полёте.
	txA, err := env.pool.Begin(ctx)
	require.NoError(t, err)
	var seqA int64
	require.NoError(t, txA.QueryRow(ctx, `
		INSERT INTO kacho_nlb.nlb_outbox (resource_type, resource_id, project_id, action, payload)
		VALUES ('nlb_load_balancer', 'nlb-A', 'prj-1', 'CREATED', '{}'::jsonb)
		RETURNING sequence_no`).Scan(&seqA))

	// B берёт следующий номер и коммитится ПЕРВОЙ (её NOTIFY будит подписчика).
	seqB := insertOutbox(t, env.pool, "nlb_load_balancer", "nlb-B", "prj-1", "DELETED", `{}`)
	require.Greater(t, seqB, seqA, "B обязана нести больший номер, иначе сценарий не тот")

	// Окно, в котором подписчик успевает отреагировать на NOTIFY от B.
	select {
	case ev := <-events:
		t.Fatalf("подписчик отдал событие %s, пока меньший номер %d ещё в полёте: "+
			"курсор уходит за дыру и событие %d теряется молча",
			ev.GetEventId(), seqA, seqA)
	case <-time.After(2 * time.Second):
	}

	require.NoError(t, txA.Commit(ctx))

	first := mustRecvEvent(t, events, 15*time.Second)
	second := mustRecvEvent(t, events, 15*time.Second)
	require.Equal(t,
		[]string{strconv.FormatInt(seqA, 10), strconv.FormatInt(seqB, 10)},
		[]string{first.GetEventId(), second.GetEventId()},
		"оба события обязаны прийти, в порядке номеров")
}

// pumpStream — единственный читатель стрима: перекладывает события в канал,
// пока стрим жив. Завершается на ошибке Recv (в т.ч. на отмене ctx стрима).
func pumpStream(
	ctx context.Context, stream grpc.ServerStreamingClient[lbv1.ResourceLifecycleEvent],
) <-chan *lbv1.ResourceLifecycleEvent {
	ch := make(chan *lbv1.ResourceLifecycleEvent, 16)
	go func() {
		defer close(ch)
		for {
			ev, err := stream.Recv()
			if err != nil {
				return
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// mustRecvEvent — событие из канала pumpStream либо провал теста по дедлайну.
func mustRecvEvent(
	t testing.TB, events <-chan *lbv1.ResourceLifecycleEvent, deadline time.Duration,
) *lbv1.ResourceLifecycleEvent {
	t.Helper()
	select {
	case ev, ok := <-events:
		require.True(t, ok, "стрим закрылся раньше ожидаемого события")
		return ev
	case <-time.After(deadline):
		t.Fatalf("событие не пришло за %v", deadline)
		return nil
	}
}
