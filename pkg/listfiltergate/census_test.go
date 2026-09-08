// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// census_test.go asserts a property that is separate from, and easy to mistake for,
// the one the analysers assert: that every transport listing method in a service is
// SEEN by that service's analyser at all.
//
// # Why this is its own check
//
// Widening the identification predicate from "the method named List" to "every
// method named List…" closes one gap and reads as if it closed the class. It does
// not. A method can be invisible for a second, independent reason: it lives outside
// the anchor root the profile names. vpc had exactly that — a listing RPC in a
// SECOND transport package (internal/handler, beside the per-resource packages under
// internal/apps/kaname/api), returning NIC attachments for instance ids the caller
// supplies with no per-RPC check behind it. Widening the name predicate did not
// reach it, and the analyser's census went on reporting a number that looked
// complete because it counted only what the analyser had looked at.
//
// That is the trap this file exists for: a census is a statement about what was
// examined, and it cannot, by construction, report what was never in view. So the
// count has to be compared against a source the analyser does not control — here, a
// sweep of the committed tree.
//
// # What it does NOT assert
//
// It does not judge whether a listing method narrows correctly. That is each
// analyser's job. This asks only "was it looked at", which is the question that was
// answered wrongly for twenty methods across four services, and then for one more
// after those twenty were fixed.
package listfiltergate

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// transportListingRe matches a listing method declared on a pointer receiver whose
// type name ends in Handler — the transport surface of every service in this repo.
//
// Deliberately anchored at the start of a line so a call, a comment or an interface
// method declaration cannot match: this sweep exists to be a SECOND opinion, and a
// second opinion that over-counts is as useless as one that under-counts.
var transportListingRe = regexp.MustCompile(
	`^func \([a-z_][a-zA-Z0-9_]* \*([A-Za-z0-9_]*Handler)\) (List[A-Za-z0-9_]*)\(`)

// treeListings sweeps the committed tree for transport listing methods of one
// service, returning "<Type>.<Method>" for each.
func treeListings(t *testing.T, root, svc string) []string {
	t.Helper()
	cmd := gitenv.Command(root, "grep", "-h", "-E",
		`^func \([a-z_][a-zA-Z0-9_]* \*[A-Za-z0-9_]*Handler\) List[A-Za-z0-9_]*\(`,
		"HEAD", "--", "services/"+svc+"/internal/")
	out, _ := cmd.Output() // exit 1 simply means "no matches"

	var found []string
	for _, line := range strings.Split(string(out), "\n") {
		m := transportListingRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		found = append(found, m[1]+"."+m[2])
	}
	sort.Strings(found)
	return found
}

// analyserListingCount runs a service's analyser and reads the listing-method count
// out of its census line.
func analyserListingCount(t *testing.T, root, svc string) int {
	t.Helper()
	cmd := exec.Command("make", "-C", "services/"+svc, "audit-list-filter")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("services/%s analyser did not pass, so its census cannot be trusted as a "+
			"measurement:\n%s", svc, out)
	}
	total := 0
	// The count is read from the CENSUS SENTENCE, not from anywhere the words
	// happen to appear: the census is the only line that follows the number with the
	// breakdown in brackets. An unanchored match summed every sentence in the
	// output, so a service that merely MENTIONED its listing methods in a second line
	// scored twice — and the failure read "the tree declares 2 but the analyser
	// judged 4", which points at the analyser rather than at this regexp.
	re := regexp.MustCompile(`(\d+) listing method\(s\) \(`)
	for _, m := range re.FindAllStringSubmatch(string(out), -1) {
		n := 0
		for _, c := range m[1] {
			n = n*10 + int(c-'0')
		}
		total += n
	}
	// Older analysers word their census differently, and each such wording is read
	// EXPLICITLY rather than by loosening the anchored match above — loosening is what
	// made the count double before, and the fix for one service must not re-open that
	// for the others.
	//
	//   registry — "N List RPC(s)"
	//   storage  — "N listing method(s) declared on the transport surface"
	//
	// storage's form was invisible the moment the anchored match began requiring the
	// bracket that the newer census prints (#684 tightened it for the five profiles it
	// migrated; storage is a sixth service carrying this tool and was not among them).
	// Its census then read as zero — and the count of a service whose census cannot be
	// parsed must never be indistinguishable from the count of a service that has no
	// listings, which is precisely what the guard below refuses.
	//
	// Each fallback is tried only while nothing has been read, so a service cannot be
	// scored twice by matching two forms at once.
	for _, alt := range []*regexp.Regexp{
		regexp.MustCompile(`(\d+) List RPC\(s\)`),
		regexp.MustCompile(`(\d+) listing method\(s\) declared`),
	} {
		if total != 0 {
			break
		}
		for _, m := range alt.FindAllStringSubmatch(string(out), -1) {
			n := 0
			for _, c := range m[1] {
				n = n*10 + int(c-'0')
			}
			total += n
		}
	}
	if total == 0 {
		t.Fatalf("could not read a listing-method count out of services/%s's census — this test "+
			"would otherwise score an unreadable census as zero and pass whenever the tree also "+
			"had none:\n%s", svc, out)
	}
	return total
}

// TestCensus_EveryTransportListingIsSeenByItsAnalyser compares each analyser's own
// count against a sweep of the committed tree.
func TestCensus_EveryTransportListingIsSeenByItsAnalyser(t *testing.T) {
	if testing.Short() {
		t.Skip("runs each service's analyser; skipped in -short")
	}
	root := repoRootForCoverage(t)
	svcs := servicesFromGit(t, root)

	type row struct {
		svc        string
		tree, seen int
	}
	var rows []row
	for _, svc := range svcs {
		tree := treeListings(t, root, svc)
		seen := analyserListingCount(t, root, svc)
		rows = append(rows, row{svc, len(tree), seen})
		if len(tree) != seen {
			t.Errorf("services/%s: the tree declares %d transport listing method(s) but its "+
				"analyser judged %d\n  tree: %s\n"+
				"  a method can be invisible for two independent reasons — its NAME is outside "+
				"the identification predicate, or its PACKAGE is outside the anchor root. Widening "+
				"the first does not close the second, and the analyser's own census cannot report "+
				"what it never had in view.",
				svc, len(tree), seen, strings.Join(tree, " "))
		}
	}
	for _, r := range rows {
		t.Logf("  services/%-9s tree=%-3d judged=%-3d", r.svc, r.tree, r.seen)
	}
}

// TestCensus_SweepFindsSomething is the paired positive.
//
// The comparison above is satisfied by two numbers that are both wrong in the same
// direction — most obviously by both being zero, which is what a broken sweep and an
// unopened tree produce together. So the sweep is required to find the listing
// methods that are certainly there.
func TestCensus_SweepFindsSomething(t *testing.T) {
	root := repoRootForCoverage(t)

	// iam has the widest listing surface in the repository; a sweep that cannot see
	// it is broken, whatever it says about anything else.
	iam := treeListings(t, root, "iam")
	if len(iam) < 20 {
		t.Fatalf("the tree sweep found only %d transport listing method(s) in services/iam, "+
			"which is implausible — the predicate is not matching, and every equality it "+
			"reports elsewhere is an agreement between two silences", len(iam))
	}

	// And the mirror: it must NOT match something that only looks like a declaration.
	for _, notADecl := range []string{
		`	rows, _ := h.svc.ListOperations(ctx)`,
		`// func (h *FooHandler) ListBar(ctx context.Context) error`,
		`ListOperations(ctx context.Context) error`,
	} {
		if transportListingRe.MatchString(strings.TrimSpace(notADecl)) {
			t.Errorf("the sweep matched something that is not a method declaration (%q) — it "+
				"over-counts, so an analyser that misses a method could still be scored equal",
				notADecl)
		}
	}
}

// TestCIRunsThisCensus locks the wiring. A gate CI does not call is worth exactly as
// much as no gate, and this one was in that state: it carries a `-short` skip, the fast
// job runs `go test ./... -race -short` (so it skipped), and the integration job selects
// only packages matching `/internal/(repo|clients)` under `services/<svc>/...` (so it
// never reached this package at all). Both halves had to be measured to see it: the skip
// alone looks harmless as long as something else is assumed to run the package.
//
// It was the ONLY test in this package that skipped under -short — the other ten run in
// the fast job — so nothing about the package's own green said the census had happened.
//
// The pattern is the tree's own: four sibling tools (foreignclouds, legacyfolder,
// paginationordergate, knownfailingsubject) each hold a TestCIRunsThisGate beside their
// step. This is the fifth, and it names the invocation rather than the job so that moving
// the step between jobs does not silently unwire it.
func TestCIRunsThisCensus(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootForCoverage(t), ".github", "workflows", "ci.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// The invocation must run this test WITHOUT -short; naming the test explicitly is what
	// makes the step's purpose checkable from here.
	const invocation = "go test ./pkg/listfiltergate/ -run TestCensus_EveryTransportListingIsSeenByItsAnalyser"
	if !strings.Contains(string(b), invocation) {
		t.Fatalf("ci.yaml does not run %q — this census skips under -short and no job "+
			"reaches its package otherwise, so without that step it never executes", invocation)
	}
	// And the step must not re-introduce the skip it exists to escape.
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, invocation) && strings.Contains(line, "-short") {
			t.Fatalf("ci.yaml runs the census with -short, which skips it: %s", strings.TrimSpace(line))
		}
	}
}
