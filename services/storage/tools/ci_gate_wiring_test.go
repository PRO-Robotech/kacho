// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package tools_regression

// A gate is only a gate if the command that runs it exists and is actually run.
// Both halves failed here at once, in opposite directions:
//
//   - the command as written down was unrunnable. Verification checklists say
//     `make audit-list-filter`, but this repository has NO Makefile at its root —
//     the target lives in services/<svc>/Makefile. Typed from the root, as written,
//     it answers "No rule to make target" and exits non-zero, so a reader following
//     the checklist could not run the gate at all;
//   - the comment about the command was stale in the other direction. This
//     service's Makefile and this package's own doc comment both stated that no CI
//     workflow invokes the target — which stopped being true when
//     .github/workflows/ci.yaml started running it for four services. A comment
//     claiming a gate is unwired, sitting next to a gate that is wired, is the same
//     hazard as the reverse: the next contributor trusts the comment.
//
// So this file asserts the two things prose cannot: that every make command CI
// issues resolves to a real target, and that this service's listauthz gate is among
// the commands CI issues.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// repoRoot walks up from this file until it finds .github/workflows.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir := scriptDir(t)
	for i := 0; i < 12; i++ {
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

// makeInvocation is one `make …` command found in a workflow, already resolved to
// the directory whose Makefile would serve it.
type makeInvocation struct {
	workflow string
	dir      string // relative to repo root; "." means repo root
	target   string
}

// workflowFile is the slice of GitHub Actions schema this check needs.
type workflowFile struct {
	Defaults struct {
		Run struct {
			WorkingDirectory string `yaml:"working-directory"`
		} `yaml:"run"`
	} `yaml:"defaults"`
	Jobs map[string]struct {
		Defaults struct {
			Run struct {
				WorkingDirectory string `yaml:"working-directory"`
			} `yaml:"run"`
		} `yaml:"defaults"`
		Steps []struct {
			Run              string `yaml:"run"`
			WorkingDirectory string `yaml:"working-directory"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

var (
	// makeRe finds a `make` command at the start of a line (indentation allowed —
	// these live inside `for … do` bodies) or after a shell separator, capturing
	// the arguments up to the end of that command.
	makeRe = regexp.MustCompile(`(?m)(?:^[ \t]*|[;&|]\s*|\(\s*)make\s+([^;&|\n]+)`)
	// loopRe finds `for <var> in a b c;` so a `make -C "services/${var}"` can be
	// resolved to the concrete directories it will run against.
	loopRe = regexp.MustCompile(`for\s+([A-Za-z_][A-Za-z0-9_]*)\s+in\s+([^;\n]+)`)
	// varRe finds ${var} / $var inside a -C argument.
	varRe = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
)

// parseMakeArgs splits a make command's arguments into the -C directory (if any)
// and the target names, dropping flags and VAR=value overrides.
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
		case strings.HasPrefix(f, "-"):
			// plain flag
		case strings.Contains(f, "="):
			// VAR=value override, not a target
		default:
			targets = append(targets, f)
		}
	}
	return dir, targets
}

// expandLoopVars resolves ${var} in dir against `for var in …` loops in the same
// run block. An unresolvable variable is reported, never skipped: a command this
// check cannot read is a command it cannot vouch for.
func expandLoopVars(t *testing.T, run, dir, where string) []string {
	t.Helper()
	m := varRe.FindStringSubmatch(dir)
	if m == nil {
		return []string{dir}
	}
	name := m[1]

	for _, loop := range loopRe.FindAllStringSubmatch(run, -1) {
		if loop[1] != name {
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

	t.Errorf("%s: `make -C %s` uses variable %q that no `for %s in …` in the same "+
		"run block defines — this check cannot tell which Makefile is meant, so it "+
		"cannot vouch for the command", where, dir, name, name)
	return nil
}

// collectMakeInvocations reads every workflow and returns each make command it
// issues, resolved to the directory whose Makefile would serve it.
func collectMakeInvocations(t *testing.T, root string) []makeInvocation {
	t.Helper()
	wfDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		t.Fatalf("read %s: %v", wfDir, err)
	}

	var out []makeInvocation
	files := 0
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yml") && !strings.HasSuffix(e.Name(), ".yaml")) {
			continue
		}
		files++
		raw, err := os.ReadFile(filepath.Join(wfDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		var wf workflowFile
		if err := yaml.Unmarshal(raw, &wf); err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}

		for jobName, job := range wf.Jobs {
			for i, step := range job.Steps {
				if step.Run == "" {
					continue
				}
				where := e.Name() + " job " + jobName + " step " + itoa(i)
				script := stripShellComments(step.Run)

				base := step.WorkingDirectory
				if base == "" {
					base = job.Defaults.Run.WorkingDirectory
				}
				if base == "" {
					base = wf.Defaults.Run.WorkingDirectory
				}
				if base == "" {
					base = "."
				}

				for _, m := range makeRe.FindAllStringSubmatch(script, -1) {
					dir, targets := parseMakeArgs(m[1])
					if len(targets) == 0 {
						continue // bare `make` with only flags — nothing to resolve
					}
					dirs := []string{base}
					if dir != "" {
						dirs = expandLoopVars(t, script, dir, where)
					}
					for _, d := range dirs {
						for _, tgt := range targets {
							out = append(out, makeInvocation{workflow: where, dir: d, target: tgt})
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

// stripShellComments drops whole-line shell comments, so a command merely
// DESCRIBED in a comment is not mistaken for one CI issues.
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

// itoa avoids pulling strconv in for one call site.
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

// makeTargets returns the target names declared by the Makefile in dir. ok is
// false when there is no Makefile there at all.
func makeTargets(root, dir string) (targets map[string]bool, ok bool) {
	path := filepath.Join(root, dir, "Makefile")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	targets = map[string]bool{}
	ruleRe := regexp.MustCompile(`(?m)^([A-Za-z0-9_./-]+)\s*:(?:[^=]|$)`)
	for _, m := range ruleRe.FindAllStringSubmatch(string(raw), -1) {
		targets[m[1]] = true
	}
	return targets, true
}

// TestEveryCIMakeCommandResolves — every `make` command any workflow issues names a
// target that the Makefile it will reach actually declares.
//
// This is the general shape of the defect that produced this file: a gate written
// down as a command nobody can run. Here it is checked where it costs the most to
// get wrong — the commands CI itself issues.
func TestEveryCIMakeCommandResolves(t *testing.T) {
	root := repoRoot(t)
	invocations := collectMakeInvocations(t, root)
	if len(invocations) == 0 {
		t.Fatal("no `make` commands found in any workflow — the parser found nothing, " +
			"so this test asserted nothing")
	}
	t.Logf("checked %d make command(s) issued by CI", len(invocations))

	for _, inv := range invocations {
		targets, ok := makeTargets(root, inv.dir)
		if !ok {
			t.Errorf("%s: `make %s` would run in %q, which has no Makefile — the command "+
				"cannot execute", inv.workflow, inv.target, inv.dir)
			continue
		}
		if !targets[inv.target] {
			t.Errorf("%s: `make %s` in %q — that Makefile declares no such target",
				inv.workflow, inv.target, inv.dir)
		}
	}
}

// TestStorageListAuthzGateIsInvokedByCI — the claim this service's Makefile and
// docs now make about themselves ("CI runs this") is asserted, not asserted-by-prose.
// It is the claim that went stale before, in the opposite direction.
func TestStorageListAuthzGateIsInvokedByCI(t *testing.T) {
	root := repoRoot(t)
	for _, inv := range collectMakeInvocations(t, root) {
		if inv.target == "audit-list-filter" && inv.dir == "services/storage" {
			t.Logf("invoked by %s", inv.workflow)
			return
		}
	}
	t.Error("no workflow issues `make -C services/storage audit-list-filter` — the gate " +
		"exists but nothing runs it, which is how it went unnoticed that storage's List " +
		"filtered nothing per object")
}

// TestStorageKnownFailingGateIsInvokedByCI — same assertion for the gate that stops
// a "known failing" record from outliving its fix.
//
// It is asserted here for the same reason as the one above, and the reason is not
// symmetry: the records this gate governs are precisely the ones nobody revisits.
// A gate over stale claims that is itself never run would be the joke version of
// the defect it exists to catch.
func TestStorageKnownFailingGateIsInvokedByCI(t *testing.T) {
	root := repoRoot(t)
	for _, inv := range collectMakeInvocations(t, root) {
		if inv.target == "audit-known-failing" && inv.dir == "services/storage" {
			t.Logf("invoked by %s", inv.workflow)
			return
		}
	}
	t.Error("no workflow issues `make -C services/storage audit-known-failing` — a gate " +
		"nothing runs cannot notice that an exclusion outlived its subject")
}
