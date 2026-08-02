// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package tools_regression locks the behaviour of this service's audit-list-filter
// gate.
//
// A gate that quietly stopped catching regressions is itself a regression, so what
// this one recognises — and what it refuses to vouch for — is asserted here rather
// than narrated in a comment. Every test drives the gate through the SAME wrapper
// CI issues (`make -C services/compute audit-list-filter` →
// tools/audit-list-filter.sh), so the whole chain is under test: wrapper → go run →
// flags → analysis.
//
// Four properties are asserted, and all four are needed:
//
//   - identification is by WHAT THE DECLARATION IS — a method named `List` on a
//     receiver whose TYPE ends in `Handler`. The receiver VARIABLE's name is never
//     consulted: renaming `h` to `hd` is a refactor no reviewer would stop, and it
//     must not remove the resource from the gate's view;
//   - the judgement is scoped to what List CALLS, not to the file it sits in. The
//     handler's `authzfilter.Filter` field must not vouch for a List that dropped
//     its own filter;
//   - "zero findings" must be unreachable from "zero read". A gate pointed at a
//     tree it cannot open reports a finding, never OK;
//   - an exclusion lives only while it has a subject. `--allow=<resource>` matching
//     nothing is itself a finding.
package tools_regression

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// anchorDir is the directory holding the transport declarations this gate reads.
const anchorDir = "internal/handler"

// serviceRoot returns services/compute (the directory holding internal/… and tools/).
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

// copyAnchor materialises a throwaway copy of the service's anchor tree so an
// injection can be made against the REAL declarations without touching the repo.
func copyAnchor(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join(serviceRoot(t), anchorDir)
	out := filepath.Join(dst, anchorDir)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(src, e.Name()))
		if rerr != nil {
			t.Fatalf("read %s: %v", e.Name(), rerr)
		}
		if werr := os.WriteFile(filepath.Join(out, e.Name()), b, 0o644); werr != nil {
			t.Fatalf("write %s: %v", e.Name(), werr)
		}
	}
	return dst
}

// patch rewrites one file of the copied tree and requires the replacement to apply,
// so an injection that stopped modelling anything fails loudly instead of passing.
func patch(t *testing.T, root, rel, old, replacement string) {
	t.Helper()
	path := filepath.Join(root, anchorDir, rel)
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

// dropPerObjectFilter removes the per-object visibility filter from Instance.List:
// the page still comes back from the DB, and nothing narrows it to what the caller
// may see. This is the leak the gate exists to refuse.
//
// Nothing else in the file changes, and that is the point. `authzfilter.Filter`
// still appears in it — the handler's field type and its constructor — so a gate
// that searches the FILE for that token keeps reporting OK while the List leaks.
func dropPerObjectFilter(t *testing.T, root string) {
	t.Helper()
	patch(t, root, "instance_handler.go", "visible, err := filterVisible(ctx,", "visible, err := unfiltered(ctx,")
}

// renameReceiver applies a legitimate refactor: the receiver VARIABLE of every
// InstanceHandler method becomes `hd`. The resource is unchanged — still a `List`
// method on `*InstanceHandler` — so the gate must go on judging it.
func renameReceiver(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, anchorDir, "instance_handler.go")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := strings.ReplaceAll(string(b), "(h *InstanceHandler)", "(hd *InstanceHandler)")
	for _, field := range []string{"h.svc", "h.listFilter", "h.ops", "h.repo"} {
		s = strings.ReplaceAll(s, field, "hd"+strings.TrimPrefix(field, "h"))
	}
	if werr := os.WriteFile(path, []byte(s), 0o644); werr != nil {
		t.Fatalf("write: %v", werr)
	}
}

// TestAuditListFilter_RealTreePasses runs the gate against the real kacho-compute
// tree through the very command CI issues, and requires it to say WHAT it judged.
// "OK" on a tree the gate never opened is the failure mode this asserts against.
func TestAuditListFilter_RealTreePasses(t *testing.T) {
	out, err := runGate(t, serviceRoot(t))
	if err != nil {
		t.Fatalf("audit-list-filter must pass against the real kacho-compute tree: %v\n--- output ---\n%s", err, out)
	}
	if strings.Contains(out, "examined 0 ") || strings.Contains(out, ", 0 resource") {
		t.Fatalf("the gate passed having examined nothing — that is not a pass\n--- output ---\n%s", out)
	}
	for _, want := range []string{"instance.List", "cluster-scoped by declaration machine_type.List"} {
		if !strings.Contains(out, want) {
			t.Errorf("a passing run must report its census (missing %q)\n--- output ---\n%s", want, out)
		}
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

// TestAuditListFilter_UnfilteredListIsAFinding — injection, red direction: the
// per-object filter is removed from a REAL List, and the gate must name it.
func TestAuditListFilter_UnfilteredListIsAFinding(t *testing.T) {
	root := copyAnchor(t)
	dropPerObjectFilter(t, root)

	out, err := runGate(t, root)
	if err == nil {
		t.Fatalf("a List that no longer narrows its page per-object must be a finding\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "instance") {
		t.Errorf("the finding must name the resource\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "instance_handler.go") {
		t.Errorf("the finding must carry the coordinate\n--- output ---\n%s", out)
	}
}

// TestAuditListFilter_LegitimateRefactorKeepsTheResourceVisible — injection, silent
// direction: renaming the receiver variable changes nothing about the resource, so
// the gate must stay quiet AND must still count it. Without the second half the
// silence would be indistinguishable from blindness.
func TestAuditListFilter_LegitimateRefactorKeepsTheResourceVisible(t *testing.T) {
	root := copyAnchor(t)
	renameReceiver(t, root)

	out, err := runGate(t, root)
	if err != nil {
		t.Fatalf("renaming a receiver variable must not fail the gate: %v\n--- output ---\n%s", err, out)
	}
	if !strings.Contains(out, "instance.List") {
		t.Fatalf("the renamed receiver must still be judged, not merely not-flagged\n--- output ---\n%s", out)
	}
}

// TestAuditListFilter_RefactorMustNotHideALeak — both injections together, and the
// reason identification cannot key on a mutable label. A gate that recognises the
// resource by the receiver's NAME stops seeing it after the rename, and then reports
// OK for a List that hands every project member every row.
func TestAuditListFilter_RefactorMustNotHideALeak(t *testing.T) {
	root := copyAnchor(t)
	renameReceiver(t, root)
	dropPerObjectFilter(t, root)

	out, err := runGate(t, root)
	if err == nil {
		t.Fatalf("a renamed receiver must not hide an unfiltered List\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "instance") {
		t.Errorf("the finding must name the resource\n--- output ---\n%s", out)
	}
}

// TestAuditListFilter_ExpiredDeclarationIsAFinding — an exclusion must expire by
// itself. Once no such method exists the declaration describes nothing, and the next
// method to inherit that name inherits an enforcement claim nobody checked.
//
// The subject moved one level down when --allow=<resource> was replaced: that flag
// excluded a whole RESOURCE, so an exclusion written for a cluster catalog also
// covered listing methods added to that handler afterwards — which is how
// addresspool's ListAddresses went unjudged in vpc. Declarations are per method now.
//
// The orphan is produced by DELETING a declared method from a copy of the real tree,
// rather than by passing a made-up name: the declaration under test is the service's
// own, so this cannot pass against a profile whose entries no longer match anything.
func TestAuditListFilter_ExpiredDeclarationIsAFinding(t *testing.T) {
	root := copyAnchor(t)
	removed := deleteADeclaredListing(t, root)

	out, err := runGate(t, root)
	if err == nil {
		t.Fatalf("a declaration matching no method must be reported\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, removed) {
		t.Errorf("the finding must name the expired entry %q\n--- output ---\n%s", removed, out)
	}
}

// deleteADeclaredListing removes a DECLARED listing method from the copied tree by
// renaming it out of the listing surface, and returns the declaration key that is
// thereby orphaned.
//
// It deletes a real method rather than passing a made-up name, so the assertion is
// about THIS service's own declarations: if the profile's entries stopped matching
// the tree, this test could not pass by naming something imaginary.
func deleteADeclaredListing(t *testing.T, root string) string {
	t.Helper()
	patch(t, root, "machine_type_handler.go", "func (h *MachineTypeHandler) List(", "func (h *MachineTypeHandler) Fetch(")
	return "machine_type.List"
}
