// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

// start_test.go — подъём тянущего величин: ОДИН догоняющий проход, и у него
// есть собственный бюджет (#666).
//
// Предмет. Прежде проходов на подъёме было ДВА: синхронный в `StartLimitSyncer`
// и ещё один — первой итерацией `Run`, до первого тика. Отсюда ровно два отказа
// на подъём с интервалом в десятки миллисекунд, наблюдавшиеся на 12 подъёмах из
// 12. Второй ничего не добавлял: он повторял тот же вопрос тому же соседу через
// миллисекунды после первого, то есть заведомо в том же состоянии сети.
//
// Отдельно: у синхронного прохода не было СВОЕГО срока. Он шёл сырым контекстом
// процесса, поэтому неотвечающий сосед держал бы подъём столько, сколько живёт
// процесс, — при том что у цикла бюджет есть и объявлен (`PullTimeout`).
//
// Пробами это не было покрыто вовсе: `grep -rln StartLimitSyncer --include=*_test.go`
// давал пусто.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// ── дублёры ──────────────────────────────────────────────────────────────────

// stubRow отдаёт курсор, каким его читает проекция.
type stubRow struct{ cursor string }

func (r stubRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("stub row: ожидался ровно один получатель")
	}
	p, ok := dest[0].(*string)
	if !ok {
		return errors.New("stub row: получатель не строка")
	}
	*p = r.cursor
	return nil
}

// stubExecer — носитель проекции. Ничего не хранит: предмет проб этого файла —
// ЧИСЛО и БЮДЖЕТ проходов, а не содержимое снимка.
type stubExecer struct{}

func (stubExecer) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (stubExecer) QueryRow(context.Context, string, ...any) pgx.Row { return stubRow{} }

// countingSource считает проходы и запоминает, нёс ли контекст каждого срок.
type countingSource struct {
	mu        sync.Mutex
	calls     int
	deadlines []bool
	err       error
}

func (s *countingSource) ListChangedSince(ctx context.Context, _ string, _ int32) ([]Change, string, error) {
	s.mu.Lock()
	s.calls++
	_, hasDeadline := ctx.Deadline()
	s.deadlines = append(s.deadlines, hasDeadline)
	s.mu.Unlock()
	if s.err != nil {
		return nil, "", s.err
	}
	return nil, "", nil
}

func (s *countingSource) snapshot() (int, []bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, append([]bool(nil), s.deadlines...)
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ── пробы ────────────────────────────────────────────────────────────────────

// TestStartLimitSyncer_OneCatchUpPassOnStartup — подъём делает РОВНО ОДИН
// догоняющий проход.
//
// Интервал взят заведомо больше окна наблюдения, поэтому всякий второй проход в
// этом окне — тот самый дубль, а не законный тик.
func TestStartLimitSyncer_OneCatchUpPassOnStartup(t *testing.T) {
	src := &countingSource{}

	stop, err := StartLimitSyncer(context.Background(), stubExecer{}, src, "kacho_test",
		Config{Interval: time.Hour, PullTimeout: 5 * time.Second}, quietLogger())
	require.NoError(t, err)
	defer stop()

	// Цикл поднимается горутиной; даём ему дойти до ожидания тика.
	require.Eventually(t, func() bool { n, _ := src.snapshot(); return n >= 1 },
		2*time.Second, 5*time.Millisecond, "догоняющий проход не состоялся вовсе")
	time.Sleep(150 * time.Millisecond)

	calls, _ := src.snapshot()
	require.Equal(t, 1, calls,
		"на подъёме обязан быть РОВНО ОДИН догоняющий проход: второй повторяет тот же "+
			"вопрос тому же соседу через миллисекунды после первого, то есть заведомо в том же "+
			"состоянии сети, и даёт второй отказ, за которым ничего не стоит")
}

// TestStartLimitSyncer_CatchUpPassCarriesItsOwnBudget — у догоняющего прохода
// есть собственный срок.
//
// Без него неотвечающий сосед держит подъём столько, сколько живёт процесс, при
// том что у цикла бюджет объявлен и применяется.
func TestStartLimitSyncer_CatchUpPassCarriesItsOwnBudget(t *testing.T) {
	src := &countingSource{}

	stop, err := StartLimitSyncer(context.Background(), stubExecer{}, src, "kacho_test",
		Config{Interval: time.Hour, PullTimeout: 250 * time.Millisecond}, quietLogger())
	require.NoError(t, err)
	defer stop()

	require.Eventually(t, func() bool { n, _ := src.snapshot(); return n >= 1 },
		2*time.Second, 5*time.Millisecond)

	_, deadlines := src.snapshot()
	require.NotEmpty(t, deadlines)
	require.True(t, deadlines[0],
		"догоняющий проход обязан нести свой срок: сырой контекст процесса означает, "+
			"что неотвечающий сосед держит подъём до конца жизни процесса")
}

// TestStartLimitSyncer_FailingCatchUpDoesNotAbortStartup — отказ догоняющего
// прохода НЕ фатален.
//
// Положительный контроль к двум пробам выше: они утверждают «не больше одного» и
// «со сроком», и обе остались бы зелёными, если бы проход перестал происходить
// вовсе либо подъём начал падать. Величина — не путь запроса: недоступность
// соседа означает, что снимок постоит со старой величиной, а предел продолжает
// действовать — место занимает триггер в writer-транзакции.
func TestStartLimitSyncer_FailingCatchUpDoesNotAbortStartup(t *testing.T) {
	src := &countingSource{err: errors.New("сосед ещё не поднялся")}

	stop, err := StartLimitSyncer(context.Background(), stubExecer{}, src, "kacho_test",
		Config{Interval: time.Hour, PullTimeout: time.Second}, quietLogger())
	require.NoError(t, err, "отказ догоняющего прохода не роняет сервис")
	defer stop()

	require.Eventually(t, func() bool { n, _ := src.snapshot(); return n == 1 },
		2*time.Second, 5*time.Millisecond, "проход обязан состояться и при отказе соседа")
	time.Sleep(150 * time.Millisecond)
	calls, _ := src.snapshot()
	require.Equal(t, 1, calls, "отказ не повторяется мгновенно — повтор ждёт тика")
}

// TestStartLimitSyncer_StopIsIdempotentAndPrompt — останов не виснет.
func TestStartLimitSyncer_StopIsIdempotentAndPrompt(t *testing.T) {
	src := &countingSource{}
	stop, err := StartLimitSyncer(context.Background(), stubExecer{}, src, "kacho_test",
		Config{Interval: time.Hour}, quietLogger())
	require.NoError(t, err)

	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("останов не завершился: цикл не отпускает контекст")
	}
}
