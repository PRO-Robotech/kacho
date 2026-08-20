// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
)

// fakeWatchStream — минимальный computev1.InternalWatchService_WatchServer
// (= grpc.ServerStreamingServer[Event]) для unit/integration-теста streamSince:
// собирает отправленные Event'ы, отдаёт управляемый Context.
type fakeWatchStream struct {
	ctx  context.Context
	sent []*computev1.Event
}

func (s *fakeWatchStream) Send(ev *computev1.Event) error { s.sent = append(s.sent, ev); return nil }
func (s *fakeWatchStream) Context() context.Context       { return s.ctx }
func (s *fakeWatchStream) SetHeader(metadata.MD) error    { return nil }
func (s *fakeWatchStream) SendHeader(metadata.MD) error   { return nil }
func (s *fakeWatchStream) SetTrailer(metadata.MD)         {}
func (s *fakeWatchStream) SendMsg(any) error              { return nil }
func (s *fakeWatchStream) RecvMsg(any) error              { return io.EOF }

// setupWatchDB — собственная база теста на одном контейнере пакета (миграции,
// включая compute_outbox из 0001_initial, уже применены в шаблоне). Возвращает dsn.
func setupWatchDB(t *testing.T) string {
	t.Helper()
	return pgtest.NewDB(t)
}

// TestIntegration_WatchStreamSince_BatchBoundary — cursor корректно продвигается
// через границу catchupBatchSize (250 > 100): все события доставлены строго по
// возрастанию sequence_no, ни одно не потеряно/не задублировано на стыке батчей.
func TestIntegration_WatchStreamSince_BatchBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := ctxWithUser(context.Background(), "usr_alice")
	dsn := setupWatchDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	const total = 250 // > 2× catchupBatchSize (100) → пересекает 2 границы батча
	for i := 0; i < total; i++ {
		_, err := pool.Exec(ctx,
			`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload) VALUES ($1,$2,$3,$4::jsonb)`,
			"Instance", "epi-"+padID(i), "CREATED", `{"i":1}`)
		require.NoError(t, err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		ids = append(ids, "epi-"+padID(i))
	}
	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, allowAllVisibility(ids...))
	fs := &fakeWatchStream{ctx: ctx}
	newCursor, err := h.streamSince(ctx, conn, 0, nil, fs)
	require.NoError(t, err)

	require.Len(t, fs.sent, total, "all outbox rows must be delivered across batch boundaries")
	// strictly ascending, contiguous cursor advancement.
	var prev int64
	for i, ev := range fs.sent {
		require.Greater(t, ev.GetSequenceNo(), prev, "sequence_no must be strictly ascending (idx %d)", i)
		prev = ev.GetSequenceNo()
	}
	assert.Equal(t, prev, newCursor, "returned cursor must equal last delivered sequence_no")
}

// TestIntegration_WatchStreamSince_KindsFilter — `resource_kind = ANY($2)` сужает
// то, что ЧИТАЕТСЯ из журнала.
//
// Прежняя редакция этого теста просила `kinds=["Disk"]` и требовала, чтобы два
// Disk-события были ДОСТАВЛЕНЫ. Так больше нельзя, и не из-за ослабления
// требований: блочное хранение ушло из compute (миграция 0021), у kind'а `Disk`
// не осталось типа объекта в модели прав, а значит про такую строку невозможно
// спросить «вправе ли вызывающий её видеть». Неотвечаемый вопрос обязан
// разрешаться отказом, поэтому строка не доставляется — см.
// TestIntegration_Watch_KindWithNoObjectTypeIsNotDelivered.
//
// Предмет теста — SQL-предикат, и он остаётся проверяемым: запрос про `Disk` не
// должен ПРОЧИТАТЬ Instance-строки, то есть модель про них даже не спрашивается.
// Если предикат сломается, чтение приведёт Instance-идентификаторы, и вопрос о
// них будет задан — тест это увидит. Различение «сузило чтение» и «сузила
// авторизация» — ровно то, чего не давало утверждение о доставке.
func TestIntegration_WatchStreamSince_KindsFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := ctxWithUser(context.Background(), "usr_alice")
	dsn := setupWatchDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	kinds := []string{"Instance", "Disk", "Image", "Instance", "Disk"}
	var instanceIDs []string
	for i, k := range kinds {
		id := "r-" + padID(i)
		_, err := pool.Exec(ctx,
			`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload) VALUES ($1,$2,$3,'{}'::jsonb)`,
			k, id, "CREATED")
		require.NoError(t, err)
		if k == "Instance" {
			instanceIDs = append(instanceIDs, id)
		}
	}

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	// Просим только Disk — kind без типа объекта.
	visDisk, visDiskStub := recordingVisibility(append([]string{"r-001", "r-004"}, instanceIDs...)...)
	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, visDisk)
	fs := &fakeWatchStream{ctx: ctx}
	_, err = h.streamSince(ctx, conn, 0, []string{"Disk"}, fs)
	require.NoError(t, err)

	assert.Empty(t, fs.sent, "kind без типа объекта в модели прав не доставляется")
	assert.Empty(t, visDiskStub.asked,
		"запрос про Disk не должен читать Instance-строки — иначе про них был бы задан вопрос")

	// Просим Instance — предикат обязан привести именно их.
	visInst, visInstStub := recordingVisibility(instanceIDs...)
	h2 := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, visInst)
	fs2 := &fakeWatchStream{ctx: ctx}
	_, err = h2.streamSince(ctx, conn, 0, []string{"Instance"}, fs2)
	require.NoError(t, err)

	require.Len(t, fs2.sent, len(instanceIDs), "предикат обязан привести все Instance-строки")
	for _, ev := range fs2.sent {
		assert.Equal(t, "Instance", ev.GetResourceKind())
	}
	assert.ElementsMatch(t, instanceIDs, visInstStub.asked, "вопрос задаётся ровно про прочитанные строки")
}

// TestIntegration_WatchStreamSince_ResumeFromCursor — from_sequence_no resume:
// события с sequence_no <= cursor пропускаются.
func TestIntegration_WatchStreamSince_ResumeFromCursor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := ctxWithUser(context.Background(), "usr_alice")
	dsn := setupWatchDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	var seqs []int64
	for i := 0; i < 5; i++ {
		var seq int64
		err := pool.QueryRow(ctx,
			`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload) VALUES ('Instance',$1,'CREATED','{}'::jsonb) RETURNING sequence_no`,
			"epi-"+padID(i)).Scan(&seq)
		require.NoError(t, err)
		seqs = append(seqs, seq)
	}

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	resumeIDs := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		resumeIDs = append(resumeIDs, "epi-"+padID(i))
	}
	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, allowAllVisibility(resumeIDs...))
	fs := &fakeWatchStream{ctx: ctx}
	// resume from the 3rd row → expect exactly the last 2 rows.
	_, err = h.streamSince(ctx, conn, seqs[2], nil, fs)
	require.NoError(t, err)

	require.Len(t, fs.sent, 2, "resume must skip rows with sequence_no <= cursor")
	assert.Equal(t, seqs[3], fs.sent[0].GetSequenceNo())
	assert.Equal(t, seqs[4], fs.sent[1].GetSequenceNo())
}

// TestIntegration_WatchStreamSince_BadPayloadFallback — не-object JSONB payload
// (json.Unmarshal→map падает) не роняет stream: событие доставляется с пустым
// Struct-payload (graceful degradation, не drop).
func TestIntegration_WatchStreamSince_BadPayloadFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := ctxWithUser(context.Background(), "usr_alice")
	dsn := setupWatchDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	// '[]'::jsonb — валидный JSONB, но НЕ object → structpb decode fails → fallback.
	_, err = pool.Exec(ctx,
		`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload) VALUES ('Instance','epi-bad','CREATED','[]'::jsonb)`)
	require.NoError(t, err)

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, allowAllVisibility("epi-bad"))
	fs := &fakeWatchStream{ctx: ctx}
	_, err = h.streamSince(ctx, conn, 0, nil, fs)
	require.NoError(t, err)

	require.Len(t, fs.sent, 1, "bad-payload row must still be delivered (fallback), not dropped")
	assert.Empty(t, fs.sent[0].GetPayload().GetFields(), "fallback payload must be an empty Struct")
}

// TestWatch_ResourceExhausted — cap на одновременные stream'ы: когда все слоты
// заняты, Watch возвращает ResourceExhausted (до любого DB-контакта).
//
// Вызывающий здесь НАЗВАН и фильтр сужает — иначе тест не отличал бы «слоты
// заняты» от «отказано в правах»: у отказа тоже нет DB-контакта, и прежняя
// редакция (пустой контекст) прошла бы по отказу, ничего не сказав про предел.
// Порядок проверок при этом обратный: авторизация идёт ПЕРЕД учётом слотов, чтобы
// отвергнутый вызывающий не тратил бюджет параллелизма (см.
// TestWatch_UnauthorizedCallerConsumesNoStreamSlot).
func TestWatch_ResourceExhausted(t *testing.T) {
	h := NewInternalWatchHandler(nil, "", slog.Default(), 1, allowAllVisibility())
	// Занимаем единственный слот — параллельный Watch должен отбиться.
	h.streamSlot <- struct{}{}

	ctx := ctxWithUser(context.Background(), "usr_alice")
	err := h.Watch(&computev1.WatchRequest{}, &fakeWatchStream{ctx: ctx})
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
}

// padID — детерминированный zero-padded суффикс id (для стабильного порядка вставки).
func padID(i int) string {
	const digits = "0123456789"
	b := []byte{digits[(i/100)%10], digits[(i/10)%10], digits[i%10]}
	return string(b)
}

// TestIntegration_WatchStreamSince_ZeroCursorReplaysWhatIsRetained — ЗАМОК НА
// СМЫСЛ НУЛЯ. Курсор исключающий, поэтому `from_sequence_no = 0` отдаёт ВСЁ, что
// ещё лежит в журнале, а не «только новые события».
//
// Проба заведена как RED против КОНТРАКТА: его комментарий обещал
// «0 = начать с current end», и на трёх предсуществующих строках утверждение
// «доставлено ноль» упало с тремя доставленными. Авторитетным признан код —
// документ обязан отражать реальность, а не намерение, — и комментарий контракта
// приведён к нему. Замок держит эту сторону: вернётся «только новые» в прозу —
// проба останется зелёной и соврать ей не даст ничто, поэтому она утверждает
// именно ЧИСЛО доставленного, а не факт вызова.
func TestIntegration_WatchStreamSince_ZeroCursorReplaysWhatIsRetained(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := ctxWithUser(context.Background(), "usr_alice")
	dsn := setupWatchDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		_, err := pool.Exec(ctx,
			`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload) VALUES ('Instance',$1,'CREATED','{}'::jsonb)`,
			"epz-"+padID(i))
		require.NoError(t, err)
		ids = append(ids, "epz-"+padID(i))
	}
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, allowAllVisibility(ids...))
	fs := &fakeWatchStream{ctx: ctx}
	cursor, err := h.streamSince(ctx, conn, 0, nil, fs)
	require.NoError(t, err)

	require.Len(t, fs.sent, 3,
		"нулевой курсор исключающий: он отдаёт всё, что лежит в журнале, а не «только новые»")
	assert.Equal(t, int64(1), fs.sent[0].GetSequenceNo())
	assert.Equal(t, fs.sent[2].GetSequenceNo(), cursor,
		"позиция продвигается до последней прочитанной строки")
}
