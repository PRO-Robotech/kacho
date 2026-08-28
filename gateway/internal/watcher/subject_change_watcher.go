// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package watcher — вторая полоса чтения отзыва на крае.
//
// Опрашивает `kacho-iam InternalIAMService.PollSubjectChanges` курсором по
// возрастанию идентификатора и на всякой непустой порции делает два дела:
// сбрасывает кэш решений ЭТОЙ реплики и закрывает открытые потоки названных
// субъектов (kacho#1022).
//
// # Почему полоса вторая, а не единственная
//
// Толчок iam (`InternalAuthzCacheService.InvalidateSubject`) доходит до ОДНОЙ
// реплики края — той, до которой дозвонился сливщик. Остальные узнают об отзыве
// только этим перепросом. Реплика, у которой его нет, держала бы поток
// отозванного до истечения срока жизни соединения; реплика, у которой он есть,
// но имена субъектов выбрасывает, не может закрыть поток НИ ПРИ КАКИХ УСЛОВИЯХ.
//
// # Почему обе полосы, а не одна
//
// Полоса запроса (кэш решений) покрывает СЛЕДУЮЩИЙ запрос; длинное соединение
// следующего запроса не делает. Сброс кэша выглядит отзывом и потока не касается
// вовсе — это тот самый «контроль, действующий на выдаче, но не на предъявлении».
package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// SubjectChange — одна строка `subject_change_outbox` в том объёме, в каком её
// ЧИТАЕТ край.
//
// Вид события (`op`) сюда намеренно не переносится: край его не читает, а поле,
// принятое и не прочитанное, обещает вызывающему поведение, которого нет.
// Почему решение не зависит от вида события — см.
// `handler.InternalAuthzCacheServer.WithSubjectStreamCloser`.
type SubjectChange struct {
	// ID — номер строки журнала; им двигается курсор.
	ID int64
	// Subject — субъект модели прав («user:usr_x»). Может быть пуст, если
	// владелец журнала его не назвал; такая строка двигает курсор и никого не
	// закрывает.
	Subject string
}

// Poller — узкий порт над RPC `PollSubjectChanges`.
type Poller interface {
	PollSubjectChanges(ctx context.Context, since int64) (changes []SubjectChange, headID int64, err error)
}

// StreamCloser — реестр открытых длинных соединений края.
//
// Реализуется проекцией потока (`gateway/internal/subscriptionstream`.Handler).
type StreamCloser interface {
	// CloseSubject закрывает потоки названного субъекта и возвращает их число.
	CloseSubject(subject string) int
	// CloseAll закрывает ВСЕ открытые потоки и возвращает их число.
	CloseAll() int
}

// minPollTimeout — пол срока одного вызова `PollSubjectChanges`: частый перепрос
// не вправе сделать срок неразумно тесным.
const minPollTimeout = 5 * time.Second

// Config — что приносит композиционный корень края.
type Config struct {
	// Poller — источник изменений субъекта. Обязателен.
	Poller Poller
	// Flush — сброс кэша решений этой реплики. Обязателен.
	Flush func()
	// Interval — период перепроса. Неположительный резолвится в 2s.
	Interval time.Duration

	// Closer — реестр открытых потоков. Ноль означает «на этой посадке длинных
	// соединений нет»; тогда перепрос делает ровно то, что делал прежде.
	Closer StreamCloser

	// StaleAfter — сколько край вправе ДЕРЖАТЬ потоки, не подтвердив чтения
	// отзыва. Обязателен вместе с [Config.Closer].
	//
	// Это ОКНО FAIL-CLOSED, и оно есть следствие решения, а не срока жизни
	// соединения: величина отсчитывается от последнего удачного перепроса и к
	// длительности соединения отношения не имеет. Обязана превосходить
	// [Config.Interval] — иначе один пропущенный перепрос объявляется аварией.
	StaleAfter time.Duration

	// Now — часы. Ноль резолвится в [time.Now]; проба подставляет свои, потому
	// что срок обязан быть свойством решения, а не занятости машины.
	Now func() time.Time

	// Logger — журнал процесса. Обязателен.
	Logger *slog.Logger
}

// SubjectChangeWatcher — перепрос изменений субъекта на ЭТОЙ реплике края.
type SubjectChangeWatcher struct {
	cfg         Config
	pollTimeout time.Duration
	cursor      int64
	primed      bool
	lastRead    time.Time
}

// New собирает наблюдателя и судит объявление.
//
// Отказ на сборке, а не первым отказом в бою: величина, при которой fail-closed
// не наступает никогда, — ошибка посадки, и обнаруживать её тогда, когда она уже
// кому-то стоила доступа, поздно.
func New(cfg Config) (*SubjectChangeWatcher, error) {
	if cfg.Poller == nil {
		return nil, fmt.Errorf("watcher: источник изменений субъекта не назван — " +
			"наблюдатель без него молчит ровно так же, как наблюдатель, которому нечего сообщить")
	}
	if cfg.Flush == nil {
		return nil, fmt.Errorf("watcher: сброс кэша решений не назван")
	}
	if cfg.Logger == nil {
		return nil, fmt.Errorf("watcher: журнал процесса не назван — " +
			"застрявший отзыв обязан быть виден, а не только не работать")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 2 * time.Second
	}
	if cfg.Closer != nil {
		if cfg.StaleAfter <= 0 {
			return nil, fmt.Errorf("watcher: провязан закрыватель потоков, но срок StaleAfter не объявлен — " +
				"поток пережил бы аварию читателя отзыва целиком, то есть контроль отключался бы тем самым " +
				"событием, ради которого заведён")
		}
		if cfg.StaleAfter <= cfg.Interval {
			return nil, fmt.Errorf("watcher: StaleAfter %v не превосходит Interval %v — "+
				"срок, исчерпываемый одним пропущенным перепросом, объявляет аварией всякую заминку",
				cfg.StaleAfter, cfg.Interval)
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	// Срок неподтверждённого чтения отсчитывается от СТАРТА, а не от нуля
	// времени: процесс, чей читатель не заработал ни разу, обязан закрывать
	// потоки — но по истечении того же объявленного срока, а не немедленно.
	pollTimeout := cfg.Interval * 4
	if pollTimeout < minPollTimeout {
		pollTimeout = minPollTimeout
	}
	return &SubjectChangeWatcher{cfg: cfg, pollTimeout: pollTimeout, lastRead: cfg.Now()}, nil
}

// Run блокирует до отмены контекста. Зовётся в своей горутине.
//
// РЕПЛИКИ: на-реплику — петля сбрасывает кэш СВОЕГО процесса, закрывает СВОИ
// потоки и держит курсор в памяти. Каждая реплика обязана опрашивать сама:
// разведи её — и у невыбранных реплик отозванный доступ продолжит действовать.
func (w *SubjectChangeWatcher) Run(ctx context.Context) {
	t := time.NewTicker(w.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

// tick — ровно один цикл перепроса, синхронно: когда он вернулся, всё, что этот
// цикл собирался сделать, сделано. Пробы гоняют его напрямую (см. export_test.go),
// поэтому исход цикла никогда не читается, пока цикл ещё идёт.
func (w *SubjectChangeWatcher) tick(ctx context.Context) {
	// Срок одного вызова: зависший обработчик iam не вправе заклинить петлю.
	pollCtx, cancel := context.WithTimeout(ctx, w.pollTimeout)
	defer cancel()
	changes, headID, err := w.cfg.Poller.PollSubjectChanges(pollCtx, w.cursor)
	if err != nil {
		w.cfg.Logger.Warn("subject-change poll failed", "err", err)
		w.failClosed()
		return
	}
	w.lastRead = w.cfg.Now()

	// Первый удачный перепрос свежей реплики: курсор принимается, сброса нет.
	// Кэш при старте пуст, а прыжок сразу на голову избавляет от проигрывания
	// накопленной истории. Отзывом принятие курсора не является.
	if !w.primed {
		w.primed = true
		w.cursor = headID
		return
	}
	if len(changes) == 0 {
		return
	}
	for _, c := range changes {
		if c.ID > w.cursor {
			w.cursor = c.ID
		}
	}
	if headID > w.cursor {
		w.cursor = headID
	}
	w.cfg.Flush()
	closed := w.closeNamed(changes)
	w.cfg.Logger.Info("authz decision-cache flushed by subject-change poll",
		"cursor", w.cursor, "subjects", len(changes), "streams_closed", closed)
}

// closeNamed закрывает потоки НАЗВАННЫХ порцией субъектов, каждого по одному
// разу: повтор имени в порции обходил бы реестр под замком впустую.
//
// Пустое имя отсекается безусловно: под ним не учтён ни один поток, а обход по
// нему был бы вопросом ни о ком.
func (w *SubjectChangeWatcher) closeNamed(changes []SubjectChange) int {
	if w.cfg.Closer == nil {
		return 0
	}
	seen := make(map[string]struct{}, len(changes))
	closed := 0
	for _, c := range changes {
		if c.Subject == "" {
			continue
		}
		if _, dup := seen[c.Subject]; dup {
			continue
		}
		seen[c.Subject] = struct{}{}
		closed += w.cfg.Closer.CloseSubject(c.Subject)
	}
	return closed
}

// failClosed закрывает ВСЕ потоки, когда читатель отзыва не подтверждался дольше
// объявленного срока.
//
// Неполученный ответ авторитета не есть «прав ни у кого не отзывали». Кого
// именно закрывать, реплика в этом состоянии знать не может: имена приезжали как
// раз тем чтением, которого нет, — поэтому радиус широкий, и он назван.
//
// Закрывает на КАЖДОМ отказе за сроком, а не однажды: клиент, переоткрывшийся в
// середине аварии, обязан закрыться тоже. На пустом реестре это стоит нуля.
func (w *SubjectChangeWatcher) failClosed() {
	if w.cfg.Closer == nil {
		return
	}
	stale := w.cfg.Now().Sub(w.lastRead)
	if stale < w.cfg.StaleAfter {
		return
	}
	closed := w.cfg.Closer.CloseAll()
	if closed == 0 {
		// Ноль закрытых — не событие: держать было нечего. Печатать это на
		// каждом перепросе значило бы утопить настоящую находку в шуме.
		return
	}
	w.cfg.Logger.Error("subject-change reader is stale: closing every open stream (fail-closed)",
		"stale_for", stale.String(), "stale_after", w.cfg.StaleAfter.String(), "streams_closed", closed)
}
