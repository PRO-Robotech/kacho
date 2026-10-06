// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

import "time"

// Clock — часы notify для сроков строки (З21, УК80). Арифметика сроков идёт
// ДЛИТЕЛЬНОСТЯМИ ([Clock.Since], [Clock.Until]) от моментов, снятых
// [Clock.Now] и несущих монотонное показание: значение не проходит через
// `.UTC()`, `.In()`, `.Local()`, `.Round()`, `.Truncate()` и сериализацию —
// каждое из них снимает монотонное показание, и вычитание молча становится
// разностью стенных часов.
type Clock interface {
	// Now — показание часов с монотонным показанием.
	Now() time.Time
	// Since — сколько прошло от t.
	Since(t time.Time) time.Duration
	// Until — сколько осталось до t.
	Until(t time.Time) time.Duration
}

// SystemClock — часы процесса: [time.Now], [time.Since], [time.Until].
type SystemClock struct{}

// Now — [time.Now].
func (SystemClock) Now() time.Time { return time.Now() }

// Since — [time.Since].
func (SystemClock) Since(t time.Time) time.Duration { return time.Since(t) }

// Until — [time.Until].
func (SystemClock) Until(t time.Time) time.Duration { return time.Until(t) }
