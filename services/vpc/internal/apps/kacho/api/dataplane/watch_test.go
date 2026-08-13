// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

func quietObserver() *Observer { return NewObserver(slog.New(slog.DiscardHandler)) }

// fakeReader — журнал намерения в памяти. Строки задаёт проба.
type fakeReader struct {
	mu     sync.Mutex
	bounds Bounds
	rows   []IntentRow
	pages  int
	err    error
}

func (f *fakeReader) Bounds(context.Context) (Bounds, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bounds, f.err
}

func (f *fakeReader) Page(_ context.Context, after, skipWithdrawnUpTo int64, limit int) ([]IntentRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.pages++
	out := make([]IntentRow, 0, limit)
	for _, r := range f.rows {
		if r.Revision <= after {
			continue
		}
		if r.Withdrawn && r.Revision <= skipWithdrawnUpTo {
			continue
		}
		out = append(out, r)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (f *fakeReader) pagesRead() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pages
}

// blockingSink — получатель, который не забирает, пока его не отпустят. Ровно
// тот медленный исполнитель, ради которого написан предел.
type blockingSink struct {
	release chan struct{}
	mu      sync.Mutex
	got     []*vpcv1.WatchIntentResponse
	once    sync.Once
}

func newBlockingSink() *blockingSink { return &blockingSink{release: make(chan struct{})} }

func (s *blockingSink) Send(m *vpcv1.WatchIntentResponse) error {
	<-s.release
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, m)
	return nil
}

func (s *blockingSink) unblock() { s.once.Do(func() { close(s.release) }) }

func (s *blockingSink) messages() []*vpcv1.WatchIntentResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*vpcv1.WatchIntentResponse, len(s.got))
	copy(out, s.got)
	return out
}

// collectSink — получатель, забирающий всё сразу.
type collectSink struct {
	mu   sync.Mutex
	got  []*vpcv1.WatchIntentResponse
	stop func()
	// until — сколько сообщений принять, прежде чем остановить поток.
	until int
}

func (s *collectSink) Send(m *vpcv1.WatchIntentResponse) error {
	s.mu.Lock()
	s.got = append(s.got, m)
	n := len(s.got)
	s.mu.Unlock()
	if s.until > 0 && n >= s.until && s.stop != nil {
		s.stop()
	}
	return nil
}

func (s *collectSink) messages() []*vpcv1.WatchIntentResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*vpcv1.WatchIntentResponse, len(s.got))
	copy(out, s.got)
	return out
}

func liveNetworkRow(rev int64, id string) IntentRow {
	return IntentRow{
		Revision: rev, ResourceID: id, Kind: KindNetwork,
		Network: &kachorepo.NetworkRecord{
			Network:   domain.Network{ID: id, ProjectID: "prj-1", VRFID: uint32(rev + 100)},
			CreatedAt: time.Unix(1700000000, 0).UTC(),
		},
	}
}

func manyRows(n int) []IntentRow {
	out := make([]IntentRow, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, liveNetworkRow(int64(i), "net-"+string(rune('a'+i%26))+itoa(i)))
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func resyncCause(msgs []*vpcv1.WatchIntentResponse) (vpcv1.ResyncCause, bool) {
	for _, m := range msgs {
		if r := m.GetResync(); r != nil {
			return r.GetCause(), true
		}
	}
	return vpcv1.ResyncCause_RESYNC_CAUSE_UNSPECIFIED, false
}

// ── предел незавершённых сообщений (задача 52) ──────────────────────────────

// Медленный получатель НЕ копит у сервера неограниченную очередь, а получает
// указание начать с полной выдачи.
//
// Проба держит получателя заблокированным, пока источник производит заведомо
// больше сообщений, чем помещается в предел. Требуются оба свойства сразу:
// переполнение НАЗВАНО (исход `resync`, а не молчаливая потеря) и объявлено
// наблюдателю (иначе мёртвый предел невидим).
func TestSlowReceiverIsToldToResyncInsteadOfGrowingTheQueue(t *testing.T) {
	obs := quietObserver()
	reader := &fakeReader{
		bounds: Bounds{Horizon: 0, Head: 1000},
		rows:   manyRows(1000),
	}
	uc := NewWatchIntentUseCase(reader, obs)
	// Соотношение то же, что у боевых величин: очередь вмещает две страницы.
	// Взяв предел МЕНЬШЕ страницы, проба доказывала бы срабатывание предела на
	// исправном получателе — то есть подтверждала бы собственную ошибку сборки.
	uc.pageLimit = 16
	uc.pendingLimit = 2 * uc.pageLimit
	uc.poll = time.Millisecond
	// Срок ожидания получателя укорочен, но НЕ снят: снятый превратил бы пробу в
	// утверждение «полная очередь есть переполнение», а это ровно то, что здесь
	// неверно. Получатель в этой пробе не забирает ВООБЩЕ — срок истечёт при любой
	// его величине.
	uc.stall = 50 * time.Millisecond

	sink := newBlockingSink()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- uc.Run(ctx, 0, sink) }()

	// Дать источнику время упереться в предел, затем отпустить получателя.
	require.Eventually(t, func() bool { return obs.Totals().Overflows > 0 }, 3*time.Second, 5*time.Millisecond,
		"предел незавершённых сообщений не сработал: очередь росла молча")
	sink.unblock()

	select {
	case err := <-done:
		require.NoError(t, err, "переполнение — не отказ потока, а указание пересинхронизироваться")
	case <-time.After(3 * time.Second):
		t.Fatal("поток не завершился после переполнения")
	}

	cause, ok := resyncCause(sink.messages())
	require.True(t, ok, "переполнение не названо получателю: потерянное сообщение неотличимо от непришедшего")
	assert.Equal(t, vpcv1.ResyncCause_RESYNC_CAUSE_STREAM_OVERFLOW, cause)
	assert.Positive(t, obs.Totals().Overflows, "переполнение не попало в наблюдаемость")
}

// Положительный контроль к пробе выше: исправный получатель НЕ получает
// указания пересинхронизироваться и НЕ вызывает переполнения.
//
// Без этой половины предыдущая проба зеленела бы на потоке, который сдаётся
// всегда.
func TestFastReceiverGetsTheWholeSnapshotWithoutResync(t *testing.T) {
	obs := quietObserver()
	rows := manyRows(50)
	reader := &fakeReader{bounds: Bounds{Horizon: 0, Head: 50}, rows: rows}
	uc := NewWatchIntentUseCase(reader, obs)
	uc.pageLimit = 16
	uc.pendingLimit = 2 * uc.pageLimit
	uc.poll = time.Millisecond
	// Срок ожидания оставлен боевым: исправный получатель обязан укладываться в
	// него с огромным запасом, и укороченный срок здесь маскировал бы обратное.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sink := &collectSink{until: len(rows) + 1, stop: cancel}

	require.NoError(t, uc.Run(ctx, 0, sink))

	msgs := sink.messages()
	_, hasResync := resyncCause(msgs)
	assert.False(t, hasResync, "исправный получатель получил указание начать сначала")
	assert.Zero(t, obs.Totals().Overflows, "переполнение объявлено там, где его не было")

	var intents int
	var syncedAt int64
	for _, m := range msgs {
		if m.GetIntent() != nil {
			intents++
		}
		if s := m.GetSynced(); s != nil {
			syncedAt = s.GetRevision()
		}
	}
	assert.Equal(t, len(rows), intents, "выдача неполна")
	assert.Equal(t, int64(50), syncedAt, "смыкание выдачи объявлено не на той ревизии")
}

// ── продолжение с известной ревизии (задача 28) ─────────────────────────────

// Ревизия ниже горизонта уплотнения — отдельный исход, а не тишина.
func TestRevisionBelowHorizonIsAnsweredWithResync(t *testing.T) {
	obs := quietObserver()
	reader := &fakeReader{bounds: Bounds{Horizon: 100, Head: 400}, rows: manyRows(400)}
	uc := NewWatchIntentUseCase(reader, obs)
	uc.poll = time.Millisecond

	sink := &collectSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	require.NoError(t, uc.Run(ctx, 42, sink))

	cause, ok := resyncCause(sink.messages())
	require.True(t, ok, "устаревшая позиция осталась без ответа")
	assert.Equal(t, vpcv1.ResyncCause_RESYNC_CAUSE_REVISION_TOO_OLD, cause)
	assert.Zero(t, reader.pagesRead(), "журнал читался, хотя продолжить с этой позиции нельзя")
}

// Ревизия выше головы журнала — тоже отдельный исход.
//
// Это худший из молчаливых случаев: «что изменилось после несуществующей
// позиции» отвечается пустотой ВСЕГДА, поэтому исполнитель ждал бы вечно,
// считая себя в синхроне.
func TestRevisionAboveHeadIsAnsweredWithResync(t *testing.T) {
	obs := quietObserver()
	reader := &fakeReader{bounds: Bounds{Horizon: 0, Head: 10}, rows: manyRows(10)}
	uc := NewWatchIntentUseCase(reader, obs)
	uc.poll = time.Millisecond

	sink := &collectSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	require.NoError(t, uc.Run(ctx, 11, sink))

	cause, ok := resyncCause(sink.messages())
	require.True(t, ok, "позиция из будущего осталась без ответа")
	assert.Equal(t, vpcv1.ResyncCause_RESYNC_CAUSE_REVISION_UNKNOWN, cause)
}

// Положительный контроль к двум пробам выше: законная позиция внутри границ
// продолжает поток, а не пересинхронизирует его.
func TestRevisionInsideBoundsContinuesTheStream(t *testing.T) {
	obs := quietObserver()
	rows := manyRows(10)
	reader := &fakeReader{bounds: Bounds{Horizon: 3, Head: 10}, rows: rows}
	uc := NewWatchIntentUseCase(reader, obs)
	uc.poll = time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// 10 - 6 = 4 намерения плюс смыкание выдачи.
	sink := &collectSink{until: 5, stop: cancel}

	require.NoError(t, uc.Run(ctx, 6, sink))

	msgs := sink.messages()
	_, hasResync := resyncCause(msgs)
	require.False(t, hasResync, "законная позиция получила указание начать сначала")

	var revs []int64
	for _, m := range msgs {
		if in := m.GetIntent(); in != nil {
			revs = append(revs, in.GetRevision())
		}
	}
	assert.Equal(t, []int64{7, 8, 9, 10}, revs, "продолжение отдало не то, что после названной ревизии")
}

// Ревизии в потоке строго возрастают — на этом стоит различение «применил
// устаревшее» и «применил новое».
func TestRevisionsArriveStrictlyIncreasing(t *testing.T) {
	obs := quietObserver()
	rows := manyRows(60)
	reader := &fakeReader{bounds: Bounds{Horizon: 0, Head: 60}, rows: rows}
	uc := NewWatchIntentUseCase(reader, obs)
	uc.pageLimit = 7
	uc.poll = time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sink := &collectSink{until: len(rows) + 1, stop: cancel}

	require.NoError(t, uc.Run(ctx, 0, sink))

	var prev int64
	var seen int
	for _, m := range sink.messages() {
		in := m.GetIntent()
		if in == nil {
			continue
		}
		seen++
		require.Greater(t, in.GetRevision(), prev,
			"ревизия не возросла: %d после %d", in.GetRevision(), prev)
		prev = in.GetRevision()
	}
	assert.Equal(t, len(rows), seen)
}

// Полная выдача (позиция 0) не тащит надгробий, которых исполнитель никогда не
// видел, — но надгробие, появившееся ПОСЛЕ начала выдачи, отдаётся.
//
// Вторая половина несущая: объект мог быть уже отправлен ранней страницей, и
// пропуск его надгробия оставил бы применённое у исполнителя навсегда.
func TestSnapshotSkipsOldTombstonesButDeliversNewOnes(t *testing.T) {
	obs := quietObserver()
	rows := []IntentRow{
		liveNetworkRow(1, "net-live"),
		{Revision: 2, ResourceID: "net-old-gone", Kind: KindNetwork, Withdrawn: true},
		{Revision: 9, ResourceID: "net-just-gone", Kind: KindNetwork, Withdrawn: true},
	}
	reader := &fakeReader{bounds: Bounds{Horizon: 0, Head: 3}, rows: rows}
	uc := NewWatchIntentUseCase(reader, obs)
	uc.poll = time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sink := &collectSink{until: 3, stop: cancel}

	require.NoError(t, uc.Run(ctx, 0, sink))

	var ids []string
	for _, m := range sink.messages() {
		if in := m.GetIntent(); in != nil {
			ids = append(ids, in.GetNetwork().GetNetwork().GetId())
		}
	}
	assert.Contains(t, ids, "net-live")
	assert.NotContains(t, ids, "net-old-gone",
		"надгробие объекта, удалённого до подписки, — шум: исполнитель его никогда не видел")
	assert.Contains(t, ids, "net-just-gone",
		"надгробие, появившееся после начала выдачи, пропущено — применённое осталось бы навсегда")
}

// failingSink — получатель, отвергающий любую отправку.
type failingSink struct{ err error }

func (s *failingSink) Send(*vpcv1.WatchIntentResponse) error { return s.err }

// Истечение срока подписки — ШТАТНЫЙ конец, а не отказ.
//
// Носитель накрывает поток сроком жизни подписки; по его истечении отправка
// перестаёт проходить. Вернув здесь ошибку, мы называли бы отказом собственное
// решение о сроке — и журнал сервиса получал бы запись об ошибке на КАЖДОЕ
// штатное переподключение исполнителя, то есть настоящие отказы утонули бы в
// расписании.
func TestSubscriptionExpiryIsANormalEnd(t *testing.T) {
	obs := quietObserver()
	reader := &fakeReader{bounds: Bounds{Head: 3}, rows: manyRows(3)}
	uc := NewWatchIntentUseCase(reader, obs)
	uc.poll = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, uc.Run(ctx, 0, &failingSink{err: errors.New("transport: the stream is done")}),
		"истечение срока подписки возвращено как отказ потока")
}

// Положительный контроль: отказ получателя при ЖИВОМ контексте — настоящая
// ошибка и доезжает до вызывающего.
//
// Без этой половины предыдущая проба зеленела бы на реализации, которая глотает
// любую ошибку отправки, — то есть на потоке, чьи отказы не видит никто.
func TestSinkFailureOnALiveStreamIsReported(t *testing.T) {
	obs := quietObserver()
	reader := &fakeReader{bounds: Bounds{Head: 3}, rows: manyRows(3)}
	uc := NewWatchIntentUseCase(reader, obs)
	uc.poll = time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	boom := errors.New("получатель отвалился")
	err := uc.Run(ctx, 0, &failingSink{err: boom})
	require.ErrorIs(t, err, boom, "отказ получателя проглочен")
}
