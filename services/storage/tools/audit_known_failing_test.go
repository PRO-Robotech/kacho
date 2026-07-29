// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package tools_regression

// Fixtures for the known-failing gate. The gate exists because two rows in this
// service's RESULTS.md outlived their fixes — one of them fixed in the very tree
// that still carried the row — so the suite kept declaring the product broken in
// two places where it was not.
//
// Every case below is an injection, and they come in pairs on purpose: for each
// shape the gate must catch there is the legitimate shape that resembles it and
// must pass. A gate proven in one direction only catches form, and the first false
// positive gets it switched off.
//
// The tracker is injected (Options.IssueState), so these run hermetically — no
// network, no `gh`, no dependence on which issues happen to be open today. The
// real tracker path is exercised separately against the real tree.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/tools/auditknownfailing"
)

// collectionJSON renders a collection artifact with n requests, matching the
// generator's shape closely enough for the roster check (which counts items).
func collectionJSON(name string, n int) string {
	items := make([]string, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, fmt.Sprintf(`{"name":"%s-%d"}`, name, i))
	}
	return fmt.Sprintf(`{"info":{"name":"kacho-storage / newman / %s"},"item":[%s]}`,
		name, strings.Join(items, ","))
}

// knownFailingTree builds a minimal suite: one collection of two cases, a results
// doc, and a case file. resultsBody is spliced in after the composition table.
func knownFailingTree(resultsBody, caseBody string) map[string]string {
	return map[string]string{
		"collections/volume.postman_collection.json": collectionJSON("volume", 2),
		"cases/volume.py": caseBody,
		"docs/RESULTS.md": `# RESULTS

## Состав

| Коллекция | Кейсов | Стадия |
|---|---:|---|
| volume | 2 | S1 |
| **Всего** | **2** | |

` + resultsBody,
	}
}

const (
	// caseClean — a case file with no exclusion annotation at all.
	caseClean = `CASES.append(Case(
    id="VOL-CR-CRUD-OK",
    title="create",
))
`
	// caseAnnotated — a case that declares itself expected-red for issue #42.
	caseAnnotated = `CASES.append(Case(
    id="VOL-CR-CRUD-OK",
    # verifies https://github.com/PRO-Robotech/kacho/issues/42 (RED until the fix)
    title="create",
))
`
	// rowFor42 — the results row that excuses that case.
	rowFor42 = `### Known failing — product bugs

| Кейс | Коллекция | Issue | Класс |
|---|---|---|---|
| VOL-CR-CRUD-OK | volume | [#42](https://github.com/PRO-Robotech/kacho/issues/42) | injected defect. |
`
	// rowNoIssue — a row that excuses a case without naming any defect.
	rowNoIssue = `### Known failing — product bugs

| Кейс | Коллекция | Issue | Класс |
|---|---|---|---|
| VOL-CR-CRUD-OK | volume | see chat | injected defect. |
`
	// rowGhostCase — a row naming a case that no longer exists.
	rowGhostCase = `### Known failing — product bugs

| Кейс | Коллекция | Issue | Класс |
|---|---|---|---|
| VOL-RETIRED-CASE-ID | volume | [#42](https://github.com/PRO-Robotech/kacho/issues/42) | injected defect. |
`
	// rowNone — the shape of a suite with nothing excused.
	rowNone = "### Known failing — product bugs: нет\n"
)

// stateFn answers with one fixed state for every issue.
func stateFn(s auditknownfailing.IssueState) func(int) (auditknownfailing.IssueState, error) {
	return func(int) (auditknownfailing.IssueState, error) { return s, nil }
}

// runKnownFailing materialises the fixture suite and audits it.
func runKnownFailing(t *testing.T, files map[string]string,
	issue func(int) (auditknownfailing.IssueState, error)) (string, error) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFile(t, filepath.Join(root, rel), content)
	}
	var buf strings.Builder
	_, err := auditknownfailing.Audit(auditknownfailing.Options{Root: root, IssueState: issue}, &buf)
	return buf.String(), err
}

func TestAuditKnownFailing(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		issue   auditknownfailing.IssueState
		wantErr bool
		wantOut string // substring the report must contain
	}{
		{
			// THE defect this gate exists for: the product was fixed, the record
			// stayed, and the suite went on declaring a defect that no longer exists.
			name:    "expired: the excused defect has been fixed",
			files:   knownFailingTree(rowFor42, caseAnnotated),
			issue:   auditknownfailing.StateClosed,
			wantErr: true,
			wantOut: "no longer open",
		},
		{
			// …and the converse, which is what keeps the gate usable: exactly the
			// same record, while the defect is genuinely open, must pass in silence.
			name:    "legitimate: the excused defect is still open",
			files:   knownFailingTree(rowFor42, caseAnnotated),
			issue:   auditknownfailing.StateOpen,
			wantErr: false,
		},
		{
			// A row with no case behind it excuses nothing — it is a claim about the
			// product with no test attached.
			name:    "unbacked: row excuses an issue no case declares",
			files:   knownFailingTree(rowFor42, caseClean),
			issue:   auditknownfailing.StateOpen,
			wantErr: true,
			wantOut: "the row excuses nothing",
		},
		{
			// The other direction: a case expected to be red that the results doc
			// never mentions is a red nobody is watching.
			name:    "invisible: annotated case no row declares",
			files:   knownFailingTree(rowNone, caseAnnotated),
			issue:   auditknownfailing.StateOpen,
			wantErr: true,
			wantOut: "invisible in the results doc",
		},
		{
			// An exclusion that names no defect can never expire — nothing about it
			// can ever be checked.
			name:    "uncheckable: row names no issue",
			files:   knownFailingTree(rowNoIssue, caseClean),
			issue:   auditknownfailing.StateOpen,
			wantErr: true,
			wantOut: "names no issue",
		},
		{
			// A record whose case was deleted has already outlived its subject.
			name:    "ghost: row names a case that no longer exists",
			files:   knownFailingTree(rowGhostCase, caseAnnotated),
			issue:   auditknownfailing.StateOpen,
			wantErr: true,
			wantOut: "which no case file declares",
		},
		{
			// A suite with nothing excused is the normal, quiet state.
			name:    "clean: nothing excused, nothing annotated",
			files:   knownFailingTree(rowNone, caseClean),
			issue:   auditknownfailing.StateOpen,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runKnownFailing(t, tc.files, stateFn(tc.issue))
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("gate verdict: gotErr=%v wantErr=%v\n--- output ---\n%s", gotErr, tc.wantErr, out)
			}
			if tc.wantOut != "" && !strings.Contains(out, tc.wantOut) {
				t.Errorf("report must name the finding (missing %q)\n--- output ---\n%s", tc.wantOut, out)
			}
		})
	}
}

// TestAuditKnownFailing_RosterMustMatchTheSuite — the composition table is checked
// against the generated collections, in both directions and on the counts.
//
// This half is here because the same document drifted the same way one table
// higher: it advertised seven collections and 122 cases while the suite held nine
// and 162. A roster that lags the suite is the same false statement as an expired
// exclusion — anyone sizing the coverage reads a number that was true once.
func TestAuditKnownFailing_RosterMustMatchTheSuite(t *testing.T) {
	base := knownFailingTree(rowNone, caseClean)

	t.Run("count drifted", func(t *testing.T) {
		files := clone(base)
		files["collections/volume.postman_collection.json"] = collectionJSON("volume", 5)
		out, err := runKnownFailing(t, files, stateFn(auditknownfailing.StateOpen))
		if err == nil {
			t.Fatalf("a stale count must be a finding\n--- output ---\n%s", out)
		}
		if !strings.Contains(out, "the generated collection holds 5") {
			t.Errorf("the finding must state both numbers\n--- output ---\n%s", out)
		}
	})

	t.Run("collection missing from the table", func(t *testing.T) {
		files := clone(base)
		files["collections/sec-d.postman_collection.json"] = collectionJSON("sec-d", 4)
		out, err := runKnownFailing(t, files, stateFn(auditknownfailing.StateOpen))
		if err == nil {
			t.Fatalf("a collection absent from the roster must be a finding\n--- output ---\n%s", out)
		}
		if !strings.Contains(out, `"sec-d"`) {
			t.Errorf("the finding must name the unlisted collection\n--- output ---\n%s", out)
		}
	})

	t.Run("table lists a collection that does not exist", func(t *testing.T) {
		files := clone(base)
		files["docs/RESULTS.md"] = strings.Replace(files["docs/RESULTS.md"],
			"| volume | 2 | S1 |", "| volume | 2 | S1 |\n| retired | 3 | gone |", 1)
		out, err := runKnownFailing(t, files, stateFn(auditknownfailing.StateOpen))
		if err == nil {
			t.Fatalf("a roster row with no collection must be a finding\n--- output ---\n%s", out)
		}
	})

	t.Run("matching roster is silent", func(t *testing.T) {
		if out, err := runKnownFailing(t, base, stateFn(auditknownfailing.StateOpen)); err != nil {
			t.Fatalf("a roster that matches the suite must pass: %v\n--- output ---\n%s", err, out)
		}
	})

	// The gate must know WHICH table is the roster. An unrelated table of numbers
	// elsewhere in the document is not a roster row, and reading it as one would
	// invent collections nobody declared — a false finding, which is how gates get
	// switched off.
	t.Run("an unrelated table of numbers is not the roster", func(t *testing.T) {
		files := clone(base)
		files["docs/RESULTS.md"] += "\n## Бюджеты\n\n| Что | Секунд |\n|---|---:|\n| op-poll | 15 |\n| фильтр | 3 |\n"
		if out, err := runKnownFailing(t, files, stateFn(auditknownfailing.StateOpen)); err != nil {
			t.Fatalf("a table that is not the roster must not be read as one: %v\n--- output ---\n%s", err, out)
		}
	})

	// …and the premise itself: no roster table at all must be reported, not
	// silently treated as "nothing to compare".
	t.Run("missing roster table is a finding", func(t *testing.T) {
		files := clone(base)
		files["docs/RESULTS.md"] = "# RESULTS\n\n" + rowNone
		out, err := runKnownFailing(t, files, stateFn(auditknownfailing.StateOpen))
		if err == nil {
			t.Fatalf("a results doc with no composition table must be a finding\n--- output ---\n%s", out)
		}
		if !strings.Contains(out, "declares no composition table") {
			t.Errorf("the finding must say the roster is missing\n--- output ---\n%s", out)
		}
	})
}

// TestAuditKnownFailing_NothingReadIsNotOK — the gate's own premise. A suite root
// with no collections and no cases must not produce a clean bill of health.
func TestAuditKnownFailing_NothingReadIsNotOK(t *testing.T) {
	files := map[string]string{"docs/RESULTS.md": "# RESULTS\n"}
	out, err := runKnownFailing(t, files, stateFn(auditknownfailing.StateOpen))
	if err == nil {
		t.Fatalf("an empty suite root must not pass\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "read 0 collection(s), 0 case file(s)") {
		t.Errorf("the census must state that nothing was read\n--- output ---\n%s", out)
	}
}

// TestAuditKnownFailing_UnreachableTrackerIsAnnouncedNotAbsorbed — the one
// dimension that may degrade must degrade LOUDLY.
//
// Liveness needs the tracker. When it cannot be consulted the gate does not fail —
// a developer offline must still be able to run it — but it must not report a
// clean liveness check either. "Did not run" and "passed" are different answers,
// and a soft pass that cannot tell them apart is how a control becomes permanently
// dead while still appearing in every log.
func TestAuditKnownFailing_UnreachableTrackerIsAnnouncedNotAbsorbed(t *testing.T) {
	unreachable := func(int) (auditknownfailing.IssueState, error) {
		return auditknownfailing.StateUnknown, fmt.Errorf("gh CLI not available")
	}
	out, err := runKnownFailing(t, knownFailingTree(rowFor42, caseAnnotated), unreachable)
	if err != nil {
		t.Fatalf("an unreachable tracker must not fail the gate: %v\n--- output ---\n%s", err, out)
	}
	for _, want := range []string{"LIVENESS NOT VERIFIED", "issue #42", "gh CLI not available"} {
		if !strings.Contains(out, want) {
			t.Errorf("the unresolved check must be announced by coordinate (missing %q)\n--- output ---\n%s", want, out)
		}
	}
}

// TestAuditKnownFailing_RealTreeIsClean runs the gate against THIS service's real
// suite, through the very command CI issues.
//
// It reaches the tracker only if a record is declared; with none declared this is
// local and hermetic. The moment someone adds a known-failing row, this test
// starts holding them to the whole contract.
func TestAuditKnownFailing_RealTreeIsClean(t *testing.T) {
	out, err := runScript(t, "audit-known-failing.sh")
	if err != nil {
		t.Fatalf("audit-known-failing must pass against the real suite: %v\n--- output ---\n%s", err, out)
	}
	if strings.Contains(out, "read 0 collection(s)") || strings.Contains(out, "0 case file(s)") {
		t.Fatalf("the gate passed having read nothing — that is not a pass\n--- output ---\n%s", out)
	}
	t.Log(strings.TrimSpace(out))
}

// clone copies a fixture map so a subtest can perturb one entry.
func clone(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
