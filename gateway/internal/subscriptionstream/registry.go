// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream

import (
	"context"
	"sync"
)

// registry — открытые потоки, сгруппированные по субъекту.
//
// # Зачем группировка ПО СУБЪЕКТУ, а не плоский список
//
// Она заведена ради kacho#1022 («отзыв прав закрывает открытый поток»), и это
// сказано прямо, чтобы её не приняли за преждевременное обобщение. Отзыв
// приходит с ИМЕНЕМ СУБЪЕКТА — край уже получает его на своём внутреннем
// слушателе, — поэтому закрытие обязано быть обходом одного ключа. Плоский
// список потребовал бы перебора всех открытых потоков на каждый отзыв, а их
// столько, сколько вкладок у всех арендаторов сразу.
//
// Здесь реализовано ТОЛЬКО ведение реестра. Подписки на событие инвалидации нет
// и в этой фазе не заводится: механизм без своего предмета — то же обещание,
// только в коде.
type registry struct {
	mu     sync.Mutex
	next   uint64
	bySubj map[string]map[uint64]context.CancelFunc
}

func newRegistry() *registry {
	return &registry{bySubj: make(map[string]map[uint64]context.CancelFunc)}
}

// add ставит поток на учёт и возвращает СНЯТИЕ С УЧЁТА.
//
// Возвращается замыкание, а не пара (субъект, номер): вызывающему не приходится
// хранить ключ, а значит и терять его. Снятие идемпотентно — повторный вызов
// ничего не делает, поэтому `defer` безопасен на любом пути выхода.
func (r *registry) add(subject string, cancel context.CancelFunc) func() {
	r.mu.Lock()
	r.next++
	id := r.next
	streams, ok := r.bySubj[subject]
	if !ok {
		streams = make(map[uint64]context.CancelFunc)
		r.bySubj[subject] = streams
	}
	streams[id] = cancel
	r.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if streams, ok := r.bySubj[subject]; ok {
				delete(streams, id)
				// Пустая карта субъекта снимается вместе с последним потоком:
				// иначе реестр растёт по числу когда-либо подписавшихся, а не
				// по числу подписанных сейчас.
				if len(streams) == 0 {
					delete(r.bySubj, subject)
				}
			}
		})
	}
}

// closeSubject отменяет контексты всех потоков субъекта и возвращает их число.
//
// Снятие с учёта делает САМ поток, выходя, а не эта функция: отмена контекста
// его разбудит, и он снимется своим `defer`. Снимай их здесь — снятие
// произошло бы дважды, а второй раз уже не по своему потоку.
func (r *registry) closeSubject(subject string) int {
	r.mu.Lock()
	streams := r.bySubj[subject]
	cancels := make([]context.CancelFunc, 0, len(streams))
	for _, cancel := range streams {
		cancels = append(cancels, cancel)
	}
	r.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels)
}
