// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// soft_open_is_observable_test.go — мягкий проход обязан быть ВИДЕН и обязан
// ОТЛИЧАТЬ настройку от сбоя.
//
// Проход, который на отказе соседа отдаёт страницу целиком, защитим ровно пока отказ
// действительно временный. Если он не различает «сосед сейчас не отвечает» и «мы
// стучимся не туда» (нет такого метода, не приняты учётные данные, ответ не той
// формы), то постоянная неправильная настройка НАВСЕГДА становится штатным режимом:
// фильтр включён, провязан, исполняется на каждом запросе — и не сузил ни одной
// страницы за всё время жизни.
//
// Отсюда три требования, и каждое проверяется отдельно: ответ, доказывающий неверную
// настройку, — громко; временный отказ может сохранять задокументированный мягкий
// проход, но обязан нести счётчик; «ноль проходов за всю жизнь» обязано быть
// отличимо от «счётчика нет».
//
// Парный положительный исход присутствует у каждого отрицательного: без него
// «счётчик вырос» неотличимо от «счётчик растёт всегда», а «страница прошла»
// неотличимо от «фильтр не работает вовсе».
package authzfilter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// logSink — перехват записей фильтра с уровнем, чтобы «громко» проверялось как
// уровень записи, а не как наличие слова в тексте.
type logSink struct{ buf bytes.Buffer }

func newLogSink() *logSink { return &logSink{} }

func (s *logSink) logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&s.buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// records — разобранные записи.
func (s *logSink) records(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		out = append(out, rec)
	}
	return out
}

// levelsOf — уровни всех записей (для утверждения «громко» / «тихо»).
func (s *logSink) levelsOf(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, rec := range s.records(t) {
		lvl, _ := rec["level"].(string)
		out = append(out, lvl)
	}
	return out
}

func failOpenFilter(cli AuthorizeClient, sink *logSink) *FGAFilter {
	cfg := DefaultConfig()
	cfg.FailOpen = true
	return NewFGAFilter(cli, cfg).WithLogger(sink.logger())
}

// TestOpenPass_TransientPeerFailure_IsWarnedAndCounted — временный отказ соседа
// сохраняет задокументированный мягкий проход, но перестаёт быть невидимым.
func TestOpenPass_TransientPeerFailure_IsWarnedAndCounted(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"peer_unavailable", status.Error(codes.Unavailable, "iam is down")},
		{"peer_deadline", status.Error(codes.DeadlineExceeded, "too slow")},
		// Голый context.DeadlineExceeded — НЕ gRPC-статус. Он обязан остаться
		// временным: иначе истёкший бюджет операции читался бы как доказательство
		// неверной настройки и поднимал ложную тревогу на здоровом, но медленном стенде.
		{"bare_context_deadline", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cli := newFakeAuthorizeClient()
			cli.err = tc.err
			sink := newLogSink()
			f := failOpenFilter(cli, sink)

			got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
				[]string{"a", "b"})
			require.NoError(t, err)
			assert.Equal(t, []string{"a", "b"}, got, "мягкий проход остаётся мягким")

			misconfigured, transient := f.OpenPassCounts()
			assert.Equal(t, uint64(1), transient, "временный проход обязан быть посчитан")
			assert.Equal(t, uint64(0), misconfigured, "временный отказ — не доказательство неверной настройки")
			assert.Contains(t, sink.levelsOf(t), "WARN", "временный отказ называется предупреждением")
			assert.NotContains(t, sink.levelsOf(t), "ERROR", "и не тревогой")
		})
	}
}

// TestOpenPass_PeerProvesMisconfiguration_IsLoudAndCounted — ответ, доказывающий,
// что по адресу не тот эндпоинт (или что нас там не принимают), — это НАСТРОЙКА, а
// не сбой: она не «пройдёт сама» и не может оставаться тихой.
func TestOpenPass_PeerProvesMisconfiguration_IsLoudAndCounted(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"no_such_method", status.Error(codes.Unimplemented, "unknown service iam.v1.AuthorizeService")},
		{"credentials_not_accepted", status.Error(codes.Unauthenticated, "missing client certificate")},
		{"caller_not_granted", status.Error(codes.PermissionDenied, "not allowed to ask")},
		{"request_shape_refused", status.Error(codes.InvalidArgument, "unknown field")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cli := newFakeAuthorizeClient()
			cli.err = tc.err
			sink := newLogSink()
			f := failOpenFilter(cli, sink)

			got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
				[]string{"a", "b"})
			require.NoError(t, err)
			assert.Equal(t, []string{"a", "b"}, got)

			misconfigured, transient := f.OpenPassCounts()
			assert.Equal(t, uint64(1), misconfigured, "неверная настройка обязана быть посчитана отдельно")
			assert.Equal(t, uint64(0), transient)
			assert.Contains(t, sink.levelsOf(t), "ERROR",
				"настройка, из-за которой фильтр не решает НИЧЕГО, не бывает предупреждением")
		})
	}
}

// TestOpenPass_ContractSkew_IsLoudAndCounted — ответ не той формы (вердиктов не
// столько, сколько вопросов) приходит не от транспорта, а от нашего собственного
// разбора: сосед отвечает по другому контракту. Это тоже настройка.
func TestOpenPass_ContractSkew_IsLoudAndCounted(t *testing.T) {
	sink := newLogSink()
	f := failOpenFilter(shortResponseClient{}, sink)

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got)

	misconfigured, transient := f.OpenPassCounts()
	assert.Equal(t, uint64(1), misconfigured)
	assert.Equal(t, uint64(0), transient)
	assert.Contains(t, sink.levelsOf(t), "ERROR")
}

// TestHealthyPage_LeavesOpenPassCountersAtZero — парный ПОЛОЖИТЕЛЬНЫЙ: на здоровом
// пути страница действительно сужается, и оба счётчика читаются нулями.
//
// Это и есть требование «ноль проходов за всю жизнь обязано быть заметно»: ноль
// здесь — прочитанное значение, а не отсутствие записи в логе, которое неотличимо
// от отсутствия самого счётчика.
func TestHealthyPage_LeavesOpenPassCountersAtZero(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("viewer", "a")
	sink := newLogSink()
	f := failOpenFilter(cli, sink)

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, got, "здоровый путь действительно сужает страницу")

	misconfigured, transient := f.OpenPassCounts()
	assert.Equal(t, uint64(0), misconfigured)
	assert.Equal(t, uint64(0), transient)
	assert.Empty(t, sink.levelsOf(t), "здоровый путь молчит")
}

// TestFailClosed_IsNotAnOpenPass — парный отрицательный к счётчикам: при
// fail-closed страница не отдаётся вовсе, и мягких проходов не засчитывается.
// Без него «счётчик равен нулю» неотличимо от «счётчик не инкрементируется никогда».
func TestFailClosed_IsNotAnOpenPass(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = errors.New("boom")
	sink := newLogSink()
	f := NewFGAFilter(cli, DefaultConfig()).WithLogger(sink.logger()) // FailOpen=false

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeInstance, ActionInstanceRead,
		[]string{"a"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code())

	misconfigured, transient := f.OpenPassCounts()
	assert.Equal(t, uint64(0), misconfigured, "отказ — не проход")
	assert.Equal(t, uint64(0), transient)
}
