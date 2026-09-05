// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// coverage_test.go answers the question no individual analyser can answer about
// itself: which services are judged at all.
//
// # The defect this exists for
//
// Every analyser of this class was correct about the service it was pointed at, and
// the set of services it was pointed at was written by hand — once in the CI loop
// (`for svc in compute nlb registry storage vpc`) and once implicitly, by whoever
// remembered to create a tools/auditlistfilter directory. Two services were never in
// either list: kacho-iam, which has the largest listing surface in the repository,
// and kacho-geo. Nothing was red, because nothing was looking.
//
// A hand-written census of what to check has exactly the failure mode the gates
// themselves were written against: it is silent about what it omits. So the set is
// derived from the tree, and a service that is not judged is a FINDING — the same
// standing as a service that is judged and fails.
//
// # Why "every service", with no "does it have a listing surface?" test
//
// A conditional membership rule needs a predicate for "this service has a public
// List", and that predicate is exactly the thing that keeps being written slightly
// too narrowly. The rule here is unconditional, and its PREMISE is asserted
// separately below (TestCoverage_PremiseEveryServiceHasAListingSurface): every
// service currently does have listing RPCs, so "all of them" costs nothing and
// cannot be quietly narrowed. If a service without a listing surface is ever added,
// that premise assertion fails and names it — which is a decision to record, not a
// skip to take silently.
//
// # Why the service list comes from git and not from the disk
//
// A stray directory beside the repository must not change the verdict, and a
// worktree carrying an untracked scratch service must not either. `git ls-tree`
// answers about the committed tree.
package listfiltergate

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// repoRootForCoverage walks up from this file until it finds the repository root
// (the directory holding both .github/workflows and services/).
//
// It fails rather than returning "": a test that cannot find the tree has proven
// nothing, and must not be indistinguishable from one that found it clean.
func repoRootForCoverage(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate this test file, so the tree was never opened")
	}
	dir := filepath.Dir(self)
	for range 12 {
		_, werr := os.Stat(filepath.Join(dir, ".github", "workflows"))
		_, serr := os.Stat(filepath.Join(dir, "services"))
		if werr == nil && serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find the repository root above this test file — nothing was examined")
	return ""
}

// servicesFromGit lists services/<name> from the COMMITTED tree.
//
// Deliberately not os.ReadDir: an untracked directory beside a tracked one would
// otherwise join or leave the census depending on whose worktree ran the test.
func servicesFromGit(t *testing.T, root string) []string {
	t.Helper()
	cmd := gitenv.Command(root, "ls-tree", "--name-only", "-d", "HEAD", "services/")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-tree services/ failed: %v — the service census could not be taken, "+
			"so this test proves nothing", err)
	}
	var svcs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		svcs = append(svcs, strings.TrimPrefix(line, "services/"))
	}
	sort.Strings(svcs)
	if len(svcs) == 0 {
		t.Fatal("git ls-tree returned no service directories — zero findings must not be " +
			"reachable from zero reads")
	}
	return svcs
}

// hasAnalyser reports whether services/<svc>/tools/auditlistfilter exists in the
// committed tree. This is the predicate under test, factored out so the positive
// control below can exercise the SAME code path on an input it must reject.
func hasAnalyser(root, svc string) bool {
	cmd := gitenv.Command(root, "ls-tree", "--name-only", "HEAD",
		"services/"+svc+"/tools/auditlistfilter/")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// TestCoverage_EveryServiceHasAListFilterAnalyser is the finding-producing half.
//
// A service with no analyser is not "not applicable" and not "nothing to report" —
// it is unjudged, and unjudged is the state every gate of this class exists to make
// impossible. iam and geo sat in it for the whole life of the class.
func TestCoverage_EveryServiceHasAListFilterAnalyser(t *testing.T) {
	root := repoRootForCoverage(t)
	svcs := servicesFromGit(t, root)

	var missing []string
	for _, svc := range svcs {
		if !hasAnalyser(root, svc) {
			missing = append(missing, svc)
		}
	}

	// The census is an assertion, not narration: "examined N" has to be readable off
	// a passing run, or a run that examined nothing looks exactly like a clean one.
	t.Logf("examined %d service(s) from the committed tree: %s", len(svcs), strings.Join(svcs, " "))
	if len(svcs) < 2 {
		t.Fatalf("only %d service(s) discovered — the census is implausible; the predicate "+
			"is reading the wrong tree", len(svcs))
	}

	if len(missing) > 0 {
		t.Errorf("%d of %d service(s) have no public-List analyser: %s\n"+
			"  a service nobody analyses is unjudged, which is the defect this class of gate "+
			"exists to prevent — not an exemption.\n"+
			"  add services/<svc>/tools/auditlistfilter (see pkg/listfiltergate for the "+
			"analyser and services/<svc>/tools/auditlistfilter/profile.go for how a service "+
			"declares its layout), a Makefile target, and the CI step.",
			len(missing), len(svcs), strings.Join(missing, " "))
	}
}

// TestCoverage_PredicateFindsAMissingAnalyser is the paired positive.
//
// The assertion above has ABSENCE as its subject, and a gate whose subject is
// absence goes green three ways: when there is nothing to find, when the search is
// broken, and when the tree never opened. So the same predicate is handed an input
// it MUST reject — a service name that certainly has no analyser — and is required
// to say so. Without this, deleting the body of hasAnalyser would leave the suite
// green.
func TestCoverage_PredicateFindsAMissingAnalyser(t *testing.T) {
	root := repoRootForCoverage(t)

	const absent = "no-such-service-listfiltergate-control"
	if hasAnalyser(root, absent) {
		t.Fatalf("hasAnalyser reported an analyser for %q, which does not exist — the "+
			"predicate cannot tell present from absent, so every green verdict it "+
			"produced above is meaningless", absent)
	}

	// And the mirror: a service that DOES have one must be recognised, or the
	// predicate is simply "always false" and the finding half would be vacuously
	// loud rather than vacuously quiet.
	const present = "registry"
	if !hasAnalyser(root, present) {
		t.Fatalf("hasAnalyser reported no analyser for services/%s, which has one — the "+
			"predicate answers false unconditionally", present)
	}
}

// TestCoverage_CIRunsEveryAnalyser locks the other half of "is this gate real?".
//
// An analyser nothing invokes is worth exactly as much as an analyser that checks
// nothing, and the two hide each other: registry once had both, and fixing either
// alone would have left the invariant unenforced. The CI step's service loop is
// therefore read out of the workflow and compared against the tree.
func TestCoverage_CIRunsEveryAnalyser(t *testing.T) {
	root := repoRootForCoverage(t)
	svcs := servicesFromGit(t, root)

	wf := filepath.Join(root, ".github", "workflows", "ci.yaml")
	raw, err := os.ReadFile(wf) // #nosec G304 -- fixed path (.github/workflows/ci.yaml) under this module's own repository root
	if err != nil {
		t.Fatalf("read %s: %v — the workflow was never opened, so this proves nothing", wf, err)
	}
	text := string(raw)

	// The step is identified by the command it runs, not by its display name: a name
	// is prose and can drift, `make -C "services/${svc}" audit-list-filter` is the
	// thing that executes.
	if !strings.Contains(text, "audit-list-filter") {
		t.Fatal("ci.yaml contains no audit-list-filter invocation at all — every analyser in " +
			"the tree is dead code")
	}

	// The set is read off the loop that drives the step, NOT searched for anywhere in
	// the file. A whole-file substring search answers "yes" for `iam` because the
	// three letters occur in unrelated steps — the exact defect this class is about:
	// a predicate that matches a name instead of the thing the name refers to.
	ran := ciAuditedServices(t, text)
	t.Logf("audit-list-filter CI step drives: %s", strings.Join(ran, " "))
	if len(ran) == 0 {
		t.Fatal("could not read the service list out of the audit-list-filter CI step — the " +
			"wiring was not examined, so nothing about it is proven")
	}

	inCI := map[string]bool{}
	for _, s := range ran {
		inCI[s] = true
	}
	var notRun []string
	for _, svc := range svcs {
		if !inCI[svc] {
			notRun = append(notRun, svc)
		}
	}
	// The mirror: a name driven by CI that is not a service in the tree is equally a
	// finding — it is an invocation with nothing left to invoke, and it will silently
	// adopt the next directory to take that name.
	svcSet := map[string]bool{}
	for _, s := range svcs {
		svcSet[s] = true
	}
	var stale []string
	for _, s := range ran {
		if !svcSet[s] {
			stale = append(stale, s)
		}
	}

	t.Logf("checked CI wiring for %d service(s)", len(svcs))
	if len(notRun) > 0 {
		t.Errorf("%d service(s) are not named in the audit-list-filter CI step: %s\n"+
			"  an analyser that exists but is never invoked is indistinguishable from an "+
			"absent one, and worse, because it counts as present in the census.",
			len(notRun), strings.Join(notRun, " "))
	}
	if len(stale) > 0 {
		t.Errorf("the audit-list-filter CI step names %d entry(ies) that are not services in "+
			"the committed tree: %s\n"+
			"  the invocation has nothing left to invoke; drop it, or the next directory of "+
			"that name inherits an expectation nobody stated.", len(stale), strings.Join(stale, " "))
	}
}

// ciAuditedServices extracts the service list from the `for svc in …; do` line that
// drives the audit-list-filter step.
//
// Scoped to that step on purpose: the question is which services THIS step runs, and
// a file-wide search cannot answer it — service names are ordinary words that occur
// in other steps, other comments and other paths.
func ciAuditedServices(t *testing.T, workflow string) []string {
	t.Helper()
	lines := strings.Split(workflow, "\n")
	// Find the loop whose body invokes audit-list-filter. The loop header and the
	// invocation are within a handful of lines of each other in a shell `run:` block.
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "for svc in ") {
			continue
		}
		body := strings.Join(lines[i:min(i+12, len(lines))], "\n")
		if !strings.Contains(body, "audit-list-filter") {
			continue
		}
		list := strings.TrimPrefix(trimmed, "for svc in ")
		list = strings.TrimSuffix(strings.TrimSpace(list), "; do")
		list = strings.TrimSuffix(strings.TrimSpace(list), ";")
		var out []string
		for _, f := range strings.Fields(list) {
			if f == "do" {
				continue
			}
			out = append(out, f)
		}
		sort.Strings(out)
		return out
	}
	return nil
}

// TestCoverage_PremiseEveryServiceHasAListingSurface asserts the premise the
// unconditional rule above rests on.
//
// The rule is "every service has an analyser", with no "does this one need one?"
// test — because that test is exactly the thing that keeps being written slightly
// too narrowly. The rule is safe only while every service really does expose a
// listing surface. That is a fact about the tree, it can change, and when it changes
// this assertion names the service instead of the rule quietly becoming a demand for
// a pointless analyser.
func TestCoverage_PremiseEveryServiceHasAListingSurface(t *testing.T) {
	root := repoRootForCoverage(t)
	svcs := servicesFromGit(t, root)

	var without []string
	counts := map[string]int{}
	for _, svc := range svcs {
		n := countListDeclarations(t, root, svc)
		counts[svc] = n
		if n == 0 {
			without = append(without, svc)
		}
	}
	for _, svc := range svcs {
		t.Logf("  services/%-9s %3d List* transport declaration(s)", svc, counts[svc])
	}
	if len(without) > 0 {
		t.Errorf("%d service(s) declare no List* transport method: %s\n"+
			"  the unconditional \"every service has an analyser\" rule assumed every service "+
			"has a listing surface. That is no longer true, so the rule now demands an "+
			"analyser with nothing to analyse. Decide explicitly: either the service really "+
			"has no public list (record it), or its transport layout is not being read.",
			len(without), strings.Join(without, " "))
	}
}

// countListDeclarations counts methods whose name starts with List, declared on a
// pointer receiver, in the committed non-test Go files of one service.
//
// Deliberately coarse: it answers "is there a listing surface here at all", not
// "which resource does this belong to" — that second question is the analyser's, and
// answering it needs the service's Profile.
func countListDeclarations(t *testing.T, root, svc string) int {
	t.Helper()
	cmd := gitenv.Command(root, "grep", "-c", "-E",
		`^func \([a-zA-Z_][a-zA-Z0-9_]* \*[A-Za-z0-9_]+\) List[A-Za-z0-9_]*\(`,
		"HEAD", "--", "services/"+svc+"/internal/")
	out, _ := cmd.Output() // exit 1 simply means "no matches"
	total := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" || strings.Contains(line, "_test.go") {
			continue
		}
		idx := strings.LastIndex(line, ":")
		if idx < 0 {
			continue
		}
		n := 0
		for _, c := range line[idx+1:] {
			if c < '0' || c > '9' {
				n = 0
				break
			}
			n = n*10 + int(c-'0')
		}
		total += n
	}
	return total
}
