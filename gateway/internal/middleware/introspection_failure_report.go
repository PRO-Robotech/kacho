// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// introspection_failure_report.go — making a control that is not currently
// enforcing visible, without drowning the log that is supposed to show it.
//
// When the provider cannot be reached, the request is let through: an outage of
// the identity provider must not take the whole API down with it. That decision
// is defensible only while somebody can see it happening. The failure is
// per-request, so a line per occurrence buries everything else in the log during
// an outage — and a single line at the start of a ten-minute outage says nothing
// about how long it lasted. Hence: one line per window, carrying the running
// total and how many occurrences it stands for.
//
// The counters are reported through the log rather than through a metric family
// because this process exposes no metrics endpoint (see the note in
// authz_metrics.go); a gauge nothing scrapes would be a number nobody can read.
package middleware

import (
	"sync"
	"time"
)

// defaultIntrospectionFailureLogInterval — how often a continuing failure is
// re-stated. Short enough that an operator watching a stand sees it promptly,
// long enough that a sustained outage does not crowd out other lines.
const defaultIntrospectionFailureLogInterval = 30 * time.Second

// introspectionFailureReporter rate-limits repeated reports while keeping an
// exact count, so consecutive lines describe the whole outage rather than two
// unrelated moments.
type introspectionFailureReporter struct {
	interval time.Duration
	now      func() time.Time

	mu         sync.Mutex
	total      int64     // every failure since start
	suppressed int64     // failures since the last emitted line
	last       time.Time // when a line was last emitted
	started    bool
}

func newIntrospectionFailureReporter(interval time.Duration, now func() time.Time) *introspectionFailureReporter {
	if interval <= 0 {
		interval = defaultIntrospectionFailureLogInterval
	}
	if now == nil {
		now = time.Now
	}
	return &introspectionFailureReporter{interval: interval, now: now}
}

// observe records one failure and reports whether this one should be logged,
// along with the running total and the number of failures the line stands for
// (itself included). The first failure always reports — an operator must not
// wait a window to learn the control stopped enforcing.
func (r *introspectionFailureReporter) observe() (report bool, total, represents int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.total++
	r.suppressed++

	now := r.now()
	if r.started && now.Sub(r.last) < r.interval {
		return false, r.total, 0
	}
	r.started = true
	r.last = now
	represents = r.suppressed
	r.suppressed = 0
	return true, r.total, represents
}
