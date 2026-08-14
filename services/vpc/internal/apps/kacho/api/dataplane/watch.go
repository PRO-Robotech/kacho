// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"context"
	"errors"
	"time"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// Sink — куда уходит сообщение потока. Ровно тот метод, который есть у
// серверного стрима gRPC, и ничего сверх: use-case не должен знать про
// транспорт.
type Sink interface {
	Send(*vpcv1.WatchIntentResponse) error
}

// WatchIntentUseCase — доставка намерения одному подписчику.
//
// # Устройство: производитель и получатель разнесены очередью
//
// Одна горутина читает журнал, другая (вызвавшая [WatchIntentUseCase.Run])
// отдаёт получателю. Между ними — ограниченная очередь [pending]. Разнесение
// нужно ради того, чтобы медленный получатель не держал открытым чтение базы;
// ограничение очереди — ради того, чтобы он же не съел память процесса,
// обслуживающего всех.
//
// Ни одно из трёх мест не молчит: исчерпание предела объявляется исходом
// `resync`, невозможная позиция — тем же исходом с другой причиной, а обычный
// конец подписки (истёк срок носителя, отменён контекст) — отсутствием ошибки.
type WatchIntentUseCase struct {
	reader IntentReader
	obs    *Observer

	pageLimit    int
	pendingLimit int
	poll         time.Duration
	stall        time.Duration
}

// NewWatchIntentUseCase собирает use-case.
func NewWatchIntentUseCase(reader IntentReader, obs *Observer) *WatchIntentUseCase {
	return &WatchIntentUseCase{
		reader:       reader,
		obs:          obs,
		pageLimit:    PageLimit,
		pendingLimit: PendingLimit,
		poll:         PollInterval,
		stall:        PendingStallBudget,
	}
}

// Run ведёт поток до конца: до отмены контекста, до отказа получателя или до
// исхода «начни с полной выдачи».
//
// Отмена контекста — ШТАТНЫЙ конец, а не ошибка: срок жизни подписки ограничен
// носителем, и по его истечении исполнитель переподключается с последней
// применённой ревизией. Возвращать здесь ошибку значило бы называть отказом
// собственное решение о сроке.
func (u *WatchIntentUseCase) Run(ctx context.Context, known int64, sink Sink) error {
	u.obs.StreamStarted(known)
	var sent int64
	err := u.run(ctx, known, sink, &sent)
	u.obs.StreamFinished(sent, err)
	return err
}

func (u *WatchIntentUseCase) run(ctx context.Context, known int64, sink Sink, sent *int64) error {
	// Границы журнала снимаются ОДНИМ обращением до первого чтения: пара
	// «горизонт и голова», взятая порознь, могла бы не существовать ни в один
	// момент, и решение о продолжении принималось бы по несуществующему
	// состоянию.
	b, err := u.reader.Bounds(ctx)
	if err != nil {
		return err
	}
	if cause, need := resyncNeeded(known, b); need {
		u.obs.ResyncSignalled(cause.String(), known, b)
		// Журнал при этом НЕ читается вовсе: продолжать с этой позиции нельзя, и
		// страница, прочитанная «на всякий случай», была бы выдачей, о полноте
		// которой нечего сказать.
		return sink.Send(resyncEvent(cause))
	}

	// Надгробия старше начала выдачи исключаются ТОЛЬКО при выдаче с нуля: их
	// объект исполнителю никогда не отдавался. Граница — голова журнала на
	// момент подписки, а не «весь поток»: снятие, случившееся ПОСЛЕ начала
	// выдачи, обязано доехать, потому что объект мог уже уйти ранней страницей.
	var skipWithdrawnUpTo int64
	if known == 0 {
		skipWithdrawnUpTo = b.Head
	}

	// Свой контекст: производитель обязан остановиться, когда получатель ушёл, —
	// иначе горутина читала бы журнал в пустоту до конца срока подписки.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	q := newPending(u.pendingLimit, u.stall)
	var produceErr error
	go func() {
		defer q.Close()
		produceErr = u.produce(ctx, known, skipWithdrawnUpTo, q)
	}()

	for msg := range q.C() {
		if err := sink.Send(msg); err != nil {
			// Отправка не прошла, а контекст уже мёртв — подписку гасят: истёк её
			// срок у носителя либо ушёл получатель. Это ШТАТНЫЙ конец, и называть
			// его отказом значило бы писать в журнал ошибку на каждое плановое
			// переподключение исполнителя, топя в них настоящие отказы.
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if msg.GetIntent() != nil {
			*sent++
			u.obs.IntentSent()
		}
	}

	// Закрытие очереди — точка синхронизации: всё, что производитель записал в
	// produceErr и в признак переполнения, здесь уже видно.
	if q.Overflowed() {
		u.obs.ResyncSignalled(vpcv1.ResyncCause_RESYNC_CAUSE_STREAM_OVERFLOW.String(), known, b)
		return sink.Send(resyncEvent(vpcv1.ResyncCause_RESYNC_CAUSE_STREAM_OVERFLOW))
	}
	if produceErr != nil && !errors.Is(produceErr, context.Canceled) &&
		!errors.Is(produceErr, context.DeadlineExceeded) {
		return produceErr
	}
	return nil
}

// resyncNeeded решает, можно ли вообще продолжить с названной позиции.
//
// Позиция 0 — «состояния у меня нет», она законна всегда: полная выдача не
// зависит ни от горизонта, ни от головы.
func resyncNeeded(known int64, b Bounds) (vpcv1.ResyncCause, bool) {
	switch {
	case known <= 0:
		return vpcv1.ResyncCause_RESYNC_CAUSE_UNSPECIFIED, false
	case known < b.Horizon:
		// След изменений ниже горизонта уплотнён: снятия намерений старше срока
		// хранения удалены, и «что изменилось с тех пор» на эту позицию больше не
		// отвечается. Равенство горизонту законно — та ревизия исполнителю уже
		// отдавалась.
		return vpcv1.ResyncCause_RESYNC_CAUSE_REVISION_TOO_OLD, true
	case known > b.Head:
		// Позиции выше головы у платформы нет. Молчание здесь — худший исход:
		// запрос «что после несуществующей позиции» отвечается пустотой всегда,
		// и исполнитель ждал бы вечно, считая себя в синхроне.
		return vpcv1.ResyncCause_RESYNC_CAUSE_REVISION_UNKNOWN, true
	default:
		return vpcv1.ResyncCause_RESYNC_CAUSE_UNSPECIFIED, false
	}
}

// produce читает журнал и кладёт сообщения в очередь.
//
// Возвращает nil, когда очередь переполнилась: переполнение — не отказ чтения,
// а решение о потоке, и принимает его получающая сторона (см. [run]).
func (u *WatchIntentUseCase) produce(ctx context.Context, known, skipWithdrawnUpTo int64, q *pending) error {
	cursor := known
	synced := false
	for {
		rows, err := u.reader.Page(ctx, cursor, skipWithdrawnUpTo, u.pageLimit)
		if err != nil {
			return err
		}
		for _, row := range rows {
			msg, err := intentMessage(row)
			if err != nil {
				return err
			}
			switch q.Offer(ctx, intentEvent(msg)) {
			case offerAccepted:
			case offerStalled:
				u.obs.Overflowed(q.Limit(), cursor)
				return nil
			case offerAborted:
				return ctx.Err()
			}
			cursor = row.Revision
		}
		if len(rows) < u.pageLimit {
			// Смыкание объявляется РОВНО ОДИН раз за поток: до него исполнитель
			// держит частичную картину, после — полную на названной ревизии и,
			// если подписывался с нуля, вправе убрать у себя всё, чего в выдаче
			// не было.
			if !synced {
				switch q.Offer(ctx, syncedEvent(cursor)) {
				case offerAccepted:
					synced = true
				case offerStalled:
					u.obs.Overflowed(q.Limit(), cursor)
					return nil
				case offerAborted:
					return ctx.Err()
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(u.poll):
			}
		}
	}
}
