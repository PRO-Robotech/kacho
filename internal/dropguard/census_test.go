// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dropguard_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/dropguard"
)

// TestARunThatMeasuredNothingDoesNotLookLikeSuccess — the census requirement.
//
// Zero violations is the same output whether the guard counted every table and
// found them empty, or never reached a database at all. The only thing separating
// those two is the count of what was read, so it is printed every time, and the
// verdict word changes with it.
func TestARunThatMeasuredNothingDoesNotLookLikeSuccess(t *testing.T) {
	inv := chain(t)

	nothing := dropguard.NothingMeasured(inv)
	if len(nothing.Violations) != 0 {
		t.Fatalf("precondition: the degraded report carries no findings, got %+v", nothing.Violations)
	}
	var b strings.Builder
	nothing.WriteCensus(&b)
	out := b.String()

	if !strings.Contains(out, "NOT VERIFIED") {
		t.Errorf("a run that measured nothing did not say so:\n%s", out)
	}
	if !strings.Contains(out, "measured 0 of 4") {
		t.Errorf("the census does not give both numbers:\n%s", out)
	}
	for _, coord := range []string{"0002/scratch", "0003/catalogue", "0003/ledger", "0003/never_here"} {
		if !strings.Contains(out, coord) {
			t.Errorf("the census does not name unanswered drop %s:\n%s", coord, out)
		}
	}

	// And the opposite: a run that did the work says OK, with the same two numbers.
	done := dropguard.Report{Service: "demo", DropsInChain: 4, Measured: 4, Rows: map[string]int64{"0003/ledger": 0}}
	b.Reset()
	done.WriteCensus(&b)
	if got := b.String(); !strings.Contains(got, "OK") || !strings.Contains(got, "measured 4 of 4") {
		t.Errorf("a complete run does not report itself as one:\n%s", got)
	}
	if strings.Contains(b.String(), "NOT VERIFIED") {
		t.Errorf("a complete run was labelled unverified:\n%s", b.String())
	}
}

// TestAPartialRunIsNeitherGreenNorSilent — the middle case: some drops counted,
// some not. It must not round to either end.
func TestAPartialRunIsNeitherGreenNorSilent(t *testing.T) {
	partial := dropguard.Report{
		Service: "demo", DropsInChain: 4, Measured: 2,
		Rows:       map[string]int64{"0003/ledger": 0, "0003/catalogue": 2},
		Unmeasured: []string{"0002/scratch", "0003/never_here"},
	}
	if partial.OK() {
		t.Error("a run that reached half the drops reported itself as OK")
	}
	var b strings.Builder
	partial.WriteCensus(&b)
	out := b.String()
	if !strings.Contains(out, "PARTIAL") || !strings.Contains(out, "measured 2 of 4") {
		t.Errorf("a partial run does not say which half it did:\n%s", out)
	}
	for _, coord := range []string{"0002/scratch", "0003/never_here"} {
		if !strings.Contains(out, coord) {
			t.Errorf("the census does not name unanswered drop %s:\n%s", coord, out)
		}
	}
}
