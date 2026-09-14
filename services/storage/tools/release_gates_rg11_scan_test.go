//go:build rg11trivy

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package tools_regression

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const rg11Proof = "deploy/scripts/assert-scan-stubs-hide-nothing-inject.sh"

// RG1.1-01..04 and D2: https://github.com/PRO-Robotech/kacho/issues/2175.
// The real scan lane needs the prepared Trivy binary and TRIVY_CACHE_DIR.
// Process doubles below prove file lifecycle only; they do not claim scan results.
func TestRG11_01(t *testing.T) {
	t.Run("real_clean", func(t *testing.T) {
		f := rg11NewScanFixture(t, false, "real")
		f.prepareTrivy(t)
		rc, out := f.run(t, "success")
		rg11WantCode(t, rc, 0, out)
		rg11WantProof(t, out)
		f.wantPreserved(t)
	})
}

func TestRG11_02(t *testing.T) {
	t.Run("missing_clean", func(t *testing.T) {
		f := rg11NewScanFixture(t, false, "absent")
		rc, out := f.run(t, "success")
		rg11WantAbsent(t, rc, out)
		f.wantPreserved(t)
	})
}

func TestRG11_03(t *testing.T) {
	for _, lane := range []struct{ name, scanner string }{
		{"dirty_present_repeat", "real"}, {"dirty_absent_repeat", "absent"},
	} {
		t.Run(lane.name, func(t *testing.T) {
			f := rg11NewScanFixture(t, true, lane.scanner)
			if lane.scanner == "real" {
				f.prepareTrivy(t)
			}
			for attempt := 1; attempt <= 2; attempt++ {
				rc, out := f.run(t, "success")
				t.Logf("attempt=%d rc=%d", attempt, rc)
				if lane.scanner == "real" {
					rg11WantCode(t, rc, 0, out)
					rg11WantProof(t, out)
				} else {
					rg11WantAbsent(t, rc, out)
				}
				f.wantPreserved(t)
			}
		})
	}
}

func TestRG11_04(t *testing.T) {
	t.Run("failure_after_mutation", func(t *testing.T) {
		f := rg11ProcessTwin(t, false)
		rg11OneFact(t, f, "success", "assertion_failure")
		rc, out := f.run(t, "assertion_failure")
		rg11WantCode(t, rc, 1, out)
		if !strings.Contains(out, "ПРОВАЛ") || !strings.Contains(out, "код 2") {
			t.Errorf("failed assertion and actual code were not reported: %s", out)
		}
		events := f.events(t)
		injected := 0
		for _, event := range events {
			if event.Kind == "scan" && event.Code == 2 && event.Phase == "suppressed" {
				injected++
				if event.SHA256 == fmt.Sprintf("%x", sha256.Sum256(f.original)) {
					t.Fatal("CONDITION NOT CREATED: failure did not follow an actual configuration mutation")
				}
			}
		}
		if injected != 1 {
			t.Fatalf("CONDITION NOT CREATED: injected scan responses=%d, want exactly one", injected)
		}
		f.wantPreserved(t)
	})
}

func TestRG11SnapshotPrepare(t *testing.T) {
	for _, fault := range []string{"create_failure", "copy_failure", "partial_snapshot", "snapshot_success"} {
		t.Run(fault, func(t *testing.T) {
			if fault == "snapshot_success" {
				rg11ProcessTwin(t, true)
				return
			}
			f := rg11ProcessTwin(t, true)
			rg11OneFact(t, f, "success", fault)
			rc, out := f.run(t, fault)
			events := f.events(t)
			if rg11EventCount(events, fault) != 1 {
				t.Fatalf("CONDITION NOT CREATED: %s injection was not reached: %+v", fault, events)
			}
			rg11WantCode(t, rc, 2, out)
			f.wantPreserved(t)
			for _, kind := range []string{"scan", "restore_attempt"} {
				if count := rg11EventCount(events, kind); count != 0 {
					t.Errorf("preparation failed but %s executed %d times", kind, count)
				}
			}
			if !regexp.MustCompile(`(?i)(snapshot|сним|резерв|подготов)`).MatchString(out) {
				t.Errorf("missing product preparation diagnostic: %s", out)
			}
			f.wantSnapshotRemoved(t)
		})
	}
}

func TestRG11SnapshotRestore(t *testing.T) {
	t.Run("restore_success", func(t *testing.T) { rg11ProcessTwin(t, true) })
	t.Run("copy_failure", func(t *testing.T) {
		f := rg11ProcessTwin(t, true)
		rg11OneFact(t, f, "success", "restore_failure")
		rc, out := f.run(t, "restore_failure")
		events := f.events(t)
		if rg11EventCount(events, "restore_failure") == 0 {
			t.Fatalf("CONDITION NOT CREATED: restore fault was not reached: %+v", events)
		}
		rg11WantCode(t, rc, 2, out)
		path := f.snapshotPath(t)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("usable recovery snapshot was not retained: %v", err)
		} else if !bytes.Equal(data, f.original) {
			t.Errorf("retained snapshot differs from original: %s", path)
		}
		if !strings.Contains(out, path) || !regexp.MustCompile(`(?i)(восстанов|restore)`).MatchString(out) {
			t.Errorf("restore refusal does not identify retained snapshot %q: %s", path, out)
		}
		failed := false
		for _, event := range events {
			if event.Kind == "restore_failure" {
				failed = true
			}
			if failed && event.Kind == "scan" {
				t.Errorf("scan continued after restore failed: phase=%s", event.Phase)
			}
		}
	})
}

type rg11ScanEvent struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Phase  string `json:"phase"`
	SHA256 string `json:"sha256"`
	Code   int    `json:"code"`
}

type rg11ScanFixture struct {
	root, state, scratch, bin, scanner string
	env                                []string
	original                           []byte
	status, diff                       string
}

func rg11NewScanFixture(t *testing.T, dirty bool, scanner string) *rg11ScanFixture {
	t.Helper()
	source := repoRoot(t)
	tmp := t.TempDir()
	if rel, err := filepath.Rel(source, tmp); err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		t.Fatal("CONDITION NOT CREATED: temporary fixture must be outside the product repository")
	}
	f := &rg11ScanFixture{root: filepath.Join(tmp, "tree"), state: filepath.Join(tmp, "state"), scratch: filepath.Join(tmp, "scratch with spaces"), bin: filepath.Join(tmp, "bin"), scanner: scanner}
	for _, dir := range []string{f.state, f.scratch, f.bin} {
		rg11Must(t, os.MkdirAll(dir, 0o700))
	}
	// Shared object storage is read-only; this clone owns its index and worktree.
	rg11Command(t, source, nil, "git", "clone", "--shared", "--quiet", "--no-checkout", source, f.root)
	rg11Command(t, f.root, nil, "git", "checkout", "--quiet", "--detach", "HEAD")
	// Exercise tracked working bytes, including a future uncommitted fix.
	modified := rg11Command(t, source, nil, "git", "ls-files", "-m", "-z")
	for _, path := range strings.Split(modified, "\x00") {
		if path != "" {
			data := rg11Read(t, filepath.Join(source, path))
			rg11Must(t, os.WriteFile(filepath.Join(f.root, path), data, 0o644))
		}
	}
	config := filepath.Join(f.root, "trivy.yaml")
	if dirty {
		data := append(rg11Read(t, config), []byte("\n# RG1.1 local change\n")...)
		rg11Must(t, os.WriteFile(config, data, 0o644))
	}
	f.original = rg11Read(t, config)
	rg11Must(t, os.WriteFile(filepath.Join(f.state, "original"), f.original, 0o600))
	f.status = rg11Command(t, f.root, nil, "git", "status", "--porcelain", "--", "trivy.yaml")
	f.diff = rg11Command(t, f.root, nil, "git", "diff", "--", "trivy.yaml")
	if (f.status != "") != dirty {
		t.Fatalf("CONDITION NOT CREATED: dirty=%v, status=%q", dirty, f.status)
	}
	f.env = rg11CleanEnv()
	for _, name := range []string{"bash", "python3", "git", "cp", "cmp", "mktemp", "rm", "dirname", "tail", "sed", "grep"} {
		path, err := exec.LookPath(name)
		rg11Must(t, err)
		path, err = filepath.Abs(path)
		rg11Must(t, err)
		rg11Must(t, os.Symlink(path, filepath.Join(f.bin, name)))
		f.env = append(f.env, "RG11_REAL_"+strings.ToUpper(name)+"="+path)
	}
	f.env = append(f.env, "PATH="+f.bin, "TMPDIR="+f.scratch, "RG11_CONFIG="+config, "RG11_STATE="+f.state)
	rg11Command(t, f.root, f.env, filepath.Join(f.bin, "python3"), "-c", "import yaml; print(yaml.__version__)")
	switch scanner {
	case "real":
		binary, err := exec.LookPath("trivy")
		rg11Must(t, err)
		rg11Must(t, os.Symlink(binary, filepath.Join(f.bin, "trivy")))
		cache := os.Getenv("TRIVY_CACHE_DIR")
		if cache == "" {
			t.Fatal("CONDITION NOT CREATED: TRIVY_CACHE_DIR must name this run's prepared cache")
		}
		rg11Read(t, filepath.Join(cache, "policy", "metadata.json"))
		ownCache := filepath.Join(tmp, "trivy-cache")
		rg11Command(t, source, nil, "cp", "-a", cache, ownCache)
		f.env = append(f.env, "TRIVY_CACHE_DIR="+ownCache)
		t.Logf("real Trivy binary_sha256=%x cache_metadata=%s", sha256.Sum256(rg11Read(t, binary)), rg11Read(t, filepath.Join(ownCache, "policy", "metadata.json")))
	case "process":
		fixture := filepath.Join(source, "services/storage/tools/testdata/rg11/scan_process.py")
		python, err := exec.LookPath("python3")
		rg11Must(t, err)
		for _, name := range []string{"mktemp", "cp", "python3"} {
			rg11Must(t, os.Remove(filepath.Join(f.bin, name)))
			launcher := "#!/bin/bash\nexec " + rg11Quote(python) + " " + rg11Quote(fixture) + " " + rg11Quote(name) + " \"$@\"\n"
			rg11Must(t, os.WriteFile(filepath.Join(f.bin, name), []byte(launcher), 0o700))
		}
		// The shell checks presence; the controlled Python gate handles scan calls.
		rg11Must(t, os.WriteFile(filepath.Join(f.bin, "trivy"), []byte("#!/bin/bash\nexit 97\n"), 0o700))
	case "absent":
	default:
		t.Fatalf("unknown scanner lane %q", scanner)
	}
	t.Logf("fixture scanner=%s dirty=%v original_sha256=%x", scanner, dirty, sha256.Sum256(f.original))
	return f
}

func (f *rg11ScanFixture) prepareTrivy(t *testing.T) {
	t.Helper()
	for _, invocation := range [][]string{{"trivy", "--version"}, {"git", "--version"}, {"bash", "--version"}} {
		out := rg11Command(t, f.root, f.env, filepath.Join(f.bin, invocation[0]), invocation[1:]...)
		t.Logf("prerequisite %s: %s", invocation[0], out)
	}
	out := rg11Command(t, f.root, f.env, filepath.Join(f.bin, "python3"), "deploy/scripts/assert-scan-stubs-hide-nothing.py")
	if !regexp.MustCompile(`целей[ :]+[1-9][0-9]*`).MatchString(out) {
		t.Fatalf("CONDITION NOT CREATED: prepared scan has no nonempty inspected target count: %s", out)
	}
	t.Logf("real preparation: %s", out)
}

func (f *rg11ScanFixture) run(t *testing.T, fault string) (int, string) {
	t.Helper()
	rg11Must(t, os.WriteFile(filepath.Join(f.state, "events.jsonl"), nil, 0o600))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(f.bin, "bash"), rg11Proof)
	cmd.Dir, cmd.Env = f.root, append(slices.Clone(f.env), "RG11_FAULT="+fault)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("CONDITION NOT CREATED: proof process timed out: %v\n%s", ctx.Err(), out)
	}
	rc := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("CONDITION NOT CREATED: proof did not start: %v", err)
		}
		rc = exit.ExitCode()
	}
	t.Logf("proof scanner=%s fault=%s rc=%d\n%s", f.scanner, fault, rc, out)
	return rc, string(out)
}

func (f *rg11ScanFixture) wantPreserved(t *testing.T) {
	t.Helper()
	got := rg11Read(t, filepath.Join(f.root, "trivy.yaml"))
	t.Logf("after process original_sha256=%x actual_sha256=%x original_bytes=%d actual_bytes=%d", sha256.Sum256(f.original), sha256.Sum256(got), len(f.original), len(got))
	if !bytes.Equal(got, f.original) {
		t.Error("trivy.yaml differs from the original working bytes after process exit")
	}
	if got := rg11Command(t, f.root, nil, "git", "status", "--porcelain", "--", "trivy.yaml"); got != f.status {
		t.Errorf("configuration git status changed: before=%q after=%q", f.status, got)
	}
	if got := rg11Command(t, f.root, nil, "git", "diff", "--", "trivy.yaml"); got != f.diff {
		t.Error("configuration git diff changed after process exit")
	}
}

func (f *rg11ScanFixture) events(t *testing.T) []rg11ScanEvent {
	t.Helper()
	data := rg11Read(t, filepath.Join(f.state, "events.jsonl"))
	var events []rg11ScanEvent
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var event rg11ScanEvent
		rg11Must(t, json.Unmarshal(line, &event))
		if event.Kind == "invalid_fixture_input" {
			t.Fatalf("CONDITION NOT CREATED: invalid process fixture input: %s", line)
		}
		events = append(events, event)
	}
	t.Logf("process observations: %s", data)
	return events
}

func (f *rg11ScanFixture) snapshotPath(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(string(rg11Read(t, filepath.Join(f.state, "snapshot-path"))))
}

func (f *rg11ScanFixture) wantSnapshotRemoved(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.state, "snapshot-path"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	rg11Must(t, err)
	path := strings.TrimSpace(string(data))
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("snapshot was not removed after safe completion: %s (%v)", path, err)
	}
}

func rg11ProcessTwin(t *testing.T, dirty bool) *rg11ScanFixture {
	t.Helper()
	f := rg11NewScanFixture(t, dirty, "process")
	rc, out := f.run(t, "success")
	rg11WantCode(t, rc, 0, out)
	rg11WantProof(t, out)
	f.wantPreserved(t)
	events := f.events(t)
	if rg11EventCount(events, "prepared") != 1 || rg11EventCount(events, "scan") == 0 {
		t.Fatalf("CONDITION NOT CREATED: positive twin has no prepared snapshot or scan calls: %+v", events)
	}
	for _, event := range events {
		if event.Kind == "prepared" && event.SHA256 != fmt.Sprintf("%x", sha256.Sum256(f.original)) {
			t.Fatal("positive twin snapshot does not match the working original")
		}
	}
	f.wantSnapshotRemoved(t)
	if t.Failed() {
		t.Fatal("positive process twin failed; negative cannot authorize implementation")
	}
	return f
}

func rg11EventCount(events []rg11ScanEvent, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func rg11OneFact(t *testing.T, f *rg11ScanFixture, positive, negative string) {
	t.Helper()
	before := append(slices.Clone(f.env), "RG11_FAULT="+positive)
	after := append(slices.Clone(f.env), "RG11_FAULT="+negative)
	var delta []string
	for i, value := range before {
		if after[i] != value {
			key, _, _ := strings.Cut(value, "=")
			delta = append(delta, key)
		}
	}
	if !slices.Equal(delta, []string{"RG11_FAULT"}) {
		t.Fatalf("fixture delta=%v, want only fault", delta)
	}
	f.wantPreserved(t)
	if t.Failed() {
		t.Fatal("positive twin input was not preserved")
	}
	t.Logf("positive twin=success same process fixture, computed changed facts=%v fault=%s", delta, negative)
}

func rg11WantCode(t *testing.T, got, want int, out string) {
	t.Helper()
	if got != want {
		t.Errorf("proof exit=%d, want %d\n%s", got, want, out)
	}
}

func rg11WantAbsent(t *testing.T, rc int, out string) {
	t.Helper()
	rg11WantCode(t, rc, 2, out)
	if !strings.Contains(out, "trivy") || !strings.Contains(out, "не найден") {
		t.Errorf("missing Trivy was not diagnosed: %s", out)
	}
	if strings.Contains(out, "ok      ") || strings.Contains(out, "пройдено") {
		t.Errorf("unexecuted scan claimed proof assertions: %s", out)
	}
}

func rg11WantProof(t *testing.T, out string) {
	t.Helper()
	for _, title := range []string{"законные заглушки", "гасящая заглушка", "гейт покрытия", "заглушка без предмета", "заглушек нет"} {
		if !regexp.MustCompile(`(?m)^ok +[^\n]*` + regexp.QuoteMeta(title)).MatchString(out) {
			t.Errorf("proof did not execute successful assertion %q: %s", title, out)
		}
	}
	if !regexp.MustCompile(`итог: утверждений [1-9][0-9]*; пройдено [1-9][0-9]*; провалено 0`).MatchString(out) {
		t.Errorf("missing nonempty successful proof outcome: %s", out)
	}
}

func rg11CleanEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "PATH" || key == "TMPDIR" || key == "GITHUB_ACTIONS" || key == "GITHUB_ENV" || key == "GITHUB_OUTPUT" || key == "GITHUB_STEP_SUMMARY" || strings.HasPrefix(key, "RG11_") || strings.HasPrefix(key, "TRIVY_") {
			continue
		}
		env = append(env, entry)
	}
	return env
}

func rg11Command(t *testing.T, dir string, env []string, command string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CONDITION NOT CREATED: %s %v: %v\n%s", command, args, err, out)
	}
	return string(out)
}

func rg11Must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("CONDITION NOT CREATED: %v", err)
	}
}
func rg11Read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	rg11Must(t, err)
	return data
}
func rg11Quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// This is the caller's process contract, not a second event checker in test code.
func TestRG11ScanExecution(t *testing.T) {
	required := []string{
		"TestRG11_01/real_clean", "TestRG11_02/missing_clean", "TestRG11_03/dirty_present_repeat", "TestRG11_03/dirty_absent_repeat", "TestRG11_04/failure_after_mutation",
		"TestRG11SnapshotPrepare/create_failure", "TestRG11SnapshotPrepare/copy_failure", "TestRG11SnapshotPrepare/partial_snapshot", "TestRG11SnapshotPrepare/snapshot_success",
		"TestRG11SnapshotRestore/copy_failure", "TestRG11SnapshotRestore/restore_success",
	}
	for _, variant := range []string{"full", "incomplete", "empty"} {
		t.Run(variant, func(t *testing.T) {
			var stream bytes.Buffer
			selected := required
			if variant == "incomplete" {
				selected = required[:len(required)-1]
			}
			if variant == "empty" {
				selected = nil
			}
			for _, name := range selected {
				for _, action := range []string{"run", "pass"} {
					rg11Must(t, json.NewEncoder(&stream).Encode(map[string]string{"Action": action, "Package": "github.com/PRO-Robotech/kacho/services/storage/tools", "Test": name}))
				}
			}
			input := filepath.Join(t.TempDir(), "events.jsonl")
			rg11Must(t, os.WriteFile(input, stream.Bytes(), 0o600))
			var workflow struct {
				Jobs map[string]struct {
					Steps []struct {
						ID  string `yaml:"id"`
						Run string `yaml:"run"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			rg11Must(t, yaml.Unmarshal(rg11Read(t, filepath.Join(repoRoot(t), ".github/workflows/security-scan.yml")), &workflow))
			var run string
			for _, step := range workflow.Jobs["trivy"].Steps {
				if step.ID == "rg11-scan-preservation" {
					run = step.Run
				}
			}
			if run == "" {
				t.Fatal("HOLDER NOT PRESENT: trivy step rg11-scan-preservation does not exist; this is not the configuration-preservation RED")
			}
			match := regexp.MustCompile(`(?ms)^.*<<'RG11_SCAN_EVENTS'[^\n]*\n(.*?)^RG11_SCAN_EVENTS\s*$`).FindStringSubmatch(run)
			if len(match) != 2 {
				t.Fatal("HOLDER NOT PRESENT: actual step has no RG11_SCAN_EVENTS checker heredoc")
			}
			cmd := exec.Command("python3", "-", input)
			cmd.Stdin, cmd.Env = strings.NewReader(match[1]), rg11CleanEnv()
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if err != nil && !errors.As(err, &exit) {
				t.Fatalf("CONDITION NOT CREATED: caller checker did not start: %v", err)
			}
			if (err == nil) != (variant == "full") {
				t.Errorf("caller checker variant=%s err=%v\n%s", variant, err, out)
			}
			t.Logf("caller variant=%s bytes=%d result=%v\n%s", variant, stream.Len(), err, out)
		})
	}
}
