// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package legacyfolder is the repository-wide gate for one retired word: a
// Kachō tenant container is a Project, and nothing in this tree calls it a
// folder any more.
//
// # Why a gate at all
//
// The hierarchy became account → project long before this gate existed, and two
// migrations renamed the column on both services that carried it
// (compute 0009, iam 0070). The name survived anyway — in a request field, in
// parameter names, in test fixtures, in query keys, and in architecture
// documents that went on describing a column the database no longer has. Prose
// cannot hold a rename in place; only something that runs can. This is that
// thing. Its verdict is the number of violations, and it also states how many
// files it inspected, so "no violations" can never be confused with "nothing
// was looked at".
//
// # What counts as a violation
//
// The identifier, not the English word. A match is the word "folder"
// IMMEDIATELY followed by an id component — folder_id, folderId, folderID,
// FOLDER_ID, folder-id, x-kacho-folder-id, _suiteFolderId, disks_folder_idx,
// and the prose form "folder ID". Case is irrelevant and so is the separator.
//
// The bare word "folder" is deliberately NOT a violation. It has an honest
// meaning here that has nothing to do with the retired container: a Postman
// collection groups its requests into folders, and `newman run --folder` is
// that tool's flag, not our vocabulary. A rule that fires on those teaches
// contributors to write around the matcher instead of avoiding the concept —
// the same reason the sibling gate in tools/foreignclouds refuses to match
// ordinary prose. Places where "folder" names our own retired container
// without an id beside it are a finding for a reader, not for this gate.
//
// # Exemptions
//
// Enumerated, each with its reason, and audited: an exemption that stops
// matching is itself reported, so the list cannot rot into a standing
// allowance. Two kinds only:
//
//   - an applied migration. Migrations are never edited (ban #5), and the two
//     that performed the rename must keep saying what they renamed — that is
//     the record of the change, not a survival of it.
//   - a file that carries the name BECAUSE the rename was enforced: this gate
//     and its fixtures, and the conformance test that sends the retired header
//     to prove the server ignores it. Flagging those pushes toward undoing the
//     fix.
//
// A file that merely still says folder_id is NOT exempt. It is a finding, and
// it stays one until somebody removes the name.
package legacyfolder

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// idWords are the components that turn a preceding "folder" into the retired
// identifier. Kept to an exact set on purpose: "identity" and "identifier" also
// begin with "id", and a prefix rule would fire on "folder identifier" — honest
// prose that names no field.
var idWords = map[string]bool{
	"id":  true,
	"ids": true,
	"idx": true, // legacy index names: disks_folder_idx, idx_conditions_folder_status
}

// separators may sit between "folder" and the id component. A single one, or
// none at all (camelCase). Anything longer is two words in a sentence, not one
// identifier.
const separators = "_-. "

// match is one occurrence, as byte offsets into the line that was searched.
type match struct {
	start, end int
}

// word is one component of an identifier or one word of prose, with its byte
// span in the original string.
type word struct {
	text       string // lower-cased
	start, end int
}

// splitWords breaks s into alphanumeric runs and then splits each run at
// camelCase humps, so folderId, FOLDER_ID, folder-id and "folder ID" all reduce
// to the same two words. Offsets refer to s.
func splitWords(s string) []word {
	var out []word
	runes := []rune(s)
	offsets := make([]int, len(runes)+1)
	off := 0
	for i, r := range runes {
		offsets[i] = off
		off += utf8.RuneLen(r)
	}
	offsets[len(runes)] = off

	isAlnum := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}

	i := 0
	for i < len(runes) {
		if !isAlnum(runes[i]) {
			i++
			continue
		}
		j := i
		for j < len(runes) && isAlnum(runes[j]) {
			j++
		}
		// One alphanumeric run: split it at lower→upper transitions and at the
		// tail of an acronym followed by a word (IDValue → ID, Value).
		k := i
		for k < j {
			m := k + 1
			for m < j {
				prev, cur := runes[m-1], runes[m]
				if unicode.IsLower(prev) && unicode.IsUpper(cur) {
					break
				}
				if unicode.IsUpper(prev) && unicode.IsUpper(cur) && m+1 < j && unicode.IsLower(runes[m+1]) {
					break
				}
				m++
			}
			out = append(out, word{
				text:  strings.ToLower(string(runes[k:m])),
				start: offsets[k],
				end:   offsets[m],
			})
			k = m
		}
		i = j
	}
	return out
}

// findLegacy returns every occurrence of the retired identifier in s.
func findLegacy(s string) []match {
	if s == "" {
		return nil
	}
	words := splitWords(s)
	var out []match
	for i := 0; i+1 < len(words); i++ {
		if words[i].text != "folder" || !idWords[words[i+1].text] {
			continue
		}
		gap := s[words[i].end:words[i+1].start]
		if len(gap) > 1 || (len(gap) == 1 && !strings.ContainsAny(gap, separators)) {
			continue
		}
		out = append(out, match{start: words[i].start, end: words[i+1].end})
	}
	return out
}

func hasMatch(s string) bool { return len(findLegacy(s)) > 0 }

// skipDirs are not this repository's authored content.
var skipDirs = map[string]bool{
	".git":         true, // version-control internals
	"node_modules": true, // installed third-party packages
	"vendor":       true, // vendored third-party sources
}

// skipFiles are dependency manifests: coordinates of third-party artifacts we
// consume and did not name.
var skipFiles = map[string]bool{
	"go.mod":            true,
	"go.sum":            true,
	"package-lock.json": true,
	"pnpm-lock.yaml":    true,
	"yarn.lock":         true,
}

// exemptFiles carry the retired name because the rename was enforced, or
// because they declare the rule. Keyed by slash-separated path from the
// repository root.
var exemptFiles = map[string]string{
	"tools/legacyfolder/legacyfolder.go":      "this gate: it has to spell the name it forbids",
	"tools/legacyfolder/legacyfolder_test.go": "the fixtures that prove this gate fires and does not over-fire",

	"internal/repohygiene/clienttruth_vpc_tenancylevel.go": "a second gate declaring the same rule: it forbids a contract comment from naming a container the tree has no field for, and cites folder_id as the canonical retired one — it has to spell the name it forbids, exactly like this gate does above",

	"services/compute/internal/migrations/0001_initial.sql":                       "applied migration: the baseline that created the column under its original name; applied migrations are never edited (ban #5)",
	"services/compute/internal/migrations/0009_rename_folder_to_project.sql":      "applied migration: the record of the rename itself — it must keep naming what it renamed",
	"services/compute/internal/handler/tenant_interceptor_project_header_test.go": "conformance test: it sends the retired header to prove the server does not honour it; removing the name would remove the proof",

	// Newman cases that ASSERT the retired word is absent from a response, and the
	// collections generated from them. The literal IS the assertion: remove it and
	// the proof that the rename reached the wire goes with it.
	"services/iam/tests/newman/cases/iam-account-redesign.py":                            "conformance case: asserts no pre-redesign container key is served",
	"services/iam/tests/newman/collections/iam-account-redesign.postman_collection.json": "generated from the conformance case above; regenerating it reproduces the literal",
}

// awaitingPass is the price of splitting a sweep across two concurrent passes:
// a file listed here sits in a tree another pass owns right now, so this one may
// not touch it. Every entry is an exact path, must exist, and must still match —
// a stale entry is reported and has to be deleted, so the second pass cannot
// leave the list behind. Nothing outside the two owned trees may be listed here
// (see AuditExemptions), which keeps this from becoming a general escape hatch.
// The count is printed on every run, including a clean one.
//
// EMPTY IS THE RESTING STATE, and getting back to it is the point. The list once
// deferred a whole sweep of the iam newman tree and four gateway fixtures, all of
// which named the retired container through one shared environment key. That key
// is now spelled with the surviving word, at the generator and in every
// collection it emits, so the deferrals had nothing left to defer — and a
// deferral with no subject is a finding, not a state to keep. The four files that
// still spell the name do so because they ASSERT the server no longer serves it;
// that is not a deferral, and they moved to exemptFiles where the reason is
// written down.
var awaitingPass = map[string]string{}

// awaitingRoots are the only trees an awaitingPass entry may name.
var awaitingRoots = []string{"services/iam/", "gateway/"}

// Finding is one occurrence, or one exemption that no longer earns its place.
type Finding struct {
	Path string // slash-separated, relative to the repository root
	Line int    // 0 for a path-name match or a stale exemption
	Text string
}

func (f Finding) String() string {
	if f.Line == 0 {
		return fmt.Sprintf("%s: %s", f.Path, f.Text)
	}
	return fmt.Sprintf("%s:%d: %s", f.Path, f.Line, f.Text)
}

// Stats is what the scan actually looked at. A gate that reports zero
// violations without saying how much it read is indistinguishable from a gate
// that read nothing, so this travels with the verdict and is printed with it.
type Stats struct {
	Inspected int // files whose path and content were searched
	Exempt    int // files skipped by exemptFiles
	Awaiting  int // files skipped by awaitingPass
}

// Scan walks the tree rooted at repoRoot and returns one finding per violation,
// sorted, together with what it inspected.
func Scan(repoRoot string) ([]Finding, Stats, error) {
	var findings []Finding
	var stats Stats
	ignored := ignoredPaths(repoRoot)

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			if rel, relErr := filepath.Rel(repoRoot, path); relErr == nil && ignored.covers(filepath.ToSlash(rel)+"/") {
				return fs.SkipDir
			}
			return nil
		}
		if skipFiles[d.Name()] {
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if _, exempt := exemptFiles[rel]; exempt {
			stats.Exempt++
			return nil
		}
		if _, pending := awaitingPass[rel]; pending {
			stats.Awaiting++
			return nil
		}
		if ignored.covers(rel) {
			return nil
		}

		stats.Inspected++
		findings = append(findings, scanPath(rel)...)
		findings = append(findings, scanContent(rel, readText(path))...)
		return nil
	})
	if err != nil {
		return nil, Stats{}, err
	}
	sortFindings(findings)
	return findings, stats, nil
}

// ignoreSet is what version control refuses to carry, as slash-separated paths
// from the repository root. Entries ending in "/" cover everything beneath.
type ignoreSet struct {
	exact  map[string]bool
	prefix []string
}

func (s ignoreSet) covers(rel string) bool {
	if s.exact[rel] {
		return true
	}
	for _, p := range s.prefix {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// ignoredPaths asks version control what it ignores under repoRoot.
//
// The subject is this repository's CONTENT, not whatever sits in a working
// tree: build output exists only on a machine that has run a build, and a gate
// whose verdict depends on unversioned local state is not a gate. Only IGNORED
// paths are dropped — a file that is merely untracked stays in scope, because a
// newly authored file nobody has added yet is exactly what this must catch.
func ignoredPaths(repoRoot string) ignoreSet {
	set := ignoreSet{exact: map[string]bool{}}
	cmd := gitenv.Command(repoRoot, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory")
	out, err := cmd.Output()
	if err != nil {
		return set
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, "/") {
			set.prefix = append(set.prefix, line)
			continue
		}
		set.exact[line] = true
	}
	return set
}

// IsRepositoryRoot reports whether root is the tree the exemption lists were
// written for. Scanning any other directory still answers "does this name the
// retired container", but auditing the lists against it does not.
func IsRepositoryRoot(root string) bool {
	_, err := os.Stat(filepath.Join(root, "tools", "legacyfolder", "legacyfolder.go"))
	return err == nil
}

// AuditExemptions reports every list entry that no longer earns its place: a
// file that is gone or no longer carries the name, and any awaitingPass entry
// outside the trees a concurrent pass owns. Without this the lists only ever
// grow, and a list that only grows is a blanket allowance with extra steps.
func AuditExemptions(repoRoot string) []Finding {
	var findings []Finding

	audit := func(list map[string]string, label, remedy string) {
		for rel, why := range list {
			full := filepath.Join(repoRoot, filepath.FromSlash(rel))
			if _, err := os.Stat(full); err != nil {
				findings = append(findings, Finding{
					Path: rel,
					Text: label + " points at a file that is gone (" + why + ") — " + remedy,
				})
				continue
			}
			if !hasMatch(readText(full)) && !hasMatch(rel) {
				findings = append(findings, Finding{
					Path: rel,
					Text: "stale " + label + " (" + why + "): nothing in this file matches any more — " + remedy,
				})
			}
		}
	}

	audit(exemptFiles, "exemption", "delete the entry from exemptFiles")
	audit(awaitingPass, "deferral", "delete the entry from awaitingPass")

	for rel := range awaitingPass {
		if !underAwaitingRoot(rel) {
			findings = append(findings, Finding{
				Path: rel,
				Text: fmt.Sprintf("deferral outside the trees a concurrent pass owns (%s) — awaitingPass is not a general exemption list; fix the file instead",
					strings.Join(awaitingRoots, ", ")),
			})
		}
	}

	sortFindings(findings)
	return findings
}

func underAwaitingRoot(rel string) bool {
	for _, root := range awaitingRoots {
		if strings.HasPrefix(rel, root) {
			return true
		}
	}
	return false
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Line < findings[j].Line
	})
}

// readText returns the file's content, or "" when it is not text we can read.
func readText(path string) string {
	b, err := os.ReadFile(path) // #nosec G304 -- path comes from this tool's own WalkDir of the repo root or from its compile-time exemption lists, not from any input
	if err != nil || !utf8.Valid(b) || strings.IndexByte(string(b), 0) >= 0 {
		return ""
	}
	return string(b)
}

// scanPath reports the retired name in the file's own name or in a directory
// above it. The rename covers names, and a path is a name.
func scanPath(rel string) []Finding {
	var out []Finding
	for range findLegacy(rel) {
		out = append(out, Finding{
			Path: rel,
			Text: "path names the retired container — a tenant container is a Project; rename it",
		})
	}
	return out
}

func scanContent(rel, content string) []Finding {
	if content == "" {
		return nil
	}
	var out []Finding
	for n, line := range strings.Split(content, "\n") {
		for _, m := range findLegacy(line) {
			out = append(out, Finding{
				Path: rel,
				Line: n + 1,
				Text: fmt.Sprintf("names the retired container (%q): %s", line[m.start:m.end], strings.TrimSpace(line)),
			})
		}
	}
	return out
}

// Report renders findings for a human, with the reason the rule exists and what
// the scan actually covered.
func Report(findings []Finding, st Stats) string {
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "verify-no-legacy-folder: %s\n", f)
	}
	fmt.Fprint(&b, `
A tenant container in Kachō is a Project. It is addressed as projectId on the
wire, stored in a project_id column, and named project everywhere else. The
word folder predates the redesign; both services that carried the column
renamed it (compute 0009, iam 0070), and no field, parameter, fixture, query
key or document may bring the name back.

If a match is in an applied migration, or in a file that carries the name
BECAUSE the rename was enforced (a test asserting the retired header is
ignored), add it to the enumerated list in tools/legacyfolder with the reason
written down. Nothing else is exempt.
`)
	fmt.Fprintf(&b, "\nInspected %d file(s); %d exempt; %d awaiting another pass.\n",
		st.Inspected, st.Exempt, st.Awaiting)
	return b.String()
}
