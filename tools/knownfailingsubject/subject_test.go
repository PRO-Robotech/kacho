// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package knownfailingsubject

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── fixtures ────────────────────────────────────────────────────────────────

// suite writes a minimal newman suite: one collection of folders and steps, plus
// the results doc. Everything the gate reads, nothing it does not.
func suite(t *testing.T, root, svc, results string, folders map[string][]string) {
	t.Helper()
	base := filepath.Join(root, "services", svc, "tests", "newman")
	mustWrite(t, filepath.Join(base, "docs", "RESULTS.md"), results)

	var items []any
	for folder, steps := range folders {
		var sub []any
		for _, s := range steps {
			sub = append(sub, map[string]any{"name": s, "request": map[string]any{"method": "GET"}})
		}
		items = append(items, map[string]any{"name": folder, "item": sub})
	}
	raw, err := json.Marshal(map[string]any{"info": map[string]any{"name": svc}, "item": items})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(base, "collections", "main.postman_collection.json"), string(raw))
}

// report writes a newman JSON report for the "main" collection in the shape newman
// really emits: an execution carries a `cursor.position` into the flattened
// collection and a `response`, and a failure is booked under `parent`/`source`
// names. The positions are recomputed here from the collection on disk, so the
// fixture cannot agree with the gate by sharing its code — if the gate's idea of
// cursor order is wrong, these tests say so.
func report(t *testing.T, root, svc string, executed, failed []string) {
	t.Helper()
	base := filepath.Join(root, "services", svc, "tests", "newman")
	// #nosec G304 -- фикстура читает файл, который сама только что записала.
	raw, err := os.ReadFile(filepath.Join(base, "collections", "main.postman_collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coll struct {
		Item []struct {
			Name string `json:"name"`
			Item []struct {
				Name string `json:"name"`
			} `json:"item"`
		} `json:"item"`
	}
	if uerr := json.Unmarshal(raw, &coll); uerr != nil {
		t.Fatal(uerr)
	}

	position := map[string][]int{} // folder name → its leaf positions
	pos := 0
	for _, folder := range coll.Item {
		for range folder.Item {
			position[folder.Name] = append(position[folder.Name], pos)
			pos++
		}
	}

	var execs, fails []any
	for _, f := range executed {
		for _, p := range position[f] {
			execs = append(execs, map[string]any{
				"cursor":   map[string]any{"position": p, "length": pos},
				"item":     map[string]any{"name": "step"},
				"response": map[string]any{"code": 200},
			})
		}
	}
	for _, f := range failed {
		fails = append(fails, map[string]any{
			"error":  map[string]any{"name": "AssertionError", "test": "something must hold"},
			"source": map[string]any{"name": "step"},
			"parent": map[string]any{"name": f},
		})
	}
	body, merr := json.Marshal(map[string]any{"run": map[string]any{
		"executions": execs, "failures": fails,
	}})
	if merr != nil {
		t.Fatal(merr)
	}
	mustWrite(t, filepath.Join(base, "out", "main.json"), string(body))
}

// openTracker / closedTracker stand in for the issue tracker without a network.
func openTracker(string, int) (IssueState, error)   { return StateOpen, nil }
func closedTracker(string, int) (IssueState, error) { return StateClosed, nil }

const liveDeclaration = `# RESULTS

## Known failing — product bugs

| Case | Diverges | Since | Noticed when fixed |
|---|---|---|---|
| ` + "`SVC-GET-CRUD-OK`" + ` | Get of own resource answers 500 | 2026-07-30 | ` + "`PRO-Robotech/kacho#4242`" + ` closes, or this gate reports the case passing |
`

// ─── (а) a declaration whose case is not in the suite ────────────────────────

// TestFiresWhenTheNamedCaseIsNotInTheSuite is the first half of "a declaration
// must have a subject". A row naming a case the suite no longer generates asserts
// something about a case that cannot fail, cannot pass and cannot be looked at.
func TestFiresWhenTheNamedCaseIsNotInTheSuite(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", liveDeclaration, map[string][]string{
		"SVC-LST-CRUD-OK — lists things": {"list"},
	})

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err == nil {
		t.Fatalf("gate is silent on a declaration naming an absent case; census %+v", rep.Census)
	}
	if !containsAll(rep.Findings, "SVC-GET-CRUD-OK", "RESULTS.md:7") {
		t.Fatalf("finding must name the case and the coordinate, got: %v", rep.Findings)
	}
}

// ─── (б) a declaration whose case passes ─────────────────────────────────────

// TestFiresWhenTheNamedCasePasses is the half no earlier gate had. A record that
// outlives its fix keeps asserting the product is broken; the run report is the
// only thing that can contradict it, so the gate must read it.
func TestFiresWhenTheNamedCasePasses(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", liveDeclaration, map[string][]string{
		"SVC-GET-CRUD-OK — get own resource": {"get"},
	})
	report(t, root, "svc", []string{"SVC-GET-CRUD-OK — get own resource"}, nil)

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err == nil {
		t.Fatalf("gate is silent on a declaration whose case passed in the report; census %+v", rep.Census)
	}
	if !containsAll(rep.Findings, "SVC-GET-CRUD-OK", "PASSED") {
		t.Fatalf("finding must say the named case passed, got: %v", rep.Findings)
	}
}

// ─── (в) a legitimate declaration ────────────────────────────────────────────

// TestSilentOnALiveDeclaration is the other direction of the injection proof: the
// same shape, with a subject that exists, is red in the report and names an open
// issue, must produce nothing. A gate that fires here gets switched off by the
// first person with a genuine red to declare.
func TestSilentOnALiveDeclaration(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", liveDeclaration, map[string][]string{
		"SVC-GET-CRUD-OK — get own resource": {"get"},
	})
	report(t, root, "svc",
		[]string{"SVC-GET-CRUD-OK — get own resource"},
		[]string{"SVC-GET-CRUD-OK — get own resource"})

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err != nil {
		t.Fatalf("gate fires on a legitimate declaration: %v", rep.Findings)
	}
	if rep.Census.Declarations != 1 || rep.Census.SubjectsResolved != 1 {
		t.Fatalf("census must show what it judged, got %+v", rep.Census)
	}
}

// ─── liveness: the issue behind the record ───────────────────────────────────

// TestFiresWhenTheIssueIsClosed keeps a record from outliving the fix along the
// dimension the tracker can answer even when no run report exists.
func TestFiresWhenTheIssueIsClosed(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", liveDeclaration, map[string][]string{
		"SVC-GET-CRUD-OK — get own resource": {"get"},
	})

	rep, err := Scan(Options{Root: root, IssueState: closedTracker})
	if err == nil {
		t.Fatalf("gate is silent while the declared issue is closed; census %+v", rep.Census)
	}
	if !containsAll(rep.Findings, "#4242", "no longer open") {
		t.Fatalf("finding must name the closed issue, got: %v", rep.Findings)
	}
}

// TestFiresWhenNoIssueIsNamed — an exclusion nobody can check never expires.
func TestFiresWhenNoIssueIsNamed(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", `# RESULTS

## Known failing — product bugs

- `+"`SVC-GET-CRUD-OK`"+` — flakes sometimes, we will look at it later.
`, map[string][]string{"SVC-GET-CRUD-OK — get own resource": {"get"}})

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err == nil {
		t.Fatalf("gate is silent on a declaration with nothing to expire; census %+v", rep.Census)
	}
	if !containsAll(rep.Findings, "names no issue") {
		t.Fatalf("finding must say the row names no issue, got: %v", rep.Findings)
	}
}

// TestUnresolvableTrackerIsAnnouncedNotPassed — the soft dimension. A tracker that
// cannot be reached must never read as "still open": the record is printed by
// coordinate and counted, so "the check did not run" stays distinguishable from
// "the check passed".
func TestUnresolvableTrackerIsAnnouncedNotPassed(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", liveDeclaration, map[string][]string{
		"SVC-GET-CRUD-OK — get own resource": {"get"},
	})
	report(t, root, "svc",
		[]string{"SVC-GET-CRUD-OK — get own resource"},
		[]string{"SVC-GET-CRUD-OK — get own resource"})

	rep, err := Scan(Options{Root: root, IssueState: func(string, int) (IssueState, error) {
		return StateUnknown, errors.New("tracker unreachable")
	}})
	if err != nil {
		t.Fatalf("an unreachable tracker is an environment, not a verdict: %v", rep.Findings)
	}
	if len(rep.Unverified) == 0 {
		t.Fatal("liveness went unchecked and unannounced — that is the soft pass this gate must not have")
	}
	if !containsAll(rep.Unverified, "#4242") {
		t.Fatalf("the unverified record must be named, got: %v", rep.Unverified)
	}
}

// ─── "not executed" is not "passed" ──────────────────────────────────────────

// TestUnexecutedCaseIsNotAPass — a folder absent from the report did not run, and
// a run that did not happen is never counted as agreement (testing.md).
func TestUnexecutedCaseIsNotAPass(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", liveDeclaration, map[string][]string{
		"SVC-GET-CRUD-OK — get own resource": {"get"},
		"SVC-LST-CRUD-OK — lists things":     {"list"},
	})
	report(t, root, "svc", []string{"SVC-LST-CRUD-OK — lists things"}, nil)

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err != nil {
		t.Fatalf("a case that never ran is not evidence that the record is stale: %v", rep.Findings)
	}
	if !containsAll(rep.Unverified, "SVC-GET-CRUD-OK", "did not run") {
		t.Fatalf("the unexecuted subject must be announced, got: %v", rep.Unverified)
	}
}

// ─── archived records are records, not declarations ──────────────────────────

// TestDoesNotFireOnAResolvedRecord is the over-fire guard for the historical half
// of these files: a section saying a red was FIXED is a record, and a record is
// allowed to name cases that now pass — that is what it is for. The skip is
// counted so it cannot become a quiet way out.
func TestDoesNotFireOnAResolvedRecord(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", `# RESULTS

## Closed — was known failing, fixed in the product

| Case | Was | Disposition |
|---|---|---|
| `+"`SVC-GET-CRUD-OK`"+` | answered 500 | **FIXED 2026-07-30** — the handler maps the error now |
`, map[string][]string{"SVC-GET-CRUD-OK — get own resource": {"get"}})
	report(t, root, "svc", []string{"SVC-GET-CRUD-OK — get own resource"}, nil)

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err != nil {
		t.Fatalf("gate fires on a resolution record: %v", rep.Findings)
	}
	if rep.Census.ArchivedSections != 1 {
		t.Fatalf("the skip must be counted, got %+v", rep.Census)
	}
}

// TestProseWordDoesNotArchiveADeclaration is the sharp edge of the "this one is
// closed" exemption. A record is exempt when it SAYS it is resolved — as a verdict,
// which these docs write in capitals (**FIXED**, СНЯТО). The lower-case word inside a
// quoted error message is not a verdict about anything, and treating it as one silently
// exempts a live declaration: measured on the real tree, a flagged IPAM record went
// unread because the error text it quoted contained "resolved".
func TestProseWordDoesNotArchiveADeclaration(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", `# RESULTS

## Known failing — product bugs

> **Under investigation (flagged, not masked)** — `+"`SVC-GET-CRUD-OK`"+` fails with
> `+"`no address pool resolved for address … (family=0)`"+`, a deterministic Operation error.
`, map[string][]string{"SVC-GET-CRUD-OK — get own resource": {"get"}})

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err == nil {
		t.Fatalf("a quoted error message exempted a live declaration; census %+v", rep.Census)
	}
	if !containsAll(rep.Findings, "names no issue") {
		t.Fatalf("finding must be about the missing issue, got: %v", rep.Findings)
	}
}

// TestUppercaseDispositionArchivesOneRow — the same exemption where it belongs: one row
// of a live section, marked with a verdict, naming a case that now passes.
func TestUppercaseDispositionArchivesOneRow(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", liveDeclaration+`
| `+"`SVC-LST-CRUD-OK`"+` | listed nothing | 2026-07-01 | **FIXED 2026-07-29** — the filter is applied now |
`, map[string][]string{
		"SVC-GET-CRUD-OK — get own resource": {"get"},
		"SVC-LST-CRUD-OK — lists things":     {"list"},
	})
	report(t, root, "svc",
		[]string{"SVC-GET-CRUD-OK — get own resource", "SVC-LST-CRUD-OK — lists things"},
		[]string{"SVC-GET-CRUD-OK — get own resource"})

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err != nil {
		t.Fatalf("gate fires on a row that states its own fix: %v", rep.Findings)
	}
	if rep.Census.Declarations != 1 || rep.Census.ArchivedSections != 1 {
		t.Fatalf("one live declaration and one skipped record expected, got %+v", rep.Census)
	}
}

// TestFiresOnADeclarationNamingNothingResolvable states the gate's own premise: a
// case id is UPPER-KEBAB and a step is matched by exact name against the generated
// collections, so a row naming neither cannot be checked by anything here — and
// pretending otherwise is how a slice of these files would go silently unread.
func TestFiresOnADeclarationNamingNothingResolvable(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", `# RESULTS

## Known failing — product bugs

- the owner-tuple lag past `+"`retry_until_authorized`"+`, tracked in `+"`PRO-Robotech/kacho#4242`"+`.
`, map[string][]string{"SVC-GET-CRUD-OK — get own resource": {"get"}})

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err == nil {
		t.Fatalf("gate is silent on a declaration with no nameable subject; census %+v", rep.Census)
	}
	if !containsAll(rep.Findings, "names no subject") {
		t.Fatalf("finding must say the row names no subject, got: %v", rep.Findings)
	}
}

// TestLowercaseStepNameResolves — the other side of that premise: the iam record
// naming one step, lowercase, is accepted when the step really exists.
func TestLowercaseStepNameResolves(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", `# RESULTS

## Known failing — product bugs

- `+"`inv-get-account-allow-warm-cache`"+` — grant→check warm-cache window, `+"`PRO-Robotech/kacho#4242`"+`, since 2026-07-30; noticed when this gate reports it passing.
`, map[string][]string{"SVC-GET-CRUD-OK — get own resource": {"inv-get-account-allow-warm-cache"}})

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err != nil {
		t.Fatalf("gate fires on a record naming a step that exists: %v", rep.Findings)
	}
	if rep.Census.SubjectsResolved != 1 {
		t.Fatalf("the step must count as a resolved subject, got %+v", rep.Census)
	}
}

// ─── census ──────────────────────────────────────────────────────────────────

// TestZeroFindingsIsNotReachableFromZeroReads — "nothing declared" and "nothing
// read" exit the same way and must never print the same thing.
func TestZeroFindingsIsNotReachableFromZeroReads(t *testing.T) {
	rep, err := Scan(Options{Root: t.TempDir(), IssueState: openTracker})
	if err == nil {
		t.Fatalf("a walk that read no results doc proved nothing; census %+v", rep.Census)
	}
	if !containsAll(rep.Findings, "read 0") {
		t.Fatalf("finding must state the census, got: %v", rep.Findings)
	}
}

// TestCensusCountsSuitesAndReports — the gate states the volume it examined.
func TestCensusCountsSuitesAndReports(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", liveDeclaration, map[string][]string{
		"SVC-GET-CRUD-OK — get own resource": {"get"},
	})
	suite(t, root, "other", "# RESULTS\n\nnothing declared here.\n", map[string][]string{
		"OTH-GET-CRUD-OK — get": {"get"},
	})
	report(t, root, "svc",
		[]string{"SVC-GET-CRUD-OK — get own resource"},
		[]string{"SVC-GET-CRUD-OK — get own resource"})

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err != nil {
		t.Fatalf("unexpected findings: %v", rep.Findings)
	}
	c := rep.Census
	if c.Docs != 2 || c.Collections != 2 || c.Reports != 1 || c.Declarations != 1 {
		t.Fatalf("census does not describe the tree it read: %+v", c)
	}
}

// ─── the real tree ───────────────────────────────────────────────────────────

// TestTreeDeclarationsHaveSubjects runs the gate over this repository. It is the
// enforcement, not a demonstration: a declaration added without a subject fails
// here before it reaches CI. Liveness degrades to unverified without a tracker,
// and reports are consulted only where a run left them.
func TestTreeDeclarationsHaveSubjects(t *testing.T) {
	rep, err := Scan(Options{Root: repoRoot(t)})
	t.Logf("census: %+v", rep.Census)
	for _, u := range rep.Unverified {
		t.Logf("unverified: %s", u)
	}
	if err != nil {
		for _, f := range rep.Findings {
			t.Errorf("%s", f)
		}
		t.Fatalf("%v", err)
	}
	if rep.Census.Docs == 0 {
		t.Fatal("the gate read no results doc in this repository — it proved nothing")
	}
}

// TestCIRunsThisGate locks the wiring, in both places it belongs: the static
// pipeline (subject + liveness, no stand needed) and the e2e job, where run
// reports exist and "the named case passes" can finally be answered.
func TestCIRunsThisGate(t *testing.T) {
	const invocation = "go run ./tools/knownfailingsubject/cmd/verify-known-failing-subject"
	for _, wf := range []string{"ci.yaml", "e2e-newman.yml"} {
		b, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", wf))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), invocation) {
			t.Fatalf("%s does not run %q — a gate CI does not call is worth no gate", wf, invocation)
		}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func containsAll(lines []string, needles ...string) bool {
	joined := strings.Join(lines, "\n")
	for _, n := range needles {
		if !strings.Contains(joined, n) {
			return false
		}
	}
	return true
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
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

// TestBacktickSpanAcrossLinesDoesNotHideTheNextName is about pairing, not about names.
// Markdown in these docs is hard-wrapped, so a backticked span — very often a quoted
// error message — can straddle a line break. Matching backticks line by line then fails
// to close that span, every following pair is offset by one, and the case id after it
// becomes invisible: no subject, no finding, nothing said. Measured on the real tree: a
// flagged IPAM record named three case ids and only the first was ever read.
func TestBacktickSpanAcrossLinesDoesNotHideTheNextName(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "svc", `# RESULTS

## Known failing — product bugs

> **Under investigation** — `+"`SVC-LST-CRUD-OK`"+` fails with `+"`no address pool resolved"+`
> `+"for address … (family=0)`"+`, and `+"`SVC-GET-CRUD-OK`"+` is the same root cause.
> Tracked in `+"`PRO-Robotech/kacho#4242`"+`.
`, map[string][]string{
		"SVC-GET-CRUD-OK — get own resource": {"get"},
		"SVC-LST-CRUD-OK — lists things":     {"list"},
	})

	rep, err := Scan(Options{Root: root, IssueState: openTracker})
	if err != nil {
		t.Fatalf("unexpected findings: %v", rep.Findings)
	}
	if rep.Census.SubjectsResolved != 2 {
		t.Fatalf("both named cases must be read as subjects, got %+v", rep.Census)
	}
}
