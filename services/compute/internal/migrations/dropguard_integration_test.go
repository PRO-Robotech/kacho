// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dropguard_integration_test.go — compute's drops are decided by a counter.
//
// The four block-storage tables (`disks`, `images`, `snapshots`, `disk_types`) were
// dropped on the strength of a paragraph: nothing ever seeded them, the join table
// went in an earlier migration rather than being copied, an id-preserving copy was
// never possible. All of that was true. None of it was checked, and a claim about
// data that nobody counts stops being a safety property the moment it stops being
// true — quietly, with the rows already gone.
//
// This replays compute's own chain into an empty Postgres, pauses at the version
// immediately before each drop, and COUNTS. Every number is compared for equality
// with the one declared in dropguard.json, and a table that could not be counted is
// reported as unverified rather than as empty.
package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/dropguard"
	"github.com/PRO-Robotech/kacho/pkg/dropguard/dropguardtest"
	"github.com/PRO-Robotech/kacho/services/compute/internal/migrations"
)

func computeOptions() dropguardtest.Options {
	return dropguardtest.Options{
		Service:      "compute",
		FS:           migrations.FS,
		ManifestPath: "dropguard.json",

		// Ratchet, declared by the caller because it cannot move on its own:
		// a drop that appeared without this number moving is a drop nobody
		// looked at. Asserted by the harness — see Options.DropsExpected.
		DropsExpected: 11,
	}
}

// TestIntegration_ComputeDropsAreMeasured — the gate.
func TestIntegration_ComputeDropsAreMeasured(t *testing.T) {
	rep := dropguardtest.Run(t, computeOptions())

	// The four block-storage tables are the reason this exists; name them, so that
	// a manifest edited down to nothing cannot leave the gate green.
	for _, key := range []string{"0021/disks", "0021/images", "0021/snapshots", "0022/disk_types"} {
		if _, ok := rep.Rows[key]; !ok {
			t.Errorf("%s was never counted — the retire of compute's block-storage duplicate rests on a number this run did not produce", key)
		}
	}
}

// TestIntegration_ComputeDropGuardRefusesRowsItWasNotToldAbout — the injection, in
// the direction that matters.
//
// A row is put into `disks` at the version where it still exists. The guard must go
// red and must say which table and how many rows, or an operator reading the failure
// cannot act on it. Without this, "the guard passes" would only mean "the guard ran".
func TestIntegration_ComputeDropGuardRefusesRowsItWasNotToldAbout(t *testing.T) {
	opts := computeOptions()
	opts.Seed = func(ctx context.Context, db *sql.DB, reached int64) error {
		if reached != 20 { // the version at which 0021's tables still exist
			return nil
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO disks (id, project_id, zone_id, size) VALUES ('dsk-injected', 'prj-injected', 'zone-injected', 1)`)
		return err
	}
	rep := dropguardtest.Measure(t, opts)

	var found *dropguard.Violation
	for i := range rep.Violations {
		if rep.Violations[i].Table == "disks" && rep.Violations[i].Kind == dropguard.ViolationRowCount {
			found = &rep.Violations[i]
		}
	}
	if found == nil {
		t.Fatalf("a row in `disks` did not stop the drop — the guard is reporting on a table it is not reading.\nviolations: %+v\ncounts: %+v", rep.Violations, rep.Rows)
	}
	msg := found.Error()
	for _, want := range []string{"disks", "holds 1 row", "declared 0"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not mention %q, so an operator cannot act on it: %s", want, msg)
		}
	}
	t.Logf("injection went red as required: %s", msg)

	// And only that one: the injection must not smear across tables it did not
	// touch, or the gate is reporting a mood rather than a measurement.
	for _, v := range rep.Violations {
		if v.Table != "disks" {
			t.Errorf("unrelated refusal raised by an injection into `disks`: %s", v.Error())
		}
	}
}

// TestIntegration_ComputeDropGuardStaysSilentOnTheLegitimateShape — the other half
// of the injection pair.
//
// A gate proved only by making it fail catches form, not substance: the first false
// alarm gets it switched off. The legitimate shape here is a table that legitimately
// holds rows at the moment it is dropped — the disk-type catalogue, seeded by our
// own 0001 — and the guard must pass it, having counted it, because the number it
// was told matches the number that is there.
func TestIntegration_ComputeDropGuardStaysSilentOnTheLegitimateShape(t *testing.T) {
	rep := dropguardtest.Measure(t, computeOptions())

	seeded := map[string]int64{"0022/disk_types": 4, "0011/zones": 3, "0011/regions": 1}
	for key, want := range seeded {
		got, ok := rep.Rows[key]
		if !ok {
			t.Errorf("%s was not counted at all", key)
			continue
		}
		if got != want {
			t.Errorf("%s holds %d row(s), the declaration says %d", key, got, want)
		}
	}
	for _, v := range rep.Violations {
		t.Errorf("a legitimately seeded catalogue was refused: %s", v.Error())
	}
}

// TestComputeDropGuardTreatsAnUnreadableTableAsUnverified — the third direction, and
// the one that is easy to get wrong.
//
// A table that could not be counted must refuse as "not verified": an absent table
// has no row count, and answering zero would make "I could not look" indistinguishable
// from "I looked and it was empty". No container: the property is in the verdict, and
// binding it to one would make it skippable exactly when containers are unavailable.
func TestComputeDropGuardTreatsAnUnreadableTableAsUnverified(t *testing.T) {
	drop := dropguard.Drop{Service: "compute", Version: 21, Table: "disks", File: "0021_drop_block_storage_duplicates.sql"}
	decl := dropguard.Declaration{Version: 21, Table: "disks", Kind: dropguard.KindRetire}

	for name, obsErr := range map[string]error{
		"absent table":          dropguard.ErrTableAbsent,
		"unreachable database":  dropguard.ErrNoConnection,
		"any other read faults": errors.New("dial tcp: connection refused"),
	} {
		vs := dropguard.Judge(drop, decl, 0, obsErr)
		if len(vs) != 1 || vs[0].Kind != dropguard.ViolationUnverified {
			t.Errorf("%s produced %+v, want exactly one %q", name, vs, dropguard.ViolationUnverified)
		}
	}
}
