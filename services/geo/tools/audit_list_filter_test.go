// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package tools_regression locks the behaviour of kacho-geo's audit-list-filter
// gate against the REAL tree.
//
// A gate that quietly stopped catching regressions is itself a regression, so what
// this one recognises — and what it refuses to vouch for — is asserted here rather
// than narrated in a comment. Every test drives the gate through the SAME wrapper
// CI issues (tools/audit-list-filter.sh), so the whole chain is under test:
// wrapper → go run → flags → analysis.
//
// # Why this file did not exist until #684
//
// geo's analyser had no regression cover at all. That is the same shape as the
// defect the analyser itself was written for: the service was judged by something
// nobody had ever proved could fail, and "it passes" was indistinguishable from "it
// cannot do otherwise".
//
// # What geo declares, and why it is not what its four neighbours declare
//
// Every other service using this gate derives the enumerate-then-narrow ban from
// the method sets of the authorization surfaces it declares. geo has none to
// declare: it holds no authorization client of its own, and its per-RPC Check is
// the shared interceptor's, wired in the composition root. Naming a source anyway —
// copying a neighbour's answer — would be a declaration with nothing behind it.
//
// What it declares instead is the property the gate can PROVE: both listings are
// the admin-curated placement catalog, answered without narrowing, so no page here
// can be taken from an enumeration. Four properties follow, and all four are needed:
//
//   - the real tree passes, and the census states the COUNT that proves the
//     premise — not merely that an exemption was written;
//   - the exemption EXPIRES WITH ITS SUBJECT: the first listing that narrows takes
//     it away, and the finding names it. Without this half, the declaration would
//     be a sentence any service could write to silence the line;
//   - "zero findings" must be unreachable from "zero read": a gate pointed at a
//     tree it cannot open reports a finding, never OK;
//   - a declaration lives only while it has a subject: an entry matching no method
//     is itself a finding.
package tools_regression

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// anchorDir is the directory holding the transport declarations this gate reads.
const anchorDir = "internal/handler"

// treesRead are the directories the gate parses for geo. There is exactly one, and
// that is itself the measurement behind the profile's declaration: geo has no
// authorization surface for the ban to be derived from.
var treesRead = []string{anchorDir}

// serviceRoot returns services/geo (the directory holding internal/… and tools/).
func serviceRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(self))
}

// runGate runs the production wrapper against the tree rooted at root and returns
// its combined output plus the verdict (nil ⇒ the gate passed that tree).
//
// The working directory is set to root as well as passing --root: a gate that reads
// the CURRENT directory instead of the one it was given must still be judged on the
// tree under test, never on the real service.
func runGate(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	argv := append([]string{filepath.Join(serviceRoot(t), "tools", "audit-list-filter.sh"), "--root=" + root}, args...)
	cmd := exec.Command("bash", argv...)
	cmd.Dir = root
	raw, err := cmd.CombinedOutput()
	return string(raw), err
}

// copyTrees materialises a throwaway copy of everything the gate reads, so an
// injection can be made against the REAL declarations without touching the repo.
//
// The file set comes from the git INDEX (pkg/treecorpus), not from a disk
// walk: under services/ a walk also reads what the repository does not contain —
// agent working copies, generated directories, run reports — every one of them
// already declared out of the tree by .gitignore.
func copyTrees(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := serviceRoot(t)
	copied := 0
	for _, tree := range treesRead {
		paths, err := treecorpus.UnderWithSuffix(filepath.Join(src, tree), ".go")
		if err != nil {
			t.Fatalf("состав %s: %v", tree, err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			rel, rerr := filepath.Rel(src, path)
			if rerr != nil {
				t.Fatalf("rel %s: %v", path, rerr)
			}
			out := filepath.Join(dst, rel)
			if merr := os.MkdirAll(filepath.Dir(out), 0o755); merr != nil {
				t.Fatalf("mkdir %s: %v", out, merr)
			}
			b, rderr := os.ReadFile(path)
			if rderr != nil {
				t.Fatalf("read %s: %v", path, rderr)
			}
			if werr := os.WriteFile(out, b, 0o644); werr != nil {
				t.Fatalf("write %s: %v", out, werr)
			}
			copied++
		}
	}
	// "Ноль прочитанного" обязано быть отличимо от "ноль находок": копия, собранная
	// из пустого состава, дала бы гейту пустое дерево, и КАЖДАЯ проба ниже упала бы
	// с сообщением не о своём предмете.
	if copied == 0 {
		t.Fatalf("состав пуст: скопировано 0 файлов из %v — инъекции ниже утверждали бы "+
			"о дереве, которого нет", treesRead)
	}
	return dst
}

// patch rewrites one file of the copied tree and requires the replacement to apply,
// so an injection that stopped modelling anything fails loudly instead of passing.
func patch(t *testing.T, root, rel, old, replacement string) {
	t.Helper()
	path := filepath.Join(root, rel)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	s := string(b)
	if !strings.Contains(s, old) {
		t.Fatalf("injection point %q is gone from %s — the injection no longer models anything", old, rel)
	}
	if werr := os.WriteFile(path, []byte(strings.ReplaceAll(s, old, replacement)), 0o644); werr != nil {
		t.Fatalf("write %s: %v", path, werr)
	}
}

// TestAuditListFilter_RealTreePasses runs the gate against the real kacho-geo tree
// through the very command CI issues, and requires it to say WHAT it judged.
// "OK" on a tree the gate never opened is the failure mode this asserts against.
func TestAuditListFilter_RealTreePasses(t *testing.T) {
	out, err := runGate(t, serviceRoot(t))
	if err != nil {
		t.Fatalf("audit-list-filter must pass against the real kacho-geo tree: %v\n--- output ---\n%s", err, out)
	}
	if strings.Contains(out, "examined 0 ") || strings.Contains(out, ", 0 resource") {
		t.Fatalf("the gate passed having examined nothing — that is not a pass\n--- output ---\n%s", out)
	}
	for _, want := range []string{
		"region.List", "zone.List",
		"cluster-scoped by declaration region.List, zone.List",
		// the ban is stated on the passing path too, and the exemption is stated
		// with the COUNT that proves it rather than as a claim on its own
		"enumerate-then-narrow ban",
		"no enumeration source, and none can apply: 0 of 2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("a passing run must report its census (missing %q)\n--- output ---\n%s", want, out)
		}
	}
	// The непокрытость geo printed until #684 must be GONE, and gone in the right
	// direction: replaced by a declared absence, not by silence. The two look alike
	// from a distance and mean opposite things — "nothing to apply, here is the
	// proof" against "nobody looked".
	if strings.Contains(out, "no enumeration source declared") {
		t.Errorf("the absence must be DECLARED, not merely printed as unwatched\n--- output ---\n%s", out)
	}
	t.Log(strings.TrimSpace(out))
}

// TestAuditListFilter_AbsentRootIsAFinding — a gate that examined nothing has
// proven nothing. Exiting 0 there is the worst answer available: it is
// indistinguishable from a clean tree, so pointing the gate at the wrong place — or
// moving the directory it reads — silently turns it into decoration.
func TestAuditListFilter_AbsentRootIsAFinding(t *testing.T) {
	out, err := runGate(t, t.TempDir())
	if err == nil {
		t.Fatalf("the gate exited 0 on a tree it never opened — that is not a pass\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, anchorDir) {
		t.Errorf("the finding must name the root it could not read\n--- output ---\n%s", out)
	}
}

// TestAuditListFilter_InapplicabilityExpiresWithTheFirstNarrowingListing — the half
// that makes the declaration worth anything.
//
// geo says the enumerate-then-narrow ban has nothing to apply to here. That is true
// only while nothing narrows, and it is a fact about the TREE, not about the
// profile — so it is injected against the tree: a new listing arrives on a handler
// the profile does not declare, which defaults to the strictest shape. The gate must
// take the exemption away and say which listing took it.
//
// Without this, EnumerationInapplicable would be a sentence any service could write
// to silence the census line — the exact form-without-substance this gate exists to
// refuse.
func TestAuditListFilter_InapplicabilityExpiresWithTheFirstNarrowingListing(t *testing.T) {
	root := copyTrees(t)
	patch(t, root, anchorDir+"/public.go",
		"func (h *RegionHandler) List(",
		"type PlacementHandler struct{ uc placementUseCase }\n\n"+
			"func (h *PlacementHandler) List(ctx context.Context) error {\n"+
			"\trows, _ := h.uc.List(ctx)\n\t_ = rows\n\treturn nil\n}\n\n"+
			"func (h *RegionHandler) List(")

	out, err := runGate(t, root)
	if err == nil {
		t.Fatalf("a narrowing listing must take the exemption away\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "The premise is gone") {
		t.Errorf("the finding must say the premise is gone\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "placement.List") {
		t.Errorf("the finding must name the listing that took the premise away\n--- output ---\n%s", out)
	}
}

// TestAuditListFilter_ExpiredDeclarationIsAFinding — a declaration lives only while
// it has a subject. Once no such method exists the entry describes nothing, and the
// next method to inherit that name inherits an enforcement claim nobody checked.
//
// The orphan is produced by DELETING a declared method from a copy of the real tree,
// rather than by passing a made-up name: the declaration under test is geo's own, so
// this cannot pass against a profile whose entries no longer match anything.
func TestAuditListFilter_ExpiredDeclarationIsAFinding(t *testing.T) {
	root := copyTrees(t)
	patch(t, root, anchorDir+"/public.go",
		"func (h *ZoneHandler) List(", "func (h *ZoneHandler) Fetch(")

	out, err := runGate(t, root)
	if err == nil {
		t.Fatalf("a declaration matching no method must be reported\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "zone.List") {
		t.Errorf("the finding must name the expired entry\n--- output ---\n%s", out)
	}
}
