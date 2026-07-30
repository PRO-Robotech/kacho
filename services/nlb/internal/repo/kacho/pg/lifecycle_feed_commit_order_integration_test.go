// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

// =============================================================================
// Курсор lifecycle-фида при ПАРАЛЛЕЛЬНЫХ писателях `nlb_outbox`.
// =============================================================================
//
// `sequence_no` выдаётся nextval'ом на INSERT, а строка становится ВИДИМОЙ на
// COMMIT — значит порядок номеров и порядок коммитов независимы. Подписчик
// (internal_lifecycle/handler.go) двигает курсор на sequence_no последнего
// ОТДАННОГО события и перечитывает только «больше курсора», поэтому строка,
// закоммиченная ПОСЛЕ строки с бо́льшим номером, обязана быть отдана до того,
// как курсор ушёл за неё. Иначе событие теряется молча и его не воспроизводит
// ни переподключение по resume_from_event_id, ни 30-секундный перепрос.
//
// Три сценария (каждый — свой feed-conn, т.е. свой водяной знак):
//  1. инверсный порядок коммитов: A взял номер раньше, коммитится позже;
//  2. писатель откатился: номер потерян навсегда — фид обязан пойти дальше,
//     а не залипнуть на дыре;
//  3. писатель ЕЩЁ БЕЗ xid (номер уже выдан, heap_insert не начат) — горизонт
//     по снимку транзакций такого писателя не видит вовсе.

// lifecycleTestBatch — размер батча, с которым подписчик вычерпывает фид
// (handler.catchupBatchSize). Батч меньше него = end-of-data.
const lifecycleTestBatch = 100

// lifecycleGateLockID — advisory-lock, на котором тест-триггер держит писателя
// в состоянии «номер выдан, строки ещё нет» (сценарий 3).
const lifecycleGateLockID = 4242

func TestLifecycleFeed_ConcurrentWriters(t *testing.T) {
	dsn := setupTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	admin := mustConnectPG(t, ctx, dsn)

	// ---------------------------------------------------------------------
	// 1. Инверсный порядок коммитов относительно номеров.
	// ---------------------------------------------------------------------
	t.Run("inverse commit order delivers both events", func(t *testing.T) {
		feed := mustOpenFeed(t, ctx, dsn)
		cursor := maxOutboxSeq(t, ctx, admin)

		// A берёт номер первым и остаётся в полёте.
		connA := mustConnectPG(t, ctx, dsn)
		txA, err := connA.Begin(ctx)
		require.NoError(t, err)
		seqA := insertOutboxTx(t, ctx, txA, "nlb-A")

		// B берёт СЛЕДУЮЩИЙ номер и коммитится ПЕРВЫМ.
		connB := mustConnectPG(t, ctx, dsn)
		txB, err := connB.Begin(ctx)
		require.NoError(t, err)
		seqB := insertOutboxTx(t, ctx, txB, "nlb-B")
		require.NoError(t, txB.Commit(ctx))
		require.Greater(t, seqB, seqA, "B обязан нести больший номер, иначе сценарий не тот")

		// Подписчик читает, пока A в полёте: строки seqA ещё не видно.
		cursor, first := drainFeed(t, ctx, feed, cursor)

		// A коммитится — в проде это будит подписчика своим NOTIFY.
		require.NoError(t, txA.Commit(ctx))

		_, second := drainFeed(t, ctx, feed, cursor)

		require.Equal(t, []int64{seqA, seqB}, append(first, second...),
			"подписчик обязан получить ОБА события в порядке номеров; "+
				"пропуск seqA=%d = молча потерянное событие жизненного цикла", seqA)
	})

	// ---------------------------------------------------------------------
	// 2. Откатившийся писатель: номер потерян навсегда, фид не залипает.
	// ---------------------------------------------------------------------
	t.Run("aborted writer does not wedge the feed", func(t *testing.T) {
		feed := mustOpenFeed(t, ctx, dsn)
		cursor := maxOutboxSeq(t, ctx, admin)

		connA := mustConnectPG(t, ctx, dsn)
		txA, err := connA.Begin(ctx)
		require.NoError(t, err)
		seqA := insertOutboxTx(t, ctx, txA, "nlb-ROLLBACK")

		connB := mustConnectPG(t, ctx, dsn)
		txB, err := connB.Begin(ctx)
		require.NoError(t, err)
		seqB := insertOutboxTx(t, ctx, txB, "nlb-AFTER-ROLLBACK")
		require.NoError(t, txB.Commit(ctx))

		cursor, first := drainFeed(t, ctx, feed, cursor)

		// A откатывается: номера seqA не будет никогда.
		require.NoError(t, txA.Rollback(ctx))

		_, second := drainFeed(t, ctx, feed, cursor)

		got := append(first, second...)
		require.Equal(t, []int64{seqB}, got,
			"дыра от отката обязана перестать держать фид (ожидали только seqB=%d)", seqB)
		require.NotContains(t, got, seqA, "откатившийся номер не существует")
	})

	// ---------------------------------------------------------------------
	// 3. Писатель, который УЖЕ взял номер, но ещё не получил xid.
	// ---------------------------------------------------------------------
	// Postgres выдаёт DEFAULT nextval при формировании кортежа — ДО BEFORE-INSERT
	// триггера и ДО heap_insert, который и назначает xid. Значит существует
	// состояние «номер выдан, транзакция без xid»; горизонт, построенный на
	// снимке транзакций (pg_current_snapshot/xmin), такого писателя не видит и
	// разрешает уйти за его номер. Тест удерживает писателя ровно в этом
	// состоянии через advisory-lock в тест-триггере.
	t.Run("xid-less in-flight writer is not skipped", func(t *testing.T) {
		installOutboxGateTrigger(t, ctx, admin)

		gate := mustConnectPG(t, ctx, dsn)
		_, err := gate.Exec(ctx, "SELECT pg_advisory_lock($1)", lifecycleGateLockID)
		require.NoError(t, err)
		gateReleased := false
		releaseGate := func() {
			if gateReleased {
				return
			}
			gateReleased = true
			_, uerr := gate.Exec(ctx, "SELECT pg_advisory_unlock($1)", lifecycleGateLockID)
			require.NoError(t, uerr)
		}
		defer releaseGate()

		feed := mustOpenFeed(t, ctx, dsn)
		cursor := maxOutboxSeq(t, ctx, admin)

		// A уходит в INSERT и застревает в триггере: номер уже выдан.
		connA := mustConnectPG(t, ctx, dsn)
		type insertResult struct {
			seq int64
			err error
		}
		done := make(chan insertResult, 1)
		go func() {
			tx, berr := connA.Begin(ctx)
			if berr != nil {
				done <- insertResult{err: berr}
				return
			}
			var seq int64
			qerr := tx.QueryRow(ctx, outboxInsertSQL, "nlb-GATE").Scan(&seq)
			if qerr != nil {
				_ = tx.Rollback(ctx)
				done <- insertResult{err: qerr}
				return
			}
			done <- insertResult{seq: seq, err: tx.Commit(ctx)}
		}()

		// Барьер на НАБЛЮДАЕМОМ состоянии, не на паузе: писатель уже держит
		// RowExclusiveLock на nlb_outbox и ещё НЕ имеет xid.
		require.Eventually(t, func() bool {
			var lockHeld, xidHeld bool
			qerr := admin.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM pg_locks
				                WHERE relation = 'kacho_nlb.nlb_outbox'::regclass
				                  AND mode = 'RowExclusiveLock' AND granted
				                  AND pid <> pg_backend_pid()),
				       EXISTS (SELECT 1 FROM pg_locks x
				                JOIN pg_locks r ON r.pid = x.pid
				               WHERE x.locktype = 'transactionid' AND x.granted
				                 AND r.relation = 'kacho_nlb.nlb_outbox'::regclass
				                 AND r.mode = 'RowExclusiveLock' AND r.granted)
			`).Scan(&lockHeld, &xidHeld)
			return qerr == nil && lockHeld && !xidHeld
		}, 20*time.Second, 20*time.Millisecond,
			"писатель обязан застрять с выданным номером, без xid — иначе сценарий не воспроизведён")

		// B берёт больший номер и коммитится, пока A висит без xid.
		connB := mustConnectPG(t, ctx, dsn)
		txB, err := connB.Begin(ctx)
		require.NoError(t, err)
		seqB := insertOutboxTx(t, ctx, txB, "nlb-AFTER-GATE")
		require.NoError(t, txB.Commit(ctx))

		cursor, first := drainFeed(t, ctx, feed, cursor)

		releaseGate()
		res := <-done
		require.NoError(t, res.err)
		require.Greater(t, seqB, res.seq, "A обязан нести меньший номер")

		_, second := drainFeed(t, ctx, feed, cursor)

		require.Equal(t, []int64{res.seq, seqB}, append(first, second...),
			"событие писателя без xid (seq=%d) обязано быть отдано, а не пропущено", res.seq)
	})
}

// =============================================================================
// helpers
// =============================================================================

// outboxInsertSQL — INSERT одной lifecycle-строки; повторяет форму
// pg.outboxEmitter.Emit, но возвращает выданный номер.
const outboxInsertSQL = `
	INSERT INTO kacho_nlb.nlb_outbox (resource_type, resource_id, project_id, action, payload)
	VALUES ('nlb_load_balancer', $1, 'prj-CONC', 'CREATED', '{}'::jsonb)
	RETURNING sequence_no`

func mustConnectPG(t testing.TB, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	})
	return conn
}

func mustOpenFeed(t testing.TB, ctx context.Context, dsn string) kacho.LifecycleConn {
	t.Helper()
	conn, err := kachopg.NewLifecycleFeed(dsn).Open(ctx)
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	return conn
}

// insertOutboxTx добавляет lifecycle-строку в уже открытой TX и возвращает
// выданный ей sequence_no (строка станет видимой только на COMMIT).
func insertOutboxTx(t testing.TB, ctx context.Context, tx pgx.Tx, resourceID string) int64 {
	t.Helper()
	var seq int64
	require.NoError(t, tx.QueryRow(ctx, outboxInsertSQL, resourceID).Scan(&seq))
	return seq
}

func maxOutboxSeq(t testing.TB, ctx context.Context, conn *pgx.Conn) int64 {
	t.Helper()
	var seq int64
	require.NoError(t, conn.QueryRow(ctx,
		`SELECT COALESCE(max(sequence_no), 0) FROM kacho_nlb.nlb_outbox`).Scan(&seq))
	return seq
}

// drainFeed повторяет курсорную семантику подписчика (handler.streamSince):
// читает батчами, двигая курсор на sequence_no ПОСЛЕДНЕГО отданного события,
// и останавливается на неполном батче. Возвращает курсор и отданные номера.
func drainFeed(t testing.TB, ctx context.Context, conn kacho.LifecycleConn, cursor int64) (int64, []int64) {
	t.Helper()
	var got []int64
	for {
		events, err := conn.EventsSince(ctx, cursor, nil, lifecycleTestBatch)
		require.NoError(t, err)
		for i := range events {
			got = append(got, events[i].SequenceNo)
			cursor = events[i].SequenceNo
		}
		if len(events) < lifecycleTestBatch {
			return cursor, got
		}
	}
}

// installOutboxGateTrigger ставит ТЕСТОВЫЙ BEFORE INSERT триггер на nlb_outbox:
// строка с resource_id='nlb-GATE' виснет на advisory-lock ПОСЛЕ того, как
// DEFAULT nextval уже выдал ей номер, и ДО heap_insert (то есть до назначения
// xid). Остальные строки он пропускает без задержки.
func installOutboxGateTrigger(t testing.TB, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	_, err := admin.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION kacho_nlb.test_outbox_gate() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.resource_id = 'nlb-GATE' THEN
				PERFORM pg_advisory_xact_lock(%d);
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER test_outbox_gate_trg BEFORE INSERT ON kacho_nlb.nlb_outbox
			FOR EACH ROW EXECUTE FUNCTION kacho_nlb.test_outbox_gate();`, lifecycleGateLockID))
	require.NoError(t, err)
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = admin.Exec(dropCtx, `
			DROP TRIGGER IF EXISTS test_outbox_gate_trg ON kacho_nlb.nlb_outbox;
			DROP FUNCTION IF EXISTS kacho_nlb.test_outbox_gate();`)
	})
}
