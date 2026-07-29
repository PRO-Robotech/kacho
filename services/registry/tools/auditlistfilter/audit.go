// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditlistfilter is the listauthz gate of kacho-registry: a public List
// must hand back only the objects its caller is allowed to see.
//
// # Why this gate is written differently from the one in kacho-storage
//
// The invariant is the same everywhere; the place it is enforced is not. In storage
// (and compute/nlb/vpc) a List is project-scoped: the repository narrows rows by
// project_id and the use-case then runs the page it just read through a batched
// per-object check. The gate there reads internal/repo/pg + the use-case packages
// and asserts exactly that; it is now an AST walk too
// (services/storage/tools/auditlistfilter), because its earlier grep recognised a
// resource by the name of a receiver VARIABLE and stopped seeing the resource the
// moment someone renamed it.
//
// kacho-registry does not have that shape, and pointing storage's gate at it would
// have passed vacuously — the directories it reads do not exist here. Registry
// authorizes in the HANDLER, because its List surface is not one uniform
// project-scoped collection:
//
//   - the objects that carry grants are registry_registry and registry_repository —
//     a repository is not project-scoped, it belongs to a registry;
//   - tags and referrers are not grantable objects at all, they live inside one
//     repository, so a page of them is settled by a single gate on that repository;
//   - repositories are a union of a durable overlay and a live projection out of
//     zot, so there is no SQL row to narrow in the first place;
//   - the gateway leaves these RPCs `<exempt>` (authN only) precisely because a
//     single per-RPC check cannot express a row filter, so the whole decision is
//     the service's to make, in the handler.
//
// So the two enforcement shapes this gate knows about are:
//
//	rowFilter  — the page holds separately-authorizable objects, so every row must
//	             be checked and the response must be built from the FILTERED slice;
//	objectGate — the page lives inside one authorizable object, so that object must
//	             be checked, its verdict acted on, and the check must happen BEFORE
//	             the page is read.
//
// # What the gate refuses to accept
//
//   - a List RPC with no declared enforcement (a new one must not ship unnoticed);
//   - a declared entry whose handler no longer exists (a rule that quietly stopped
//     applying);
//   - a filter that is called and whose result is not what the caller receives;
//   - a gate whose verdict is discarded, or that runs after the read;
//   - "enumerate everything the subject may see, then narrow to it": that
//     enumeration is capped server-side with no continuation token, so a tenant's
//     own repository silently falls outside the prefix and becomes invisible;
//   - a filter that returns rows alongside an authorization error — that hands out
//     the very page the check could not vouch for;
//   - a composition root that builds the handler without an authorizer, which turns
//     every filter above into a no-op.
//
// Judgement is made on the AST, never on text: the parser is asked NOT to keep
// comments, so a comment naming a filter cannot satisfy a rule about calling one.
// Both directions are locked by fixtures in audit_test.go.
package auditlistfilter

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// shape is how a List RPC is expected to enforce visibility.
type shape int

const (
	// rowFilter — the page holds separately-authorizable objects; each row is
	// checked and the response is built from the filtered slice.
	rowFilter shape = iota
	// objectGate — the page lives inside a single authorizable object; that object
	// is checked before the page is read.
	objectGate
)

// rule is the enforcement declared for one handler method.
type rule struct {
	shape  shape
	helper string // repoAuthz method that must carry it out
	object string // what the caller is checked against, for the finding text
}

// enforcement declares, per List handler, how visibility is decided. It is
// deliberately explicit: adding a List RPC means stating here what the caller is
// checked against, and until that is stated the gate fails closed.
var enforcement = map[string]rule{
	"RegistryHandler.List":             {rowFilter, "filterRegistries", "registry_registry per row"},
	"RegistryHandler.ListRepositories": {rowFilter, "filterRepos", "registry_repository per row"},
	"RegistryHandler.ListOperations":   {rowFilter, "filterOperations", "registry_repository of each repo-scoped operation"},
	"RegistryHandler.ListTags":         {objectGate, "checkRepo", "registry_repository holding the tags"},
	"RegistryHandler.ListReferrers":    {objectGate, "checkRepository", "registry_repository holding the referrers"},
}

// bannedEnumeration are the call shapes that ask the authorization store to list
// everything a subject may see. The answer is capped server-side and has no
// continuation token, so narrowing to it makes a tenant's own resources invisible
// past the cap — permanently, and silently.
var bannedEnumeration = map[string]bool{
	"ListAllowedIDs": true,
	"ListObjects":    true,
}

// authzField is the handler field holding the per-object authorizer.
const authzField = "authz"

// Analyze inspects the kacho-registry tree rooted at serviceRoot and returns one
// finding per violation, sorted. An empty result means the invariant holds.
func Analyze(serviceRoot string) ([]string, error) {
	fset := token.NewFileSet()

	handlerDir := filepath.Join(serviceRoot, "internal", "handler")
	handlerFiles, err := parseDir(fset, handlerDir)
	if err != nil {
		return nil, err
	}

	var findings []string
	listMethods, authzMethods := collectMethods(handlerFiles)

	findings = append(findings, checkSurfaceIsDeclared(listMethods)...)
	findings = append(findings, checkHelpersExist(authzMethods)...)

	for name, fd := range listMethods {
		r, declared := enforcement[name]
		if !declared {
			continue // already reported by checkSurfaceIsDeclared
		}
		findings = append(findings, checkListMethod(fset, name, fd, r)...)
	}

	findings = append(findings, checkFiltersFailClosed(authzMethods)...)

	wiring, err := checkHandlerIsWired(fset, filepath.Join(serviceRoot, "cmd"))
	if err != nil {
		return nil, err
	}
	findings = append(findings, wiring...)

	sort.Strings(findings)
	return findings, nil
}

// parseDir parses every non-test .go file directly inside dir. Comments are NOT
// retained: the gate must judge code, never prose about code.
func parseDir(fset *token.FileSet, dir string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", name, perr)
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no Go sources under %s — nothing was inspected, so nothing can be vouched for", dir)
	}
	return out, nil
}

// collectMethods returns the List-shaped handler methods keyed "<Receiver>.<Method>",
// and the methods of repoAuthz keyed by their own name.
func collectMethods(files []*ast.File) (list map[string]*ast.FuncDecl, authz map[string]*ast.FuncDecl) {
	list = map[string]*ast.FuncDecl{}
	authz = map[string]*ast.FuncDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Body == nil {
				continue
			}
			recv := receiverTypeName(fd)
			switch {
			case recv == "repoAuthz":
				authz[fd.Name.Name] = fd
			case strings.HasPrefix(fd.Name.Name, "List"):
				list[recv+"."+fd.Name.Name] = fd
			}
		}
	}
	return list, authz
}

// receiverTypeName returns the receiver's type name, with any pointer star removed.
func receiverTypeName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	expr := fd.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// checkSurfaceIsDeclared reports List handlers with no declared enforcement, and
// declarations whose handler is gone. Both directions matter: the first is a new RPC
// shipping unreviewed, the second is a rule that silently stopped applying.
func checkSurfaceIsDeclared(list map[string]*ast.FuncDecl) []string {
	var out []string
	for name := range list {
		if _, ok := enforcement[name]; !ok {
			out = append(out, fmt.Sprintf(
				"%s: a List RPC with no declared listauthz enforcement — declare in "+
					"tools/auditlistfilter what its caller is checked against before it ships",
				name))
		}
	}
	for name := range enforcement {
		if _, ok := list[name]; !ok {
			out = append(out, fmt.Sprintf(
				"%s: listauthz enforcement is declared for a handler method that does not "+
					"exist — the rule stopped applying without anyone noticing",
				name))
		}
	}
	return out
}

// checkHelpersExist reports declared helpers that repoAuthz does not implement — a
// rename would otherwise leave every rule pointing at nothing.
func checkHelpersExist(authz map[string]*ast.FuncDecl) []string {
	seen := map[string]bool{}
	var out []string
	for name, r := range enforcement {
		if seen[r.helper] {
			continue
		}
		seen[r.helper] = true
		if _, ok := authz[r.helper]; !ok {
			out = append(out, fmt.Sprintf(
				"repoAuthz.%s: declared as the authorization helper for %s, but no such "+
					"method exists", r.helper, name))
		}
	}
	return out
}

// checkListMethod applies the declared enforcement to one handler method.
func checkListMethod(fset *token.FileSet, name string, fd *ast.FuncDecl, r rule) []string {
	out := checkNoEnumeration(name, fd)
	switch r.shape {
	case rowFilter:
		return append(out, checkRowFilter(name, fd, r)...)
	case objectGate:
		return append(out, checkObjectGate(fset, name, fd, r)...)
	}
	return out
}

// checkNoEnumeration rejects "list everything the subject may see, then narrow".
func checkNoEnumeration(name string, fd *ast.FuncDecl) []string {
	var out []string
	for _, call := range callsNamed(fd.Body, bannedEnumeration) {
		out = append(out, fmt.Sprintf(
			"%s: calls %s — enumerating every allowed id is capped server-side with no "+
				"continuation token, so the caller's own objects fall outside the cap and "+
				"disappear; check the page that was read instead",
			name, call))
	}
	return out
}

// checkRowFilter asserts the page is filtered per object AND that the filtered slice
// is the one the response is built from.
func checkRowFilter(name string, fd *ast.FuncDecl, r rule) []string {
	assigns := authzCallAssignments(fd.Body, r.helper)
	if len(assigns) == 0 {
		return []string{fmt.Sprintf(
			"%s: the page is returned without a per-object visibility filter — it must "+
				"pass through h.%s.%s (%s)",
			name, authzField, r.helper, r.object)}
	}

	ranged := rangedIdents(fd.Body)
	var out []string
	for _, as := range assigns {
		result := assignedName(as)
		if result == "" {
			out = append(out, fmt.Sprintf(
				"%s: the result of h.%s.%s is discarded, so the unfiltered page is what "+
					"the caller receives", name, authzField, r.helper))
			continue
		}
		if !ranged[result] {
			out = append(out, fmt.Sprintf(
				"%s: h.%s.%s runs but its result %q is never the collection the response "+
					"is built from — the unfiltered page is what the caller receives",
				name, authzField, r.helper, result))
		}
		for _, in := range identArgs(as.Rhs[0].(*ast.CallExpr)) {
			if ranged[in] {
				out = append(out, fmt.Sprintf(
					"%s: the response is built from %q, the unfiltered page handed TO "+
						"h.%s.%s — build it from the filtered result",
					name, in, authzField, r.helper))
			}
		}
	}
	return out
}

// checkObjectGate asserts the enclosing object is checked, that the verdict is acted
// on, and that the check precedes the read.
func checkObjectGate(fset *token.FileSet, name string, fd *ast.FuncDecl, r rule) []string {
	assigns := authzCallAssignments(fd.Body, r.helper)
	bare := bareAuthzCalls(fd.Body, r.helper)
	if len(assigns) == 0 && len(bare) == 0 {
		return []string{fmt.Sprintf(
			"%s: the page is returned without checking the object that holds it — it must "+
				"pass through h.%s.%s (%s)",
			name, authzField, r.helper, r.object)}
	}

	var out []string
	checked := map[string]bool{}
	for _, id := range errorTestedIdents(fd.Body) {
		checked[id] = true
	}

	gatePos := token.NoPos
	acted := false
	for _, as := range assigns {
		if pos := as.Pos(); gatePos == token.NoPos || pos < gatePos {
			gatePos = pos
		}
		if n := assignedName(as); n != "" && checked[n] {
			acted = true
		}
	}
	for _, call := range bare {
		if pos := call.Pos(); gatePos == token.NoPos || pos < gatePos {
			gatePos = pos
		}
	}
	if !acted {
		out = append(out, fmt.Sprintf(
			"%s: h.%s.%s is called but its verdict is never tested — a denial does not "+
				"stop the response", name, authzField, r.helper))
	}

	if readPos := firstUseCaseCall(fd.Body); readPos != token.NoPos && gatePos != token.NoPos && gatePos > readPos {
		out = append(out, fmt.Sprintf(
			"%s: h.%s.%s runs at %s, after the use-case has already read the page at %s — "+
				"the check must happen before the read",
			name, authzField, r.helper,
			fset.Position(gatePos).String(), fset.Position(readPos).String()))
	}
	return out
}

// checkFiltersFailClosed asserts no repoAuthz filter ever returns rows together with
// an error: a page the check could not vouch for must not reach the caller.
func checkFiltersFailClosed(authz map[string]*ast.FuncDecl) []string {
	var out []string
	for name, fd := range authz {
		if !strings.HasPrefix(name, "filter") {
			continue
		}
		walkSkippingClosures(fd.Body, func(n ast.Node) {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 2 {
				return
			}
			if isNil(ret.Results[1]) || isNil(ret.Results[0]) {
				return
			}
			out = append(out, fmt.Sprintf(
				"repoAuthz.%s: returns rows together with an error — an authorization "+
					"failure must yield no rows at all, not the page the check could not "+
					"vouch for", name))
		})
	}
	return out
}

// checkHandlerIsWired asserts the composition root builds the public handler with a
// real authorizer. A nil one is breakglass: every filter above becomes a no-op.
func checkHandlerIsWired(fset *token.FileSet, cmdDir string) ([]string, error) {
	var files []*ast.File
	err := filepath.WalkDir(cmdDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", path, perr)
		}
		files = append(files, f)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go sources under %s — the composition root could not be inspected", cmdDir)
	}

	var out []string
	found := false
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "NewRegistryHandler" {
				return true
			}
			found = true
			if len(call.Args) >= 2 && isNil(call.Args[1]) {
				out = append(out, fmt.Sprintf(
					"%s: NewRegistryHandler is wired with a nil authorizer — that is "+
						"breakglass, and it turns every per-object List filter into a no-op",
					fset.Position(call.Pos()).String()))
			}
			return true
		})
	}
	if !found {
		out = append(out, fmt.Sprintf(
			"%s: no call to NewRegistryHandler — the public handler, and with it every "+
				"List filter, is never wired", cmdDir))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// AST helpers
// ---------------------------------------------------------------------------

// isAuthzCall reports whether call is `<x>.authz.<helper>(…)`.
func isAuthzCall(call *ast.CallExpr, helper string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != helper {
		return false
	}
	inner, ok := sel.X.(*ast.SelectorExpr)
	return ok && inner.Sel.Name == authzField
}

// authzCallAssignments returns the assignments whose sole right-hand side is a call
// to the given authz helper.
func authzCallAssignments(body *ast.BlockStmt, helper string) []*ast.AssignStmt {
	var out []*ast.AssignStmt
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		if call, ok := as.Rhs[0].(*ast.CallExpr); ok && isAuthzCall(call, helper) {
			out = append(out, as)
		}
		return true
	})
	return out
}

// bareAuthzCalls returns calls to the helper made as plain statements (result not
// bound at all).
func bareAuthzCalls(body *ast.BlockStmt, helper string) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		es, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		if call, ok := es.X.(*ast.CallExpr); ok && isAuthzCall(call, helper) {
			out = append(out, call)
		}
		return true
	})
	return out
}

// assignedName returns the first non-blank identifier an assignment binds, or "".
func assignedName(as *ast.AssignStmt) string {
	for _, lhs := range as.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		return id.Name
	}
	return ""
}

// identArgs returns the identifier arguments of a call, minus the context.
func identArgs(call *ast.CallExpr) []string {
	var out []string
	for _, a := range call.Args {
		if id, ok := a.(*ast.Ident); ok && id.Name != "ctx" {
			out = append(out, id.Name)
		}
	}
	return out
}

// rangedIdents returns the identifiers a `for … range` in the body iterates over.
func rangedIdents(body *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		rs, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		if id, ok := rs.X.(*ast.Ident); ok {
			out[id.Name] = true
		}
		return true
	})
	return out
}

// errorTestedIdents returns identifiers compared against nil somewhere in the body —
// the shape of acting on a verdict, in both the `if err := f(); err != nil` and the
// `err := f()` … `if err != nil` spellings.
func errorTestedIdents(body *ast.BlockStmt) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || bin.Op != token.NEQ {
			return true
		}
		id, ok := bin.X.(*ast.Ident)
		if ok && isNil(bin.Y) {
			out = append(out, id.Name)
		}
		return true
	})
	return out
}

// firstUseCaseCall returns the position of the earliest `<x>.uc.<Method>(…)` call.
func firstUseCaseCall(body *ast.BlockStmt) token.Pos {
	pos := token.NoPos
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		inner, ok := sel.X.(*ast.SelectorExpr)
		if !ok || inner.Sel.Name != "uc" {
			return true
		}
		if pos == token.NoPos || call.Pos() < pos {
			pos = call.Pos()
		}
		return true
	})
	return pos
}

// walkSkippingClosures visits body but does not descend into function literals, so a
// goroutine's own `return err` is not mistaken for the enclosing method's result.
func walkSkippingClosures(body *ast.BlockStmt, visit func(ast.Node)) {
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if n != nil {
			visit(n)
		}
		return true
	})
}

// isNil reports whether an expression is the identifier nil.
func isNil(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

// callsNamed returns the names of called selectors whose final name is in want.
func callsNamed(body *ast.BlockStmt, want map[string]bool) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && want[sel.Sel.Name] {
			out = append(out, sel.Sel.Name)
		}
		return true
	})
	return out
}
