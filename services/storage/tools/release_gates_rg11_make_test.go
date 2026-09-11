// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package tools_regression

// verifies https://github.com/PRO-Robotech/kacho/issues/2550
// RG1.1-11..18: workflow input reaches the existing CI make gate unchanged.
// The subprocess copies the gate, never its parser or Makefile predicate, and
// supplies only scriptDir so its ordinary repoRoot walk reaches the fixture.

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type rg11MakeTuple struct {
	Workflow string `json:"workflow"`
	Job      string `json:"job"`
	Step     int    `json:"step"`
	Dir      string `json:"dir"`
	Target   string `json:"target"`
	Wrapped  bool   `json:"wrapped"`
	RunSHA   string `json:"run_sha256,omitempty"`
}

type rg11MakeInventory struct {
	Base      string            `json:"base_sha"`
	CorpusSHA string            `json:"corpus_sha256"`
	ReaderSHA string            `json:"source_reader_sha256"`
	Files     map[string]string `json:"files"`
	Old       []rg11MakeTuple   `json:"old"`
	Wrapped   []rg11MakeTuple   `json:"wrapped"`
}

type rg11MakeEvidence struct {
	Started    bool              `json:"started"`
	Output     string            `json:"output"`
	Exit       int               `json:"exit"`
	Error      string            `json:"error,omitempty"`
	Command    []string          `json:"command"`
	Directory  string            `json:"directory"`
	Root       string            `json:"fixture_root"`
	ReaderSHA  string            `json:"reader_sha256"`
	FixtureSHA map[string]string `json:"fixture_sha256"`
}

func rg11MakeSHA(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw)) }

func rg11MakeRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func rg11MakeWrite(t *testing.T, root, name string, raw []byte) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// rg11MakeLive validates independently transcribed coordinates against the
// immutable, complete workflow corpus before asking the reader any question.
func rg11MakeLive(t *testing.T) (map[string]string, rg11MakeInventory) {
	t.Helper()
	dir := filepath.Join(scriptDir(t), "testdata", "rg11-make")
	var inventory rg11MakeInventory
	if err := json.Unmarshal(rg11MakeRead(t, filepath.Join(dir, "inventory.json")), &inventory); err != nil {
		t.Fatal(err)
	}
	z, err := gzip.NewReader(bytes.NewReader(rg11MakeRead(t, filepath.Join(dir, "live-corpus.json.gz"))))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if rg11MakeSHA(raw) != inventory.CorpusSHA {
		t.Fatal("fixture corpus digest differs from its capture")
	}
	var files map[string]string
	if err := json.Unmarshal(raw, &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != len(inventory.Files) || len(inventory.Old) == 0 || len(inventory.Wrapped) == 0 {
		t.Fatal("incomplete independent inventory")
	}
	for name, content := range files {
		if rg11MakeSHA([]byte(content)) != inventory.Files[name] {
			t.Fatalf("fixture digest differs: %s", name)
		}
	}
	for _, tuple := range append(append([]rg11MakeTuple{}, inventory.Old...), inventory.Wrapped...) {
		var wf struct {
			Jobs map[string]struct {
				Steps []struct {
					Run string `yaml:"run"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal([]byte(files[".github/workflows/"+tuple.Workflow]), &wf); err != nil {
			t.Fatal(err)
		}
		job, exists := wf.Jobs[tuple.Job]
		if !exists || tuple.Step < 0 || tuple.Step >= len(job.Steps) {
			t.Fatalf("inventory coordinate has no actual step: %+v", tuple)
		}
		run := job.Steps[tuple.Step].Run
		if rg11MakeSHA([]byte(run)) != tuple.RunSHA || !strings.Contains(run, tuple.Target) {
			t.Fatalf("inventory differs from actual step: %+v", tuple)
		}
	}
	var baseline struct {
		ReaderSHA   string          `json:"reader_sha256"`
		CorpusSHA   string          `json:"corpus_sha256"`
		Invocations []rg11MakeTuple `json:"invocations"`
	}
	if err := json.Unmarshal(rg11MakeRead(t, filepath.Join(dir, "old-reader-capture.json")), &baseline); err != nil {
		t.Fatal(err)
	}
	if baseline.ReaderSHA != inventory.ReaderSHA || baseline.CorpusSHA != inventory.CorpusSHA ||
		!reflect.DeepEqual(rg11MakeKeys(baseline.Invocations), rg11MakeKeys(inventory.Old)) {
		t.Fatal("actual OLD collector capture does not equal independent workflow/job/step/dir/target inventory")
	}
	return files, inventory
}

func rg11MakeWorkflow(t *testing.T, run, level string) string {
	t.Helper()
	step := map[string]any{"run": run}
	job := map[string]any{"runs-on": "ubuntu-latest", "steps": []any{step}}
	wf := map[string]any{"name": "RG1.1 make fixture", "on": "push", "jobs": map[string]any{"probe": job}}
	defaults := map[string]any{"run": map[string]any{"working-directory": "services/storage"}}
	switch level {
	case "step":
		step["working-directory"] = "services/storage"
	case "job":
		job["defaults"] = defaults
	case "workflow":
		wf["defaults"] = defaults
	}
	raw, err := yaml.Marshal(wf)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func rg11MakeFixture(t *testing.T, run, level string) map[string]string {
	t.Helper()
	files, _ := rg11MakeLive(t)
	return map[string]string{
		".github/workflows/rg11.yml":  rg11MakeWorkflow(t, run, level),
		".github/scripts/stand-up.sh": files[".github/scripts/stand-up.sh"],
		"Makefile":                    "root-only:\n\t@:\n",
		"deploy/Makefile":             "helm-deps:\n\t@:\ndev-up:\n\t@:\n",
		"services/storage/Makefile":   "audit-list-filter:\n\t@:\naudit-known-failing:\n\t@:\n",
		"rg1-empty-dir/.keep":         "",
	}
}

func rg11MakeSingle(t *testing.T) map[string]string {
	t.Helper()
	files, _ := rg11MakeLive(t)
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(files[".github/workflows/ci.yaml"]), &wf); err != nil {
		t.Fatal(err)
	}
	return rg11MakeFixture(t, wf.Jobs["helm"].Steps[21].Run, "")
}

// The delta is calculated over bytes before the subprocess starts. A negative
// fixture may replace one occurrence in one named file and nothing else.
func rg11MakeDelta(t *testing.T, good map[string]string, name, before, after string) map[string]string {
	t.Helper()
	if before == "" || before == after || strings.Count(good[name], before) != 1 {
		t.Fatalf("fixture delta is not one unique replacement: %q", before)
	}
	bad := make(map[string]string, len(good))
	for name, content := range good {
		bad[name] = content
	}
	position := strings.Index(good[name], before)
	bad[name] = good[name][:position] + after + good[name][position+len(before):]
	changed := 0
	for path, content := range good {
		if content != bad[path] {
			changed++
		}
	}
	// The replacement text may already occur in a comment. Undo the captured
	// occurrence, so an earlier identical literal cannot intercept the guard.
	restored := bad[name][:position] + before + bad[name][position+len(after):]
	if changed != 1 || restored != good[name] {
		t.Fatal("fixture changed more than its declared fact")
	}
	t.Logf("computed delta: %s: %q -> %q; changed files=%d", name, before, after, changed)
	return bad
}

const rg11MakeSupport = `package tools_regression
import (
 "encoding/json"
 "fmt"
 "os"
 "reflect"
 "regexp"
 "strconv"
 "testing"
)
func scriptDir(t *testing.T) string {
 t.Helper()
 root := os.Getenv("RG11_MAKE_FIXTURE_ROOT")
 if root == "" { t.Fatal("fixture root was not supplied") }
 return root
}
func TestRG11SnapshotCollector(t *testing.T) {
 invocations := collectMakeInvocations(t, scriptDir(t))
 type tuple struct { Workflow string ` + "`json:\"workflow\"`" + `; Job string ` + "`json:\"job\"`" + `; Step int ` + "`json:\"step\"`" + `; Dir string ` + "`json:\"dir\"`" + `; Target string ` + "`json:\"target\"`" + `; Wrapped bool ` + "`json:\"wrapped\"`" + ` }
 out := make([]tuple, 0, len(invocations))
 coordinate := regexp.MustCompile("^(.*) job (.*) step ([0-9]+)$")
 for _, inv := range invocations {
  match := coordinate.FindStringSubmatch(inv.workflow)
  if match == nil { t.Fatalf("collector lost workflow/job/step: %+v", inv) }
  step, err := strconv.Atoi(match[3]); if err != nil { t.Fatal(err) }
  // An absent provenance field is observable missing coverage, not a compile
  // failure. This keeps the regression executable against the original reader.
  field := reflect.ValueOf(inv).FieldByName("wrapped")
  wrapped := field.IsValid() && field.Kind() == reflect.Bool && field.Bool()
  out = append(out, tuple{match[1], match[2], step, inv.dir, inv.target, wrapped})
 }
 raw, err := json.Marshal(out); if err != nil { t.Fatal(err) }
 fmt.Printf("RG11_CAPTURE=%s\n", raw)
}
`

func rg11MakeRun(t *testing.T, files map[string]string) rg11MakeEvidence {
	t.Helper()
	root, pkg := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".github/workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	fileSHA := make(map[string]string, len(files))
	for name, content := range files {
		if filepath.IsAbs(name) || strings.HasPrefix(filepath.Clean(name), "..") {
			t.Fatalf("fixture path escapes owned root: %s", name)
		}
		if strings.HasPrefix(name, ".github/workflows/") {
			var parsed any
			if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
				t.Fatalf("invalid fixture YAML: %v", err)
			}
		}
		rg11MakeWrite(t, root, name, []byte(content))
		fileSHA[name] = rg11MakeSHA([]byte(content))
	}
	source := rg11MakeRead(t, filepath.Join(scriptDir(t), "ci_gate_wiring_test.go"))
	rg11MakeWrite(t, pkg, "ci_gate_wiring_test.go", source)
	if !bytes.Equal(source, rg11MakeRead(t, filepath.Join(pkg, "ci_gate_wiring_test.go"))) {
		t.Fatal("gate copy differs from product bytes")
	}
	rg11MakeWrite(t, pkg, "fixture_test.go", []byte(rg11MakeSupport))
	rg11MakeWrite(t, pkg, "go.mod", []byte("module rg11fixture\n\ngo 1.26.0\n\nrequire gopkg.in/yaml.v3 v3.0.1\n"))
	rg11MakeWrite(t, pkg, "go.sum", rg11MakeRead(t, filepath.Join(repoRoot(t), "go.sum")))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-v", "-count=1", "-run", "^(TestEveryCIMakeCommandResolves|TestRG11SnapshotCollector)$", ".")
	cmd.Dir = pkg
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "RG11_MAKE_FIXTURE_ROOT=") && !strings.HasPrefix(entry, "GOWORK=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GOWORK=off", "RG11_MAKE_FIXTURE_ROOT="+root)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	evidence := rg11MakeEvidence{Exit: -1, Command: cmd.Args, Directory: pkg, Root: root, ReaderSHA: rg11MakeSHA(source), FixtureSHA: fileSHA}
	err := cmd.Start()
	if err == nil {
		evidence.Started = true
		err = cmd.Wait()
		if cmd.ProcessState != nil {
			evidence.Exit = cmd.ProcessState.ExitCode()
		}
	}
	if err != nil {
		evidence.Error = err.Error()
	}
	if ctx.Err() != nil {
		evidence.Error = ctx.Err().Error()
	}
	evidence.Output = output.String()
	if dir := os.Getenv("RG11_MAKE_EVIDENCE_DIR"); dir != "" {
		fixtureJSON, err := json.Marshal(fileSHA)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.ReplaceAll(t.Name(), "/", "__") + "__" + rg11MakeSHA(fixtureJSON)[:12]
		raw, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		rg11MakeWrite(t, dir, name+".json", raw)
		rg11MakeWrite(t, dir, name+".stdout", []byte(evidence.Output))
	}
	t.Logf("gate source sha256=%s, external go test exit=%d\n%s", evidence.ReaderSHA, evidence.Exit, evidence.Output)
	return evidence
}

func rg11MakeOutcome(e rg11MakeEvidence) (string, string) {
	if !e.Started {
		return "NOT_EXECUTED", "checking process did not start: " + e.Error
	}
	if strings.TrimSpace(e.Output) == "" {
		return "NOT_EXECUTED", "checking output was not captured"
	}
	if !strings.Contains(e.Output, "=== RUN   TestEveryCIMakeCommandResolves") ||
		(!strings.Contains(e.Output, "--- PASS: TestEveryCIMakeCommandResolves") && !strings.Contains(e.Output, "--- FAIL: TestEveryCIMakeCommandResolves")) {
		return "NOT_EXECUTED", "checking test did not finish: " + e.Error
	}
	switch e.Exit {
	case 0:
		return "GREEN", ""
	case 1:
		return "RED", ""
	default:
		return "NOT_EXECUTED", fmt.Sprintf("unexpected process exit %d: %s", e.Exit, e.Error)
	}
}

func rg11MakeTuples(t *testing.T, e rg11MakeEvidence) []rg11MakeTuple {
	t.Helper()
	matches := regexp.MustCompile(`(?m)^RG11_CAPTURE=(.*)$`).FindAllStringSubmatch(e.Output, -1)
	if len(matches) != 1 {
		t.Fatalf("expected one collector capture, got %d", len(matches))
	}
	var tuples []rg11MakeTuple
	if err := json.Unmarshal([]byte(matches[0][1]), &tuples); err != nil {
		t.Fatal(err)
	}
	return tuples
}

func rg11MakeKeys(tuples []rg11MakeTuple) []string {
	keys := make([]string, 0, len(tuples))
	for _, x := range tuples {
		keys = append(keys, fmt.Sprintf("%s\t%s\t%d\t%s\t%s", x.Workflow, x.Job, x.Step, x.Dir, x.Target))
	}
	sort.Strings(keys)
	return keys
}

func rg11MakeCheck(t *testing.T, files map[string]string, exit int, expected []rg11MakeTuple, fragments ...string) rg11MakeEvidence {
	t.Helper()
	e := rg11MakeRun(t, files)
	if outcome, reason := rg11MakeOutcome(e); outcome == "NOT_EXECUTED" {
		t.Fatalf("NOT_EXECUTED: %s", reason)
	}
	if e.Exit != exit {
		t.Errorf("external go test exit=%d, want %d", e.Exit, exit)
	}
	actual := rg11MakeTuples(t, e)
	if !reflect.DeepEqual(rg11MakeKeys(actual), rg11MakeKeys(expected)) {
		t.Errorf("tuple multiset differs\ngot  %v\nwant %v", rg11MakeKeys(actual), rg11MakeKeys(expected))
	}
	wrapped := 0
	for _, x := range expected {
		if x.Wrapped {
			wrapped++
		}
	}
	actualWrapped := 0
	for _, x := range actual {
		if x.Wrapped {
			actualWrapped++
		}
	}
	if actualWrapped != wrapped {
		t.Errorf("collector under stand-up=%d, want %d", actualWrapped, wrapped)
	}
	census := regexp.MustCompile(`checked ([0-9]+) make command\(s\) issued by CI; under stand-up: ([0-9]+)`).FindAllStringSubmatch(e.Output, -1)
	if len(census) != 1 {
		t.Errorf("gate must print exactly one total and stand-up census, got %d", len(census))
	} else {
		total, _ := strconv.Atoi(census[0][1])
		under, _ := strconv.Atoi(census[0][2])
		if total != len(expected) || under != wrapped {
			t.Errorf("gate census total=%d wrapped=%d; want %d %d", total, under, len(expected), wrapped)
		}
	}
	for _, fragment := range fragments {
		if !strings.Contains(e.Output, fragment) {
			t.Errorf("diagnostic missing %q", fragment)
		}
	}
	return e
}

func rg11MakeOne(dir, target string) []rg11MakeTuple {
	return []rg11MakeTuple{{Workflow: "rg11.yml", Job: "probe", Step: 0, Dir: dir, Target: target, Wrapped: true}}
}

func TestRG11_11(t *testing.T) {
	t.Run("single_valid", func(t *testing.T) { rg11MakeCheck(t, rg11MakeSingle(t), 0, rg11MakeOne("deploy", "helm-deps")) })
}

func TestRG11_12(t *testing.T) {
	t.Run("single_invalid", func(t *testing.T) {
		good := rg11MakeSingle(t)
		rg11MakeCheck(t, good, 0, rg11MakeOne("deploy", "helm-deps"))
		bad := rg11MakeDelta(t, good, ".github/workflows/rg11.yml", "make helm-deps", "make rg1-nonexistent-target")
		rg11MakeCheck(t, bad, 1, rg11MakeOne("deploy", "rg1-nonexistent-target"), "rg11.yml job probe step 0", "deploy", "rg1-nonexistent-target", "no such target")
	})
}

func TestRG11_13(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		name := "continued_valid"
		if invalid {
			name = "continued_invalid"
		}
		t.Run(name, func(t *testing.T) {
			good := rg11MakeFixture(t, ".github/scripts/stand-up.sh --label \"RG1.1 continuation\" --dir deploy -- \\\n  make helm-deps \\\n    IMAGES=\"${{ matrix.shard.images }}\" \\\n    BUILD_UI=\"${{ matrix.shard.build_ui }}\"\n", "")
			rg11MakeCheck(t, good, 0, rg11MakeOne("deploy", "helm-deps"))
			if invalid {
				bad := rg11MakeDelta(t, good, ".github/workflows/rg11.yml", "make helm-deps", "make rg1-nonexistent-target")
				rg11MakeCheck(t, bad, 1, rg11MakeOne("deploy", "rg1-nonexistent-target"), "rg11.yml job probe step 0", "deploy", "no such target")
			}
		})
	}
}

func TestRG11_14(t *testing.T) {
	t.Run("wrong_directory", func(t *testing.T) {
		good := rg11MakeSingle(t)
		rg11MakeCheck(t, good, 0, rg11MakeOne("deploy", "helm-deps"))
		bad := rg11MakeDelta(t, good, ".github/workflows/rg11.yml", "--dir deploy", "--dir services/storage")
		rg11MakeCheck(t, bad, 1, rg11MakeOne("services/storage", "helm-deps"), "rg11.yml job probe step 0", "services/storage", "helm-deps", "no such target")
	})
}

func TestRG11_15(t *testing.T) {
	for _, level := range []string{"step", "job", "workflow"} {
		for _, invalid := range []bool{false, true} {
			name := level + "_valid"
			if invalid {
				name = level + "_invalid"
			}
			t.Run(name, func(t *testing.T) {
				good := rg11MakeFixture(t, "../../.github/scripts/stand-up.sh --label \"RG1.1 directory\" --dir ../../deploy -- make helm-deps", level)
				rg11MakeCheck(t, good, 0, rg11MakeOne("deploy", "helm-deps"))
				if invalid {
					bad := rg11MakeDelta(t, good, ".github/workflows/rg11.yml", "make helm-deps", "make rg1-nonexistent-target")
					rg11MakeCheck(t, bad, 1, rg11MakeOne("deploy", "rg1-nonexistent-target"), "rg11.yml job probe step 0", "deploy", "no such target")
				}
			})
		}
	}
}

func TestRG11_16(t *testing.T) {
	t.Run("missing_makefile", func(t *testing.T) {
		good := rg11MakeSingle(t)
		rg11MakeCheck(t, good, 0, rg11MakeOne("deploy", "helm-deps"))
		bad := rg11MakeDelta(t, good, ".github/workflows/rg11.yml", "--dir deploy", "--dir rg1-empty-dir")
		rg11MakeCheck(t, bad, 1, rg11MakeOne("rg1-empty-dir", "helm-deps"), "rg11.yml job probe step 0", "rg1-empty-dir", "no Makefile")
	})
}

func rg11MakePlain(t *testing.T) (map[string]string, []rg11MakeTuple) {
	t.Helper()
	// Forms captured from ci.yaml authz-artifacts step 7 and console-e2e.yml
	// probes step 24: loop/-C/redirection and a quoted NAME=value override.
	run := "# make rg1-nonexistent-target\nfor svc in storage; do\n  make -C \"services/${svc}\" audit-list-filter >\"${logs}/${svc}.out\" 2>&1 && rc=0 || rc=$?\ndone\nmake -C deploy dev-up CLUSTER_NAME=\"$CLUSTER_NAME\"\n"
	expected := []rg11MakeTuple{{Workflow: "rg11.yml", Job: "probe", Step: 0, Dir: "services/storage", Target: "audit-list-filter"}, {Workflow: "rg11.yml", Job: "probe", Step: 0, Dir: "deploy", Target: "dev-up"}}
	return rg11MakeFixture(t, run, ""), expected
}

func TestRG11_17(t *testing.T) {
	t.Run("live_census", func(t *testing.T) {
		files, inventory := rg11MakeLive(t)
		expected := append([]rg11MakeTuple{}, inventory.Old...)
		for _, x := range inventory.Wrapped {
			x.Wrapped = true
			expected = append(expected, x)
		}
		rg11MakeCheck(t, files, 0, expected)
	})
	t.Run("plain_valid", func(t *testing.T) { files, expected := rg11MakePlain(t); rg11MakeCheck(t, files, 0, expected) })
	t.Run("plain_invalid", func(t *testing.T) {
		good, expected := rg11MakePlain(t)
		rg11MakeCheck(t, good, 0, expected)
		bad := rg11MakeDelta(t, good, ".github/workflows/rg11.yml", "audit-list-filter", "rg1-nonexistent-target")
		expected[0].Target = "rg1-nonexistent-target"
		rg11MakeCheck(t, bad, 1, expected, "rg11.yml job probe step 0", "services/storage", "no such target")
	})
}

func rg11MakeCoverage(total, wrapped, wantTotal, wantWrapped int) error {
	if total == 0 || wantTotal == 0 || wantWrapped == 0 {
		return fmt.Errorf("empty coverage subject")
	}
	if total != wantTotal || wrapped != wantWrapped {
		return fmt.Errorf("coverage total=%d wrapped=%d, want total=%d wrapped=%d", total, wrapped, wantTotal, wantWrapped)
	}
	return nil
}

func TestRG11_18(t *testing.T) {
	t.Run("no_workflows", func(t *testing.T) {
		good := rg11MakeSingle(t)
		rg11MakeCheck(t, good, 0, rg11MakeOne("deploy", "helm-deps"))
		bad := make(map[string]string, len(good)-1)
		for name, content := range good {
			if name != ".github/workflows/rg11.yml" {
				bad[name] = content
			}
		}
		if len(good)-len(bad) != 1 {
			t.Fatal("expected exactly one removed workflow")
		}
		e := rg11MakeRun(t, bad)
		if category, reason := rg11MakeOutcome(e); category != "RED" {
			t.Fatalf("want executed refusal, got %s: %s", category, reason)
		}
		if !strings.Contains(e.Output, "no workflow files read") {
			t.Fatal("missing empty-workflow diagnostic")
		}
	})
	t.Run("comment_only", func(t *testing.T) {
		good := rg11MakeSingle(t)
		rg11MakeCheck(t, good, 0, rg11MakeOne("deploy", "helm-deps"))
		bad := make(map[string]string, len(good))
		for name, content := range good {
			bad[name] = content
		}
		// YAML quotes delimit the string; the only semantic change is the shell
		// comment marker. Construct it through YAML to keep the fixture valid.
		var wf map[string]any
		if err := yaml.Unmarshal([]byte(good[".github/workflows/rg11.yml"]), &wf); err != nil {
			t.Fatal(err)
		}
		jobs := wf["jobs"].(map[string]any)
		job := jobs["probe"].(map[string]any)
		step := job["steps"].([]any)[0].(map[string]any)
		originalRun := step["run"].(string)
		step["run"] = "# " + originalRun
		raw, err := yaml.Marshal(wf)
		if err != nil {
			t.Fatal(err)
		}
		bad[".github/workflows/rg11.yml"] = string(raw)
		step["run"] = originalRun
		restored, err := yaml.Marshal(wf)
		if err != nil {
			t.Fatal(err)
		}
		if string(restored) != good[".github/workflows/rg11.yml"] {
			t.Fatal("comment fixture changed another YAML fact")
		}
		t.Log("computed semantic delta: jobs.probe.steps[0].run gains one shell comment prefix")
		e := rg11MakeRun(t, bad)
		if category, reason := rg11MakeOutcome(e); category != "RED" {
			t.Fatalf("want executed refusal, got %s: %s", category, reason)
		}
		if !strings.Contains(e.Output, "no `make` commands found") {
			t.Fatal("missing empty-command diagnostic")
		}
	})
	t.Run("zero_wrapped", func(t *testing.T) {
		files, inventory := rg11MakeLive(t)
		e := rg11MakeRun(t, files)
		if category, reason := rg11MakeOutcome(e); category == "NOT_EXECUTED" {
			t.Fatalf("%s: %s", category, reason)
		}
		tuples := rg11MakeTuples(t, e)
		wrapped := 0
		for _, tuple := range tuples {
			if tuple.Wrapped {
				wrapped++
			}
		}
		wantTotal := len(inventory.Old) + len(inventory.Wrapped)
		if err := rg11MakeCoverage(len(tuples), wrapped, wantTotal, len(inventory.Wrapped)); err != nil {
			t.Fatalf("positive census control: %v", err)
		}
		goodCensus, badCensus := [2]int{len(tuples), wrapped}, [2]int{len(tuples), 0}
		delta := 0
		for i := range goodCensus {
			if goodCensus[i] != badCensus[i] {
				delta++
			}
		}
		if delta != 1 {
			t.Fatal("zero-wrapper evidence did not change exactly one census field")
		}
		if err := rg11MakeCoverage(len(tuples), 0, wantTotal, len(inventory.Wrapped)); err == nil {
			t.Fatal("holder accepted zero wrappers with a nonempty independent list")
		}
		t.Logf("computed census delta=%d; zero-wrapper report refused", delta)
	})
	for _, kind := range []string{"no_process", "no_output"} {
		t.Run(kind, func(t *testing.T) {
			files, _ := rg11MakePlain(t)
			good := rg11MakeRun(t, files)
			if category, reason := rg11MakeOutcome(good); category != "GREEN" {
				t.Fatalf("positive evidence control: %s: %s", category, reason)
			}
			bad := good
			if kind == "no_process" {
				bad.Started = false
			} else {
				bad.Output = ""
			}
			before, after := reflect.ValueOf(good), reflect.ValueOf(bad)
			delta := 0
			for i := 0; i < before.NumField(); i++ {
				if !reflect.DeepEqual(before.Field(i).Interface(), after.Field(i).Interface()) {
					delta++
				}
			}
			if delta != 1 {
				t.Fatal("evidence must change exactly one fact")
			}
			category, reason := rg11MakeOutcome(bad)
			if category != "NOT_EXECUTED" || reason == "" {
				t.Fatalf("missing evidence accepted: %s %s", category, reason)
			}
			t.Logf("computed evidence delta=%d; missing evidence outcome=%s; reason=%s", delta, category, reason)
		})
	}
}

// TestRG11MakeDesignForms exercises the D5/D6 parsing and cwd decisions supporting
// RG1.1-11/13/15/17. Every supported form retains its one-target negative twin.
func TestRG11MakeDesignForms(t *testing.T) {
	// D5 requires a coordinate-bearing refusal for an unsupported use of the
	// canonical wrapper. An ordinary make command must not hide its omission.
	for _, tc := range []struct{ name, wrappedLine string }{
		{"unsupported_bash_option_prefix", "bash -e .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps"},
		{"unsupported_if_prefix", "if .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps; then :; fi"},
		{"unsupported_bash_cluster_eo", "bash -eo pipefail .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps"},
		{"unsupported_bash_cluster_euo", "bash -euo pipefail .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps"},
		{"unsupported_bash_cluster_eO", "bash -eO extglob .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps"},
		{"unsupported_assignment_prefix", "RG11_CHECK=1 .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps"},
		{"unsupported_exec_prefix", "exec .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps"},
		{"unsupported_command_prefix", "command .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps"},
		{"unsupported_env_prefix", "env RG11_CHECK=1 .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps"},
		{"unsupported_group_prefix", "{ .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps; }"},
		{"unsupported_redirect_prefix", ">probe.out .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const directLine = ".github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make helm-deps"
			good := rg11MakeFixture(t, directLine+"\nmake root-only\n", "")
			expected := append(rg11MakeOne("deploy", "helm-deps"), rg11MakeTuple{Workflow: "rg11.yml", Job: "probe", Step: 0, Dir: ".", Target: "root-only"})
			rg11MakeCheck(t, good, 0, expected)
			bad := rg11MakeDelta(t, good, ".github/workflows/rg11.yml", "helm-deps", "rg1-nonexistent-target")
			expected[0].Target = "rg1-nonexistent-target"
			rg11MakeCheck(t, bad, 1, expected, "rg11.yml job probe step 0", "deploy", "no such target")

			unsupported := rg11MakeDelta(t, good, ".github/workflows/rg11.yml", directLine, tc.wrappedLine)
			unsupportedBad := rg11MakeDelta(t, unsupported, ".github/workflows/rg11.yml", "helm-deps", "rg1-nonexistent-target")
			for _, fixture := range []map[string]string{unsupported, unsupportedBad} {
				e := rg11MakeRun(t, fixture)
				category, reason := rg11MakeOutcome(e)
				if category == "NOT_EXECUTED" {
					t.Fatalf("NOT_EXECUTED: %s", reason)
				}
				if category != "RED" {
					t.Errorf("unsupported canonical wrapper must be refused before target resolution, got %s", category)
				}
				if !strings.Contains(e.Output, "rg11.yml job probe step 0") || !strings.Contains(strings.ToLower(e.Output), "unsupported") {
					t.Error("unsupported canonical wrapper refusal must explain the unsupported form and name workflow/job/step")
				}
			}
		})
	}
	for _, tc := range []struct{ name, run, level string }{
		{"quoted_label", `bash ".github/scripts/stand-up.sh" --label 'RG1.1 ; make label-only | > # text' --dir 'deploy' -- make helm-deps`, ""},
		{"escaped_label", `./.github/scripts/stand-up.sh --label RG1.1\;label --dir deploy -- make helm-deps`, ""},
		{"child_make_C", `.github/scripts/stand-up.sh --label "RG1.1" --dir services/storage -- make -C ../../deploy helm-deps`, ""},
		{"child_make_C_attached", `.github/scripts/stand-up.sh --label "RG1.1" --dir services/storage -- make -C../../deploy helm-deps`, ""},
		{"without_dir", `../../.github/scripts/stand-up.sh --label "RG1.1" -- make -C ../../deploy helm-deps`, "step"},
		{"redirect", `.github/scripts/stand-up.sh --label "RG1.1" --dir deploy -- make helm-deps >"${logs}/result" 2>&1`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			good := rg11MakeFixture(t, tc.run, tc.level)
			rg11MakeCheck(t, good, 0, rg11MakeOne("deploy", "helm-deps"))
			bad := rg11MakeDelta(t, good, ".github/workflows/rg11.yml", "helm-deps", "rg1-nonexistent-target")
			rg11MakeCheck(t, bad, 1, rg11MakeOne("deploy", "rg1-nonexistent-target"), "rg11.yml job probe step 0", "deploy", "no such target")
		})
	}
	t.Run("working_directory_precedence", func(t *testing.T) {
		for _, level := range []string{"step", "job"} {
			t.Run(level, func(t *testing.T) {
				good := rg11MakeFixture(t, `../../.github/scripts/stand-up.sh --label "RG1.1" --dir ../../deploy -- make helm-deps`, level)
				var wf map[string]any
				if err := yaml.Unmarshal([]byte(good[".github/workflows/rg11.yml"]), &wf); err != nil {
					t.Fatal(err)
				}
				wf["defaults"] = map[string]any{"run": map[string]any{"working-directory": "rg1-empty-dir"}}
				if level == "step" {
					wf["jobs"].(map[string]any)["probe"].(map[string]any)["defaults"] = map[string]any{"run": map[string]any{"working-directory": "deploy"}}
				}
				raw, err := yaml.Marshal(wf)
				if err != nil {
					t.Fatal(err)
				}
				good[".github/workflows/rg11.yml"] = string(raw)
				rg11MakeCheck(t, good, 0, rg11MakeOne("deploy", "helm-deps"))
				bad := rg11MakeDelta(t, good, ".github/workflows/rg11.yml", "helm-deps", "rg1-nonexistent-target")
				rg11MakeCheck(t, bad, 1, rg11MakeOne("deploy", "rg1-nonexistent-target"), "deploy", "no such target")
			})
		}
	})
	for _, tc := range []struct{ name, before, after string }{
		{"repeated_C", "-- make -C deploy", "-- make -C deploy -C ."},
		{"missing_label_value", "--label RG1.1 --dir .", "--label --dir ."},
		{"dynamic_directory", "--dir .", "--dir ${RG11_UNKNOWN_DIRECTORY}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			good := rg11MakeFixture(t, ".github/scripts/stand-up.sh --label RG1.1 --dir . -- make -C deploy helm-deps\nmake root-only\n", "")
			expected := append(rg11MakeOne("deploy", "helm-deps"), rg11MakeTuple{Workflow: "rg11.yml", Job: "probe", Step: 0, Dir: ".", Target: "root-only"})
			rg11MakeCheck(t, good, 0, expected)
			bad := rg11MakeDelta(t, good, ".github/workflows/rg11.yml", tc.before, tc.after)
			e := rg11MakeRun(t, bad)
			if category, reason := rg11MakeOutcome(e); category != "RED" {
				t.Errorf("unsupported recognized wrapper must be refused, got %s: %s", category, reason)
			}
			if !strings.Contains(e.Output, "rg11.yml job probe step 0") {
				t.Error("unsupported wrapper refusal lost its coordinate")
			}
		})
	}
	for _, tc := range []struct{ name, run string }{
		{"basename_impostor", "scripts/stand-up.sh --label RG1.1 --dir deploy -- make rg1-nonexistent-target\nmake root-only\n"},
		{"self_test", "bash .github/scripts/stand-up.sh --self-test\nmake root-only\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := []rg11MakeTuple{{Workflow: "rg11.yml", Job: "probe", Step: 0, Dir: ".", Target: "root-only"}}
			rg11MakeCheck(t, rg11MakeFixture(t, tc.run, ""), 0, expected)
		})
	}
	for _, tc := range []struct{ name, run string }{
		{"mention_printf", "printf '%s\\n' .github/scripts/stand-up.sh\nmake root-only\n"},
		{"mention_echo", "echo .github/scripts/stand-up.sh\nmake root-only\n"},
		{"mention_other_script", "bash ./rg11-other.sh .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make rg1-nonexistent-target\nmake root-only\n"},
		{"mention_bash_c_args", "bash -c ':' .github/scripts/stand-up.sh --label RG1.1 --dir deploy -- make rg1-nonexistent-target\nmake root-only\n"},
		{"mention_command_v", "command -v .github/scripts/stand-up.sh\nmake root-only\n"},
		{"mention_command_V", "command -V .github/scripts/stand-up.sh\nmake root-only\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := rg11MakeFixture(t, tc.run, "")
			if tc.name == "mention_other_script" {
				fixture["rg11-other.sh"] = "#!/usr/bin/env bash\nprintf 'OTHER_SCRIPT_EXECUTED\\n'\n"
			}
			expected := []rg11MakeTuple{{Workflow: "rg11.yml", Job: "probe", Step: 0, Dir: ".", Target: "root-only"}}
			rg11MakeCheck(t, fixture, 0, expected)
		})
	}
}
