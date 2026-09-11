// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package tools_regression

// A gate is only a gate if the command that runs it exists and is actually run.
// Both halves failed here at once, in opposite directions:
//
//   - the command as written down was unrunnable. Verification checklists named
//     `make audit-list-filter` without its service directory. The storage target
//     is declared in services/storage/Makefile and is invoked from the repository
//     root with `make -C services/storage audit-list-filter`. The working directory
//     is part of the command: a target in another Makefile cannot serve it;
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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	wrapped  bool
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
	// shellOpRe finds the first shell metacharacter in a field. None of these can
	// occur in a make target, in a flag, or in a VAR=value override, so the field
	// carrying one ENDS the argument list: everything from it onward belongs to the
	// shell, not to make.
	//
	// WHY. makeRe stops its capture at `;`, `&` and `|`, so those three never reach
	// the splitter — but a REDIRECTION does, and a redirection is not a target at
	// any Makefile, ever. `make -C "services/${svc}" audit-list-filter
	// >"${logs}/${svc}.out" 2>&1` therefore yielded THREE targets: the real one plus
	// `>"${logs}/${svc}.out` and `2>`, and the gate reported that the Makefile
	// declares no such target — once per service the loop names. It was right about
	// the target and wrong about the command: a step body runs under `bash -e`, where
	// a return code taken as a CONDITION aborts the step silently, so `cmd >log 2>&1
	// && rc=0 || rc=$?` is the form such a step is obliged to use. Only the reader
	// was broken.
	//
	// The whole set is closed here at once rather than one form per incident, because
	// a form the reader does not know is not an edge case: whatever is written in it
	// stops being observed at all, and the gate goes on printing a verdict.
	shellOpRe = regexp.MustCompile(`[<>|;&()]`)
)

// parseMakeArgs splits a make command's arguments into the -C directory (if any)
// and the target names, dropping flags and VAR=value overrides, and stopping at the
// first shell operator.
func parseMakeArgs(args string) (dir string, targets []string) {
	fields := strings.Fields(args)
	for i := 0; i < len(fields); i++ {
		f, stop := fields[i], false
		if loc := shellOpRe.FindStringIndex(f); loc != nil {
			// A GLUED operator (`target>log`, `target)` closing a subshell) still
			// leaves a real argument in front of it, and dropping the whole field
			// would lose a command the gate is supposed to vouch for. A bare
			// redirection (`>log`) leaves nothing, and a file-descriptor prefix
			// (the `2` of `2>&1`) belongs to the redirection rather than to make.
			f, stop = f[:loc[0]], true
			if f == "" || isFileDescriptor(f) {
				break
			}
		}
		f = strings.Trim(f, `"'`)
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
		if stop {
			break
		}
	}
	return dir, targets
}

// isFileDescriptor reports whether s is the file-descriptor prefix of a redirection
// — the `2` of `2>&1`. Such a prefix is part of the redirection, not a target: a
// Makefile target named after a bare number does not occur, and one could not be
// written glued to a redirection even if it did.
func isFileDescriptor(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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

// shellWord keeps the unquoted value and its original span. Only wrapper options
// need unquoting; the make arguments still go through parseMakeArgs unchanged.
type shellWord struct {
	value      string
	start, end int
}

type shellCommand struct {
	start, end int
	words      []shellWord
	err        error
}

// readShellCommand reads one simple command without evaluating shell syntax.
// Quoted/escaped separators belong to their word; unquoted separators end the
// command. Redirections end argv but remain in the span masked from makeRe.
// Errors matter only when the caller recognizes the canonical wrapper.
func readShellCommand(script string, start int) shellCommand {
	cmd := shellCommand{start: start, end: len(script)}
	var word strings.Builder
	wordStart := -1
	var quote byte
	redirect := false
	flush := func(end int) {
		if wordStart >= 0 {
			cmd.words = append(cmd.words, shellWord{word.String(), wordStart, end})
			word.Reset()
			wordStart = -1
		}
	}
	for i := start; i < len(script); i++ {
		c := script[i]
		if c == '\\' && quote != '\'' {
			if i+1 == len(script) {
				cmd.err = fmt.Errorf("unfinished shell escape")
				break
			}
			if !redirect {
				if wordStart < 0 {
					wordStart = i
				}
				// Inside double quotes bash only removes these backslashes.
				if quote == '"' && !strings.ContainsRune("$`\"\\\n", rune(script[i+1])) {
					word.WriteByte(c)
				}
				word.WriteByte(script[i+1])
			}
			i++
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else if !redirect {
				word.WriteByte(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			if !redirect && wordStart < 0 {
				wordStart = i
			}
			continue
		}
		if strings.ContainsRune("\n;&|()", rune(c)) {
			flush(i)
			cmd.end = i + 1
			return cmd
		}
		if c == '#' && wordStart < 0 {
			if end := strings.IndexByte(script[i:], '\n'); end >= 0 {
				cmd.end = i + end + 1
			}
			return cmd
		}
		if c == '<' || c == '>' {
			if !isFileDescriptor(word.String()) {
				flush(i)
			}
			word.Reset()
			wordStart = -1
			redirect = true
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' {
			flush(i)
			continue
		}
		if !redirect {
			if wordStart < 0 {
				wordStart = i
			}
			word.WriteByte(c)
		}
	}
	flush(len(script))
	if quote != 0 {
		cmd.err = fmt.Errorf("unterminated shell quote")
	}
	return cmd
}

// ciDirectory follows a relative cd and preserves an absolute operand.
func ciDirectory(base, dir string) string {
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Join(base, dir)
}

func dynamicCIDirectory(dir string) bool {
	return strings.ContainsAny(dir, "$`") || strings.Contains(dir, "GH_EXPR")
}

// shellExecutableWord locates the command after shell control words and the
// script-file operand of bash. It only locates these forms so standUpMake can
// refuse unsupported prefixes; it does not interpret their execution semantics.
// Arguments to echo/printf/another script are never scanned for wrapper paths.
func shellExecutableWord(script string, words []shellWord) int {
	i := 0
	for i < len(words) && script[words[i].start:words[i].end] == words[i].value {
		switch words[i].value {
		case "if", "elif", "while", "until", "then", "else", "do", "!":
			i++
			continue
		}
		break
	}
	if i < len(words) && words[i].value == "bash" {
		i++
		for i < len(words) {
			option := words[i].value
			if option == "--" {
				i++
				break
			}
			if !strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "+") {
				break
			}
			switch option {
			case "-", "--help", "--version":
				return -1 // stdin or informational mode has no script-file operand
			case "-o", "+o", "-O", "+O", "--rcfile", "--init-file":
				i += 2 // the next word is an option value, not the script
			default:
				if !strings.HasPrefix(option, "--") && strings.ContainsAny(option[1:], "cs") {
					return -1 // -c/-s leaves subsequent words as positional arguments
				}
				i++
			}
		}
	}
	if i >= len(words) {
		return -1
	}
	return i
}

// standUpMake reads only the canonical stand-up interface. The bool says whether
// this command's span is owned by the wrapper, even when its arguments are invalid.
func standUpMake(root, base, script string, cmd shellCommand) (inv []makeInvocation, recognized bool, err error) {
	commandWord := shellExecutableWord(script, cmd.words)
	if commandWord < 0 {
		return nil, false, nil
	}
	words := cmd.words[commandWord:]
	const canonical = ".github/scripts/stand-up.sh"
	path := words[0].value
	if dynamicCIDirectory(base) {
		// A dynamic cwd cannot establish the identity of a relative canonical
		// path. Announce that limitation instead of substituting the root.
		if filepath.Clean(path) == canonical || strings.HasSuffix(filepath.Clean(path), "/"+canonical) {
			return nil, true, fmt.Errorf("cannot resolve dynamic working-directory %q", base)
		}
		return nil, false, nil
	}
	if ciDirectory(ciDirectory(root, base), path) != filepath.Join(root, canonical) {
		return nil, false, nil
	}
	if commandWord != 0 && !(commandWord == 1 && cmd.words[0].value == "bash") {
		return nil, true, fmt.Errorf("unsupported shell prefix before canonical stand-up command")
	}
	if cmd.err != nil {
		return nil, true, cmd.err
	}
	words = words[1:]
	if len(words) == 1 && words[0].value == "--self-test" {
		return nil, true, nil
	}
	label, dir := "", base
	for len(words) > 0 && words[0].value != "--" {
		key := words[0].value
		if key != "--label" && key != "--dir" {
			return nil, true, fmt.Errorf("unsupported stand-up option %q", key)
		}
		if len(words) < 2 || words[1].value == "" || strings.HasPrefix(words[1].value, "--") {
			return nil, true, fmt.Errorf("stand-up %s requires a value", key)
		}
		if key == "--label" {
			label = words[1].value
		} else {
			if dynamicCIDirectory(words[1].value) || strings.ContainsAny(words[1].value, " \t\n") {
				return nil, true, fmt.Errorf("cannot resolve stand-up --dir %q", words[1].value)
			}
			dir = ciDirectory(base, words[1].value)
		}
		words = words[2:]
	}
	if label == "" || len(words) < 2 || words[1].value != "make" {
		return nil, true, fmt.Errorf("stand-up requires --label and -- followed by make")
	}
	words = words[1:]
	changes := 0
	for i := 1; i < len(words); i++ {
		if words[i].value == "-C" || strings.HasPrefix(words[i].value, "-C") {
			changes++
			value := strings.TrimPrefix(words[i].value, "-C")
			if value == "" {
				if i+1 == len(words) || strings.HasPrefix(words[i+1].value, "-") {
					return nil, true, fmt.Errorf("stand-up child make -C requires a directory")
				}
				i++
				value = words[i].value
			}
			if value == "" || dynamicCIDirectory(value) || strings.ContainsAny(value, " \t\n") {
				return nil, true, fmt.Errorf("cannot resolve stand-up child make -C %q", value)
			}
		}
	}
	if changes > 1 {
		return nil, true, fmt.Errorf("unsupported repeated -C in stand-up child make")
	}
	childDir, targets := parseMakeArgs(script[words[0].end:words[len(words)-1].end])
	if childDir != "" {
		dir = ciDirectory(dir, childDir)
	}
	if len(targets) == 0 {
		return nil, true, fmt.Errorf("stand-up child make has no explicit target to resolve")
	}
	for _, target := range targets {
		inv = append(inv, makeInvocation{dir: dir, target: target, wrapped: true})
	}
	return inv, true, nil
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

		jobNames := make([]string, 0, len(wf.Jobs))
		for name := range wf.Jobs {
			jobNames = append(jobNames, name)
		}
		sort.Strings(jobNames)
		for _, jobName := range jobNames {
			job := wf.Jobs[jobName]
			for i, step := range job.Steps {
				if step.Run == "" {
					continue
				}
				where := e.Name() + " job " + jobName + " step " + itoa(i)
				script := stripShellComments(maskExpressions(joinContinuations(step.Run)))

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

				ordinary := []byte(script)
				for start := 0; start < len(script); {
					cmd := readShellCommand(script, start)
					start = cmd.end
					wrapped, recognized, err := standUpMake(root, base, script, cmd)
					if !recognized {
						continue
					}
					// Even an invalid recognized wrapper owns its span: its label
					// and child must never be mistaken for ordinary make commands.
					for pos := cmd.start; pos < cmd.end; pos++ {
						if ordinary[pos] != '\n' {
							ordinary[pos] = ' '
						}
					}
					if err != nil {
						t.Errorf("%s: stand-up make command cannot be read: %v", where, err)
						continue
					}
					for _, inv := range wrapped {
						inv.workflow = where
						out = append(out, inv)
					}
				}
				for _, m := range makeRe.FindAllStringSubmatch(string(ordinary), -1) {
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

// joinContinuations splices shell line-continuations (a trailing backslash) into
// one logical line BEFORE any command is read.
//
// WHY. The extractor takes the tokens that follow `make` up to the end of the
// line. A multi-line invocation — `make dev-up \` then its arguments — therefore
// yielded the target `\`, and the gate reported that the Makefile declares no
// such target. The Makefile was fine; the reader was not, and its verdict was a
// property of formatting rather than of the tree.
//
// Continuations are spliced the way the shell splices them (backslash + newline
// vanish, the next line continues the same command), so what the gate reads is
// what CI executes.
func joinContinuations(script string) string {
	return strings.ReplaceAll(script, "\\\n", " ")
}

// maskExpressions collapses a GitHub expression into ONE space-free token before
// the command is split into fields.
//
// WHY. `${{ matrix.shard.images }}` carries spaces, so field-splitting tore it
// into `${{`, `matrix.shard.images`, `}}` — and each fragment was then read as a
// make target. The gate reported three targets the Makefile does not declare, all
// three invented by its own reader: the expression is substituted by the runner
// BEFORE the shell ever sees it, and never reaches make as separate words.
//
// It is masked rather than expanded on purpose: its value is decided at run time,
// so any expansion here would be a guess. One opaque token is honest — the reader
// says «a value goes here», not «this value goes here».
var ghExprRe = regexp.MustCompile(`\$\{\{[^}]*\}\}`)

func maskExpressions(script string) string {
	return ghExprRe.ReplaceAllString(script, "GH_EXPR")
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
	path := filepath.Join(ciDirectory(root, dir), "Makefile")
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
	wrapped := 0
	for _, inv := range invocations {
		if inv.wrapped {
			wrapped++
		}
	}
	t.Logf("checked %d make command(s) issued by CI; under stand-up: %d", len(invocations), wrapped)

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
