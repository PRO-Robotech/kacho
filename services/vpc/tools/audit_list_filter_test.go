// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package tools_regression locks the behaviour of this service's audit-list-filter
// gate.
//
// A gate that quietly stopped catching regressions is itself a regression, so what
// this one recognises — and what it refuses to vouch for — is asserted here rather
// than narrated in a comment. Every test drives the gate through the SAME wrapper
// CI issues (`make -C services/vpc audit-list-filter` → tools/audit-list-filter.sh),
// so the whole chain is under test: wrapper → go run → flags → analysis.
//
// Four properties are asserted, and all four are needed:
//
//   - identification is by WHAT THE DECLARATION IS — a package that declares `List`
//     on the transport type. The FILE holding that declaration is not part of the
//     resource's identity: renaming handler.go is routine, and it must not remove
//     the resource from the gate's view;
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

// serviceRoot returns services/vpc (the directory holding internal/… and tools/).
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
	// vpc has TWO transport packages and the gate audits both, so a copy holding
	// only one of them would fail on the missing anchor rather than on whatever the
	// test injected — and the failure would look like the injection working.
	copyFlatPackage(t, dst, "internal/handler")
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

// filterCall is the per-object narrowing the Network List performs. It sits in a
// package-local helper, not in the use-case body — which is why the gate follows
// the calls List makes rather than reading one method.
const filterCall = "visible, ferr := listnarrow.Page(ctx, u.narrower,"

// dropPerObjectFilter removes the per-object visibility filter from the Network
// List and leaves behind a comment that names it — the ordinary shape of a
// deletion: the call goes, the prose about it stays. Prose must never satisfy the
// gate, so the leak has to be reported all the same.
func dropPerObjectFilter(t *testing.T, root string) {
	t.Helper()
	patch(t, root, "network/list.go", filterCall,
		"// пообъектно: listnarrow.Page(ctx, u.narrower, …) сужает страницу.\n\t"+
			"visible, ferr := listnarrow.AllOf(ctx, u.narrower,")
}

// renameHandlerFile applies a legitimate refactor: handler.go becomes
// grpc_handler.go. Nothing about the resource changes — the same declarations, in
// the same package — so the gate must go on judging it.
func renameHandlerFile(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, anchorDir, "network")
	if err := os.Rename(filepath.Join(dir, "handler.go"), filepath.Join(dir, "grpc_handler.go")); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

// TestAuditListFilter_RealTreePasses runs the gate against the real kacho-vpc tree
// through the very command CI issues, and requires it to say WHAT it judged.
// "OK" on a tree the gate never opened is the failure mode this asserts against.
func TestAuditListFilter_RealTreePasses(t *testing.T) {
	out, err := runGate(t, serviceRoot(t))
	if err != nil {
		t.Fatalf("audit-list-filter must pass against the real kacho-vpc tree: %v\n--- output ---\n%s", err, out)
	}
	if strings.Contains(out, "examined 0 ") || strings.Contains(out, ", 0 resource") {
		t.Fatalf("the gate passed having examined nothing — that is not a pass\n--- output ---\n%s", out)
	}
	for _, want := range []string{"network.List", "cluster-scoped by declaration addresspool.List"} {
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
	if !strings.Contains(out, "network") {
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
	renameHandlerFile(t, root)

	out, err := runGate(t, root)
	if err != nil {
		t.Fatalf("renaming handler.go must not fail the gate: %v\n--- output ---\n%s", err, out)
	}
	if !strings.Contains(out, "network") {
		t.Fatalf("the List in the renamed file must still be judged, not merely not-flagged\n--- output ---\n%s", out)
	}
}

// TestAuditListFilter_RefactorMustNotHideALeak — both injections together, and the
// reason identification cannot key on a file name. A gate that recognises the
// resource by the presence of handler.go stops seeing it after the rename, and then
// reports OK for a List that hands every project member every row.
func TestAuditListFilter_RefactorMustNotHideALeak(t *testing.T) {
	root := copyAnchor(t)
	renameHandlerFile(t, root)
	dropPerObjectFilter(t, root)

	out, err := runGate(t, root)
	if err == nil {
		t.Fatalf("a renamed file must not hide an unfiltered List\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "network") {
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
// Осиротить объявление значит убрать ВСЕХ его производителей. У пула адресов их
// два: внутренний транспорт и публичный, опубликованный ADM-1 S1 рядом с ним на
// время окна расширения. Переименование одного оставляло бы второй, ключ
// продолжал бы находиться, и проба зеленела бы, ничего не проверив, — то есть
// стала бы ровно тем, что она стережёт: формой без содержания.
//
// Когда внутренний транспорт снимут стадией S3, здесь останется одна строка. Это
// и есть признак, по которому видно, что фикстура следует за деревом, а не
// описывает его прошлое.
func deleteADeclaredListing(t *testing.T, root string) string {
	t.Helper()
	patch(t, root, "addresspool/handler.go", "func (h *Handler) ListAddresses(", "func (h *Handler) FetchAddresses(")
	patch(t, root, "addresspool/public_handler.go", "func (h *PublicHandler) ListAddresses(", "func (h *PublicHandler) FetchAddresses(")
	return "addresspool.ListAddresses"
}

// copyFlatPackage copies one flat (non-per-resource) package of the real tree into
// the fixture root, so the second profile has its anchor to read.
func copyFlatPackage(t *testing.T, dst, rel string) {
	t.Helper()
	src := filepath.Join(serviceRoot(t), rel)
	files, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	out := filepath.Join(dst, rel)
	if merr := os.MkdirAll(out, 0o755); merr != nil {
		t.Fatalf("mkdir: %v", merr)
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".go") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(src, f.Name()))
		if rerr != nil {
			t.Fatalf("read %s: %v", f.Name(), rerr)
		}
		if werr := os.WriteFile(filepath.Join(out, f.Name()), b, 0o644); werr != nil {
			t.Fatalf("write %s: %v", f.Name(), werr)
		}
	}
}
