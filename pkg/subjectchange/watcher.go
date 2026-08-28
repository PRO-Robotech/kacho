// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package subjectchange — ЧИТАТЕЛЬ журнала смены субъекта, живущий в фундаменте.
//
// # Направление: соединение открывает ПОТРЕБИТЕЛЬ
//
// Владелец прав объявлен ЛИСТОМ графа рёбер: его зовут, он не зовёт никого.
// Прежде он сам дозванивался до потребителя и гасил его кэш толчком — ребро из
// листа обратно, да ещё и с обязательной ручкой адреса потребителя, из-за
// которой владелец не поднимался там, где потребителя нет вовсе. Толчок снят
// (задача #1024); читатель — здесь, и открывает соединение он.
//
// # Почему в фундаменте, а не у потребителя
//
// Свойство «смена прав доезжает до кэша решений» держится ОДНОЙ реализацией. Живи
// читатель у каждого потребителя, свойство держалось бы столькими независимыми
// копиями, сколько потребителей, — и достаточно одной забывчивой, чтобы отзыв
// пережил себя у одного из них, причём незаметно: у остальных проба зелёная.
//
// Побочное и не менее важное: живя здесь, читатель проверяется ОДНОЙ пробой
// вместе с производителем журнала — сквозь обе стороны, а не по половине. Пока
// он лежал в дереве потребителя, такая проба была невыразима: правило видимости
// `internal/` не пускает потребителя к производителю и обратно.
//
// # Что он делает
//
// Читает журнал курсором по возрастанию позиции; на любой непустой партии гасит
// кэш решений СВОЕГО процесса. Реплика, обслужившая мутацию, гасит его сама и
// немедленно; эта петля сводит СОСЕДНИЕ реплики в пределах одного окна.
package subjectchange

import (
	"context"
	"log/slog"
	"time"
)

// Poller — узкий порт над чтением журнала. Реализуется [Reader]; в пробах —
// чем угодно, потому что предмет петли не транспорт, а курсор и решение гасить.
type Poller interface {
	PollSubjectChanges(ctx context.Context, since int64) (ids []int64, headID int64, err error)
}

// minPollTimeout floors the per-call PollSubjectChanges deadline so a fast poll
// interval cannot make the deadline unreasonably tight.
const minPollTimeout = 5 * time.Second

// Watcher читает журнал смены субъекта и гасит кэш решений своего процесса,
// когда в журнале появляются новые строки.
type Watcher struct {
	poller      Poller
	flush       func()
	interval    time.Duration
	pollTimeout time.Duration
	logger      *slog.Logger
	cursor      int64
	primed      bool
}

// New собирает читателя. interval ≤ 0 резолвится в 2 с.
func New(p Poller, flush func(), interval time.Duration, logger *slog.Logger) *Watcher {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	// Bound a single PollSubjectChanges call: a hung iam handler must not stall
	// the whole cross-replica invalidation loop forever. The deadline scales
	// with the interval (a few ticks' worth) but never drops below a floor, so
	// the loop self-recovers on the next tick instead of blocking indefinitely.
	pollTimeout := interval * 4
	if pollTimeout < minPollTimeout {
		pollTimeout = minPollTimeout
	}
	return &Watcher{poller: p, flush: flush, interval: interval, pollTimeout: pollTimeout, logger: logger}
}

// Run blocks until ctx is cancelled. Call in a goroutine.
//
// РЕПЛИКИ: на-реплику — петля гасит кэш СВОЕГО процесса и держит курсор в
// памяти. Каждая реплика обязана читать сама: разведи её выбором одной — и кэш
// невыбранных не погаснет вовсе, то есть отозванный доступ продолжит там
// действовать. Дубль чтения безвреден не по намерению, а по свойству оператора:
// чтение журнала ничего не меняет, а гашение кэша идемпотентно — второй сброс
// уже пустого кэша не отличим от первого.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Poll(ctx)
		}
	}
}

// Poll исполняет РОВНО ОДНО чтение журнала: прочитать, затем принять голову за
// курсор ЛИБО двинуть курсор и погасить кэш.
//
// Цикл синхронный: когда вызов вернулся, всё, что это чтение собиралось
// сделать, сделано. Отсюда два следствия, и оба намеренные.
//
// ПЕРВОЕ — метод публичный. Он и есть та единица работы, которую [Watcher.Run]
// повторяет по расписанию; потребителю, ведущему собственный такт, незачем
// поднимать вторую петлю рядом. Тест-только-шов здесь стоял бы ровно на этом же
// месте и означал бы то же самое, только не будучи назван.
//
// ВТОРОЕ — им ставится СКВОЗНОЙ вопрос. Проба, спрашивающая «изменение прав
// доехало ли до кэша решений», обязана читать наблюдаемое ПОСЛЕ того, как
// чтение закончилось, а не пока оно идёт. Через [Watcher.Run] это означало бы
// угадывать момент по таймеру — угадывание верное на свободной машине и неверное
// на занятой.
func (w *Watcher) Poll(ctx context.Context) {
	// Per-call deadline: a stalled iam handler must not wedge the loop forever.
	pollCtx, cancel := context.WithTimeout(ctx, w.pollTimeout)
	defer cancel()
	ids, headID, err := w.poller.PollSubjectChanges(pollCtx, w.cursor)
	if err != nil {
		w.logger.Warn("subject-change poll failed", "err", err)
		return
	}
	// First successful read on a fresh process: adopt headID as the cursor and
	// do NOT flush. The cache is cold at startup, and jumping straight to headID
	// skips replaying the historical backlog in subject_change_outbox.
	if !w.primed {
		w.primed = true
		w.cursor = headID
		return
	}
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		if id > w.cursor {
			w.cursor = id
		}
	}
	if headID > w.cursor {
		w.cursor = headID
	}
	w.flush()
	w.logger.Info("authz decision-cache flushed by subject-change poll", "cursor", w.cursor)
}
