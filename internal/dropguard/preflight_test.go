// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dropguard_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/PRO-Robotech/kacho/internal/dropguard"
)

// preflightChain — two retires at different versions, so "already applied" and
// "still pending" can be told apart within one run. Built from real migration text
// through the real parser: an Inv assembled by hand would let the probe agree with
// itself about a shape the parser never produces.
func preflightChain(t *testing.T) dropguard.Inv {
	t.Helper()
	inv, err := dropguard.Inventory("demo", fstest.MapFS{
		"0001_initial.sql": &fstest.MapFile{Data: []byte(`
-- +goose Up
CREATE TABLE widgets (id TEXT PRIMARY KEY);
CREATE TABLE gadgets (id TEXT PRIMARY KEY);
-- +goose Down
DROP TABLE gadgets;
DROP TABLE widgets;
`)},
		"0003_retire_widgets.sql": &fstest.MapFile{Data: []byte(`
-- +goose Up
DROP TABLE widgets;
-- +goose Down
CREATE TABLE widgets (id TEXT PRIMARY KEY);
`)},
		"0009_retire_gadgets.sql": &fstest.MapFile{Data: []byte(`
-- +goose Up
DROP TABLE gadgets;
-- +goose Down
CREATE TABLE gadgets (id TEXT PRIMARY KEY);
`)},
	})
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	return inv
}

// allPending — nothing has been applied yet, so every drop is still ahead of us.
func allPending(int64) (bool, error) { return false, nil }

// counts serves a fixed answer per table.
func counts(m map[string]int64) dropguard.Counter {
	return func(_ context.Context, table string) (int64, error) {
		n, ok := m[table]
		if !ok {
			return 0, fmt.Errorf("%w: %q", dropguard.ErrTableAbsent, table)
		}
		return n, nil
	}
}

// TestPreflightPassesWhatItIsMeantToPass — the positive control.
//
// It is first on purpose. An assertion that a guard refuses is worth nothing beside
// a guard that refuses everything, and the two are indistinguishable without this.
func TestPreflightPassesWhatItIsMeantToPass(t *testing.T) {
	rep := dropguard.Preflight(context.Background(), counts(map[string]int64{
		"widgets": 0,
		"gadgets": 0,
	}), preflightChain(t), allPending, nil)

	if !rep.OK() {
		t.Fatalf("two empty tables must pass, got %+v", rep.Violations)
	}
	if rep.Counted != 2 || rep.Pending != 2 {
		t.Fatalf("counted %d of %d pending, want 2 of 2 — a pass that measured nothing is not a pass",
			rep.Counted, rep.Pending)
	}
}

// TestPreflightRefusesATableThatStillHoldsRows — the half the task exists for.
//
// The message must name the table and the number, or the operator cannot act on it.
func TestPreflightRefusesATableThatStillHoldsRows(t *testing.T) {
	rep := dropguard.Preflight(context.Background(), counts(map[string]int64{
		"widgets": 17,
		"gadgets": 0,
	}), preflightChain(t), allPending, nil)

	if rep.OK() {
		t.Fatal("a pending drop of a table holding 17 rows must be refused")
	}
	if len(rep.Violations) != 1 {
		t.Fatalf("want exactly one violation, got %+v", rep.Violations)
	}
	v := rep.Violations[0]
	if v.Kind != dropguard.ViolationRowCount {
		t.Errorf("violation kind %q, want %q", v.Kind, dropguard.ViolationRowCount)
	}
	msg := v.Error()
	for _, want := range []string{"widgets", "17", "0003_retire_widgets.sql"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not name %q — an operator cannot act on it", msg, want)
		}
	}
}

// TestUnreachableIsNotEmpty — the distinction the whole guard rests on.
//
// A database that could not be asked and a table that came back empty must not reach
// the verdict as the same thing. Reporting "zero rows" for a question never asked is
// reporting the SAFE answer under exactly the conditions in which nothing is known.
func TestUnreachableIsNotEmpty(t *testing.T) {
	unreachable := func(_ context.Context, table string) (int64, error) {
		return 0, fmt.Errorf("%w: ping before counting %q", dropguard.ErrNoConnection, table)
	}
	rep := dropguard.Preflight(context.Background(), unreachable, preflightChain(t), allPending, nil)

	if rep.OK() {
		t.Fatal("an unreachable database is not a clean one")
	}
	for _, v := range rep.Violations {
		if v.Kind != dropguard.ViolationUnverified {
			t.Errorf("violation kind %q, want %q", v.Kind, dropguard.ViolationUnverified)
		}
	}
	if rep.Counted != 0 {
		t.Errorf("counted %d, want 0 — nothing was measured, and the census must say so", rep.Counted)
	}
	// The mirror: the SAME shape with a real zero passes. Without this the test above
	// would also be green on a guard that refuses every outcome alike.
	if clean := dropguard.Preflight(context.Background(), counts(map[string]int64{
		"widgets": 0, "gadgets": 0,
	}), preflightChain(t), allPending, nil); !clean.OK() {
		t.Fatalf("a measured zero must pass where an unmeasured zero refuses, got %+v", clean.Violations)
	}
}

// TestAbsentTableDestroysNothing — absent is an ANSWER, not a failure to get one.
//
// On a live database a table that is not there cannot lose a row, whatever the
// migration says about it. That differs from the replayed-chain guard, where absence
// at version-1 means the count could not be taken where it should have been.
func TestAbsentTableDestroysNothing(t *testing.T) {
	rep := dropguard.Preflight(context.Background(), counts(map[string]int64{
		"gadgets": 0, // widgets missing entirely
	}), preflightChain(t), allPending, nil)

	if !rep.OK() {
		t.Fatalf("an absent table holds nothing to destroy, got %+v", rep.Violations)
	}
	if len(rep.AbsentAt) != 1 || !strings.Contains(rep.AbsentAt[0], "widgets") {
		t.Fatalf("absence must be on the record, got %+v", rep.AbsentAt)
	}
	if rep.Counted != 2 {
		t.Errorf("counted %d, want 2 — an observed absence IS a measurement", rep.Counted)
	}
}

// TestAppliedVersionsAreNotPending — the drop already happened; there is nothing
// left to guard, and counting the table now would measure whatever replaced it.
func TestAppliedVersionsAreNotPending(t *testing.T) {
	appliedThrough9 := func(v int64) (bool, error) { return v <= 9, nil }

	rep := dropguard.Preflight(context.Background(), counts(map[string]int64{
		"widgets": 500, "gadgets": 500,
	}), preflightChain(t), appliedThrough9, nil)

	if !rep.OK() {
		t.Fatalf("no drop is pending, so nothing can be destroyed: %+v", rep.Violations)
	}
	if rep.Pending != 0 || rep.Counted != 0 {
		t.Fatalf("pending %d counted %d, want 0/0", rep.Pending, rep.Counted)
	}
	if rep.DropsInChain != 2 {
		t.Errorf("chain census lost: %d, want 2", rep.DropsInChain)
	}
}

// TestCannotTellWhetherAppliedIsRefused — the fabricated-fallback half.
//
// If the applied set cannot be read, the honest answer is "unknown". Guessing
// "applied" would skip every check, which is the safe-looking answer produced by
// knowing nothing — the exact failure mode this package was written against.
func TestCannotTellWhetherAppliedIsRefused(t *testing.T) {
	broken := func(int64) (bool, error) { return false, errors.New("permission denied for relation goose_db_version") }

	rep := dropguard.Preflight(context.Background(), counts(map[string]int64{
		"widgets": 0, "gadgets": 0,
	}), preflightChain(t), broken, nil)

	if rep.OK() {
		t.Fatal("an unreadable applied-set must refuse, not silently skip every drop")
	}
	if rep.Violations[0].Kind != dropguard.ViolationUnverified {
		t.Errorf("kind %q, want %q", rep.Violations[0].Kind, dropguard.ViolationUnverified)
	}
}

// TestApprovalReleasesExactlyOneDrop — an operator decision names the drop it makes.
//
// Both halves matter: the named drop goes through, and a DIFFERENT non-empty drop is
// still refused by the same run. An approval that releases more than it names is a
// global off switch wearing a specific name.
func TestApprovalReleasesExactlyOneDrop(t *testing.T) {
	approvals := []dropguard.Approval{{Version: 3, Table: "widgets"}}

	t.Run("named drop proceeds", func(t *testing.T) {
		rep := dropguard.Preflight(context.Background(), counts(map[string]int64{
			"widgets": 17, "gadgets": 0,
		}), preflightChain(t), allPending, approvals)
		if !rep.OK() {
			t.Fatalf("the approved drop must proceed, got %+v", rep.Violations)
		}
		if len(rep.Approved) != 1 || !strings.Contains(rep.Approved[0], "widgets") {
			t.Fatalf("an approval must stay on the record, got %+v", rep.Approved)
		}
	})

	t.Run("an unnamed drop is still refused", func(t *testing.T) {
		rep := dropguard.Preflight(context.Background(), counts(map[string]int64{
			"widgets": 17, "gadgets": 4,
		}), preflightChain(t), allPending, approvals)
		if rep.OK() {
			t.Fatal("approving one drop must not release another")
		}
		if len(rep.Violations) != 1 || rep.Violations[0].Table != "gadgets" {
			t.Fatalf("want the gadgets drop refused, got %+v", rep.Violations)
		}
	})

	t.Run("an approval that matches no pending drop is reported, not obeyed", func(t *testing.T) {
		stale := []dropguard.Approval{{Version: 999, Table: "nothing"}}
		rep := dropguard.Preflight(context.Background(), counts(map[string]int64{
			"widgets": 17, "gadgets": 0,
		}), preflightChain(t), allPending, stale)
		if rep.OK() {
			t.Fatal("a stale approval releases nothing")
		}
		if len(rep.StaleApprovals) != 1 {
			t.Fatalf("a stale approval must be named in the census, got %+v", rep.StaleApprovals)
		}
	})
}

// TestParseApprovalsRefusesWhatItCannotUnderstand — a mistyped approval must not
// read as "approve nothing" on a run whose whole purpose is to refuse.
func TestParseApprovalsRefusesWhatItCannotUnderstand(t *testing.T) {
	t.Run("well formed", func(t *testing.T) {
		got, err := dropguard.ParseApprovals("3/widgets, 0045/kacho_vpc.dataplane_intent")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		want := []dropguard.Approval{{Version: 3, Table: "widgets"}, {Version: 45, Table: "kacho_vpc.dataplane_intent"}}
		if len(got) != len(want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("entry %d: got %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("empty means no approvals", func(t *testing.T) {
		got, err := dropguard.ParseApprovals("   ")
		if err != nil || len(got) != 0 {
			t.Fatalf("got (%+v, %v), want (empty, nil)", got, err)
		}
	})

	for _, bad := range []string{"widgets", "3", "3/", "/widgets", "x/widgets", "3/widgets/extra"} {
		t.Run("malformed "+bad, func(t *testing.T) {
			if _, err := dropguard.ParseApprovals(bad); err == nil {
				t.Fatalf("%q parsed without error — a typo must not read as 'approve nothing'", bad)
			}
		})
	}
}

// TestCensusSaysHowMuchWasLookedAt — "no violations" from a run that measured
// nothing must not be readable as success.
func TestCensusSaysHowMuchWasLookedAt(t *testing.T) {
	rep := dropguard.Preflight(context.Background(), counts(map[string]int64{
		"widgets": 0, "gadgets": 0,
	}), preflightChain(t), allPending, nil)

	var sb strings.Builder
	rep.WriteCensus(&sb)
	out := sb.String()
	for _, want := range []string{"demo", "2"} {
		if !strings.Contains(out, want) {
			t.Errorf("census %q does not name %q", out, want)
		}
	}
	if !strings.Contains(rep.Summary(), "counted 2 of 2") {
		t.Errorf("summary %q must say how many of how many were counted", rep.Summary())
	}
}

// TestGateRefusesBeforeItEverTouchesTheDatabase — the two ways the check can be
// unable to start.
//
// Both must stop the deploy rather than wave it through. They are reachable with a
// nil handle precisely because they are decided before anything is asked of the
// database, which is also why they can be asserted without one.
func TestGateRefusesBeforeItEverTouchesTheDatabase(t *testing.T) {
	valid := fstest.MapFS{"0003_retire.sql": &fstest.MapFile{Data: []byte(
		"-- +goose Up\nDROP TABLE widgets;\n-- +goose Down\nCREATE TABLE widgets (id TEXT);\n")}}

	t.Run("migrations unreadable", func(t *testing.T) {
		err := dropguard.Gate(context.Background(), nil, "demo", fstest.MapFS{}, io.Discard)
		if err == nil {
			t.Fatal("migrations that cannot be read mean the pending drops are unknown, not absent")
		}
	})

	t.Run("approval list malformed", func(t *testing.T) {
		t.Setenv(dropguard.ApprovalEnv, "not-an-approval")
		err := dropguard.Gate(context.Background(), nil, "demo", valid, io.Discard)
		if err == nil {
			t.Fatal("an unreadable approval list must stop the deploy, not read as 'approve nothing'")
		}
		if !strings.Contains(err.Error(), dropguard.ApprovalEnv) {
			t.Errorf("error %q does not name the variable the operator has to fix", err)
		}
	})
}
