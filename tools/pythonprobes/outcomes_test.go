// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pythonprobes

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CI-PY-1: https://github.com/PRO-Robotech/kacho/issues/2629 (also #2281/#2705).
// The driver owns fixtures and observations only. Every verdict is produced by
// the current Python CLI, shell runner, Make, ci-local or actual workflow body.
// A broken positive fixture is NOT_EXECUTED and never opens implementation.
func TestPythonOutcomesProducer(t *testing.T) { pythonOutcomes(t, "producer", 8, 8*time.Minute) }
func TestPythonOutcomesChain(t *testing.T)    { pythonOutcomes(t, "chain", 4, 28*time.Minute) }
func TestPythonOutcomesCallers(t *testing.T)  { pythonOutcomes(t, "callers", 1, 14*time.Minute) }

func pythonOutcomes(t *testing.T, group string, want int, timeout time.Duration) {
	t.Helper()
	root := repoRoot(t)
	work := t.TempDir()
	if rel, err := filepath.Rel(root, work); err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		t.Fatalf("NOT_EXECUTED: TMPDIR must be outside the git checkout: %s", work)
	}
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", filepath.Join(root, "tools/pythonprobes/testdata/outcomes_driver.py"), group, root, work)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "GIT_") && !strings.HasPrefix(item, "GOWORK=") && !strings.HasPrefix(item, "PYTHONPATH=") && !strings.HasPrefix(item, "KACHO_CI_") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "GOWORK=off", "PYTEST_DISABLE_PLUGIN_AUTOLOAD=1")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("NOT_EXECUTED: fixture driver did not finish: %v\nstdout=%s\nstderr=%s", err, out.String(), stderr.String())
	}
	var report struct {
		PrerequisiteError string `json:"prerequisite_error"`
		Cases             []struct {
			Name       string   `json:"name"`
			Errors     []string `json:"errors"`
			Assertions int      `json:"assertions"`
			Evidence   []string `json:"evidence"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("NOT_EXECUTED: unreadable driver report: %v\n%s\n%s", err, out.String(), stderr.String())
	}
	if report.PrerequisiteError != "" {
		t.Fatalf("NOT_EXECUTED: %s", report.PrerequisiteError)
	}
	if len(report.Cases) != want {
		t.Fatalf("NOT_EXECUTED: scenario coverage=%d, want %d", len(report.Cases), want)
	}
	for _, c := range report.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if c.Assertions == 0 {
				t.Fatal("empty assertion subject")
			}
			t.Logf("assertions=%d, captured commands=%d, evidence=%v", c.Assertions, len(c.Evidence), c.Evidence)
			for _, problem := range c.Errors {
				t.Error(problem)
			}
		})
	}
}
