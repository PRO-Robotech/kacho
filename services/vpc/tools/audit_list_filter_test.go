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
// Five properties are asserted, and all five are needed:
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
//     nothing is itself a finding;
//   - the enumerate-then-narrow ban is DERIVED from the method sets of this
//     service's declared authorization surfaces, not read off a hand-written list.
//     A list of names refuses only the forms someone already met — which is how a
//     third form lived in iam for months (#651) — so the injection here adds a
//     method to a real surface and calls it from a real listing, and the finding
//     must name both the call and the declaration it was derived from.
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

// treesRead are the directories under the SERVICE root the gate parses: BOTH
// transport packages (vpc is the only service with two, and the command audits
// both), and the declared enumeration source whose method set derives the ban.
var treesRead = []string{anchorDir, "internal/handler", "internal/check"}

// sharedTreesRead are the directories under the MODULE root the gate parses. The
// port through which every consumer service asks kaname the authorization
// question is shared foundation, not service code — so a copy holding only the
// service would make the derivation unresolvable, and every injection below would
// fail on that instead of on its own subject.
var sharedTreesRead = []string{"pkg/listnarrow"}

// moduleRoot returns the module root — the directory holding go.mod, pkg/ and
// services/. It is where a Shared enumeration source is resolved from.
func moduleRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(filepath.Dir(serviceRoot(t)))
}

// copyTree materialises one tracked tree into dst and returns how many files it
// wrote. The file set comes from the git INDEX (pkg/treecorpus), not from a
// disk walk: under services/ a walk also reads what the repository does not contain
// — agent working copies, generated directories, run reports.
func copyTree(t *testing.T, base, tree, dst string) int {
	t.Helper()
	paths, err := treecorpus.UnderWithSuffix(filepath.Join(base, tree), ".go")
	if err != nil {
		t.Fatalf("состав %s: %v", tree, err)
	}
	copied := 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, rerr := filepath.Rel(base, path)
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
	return copied
}

// copyTrees materialises a throwaway copy of everything the gate reads and returns
// the SERVICE root inside it.
//
// The copy reproduces the real layout — module root on top, services/<svc> beneath
// it, pkg/ beside it — because a Shared enumeration source is resolved from the
// module root, and the module root is found by walking up to go.mod. A flat copy
// would resolve nothing and the finding would be about the copy, not the injection.
func copyTrees(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	svc := filepath.Join(dst, "services", "vpc")
	copied := 0
	for _, tree := range treesRead {
		copied += copyTree(t, serviceRoot(t), tree, svc)
	}
	for _, tree := range sharedTreesRead {
		copied += copyTree(t, moduleRoot(t), tree, dst)
	}
	// The go.mod is what makes this copy a module: the gate walks up from --root to
	// find it, exactly as it does in the real tree.
	if err := os.WriteFile(filepath.Join(dst, "go.mod"),
		[]byte("module github.com/PRO-Robotech/kacho\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	// "Ноль прочитанного" обязано быть отличимо от "ноль находок": копия, собранная
	// из пустого состава, дала бы гейту пустое дерево, и КАЖДАЯ проба ниже упала бы
	// с сообщением не о своём предмете.
	if copied == 0 {
		t.Fatalf("состав пуст: скопировано 0 файлов из %v/%v — инъекции ниже утверждали бы "+
			"о дереве, которого нет", treesRead, sharedTreesRead)
	}
	return svc
}

// patch rewrites one file of the copied tree and requires the replacement to apply,
// so an injection that stopped modelling anything fails loudly instead of passing.
// base is the directory rel is relative to: the copied SERVICE root for the
// service's own files, its parent's parent for the shared foundation.
func patch(t *testing.T, base, rel, old, replacement string) {
	t.Helper()
	path := filepath.Join(base, rel)
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
	patch(t, root, anchorDir+"/network/list.go", filterCall,
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
	for _, want := range []string{
		"network.List", "cluster-scoped by declaration addresspool.List",
		// the ban is DERIVED, and the census says from what — so a run that derived
		// nothing is distinguishable from this one. Both profiles print it, because
		// both are audited against the same tree in the same run.
		"enumerate-then-narrow ban",
		"source internal/check.IAMCheckClient",
		"source pkg/listnarrow.AuthorizeClient",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("a passing run must report its census (missing %q)\n--- output ---\n%s", want, out)
		}
	}
	// The непокрытость this service printed until #684 must be GONE, not merely
	// unread: while that line stood, "the ban is the hand-written list only" was the
	// gate's own statement about itself — and it stood for BOTH profiles.
	if strings.Contains(out, "no enumeration source declared") {
		t.Errorf("the ban must be derived, not hand-written only\n--- output ---\n%s", out)
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
	root := copyTrees(t)
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
	root := copyTrees(t)
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
	root := copyTrees(t)
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
	root := copyTrees(t)
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
	patch(t, root, anchorDir+"/addresspool/handler.go", "func (h *Handler) ListAddresses(", "func (h *Handler) FetchAddresses(")
	patch(t, root, anchorDir+"/addresspool/public_handler.go", "func (h *PublicHandler) ListAddresses(", "func (h *PublicHandler) FetchAddresses(")
	return "addresspool.ListAddresses"
}

// TestAuditListFilter_DerivedBanRefusesAPageTakenFromAnEnumeration — the property
// #684 is about, injected in both flavours the service actually has.
//
// Neither injected call name is in the hand-written floor, so neither can be caught
// by it: the ONLY thing that can refuse them is the derivation reading the method
// set of a declared authorization surface. Each case therefore adds the method to
// that surface AND calls it from a real listing — which is exactly how the form
// would arrive in real life, and exactly what the floor could not see (#651).
func TestAuditListFilter_DerivedBanRefusesAPageTakenFromAnEnumeration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		widen      func(t *testing.T, root string)
		call       string
		wantCall   string
		wantSource string
	}{
		{
			// vpc's own client to kaname: "which networks may this subject see"
			// written one method along from "may this subject see this network".
			name: "own iam client",
			widen: func(t *testing.T, root string) {
				patch(t, root, "internal/check/check_client.go",
					"func (c *IAMCheckClient) Check(",
					"func (c *IAMCheckClient) AllowedNetworkIDs(ctx context.Context, subjectID string) ([]string, error) {\n"+
						"\treturn nil, nil\n}\n\nfunc (c *IAMCheckClient) Check(")
			},
			call:       "\tallowed, _ := u.checks.AllowedNetworkIDs(ctx, \"user:x\")\n\t_ = allowed\n",
			wantCall:   "reaches AllowedNetworkIDs",
			wantSource: "internal/check.IAMCheckClient",
		},
		{
			// the SHARED port to kaname's AuthorizeService — the surface that
			// already carries ListObjects on the iam side, so this is the shortest
			// path from "narrow the page" to "enumerate the universe".
			name: "shared authorize port",
			widen: func(t *testing.T, root string) {
				patch(t, filepath.Dir(filepath.Dir(root)), "pkg/listnarrow/narrower.go",
					"type AuthorizeClient interface {",
					"type AuthorizeClient interface {\n\tVisibleObjectIDs(ctx context.Context, subject, objectType string) ([]string, error)")
			},
			call:       "\tallowed, _ := u.narrower.VisibleObjectIDs(ctx, \"user:x\", \"vpc_network\")\n\t_ = allowed\n",
			wantCall:   "reaches VisibleObjectIDs",
			wantSource: "pkg/listnarrow.AuthorizeClient",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := copyTrees(t)
			tc.widen(t, root)
			patch(t, root, anchorDir+"/network/list.go",
				"\tnets, next, lerr := r.Networks().List(ctx, f, p)", tc.call+"\tnets, next, lerr := r.Networks().List(ctx, f, p)")

			out, err := runGate(t, root)
			if err == nil {
				t.Fatalf("a listing taking its page from an enumeration must be refused\n--- output ---\n%s", out)
			}
			if !strings.Contains(out, tc.wantCall) {
				t.Errorf("the finding must name the call (%s)\n--- output ---\n%s", tc.wantCall, out)
			}
			if !strings.Contains(out, "network") {
				t.Errorf("the finding must carry the coordinate\n--- output ---\n%s", out)
			}
			if !strings.Contains(out, tc.wantSource) {
				t.Errorf("a DERIVED ban must name the declaration it came from (%s)\n--- output ---\n%s", tc.wantSource, out)
			}
		})
	}
}

// TestAuditListFilter_EnumerationSourceExpiresWithItsSubject — a source that moved
// is a finding, not a quiet loss of the ban it derives. Without this the profile
// could point at a renamed type, derive nothing, and the run would look exactly like
// a clean one — and here it would do so TWICE, once per profile.
func TestAuditListFilter_EnumerationSourceExpiresWithItsSubject(t *testing.T) {
	root := copyTrees(t)
	patch(t, root, "internal/check/check_client.go", "IAMCheckClient", "IAMCheckClientMoved")

	out, err := runGate(t, root)
	if err == nil {
		t.Fatalf("a source that no longer resolves must be a finding\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "nothing left to describe") {
		t.Errorf("the finding must say the entry has nothing left to describe\n--- output ---\n%s", out)
	}
}
