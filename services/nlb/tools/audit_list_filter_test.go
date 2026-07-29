// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package tools_regression locks the behaviour of this service's audit-list-filter
// gate.
//
// A gate that quietly stopped catching regressions is itself a regression, so what
// this one recognises — and what it refuses to vouch for — is asserted here rather
// than narrated in a comment. Every test drives the gate through the SAME wrapper
// CI issues (`make -C services/nlb audit-list-filter` → tools/audit-list-filter.sh),
// so the whole chain is under test: wrapper → go run → flags → analysis.
//
// Four properties are asserted, and all four are needed:
//
//   - identification is by WHAT THE DECLARATION IS — a package that declares `List`
//     on the transport type. The FILE holding the List use-case is not part of the
//     resource's identity: splitting or renaming list.go is routine, and it must
//     not remove the resource from the gate's view;
//   - prose is not code. Deleting the filter call and leaving the sentence that
//     described it is the ordinary shape of a deletion, and the gate must tell the
//     two apart;
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

// anchorDir is the directory holding the resource packages this gate reads.
const anchorDir = "internal/apps/kacho/api"

// serviceRoot returns services/nlb (the directory holding internal/… and tools/).
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
	pkgs, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	for _, p := range pkgs {
		if !p.IsDir() {
			continue
		}
		files, ferr := os.ReadDir(filepath.Join(src, p.Name()))
		if ferr != nil {
			t.Fatalf("read %s: %v", p.Name(), ferr)
		}
		out := filepath.Join(dst, anchorDir, p.Name())
		if merr := os.MkdirAll(out, 0o755); merr != nil {
			t.Fatalf("mkdir: %v", merr)
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".go") {
				continue
			}
			b, rerr := os.ReadFile(filepath.Join(src, p.Name(), f.Name()))
			if rerr != nil {
				t.Fatalf("read %s/%s: %v", p.Name(), f.Name(), rerr)
			}
			if werr := os.WriteFile(filepath.Join(out, f.Name()), b, 0o644); werr != nil {
				t.Fatalf("write %s/%s: %v", p.Name(), f.Name(), werr)
			}
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

// filterCall is the per-object narrowing the load balancer List performs.
const filterCall = "recs, err = authzfilter.FilterVisiblePage(ctx, u.authz,"

// dropPerObjectFilter removes the per-object visibility filter from the load
// balancer List and leaves behind a comment that names it — the ordinary shape of a
// deletion: the call goes, the prose about it stays. Prose must never satisfy the
// gate, so the leak has to be reported all the same.
func dropPerObjectFilter(t *testing.T, root, listFile string) {
	t.Helper()
	patch(t, root, listFile, filterCall,
		"// RBAC: страницу сужает authzfilter.FilterVisiblePage(ctx, u.authz, …).\n\t"+
			"recs, err = authzfilter.UnfilteredPage(ctx, u.authz,")
}

// moveListFile applies a legitimate refactor: the List use-case moves from list.go
// to list_usecase.go. Nothing about the resource changes — the same declarations,
// in the same package — so the gate must go on judging it.
func moveListFile(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, anchorDir, "loadbalancer")
	if err := os.Rename(filepath.Join(dir, "list.go"), filepath.Join(dir, "list_usecase.go")); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

// TestAuditListFilter_RealTreePasses runs the gate against the real kacho-nlb tree
// through the very command CI issues, and requires it to say WHAT it judged.
// "OK" on a tree the gate never opened is the failure mode this asserts against.
func TestAuditListFilter_RealTreePasses(t *testing.T) {
	out, err := runGate(t, serviceRoot(t))
	if err != nil {
		t.Fatalf("audit-list-filter must pass against the real kacho-nlb tree: %v\n--- output ---\n%s", err, out)
	}
	if strings.Contains(out, "examined 0 ") || strings.Contains(out, ", 0 resource") {
		t.Fatalf("the gate passed having examined nothing — that is not a pass\n--- output ---\n%s", out)
	}
	for _, want := range []string{"checked ", "listener", "loadbalancer", "targetgroup"} {
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
	dropPerObjectFilter(t, root, "loadbalancer/list.go")

	out, err := runGate(t, root)
	if err == nil {
		t.Fatalf("a List that no longer narrows its page per-object must be a finding\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "loadbalancer") {
		t.Errorf("the finding must name the resource\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "handler.go") {
		t.Errorf("the finding must carry the coordinate of the List declaration\n--- output ---\n%s", out)
	}
}

// TestAuditListFilter_LegitimateRefactorKeepsTheResourceVisible — injection, silent
// direction: moving the List use-case to another file of the same package changes
// nothing about the resource, so the gate must stay quiet AND must still count it.
// Without the second half the silence would be indistinguishable from blindness.
func TestAuditListFilter_LegitimateRefactorKeepsTheResourceVisible(t *testing.T) {
	root := copyAnchor(t)
	moveListFile(t, root)

	out, err := runGate(t, root)
	if err != nil {
		t.Fatalf("moving the List use-case within its package must not fail the gate: %v\n--- output ---\n%s", err, out)
	}
	if !strings.Contains(out, "loadbalancer") {
		t.Fatalf("the moved List must still be judged, not merely not-flagged\n--- output ---\n%s", out)
	}
}

// TestAuditListFilter_RefactorMustNotHideALeak — both injections together, and the
// reason identification cannot key on a file name. A gate that recognises the
// resource by the presence of list.go stops seeing it after the move, and then
// reports OK for a List that hands every project member every row.
func TestAuditListFilter_RefactorMustNotHideALeak(t *testing.T) {
	root := copyAnchor(t)
	moveListFile(t, root)
	dropPerObjectFilter(t, root, "loadbalancer/list_usecase.go")

	out, err := runGate(t, root)
	if err == nil {
		t.Fatalf("a moved file must not hide an unfiltered List\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "loadbalancer") {
		t.Errorf("the finding must name the resource\n--- output ---\n%s", out)
	}
}

// TestAuditListFilter_ExpiredWhitelistEntryIsAFinding — an exclusion must expire by
// itself. `--allow=<resource>` suppresses a check; once no such resource exists the
// entry suppresses nothing, and the next resource to inherit that name inherits the
// blind spot in silence.
func TestAuditListFilter_ExpiredWhitelistEntryIsAFinding(t *testing.T) {
	out, err := runGate(t, serviceRoot(t), "--allow=retired_resource")
	if err == nil {
		t.Fatalf("a whitelist entry matching no resource must be reported\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "retired_resource") {
		t.Errorf("the finding must name the expired entry\n--- output ---\n%s", out)
	}
}
