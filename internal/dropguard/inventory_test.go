// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dropguard_test

import (
	"testing"
	"testing/fstest"

	"github.com/PRO-Robotech/kacho/internal/dropguard"
)

// TestInventoryReadsTheExecutablePart — the inventory must see statements, not
// words. A migration that merely TALKS about dropping a table has dropped nothing,
// and a gate that cannot tell the difference will demand declarations for prose and
// miss the one drop hidden behind a `/* */`.
func TestInventoryReadsTheExecutablePart(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_initial.sql": &fstest.MapFile{Data: []byte(`
-- +goose Up
CREATE TABLE widgets (id TEXT PRIMARY KEY);
INSERT INTO widgets (id) VALUES ('seed-1'), ('seed-2');
-- +goose Down
DROP TABLE widgets;
`)},
		"0002_prose_only.sql": &fstest.MapFile{Data: []byte(`
-- +goose Up
-- This migration explains why we will one day DROP TABLE widgets, but does not.
/* Nor does this block comment, which also says DROP TABLE widgets. */
ALTER TABLE widgets ADD COLUMN note TEXT NOT NULL DEFAULT '';
-- +goose Down
ALTER TABLE widgets DROP COLUMN note;
`)},
		"0003_drop_widgets.sql": &fstest.MapFile{Data: []byte(`
-- +goose Up
DROP TABLE IF EXISTS widgets;
-- +goose Down
CREATE TABLE widgets (id TEXT PRIMARY KEY);
`)},
	}

	inv, err := dropguard.Inventory("demo", fsys)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if inv.FilesScanned != 3 {
		t.Fatalf("files scanned: got %d, want 3 — a census that cannot be wrong cannot be trusted", inv.FilesScanned)
	}
	if len(inv.Drops) != 1 {
		t.Fatalf("drops: got %d %+v, want exactly 1 (0003); the Down-section drop in 0001 and the two commented mentions in 0002 destroy nothing", len(inv.Drops), inv.Drops)
	}
	d := inv.Drops[0]
	if d.Table != "widgets" || d.Version != 3 {
		t.Errorf("drop: got %s at version %d, want widgets at 3", d.Table, d.Version)
	}
	if d.RecreatedHere {
		t.Error("0003 re-creates widgets only in its Down section — that is a rollback, not an idempotency preamble")
	}
	if !inv.SeedsTable("widgets", 3) {
		t.Error("0001 INSERTs into widgets before version 3 — a declaration expecting rows there is grounded")
	}
	if inv.SeedsTable("widgets", 1) {
		t.Error("nothing INSERTs into widgets before version 1")
	}
}

// TestInventorySeesTheIdempotencyPreamble — `DROP TABLE IF EXISTS x` immediately
// followed by `CREATE TABLE x` in the same Up section destroys nothing on a chain
// that has never run: there is no x to destroy. That is a different act from a
// retire, and the difference is READ from the migration rather than asserted by
// whoever writes the declaration.
func TestInventorySeesTheIdempotencyPreamble(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_backup_and_swap.sql": &fstest.MapFile{Data: []byte(`
-- +goose Up
DROP TABLE IF EXISTS backup_roles;
CREATE TABLE backup_roles (LIKE roles INCLUDING DEFAULTS);
INSERT INTO backup_roles SELECT * FROM roles;
-- +goose Down
DROP TABLE backup_roles;
`)},
	}
	inv, err := dropguard.Inventory("demo", fsys)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if len(inv.Drops) != 1 {
		t.Fatalf("drops: got %d, want 1", len(inv.Drops))
	}
	if !inv.Drops[0].RecreatedHere {
		t.Error("backup_roles is re-created in the same Up section — the drop is a preamble, not a retire")
	}
}

// TestInventoryRejectsAMigrationWithoutAnUpSection — a file goose would not
// recognise must not be silently counted as "scanned, nothing found". That is the
// difference between zero findings and zero reading.
func TestInventoryRejectsAMigrationWithoutAnUpSection(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_no_markers.sql": &fstest.MapFile{Data: []byte("DROP TABLE widgets;\n")},
	}
	if _, err := dropguard.Inventory("demo", fsys); err == nil {
		t.Fatal("a migration with no `+goose Up` marker must be an error, not an empty scan")
	}
}
