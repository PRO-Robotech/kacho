// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ci_wiring_test.go asserts the half of "is this gate real?" that the gate itself
// cannot answer: whether anything runs it.
//
// Registry had both halves broken at once. The make target existed but only echoed a
// sentence and exited 0, and the CI step that runs this gate looped over
// compute/nlb/storage/vpc — so registry was outside it anyway. A gate nothing invokes
// is worth exactly as much as a gate that checks nothing, and the two failures hid
// each other: fixing only one would still have left the invariant unenforced.
//
// So this file checks the wiring by reading the workflows, not by trusting a comment
// that says CI runs it.
package auditlistfilter

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// repoRoot walks up from this file until it finds .github/workflows.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(self)
	for range 12 {
		if fi, err := os.Stat(filepath.Join(dir, ".github", "workflows")); err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find .github/workflows above this test file")
	return ""
}

// workflowFile is the slice of the GitHub Actions schema this check needs.
type workflowFile struct {
	Jobs map[string]struct {
		Steps []struct {
			Run string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

var (
	// makeRe finds a `make` command at the start of a line (indentation allowed —
	// these live inside `for … do` bodies) or after a shell separator.
	makeRe = regexp.MustCompile(`(?m)(?:^[ \t]*|[;&|]\s*)make\s+([^;&|\n]+)`)
	// loopRe finds `for <var> in a b c;`, so `make -C "services/${var}"` can be
	// resolved to the concrete directories it will run against.
	loopRe = regexp.MustCompile(`for\s+([A-Za-z_][A-Za-z0-9_]*)\s+in\s+([^;\n]+)`)
	// varRe finds ${var} / $var inside a -C argument.
	varRe = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
	// ruleRe finds the target names a Makefile declares.
	ruleRe = regexp.MustCompile(`(?m)^([A-Za-z0-9_./-]+)\s*:(?:[^=]|$)`)
)

// invocation is one `make …` command a workflow issues, resolved to the directory
// whose Makefile would serve it.
type invocation struct {
	where  string
	dir    string
	target string
}

// collectMakeInvocations reads every workflow and returns the make commands it
// issues. Loop variables in `-C` are expanded against the `for … in` in the same
// run block; a command that cannot be resolved is reported, never skipped.
func collectMakeInvocations(t *testing.T, root string) []invocation {
	t.Helper()
	wfDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		t.Fatalf("read %s: %v", wfDir, err)
	}

	var out []invocation
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
			continue
		}
		files++
		raw, rerr := os.ReadFile(filepath.Join(wfDir, name))
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		var wf workflowFile
		if uerr := yaml.Unmarshal(raw, &wf); uerr != nil {
			t.Fatalf("parse %s: %v", name, uerr)
		}
		for jobName, job := range wf.Jobs {
			for i, step := range job.Steps {
				if step.Run == "" {
					continue
				}
				script := stripShellComments(step.Run)
				where := name + " job " + jobName + " step " + itoa(i)
				for _, m := range makeRe.FindAllStringSubmatch(script, -1) {
					dir, targets := parseMakeArgs(m[1])
					if len(targets) == 0 {
						continue
					}
					dirs := []string{"."}
					if dir != "" {
						dirs = expandLoopVars(t, script, dir, where)
					}
					for _, d := range dirs {
						for _, tgt := range targets {
							out = append(out, invocation{where: where, dir: d, target: tgt})
						}
					}
				}
			}
		}
	}
	if files == 0 {
		t.Fatal("no workflow files read — the walk found nothing, so it asserted nothing")
	}
	return out
}

// parseMakeArgs splits a make command's arguments into the -C directory (if any) and
// the target names, dropping flags and VAR=value overrides.
func parseMakeArgs(args string) (dir string, targets []string) {
	fields := strings.Fields(args)
	for i := 0; i < len(fields); i++ {
		f := strings.Trim(fields[i], `"'`)
		switch {
		case f == "-C" && i+1 < len(fields):
			dir = strings.Trim(fields[i+1], `"'`)
			i++
		case strings.HasPrefix(f, "-C") && len(f) > 2:
			dir = strings.Trim(f[2:], `"'`)
		case strings.HasPrefix(f, "-"), strings.Contains(f, "="):
			// a flag, or a VAR=value override — neither is a target
		default:
			targets = append(targets, f)
		}
	}
	return dir, targets
}

// expandLoopVars resolves ${var} in dir against a `for var in …` in the same run
// block.
func expandLoopVars(t *testing.T, script, dir, where string) []string {
	t.Helper()
	m := varRe.FindStringSubmatch(dir)
	if m == nil {
		return []string{dir}
	}
	for _, loop := range loopRe.FindAllStringSubmatch(script, -1) {
		if loop[1] != m[1] {
			continue
		}
		var out []string
		for _, v := range strings.Fields(loop[2]) {
			out = append(out, varRe.ReplaceAllString(dir, v))
		}
		if len(out) > 0 {
			return out
		}
	}
	t.Errorf("%s: `make -C %s` uses variable %q that no `for %s in …` in the same run "+
		"block defines — this check cannot tell which Makefile is meant, so it cannot "+
		"vouch for the command", where, dir, m[1], m[1])
	return nil
}

// stripShellComments drops whole-line shell comments, so a command merely DESCRIBED
// in a comment is not mistaken for one CI issues.
func stripShellComments(script string) string {
	lines := strings.Split(script, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}

// itoa renders a small non-negative int without pulling in strconv for one call.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestGateIsInvokedByCI — some workflow actually issues this service's listauthz
// gate. Registry was missing from the loop that runs it, which is why a target that
// checked nothing went unnoticed for as long as it did.
func TestGateIsInvokedByCI(t *testing.T) {
	root := repoRoot(t)
	for _, inv := range collectMakeInvocations(t, root) {
		if inv.target == "audit-list-filter" && inv.dir == "services/registry" {
			t.Logf("invoked by %s", inv.where)
			return
		}
	}
	t.Error("no workflow issues `make -C services/registry audit-list-filter` — the gate " +
		"exists but nothing runs it, which is indistinguishable from not having one")
}

// TestGateTargetResolves — the command CI issues can actually execute: the Makefile
// it reaches declares that target. A gate named in a workflow but absent from the
// Makefile fails the job for the wrong reason, or (with `|| true` nearby) not at all.
func TestGateTargetResolves(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "services", "registry", "Makefile"))
	if err != nil {
		t.Fatalf("read registry Makefile: %v", err)
	}
	for _, m := range ruleRe.FindAllStringSubmatch(string(raw), -1) {
		if m[1] == "audit-list-filter" {
			return
		}
	}
	t.Error("services/registry/Makefile declares no audit-list-filter target — the command " +
		"CI issues cannot run")
}
