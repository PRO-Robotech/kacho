// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// export_test.go — a single test-only seam onto one poll cycle.
//
// WHY. What the watcher's cases are about is per-tick semantics: which tick
// primes, which tick flushes, what the cursor is after an error. Driving those
// through Run means driving them through a time.Ticker on a shared machine —
// the case then has to guess when a tick has finished, and the only signal
// available from outside is one the FAKE emits, which necessarily fires at the
// START of the cycle, before the flush the case is counting. That guess is
// right on an idle machine and wrong on a busy one.
//
// Tick removes the guess rather than widening it: it is the same cycle Run
// drives, called synchronously, so "the tick has finished" is the call
// returning. No ticker, no goroutine, no budget, nothing to wait for.
//
// Run itself is not left unexercised — TestSubjectChangeWatcher_PollHasPerCallDeadline
// drives the real loop and proves it reaches tick with a bounded context.
package watcher

import "context"

// Tick runs exactly one poll cycle and returns when it is complete.
func (w *SubjectChangeWatcher) Tick(ctx context.Context) { w.tick(ctx) }
