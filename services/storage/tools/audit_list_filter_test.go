// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package tools_regression holds Go-level regression tests for the repo's shell
// CI gates (currently audit-list-filter.sh). Keeping them under `go test ./...`
// means the gate's own detection logic is exercised by the standard verification
// harness — a shell-gate that silently stops catching regressions is itself a
// regression, so we lock its behaviour with fixtures.
//
// Two distinct things are asserted here, and both are needed:
//   - TestAuditListFilter — the gate's DETECTION LOGIC against synthetic fixtures
//     (does it still catch each leak shape?);
//   - TestAuditListFilter_RealTreePasses — the gate against THIS SERVICE'S REAL
//     tree, inside the ordinary `go test ./...` run.
//
// A note on why the second one exists, since this comment used to say something
// that is no longer true. It was written when NO CI job invoked
// `make audit-list-filter`, which made routing the gate through `go test` the only
// thing that covered storage at all. CI has since grown that step
// (.github/workflows/ci.yaml, job authz-artifacts, four services), so the claim
// went stale in place — and a stale claim about whether a gate runs is exactly the
// hazard the gate is for. Both paths are kept deliberately: `go test` reaches the
// real tree wherever tests run, the make target is what CI issues. The wiring is no
// longer described in prose alone — see ci_gate_wiring_test.go.
package tools_regression

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// scriptDir returns the directory this test file lives in (…/tools), which also
// holds audit-list-filter.sh.
func scriptDir(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(self)
}

// writeFile writes content to path, creating parent dirs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// runGate materialises a throwaway workspace (tools/ + fixture repo/service
// files), copies the real audit-list-filter.sh into it, runs it, and returns the
// combined output plus the process error (nil ⇒ exit 0, non-nil ⇒ gate failed).
// The script does `cd "$(dirname "$0")/.."` and inspects internal/repo/pg +
// internal/service, so a copy in <tmp>/tools sees only our fixtures.
func runGate(t *testing.T, files map[string]string) (string, error) {
	t.Helper()
	root := t.TempDir()

	src, err := os.ReadFile(filepath.Join(scriptDir(t), "audit-list-filter.sh"))
	if err != nil {
		t.Fatalf("read real script: %v", err)
	}
	dst := filepath.Join(root, "tools", "audit-list-filter.sh")
	writeFile(t, dst, string(src))
	if err := os.Chmod(dst, 0o755); err != nil {
		t.Fatalf("chmod script: %v", err)
	}
	for rel, content := range files {
		writeFile(t, filepath.Join(root, rel), content)
	}

	cmd := exec.Command("bash", dst)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

const (
	// repoNarrows — a public List whose body narrows by project_id (compliant).
	repoNarrows = `package pg

func (r *VolumeRepo) Insert() {
	_ = "INSERT ... project_id = $1"
}

func (r *VolumeRepo) List() {
	_ = "SELECT ... WHERE v.project_id = $1"
}
`
	// repoListDropsNarrowing — the Finding-2 hole: List drops project narrowing,
	// but a `project_id = $` predicate survives in Insert (file-scope grep would
	// give false confidence). A body-scoped gate MUST flag this.
	repoListDropsNarrowing = `package pg

func (r *VolumeRepo) Insert() {
	_ = "INSERT ... project_id = $1"
}

func (r *VolumeRepo) List() {
	_ = "SELECT ... FROM volumes"
}
`
	// ucCompliant — a use-case List that (a) rejects empty projectId and (b) narrows
	// the page it just read to the ids the caller may actually see (per-object).
	ucCompliant = `package volume

func (u *UseCase) List() error {
	if p.ProjectID == "" {
		return errRequired
	}
	vols, next, err := u.reader.List(ctx, p)
	visible, ferr := authzfilter.FilterVisiblePage(ctx, u.listFilter,
		authzfilter.ResourceTypeVolume, authzfilter.ActionVolumeList, vols, idOf)
	return nil
}
`
	// ucNoGuard — a use-case List missing the empty-projectId backstop.
	ucNoGuard = `package volume

func (u *UseCase) List() error {
	vols, next, err := u.reader.List(ctx, p)
	visible, ferr := authzfilter.FilterVisiblePage(ctx, u.listFilter,
		authzfilter.ResourceTypeVolume, authzfilter.ActionVolumeList, vols, idOf)
	return nil
}
`
	// ucNoPerObjectFilter — THE hole this service shipped with: project scoping is
	// in place (repo narrows by project_id, use-case demands projectId), so the old
	// gate was fully satisfied — yet every project member saw every row, because
	// nothing ever asked whether the caller may see THESE objects.
	ucNoPerObjectFilter = `package volume

func (u *UseCase) List() error {
	if p.ProjectID == "" {
		return errRequired
	}
	vols, next, err := u.reader.List(ctx, p)
	return nil
}
`
	// ucEnumerateAllowedIDs — the OTHER rejected shape: "enumerate everything the
	// subject may see, then narrow the SQL to it". OpenFGA's ListObjects caps that
	// enumeration server-side with no continuation token, so a tenant's own resource
	// silently falls outside the prefix and becomes invisible. The gate must NOT
	// accept it as a per-object filter.
	ucEnumerateAllowedIDs = `package volume

func (u *UseCase) List() error {
	if p.ProjectID == "" {
		return errRequired
	}
	allowed, err := u.authz.ListAllowedIDs(ctx, subject, resourceType, action)
	vols, next, err := u.reader.ListByIDs(ctx, allowed, p)
	return nil
}
`
	// ucFilterOnlyInComment — prose must never satisfy the gate: a comment naming
	// FilterVisibleIDs next to a List that filters nothing is the exact "form without
	// substance" this gate exists to catch.
	ucFilterOnlyInComment = `package volume

func (u *UseCase) List() error {
	if p.ProjectID == "" {
		return errRequired
	}
	// Visibility is narrowed per-object via authzfilter.FilterVisibleIDs.
	vols, next, err := u.reader.List(ctx, p)
	return nil
}
`
	// ucCompliantMentionsListObjects — the mirror case: a compliant List whose
	// comment EXPLAINS why ListObjects is banned must not be flagged for saying so.
	ucCompliantMentionsListObjects = `package volume

func (u *UseCase) List() error {
	if p.ProjectID == "" {
		return errRequired
	}
	vols, next, err := u.reader.List(ctx, p)
	// Never ListAllowedIDs/ListObjects here: the enumeration is capped server-side.
	visible, ferr := authzfilter.FilterVisiblePage(ctx, u.listFilter,
		authzfilter.ResourceTypeVolume, authzfilter.ActionVolumeList, vols, idOf)
	return nil
}
`
)

func TestAuditListFilter(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		wantErr bool // true ⇒ gate must exit non-zero
	}{
		{
			name: "compliant: repo narrows, use-case requires projectId AND filters per-object",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":   repoNarrows,
				"internal/service/volume/volume.go": ucCompliant,
			},
			wantErr: false,
		},
		{
			// Core Finding-2 regression: file-scope grep passes (Insert carries the
			// predicate) but the List body itself no longer narrows — must FAIL.
			name: "leak: List body drops project narrowing though Insert keeps predicate",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":   repoListDropsNarrowing,
				"internal/service/volume/volume.go": ucCompliant,
			},
			wantErr: true,
		},
		{
			// THE blind spot: project scoping present, per-object visibility absent.
			// The gate used to pass this — that is how storage shipped a List that
			// showed every project member every volume/snapshot/image.
			name: "leak: use-case List never asks who may see the page's objects",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":   repoNarrows,
				"internal/service/volume/volume.go": ucNoPerObjectFilter,
			},
			wantErr: true,
		},
		{
			// enumerate-then-narrow is NOT an accepted substitute for a per-page
			// batched check (ListObjects truncation makes own resources invisible).
			name: "reject: enumerate-all-allowed-ids instead of a per-page batch check",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":   repoNarrows,
				"internal/service/volume/volume.go": ucEnumerateAllowedIDs,
			},
			wantErr: true,
		},
		{
			// Finding-1 backstop assertion: repo narrows, but the use-case forgot the
			// required-projectId guard — the gate must also catch that.
			name: "leak: use-case List does not require projectId",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":   repoNarrows,
				"internal/service/volume/volume.go": ucNoGuard,
			},
			wantErr: true,
		},
		{
			// A comment is not an implementation: the gate judges code, not prose.
			name: "leak: per-object filter only mentioned in a comment",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":   repoNarrows,
				"internal/service/volume/volume.go": ucFilterOnlyInComment,
			},
			wantErr: true,
		},
		{
			// …and the converse: documenting WHY enumeration is banned must not fail
			// a List that does the right thing.
			name: "compliant: comment explains the banned enumeration shape",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":   repoNarrows,
				"internal/service/volume/volume.go": ucCompliantMentionsListObjects,
			},
			wantErr: false,
		},
		{
			// Missing use-case file ⇒ cannot prove the backstop ⇒ fail closed.
			name: "fail-closed: use-case List file absent",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go": repoNarrows,
			},
			wantErr: true,
		},
		{
			// Whitelisted cluster-catalog resource: List need not narrow.
			name: "whitelist: disk_type cluster-catalog List not project-scoped",
			files: map[string]string{
				"internal/repo/pg/disk_type_repo.go": `package pg

func (r *DiskTypeRepo) List() {
	_ = "SELECT ... FROM disk_types"
}
`,
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runGate(t, tc.files)
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Fatalf("gate exit: gotErr=%v wantErr=%v\n--- output ---\n%s", gotErr, tc.wantErr, out)
			}
		})
	}
}

// TestAuditListFilter_RealTreePasses гоняет гейт против НАСТОЯЩЕГО дерева
// kacho-storage (не фикстур). Смысл — покрытие: `make audit-list-filter` не
// вызывается ни одним CI-job'ом, поэтому сам по себе скрипт лишь «лежит рядом».
// `go test ./services/storage/...` в CI гоняется, и через этот тест гейт наконец
// судит реальные List'ы: обронённое сужение по project_id, пропавший
// required-projectId backstop или снятый per-object фильтр уронят сборку здесь.
//
// Тест НЕ помечен -short-скипом намеренно: unit-джоба CI гоняет именно `-short`, а
// гейт стоит миллисекунды и не требует Docker/Postgres.
func TestAuditListFilter_RealTreePasses(t *testing.T) {
	script := filepath.Join(scriptDir(t), "audit-list-filter.sh")
	cmd := exec.Command("bash", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("audit-list-filter must pass against the real kacho-storage tree: %v\n--- output ---\n%s", err, out)
	}
}
