// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditlistfilter is the CI gate for public List<Resource>: it asserts
// project-scoped listauthz posture (INV-10) AND per-object visibility filtering,
// parity with vpc/compute/nlb.
//
// # What is checked
//
// For every public project-scoped List the gate requires all three of:
//
//  1. the body of the repo adapter's List narrows rows by `project_id`
//     (listauthz posture). Scoped to the List body, never the file: a
//     `project_id = $` predicate surviving in Insert/Get must not vouch for a
//     List that dropped its own;
//  2. the use-case List rejects an empty `projectId` (in-service backstop —
//     the repo narrows only when ProjectID != "", so an empty scope would
//     return rows of EVERY project). A use-case that cannot be found is
//     treated fail-closed: an unprovable backstop is a finding;
//  3. the body of that same use-case List runs the page it just read through a
//     per-object visibility filter (authzfilter.FilterVisiblePage /
//     FilterVisibleIDs → kacho-iam AuthorizeService.BatchCheck).
//
// (1)+(2) are project scope. They answer "whose project is this", never "may
// this caller see THESE objects" — without (3) every project member sees every
// row of the project, which is precisely how this service shipped its List.
//
// Shape (3) must be "read a page by cursor → batch-check its ids". The inverse —
// ListAllowedIDs/ListObjects ("enumerate everything the subject may see", then
// narrow the SQL to that set) — is rejected explicitly: OpenFGA caps that
// enumeration server-side with no continuation token, so a tenant's own resource
// silently falls outside the prefix and becomes invisible forever. The shape is
// gone from every sibling service; the gate names it so it cannot return "as an
// equivalent".
//
// # Why this is an AST walk and not a grep
//
// The gate this replaced recognised a resource by the literal text
// `func (r *…Repo) List(`, i.e. by what the RECEIVER VARIABLE was NAMED. Renaming
// `r` to `repo` — a refactor no reviewer would stop — removed the resource from
// the gate's view entirely, and whatever its use-case did then went unjudged
// while the gate printed OK. Identification here is by what the declaration IS:
// a method named exactly `List` on a receiver whose TYPE is named `<X>Repo`.
// `ListOps`/`ListAttachments` are different methods and are not confused with it.
//
// Two further things follow from parsing rather than grepping, and both were
// previously handled by hand: comments are not code (a comment naming
// FilterVisibleIDs can never satisfy check (3), and one explaining why
// ListObjects is banned can never trip (3a)), and the use-case List is found
// anywhere in its package — splitting a package into list.go/create.go is
// routine and must neither hide a leak nor manufacture a false red.
//
// # Census
//
// Audit reports what it examined — adapter files parsed, resources discovered,
// checked and whitelisted — and treats "nothing examined" as a finding. A gate
// pointed at the wrong tree, or at a tree whose adapters moved, must not be
// indistinguishable from a clean one.
//
// # Whitelist
//
// A cluster-catalog resource (project scope and per-object grants inapplicable
// by design — disk_type is the `{cluster,*}` viewer reference data) is excluded
// with --allow=<resource>. An entry matching no discovered resource is itself a
// finding: an exclusion lives only while it has a subject, otherwise the next
// resource to inherit that name inherits the blind spot in silence.
package auditlistfilter

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	// adapterRoot holds the pg repo adapters, relative to the service root.
	adapterRoot = "internal/repo/pg"
	// useCaseRoot holds the use-case packages, relative to the service root.
	useCaseRoot = "internal/apps/kacho/api"
)

// projectNarrowRe matches a `project_id = $…` predicate, with or without a table
// alias (`v.project_id = $1`). It is applied ONLY to string literals found inside
// the List body, so neither a comment nor a predicate in a sibling method counts.
var projectNarrowRe = regexp.MustCompile(`(?:[A-Za-z_][A-Za-z0-9_]*\.)?project_id\s*=\s*\$`)

// perObjectFilters are the accepted per-object visibility calls (check 3).
var perObjectFilters = map[string]bool{
	"FilterVisiblePage": true,
	"FilterVisibleIDs":  true,
}

// enumerateThenNarrow are the rejected enumerate-all-allowed-ids calls (check 3a).
var enumerateThenNarrow = map[string]bool{
	"ListAllowedIDs": true,
	"ListObjects":    true,
}

// Options configures one audit run.
type Options struct {
	// Root is the service root (the directory holding internal/…).
	Root string
	// Allow lists resources excluded from the checks (cluster catalogs).
	Allow []string
}

// Report is the census of one audit run: what was examined, and what was found.
type Report struct {
	AdapterFiles int      // adapter files parsed under internal/repo/pg
	Resources    []string // resources discovered (snake_case), sorted
	Checked      []string // resources actually judged
	Whitelisted  []string // resources skipped because they are whitelisted
	Findings     []string // one line per finding; non-empty ⇒ the gate fails
}

// listMethod is a public List declaration located by its receiver TYPE.
type listMethod struct {
	file string
	fn   *ast.FuncDecl
}

// Audit runs the gate against o.Root, writes its census and findings to out, and
// returns an error when the tree does not satisfy the contract. "Nothing
// examined" is an error too — see the package comment.
func Audit(o Options, out io.Writer) (Report, error) {
	rep := Report{}
	root := o.Root
	if root == "" {
		root = "."
	}

	adapters, files, err := findRepoLists(filepath.Join(root, adapterRoot))
	rep.AdapterFiles = files
	if err != nil {
		rep.Findings = append(rep.Findings, err.Error())
		return finish(rep, out)
	}

	for res := range adapters {
		rep.Resources = append(rep.Resources, res)
	}
	sort.Strings(rep.Resources)

	if len(rep.Resources) == 0 {
		rep.Findings = append(rep.Findings, fmt.Sprintf(
			"no public List adapter found under %s (parsed %d file(s)) — the gate examined nothing, "+
				"so it proved nothing; zero findings must not be reachable from zero reads",
			filepath.Join(root, adapterRoot), files))
		return finish(rep, out)
	}

	allowed := map[string]bool{}
	for _, a := range o.Allow {
		allowed[a] = true
	}
	// An exclusion lives only while it has a subject.
	for _, a := range sortedKeys(allowed) {
		if !contains(rep.Resources, a) {
			rep.Findings = append(rep.Findings, fmt.Sprintf(
				"whitelist entry %q matches no resource under %s — the exclusion has nothing left to "+
					"exclude; drop it, or the next resource of that name inherits the blind spot silently",
				a, filepath.Join(root, adapterRoot)))
		}
	}

	for _, res := range rep.Resources {
		if allowed[res] {
			rep.Whitelisted = append(rep.Whitelisted, res)
			continue
		}
		rep.Checked = append(rep.Checked, res)
		rep.Findings = append(rep.Findings, checkResource(root, res, adapters[res])...)
	}

	return finish(rep, out)
}

// checkResource applies checks (1)…(3a) to one resource.
func checkResource(root, res string, lm listMethod) []string {
	var findings []string

	// (1) the repo List body must narrow by project_id.
	if !narrowsByProject(lm.fn) {
		return append(findings, fmt.Sprintf(
			"%s — repo.List body does not narrow by project_id\n  repo: %s", res, lm.file))
	}

	// (2)+(3) live in the use-case package. Absence is fail-closed: a backstop
	// that cannot be located cannot be vouched for.
	ucDir := filepath.Join(root, useCaseRoot, packageDirFor(res))
	uc, err := findUseCaseList(ucDir)
	if err != nil {
		return append(findings, fmt.Sprintf("%s — %v (cannot prove the projectId backstop; fail-closed)", res, err))
	}

	if !rejectsEmptyProjectID(uc.fn) {
		return append(findings, fmt.Sprintf(
			"%s — use-case List does not reject empty projectId\n  service: %s", res, uc.file))
	}

	if !callsAnyOf(uc.fn, perObjectFilters) {
		return append(findings, fmt.Sprintf(
			"%s — use-case List does not filter the page per-object (FilterVisiblePage/FilterVisibleIDs)\n  service: %s",
			res, uc.file))
	}

	// (3a) enumerate-then-narrow is not a substitute for a per-page batch check.
	if callsAnyOf(uc.fn, enumerateThenNarrow) {
		findings = append(findings, fmt.Sprintf(
			"%s — use-case List enumerates allowed ids (ListAllowedIDs/ListObjects) instead of batch-checking the page\n  service: %s",
			res, uc.file))
	}

	return findings
}

// findRepoLists parses every non-test .go file under dir and returns the public
// List method of each `<X>Repo` receiver type, keyed by snake_case resource name,
// plus the number of files parsed.
func findRepoLists(dir string) (map[string]listMethod, int, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, 0, fmt.Errorf("adapter root %s is absent — the gate examined nothing, so it proved "+
			"nothing (a gate pointed at the wrong tree must not be indistinguishable from a clean one)", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", dir, err)
	}

	out := map[string]listMethod{}
	parsed := 0
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil, parsed, fmt.Errorf("parse %s: %w", path, perr)
		}
		parsed++
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "List" || fn.Body == nil {
				continue
			}
			recv := receiverTypeName(fn)
			if !strings.HasSuffix(recv, "Repo") || recv == "Repo" {
				continue
			}
			out[toSnake(strings.TrimSuffix(recv, "Repo"))] = listMethod{file: path, fn: fn}
		}
	}
	return out, parsed, nil
}

// findUseCaseList locates `func (*UseCase) List` anywhere in the package at dir.
func findUseCaseList(dir string) (listMethod, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return listMethod{}, fmt.Errorf("use-case package %s absent", dir)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return listMethod{}, fmt.Errorf("parse %s: %w", path, perr)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "List" || fn.Body == nil {
				continue
			}
			if receiverTypeName(fn) == "UseCase" {
				return listMethod{file: path, fn: fn}, nil
			}
		}
	}
	return listMethod{}, fmt.Errorf("use-case package %s declares no (*UseCase).List", dir)
}

// receiverTypeName returns the bare type name of fn's receiver ("VolumeRepo" for
// both `func (r *VolumeRepo)` and `func (*VolumeRepo)`), or "" when fn is not a
// method. The receiver's VARIABLE name is deliberately never consulted.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if idx, ok := expr.(*ast.IndexExpr); ok { // generic receiver Repo[T]
		expr = idx.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// narrowsByProject reports whether any string literal inside fn's body carries a
// `project_id = $` predicate. Literals only: SQL lives in strings, prose does not.
func narrowsByProject(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			s = lit.Value
		}
		if projectNarrowRe.MatchString(s) {
			found = true
			return false
		}
		return true
	})
	return found
}

// rejectsEmptyProjectID reports whether fn's body compares a .ProjectID selector
// against the empty string, in either operand order.
func rejectsEmptyProjectID(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || bin.Op != token.EQL {
			return true
		}
		if isProjectIDSelector(bin.X) && isEmptyString(bin.Y) ||
			isProjectIDSelector(bin.Y) && isEmptyString(bin.X) {
			found = true
			return false
		}
		return true
	})
	return found
}

func isProjectIDSelector(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "ProjectID"
}

func isEmptyString(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s == ""
}

// callsAnyOf reports whether fn's body CALLS a function whose name is in names.
// A call node is required, so naming one in a comment or a string can never
// satisfy — nor trip — a check.
func callsAnyOf(fn *ast.FuncDecl, names map[string]bool) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch f := call.Fun.(type) {
		case *ast.SelectorExpr:
			if names[f.Sel.Name] {
				found = true
				return false
			}
		case *ast.Ident:
			if names[f.Name] {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// packageDirFor maps a snake_case resource to its use-case package directory
// (disk_type → disktype), matching the layout of internal/apps/kacho/api.
func packageDirFor(res string) string { return strings.ReplaceAll(res, "_", "") }

// toSnake converts an exported Go type stem to snake_case (DiskType → disk_type).
func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// finish prints the census, then the findings, and turns findings into an error.
func finish(rep Report, out io.Writer) (Report, error) {
	_, _ = fmt.Fprintf(out, "audit-list-filter: examined %d adapter file(s), %d resource(s) (%d checked, %d whitelisted)\n",
		rep.AdapterFiles, len(rep.Resources), len(rep.Checked), len(rep.Whitelisted))
	if len(rep.Checked) > 0 {
		_, _ = fmt.Fprintf(out, "audit-list-filter: checked %s\n", strings.Join(rep.Checked, ", "))
	}
	if len(rep.Whitelisted) > 0 {
		_, _ = fmt.Fprintf(out, "audit-list-filter: whitelisted %s\n", strings.Join(rep.Whitelisted, ", "))
	}
	if len(rep.Findings) == 0 {
		_, _ = fmt.Fprintln(out, "audit-list-filter: OK")
		return rep, nil
	}
	for _, f := range rep.Findings {
		_, _ = fmt.Fprintf(out, "audit-list-filter: %s\n", f)
	}
	_, _ = fmt.Fprint(out, explanation)
	return rep, fmt.Errorf("audit-list-filter: %d finding(s)", len(rep.Findings))
}

const explanation = `
Every public project-scoped List<Resource> must:
  (1) narrow rows by project_id in the repo.List body;
  (2) reject an empty projectId in the use-case List (in-service backstop);
  (3) filter the page it just read per-object via FilterVisiblePage/
      FilterVisibleIDs (kacho-iam BatchCheck, on the same relation Get enforces).
(1)+(2) are project scope — they do NOT answer 'may this caller see THESE
objects'; without (3) every project member sees every row of the project.
Enumerating all allowed ids (ListAllowedIDs/ListObjects) is not a substitute.
Whitelist a cluster-catalog resource with --allow=<resource> if the
cluster-wide surface is intentional — and drop the entry once the resource is
gone, since an exclusion with no subject only hides its successor.
`
