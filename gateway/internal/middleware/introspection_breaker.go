// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// introspection_breaker.go — the service-wide half of "how often do we ask a
// provider that is not answering".
//
// WHY THIS IS NOT MORE OF THE SAME. The two mechanisms already in
// introspection_cache.go — a briefly remembered non-answer, and an in-flight
// group — are both keyed on the TOKEN, deliberately: they exist to stop one
// credential costing many round-trips. Neither can see the shape an outage
// actually has. During an outage the gateway is not holding one token, it is
// holding as many as it has live callers, and every one of them is a first
// question: it misses the remembered failure (nothing was remembered about IT),
// and it misses the flight (nothing else is asking about IT). So N live tokens
// cost N held goroutines and N connections per window, aimed at something already
// struggling — the very stampede the per-token work removed, re-entering by a
// door that work does not cover.
//
// WHAT IT CHANGES, STATED PLAINLY. This does not merely ask less often; it
// changes WHOM we stop asking. A remembered non-answer withholds the question
// only from the token that actually failed. A breaker withholds it from tokens
// that were never tried — and since the unanswered verdict PASSES the request,
// that is a widening of the soft pass, not a cost optimisation. It is therefore
// a deliberate decision rather than a side effect of tuning, and it is defensible
// on three specific grounds:
//
//   - The fault is a property of the PROVIDER, not of a credential. "Nothing
//     answered" says nothing about which token was offered, so a token never
//     tried would have received the same non-answer. This is exactly the argument
//     rememberMisconfigured makes for holding the wrong-address verdict
//     process-wide — with the important difference that that verdict refuses and
//     this one passes, which is why this one needs evidence and a short leash.
//   - It requires EVIDENCE, and consecutive evidence at that. The breaker opens
//     only after a run of questions that nobody answered, so a provider that is
//     merely unwell — answering some, failing others — keeps being asked. The
//     answers it does give are the control still working, and stopping on the
//     strength of intermittent noise would give the widening away for free.
//   - It is BOUNDED IN TIME and re-established by asking, not by a restart. One
//     probe per cooldown, and a single answer closes it. The window in which we
//     are not asking is therefore never longer than the cooldown past the moment
//     the provider recovered.
//
// The alternative — keep asking per token — is not "safer". It buys no verdict
// (the provider is not answering; there is nothing to learn), and it spends the
// gateway's own capacity to buy that nothing, converting a provider brown-out
// into an outage of this API. A control that cannot enforce should say so and
// stand aside cheaply, which is what this does; what it must not do is stand
// aside QUIETLY, which is why the reporting in auth_revocation.go keeps counting
// every request served without a check, breaker open or not.
//
// WHAT IT DELIBERATELY DOES NOT TOUCH. The wrong-address verdict
// (ErrIntrospectionMisconfigured) refuses service and is checked in Introspect
// BEFORE this breaker is consulted. Silences must never accumulate into that
// verdict — no number of unanswered questions is evidence about the address — and
// this breaker must never soften it into a pass. The two live in separate
// branches with separate memories and separate log lines, and stay that way.
package middleware

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// introspectionBreakerThreshold — how many CONSECUTIVE unanswered questions
// establish that the provider is answering nobody, rather than that one question
// was unlucky.
//
// Consecutive is the load-bearing word: any answer at all resets the count, so
// this cannot be reached by a provider that is merely degraded. Five is chosen to
// be unmistakable rather than sensitive — a run of five with no answer in between
// is not noise — while still being reached almost immediately at any real request
// rate, which is when the cost being avoided is actually being paid.
const introspectionBreakerThreshold = 5

// introspectionBreakerCooldown — how long the breaker stays open before letting
// ONE question through to see whether the provider came back.
//
// This is the bound on how late a recovery can be noticed, so it is short: a
// provider that recovers is asked again within a second of doing so, and the
// service-wide pause never outlives the fault by more than that. It is the same
// order as the per-token failure window and the per-call budget on purpose —
// while the breaker is open the control is not enforcing, and every part of this
// file treats that state as something to leave quickly, not to settle into.
const introspectionBreakerCooldown = time.Second

// errIntrospectionCircuitOpen marks the answer a caller gets when the breaker
// chose not to ask.
//
// It is wrapped around the last real failure rather than replacing it, so the
// log line in auth_revocation.go still shows WHAT the provider was doing when we
// stopped asking — "circuit open" alone would tell an operator that we are not
// asking without telling them why we stopped.
//
// Unexported on purpose: it must be indistinguishable from any other non-answer
// to the code that decides what happens to the request. The verdict is "the
// provider did not answer", the request passes, and giving callers a way to
// branch on this specific reason would invite exactly the special-casing that
// turns a soft pass into a second, undocumented policy.
var errIntrospectionCircuitOpen = errors.New("introspection circuit open: provider is not answering")

// introspectionBreaker is a three-state breaker (closed → open → half-open),
// held once for the process by IntrospectionCache.
//
// Everything is under one mutex. The critical sections are a handful of
// comparisons on a path that already contemplates a network round-trip, and the
// half-open state is only correct if "is the cooldown over" and "has a probe
// already been claimed" are decided together — a lock-free version of this would
// be a race with the claim, i.e. the stampede it is here to prevent.
type introspectionBreaker struct {
	now       func() time.Time
	threshold int
	cooldown  time.Duration

	mu sync.Mutex
	// consecutive counts unanswered questions since the last answer of any kind.
	consecutive int
	// open — the breaker is withholding questions.
	open bool
	// retryAt — when one probe may be let through.
	retryAt time.Time
	// probing — a probe has been claimed and its outcome is not in yet. Guards
	// the half-open state against releasing the whole backlog at once.
	probing bool
	// lastErr — what the provider was doing when we stopped asking, so the
	// verdict handed to callers still describes the fault.
	lastErr error
}

func newIntrospectionBreaker(threshold int, cooldown time.Duration, now func() time.Time) *introspectionBreaker {
	if threshold <= 0 {
		threshold = introspectionBreakerThreshold
	}
	if cooldown <= 0 {
		cooldown = introspectionBreakerCooldown
	}
	if now == nil {
		now = time.Now
	}
	return &introspectionBreaker{now: now, threshold: threshold, cooldown: cooldown}
}

// allow reports whether this question may reach the provider, and if not, the
// verdict to hand back.
//
// A caller that receives permit==true MUST report the outcome through
// recordAnswered or recordUnanswered. The half-open probe is CLAIMED here, so a
// permit that is never reported back leaves the breaker holding a probe that
// will not arrive — see the call site in IntrospectionCache.ask, where the permit
// is taken immediately before the round-trip that always records one or the other.
func (b *introspectionBreaker) allow() (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.open {
		return true, nil
	}
	// Still cooling down, or somebody else already took the probe.
	if b.now().Before(b.retryAt) || b.probing {
		return false, b.verdictLocked()
	}
	b.probing = true
	return true, nil
}

// verdictLocked builds the answer for a question we chose not to ask. Callers
// hold b.mu.
func (b *introspectionBreaker) verdictLocked() error {
	if b.lastErr == nil {
		return errIntrospectionCircuitOpen
	}
	return fmt.Errorf("%w: %w", errIntrospectionCircuitOpen, b.lastErr)
}

// recordAnswered files that something answered, and closes the breaker.
//
// "Answered" is broader than "the token is live": an `active=false` verdict and a
// wrong-address answer both mean the round-trip completed and the provider (or
// whatever holds that address) is reachable, which is the only question this
// breaker asks. Counting the wrong-address case here is also what keeps the two
// mechanisms from deadlocking each other — that verdict has its own process-wide
// memory and refuses service on its own terms, and it is checked before this
// breaker, so a closed breaker cannot let anything past it.
func (b *introspectionBreaker) recordAnswered() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecutive = 0
	b.open = false
	b.probing = false
	b.lastErr = nil
}

// recordUnanswered files that nothing answered.
//
// From half-open this re-opens with a FRESH cooldown rather than counting toward
// the threshold again: the probe already carried the whole question, and a
// provider that failed its probe has told us to wait another window, not to send
// four more.
func (b *introspectionBreaker) recordUnanswered(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lastErr = err

	if b.probing {
		b.probing = false
		b.retryAt = b.now().Add(b.cooldown)
		return
	}

	b.consecutive++
	if !b.open && b.consecutive >= b.threshold {
		b.open = true
		b.retryAt = b.now().Add(b.cooldown)
	}
}
