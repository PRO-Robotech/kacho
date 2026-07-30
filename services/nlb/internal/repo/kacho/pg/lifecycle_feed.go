// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// lifecycleListenChannel — Postgres NOTIFY-канал, на который шлёт trigger
// `nlb_outbox_notify_trg` после INSERT в `nlb_outbox` (миграция 0001).
const lifecycleListenChannel = "nlb_outbox"

// lifecycleConnectTimeout — защита от self-DoS: медленный Connect под нагрузкой
// не должен надолго удерживать stream-слот.
const lifecycleConnectTimeout = 2 * time.Second

// lifecycleStallWarnAfter — с какого возраста удержанного горизонта (см.
// advanceWatermark) фид жалуется в лог. Горизонт держит ЧУЖАЯ незавершённая
// транзакция; дольше одного idle-перепроса подписчика (30s) это означает
// зависшего писателя `nlb_outbox`, а не нормальную конкуренцию. Без этой
// жалобы «фид молчит, потому что кто-то не коммитится» неотличимо от «событий
// нет» — ровно тот немой отказ, который мы ловим в дренажах.
const lifecycleStallWarnAfter = 30 * time.Second

// lifecycleWatermarkSQL — наблюдение горизонта фида одним снимком:
//
//	(1) максимальный ВИДИМЫЙ sequence_no;
//	(2) множество транзакций, которые прямо сейчас пишут в `nlb_outbox`.
//
// Почему именно блокировка таблицы, а не снимок транзакций (pg_current_snapshot/
// xmin/xmax): писатель держит `RowExclusiveLock` на `nlb_outbox` с момента
// планирования своего INSERT — то есть ЕЩЁ ДО того, как DEFAULT nextval выдал
// ему sequence_no, — и до конца транзакции. А xid назначается только на
// heap_insert, ПОСЛЕ nextval и после BEFORE-триггеров: существует наблюдаемое
// состояние «номер уже выдан, транзакции с xid ещё нет», в котором горизонт по
// xid слеп (проверено тестом `xid-less in-flight writer is not skipped`).
// Дополнительно: `pg_snapshot_xmax` НЕ является верхней границей работающих
// xid (это latestCompletedXid+1), поэтому «xmin догнал прежний xmax» ничего не
// доказывает.
//
// `virtualtransaction` (`backendID/localXID`) идентифицирует ТРАНЗАКЦИЮ, а не
// соединение: тот же backend в следующей транзакции получит другой
// идентификатор. Поэтому непрерывно пишущие фоновые воркеры не могут удерживать
// горизонт вечно — зафиксированное множество всегда доистекает.
//
// `pg_locks` читается любой ролью целиком (в отличие от `pg_stat_activity`,
// который прячет чужие backend'ы), поэтому наблюдение не зависит от того, под
// какой ролью пишет сосед. Данные узловые: фид обязан быть подключён к тому же
// узлу, что и писатели — на реплике это и так не работает (NOTIFY не
// реплицируется).
const lifecycleWatermarkSQL = `
	SELECT COALESCE((SELECT max(sequence_no) FROM kacho_nlb.nlb_outbox), 0),
	       COALESCE((SELECT array_agg(DISTINCT l.virtualtransaction)
	                   FROM pg_locks l
	                  WHERE l.relation = 'kacho_nlb.nlb_outbox'::regclass
	                    AND l.mode = 'RowExclusiveLock'
	                    AND l.granted
	                    AND l.pid <> pg_backend_pid()
	                    AND l.database = (SELECT oid FROM pg_database
	                                       WHERE datname = current_database())),
	                '{}'::text[])`

// LifecycleFeed — pgx-реализация kacho.LifecycleFeed поверх dedicated-conn'ов
// (вне pgxpool — LISTEN требует выделенной сессии).
type LifecycleFeed struct {
	dsn string
}

// NewLifecycleFeed — конструктор. dsn — connection string для dedicated
// LISTEN-conn'ов (composition root передаёт cfg.Repository.Postgres.URL).
func NewLifecycleFeed(dsn string) *LifecycleFeed {
	return &LifecycleFeed{dsn: dsn}
}

// Open поднимает dedicated pgx.Conn под connect-timeout и выполняет LISTEN.
// Ошибка Connect/LISTEN отдаётся как есть — app-слой маппит её в Unavailable
// без leak'а pgx-текста (db host / port / sslmode).
func (f *LifecycleFeed) Open(ctx context.Context) (kacho.LifecycleConn, error) {
	connectCtx, cancel := context.WithTimeout(ctx, lifecycleConnectTimeout)
	conn, err := pgx.Connect(connectCtx, f.dsn)
	cancel()
	if err != nil {
		return nil, err
	}
	// Идентификатор канала — literal, не из user-input (защита от SQL-injection).
	// Тот же bounded-таймаут, что и на Connect: LISTEN на half-open/перегруженной
	// сессии не должен вечно держать stream-слот + dedicated conn (self-DoS).
	listenCtx, listenCancel := context.WithTimeout(ctx, lifecycleConnectTimeout)
	_, err = conn.Exec(listenCtx, "LISTEN "+lifecycleListenChannel)
	listenCancel()
	if err != nil {
		closeCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		_ = conn.Close(closeCtx)
		c()
		return nil, err
	}
	return &lifecycleConn{conn: conn, log: slog.Default()}, nil
}

// lifecycleConn — dedicated LISTEN-сессия одного Subscribe-стрима вместе с
// водяным знаком этого стрима (см. advanceWatermark). Состояние принадлежит
// одной сессии и читается одной горутиной — как и объявляет порт
// kacho.LifecycleConn.
type lifecycleConn struct {
	conn *pgx.Conn
	log  *slog.Logger

	// watermark — граница УСТОЯВШЕГОСЯ: каждый номер ≤ watermark либо уже
	// видим, либо не появится никогда (писатель откатился). Только до этой
	// границы можно отдавать события и двигать курсор.
	watermark int64

	// pendingSeq / pendingWriters — наблюдение, ожидающее подтверждения:
	// номер pendingSeq станет границей, когда ни одна из транзакций
	// pendingWriters больше не пишет в `nlb_outbox`. Множество фиксируется
	// ОДИН раз и не пополняется новыми писателями — иначе под непрерывной
	// записью оно никогда бы не опустело.
	pendingSeq     int64
	pendingWriters []string
	pendingSince   time.Time

	// lastStallWarn — троттлинг жалобы на удержанный горизонт.
	lastStallWarn time.Time
}

// EventsSince читает строки `nlb_outbox` в окне (cursor, watermark] — то есть
// только УСТОЯВШИЕСЯ события (опц. фильтр по resource_type) батчем до limit.
// LIMIT — literal int (не user-input).
//
// Почему окно, а не просто «> cursor»: `sequence_no` выдаётся nextval'ом на
// INSERT, а строка становится видимой на COMMIT, поэтому порядок номеров и
// порядок коммитов независимы. Подписчик двигает курсор на номер последнего
// ОТДАННОГО события, значит отдать номер, за которым ещё может появиться
// меньший, — это потерять меньший навсегда: перечитывание идёт строго «больше
// курсора», а resume_from_event_id воспроизводит ту же дыру. Водяной знак
// (advanceWatermark) гарантирует обратное: к моменту отдачи события все меньшие
// номера уже определены — видимы или потеряны откатом.
func (c *lifecycleConn) EventsSince(
	ctx context.Context, cursor int64, kinds []string, limit int,
) ([]kacho.OutboxEvent, error) {
	if err := c.advanceWatermark(ctx); err != nil {
		return nil, err
	}
	if c.watermark <= cursor {
		// Устоявшегося за курсором нет: либо событий нет вовсе, либо они
		// удерживаются незавершённым писателем (жалоба — в advanceWatermark).
		return nil, nil
	}

	args := []any{cursor, c.watermark}
	var kindFilter string
	if len(kinds) > 0 {
		kindFilter = " AND resource_type = ANY($3)"
		args = append(args, kinds)
	}
	q := fmt.Sprintf(`
		SELECT sequence_no, resource_type, resource_id, project_id, action, payload, emitted_at
		FROM kacho_nlb.nlb_outbox
		WHERE sequence_no > $1 AND sequence_no <= $2%s
		ORDER BY sequence_no ASC
		LIMIT %d
	`, kindFilter, limit)

	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []kacho.OutboxEvent
	for rows.Next() {
		var ev kacho.OutboxEvent
		if err := rows.Scan(&ev.SequenceNo, &ev.ResourceType, &ev.ResourceID,
			&ev.ProjectID, &ev.Action, &ev.Payload, &ev.EmittedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// advanceWatermark двигает границу устоявшегося по одному наблюдению
// lifecycleWatermarkSQL.
//
// Правило: максимальный видимый номер M становится границей, когда ни одна
// транзакция, писавшая в `nlb_outbox` в момент наблюдения M, больше не пишет.
// Только такая транзакция может ещё выпустить номер ≤ M: писатель, взявший
// блокировку ПОСЛЕ наблюдения, вызовет nextval позже и получит номер > M.
//
// Два исхода наблюдения:
//   - писателей нет — граница переносится сразу (обычный ненагруженный случай,
//     задержка доставки нулевая);
//   - писатели есть — наблюдение запоминается и подтверждается на следующем
//     проходе. Проход инициирует NOTIFY того самого писателя (он держит
//     блокировку именно потому, что вставляет строку в `nlb_outbox`, а COMMIT
//     доставляет его NOTIFY), поэтому нормальная задержка — время его
//     транзакции. Исключение — ОТКАТ писателя: NOTIFY не будет, и подтверждение
//     приедет на 30-секундном idle-перепросе подписчика.
//
// Дыра от отката закрывается тем же правилом: номер отменённой транзакции не
// появится никогда, а её блокировка снята — значит граница переносится ЗА дыру,
// и фид не залипает (тест `aborted writer does not wedge the feed`).
//
// Осознанный размен, тот же, что у дренажей outbox: писатель, не завершающий
// транзакцию, задерживает фид целиком (у него может быть сколь угодно малый
// невыпущенный номер, и никакое наблюдение этого не опровергнет). Выбор в
// пользу «не потерять» против «доставить сейчас»; удержание дольше
// lifecycleStallWarnAfter — в лог.
func (c *lifecycleConn) advanceWatermark(ctx context.Context) error {
	var (
		maxSeq  int64
		writers []string
	)
	if err := c.conn.QueryRow(ctx, lifecycleWatermarkSQL).Scan(&maxSeq, &writers); err != nil {
		return err
	}

	// 1. Подтвердить прошлое наблюдение: его писатели доистекли.
	if len(c.pendingWriters) > 0 && !anyStillWriting(c.pendingWriters, writers) {
		if c.pendingSeq > c.watermark {
			c.watermark = c.pendingSeq
		}
		c.pendingSeq, c.pendingWriters, c.pendingSince = 0, nil, time.Time{}
	}

	// 2. Новое наблюдение берём только когда прошлое снято — иначе под
	// непрерывной записью множество ожидания обновлялось бы вечно.
	if len(c.pendingWriters) == 0 && maxSeq > c.watermark {
		if len(writers) == 0 {
			c.watermark = maxSeq
		} else {
			c.pendingSeq, c.pendingWriters, c.pendingSince = maxSeq, writers, time.Now()
		}
	}

	c.warnIfStalled()
	return nil
}

// anyStillWriting — пересекается ли зафиксированное множество писателей с
// текущим. Оба множества — единицы элементов (число одновременных писателей
// `nlb_outbox`), поэтому линейный проход дешевле любой индексации.
func anyStillWriting(pending, current []string) bool {
	for _, p := range pending {
		for _, cur := range current {
			if p == cur {
				return true
			}
		}
	}
	return false
}

// warnIfStalled жалуется, когда горизонт удерживается дольше
// lifecycleStallWarnAfter. Троттлинг — не чаще одной записи за тот же
// интервал: под потоком NOTIFY проходов много, а событие ровно одно.
func (c *lifecycleConn) warnIfStalled() {
	if len(c.pendingWriters) == 0 {
		c.lastStallWarn = time.Time{}
		return
	}
	held := time.Since(c.pendingSince)
	if held < lifecycleStallWarnAfter {
		return
	}
	if !c.lastStallWarn.IsZero() && time.Since(c.lastStallWarn) < lifecycleStallWarnAfter {
		return
	}
	c.lastStallWarn = time.Now()
	c.log.Warn("lifecycle feed watermark held by unfinished nlb_outbox writer",
		"held_for", held.Truncate(time.Second).String(),
		"withheld_up_to_sequence_no", c.pendingSeq,
		"writer_transactions", len(c.pendingWriters),
	)
}

// WaitForNotification блокирует до NOTIFY на канал либо ctx-таймаута/отмены.
func (c *lifecycleConn) WaitForNotification(ctx context.Context) error {
	_, err := c.conn.WaitForNotification(ctx)
	return err
}

// Close снимает LISTEN и закрывает соединение (best-effort, под собственными
// bounded-таймаутами — вызывается из defer на любом пути выхода).
func (c *lifecycleConn) Close() {
	unlistenCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, _ = c.conn.Exec(unlistenCtx, "UNLISTEN "+lifecycleListenChannel)
	cancel()
	closeCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	_ = c.conn.Close(closeCtx)
	cancel2()
}

var (
	_ kacho.LifecycleFeed = (*LifecycleFeed)(nil)
	_ kacho.LifecycleConn = (*lifecycleConn)(nil)
)
