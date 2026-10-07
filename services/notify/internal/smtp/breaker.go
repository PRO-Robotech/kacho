// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package smtp

import (
	"sync"
	"time"
)

// BreakerThreshold — сколько недоступностей ретранслятора ПОДРЯД размыкают цепь
// (З26 «Размыкатель», константа notify). Одиночный разрыв или 421 цепь не
// размыкает: строку повторит DEFER; пять сессий подряд без ответа узла — уже не
// случайность, и забирать строки, обрекая каждую на тот же DEFER, незачем.
const BreakerThreshold = 5

// BreakerCooldown — интервал, на который разомкнутая цепь останавливает забор
// строк (константа notify). Строки ждут `pending` и закрываются своим сроком, а не
// отказом; 30 с — один пробный забор на полминуты к лежащему узлу и не больше
// 30 с задержки письма после того, как узел вернулся.
const BreakerCooldown = 30 * time.Second

// Breaker — размыкатель ретранслятора. Безопасен для одновременного вызова из
// нескольких отправляющих горутин.
//
// Вклад даёт только клетка с [Cell.RelayUnavailable]. Любая иная клетка — узел
// ответил (принял письмо, отверг адресата или установку) — серию обрывает.
// По истечении охлаждения счёт серии сохраняется: первая же недоступность после
// охлаждения размыкает снова (пробный забор), первая иная клетка — закрывает.
type Breaker struct {
	now func() time.Time

	mu          sync.Mutex
	consecutive int
	openUntil   time.Time
}

// NewBreaker — размыкатель на часах now (в процессе — time.Now, в пробах —
// управляемые). nil — ошибка программы.
func NewBreaker(now func() time.Time) *Breaker {
	if now == nil {
		panic("smtp.NewBreaker: часы не заданы")
	}
	return &Breaker{now: now}
}

// Observe учитывает клетку исхода одной сессии.
func (b *Breaker) Observe(c Cell) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !c.RelayUnavailable {
		b.consecutive = 0
		return
	}
	b.consecutive++
	if b.consecutive >= BreakerThreshold {
		b.openUntil = b.now().Add(BreakerCooldown)
	}
}

// Open — true: цепь разомкнута, notify строки не забирает.
func (b *Breaker) Open() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.now().Before(b.openUntil)
}
