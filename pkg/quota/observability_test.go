// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

// observability_test.go — «ни одного удавшегося прохода за всю жизнь» ЗАМЕТНО
// (#666).
//
// Предмет. Накопительные числа тянущего читала ровно одна интеграционная проба;
// `Stats()` не имел вызывающих в прод-коде, метрик у пакета не было ни одной.
// Пока читателя нет, «предел не меняли» неотличимо от «тянущий мёртв» — и
// именно это заставило прочитать нулевой счёт строк как «не доставлено ничего»,
// хотя ноль изменённых строк после догоняющего прохода есть ПРАВИЛЬНЫЙ исход.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSyncer_ObserverSeesEveryOutcome(t *testing.T) {
	const schema = "kacho_test"

	t.Run("удавшийся проход виден стоку — и без единой изменённой строки", func(t *testing.T) {
		rec := NewMemRecorder()
		src := &countingSource{}
		syncer, err := NewSyncer(src, &memProjection{}, Config{}, quietLogger())
		require.NoError(t, err)
		syncer.WithObserver(schema, rec)

		rows, err := syncer.RunOnce(context.Background())
		require.NoError(t, err)
		require.Zero(t, rows)

		pulls, failures, applied, last := rec.Snapshot(schema)
		require.Equal(t, 1, pulls,
			"проход состоялся — и это ОТДЕЛЬНАЯ величина от числа строк: на неизменной "+
				"конфигурации живой тянущий правит ноль строк, поэтому нулевой счёт строк "+
				"сам по себе не говорит ничего, а нулевой счёт проходов говорит")
		require.Zero(t, failures)
		require.Zero(t, applied)
		require.NotZero(t, last, "возраст последнего успеха — то, чем мёртвый тянущий выдаёт себя раньше всего")
	})

	t.Run("отказ виден стоку и НЕ засчитывается проходом", func(t *testing.T) {
		rec := NewMemRecorder()
		src := &countingSource{err: errors.New("сосед не отвечает")}
		syncer, err := NewSyncer(src, &memProjection{}, Config{}, quietLogger())
		require.NoError(t, err)
		syncer.WithObserver(schema, rec)

		_, rerr := syncer.RunOnce(context.Background())
		require.Error(t, rerr)

		pulls, failures, _, last := rec.Snapshot(schema)
		require.Zero(t, pulls, "отказ проходом не является")
		require.Equal(t, 1, failures)
		require.Zero(t, last, "момент последнего УСПЕХА отказом не двигается")
	})

	// Сток не подключён — тянущий работает. Положительный контроль: без него обе
	// пробы выше остались бы зелёными на тянущем, который без стока падает.
	t.Run("без стока тянущий работает", func(t *testing.T) {
		syncer, err := NewSyncer(&countingSource{}, &memProjection{}, Config{}, quietLogger())
		require.NoError(t, err)
		_, rerr := syncer.RunOnce(context.Background())
		require.NoError(t, rerr)
	})
}

// TestSyncer_NeverSucceededIsSaidOutLoud — «ни разу не удалось» пишется ОТДЕЛЬНОЙ
// строкой, отличимой от обычного отказа.
//
// Пока обе беды писались одинаково, неработающий механизм читался как отставший
// снимок: первое означает «величины не доезжают НИКОГДА», второе — «доедут
// следующим тиком».
func TestSyncer_NeverSucceededIsSaidOutLoud(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	src := &countingSource{err: errors.New("сосед не отвечает")}
	syncer, err := NewSyncer(src, &memProjection{},
		Config{Interval: 20 * time.Millisecond, PullTimeout: time.Second}, logger)
	require.NoError(t, err)
	syncer.WithObserver("kacho_test", NewMemRecorder())

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	syncer.Run(ctx)

	require.Contains(t, buf.String(), "has NEVER succeeded",
		"состояние «ни одного удавшегося прохода за всю жизнь» обязано звучать отдельно "+
			"от обычного отказа: первое — неработающий механизм, второе — отставший снимок")

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: после удавшегося прохода эта строка НЕ печатается,
	// иначе она звучала бы всегда и перестала бы что-либо означать.
	var buf2 bytes.Buffer
	logger2 := slog.New(slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelDebug}))
	healthy := &flakySource{failAfter: 1}
	syncer2, err := NewSyncer(healthy, &memProjection{},
		Config{Interval: 20 * time.Millisecond, PullTimeout: time.Second}, logger2)
	require.NoError(t, err)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel2()
	syncer2.Run(ctx2)

	require.NotContains(t, buf2.String(), "has NEVER succeeded",
		"после состоявшегося прохода это уже отставший снимок, а не мёртвый механизм")
	require.True(t, strings.Contains(buf2.String(), "quota limit sync failed"),
		"обычный отказ при этом по-прежнему называется")
}

// flakySource отвечает успехом failAfter раз, затем отказывает.
type flakySource struct {
	failAfter int
	calls     int
}

func (f *flakySource) ListChangedSince(context.Context, string, int32) ([]Change, string, error) {
	f.calls++
	if f.calls > f.failAfter {
		return nil, "", errors.New("сосед перестал отвечать")
	}
	return nil, "", nil
}

// memProjection — проекция в памяти. Ничего не глотает: отсутствие строки —
// законное состояние, а не ошибка, и дублёр это воспроизводит, а не сглаживает.
type memProjection struct {
	cursor    string
	heartbeat int
}

func (p *memProjection) LoadCursor(context.Context) (string, error) { return p.cursor, nil }
func (p *memProjection) ApplyChange(context.Context, Change) (int64, error) {
	return 0, nil
}
func (p *memProjection) SaveCursor(_ context.Context, cursor string, _ int64) error {
	p.cursor = cursor
	return nil
}
func (p *memProjection) Heartbeat(context.Context) error { p.heartbeat++; return nil }
