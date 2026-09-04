// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package foreignclouds

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// write lays one file into a throwaway tree.
func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scan(t *testing.T, root string) []Finding {
	t.Helper()
	f, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestFires is the half that matters: every shape of violation must be caught.
// A gate that cannot be made to fail is not a gate.
func TestFires(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		body string
	}{
		{"prose in documentation", "docs/x.md", "Behaves like Yandex Cloud does.\n"},
		{"go comment", "svc/a.go", "// same shape as the AWS metadata service\n"},
		{"identifier component", "proto/a.proto", "  MetadataOption aws_v1_http_endpoint = 2;\n"},
		{"environment variable", "deploy/env.yaml", "  KACHO_GCP_PROJECT: x\n"},
		{"lower-case acronym", "svc/b.go", "// gcp-flavoured token exchange\n"},
		{"another provider", "docs/y.md", "Deployed on Hetzner.\n"},
		{"file name", "scripts/yandex-proxy.js", "const x = 1;\n"},
		{"directory name", "tests/aws/fixture.json", "{}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, c.rel, c.body)
			if got := scan(t, root); len(got) == 0 {
				t.Fatalf("no finding for %s — the gate would pass on a real violation", c.rel)
			}
		})
	}
}

// TestDoesNotOverFire is the other half. A rule that flags ordinary English
// teaches contributors to write around the matcher rather than to avoid the
// vendor, which is why one such rule was deleted from the comment guard.
func TestDoesNotOverFire(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/prose.md", strings.Join([]string{
		"The laws of the pool are simple.",
		"It draws an address from the free list.",
		"Gceilings and awsome are not words, but neither names a cloud.",
		"The gateway forwards the request verbatim.",
		"Amazonite is a mineral; amazons are not a provider.",
	}, "\n"))
	write(t, root, "go.sum", "github.com/Azure/go-ansiterm v0.0.0 h1:x=\n")
	write(t, root, "ui/package-lock.json", `{"name":"@aws-sdk/client-s3"}`)
	write(t, root, ".github/workflows/ci.yaml", "      - uses: azure/setup-helm@v4\n")
	write(t, root, "deploy/zot.yaml", "  # credentials arrive as AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY\n")

	if got := scan(t, root); len(got) != 0 {
		t.Fatalf("gate fired on legitimate content:\n%s", Report(got))
	}
}

// TestAmazoniteIsNotAProvider pins the boundary rule on its own, because
// "amazon" is the token most likely to be widened into ordinary words.
func TestAmazoniteIsNotAProvider(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/rock.md", "Amazonite polishes well.\n")
	if got := scan(t, root); len(got) != 0 {
		t.Fatalf("boundary rule is too wide:\n%s", Report(got))
	}

	write(t, root, "docs/cloud.md", "Runs on Amazon.\n")
	if got := scan(t, root); len(got) == 0 {
		t.Fatal("boundary rule is too narrow: the provider itself went unnoticed")
	}
}

// TestCoordinateExemptionIsLocal proves the third-party-coordinate exemption
// frees the coordinate and nothing else on the same line.
func TestCoordinateExemptionIsLocal(t *testing.T) {
	root := t.TempDir()
	write(t, root, "deploy/z.yaml", "  # AWS_ACCESS_KEY_ID comes from the secret, and we also run on Yandex\n")
	got := scan(t, root)
	if len(got) != 1 {
		t.Fatalf("expected exactly the non-coordinate match, got %d:\n%s", len(got), Report(got))
	}
	if !strings.Contains(got[0].Text, "yandex") {
		t.Fatalf("wrong match survived: %s", got[0])
	}
}

// TestStaleExemptionIsReported keeps the exemption list from rotting into a
// blanket allowance: an entry whose file is gone, or which stopped matching,
// must be noticed rather than accumulate.
func TestStaleExemptionIsReported(t *testing.T) {
	root := t.TempDir()
	got := AuditExemptions(root)
	// Every file entry dangles — exemptions and declared-abbreviation entries
	// alike — and every coordinate suppresses nothing.
	if want := len(exemptFiles) + len(debtFiles) + len(coordinates); len(got) != want {
		t.Fatalf("expected %d dangling-allowance findings in an empty tree, got %d:\n%s",
			want, len(got), Report(got))
	}

	// A file that exists but no longer carries a token is stale in the other way.
	const rel = "tools/foreignclouds/foreignclouds.go"
	if _, ok := exemptFiles[rel]; !ok {
		t.Fatalf("%s is expected to be an exemption", rel)
	}
	write(t, root, rel, "package foreignclouds\n")
	// The same path is also a declared-abbreviation entry, so it produces two
	// stale findings; assert on the exemption one specifically.
	for _, f := range AuditExemptions(root) {
		if f.Path == rel && strings.Contains(f.Text, "stale exemption") {
			return
		}
	}
	t.Fatalf("a token-free exempt file went unreported")
}

// TestCoordinateExemptionCanGoStale is the direction the audit could not answer.
//
// A coordinate exemption is a line-local licence: it earns its place only while
// some line OUTSIDE the exemption list would otherwise be a finding. The walk
// looking for that line did not apply the exemption list to itself, so every
// coordinate found its own declaration in this package's source and marked
// itself used. The expiry mechanism was therefore dead by construction — the
// list could only grow, which is the very defect the audit exists to catch.
//
// The tree here contains the coordinates ONLY inside exempt files: the gate's
// own source and a fixture file. Since those files are skipped wholesale by the
// scan, no coordinate can stand between a token and a finding, so every one of
// them must be reported.
func TestCoordinateExemptionCanGoStale(t *testing.T) {
	root := t.TempDir()

	// Every exempt file present and carrying tokens, so that no FILE entry is
	// reported and only the coordinate verdict is left to read.
	for rel := range exemptFiles {
		body := "package p // yandex aws gcp azure amazon ycloud gce alibaba aliyun digitalocean hetzner\n"
		if rel == "tools/foreignclouds/foreignclouds.go" {
			// The real declaration block: this is exactly what used to satisfy
			// every coordinate.
			for _, c := range coordinates {
				body += "\t{" + strconv.Quote(c.text) + ", " + strconv.Quote(c.why) + "},\n"
			}
		}
		write(t, root, rel, body)
	}

	var stale []string
	for _, f := range AuditExemptions(root) {
		if strings.Contains(f.Text, "stale coordinate exemption") {
			stale = append(stale, f.Text)
		}
	}
	if len(stale) != len(coordinates) {
		t.Fatalf("coordinate exemptions reported stale: %d, want all %d.\n"+
			"An exemption whose only occurrence is inside the exemption list itself "+
			"suppresses nothing; if it is not reported, the list can never expire.\ngot:\n  %s",
			len(stale), len(coordinates), strings.Join(stale, "\n  "))
	}
}

// TestCoordinateExemptionWithASubjectIsNotReported is the mirror half: the same
// shape, legitimately in use, must stay silent. Without it the check above would
// be satisfied by an audit that simply reports every coordinate always.
func TestCoordinateExemptionWithASubjectIsNotReported(t *testing.T) {
	root := t.TempDir()
	for rel := range exemptFiles {
		write(t, root, rel, "package p // yandex aws gcp azure amazon ycloud gce alibaba aliyun digitalocean hetzner\n")
	}
	// One ordinary, non-exempt file where each coordinate really does stand
	// between a token and a finding.
	body := ""
	for _, c := range coordinates {
		body += "value: " + c.text + "\n"
	}
	write(t, root, "deploy/uses-third-party.yaml", body)

	for _, f := range AuditExemptions(root) {
		if strings.Contains(f.Text, "stale coordinate exemption") {
			t.Errorf("coordinate in genuine use reported stale — the audit reports the "+
				"form rather than the substance: %s", f)
		}
	}
}

// TestRealExemptionsAllEarnTheirPlace runs the same audit against the tree.
func TestRealExemptionsAllEarnTheirPlace(t *testing.T) {
	if got := AuditExemptions(repoRoot(t)); len(got) != 0 {
		t.Fatalf("exemption list has rotted:\n%s", Report(got))
	}
}

// repoRoot locates the repository from this file's own position.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(self), "..", "..")
}

// TestRepositoryNamesNoOtherCloud runs the gate against the real tree, so a
// violation fails `go test ./...` and not only the dedicated CI step.
func TestRepositoryNamesNoOtherCloud(t *testing.T) {
	got := scan(t, repoRoot(t))
	if len(got) != 0 {
		t.Fatalf("%d violation(s) of the second non-negotiable:\n%s", len(got), Report(got))
	}
}

// TestCIRunsThisGate locks the wiring. A gate CI does not call is worth exactly
// as much as no gate; that pairing has already happened once in this repository.
func TestCIRunsThisGate(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	const invocation = "go run ./tools/foreignclouds/cmd/verify-no-foreign-clouds"
	if !strings.Contains(string(b), invocation) {
		t.Fatalf("ci.yaml does not run %q — wire it back in", invocation)
	}
}

// TestScan_SkipsWhatVersionControlIgnores pins the gate's verdict to the
// repository's CONTENT, not to whatever happens to sit in a working tree.
//
// Build output is the difference. A UI remote's bundled vendor chunk carries
// other clouds' names because the third-party library it bundles does; that
// chunk is ignored by version control and exists only on a machine that has run
// a build. Scanning it made the gate answer differently in a fresh checkout
// than on a developer's machine — the same tree, two verdicts, and the one CI
// reports is the one nobody sees locally. A gate whose result depends on
// unversioned local state cannot be a gate.
//
// Files that are merely untracked-and-not-ignored are STILL scanned: a newly
// authored file that has not been added yet is exactly what a pre-commit check
// must catch.
func TestScan_SkipsWhatVersionControlIgnores(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := gitenv.Command(root, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.invalid")
	run("config", "user.name", "t")

	mustWrite := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite(".gitignore", "dist/\n")
	// Ignored build output — must NOT be scanned.
	mustWrite("dist/bundle.js", "// deployed on aws by the vendor\n")
	// Authored, untracked, not ignored — must STILL be scanned.
	mustWrite("notes.md", "we behave like aws here\n")
	run("add", ".gitignore", "notes.md")

	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, f := range findings {
		if strings.HasPrefix(f.Path, "dist/") {
			t.Errorf("scanned ignored build output: %s", f)
		}
	}
	var sawAuthored bool
	for _, f := range findings {
		if f.Path == "notes.md" {
			sawAuthored = true
		}
	}
	if !sawAuthored {
		t.Error("authored untracked file was not scanned — the gate must still catch it")
	}
}

// ---------------------------------------------------------------------------
// Заявленный остаток по сокращению провайдера
// ---------------------------------------------------------------------------

// debtAudit runs the declared-allowance audit against a throwaway tree.
func debtAudit(t *testing.T, root string) []Finding {
	t.Helper()
	var out []Finding
	for _, f := range AuditExemptions(root) {
		if strings.Contains(f.Text, "provider abbreviation") {
			out = append(out, f)
		}
	}
	return out
}

// TestDebtMentionMustBeDeclared — незаявленное вхождение сокращения обязано быть
// находкой. Пока сокращение просто отсутствовало в словаре, новое упоминание
// добавлялось молча: число росло, вердикт не менялся. Список без энфорсмента
// роста — это не список, а комментарий.
func TestDebtMentionMustBeDeclared(t *testing.T) {
	root := t.TempDir()
	write(t, root, "tools/foreignclouds/foreignclouds.go", "package foreignclouds\n")
	write(t, root, "docs/new.md", "смотри yc-стилистику\n")

	got := debtAudit(t, root)
	var named bool
	for _, f := range got {
		if f.Path == "docs/new.md" {
			named = true
		}
	}
	if !named {
		t.Fatalf("undeclared mention of the abbreviation was not reported:\n%s", Report(got))
	}
}

// TestDebtEntryWithoutSubjectIsReported — запись, которой больше нечего
// разрешать, обязана истекать сама (testing.md §«Исключение живёт, пока у него
// есть предмет»). Иначе список только растёт и становится бланкетным.
func TestDebtEntryWithoutSubjectIsReported(t *testing.T) {
	root := t.TempDir()
	write(t, root, "tools/foreignclouds/foreignclouds.go", "package foreignclouds\n")
	// Файл существует, но упоминания в нём уже нет — значит запись мертва.
	write(t, root, "docs/fixed.md", "здесь всё описано в своих терминах\n")

	saved := debtFiles
	debtFiles = map[string]string{"docs/fixed.md": "test fixture"}
	defer func() { debtFiles = saved }()

	got := debtAudit(t, root)
	var named bool
	for _, f := range got {
		if f.Path == "docs/fixed.md" && strings.Contains(f.Text, "stale") {
			named = true
		}
	}
	if !named {
		t.Fatalf("dead entry was not reported as stale:\n%s", Report(got))
	}
}

// TestDebtEntryWithASubjectIsSilent — обратная половина: законная запись НЕ
// шумит. Без неё гейт ловил бы форму, а не существо, и первый ложный срабат
// его бы отключил.
func TestDebtEntryWithASubjectIsSilent(t *testing.T) {
	root := t.TempDir()
	write(t, root, "tools/foreignclouds/foreignclouds.go", "package foreignclouds\n")
	write(t, root, "migrations/0001.sql", "-- yc-style seed key\n")

	saved := debtFiles
	debtFiles = map[string]string{"migrations/0001.sql": "applied migration, frozen by ban #5"}
	defer func() { debtFiles = saved }()

	if got := debtAudit(t, root); len(got) != 0 {
		t.Fatalf("legitimate entry reported:\n%s", Report(got))
	}
}

// TestRealDebtListIsExact — список против реального дерева: ни устаревших
// записей, ни незаявленных упоминаний. Это и есть решение про словарь:
// сокращение не роняет гейт на прозе, но перечислено с причиной и защищено
// от роста.
func TestRealDebtListIsExact(t *testing.T) {
	if got := debtAudit(t, repoRoot(t)); len(got) != 0 {
		t.Fatalf("declared remainder disagrees with the tree (%d):\n%s", len(got), Report(got))
	}
}
