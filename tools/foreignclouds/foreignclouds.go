// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package foreignclouds is the repository-wide gate for the second
// non-negotiable: Kachō is described in its own terms, and no other cloud
// provider is named anywhere in this tree — not in code, not in documentation,
// not in comments, not in environment variables, not in file names.
//
// # Why a gate at all
//
// The rule existed as prose and was enforced by review. Review does not scale
// and does not leave a record: a document could report the tree "clean" without
// anything having been run, because there was nothing to run. This package is
// the thing that runs. It counts violations and its exit status is that count.
//
// # How this differs from the comment guard in compute
//
// services/compute/internal/commentlint is a narrow, different instrument and
// stays as it is. It reads ONE service (services/compute, internal/ and cmd/),
// looks ONLY at Go comments, and its main subject is process noise — tracker
// ids, phase markers, TODO. This gate reads the WHOLE repository, every text
// file and every path, and knows only one subject: the name of another cloud.
// The two overlap on a handful of tokens inside compute's Go comments; neither
// is a re-implementation of the other.
//
// # What counts as a name
//
// Only provider and provider-product names, matched on identifier boundaries so
// that "aws" is found inside aws_v1_http_endpoint and AWS_REGION but not inside
// "laws" or "draws". A rule must not fire on ordinary prose: a matcher that
// flags honest English teaches contributors to write around the matcher instead
// of avoiding the vendor, and such a rule was removed from commentlint for
// exactly that reason. Nothing is added here that a reader would not recognise
// as somebody else's cloud.
//
// Region identifiers of a foreign cloud are deliberately NOT in the token list.
// They are a real instance of the same ban, but they are seed data: they are
// primary keys inside applied migrations, which are never edited (ban #5), so a
// rule covering them could not be satisfied by any change to this tree. That
// debt is tracked as a divergence in services/compute/docs/engineering/architecture, not
// as an alarm nobody can turn off.
//
// # Exemptions
//
// Two kinds only, both enumerated below with a reason attached to each entry,
// and both audited: an exemption that stops matching anything is itself
// reported, so the list cannot quietly rot into a permanent allowance.
//
//   - a coordinate of third-party software we consume — a GitHub Action
//     publisher, an SDK's fixed environment-variable names. We did not choose
//     those names and cannot change them; the ban is about what WE name.
//   - a file that carries the token BECAUSE the ban was enforced — a proto
//     reserving retired field names so they can never be reused, a conformance
//     test asserting the tokens are absent from responses, the rule
//     declarations themselves. Flagging those pushes toward undoing the fix.
//
// A file that merely happens to mention another cloud is NOT exempt. It is a
// finding, and it stays a finding until somebody removes the mention.
package foreignclouds

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// tokens are the provider and provider-product names. Each is matched
// case-insensitively on identifier boundaries (see findTokens below).
//
// The two-letter abbreviation of one provider is deliberately absent. On its
// own it is far weaker evidence than a full name, and this tree still carries
// it in hundreds of places that no change here can reach — adding it would bury
// the findings that are actionable under ones that are not. The narrow comment
// guard in compute already covers that abbreviation where it matters most.
var tokens = []string{
	"yandex",
	"ycloud",
	"aws",
	"amazon",
	"gcp",
	"gce",
	"azure",
	"alibaba",
	"aliyun",
	"digitalocean",
	"hetzner",
	// The metadata-service abbreviation of one provider. Added 2026-08-13 with
	// its radius measured first: two occurrences in the tree, both the same
	// comment (contract + its generated stub), and no substring false positives
	// — the token never appears inside another word. A field explained through
	// someone else's product name is a field whose contract is theirs to change.
	"imds",
}

// match is one occurrence of one token in a line.
type match struct {
	token      string
	start, end int // byte offsets into the line that was searched
}

// findTokens returns every token occurrence in s, compared without regard to
// case and bounded on identifier components: a letter or digit on either side
// prevents a match ("laws", "amazonite", "gceilings"), while '_', '-', '.' and
// whitespace do not — which is how aws_v1_http_endpoint, AWS_ACCESS_KEY_ID and
// "AWS flavored metadata" are all reached by the same token.
//
// Deliberately not a regular expression. Go has no look-around, so a pattern
// that spells the boundaries CONSUMES them, and two tokens sharing one
// separator ("AWS STS ... / GCP Workload Identity") would cost one of the two
// findings — silently, and only on the lines that name more than one cloud.
func findTokens(s string) []match {
	if s == "" {
		return nil
	}
	lower := strings.ToLower(s)
	var out []match
	for _, tok := range tokens {
		for from := 0; from < len(lower); {
			at := strings.Index(lower[from:], tok)
			if at < 0 {
				break
			}
			start := from + at
			end := start + len(tok)
			if !alnumAt(lower, start-1) && !alnumAt(lower, end) {
				out = append(out, match{token: tok, start: start, end: end})
			}
			from = start + 1
		}
	}
	return out
}

// alnumAt reports whether s[i] is a letter or digit; out-of-range counts as a
// boundary, so a token at either end of the string still matches.
func alnumAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// skipDirs are not part of this repository's authored content.
var skipDirs = map[string]bool{
	".git":         true, // version-control internals
	"node_modules": true, // installed third-party packages
	"vendor":       true, // vendored third-party sources
}

// skipFiles are dependency manifests: they list the coordinates of third-party
// artifacts. We consume those artifacts; we did not name them, and the ban is
// about what this product calls things.
var skipFiles = map[string]bool{
	"go.mod":            true,
	"go.sum":            true,
	"package-lock.json": true,
	"pnpm-lock.yaml":    true,
	"yarn.lock":         true,
}

// coordinate is a third-party artifact whose own name carries a token. A line
// is exempt only while the match sits inside one of these.
type coordinate struct {
	text string
	why  string
}

var coordinates = []coordinate{
	{"azure/setup-", "GitHub Action publisher namespace (azure/setup-helm, azure/setup-kubectl)"},
	{"AWS_ACCESS_KEY_ID", "environment variable read by the S3 credential chain of the registry storage driver"},
	{"AWS_SECRET_ACCESS_KEY", "environment variable read by the S3 credential chain of the registry storage driver"},
	{"aws-sdk", "the SDK that defines the S3 credential chain, named as the mechanism"},
}

// exemptFile is a whole file that carries a token because the ban was enforced,
// or because the file declares the rule. Keyed by slash-separated path from the
// repository root.
var exemptFiles = map[string]string{
	"tools/foreignclouds/foreignclouds.go":      "this gate: it has to spell the tokens it forbids",
	"tools/foreignclouds/foreignclouds_test.go": "the fixtures that prove this gate fires and does not over-fire",

	"services/compute/internal/commentlint/commentlint_test.go": "the sibling comment guard: it declares tokens of its own",

	"proto/kacho/cloud/compute/v1/instance.proto":   "reserves the retired gce_*/aws_* field names so they can never be reused; dropping the reservation would re-open the hole",
	"pkg/api/kacho/cloud/compute/v1/instance.pb.go": "generated from the proto above, and echoes its retirement note",
	// Второй ВЫХОД той же генерации — модуль провайдера IaC. Он появился, когда у
	// провайдера завёлся ресурс машины: выход, порождённый «на будущее», в это
	// дерево не кладут. Причина послабления та же, что у первого выхода, и она
	// структурная: снять её можно только сняв резервирование имён в самом proto,
	// а это открыло бы повторное использование имён, которые повторно
	// использовать нельзя никогда.
	"services/compute/internal/migrations/0016_instance_redesign.sql": "an applied migration whose comment records that retirement; applied migrations are never edited",

	"services/compute/tests/newman/cases/instance-redesign.py":                            "conformance case asserting those tokens are ABSENT from responses",
	"services/compute/tests/newman/collections/instance-redesign.postman_collection.json": "generated from the case above",
}

// Finding is one occurrence, or one exemption that no longer matches anything.
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

// Scan walks the tree rooted at repoRoot and returns one finding per violation,
// sorted. An empty result means no other cloud is named anywhere in the tree.
func Scan(repoRoot string) ([]Finding, error) {
	var findings []Finding
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
			return nil
		}
		if ignored.covers(rel) {
			return nil
		}

		findings = append(findings, scanPath(rel)...)
		findings = append(findings, scanContent(rel, readText(path))...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortFindings(findings)
	return findings, nil
}

// ignoreSet is what version control refuses to carry, as slash-separated paths
// from the repository root. Entries ending in "/" cover everything beneath them.
type ignoreSet struct {
	exact  map[string]bool
	prefix []string
}

// covers reports whether rel (a file path, or a directory path ending in "/")
// is ignored.
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
// The gate's subject is this repository's CONTENT, not whatever sits in a
// working tree. Build output is where the two diverge: a UI remote's bundled
// vendor chunk names other clouds because the third-party library it bundles
// does, and that chunk exists only on a machine that has run a build. Scanning
// it made the same tree answer differently in a fresh checkout than locally,
// and the verdict CI reports is the one nobody can reproduce. A gate whose
// result depends on unversioned local state is not a gate.
//
// Only IGNORED paths are dropped. A file that is merely untracked stays in
// scope: a newly authored file nobody has added yet is exactly what this must
// catch before it lands.
//
// Outside a repository (or without git) nothing is ignored and the whole tree
// is scanned — scanning too much is a loud, fixable answer; scanning too little
// is a silent one.
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

// IsRepositoryRoot reports whether root is the tree the exemption list was
// written for. Scanning any other directory is still meaningful — "does this
// name another cloud" — but auditing the list against it is not.
func IsRepositoryRoot(root string) bool {
	_, err := os.Stat(filepath.Join(root, "tools", "foreignclouds", "foreignclouds.go"))
	return err == nil
}

// AuditExemptions reports every exemption that no longer earns its place: a
// file that is gone or no longer carries a token, a coordinate that suppresses
// nothing. Without this the lists only ever grow, and a list that only grows is
// a blanket allowance with extra steps — the same defect this gate exists to
// catch, one level up.
//
// Kept apart from Scan so that scanning an arbitrary directory answers only
// "does this tree name another cloud".
func AuditExemptions(repoRoot string) []Finding {
	var findings []Finding

	for rel, why := range exemptFiles {
		full := filepath.Join(repoRoot, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err != nil {
			findings = append(findings, Finding{
				Path: rel,
				Text: "exemption points at a file that is gone (" + why + ") — delete the entry from exemptFiles",
			})
			continue
		}
		if !hasMatch(readText(full)) && !hasMatch(rel) {
			findings = append(findings, Finding{
				Path: rel,
				Text: "stale exemption (" + why + "): nothing in this file matches any more — delete the entry from exemptFiles",
			})
		}
	}

	for _, c := range unusedCoordinates(repoRoot) {
		findings = append(findings, Finding{
			Path: "tools/foreignclouds",
			Text: fmt.Sprintf("stale coordinate exemption %q (%s): it suppresses nothing in this tree — delete it", c.text, c.why),
		})
	}

	findings = append(findings, auditDebtFiles(repoRoot)...)

	sortFindings(findings)
	return findings
}

// auditDebtFiles holds debtFiles to the same standard as every other declared
// allowance: it must name a real subject, and it must not be able to grow
// quietly.
//
// Both halves matter, and each one was missing for a different reason. The
// abbreviation stays out of `tokens` because prose in files owned by other work
// cannot be fixed by this change — but "absent from the dictionary" also meant
// **absent from the verdict**: a new mention could be added and the gate would
// still print OK, with only a number moving that nobody compares. Enumerating
// the remainder puts it back under the verdict without burying the findings that
// are actionable today: an undeclared mention fails, and a declared entry that
// no longer has anything to allow fails too.
func auditDebtFiles(repoRoot string) []Finding {
	var findings []Finding

	for rel, why := range debtFiles {
		full := filepath.Join(repoRoot, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err != nil {
			findings = append(findings, Finding{
				Path: rel,
				Text: "declared provider abbreviation entry points at a file that is gone (" + why + ") — delete it from debtFiles",
			})
			continue
		}
		if countDebtTokens(readText(full)) == 0 {
			findings = append(findings, Finding{
				Path: rel,
				Text: "stale declared provider abbreviation entry (" + why + "): nothing in this file carries it any more — delete it from debtFiles",
			})
		}
	}

	for _, rel := range undeclaredDebtFiles(repoRoot) {
		findings = append(findings, Finding{
			Path: rel,
			Text: "carries the provider abbreviation but is not declared — remove the mention, " +
				"or add the path to debtFiles in tools/foreignclouds with the reason it cannot be removed yet",
		})
	}

	return findings
}

// undeclaredDebtFiles returns the files that carry a debt token without an entry
// in debtFiles. Walk rules are ScanDebt's, so the enumerated list and the printed
// number are answers about the same set of files — a guard and a self-report that
// disagree are worse than either alone.
func undeclaredDebtFiles(repoRoot string) []string {
	var out []string
	ignored := ignoredPaths(repoRoot)

	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || ignored.covers(rel) {
				return fs.SkipDir
			}
			return nil
		}
		if ignored.covers(rel) {
			return nil
		}
		if _, declared := debtFiles[rel]; declared {
			return nil
		}
		if countDebtTokens(readText(path)) > 0 {
			out = append(out, rel)
		}
		return nil
	})

	sort.Strings(out)
	return out
}

// unusedCoordinates returns the coordinate exemptions that never stood between
// a token and a finding.
//
// The walk applies THE SAME skip set as Scan — exempt files, dependency
// manifests, non-authored directories, whatever version control ignores —
// because a coordinate can only earn its place on a line Scan actually judges.
// A file Scan skips wholesale has no findings for a coordinate to suppress, so
// an occurrence there is not use.
//
// That was the defect, and it disabled the expiry mechanism outright: the walk
// read the whole tree including this very file, where every coordinate is
// spelled out in its own declaration. Each one therefore found itself and was
// marked used, so no coordinate could EVER be reported stale — the list was able
// only to grow, which is exactly the blanket allowance the audit exists to
// prevent. The same held for the fixtures that prove the gate fires, and for a
// build artefact naming a cloud in an ignored directory.
func unusedCoordinates(repoRoot string) []coordinate {
	used := make(map[string]bool, len(coordinates))
	ignored := ignoredPaths(repoRoot)

	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			if ignored.covers(rel + "/") {
				return fs.SkipDir
			}
			return nil
		}
		if skipFiles[d.Name()] {
			return nil
		}
		if _, exempt := exemptFiles[rel]; exempt {
			return nil
		}
		if ignored.covers(rel) {
			return nil
		}
		for _, line := range strings.Split(readText(path), "\n") {
			for _, m := range findTokens(line) {
				for _, c := range coordinates {
					if !used[c.text] && coversMatch(line, c.text, m) {
						used[c.text] = true
					}
				}
			}
		}
		return nil
	})

	var out []coordinate
	for _, c := range coordinates {
		if !used[c.text] {
			out = append(out, c)
		}
	}
	return out
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
//
// Every path reaching here was produced by this tool's own filepath.WalkDir of
// the repository root — it is a file the walk already found, never a name taken
// from a request, an argument, or a config value. This is a build-time gate over
// the checked-out tree; it links into no server.
func readText(path string) string {
	b, err := os.ReadFile(path) // #nosec G304 -- path comes from this tool's own WalkDir of the repo root, not from any input
	if err != nil || !utf8.Valid(b) || strings.IndexByte(string(b), 0) >= 0 {
		return ""
	}
	return string(b)
}

func hasMatch(s string) bool { return len(findTokens(s)) > 0 }

// scanPath reports a token in the file's own name or in a directory above it.
// The ban covers names, and a path is a name.
func scanPath(rel string) []Finding {
	var out []Finding
	for _, m := range findTokens(rel) {
		out = append(out, Finding{
			Path: rel,
			Text: fmt.Sprintf("path names another cloud (%q) — rename it", m.token),
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
		for _, m := range findTokens(line) {
			if isCoordinate(line, m) {
				continue
			}
			out = append(out, Finding{
				Path: rel,
				Line: n + 1,
				Text: fmt.Sprintf("names another cloud (%q): %s", m.token, strings.TrimSpace(line)),
			})
		}
	}
	return out
}

// isCoordinate reports whether the match sits inside the name of third-party
// software we consume.
func isCoordinate(line string, m match) bool {
	for _, c := range coordinates {
		if coversMatch(line, c.text, m) {
			return true
		}
	}
	return false
}

// coversMatch reports whether some occurrence of coord in line encloses m.
func coversMatch(line, coord string, m match) bool {
	lower := strings.ToLower(line)
	needle := strings.ToLower(coord)
	for from := 0; from < len(lower); {
		at := strings.Index(lower[from:], needle)
		if at < 0 {
			return false
		}
		start := from + at
		if m.start >= start && m.end <= start+len(needle) {
			return true
		}
		from = start + 1
	}
	return false
}

// Report renders findings for a human, with the reason the rule exists.
func Report(findings []Finding) string {
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "verify-no-foreign-clouds: %s\n", f)
	}
	fmt.Fprint(&b, `
Kachō is described in its own terms. No other cloud provider is named in this
tree — not in code, not in documentation, not in comments, not in environment
variables, not in file names.

If a match is the name of third-party software we consume, or sits in a file
that carries the token BECAUSE the ban was enforced (a proto reserving retired
field names, a test asserting their absence), add it to the enumerated list in
tools/foreignclouds with the reason written down. Nothing else is exempt.
`)
	return b.String()
}

// ---------------------------------------------------------------------------
// Заявленный долг: сокращение, которое гейт намеренно НЕ роняет
// ---------------------------------------------------------------------------

// debtTokens — имена чужого облака, которые гейт НЕ считает нарушением, потому что
// починить их одним изменением нельзя. Сейчас в списке одно: двухбуквенное сокращение
// провайдера (см. пояснение к `tokens` выше).
//
// Зачем отдельный счёт. Исключение, о котором молчат, неотличимо от отсутствия
// предмета: пока сокращение просто не входило в список токенов, гейт печатал «OK», и по
// его выводу нельзя было узнать, что запрет #2 в дереве нарушен — вместе с тем, что
// нарушение оформлено как ЦЕЛЬ («parity goal» в обзорных документах, критерий приёмки,
// целый реестр расхождений относительно чужого API). Число делает долг видимым, не
// заваливая находки, которые исправимы прямо сейчас.
var debtTokens = []string{"yc"}

// debtFiles — files that still carry a debt token, each with the reason it is
// still there. Keyed by slash-separated path from the repository root.
//
// Почему список, а не запись в `tokens`. Промоушен сокращения потребовал бы
// **whole-file** exemption на каждый файл из остатка — а такая запись глушит и
// ПОЛНЫЕ имена провайдеров в том же файле, то есть ослабляет гейт ради прозы.
// Отдельный список слабее по охвату (разрешает только сокращение) и строже по
// последствиям: полное имя в любом из этих файлов по-прежнему роняет сборку.
//
// Запись обязана называть причину и истекает сама: см. auditDebtFiles.
var debtFiles = map[string]string{
	// Применённые миграции неизменны (ban #5) — упоминание в них нельзя убрать
	// никаким изменением этого дерева.
	"services/compute/internal/migrations/0001_initial.sql":           "applied migration: comment and seeded catalogue keys are frozen (ban #5)",
	"services/compute/internal/migrations/0016_instance_redesign.sql": "applied migration: comment recording the retirement is frozen (ban #5)",

	// Токен здесь — ПРОВЕРЯЕМОЕ ЗНАЧЕНИЕ, а не проза: кейс утверждает, что
	// brand-токенов НЕТ в ответе. Убрать его — значит удалить проверку.
	"services/compute/tests/newman/cases/instance-redesign.py":                            "conformance case: the token is an asserted value — the case proves it is ABSENT from responses",
	"services/compute/tests/newman/collections/instance-redesign.postman_collection.json": "generated from the case above",

	// Этот файл обязан выписать сокращение, которое считает.
	"tools/foreignclouds/foreignclouds.go":      "this gate: it has to spell the abbreviation it counts",
	"tools/foreignclouds/foreignclouds_test.go": "the fixtures that prove this list fires on an undeclared mention and expires on a dead entry",

	// Проза в комментариях, которую эта правка не трогала: файлы находятся в
	// работе у параллельных задач (newman-кейсы/скрипты, services/iam/internal).
	// Правка тех же слов рядом с чужими изменениями даёт конфликт на ровном
	// месте, поэтому вынесено следующим шагом — запись истечёт сама, когда
	// упоминание уйдёт.
	"services/nlb/tests/newman/cases/operation.py":             "prose comment; file is owned by concurrent newman work",
	"services/compute/tests/newman/scripts/run-incremental.js": "prose comment in the suite runner; file is owned by concurrent newman work",
	"services/compute/tests/newman/scripts/run-incremental.sh": "prose comment in the suite runner; file is owned by concurrent newman work",
	"services/iam/internal/apps/kacho/shared/doc.go":           "prose comment; services/iam/internal is owned by concurrent work",
	"services/iam/internal/apps/kacho/shared/errors.go":        "prose comment; services/iam/internal is owned by concurrent work",
	"services/iam/internal/domain/project.go":                  "prose comment; services/iam/internal is owned by concurrent work",
	"services/iam/internal/domain/status.go":                   "prose comment; services/iam/internal is owned by concurrent work",
}

// Debt — сводка заявленного долга: сколько вхождений в скольких файлах.
type Debt struct {
	Occurrences int
	Files       int
	TopFiles    []string // до десяти файлов с наибольшим числом вхождений, отсортированы
}

// ScanDebt считает вхождения debtTokens по тому же дереву и по тем же правилам
// границ, что и Scan. Не влияет на вердикт — только на его наблюдаемость.
func ScanDebt(repoRoot string) (Debt, error) {
	perFile := map[string]int{}
	ignored := ignoredPaths(repoRoot)

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || ignored.covers(rel) {
				return fs.SkipDir
			}
			return nil
		}
		if ignored.covers(rel) {
			return nil
		}
		content := readText(path)
		if content == "" {
			return nil
		}
		n := 0
		for _, line := range strings.Split(content, "\n") {
			n += countDebtTokens(line)
		}
		if n > 0 {
			perFile[rel] = n
		}
		return nil
	})
	if err != nil {
		return Debt{}, err
	}

	d := Debt{Files: len(perFile)}
	names := make([]string, 0, len(perFile))
	for f, n := range perFile {
		d.Occurrences += n
		names = append(names, f)
	}
	sort.Slice(names, func(i, j int) bool {
		if perFile[names[i]] != perFile[names[j]] {
			return perFile[names[i]] > perFile[names[j]]
		}
		return names[i] < names[j]
	})
	for i, f := range names {
		if i == 10 {
			break
		}
		d.TopFiles = append(d.TopFiles, fmt.Sprintf("%s (%d)", f, perFile[f]))
	}
	return d, nil
}

// countDebtTokens counts debt-token occurrences in s with the same identifier
// boundaries Scan uses, so the number is comparable with the findings above it.
func countDebtTokens(s string) int {
	lower := strings.ToLower(s)
	n := 0
	for _, tok := range debtTokens {
		for i := 0; ; {
			j := strings.Index(lower[i:], tok)
			if j < 0 {
				break
			}
			start := i + j
			end := start + len(tok)
			if !alnumAt(lower, start-1) && !alnumAt(lower, end) {
				n++
			}
			i = end
		}
	}
	return n
}
