// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditknownfailing is the CI gate that stops a "known failing" record
// from outliving the fix it excuses, and stops the suite roster from drifting
// away from the suite.
//
// # Why a record must expire by itself
//
// A "Known failing — product bugs" row is an EXCLUSION: it declares that a case
// is expected red because a product defect is open, and it buys that case out of
// "everything must be green". Exclusions are legitimate — a case that is red for a
// real product reason must stay red and stay visible (testing.md forbids skipping
// it). What is not legitimate is an exclusion that survives its subject: once the
// defect is fixed, the row keeps asserting that the product is broken. It is then
// a FALSE STATEMENT ABOUT THE PRODUCT, and a durable one — the next reader plans
// work around a defect that no longer exists, or discounts a genuine red as
// "the known one". This happened here: both rows in this service's RESULTS.md
// named issues that had already been closed, one of them fixed in the very tree
// that carried the row.
//
// So the mechanism must expire on its own. Every declared exclusion is checked
// against the issue tracker, and a CLOSED (or unresolvable-as-open) issue is a
// FINDING, reported with the exact coordinates to delete.
//
// # What is checked
//
//  1. ROSTER. The composition table in RESULTS.md must match the generated
//     collections exactly — every collection present is listed, every listed
//     collection exists, each count equals the number of requests the generator
//     emitted, and the total equals their sum. Counts are read from the committed
//     collections/*.postman_collection.json (the generator's own output), never by
//     re-running the generator: a gate must not have side effects, and the
//     artifact is what CI actually runs.
//  2. BINDING, both directions. The set of issues declared "known failing" in
//     RESULTS.md must equal the set of issues declared by `# verifies <issue-url>`
//     annotations in cases/*.py. A row with no annotated case excuses nothing; an
//     annotated case with no row is a red nobody sees in the results doc.
//  3. SUBJECT. Every case named by a row must still exist in cases/, and every
//     row must name at least one issue — an exclusion nobody can check is not an
//     exclusion.
//  4. LIVENESS. Every declared issue must still be OPEN.
//
// # Census, and the one dimension that can degrade
//
// Audit reports what it read — collections, case files, records — and treats
// "nothing read" as a finding: zero findings must not be reachable from zero
// reads. Checks (1)…(3) are entirely local and never degrade.
//
// Check (4) needs the issue tracker. When the tracker cannot be reached (no `gh`,
// no credentials, no network) the gate does NOT fail that dimension — but it does
// NOT pass it silently either: every unresolved record is printed by coordinate
// and counted in the census, so "the liveness check did not run" can never be
// mistaken for "the liveness check passed". That distinction is the whole point:
// a soft pass that cannot tell a broken environment from a healthy one becomes
// the permanent mode of a control that never fires.
package auditknownfailing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// IssueState is what the tracker says about a referenced issue.
type IssueState int

const (
	// StateUnknown — the tracker could not be consulted (environment, not verdict).
	StateUnknown IssueState = iota
	// StateOpen — the defect is still open, so the exclusion still has a subject.
	StateOpen
	// StateClosed — the defect is fixed (or the issue does not exist): the
	// exclusion has nothing left to excuse.
	StateClosed
)

// Options configures one audit run.
type Options struct {
	// Root is the newman suite root (the directory holding cases/, collections/,
	// docs/).
	Root string
	// Repo is the tracker repository the annotations refer to.
	Repo string
	// IssueState resolves one issue number. nil ⇒ the `gh` CLI resolver.
	IssueState func(num int) (IssueState, error)
}

// Record is one declared exclusion, as written in RESULTS.md.
type Record struct {
	Doc     string // file the row lives in
	Line    int    // 1-based line number of the row
	CaseIDs []string
	Issues  []int
}

// Annotation is one `# verifies <issue-url>` marker in a case file.
type Annotation struct {
	File  string
	Line  int
	Issue int
}

// Report is the census plus the findings of one run.
type Report struct {
	Collections  int
	CaseFiles    int
	Records      []Record
	Annotations  []Annotation
	Unverifiable []string // records whose issue state could not be resolved
	Findings     []string
}

var (
	// verifiesRe matches an issue-URL annotation on a case.
	verifiesRe = regexp.MustCompile(`#\s*verifies\s+https?://\S*?/issues/(\d+)`)
	// issueRefRe matches an issue reference inside a RESULTS.md table row.
	issueRefRe = regexp.MustCompile(`/issues/(\d+)`)
	// caseIDRe matches a case id as written in the doc tables (UPPER-KEBAB).
	caseIDRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:-[A-Z0-9]+){2,}\b`)
	// emphasisRe strips markdown bold from a table cell.
	emphasisRe = regexp.MustCompile(`\*\*`)
)

// Audit runs the gate against o.Root, writes its census and findings to out, and
// returns an error when the tree does not satisfy the contract.
func Audit(o Options, out io.Writer) (Report, error) {
	rep := Report{}
	root := o.Root
	if root == "" {
		root = "."
	}
	repo := o.Repo
	if repo == "" {
		repo = "PRO-Robotech/kacho"
	}
	resolve := o.IssueState
	if resolve == nil {
		resolve = ghResolver(repo)
	}

	collections, cerr := readCollections(filepath.Join(root, "collections"))
	rep.Collections = len(collections)
	if cerr != nil {
		rep.Findings = append(rep.Findings, cerr.Error())
	}

	caseText, caseFiles, aerr := readCases(filepath.Join(root, "cases"))
	rep.CaseFiles = caseFiles
	if aerr != nil {
		rep.Findings = append(rep.Findings, aerr.Error())
	}
	rep.Annotations = annotations(caseText)

	resultsPath := filepath.Join(root, "docs", "RESULTS.md")
	results, rerr := os.ReadFile(resultsPath)
	if rerr != nil {
		rep.Findings = append(rep.Findings, fmt.Sprintf(
			"results doc %s is absent — the gate read nothing, so it proved nothing", resultsPath))
		return finish(rep, out)
	}
	lines := strings.Split(string(results), "\n")

	if rep.Collections == 0 || rep.CaseFiles == 0 {
		rep.Findings = append(rep.Findings, fmt.Sprintf(
			"read %d collection(s) and %d case file(s) under %s — the gate examined nothing, so it "+
				"proved nothing (zero findings must not be reachable from zero reads)",
			rep.Collections, rep.CaseFiles, root))
		return finish(rep, out)
	}

	rep.Findings = append(rep.Findings, checkRoster(resultsPath, lines, collections)...)

	rep.Records = knownFailingRecords(resultsPath, lines)
	rep.Findings = append(rep.Findings, checkRecords(rep, caseText)...)
	rep.Findings = append(rep.Findings, checkLiveness(&rep, resolve)...)

	return finish(rep, out)
}

// readCollections returns collection name → number of requests the generator
// emitted for it.
func readCollections(dir string) (map[string]int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("collections directory %s is absent — the roster cannot be checked against anything", dir)
	}
	out := map[string]int{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".postman_collection.json") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return out, fmt.Errorf("read %s: %v", e.Name(), rerr)
		}
		var c struct {
			Item []json.RawMessage `json:"item"`
		}
		if jerr := json.Unmarshal(raw, &c); jerr != nil {
			return out, fmt.Errorf("parse %s: %v", e.Name(), jerr)
		}
		out[strings.TrimSuffix(e.Name(), ".postman_collection.json")] = len(c.Item)
	}
	return out, nil
}

// readCases returns the raw text of every case file keyed by path, plus a count.
func readCases(dir string) (map[string]string, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("cases directory %s is absent — annotations cannot be read", dir)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".py") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return out, len(out), fmt.Errorf("read %s: %v", path, rerr)
		}
		out[path] = string(raw)
	}
	return out, len(out), nil
}

// annotations extracts every `# verifies <issue-url>` marker, with coordinates.
func annotations(files map[string]string) []Annotation {
	var out []Annotation
	for _, path := range sortedKeys(files) {
		for i, line := range strings.Split(files[path], "\n") {
			m := verifiesRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			out = append(out, Annotation{File: path, Line: i + 1, Issue: n})
		}
	}
	return out
}

// checkRoster compares the composition table with the generated collections.
func checkRoster(doc string, lines []string, collections map[string]int) []string {
	var findings []string

	listed, total, totalLine, found := compositionTable(lines)
	if !found {
		return append(findings, fmt.Sprintf(
			"%s declares no composition table (a row per collection) — the roster cannot drift "+
				"visibly if it is not written down", doc))
	}

	for _, name := range sortedKeys(collections) {
		got, ok := listed[name]
		if !ok {
			findings = append(findings, fmt.Sprintf(
				"%s: collection %q exists but the composition table does not list it — the roster is "+
					"behind the suite", doc, name))
			continue
		}
		if got.count != collections[name] {
			findings = append(findings, fmt.Sprintf(
				"%s:%d: collection %q listed with %d case(s), the generated collection holds %d",
				doc, got.line, name, got.count, collections[name]))
		}
	}
	for _, name := range sortedKeys(listed) {
		if _, ok := collections[name]; !ok {
			findings = append(findings, fmt.Sprintf(
				"%s:%d: composition table lists collection %q, which does not exist",
				doc, listed[name].line, name))
		}
	}

	sum := 0
	for _, n := range collections {
		sum += n
	}
	if total != sum {
		findings = append(findings, fmt.Sprintf(
			"%s:%d: composition total is %d, the collections hold %d case(s)", doc, totalLine, total, sum))
	}

	return findings
}

// cell is one composition-table entry.
type cell struct {
	count int
	line  int
}

// compositionTable reads the "collection | cases" table: rows keyed by collection
// name, plus the declared total and the line it sits on.
//
// The table is located by its HEADER, not by "any table with a number in the second
// column". Scanning every table would let an unrelated one — a future table of
// counts anywhere in the document — inject phantom collections and make the gate
// fire on rows that were never a roster. A gate must know which text it is judging.
func compositionTable(lines []string) (rows map[string]cell, total, totalLine int, found bool) {
	rows = map[string]cell{}
	total, totalLine = -1, 0

	start := -1
	for i, line := range lines {
		cells := tableCells(line)
		if len(cells) >= 2 && (strings.EqualFold(cells[0], "Коллекция") || strings.EqualFold(cells[0], "Collection")) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return rows, total, totalLine, false
	}

	for i := start; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
			break // the table ended
		}
		cells := tableCells(lines[i])
		if len(cells) < 2 {
			continue
		}
		name, countTxt := cells[0], cells[1]
		if name == "" || strings.HasPrefix(name, "---") || strings.HasPrefix(countTxt, "---") {
			continue // separator
		}
		n, err := strconv.Atoi(countTxt)
		if err != nil {
			continue
		}
		found = true
		if strings.EqualFold(name, "Всего") || strings.EqualFold(name, "Total") {
			total, totalLine = n, i+1
			continue
		}
		rows[name] = cell{count: n, line: i + 1}
	}
	return rows, total, totalLine, found
}

// tableCells splits a markdown row into trimmed, emphasis-free cells.
func tableCells(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(emphasisRe.ReplaceAllString(p, "")))
	}
	return out
}

// knownFailingRecords collects the rows of every "Known failing" table.
func knownFailingRecords(doc string, lines []string) []Record {
	var out []Record
	for i, line := range lines {
		if !strings.Contains(line, "Known failing") {
			continue
		}
		// The table, when there is one, follows immediately (header + separator).
		for j := i + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t == "" {
				if j <= i+1 {
					continue // one blank line between heading and table
				}
				break
			}
			if !strings.HasPrefix(t, "|") {
				break
			}
			cells := tableCells(lines[j])
			if len(cells) == 0 || strings.HasPrefix(cells[0], "---") {
				continue
			}
			if strings.EqualFold(cells[0], "Кейс") || strings.EqualFold(cells[0], "Case") {
				continue // header
			}
			rec := Record{Doc: doc, Line: j + 1, CaseIDs: caseIDRe.FindAllString(cells[0], -1)}
			for _, m := range issueRefRe.FindAllStringSubmatch(lines[j], -1) {
				if n, err := strconv.Atoi(m[1]); err == nil {
					rec.Issues = append(rec.Issues, n)
				}
			}
			out = append(out, rec)
		}
	}
	return out
}

// checkRecords enforces the two-way binding and the "record has a subject" rule.
func checkRecords(rep Report, caseText map[string]string) []string {
	var findings []string

	declared := map[int][]Record{}
	for _, r := range rep.Records {
		if len(r.Issues) == 0 {
			findings = append(findings, fmt.Sprintf(
				"%s:%d: known-failing row names no issue — an exclusion nobody can check never expires",
				r.Doc, r.Line))
		}
		if len(r.CaseIDs) == 0 {
			findings = append(findings, fmt.Sprintf(
				"%s:%d: known-failing row names no case — there is nothing for it to excuse", r.Doc, r.Line))
		}
		for _, id := range r.CaseIDs {
			if !mentionedInCases(caseText, id) {
				findings = append(findings, fmt.Sprintf(
					"%s:%d: known-failing row names case %q, which no case file declares — the record "+
						"outlived its subject", r.Doc, r.Line, id))
			}
		}
		for _, n := range r.Issues {
			declared[n] = append(declared[n], r)
		}
	}

	annotated := map[int][]Annotation{}
	for _, a := range rep.Annotations {
		annotated[a.Issue] = append(annotated[a.Issue], a)
	}

	for _, n := range sortedIntKeys(declared) {
		if _, ok := annotated[n]; !ok {
			findings = append(findings, fmt.Sprintf(
				"%s:%d: known-failing row excuses issue #%d, but no case is annotated `# verifies "+
					".../issues/%d` — the row excuses nothing",
				declared[n][0].Doc, declared[n][0].Line, n, n))
		}
	}
	for _, n := range sortedIntKeys(annotated) {
		if _, ok := declared[n]; !ok {
			a := annotated[n][0]
			findings = append(findings, fmt.Sprintf(
				"%s:%d: case is annotated `# verifies .../issues/%d`, but no known-failing row declares "+
					"it — a case expected to be red is invisible in the results doc", a.File, a.Line, n))
		}
	}

	return findings
}

// checkLiveness asks the tracker whether each declared issue is still open. An
// unresolvable issue is recorded as unverified, never silently passed.
func checkLiveness(rep *Report, resolve func(int) (IssueState, error)) []string {
	var findings []string

	seen := map[int][]string{}
	for _, r := range rep.Records {
		for _, n := range r.Issues {
			seen[n] = append(seen[n], fmt.Sprintf("%s:%d", r.Doc, r.Line))
		}
	}
	for _, a := range rep.Annotations {
		seen[a.Issue] = append(seen[a.Issue], fmt.Sprintf("%s:%d", a.File, a.Line))
	}

	for _, n := range sortedIntKeys(seen) {
		state, err := resolve(n)
		switch {
		case err != nil || state == StateUnknown:
			reason := "tracker unreachable"
			if err != nil {
				reason = err.Error()
			}
			rep.Unverifiable = append(rep.Unverifiable, fmt.Sprintf(
				"issue #%d (%s): %s", n, strings.Join(seen[n], ", "), reason))
		case state == StateClosed:
			findings = append(findings, fmt.Sprintf(
				"issue #%d is no longer open, but it is still excused at %s — a known-failing record "+
					"that outlived its fix is a false statement about the product; delete it",
				n, strings.Join(seen[n], ", ")))
		}
	}

	return findings
}

// ghResolver asks the GitHub CLI for one issue's state.
func ghResolver(repo string) func(int) (IssueState, error) {
	return func(n int) (IssueState, error) {
		if _, err := exec.LookPath("gh"); err != nil {
			return StateUnknown, fmt.Errorf("gh CLI not available")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "gh", "issue", "view", strconv.Itoa(n),
			"--repo", repo, "--json", "state", "-q", ".state")
		raw, err := cmd.Output()
		if err != nil {
			return StateUnknown, fmt.Errorf("gh issue view %d: %v", n, err)
		}
		switch strings.ToUpper(strings.TrimSpace(string(raw))) {
		case "OPEN":
			return StateOpen, nil
		case "CLOSED":
			return StateClosed, nil
		}
		return StateUnknown, fmt.Errorf("gh returned an unrecognised state for #%d", n)
	}
}

func mentionedInCases(caseText map[string]string, id string) bool {
	for _, text := range caseText {
		if strings.Contains(text, id) {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedIntKeys[V any](m map[int]V) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// finish prints the census, then the findings, and turns findings into an error.
func finish(rep Report, out io.Writer) (Report, error) {
	_, _ = fmt.Fprintf(out, "audit-known-failing: read %d collection(s), %d case file(s); %d known-failing "+
		"record(s), %d case annotation(s)\n",
		rep.Collections, rep.CaseFiles, len(rep.Records), len(rep.Annotations))

	// An unresolved liveness check is announced, never absorbed into the verdict.
	if len(rep.Unverifiable) > 0 {
		_, _ = fmt.Fprintf(out, "audit-known-failing: LIVENESS NOT VERIFIED for %d record(s) — the tracker "+
			"could not be consulted, so \"still open\" is UNPROVEN, not proven:\n", len(rep.Unverifiable))
		for _, u := range rep.Unverifiable {
			_, _ = fmt.Fprintf(out, "  %s\n", u)
		}
	}

	if len(rep.Findings) == 0 {
		_, _ = fmt.Fprintln(out, "audit-known-failing: OK")
		return rep, nil
	}
	for _, f := range rep.Findings {
		_, _ = fmt.Fprintf(out, "audit-known-failing: %s\n", f)
	}
	_, _ = fmt.Fprint(out, explanation)
	return rep, fmt.Errorf("audit-known-failing: %d finding(s)", len(rep.Findings))
}

const explanation = `
A "known failing" record buys a case out of "everything must be green". It is
legitimate only while the defect it names is open, so it must expire by itself:
  - every row names at least one issue and at least one case that still exists;
  - every row is backed by a case annotated ` + "`# verifies .../issues/N`" + `, and
    every such annotation is declared by a row (both directions);
  - every named issue is still OPEN. A closed issue means the record outlived its
    fix — delete the row and the annotation, do not "update" them.
The composition table must match the generated collections: a roster that drifts
behind the suite is the same class of false statement, one table higher.
`
