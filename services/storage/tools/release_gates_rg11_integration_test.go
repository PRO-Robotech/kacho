// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package tools_regression

// RG1.1-05…10 exercise delivery of a captured producer result through the real
// CI command, Makefile, xargs and classifier. They do not run the VPC database
// tests. The only substituted command is GO, the Makefile's existing seam.
// verifies https://github.com/PRO-Robotech/kacho/issues/2580

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
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const rg11IntegrationPackage = "github.com/PRO-Robotech/kacho/services/vpc/internal/repo"

type rg11IntegrationInput struct {
	PostgresAvailable bool
	RemoveMarker      bool
	ExitOverride      string
	GitHubActions     string
	EmptySelection    bool
}

func rg11UnmetInput() rg11IntegrationInput {
	return rg11IntegrationInput{GitHubActions: "true"}
}

func rg11GreenInput() rg11IntegrationInput {
	f := rg11UnmetInput()
	f.PostgresAvailable = true
	return f
}

func TestRG11_05(t *testing.T) {
	t.Run("unmet_ci", func(t *testing.T) {
		f := rg11NewIntegrationFixture(t)
		green, unmet := rg11GreenInput(), rg11UnmetInput()
		rg11IntegrationDelta(t, green, unmet, "PostgresAvailable", "TestRG11_06/green_ci")
		f.check(t, "green_twin", green, 0, 0, false)
		got := f.check(t, "unmet_ci", unmet, 2, 75, true)
		rg11CheckAnnotation(t, got.output)
	})
}

func TestRG11_06(t *testing.T) {
	t.Run("green_ci", func(t *testing.T) {
		f := rg11NewIntegrationFixture(t)
		got := f.check(t, "green_ci", rg11GreenInput(), 0, 0, false)
		rg11NoAnnotation(t, got.output)
	})
}

func TestRG11_07(t *testing.T) {
	t.Run("ordinary_go1", func(t *testing.T) {
		f := rg11NewIntegrationFixture(t)
		twin, input := rg11UnmetInput(), rg11UnmetInput()
		input.RemoveMarker = true
		rg11IntegrationDelta(t, twin, input, "RemoveMarker", "TestRG11_05/unmet_ci")
		f.check(t, "unmet_twin", twin, 2, 75, true)
		got := f.check(t, "ordinary_go1", input, 2, 123, false)
		rg11NoAnnotation(t, got.output)
	})
	t.Run("ordinary_go2", func(t *testing.T) {
		f := rg11NewIntegrationFixture(t)
		twin := rg11UnmetInput()
		twin.RemoveMarker = true
		input := twin
		input.ExitOverride = "2"
		rg11IntegrationDelta(t, twin, input, "ExitOverride", "TestRG11_07/ordinary_go1")
		rg11NoAnnotation(t, f.check(t, "ordinary_go1_twin", twin, 2, 123, false).output)
		rg11NoAnnotation(t, f.check(t, "ordinary_go2", input, 2, 123, false).output)
	})
}

func TestRG11_08(t *testing.T) {
	t.Run("unmet_local", func(t *testing.T) {
		f := rg11NewIntegrationFixture(t)
		twin := rg11UnmetInput()
		f.check(t, "unmet_ci_twin", twin, 2, 75, true)
		// D3 also requires exact equality to "true", not mere non-emptiness.
		for _, value := range []string{"", "false", "TRUE"} {
			input := twin
			input.GitHubActions = value
			rg11IntegrationDelta(t, twin, input, "GitHubActions", "TestRG11_05/unmet_ci")
			got := f.check(t, "local_"+value, input, 2, 75, true)
			rg11NoAnnotation(t, got.output)
		}
	})
}

func TestRG11_09(t *testing.T) {
	t.Run("marker_with_zero", func(t *testing.T) {
		f := rg11NewIntegrationFixture(t)
		twin, input := rg11UnmetInput(), rg11UnmetInput()
		input.ExitOverride = "0"
		rg11IntegrationDelta(t, twin, input, "ExitOverride", "TestRG11_05/unmet_ci")
		f.check(t, "unmet_twin", twin, 2, 75, true)
		rg11NoAnnotation(t, f.check(t, "marker_with_zero", input, 0, 0, false).output)
	})
}

func TestRG11_10(t *testing.T) {
	t.Run("empty_selection", func(t *testing.T) {
		f := rg11NewIntegrationFixture(t)
		twin, input := rg11GreenInput(), rg11GreenInput()
		input.EmptySelection = true
		rg11IntegrationDelta(t, twin, input, "EmptySelection", "TestRG11_06/green_ci")
		f.check(t, "green_twin", twin, 0, 0, false)
		got := f.check(t, "empty_selection", input, 2, 1, false)
		if !strings.Contains(got.output, "обход пуст") {
			t.Errorf("empty enumeration has no diagnostic: %s", got.output)
		}
		rg11NoAnnotation(t, got.output)
	})
	t.Run("missing_log", func(t *testing.T) {
		f := rg11NewIntegrationFixture(t)
		log := filepath.Join(f.root, "classifier.log")
		rg11WriteIntegrationFile(t, log, f.capture, 0o600)
		args := []string{"deploy/scripts/classify-integration-outcome.sh", "1", log}
		env := f.environment(rg11UnmetInput())
		twin := f.run(t, "existing_log_twin", "bash", args, env)
		if twin.code != 75 {
			t.Fatalf("existing log control: classifier exit=%d, want 75; %s", twin.code, twin.output)
		}
		if err := os.Remove(log); err != nil {
			t.Fatalf("NOT_EXECUTED: remove only the input log: %v", err)
		}
		if _, err := os.Stat(log); !os.IsNotExist(err) {
			t.Fatalf("NOT_EXECUTED: missing-log condition not created: %v", err)
		}
		t.Log("delta: twin=existing_log_twin, computed=[log_exists], declared=log_exists; argv/env unchanged")
		got := f.run(t, "missing_log", "bash", args, env)
		if got.code != 2 || !strings.Contains(got.output, "нужен код возврата и файл вывода") {
			t.Errorf("missing log: classifier exit=%d, want 2 and required-output-file diagnostic; %s", got.code, got.output)
		}
		rg11NoAnnotation(t, got.output)
	})
}

type rg11IntegrationFixture struct {
	root    string
	step    string
	stepEnv map[string]string
	capture []byte
	marker  string
}

type rg11IntegrationResult struct {
	code   int
	output string
}

func rg11NewIntegrationFixture(t *testing.T) rg11IntegrationFixture {
	t.Helper()
	for _, tool := range []string{"bash", "make", "xargs", "tee", "grep", "wc", "mktemp", "cat", "dirname", "rm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("NOT_EXECUTED: prerequisite %s: %v", tool, err)
		}
	}
	source := repoRoot(t)
	f := rg11IntegrationFixture{root: t.TempDir()}
	for _, path := range []string{"Makefile", "deploy/scripts/classify-integration-outcome.sh", "deploy/scripts/pgtest-unavailable-mark.sh"} {
		body := rg11ReadIntegrationFile(t, filepath.Join(source, path))
		rg11WriteIntegrationFile(t, filepath.Join(f.root, path), body, 0o700)
		t.Logf("source %s sha256=%x", path, sha256.Sum256(body))
	}
	if stat, err := os.Stat(filepath.Join(source, "services/vpc/internal/repo")); err != nil || !stat.IsDir() {
		t.Fatalf("NOT_EXECUTED: selected VPC integration package absent: %v", err)
	}
	markScript := rg11ReadIntegrationFile(t, filepath.Join(f.root, "deploy/scripts/pgtest-unavailable-mark.sh"))
	match := regexp.MustCompile(`(?m)^PGTEST_UNAVAILABLE_MARK='([^']+)'$`).FindSubmatch(markScript)
	if len(match) != 2 {
		t.Fatal("NOT_EXECUTED: canonical producer marker could not be read")
	}
	f.marker = string(match[1])
	dir := filepath.Join(source, "services/storage/tools/testdata/rg11")
	f.capture = rg11ReadIntegrationFile(t, filepath.Join(dir, "pgtest-unavailable.log"))
	metaBytes := rg11ReadIntegrationFile(t, filepath.Join(dir, "capture.json"))
	var meta struct {
		SourceSHA     string            `json:"source_sha"`
		Repo          string            `json:"repo"`
		GoVersion     string            `json:"go_version"`
		DockerVersion string            `json:"docker_version"`
		ImageAbsent   bool              `json:"image_absent"`
		RegistryErrno int               `json:"registry_connect_errno"`
		CompileRC     int               `json:"compile_rc"`
		Command       []string          `json:"command"`
		Env           map[string]string `json:"env_overrides"`
		ReturnCode    int               `json:"returncode"`
		OutputSHA256  string            `json:"stdout_stderr_sha256"`
		OutputBytes   int               `json:"output_bytes"`
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("NOT_EXECUTED: capture metadata: %v", err)
	}
	wantCommand := []string{"timeout", "45s", "go", "test", "./pkg/pgtest", "-run", "^TestOneContainerManyDatabases$", "-count=1", "-v"}
	if meta.SourceSHA != "d941344bd972b2e994263a9e964afa1491aff293" || meta.Repo != "PRO-Robotech/kacho" ||
		meta.GoVersion == "" || meta.DockerVersion == "" || !meta.ImageAbsent || meta.RegistryErrno != 111 ||
		meta.CompileRC != 0 || meta.ReturnCode != 1 || !reflect.DeepEqual(meta.Command, wantCommand) ||
		meta.Env["TESTCONTAINERS_HUB_IMAGE_NAME_PREFIX"] != "127.0.0.1:1/rg1" ||
		meta.Env["TESTCONTAINERS_RYUK_DISABLED"] != "true" || meta.OutputBytes != len(f.capture) ||
		meta.OutputSHA256 != fmt.Sprintf("%x", sha256.Sum256(f.capture)) ||
		meta.OutputSHA256 != "229785c7a3702acd71783b0ef7c7a2e87e322967bd9d488e495409d40728cc34" ||
		string(rg11ReadIntegrationFile(t, filepath.Join(dir, "go-test.exit"))) != "1\n" ||
		!bytes.Contains(f.capture, []byte("=== RUN   TestOneContainerManyDatabases\n")) ||
		bytes.Count(f.capture, []byte("pgtest: "+f.marker)) != 1 || bytes.Contains(metaBytes, []byte("/home/")) {
		t.Fatal("NOT_EXECUTED: actual pgtest capture provenance, output, or exit does not match")
	}
	workflow := rg11ReadIntegrationFile(t, filepath.Join(source, ".github/workflows/ci.yaml"))
	type runDefaults struct {
		Run struct {
			Shell string `yaml:"shell"`
			Dir   string `yaml:"working-directory"`
		} `yaml:"run"`
	}
	var ci struct {
		Defaults runDefaults `yaml:"defaults"`
		Jobs     map[string]struct {
			Defaults runDefaults `yaml:"defaults"`
			Steps    []struct {
				Name  string            `yaml:"name"`
				Run   string            `yaml:"run"`
				Shell string            `yaml:"shell"`
				Dir   string            `yaml:"working-directory"`
				Env   map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflow, &ci); err != nil {
		t.Fatalf("NOT_EXECUTED: actual CI YAML: %v", err)
	}
	job, exists := ci.Jobs["integration"]
	if !exists {
		t.Fatal("NOT_EXECUTED: integration job absent")
	}
	selected := 0
	for _, step := range job.Steps {
		if !strings.HasPrefix(step.Name, "integration ${{ matrix.svc }}") {
			continue
		}
		selected++
		shell := ci.Defaults.Run.Shell
		if job.Defaults.Run.Shell != "" {
			shell = job.Defaults.Run.Shell
		}
		if step.Shell != "" {
			shell = step.Shell
		}
		if shell != "bash" || ci.Defaults.Run.Dir != "" || job.Defaults.Run.Dir != "" || step.Dir != "" {
			t.Fatal("NOT_EXECUTED: CI shell or working directory changed; refresh the execution fixture")
		}
		f.step = strings.ReplaceAll(step.Run, "${{ matrix.svc }}", "vpc")
		f.stepEnv = make(map[string]string)
		for key, value := range step.Env {
			f.stepEnv[key] = strings.ReplaceAll(value, "${{ matrix.svc }}", "vpc")
		}
	}
	if selected != 1 || strings.Contains(f.step, "${{") || f.stepEnv["SVC"] != "vpc" || f.step == "" {
		t.Fatalf("NOT_EXECUTED: CI step selection/resolution invalid: selected=%d", selected)
	}
	t.Logf("source .github/workflows/ci.yaml sha256=%x; selected step=integration; shell=bash --noprofile --norc -eo pipefail; cwd=fixture root", sha256.Sum256(workflow))
	t.Logf("capture source=%s sha256=%s bytes=%d go_exit=%d", meta.SourceSHA, meta.OutputSHA256, len(f.capture), meta.ReturnCode)
	rg11WriteIntegrationFile(t, filepath.Join(f.root, "go-child"), []byte(rg11GoChild), 0o700)
	return f
}

// This double accepts only the two commands issued by the real VPC recipe.
// Its output is replayed before xargs, not in place of the classifier or make.
const rg11GoChild = `#!/usr/bin/env bash
set -eu
trap 'rg11_status=$?; printf "%s %d\n" "$1" "$rg11_status" >> "$RG11_GO_EXITS"' EXIT
printf '%s\n' "$*" >> "$RG11_GO_CALLS"
case "$1" in
  list)
    [ "$#" -eq 2 ] && [ "$2" = './services/vpc/...' ] || exit 97
    cat "$RG11_PACKAGES"
    ;;
  test)
    [ "$#" -eq 9 ] && [ "$2" = '-tags=integration' ] && [ "$3" = '-race' ] &&
    [ "$4" = '-count=1' ] && [ "$5" = '-timeout' ] && [ "$7" = '-p' ] &&
    [ "$8" = '1' ] && [ "$9" = 'github.com/PRO-Robotech/kacho/services/vpc/internal/repo' ] || exit 97
    cat "$RG11_CHILD_OUTPUT"
    exit "$RG11_CHILD_EXIT"
    ;;
  *) exit 97 ;;
esac
`

func (f rg11IntegrationFixture) environment(input rg11IntegrationInput) []string {
	// Keep startup hooks, MAKEFLAGS and inherited GO overrides out of the fixture.
	env := []string{"PATH=" + os.Getenv("PATH"), "CI=true", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TMPDIR=" + f.root, "GO=" + filepath.Join(f.root, "go-child")}
	for key, value := range f.stepEnv {
		env = append(env, key+"="+value)
	}
	if input.GitHubActions != "" {
		env = append(env, "GITHUB_ACTIONS="+input.GitHubActions)
	}
	return env
}

func (f rg11IntegrationFixture) check(t *testing.T, label string, input rg11IntegrationInput, wantExit, wantRecipe int, human bool) rg11IntegrationResult {
	t.Helper()
	log, childExit := f.capture, "1"
	if input.PostgresAvailable {
		log, childExit = []byte("ok\t"+rg11IntegrationPackage+"\t0.001s\n"), "0"
	}
	if input.RemoveMarker {
		var kept []byte
		removed := 0
		for _, line := range bytes.SplitAfter(log, []byte("\n")) {
			if bytes.Contains(line, []byte(f.marker)) {
				removed++
				continue
			}
			kept = append(kept, line...)
		}
		if removed != 1 {
			t.Fatalf("NOT_EXECUTED: marker-line delta removed %d lines, want 1", removed)
		}
		log = kept
	}
	if input.ExitOverride != "" {
		childExit = input.ExitOverride
	}
	packages := rg11IntegrationPackage + "\n"
	if input.EmptySelection {
		packages = ""
	}
	calls := filepath.Join(f.root, label+".calls")
	rg11WriteIntegrationFile(t, calls, nil, 0o600)
	exits := filepath.Join(f.root, label+".exits")
	rg11WriteIntegrationFile(t, exits, nil, 0o600)
	output := filepath.Join(f.root, label+".producer")
	rg11WriteIntegrationFile(t, output, log, 0o600)
	packageFile := filepath.Join(f.root, label+".packages")
	rg11WriteIntegrationFile(t, packageFile, []byte(packages), 0o600)
	env := append(f.environment(input), "RG11_GO_CALLS="+calls, "RG11_GO_EXITS="+exits, "RG11_PACKAGES="+packageFile, "RG11_CHILD_OUTPUT="+output, "RG11_CHILD_EXIT="+childExit)
	step := f.step
	if input.GitHubActions != "true" {
		step = "make test-integration\n"
	}
	script := filepath.Join(f.root, label+".step.sh")
	rg11WriteIntegrationFile(t, script, []byte(step), 0o600)
	got := f.run(t, label, "bash", []string{"--noprofile", "--norc", "-eo", "pipefail", script}, env)
	callText := string(rg11ReadIntegrationFile(t, calls))
	callLines := strings.Split(strings.TrimSpace(callText), "\n")
	wantCalls := 2
	if input.EmptySelection {
		wantCalls = 1
	}
	if len(callLines) != wantCalls || callLines[0] != "list ./services/vpc/..." {
		t.Fatalf("NOT_EXECUTED: GO child census mismatch: want %d calls, got %q", wantCalls, callText)
	}
	if !input.EmptySelection && (!strings.HasPrefix(callLines[1], "test -tags=integration -race -count=1 -timeout ") || !strings.HasSuffix(callLines[1], " -p 1 "+rg11IntegrationPackage)) {
		t.Fatalf("NOT_EXECUTED: GO test command was not the actual integration recipe: %q", callLines[1])
	}
	wantExits := "list 0\n"
	if !input.EmptySelection {
		wantExits += "test " + childExit + "\n"
	}
	observedExits := string(rg11ReadIntegrationFile(t, exits))
	if observedExits != wantExits {
		t.Fatalf("NOT_EXECUTED: GO child did not produce the requested result: exits=%q, want=%q", observedExits, wantExits)
	}
	t.Logf("child census label=%s list=1 test=%d; exits=%q; calls=%q", label, wantCalls-1, observedExits, callText)
	if got.code != wantExit {
		t.Errorf("%s outer exit=%d, want %d", label, got.code, wantExit)
	}
	if wantRecipe != 0 {
		matches := regexp.MustCompile(`(?m)^make(?:\[[0-9]+\])?: \*\*\* .* Error ([0-9]+)$`).FindAllStringSubmatch(got.output, -1)
		if len(matches) != 1 || matches[0][1] != strconv.Itoa(wantRecipe) {
			t.Errorf("%s make recipe error=%v, want exactly %d", label, matches, wantRecipe)
		}
	}
	if !input.EmptySelection && !strings.Contains(got.output, "пакетов: 1 (из осмотренных 1)") {
		t.Errorf("%s nonempty package census missing", label)
	}
	if human && (!strings.Contains(got.output, "Postgres для integration не поднялся") || !strings.Contains(got.output, "Вердикта нет НИ У ОДНОЙ пробы") || !strings.Contains(got.output, "прогон недействителен и повторяется после устранения причины")) {
		t.Errorf("%s missing human third-outcome explanation", label)
	}
	return got
}

func (f rg11IntegrationFixture) run(t *testing.T, label, name string, args, env []string) rg11IntegrationResult {
	t.Helper()
	t.Logf("exec label=%s cwd=%s argv=%q env=%q", label, f.root, append([]string{name}, args...), env)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = f.root, env
	body, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("NOT_EXECUTED: %s process deadline: %v; output=%s", label, ctx.Err(), body)
	}
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() < 0 {
			t.Fatalf("NOT_EXECUTED: %s process did not finish: %v; output=%s", label, err, body)
		}
		code = exit.ExitCode()
	}
	t.Logf("result label=%s exit=%d output_bytes=%d output_sha256=%x\n%s", label, code, len(body), sha256.Sum256(body), body)
	return rg11IntegrationResult{code: code, output: string(body)}
}

func rg11IntegrationDelta(t *testing.T, twin, input rg11IntegrationInput, declared, twinName string) {
	t.Helper()
	a, b := reflect.ValueOf(twin), reflect.ValueOf(input)
	var changed []string
	for i := 0; i < a.NumField(); i++ {
		if !reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
			changed = append(changed, a.Type().Field(i).Name)
		}
	}
	if !reflect.DeepEqual(changed, []string{declared}) {
		t.Fatalf("NOT_EXECUTED: twin=%s computed delta=%v, declared=%s", twinName, changed, declared)
	}
	t.Logf("delta: twin=%s computed=%v declared=%s", twinName, changed, declared)
}

func rg11CheckAnnotation(t *testing.T, output string) {
	t.Helper()
	var annotations []string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "::error") {
			annotations = append(annotations, line)
		}
	}
	if len(annotations) != 1 {
		t.Errorf("RG1.1-05: executed CI emitted %d error annotations, want exactly 1 with title УСЛОВИЕ НЕ СОЗДАНО", len(annotations))
		return
	}
	line := annotations[0]
	if !strings.HasPrefix(line, "::error title=УСЛОВИЕ НЕ СОЗДАНО::") {
		t.Errorf("annotation title is not fixed: %s", line)
	}
	for _, fragment := range []string{"Postgres", "integration", "недействителен", "повтор", "причин"} {
		if !strings.Contains(strings.ToLower(line), strings.ToLower(fragment)) {
			t.Errorf("annotation does not explain %q: %s", fragment, line)
		}
	}
	if strings.Contains(line, "127.0.0.1") || strings.Contains(line, "generic container") || strings.Contains(line, "pgtest:") {
		t.Errorf("annotation interpolates the dynamic producer journal: %s", line)
	}
}

func rg11NoAnnotation(t *testing.T, output string) {
	t.Helper()
	if regexp.MustCompile(`(?m)^::(?:error|warning|notice)(?: |::)`).MatchString(output) {
		t.Errorf("unexpected GitHub Actions control annotation: %s", output)
	}
}

func rg11ReadIntegrationFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("NOT_EXECUTED: read fixture %s: %v", path, err)
	}
	return body
}

func rg11WriteIntegrationFile(t *testing.T, path string, body []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("NOT_EXECUTED: create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatalf("NOT_EXECUTED: write fixture %s: %v", path, err)
	}
}
