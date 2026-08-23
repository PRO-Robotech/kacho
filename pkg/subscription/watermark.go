// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscription

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// stallWarnAfter — с какого возраста удержанного горизонта поток жалуется в лог.
//
// Горизонт держит ЧУЖАЯ незавершённая транзакция; дольше одного холостого
// перепроса это означает зависшего писателя журнала, а не нормальную
// конкуренцию. Без жалобы «поток молчит, потому что кто-то не коммитится»
// неотличимо от «событий нет» — ровно тот немой отказ, который мы ловим в
// дренажах.
const stallWarnAfter = 30 * time.Second

// watermark — ВОДЯНОЙ ЗНАК: граница устоявшегося в журнале одного потока.
//
// Имя взято у источника намеренно. Приёмка фазы Ф1 назначила предикатом переноса
// техники именно его: «`watermark` в дереве — не ноль ВНЕ `services/nlb`». Пока
// техника звалась здесь иначе, предикат отвечал «не перенесена» на перенесённой
// технике — и снятие мёртвого читателя nlb (kacho#1043) уничтожило бы
// единственный написанный ответ на этот класс, формально не нарушив ни одного
// условия.
//
// # Что здесь перенесено, а не изобретено
//
// Техника взята у `services/nlb/internal/repo/kacho/pg/lifecycle_feed.go`, где
// она написана, разобрана и покрыта пробой на трёх подслучаях. Перенос — а не
// повторное изобретение — был условием фазы: единственный написанный ответ на
// этот класс жил там, и снятие того файла до переноса уничтожило бы его.
//
// Обобщено ровно одно: имя таблицы и имя колонки номера пришли параметрами.
// Правило, разбор отката и размен остались дословно.
//
// # Правило
//
// Максимальный ВИДИМЫЙ номер M становится границей, когда ни одна транзакция,
// писавшая в журнал в момент наблюдения M, больше не пишет. Только такая
// транзакция может ещё выпустить номер ≤ M: писатель, взявший блокировку ПОСЛЕ
// наблюдения, вызовет счётчик позже и получит номер > M.
//
// # Почему блокировка таблицы, а не снимок транзакций
//
// Писатель держит `RowExclusiveLock` на журнале с момента ПЛАНИРОВАНИЯ своей
// вставки — то есть ещё до того, как счётчик выдал ему номер, — и до конца
// транзакции. Идентификатор транзакции назначается позже, на самой вставке:
// существует наблюдаемое состояние «номер уже выдан, идентификатора транзакции
// ещё нет», в котором горизонт по снимку транзакций СЛЕП. Дополнительно:
// верхняя граница снимка не является верхней границей работающих транзакций,
// поэтому «нижняя догнала прежнюю верхнюю» не доказывает ничего.
//
// `virtualtransaction` идентифицирует ТРАНЗАКЦИЮ, а не соединение: тот же
// процесс в следующей транзакции получит другой идентификатор. Поэтому
// непрерывно пишущие фоновые работники не могут удерживать горизонт вечно —
// зафиксированное множество всегда доистекает.
//
// # Осознанный размен
//
// Писатель, не завершающий транзакцию, задерживает поток целиком: у него может
// быть сколь угодно малый невыпущенный номер, и никакое наблюдение этого не
// опровергнет. Выбор в пользу «не потерять» против «доставить сейчас»;
// удержание дольше [stallWarnAfter] — в лог.
type watermark struct {
	log   *slog.Logger
	query string

	// settled — граница устоявшегося: каждый номер ≤ settled либо уже видим,
	// либо не появится никогда (писатель откатился).
	settled int64

	// earliest — самая ранняя удержанная строка журнала (0, если журнал пуст).
	earliest int64

	// pendingSeq / pendingWriters — наблюдение, ожидающее подтверждения.
	// Множество фиксируется ОДИН раз и не пополняется новыми писателями — иначе
	// под непрерывной записью оно никогда бы не опустело.
	pendingSeq     int64
	pendingWriters []string
	pendingSince   time.Time

	lastStallWarn time.Time
	now           func() time.Time
}

// newWatermark собирает наблюдателя для одной таблицы.
//
// Имя таблицы и колонки попадают в текст запроса, поэтому они обязаны быть уже
// осуждены [Journal.Validate]: негодного имени сюда не доходит by construction,
// а не потому, что здесь его экранируют.
func newWatermark(s Storage, log *slog.Logger, now func() time.Time) *watermark {
	return &watermark{
		log: log,
		now: now,
		query: fmt.Sprintf(`
			SELECT COALESCE((SELECT max(%[1]s) FROM %[2]s), 0),
			       COALESCE((SELECT min(%[1]s) FROM %[2]s), 0),
			       COALESCE((SELECT array_agg(DISTINCT l.virtualtransaction)
			                   FROM pg_locks l
			                  WHERE l.relation = $1::regclass
			                    AND l.mode = 'RowExclusiveLock'
			                    AND l.granted
			                    AND l.pid <> pg_backend_pid()
			                    AND l.database = (SELECT oid FROM pg_database
			                                       WHERE datname = current_database())),
			                '{}'::text[])`, s.PositionColumn, s.Table),
	}
}

// advance двигает границу устоявшегося по одному наблюдению.
//
// Два исхода:
//   - писателей нет — граница переносится сразу (обычный ненагруженный случай,
//     задержка доставки нулевая);
//   - писатели есть — наблюдение запоминается и подтверждается на следующем
//     проходе. Проход инициирует пробуждение того самого писателя (он держит
//     блокировку именно потому, что вставляет строку, а фиксация доставляет его
//     уведомление), поэтому нормальная задержка — время его транзакции.
//     Исключение — ОТКАТ писателя: уведомления не будет, и подтверждение
//     приедет на холостом перепросе.
//
// Дыра от отката закрывается тем же правилом: номер отменённой транзакции не
// появится никогда, а её блокировка снята — значит граница переносится ЗА дыру,
// и поток не залипает.
func (h *watermark) advance(ctx context.Context, conn *pgx.Conn, table string) error {
	var (
		maxSeq  int64
		minSeq  int64
		writers []string
	)
	if err := conn.QueryRow(ctx, h.query, table).Scan(&maxSeq, &minSeq, &writers); err != nil {
		return err
	}
	h.earliest = minSeq

	// 1. Подтвердить прошлое наблюдение: его писатели доистекли.
	if len(h.pendingWriters) > 0 && !anyStillWriting(h.pendingWriters, writers) {
		if h.pendingSeq > h.settled {
			h.settled = h.pendingSeq
		}
		h.pendingSeq, h.pendingWriters, h.pendingSince = 0, nil, time.Time{}
	}

	// 2. Новое наблюдение берём только когда прошлое снято — иначе под
	// непрерывной записью множество ожидания обновлялось бы вечно.
	if len(h.pendingWriters) == 0 && maxSeq > h.settled {
		if len(writers) == 0 {
			h.settled = maxSeq
		} else {
			h.pendingSeq, h.pendingWriters, h.pendingSince = maxSeq, writers, h.now()
		}
	}

	h.warnIfStalled()
	return nil
}

// floor — нижняя возобновимая позиция.
//
// У владельца, удерживающего журнал целиком, её не существует, и служебное
// сообщение говорит это отдельным признаком, а не нулём в поле позиции. У
// владельца, который журнал чистит, она выводится из самой ранней удержанной
// строки: с позиции `earliest-1` возобновление ещё не теряет ничего.
//
// Пустой журнал у чистящего владельца означает, что удержано ноль строк, и
// возобновиться можно только с текущей границы: всё, что было до неё, уже
// снято.
func (h *watermark) floor(r Retention) int64 {
	if r == RetainsEverything {
		return 0
	}
	if h.earliest > 0 {
		return h.earliest - 1
	}
	return h.settled
}

// anyStillWriting — пересекается ли зафиксированное множество писателей с
// текущим. Оба множества — единицы элементов, поэтому линейный проход дешевле
// любой индексации.
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

// warnIfStalled жалуется, когда горизонт удерживается дольше [stallWarnAfter].
// Троттлинг — не чаще одной записи за тот же интервал: под потоком пробуждений
// проходов много, а событие ровно одно.
func (h *watermark) warnIfStalled() {
	if len(h.pendingWriters) == 0 {
		h.lastStallWarn = time.Time{}
		return
	}
	held := h.now().Sub(h.pendingSince)
	if held < stallWarnAfter {
		return
	}
	if !h.lastStallWarn.IsZero() && h.now().Sub(h.lastStallWarn) < stallWarnAfter {
		return
	}
	h.lastStallWarn = h.now()
	h.log.Warn("subscription: settled watermark held by an unfinished journal writer",
		"held_for", held.Truncate(time.Second).String(),
		"withheld_up_to", h.pendingSeq,
		"writer_transactions", len(h.pendingWriters),
	)
}
