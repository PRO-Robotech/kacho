// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package drainer

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Class is the outcome of classifying an applier (or decoder) error. It is the
// single, testable decision point that drives whether the drainer marks a row
// success, poisons it (no further retry) or retries it unbounded with backoff.
//
// Transient-class no-poison rule: a long-but-transient IAM outage (gRPC
// Unavailable / DeadlineExceeded / connection-refused / timeout) — and a
// concurrency conflict (409) — must NEVER poison a row: it retries forever (with
// backoff) so the owner-tuple is never lost to a temporary peer outage or a
// racing writer. Only permanent errors (4xx other than the conflict class,
// decode-failure, malformed) poison.
type Class int

const (
	// ClassSuccess — nil error; the row is delivered.
	ClassSuccess Class = iota
	// ClassAlreadyApplied — the target reports already-applied (for OpenFGA: a 400
	// `already_exists` on write / `cannot_delete` on delete); idempotent success,
	// the row is marked sent. NOT the OpenFGA 409 — that is a transactional abort
	// that applied NOTHING, so marking it sent would silently drop the tuple; it
	// belongs to ClassTransient.
	ClassAlreadyApplied
	// ClassPermanent — retry is pointless (ErrPermanent, gRPC InvalidArgument /
	// 4xx-non-409, decode-failure). The row is poisoned (attempt_count forced to
	// MaxAttempts) and surfaced for an operator.
	ClassPermanent
	// ClassTransient — a temporary failure (peer Unavailable / DeadlineExceeded /
	// connection-refused / timeout / any unclassified error). The row is retried
	// unbounded with backoff and is NEVER driven into the poison gate.
	ClassTransient
)

// String renders the class for logs/metrics labels.
func (c Class) String() string {
	switch c {
	case ClassSuccess:
		return "success"
	case ClassAlreadyApplied:
		return "already_applied"
	case ClassPermanent:
		return "permanent"
	case ClassTransient:
		return "transient"
	default:
		return "unknown"
	}
}

// Classify maps an applier error to a Class.
//
// Decision order (most-specific first):
//  1. nil                                   → ClassSuccess
//  2. errors.Is(err, ErrAlreadyApplied)     → ClassAlreadyApplied
//  3. errors.Is(err, ErrPermanent)          → ClassPermanent
//  4. gRPC InvalidArgument / PermissionDenied → ClassPermanent
//  5. everything else (Unavailable,
//     DeadlineExceeded, NotFound,
//     FailedPrecondition,
//     connection-refused/timeout, raw)      → ClassTransient
//
// Rationale for step 4: both codes describe a decision about the REQUEST — its
// content, or the caller's authority to make it — and an identical retry changes
// neither. See isPermanentGRPC for why calling a refusal "transient" produces a
// permanently wedged partition rather than an eventual success.
//
// Rationale for step 5: the remaining codes describe peer STATE, which a retry can
// genuinely find changed. A raw, un-wrapped error of unknown shape is likewise
// treated as transient — fail-SAFE for delivery (retry rather than lose the
// tuple). Appliers that KNOW an error is permanent must wrap it in ErrPermanent.
func Classify(err error) Class {
	if err == nil {
		return ClassSuccess
	}
	if errors.Is(err, ErrAlreadyApplied) {
		return ClassAlreadyApplied
	}
	if errors.Is(err, ErrPermanent) {
		return ClassPermanent
	}
	if isPermanentGRPC(err) {
		return ClassPermanent
	}
	// Unavailable / DeadlineExceeded / NotFound / FailedPrecondition / connection
	// errors / any unclassified error → transient (never poison).
	return ClassTransient
}

// isPermanentGRPC reports whether err carries a gRPC status code that is
// permanent on the applier side.
//
// InvalidArgument is permanent for the obvious reason: the peer rejected the
// content, and re-sending the same content cannot change that.
//
// PermissionDenied is permanent for the same reason, arrived at less obviously.
// An authorization decision is a function of (caller, relation, object); a retry
// alters none of the three, so repeating an identical refused request cannot start
// succeeding. Calling it transient was justified as "the peer may not be
// provisioned yet, it will heal" — but that is not what the retry buys. A
// transient row is deliberately held one attempt BELOW the poison gate
// (markTransientFailure), so it never leaves the claim query's blocking set, and
// with PartitionColumn set every later row of its partition is never claimed. What
// the old classification actually produced was a partition wedged forever. It was
// observed in production shape: a register queue in which no row had ever been
// delivered, all refused on authorization grounds — and because a resource's
// unregistration is queued behind its registration, deleted resources kept their
// grants. A grant outliving the thing it grants is over-grant.
//
// Poisoning is the safe direction by comparison. The refused write never happened,
// so nothing was granted (under-grant is fail-closed), and the partition unblocks,
// so the intents queued behind it — including the revocations — apply.
//
// It is NOT self-healing on its own, and that matters: a poisoned registration means
// kacho-iam never got the resource's mirror row, and every candidate query the
// binding reconciler runs reads that mirror, so the resource has no owner tuple and
// no materialized verbs until the row is delivered. Poisoning is therefore only
// correct in a service that also re-drives poisoned rows periodically
// (reconciler.RedrivePoisoned). With that backstop the outcome is a bounded pause:
// a cause that was temporary succeeds on a later pass, and a cause that is genuinely
// permanent keeps poisoning — visibly, via the poison counter — instead of silently
// wedging every intent behind it. Without it, poisoning loses the intent for good.
//
// The other codes (NotFound, FailedPrecondition, Unavailable, DeadlineExceeded)
// stay transient: they describe peer STATE, which a retry genuinely can find
// changed. Appliers wrap additional permanent cases in ErrPermanent.
func isPermanentGRPC(err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	return st.Code() == codes.InvalidArgument || st.Code() == codes.PermissionDenied
}
