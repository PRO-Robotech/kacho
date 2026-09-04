// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package legacyfolder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// TestFires lists every spelling the retired identifier has actually worn in
// this tree. A gate is only worth its exemption list if it catches the forms
// that were really there, so these are quotations, not inventions.
func TestFires(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"snake", "  folder_id             TEXT         NOT NULL,"},
		{"camel", `b = {"folderId": "{{projectA1Id}}"}`},
		{"camel acronym", "func (r *ImageRepo) Get(ctx context.Context, folderID, family string)"},
		{"screaming", "    const FOLDER_ID = __ENV.FOLDER_ID;"},
		{"header", "  'x-kacho-folder-id': PROJECT_ID,"},
		{"embedded camel", "pm.environment.set('_suiteFolderId', x);"},
		{"index suffix", "CREATE INDEX disks_folder_idx ON disks (folder_id);"},
		{"prose with space", "// ListMaintenancesRequest allows listing by cloud ID, folder ID."},
		{"query key", "curl 'http://localhost:18080/compute/v1/instances?folderId=x'"},
		{"dotted", "scope.folder.id"},
		{"plural", "the folder ids of the account"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !hasMatch(c.line) {
				t.Fatalf("gate does not fire on %q — it would let this spelling back in", c.line)
			}
		})
	}
}

// TestDoesNotOverFire is the other half. A rule that flags honest text teaches
// contributors to write around the matcher instead of avoiding the concept, and
// "folder" has an honest meaning here: Postman groups requests into folders and
// newman has a --folder flag. Neither is our vocabulary for a tenant container.
func TestDoesNotOverFire(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"postman folder", "newman run collections/vpc.json --folder VPC-NET-CRUD-OK"},
		{"collection folder", "// каждый case — отдельный folder в коллекции"},
		{"migration filename prose", "renamed the same way in 0009_rename_folder_to_project.sql"},
		{"folder identifier prose", "the folder identifier was retired"},
		{"folder identity prose", "a folder identity means nothing here"},
		{"words apart", "the folder and the id are unrelated"},
		{"project id", `{"projectId": "{{projectA1Id}}"}`},
		{"unrelated id", "instanceId, subnetId, addressId"},
		{"holder", "the placeholder_id column"},
		{"newline between", "folder\nid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if hasMatch(c.line) {
				t.Fatalf("gate fires on honest text %q — it would push contributors to write around it", c.line)
			}
		})
	}
}

// TestScanReportsWhatItInspected separates "no violations" from "nothing was
// looked at". Those two are the same exit status and must never be the same
// report: a gate whose walk silently covers zero files passes forever.
func TestScanReportsWhatItInspected(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "clean.go", "package p // a project_id lives here\n")
	mustWrite(t, root, "dirty/broken.py", `body = {"folderId": x}`+"\n")

	findings, stats, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d: %v", len(findings), findings)
	}
	if stats.Inspected != 2 {
		t.Fatalf("want 2 files inspected, got %d — the count is the only thing separating a clean tree from an unwalked one", stats.Inspected)
	}
	if !strings.Contains(Report(findings, stats), "Inspected 2 file(s)") {
		t.Fatalf("report does not state how much was inspected:\n%s", Report(findings, stats))
	}
}

// TestScanFindsPathNames pins that a name is a name whether it is inside a file
// or on it.
func TestScanFindsPathNames(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "docs/folder_id_notes.md", "nothing offensive inside\n")

	findings, _, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 1 || findings[0].Line != 0 {
		t.Fatalf("want one path finding, got %v", findings)
	}
}

// TestScanSkipsWhatVersionControlIgnores pins the verdict to the repository's
// content rather than to whatever sits in a working tree. Build output exists
// only on a machine that has run a build; scanning it makes the same tree
// answer differently in CI and locally, and the answer nobody can reproduce is
// the one CI reports. Untracked-but-not-ignored files are still scanned — a
// newly authored file nobody has added yet is exactly what this must catch.
func TestScanSkipsWhatVersionControlIgnores(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := gitenv.Command(root, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@example.invalid")
	git("config", "user.name", "t")

	mustWrite(t, root, ".gitignore", "dist/\n")
	mustWrite(t, root, "dist/bundle.js", "var folderId = 1;\n")
	mustWrite(t, root, "notes.md", "still says folderId here\n")
	git("add", ".gitignore", "notes.md")

	findings, _, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 1 || findings[0].Path != "notes.md" {
		t.Fatalf("want only the authored file flagged, got %v", findings)
	}
}

// TestAuditExemptionsCatchesStaleEntries keeps the lists from rotting into a
// standing allowance: an entry that no longer suppresses anything is itself a
// violation and has to be deleted.
func TestAuditExemptionsCatchesStaleEntries(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "tools/legacyfolder/legacyfolder.go", "package legacyfolder // folder_id\n")

	savedExempt, savedAwait := exemptFiles, awaitingPass
	defer func() { exemptFiles, awaitingPass = savedExempt, savedAwait }()
	awaitingPass = map[string]string{}
	exemptFiles = map[string]string{
		"tools/legacyfolder/legacyfolder.go": "the gate itself",
		"gone.md":                            "a file that no longer exists",
		"tools/legacyfolder/clean.txt":       "a file that no longer carries the name",
	}
	mustWrite(t, root, "tools/legacyfolder/clean.txt", "projectId only\n")

	findings := AuditExemptions(root)
	if len(findings) != 2 {
		t.Fatalf("want the two dead entries reported, got %v", findings)
	}
}

// TestDeferralsStayInsideTheTreesAnotherPassOwns keeps awaitingPass from
// becoming a general escape hatch. It exists only because two passes ran over
// the same tree at once; a deferral anywhere else means somebody chose not to
// fix a file they could have fixed.
func TestDeferralsStayInsideTheTreesAnotherPassOwns(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "tools/legacyfolder/legacyfolder.go", "package legacyfolder\n")
	mustWrite(t, root, "services/vpc/thing.go", "var folderId int\n")

	savedExempt, savedAwait := exemptFiles, awaitingPass
	defer func() { exemptFiles, awaitingPass = savedExempt, savedAwait }()
	exemptFiles = map[string]string{}
	awaitingPass = map[string]string{"services/vpc/thing.go": "not a tree anyone else owns"}

	findings := AuditExemptions(root)
	if len(findings) != 1 || !strings.Contains(findings[0].Text, "outside the trees") {
		t.Fatalf("want the out-of-bounds deferral reported, got %v", findings)
	}
}

// TestTreeIsClean is the gate itself, run against this repository on every
// `go test ./...`. Its message names the count so a regression reads as a
// number, not as a stack trace.
func TestTreeIsClean(t *testing.T) {
	root := repoRoot(t)
	findings, stats, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	findings = append(findings, AuditExemptions(root)...)
	if stats.Inspected == 0 {
		t.Fatal("the scan inspected zero files — a clean verdict here would mean nothing")
	}
	if len(findings) != 0 {
		t.Fatalf("%d occurrence(s) of the retired container name:\n%s", len(findings), Report(findings, stats))
	}
	t.Logf("inspected %d file(s); %d exempt; %d awaiting another pass", stats.Inspected, stats.Exempt, stats.Awaiting)
}

// TestCIRunsThisGate locks the wiring. A gate CI does not call is worth exactly
// as much as no gate; that pairing has already happened in this repository.
func TestCIRunsThisGate(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	const invocation = "go run ./tools/legacyfolder/cmd/verify-no-legacy-folder"
	if !strings.Contains(string(b), invocation) {
		t.Fatalf("ci.yaml does not run %q — wire it back in", invocation)
	}
}

func mustWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
