// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package watcher_test

// revocation_closes_streams_test.go — вторая полоса чтения отзыва (kacho#1022).
//
// Толчок от iam доходит до ОДНОЙ реплики края; остальные узнают об отзыве только
// этим перепросом. Реплика, чей перепрос имена субъектов выбрасывает, закрыть
// поток не может ни при каких условиях — то есть у соседних реплик отзыв
// действия не имеет вовсе.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/watcher"
)

// closerStub — реестр открытых потоков на стенде.
type closerStub struct {
	mu       sync.Mutex
	closed   []string
	sweeps   int
	perClose int
}

func (c *closerStub) CloseSubject(subject string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = append(c.closed, subject)
	return c.perClose
}

func (c *closerStub) CloseAll() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweeps++
	return c.perClose
}

func (c *closerStub) snapshot() ([]string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.closed...), c.sweeps
}

// subjectPoller отдаёт заготовленные порции ИМЕНОВАННЫХ изменений.
type subjectPoller struct {
	mu      sync.Mutex
	batches [][]watcher.SubjectChange
	errs    []error
	calls   int
}

func (p *subjectPoller) PollSubjectChanges(
	_ context.Context, _ int64,
) ([]watcher.SubjectChange, int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := p.calls
	p.calls++
	if i < len(p.errs) && p.errs[i] != nil {
		return nil, 0, p.errs[i]
	}
	var b []watcher.SubjectChange
	if i < len(p.batches) {
		b = p.batches[i]
	}
	var head int64
	for _, c := range b {
		if c.ID > head {
			head = c.ID
		}
	}
	return b, head, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestPolledRevocationClosesExactlyTheNamedSubjects — радиус полосы перепроса.
//
// Положительный контроль и отрицание стоят в одной пробе: названный субъект
// закрыт, неназванный не тронут. Одно отрицание зеленело бы на устройстве,
// которое не закрывает никого.
func TestPolledRevocationClosesExactlyTheNamedSubjects(t *testing.T) {
	p := &subjectPoller{batches: [][]watcher.SubjectChange{
		{{ID: 7, Subject: "user:usr-a"}},
		{{ID: 9, Subject: "user:usr-b"}, {ID: 10, Subject: "service_account:sva-c"}},
	}}
	closer := &closerStub{perClose: 1}
	var flushes int
	w := newWatcher(t, p, func() { flushes++ }, closer)

	w.Tick(context.Background()) // праймящий: курсор принимается, отзывом не является
	if closed, _ := closer.snapshot(); len(closed) != 0 {
		t.Fatalf("праймящий перепрос закрыл потоки %v — принятие курсора отзывом не является", closed)
	}

	w.Tick(context.Background())
	closed, _ := closer.snapshot()
	if len(closed) != 2 || closed[0] != "user:usr-b" || closed[1] != "service_account:sva-c" {
		t.Fatalf("закрыты %v, ожидались ровно названные порцией субъекты", closed)
	}
	if flushes != 1 {
		t.Errorf("сбросов кэша %d, ожидался 1 — вторая полоса не отменяет первую", flushes)
	}
}

// TestPolledRevocationNamesEachSubjectOnce — повтор имени в одной порции не
// умножает закрытий: закрытие идемпотентно, но лишний обход реестра под замком
// стоит ровно столько же, сколько нужный.
func TestPolledRevocationNamesEachSubjectOnce(t *testing.T) {
	p := &subjectPoller{batches: [][]watcher.SubjectChange{
		{{ID: 1, Subject: "user:usr-a"}},
		{
			{ID: 2, Subject: "user:usr-a"},
			{ID: 3, Subject: "user:usr-a"},
			{ID: 4, Subject: ""},
		},
	}}
	closer := &closerStub{}
	w := newWatcher(t, p, func() {}, closer)
	w.Tick(context.Background())
	w.Tick(context.Background())

	closed, _ := closer.snapshot()
	if len(closed) != 1 || closed[0] != "user:usr-a" {
		t.Fatalf("закрыты %v, ожидалось одно закрытие «user:usr-a» (пустое имя отсекается безусловно)", closed)
	}
}

// TestUnreadableRevocationClosesEverything — FAIL-CLOSED.
//
// Неполученный ответ авторитета отзыва НЕ есть «прав не отзывали». Реплика,
// потерявшая читателя дольше объявленного срока, не вправе держать потоки: она
// больше не может утверждать, что чьи-то права целы.
func TestUnreadableRevocationClosesEverything(t *testing.T) {
	down := errors.New("iam недоступен")
	p := &subjectPoller{
		batches: [][]watcher.SubjectChange{{{ID: 1, Subject: "user:usr-a"}}},
		errs:    []error{nil, down, down, down, down},
	}
	closer := &closerStub{perClose: 2}
	clock := &testClock{now: time.Unix(1_700_000_000, 0)}
	w := newWatcherAt(t, p, func() {}, closer, 10*time.Second, clock.Now)

	w.Tick(context.Background()) // праймящий успех: читатель подтверждён
	if _, sweeps := closer.snapshot(); sweeps != 0 {
		t.Fatalf("подтверждённый читатель дал %d сплошных закрытий, ожидался 0", sweeps)
	}

	// Отказ в пределах срока — ещё не утверждение о потере читателя.
	clock.advance(4 * time.Second)
	w.Tick(context.Background())
	if _, sweeps := closer.snapshot(); sweeps != 0 {
		t.Fatalf("отказ внутри объявленного срока дал %d сплошных закрытий, ожидался 0", sweeps)
	}

	// Срок исчерпан — потоки закрываются, и закрываются на КАЖДОМ последующем
	// отказе: клиент, переоткрывшийся в середине аварии, обязан закрыться тоже.
	clock.advance(7 * time.Second)
	w.Tick(context.Background())
	w.Tick(context.Background())
	if _, sweeps := closer.snapshot(); sweeps != 2 {
		t.Fatalf("сплошных закрытий %d, ожидалось 2 — по одному на каждый отказ за сроком", sweeps)
	}

	if closed, _ := closer.snapshot(); len(closed) != 0 {
		t.Errorf("сплошное закрытие тронуло поимённые %v — у него нет имён, оно закрывает всех", closed)
	}
}

// TestRecoveredReaderStopsSweeping — срок отсчитывается от ПОСЛЕДНЕГО удачного
// чтения, а не от старта: восстановившийся читатель снимает fail-closed.
func TestRecoveredReaderStopsSweeping(t *testing.T) {
	down := errors.New("iam недоступен")
	p := &subjectPoller{
		batches: [][]watcher.SubjectChange{{{ID: 1, Subject: "user:usr-a"}}, nil, nil, nil},
		errs:    []error{nil, down, nil, nil},
	}
	closer := &closerStub{perClose: 1}
	clock := &testClock{now: time.Unix(1_700_000_000, 0)}
	w := newWatcherAt(t, p, func() {}, closer, 10*time.Second, clock.Now)

	w.Tick(context.Background()) // праймящий успех
	clock.advance(20 * time.Second)
	w.Tick(context.Background()) // отказ за сроком → сплошное закрытие
	if _, sweeps := closer.snapshot(); sweeps != 1 {
		t.Fatalf("сплошных закрытий %d, ожидалось 1", sweeps)
	}

	w.Tick(context.Background()) // успех: читатель подтверждён заново
	clock.advance(5 * time.Second)
	w.Tick(context.Background()) // отказа нет, срок не исчерпан
	if _, sweeps := closer.snapshot(); sweeps != 1 {
		t.Fatalf("сплошных закрытий %d после восстановления читателя, ожидалось 1", sweeps)
	}
}

// TestWatcherWithoutCloserStillFlushes — закрыватель необязателен, и его
// отсутствие не отменяет первой полосы. Иначе провязка одного механизма
// молча выключала бы другой.
func TestWatcherWithoutCloserStillFlushes(t *testing.T) {
	p := &subjectPoller{batches: [][]watcher.SubjectChange{
		{{ID: 1, Subject: "user:usr-a"}},
		{{ID: 2, Subject: "user:usr-b"}},
	}}
	var flushes int
	w, err := watcher.New(watcher.Config{
		Poller: p, Flush: func() { flushes++ }, Interval: time.Second, Logger: quietLogger(),
	})
	if err != nil {
		t.Fatalf("сборка наблюдателя: %v", err)
	}
	w.Tick(context.Background())
	w.Tick(context.Background())
	if flushes != 1 {
		t.Fatalf("сбросов %d, ожидался 1", flushes)
	}
}

// TestStaleBudgetIsRefusedAtStartup — величина, при которой fail-closed не
// наступает никогда, отвергается на сборке, а не первым отказом в бою.
func TestStaleBudgetIsRefusedAtStartup(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  watcher.Config
	}{
		{
			name: "закрыватель без объявленного срока",
			cfg: watcher.Config{
				Poller: &subjectPoller{}, Flush: func() {}, Interval: time.Second,
				Closer: &closerStub{}, StaleAfter: 0, Logger: quietLogger(),
			},
		},
		{
			name: "срок не превосходит перепроса",
			cfg: watcher.Config{
				Poller: &subjectPoller{}, Flush: func() {}, Interval: 10 * time.Second,
				Closer: &closerStub{}, StaleAfter: 10 * time.Second, Logger: quietLogger(),
			},
		},
		{
			name: "нет источника",
			cfg: watcher.Config{
				Poller: nil, Flush: func() {}, Interval: time.Second, Logger: quietLogger(),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := watcher.New(tc.cfg); err == nil {
				t.Fatal("негодное объявление принято сборкой")
			}
		})
	}
}

// testClock — управляемые часы: срок fail-closed измеряется решением, а не тем,
// сколько успела проработать проба на занятой машине.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newWatcher(
	t *testing.T, p watcher.Poller, flush func(), closer watcher.StreamCloser,
) *watcher.SubjectChangeWatcher {
	t.Helper()
	return newWatcherAt(t, p, flush, closer, 30*time.Second, time.Now)
}

func newWatcherAt(
	t *testing.T, p watcher.Poller, flush func(), closer watcher.StreamCloser,
	staleAfter time.Duration, now func() time.Time,
) *watcher.SubjectChangeWatcher {
	t.Helper()
	w, err := watcher.New(watcher.Config{
		Poller:     p,
		Flush:      flush,
		Interval:   time.Second,
		Closer:     closer,
		StaleAfter: staleAfter,
		Now:        now,
		Logger:     quietLogger(),
	})
	if err != nil {
		t.Fatalf("сборка наблюдателя: %v", err)
	}
	return w
}
