// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package tools_regression holds Go-level regression tests for the repo's shell
// CI gates (currently audit-list-filter.sh). Keeping them under `go test ./...`
// means the gate's own detection logic is exercised by the standard verification
// harness — a shell-gate that silently stops catching regressions is itself a
// regression, so we lock its behaviour with fixtures.
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
	// ucRequiresProject — a use-case List that rejects empty projectId (compliant).
	ucRequiresProject = `package volume

func (u *UseCase) List() error {
	if p.ProjectID == "" {
		return errRequired
	}
	return nil
}
`
	// ucNoGuard — a use-case List missing the empty-projectId backstop.
	ucNoGuard = `package volume

func (u *UseCase) List() error {
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
			name: "compliant: List narrows and use-case requires projectId",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":   repoNarrows,
				"internal/service/volume/volume.go": ucRequiresProject,
			},
			wantErr: false,
		},
		{
			// Core Finding-2 regression: file-scope grep passes (Insert carries the
			// predicate) but the List body itself no longer narrows — must FAIL.
			name: "leak: List body drops project narrowing though Insert keeps predicate",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":   repoListDropsNarrowing,
				"internal/service/volume/volume.go": ucRequiresProject,
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
