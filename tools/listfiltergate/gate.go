// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package listfiltergate is the analyser behind `make -C services/<svc>
// audit-list-filter` for the three services whose public List is a project-scoped
// collection narrowed per object: kacho-compute, kacho-nlb and kacho-vpc.
//
// # What it asserts
//
// Every public `List<Resource>` RPC must hand back only the rows its caller may
// see. The page is read from the service's own DB first (cursor, project-scoped)
// and only then run through a batched per-object check against kacho-iam
// (`AuthorizeService.BatchCheck`, relation viewer ∪ v_list). So for each resource
// the gate requires that the call graph rooted at the List transport declaration
// reaches one of that service's accepted filter calls.
//
// The inverse shape — "enumerate every id the subject may see, then narrow the SQL
// to that set" — is refused explicitly. That enumeration is capped server-side with
// no continuation token, so a tenant's own row silently falls outside the prefix
// and becomes invisible for good. The shape is gone from all three services; the
// gate names it so it cannot come back "as an equivalent".
//
// # Why this parses instead of grepping
//
// The gates this replaced recognised a resource by a MUTABLE LABEL: compute by the
// receiver VARIABLE's name (`func (h *…Handler) List(`), nlb by a file being called
// list.go, vpc by a file being called handler.go. Every one of those is changed by
// a refactor no reviewer would stop — rename the receiver, split a package, rename
// a file — and the resource then left the gate's view entirely: whatever its List
// did afterwards went unjudged while the gate printed OK.
//
// Identification here is by what the declaration IS: a method named exactly `List`
// on the transport TYPE (Profile.ReceiverSuffix). Two further properties follow
// from parsing rather than searching text, and both were live defects:
//
//   - comments are not code. A comment naming the filter cannot satisfy the check,
//     and a comment explaining why enumeration is banned cannot trip it. Deleting a
//     call and leaving the sentence that described it is the ordinary shape of a
//     deletion, and text search cannot tell the two apart;
//   - the judgement is scoped to what List actually CALLS, not to the file it sits
//     in. A filter-typed struct field, or a filtered sibling method, must not vouch
//     for a List that dropped its own. This is not hypothetical: compute's gate
//     reported OK for a List with its filter removed, because the token it searched
//     for survived in the handler field's type.
//
// # Census
//
// The gate reports what it examined — files parsed, packages, resources
// discovered, checked and whitelisted — on every path, and treats "nothing
// examined" as a finding. A gate pointed at the wrong tree, or at a tree whose
// packages moved, must not be indistinguishable from a clean one: "zero findings"
// has to be unreachable from "zero read".
//
// # Whitelist
//
// A cluster-catalog resource — project scope and per-object grants inapplicable by
// design, as with compute's machine_type and vpc's addresspool — is excluded with
// --allow=<resource>. An entry matching no discovered resource is itself a finding:
// an exclusion lives only while it has a subject, otherwise the next resource to
// inherit that name inherits the blind spot in silence.
//
// # Relationship to the other two gates of this class
//
// kacho-storage and kacho-registry own bespoke analysers of the same class
// (services/{storage,registry}/tools/auditlistfilter) and are NOT driven from here.
// Storage asserts more than this package does — its repo adapter must narrow by
// project_id and its use-case must reject an empty projectId — because its layout
// puts both within reach of one walk. Registry's List surface is not project-scoped
// at all (grants live on the registry and the repository; tags are not grantable
// objects), so it declares a per-RPC enforcement shape instead. What is shared
// across all five is the discipline, not the code.
package listfiltergate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Profile describes one service's layout: where the public List declarations live,
// how one resource is told apart from its neighbours, and which call proves that
// the page was narrowed per object.
//
// Every field below names a directory or a DECLARED type/function — never a naming
// habit — so renaming a variable or a file cannot change what the gate sees.
type Profile struct {
	// Service is the service directory name, used in messages.
	Service string

	// AnchorRoot holds the public List declarations, relative to the service root.
	AnchorRoot string

	// PerPackage selects how one resource is delimited:
	//
	//   false — AnchorRoot is ONE package and resources are told apart by the
	//           receiver TYPE (compute: InstanceHandler and MachineTypeHandler in
	//           internal/handler). Resource name = snake_case of the type's stem.
	//   true  — each immediate subdirectory of AnchorRoot is one resource package
	//           whose transport type carries the same name everywhere (vpc and nlb
	//           both call it `Handler`). Resource name = the directory.
	PerPackage bool

	// ReceiverSuffix is the transport type's exact name (when PerPackage) or its
	// suffix (otherwise). It is a declared TYPE: `func (h *X) List` and
	// `func (handler *X) List` are the same declaration to this gate.
	ReceiverSuffix string

	// Filters are the call names that prove the page was narrowed per object.
	Filters []string

	// Banned are the call names that ask the authorization store to enumerate
	// everything a subject may see.
	Banned []string
}

// Options configures one audit run.
type Options struct {
	// Root is the service root (the directory holding internal/…).
	Root string
	// Allow lists resources excluded from the check (cluster catalogs).
	Allow []string
}

// Report is the census plus the findings of one run.
type Report struct {
	Files       int      // non-test .go files parsed under AnchorRoot
	Packages    int      // packages parsed (1 when Profile.PerPackage is false)
	Resources   []string // resources discovered, sorted
	Checked     []string // resources actually judged
	Whitelisted []string // resources skipped because whitelisted
	// Unattributed are public List declarations that resolved to no resource —
	// each one is also a finding. Kept in the census so a passing run can state
	// that the number is zero, rather than leaving it unsaid.
	Unattributed []string
	Findings     []string // one line per finding; non-empty ⇒ the gate fails
}

// Audit runs the gate against o.Root, writes its census and findings to out, and
// returns an error when the tree does not satisfy the contract. "Nothing examined"
// is an error too — see the package comment.
func Audit(p Profile, o Options, out io.Writer) (Report, error) {
	rep := Report{}
	root := o.Root
	if root == "" {
		root = "."
	}
	anchor := filepath.Join(root, p.AnchorRoot)

	units, err := loadUnits(p, anchor)
	for _, u := range units {
		rep.Files += len(u.files)
	}
	rep.Packages = len(units)
	if err != nil {
		rep.Findings = append(rep.Findings, err.Error())
		return finish(p, rep, out)
	}

	anchors := map[string][]anchorDecl{}
	for _, u := range units {
		found, orphans := u.anchors(p)
		for res, decls := range found {
			anchors[res] = append(anchors[res], decls...)
		}
		rep.Unattributed = append(rep.Unattributed, orphans...)
	}
	for res := range anchors {
		rep.Resources = append(rep.Resources, res)
	}
	sort.Strings(rep.Resources)
	sort.Strings(rep.Unattributed)

	// A public List the gate could not attribute is a finding, not a census gap.
	// Reported before the "nothing examined" branch below on purpose: a tree whose
	// EVERY List is unattributed reads as "no resources found", and "the gate does
	// not recognise this layout" is a far more actionable answer than "the anchor
	// root holds no List".
	for _, orphan := range rep.Unattributed {
		rep.Findings = append(rep.Findings, fmt.Sprintf(
			"public List declaration attributed to no resource — its List goes unjudged while the "+
				"gate reports OK: %s\n  identification is by the declared transport type (%s); either "+
				"name the type so it is recognised, or state in the service's Profile that this is the "+
				"transport surface",
			orphan, p.ReceiverSuffix))
	}

	if len(rep.Resources) == 0 {
		rep.Findings = append(rep.Findings, fmt.Sprintf(
			"no public List declaration found under %s (parsed %d file(s) in %d package(s)) — the gate "+
				"examined nothing, so it proved nothing; zero findings must not be reachable from zero reads",
			anchor, rep.Files, rep.Packages))
		return finish(p, rep, out)
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
				a, anchor))
		}
	}

	for _, res := range rep.Resources {
		if allowed[res] {
			rep.Whitelisted = append(rep.Whitelisted, res)
			continue
		}
		rep.Checked = append(rep.Checked, res)
		for _, a := range anchors[res] {
			rep.Findings = append(rep.Findings, checkAnchor(p, res, a)...)
		}
	}

	return finish(p, rep, out)
}

// anchorDecl is one public List declaration together with the package it can reach.
type anchorDecl struct {
	pos  string // "<file>:<line>:<col>" of the declaration
	fn   *ast.FuncDecl
	unit *unit
}

// checkAnchor judges one List declaration: the call graph it roots must reach an
// accepted per-object filter, and must not reach a banned enumeration.
func checkAnchor(p Profile, res string, a anchorDecl) []string {
	called := a.unit.reachableCalls(a.fn)

	var findings []string
	if !anyOf(called, p.Filters) {
		findings = append(findings, fmt.Sprintf(
			"%s — List hands back its page without a per-object visibility filter: nothing along the "+
				"calls it makes reaches %s (kacho-iam BatchCheck, relation viewer ∪ v_list)\n  declared: %s",
			res, orList(p.Filters), a.pos))
	}
	for _, b := range p.Banned {
		if called[b] {
			findings = append(findings, fmt.Sprintf(
				"%s — List reaches %s: enumerating every allowed id is capped server-side with no "+
					"continuation token, so the caller's own rows fall outside the cap and disappear; "+
					"check the page that was read instead\n  declared: %s",
				res, b, a.pos))
		}
	}
	return findings
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// unit is one parsed package, indexed for call resolution.
type unit struct {
	dir   string
	name  string // resource name when Profile.PerPackage (the directory), else ""
	fset  *token.FileSet
	files []*ast.File

	funcs   map[string]*ast.FuncDecl     // package-level funcs by name
	methods map[string]*ast.FuncDecl     // "Type.Method"
	fields  map[string]map[string]string // struct Type → field → package-local type
	recv    map[*ast.FuncDecl][2]string  // fn → {receiver var, receiver type}
}

// loadUnits parses the anchor tree. An absent or unreadable root is an error, never
// an empty result: a gate that could not open the tree has proven nothing.
func loadUnits(p Profile, anchor string) ([]*unit, error) {
	info, err := os.Stat(anchor)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf(
			"anchor root %s is absent — the gate examined nothing, so it proved nothing "+
				"(a gate pointed at the wrong tree must not be indistinguishable from a clean one)", anchor)
	}
	if !p.PerPackage {
		u, uerr := loadUnit(anchor, "")
		if uerr != nil {
			return nil, uerr
		}
		return []*unit{u}, nil
	}
	entries, rerr := os.ReadDir(anchor)
	if rerr != nil {
		return nil, fmt.Errorf("read %s: %w", anchor, rerr)
	}
	var out []*unit
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		u, uerr := loadUnit(filepath.Join(anchor, e.Name()), e.Name())
		if uerr != nil {
			return out, uerr
		}
		out = append(out, u)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf(
			"anchor root %s holds no package directory — the gate examined nothing, so it proved nothing", anchor)
	}
	return out, nil
}

// loadUnit parses every non-test .go file directly inside dir and indexes it.
//
// Comments are NOT retained (parser mode 0): the gate must judge code, never prose
// about code.
func loadUnit(dir, name string) (*unit, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	u := &unit{
		dir:     dir,
		name:    name,
		fset:    token.NewFileSet(),
		funcs:   map[string]*ast.FuncDecl{},
		methods: map[string]*ast.FuncDecl{},
		fields:  map[string]map[string]string{},
		recv:    map[*ast.FuncDecl][2]string{},
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, perr := parser.ParseFile(u.fset, path, nil, 0)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, perr)
		}
		u.files = append(u.files, f)
	}
	u.index()
	return u, nil
}

// index builds the tables call resolution needs: package funcs, methods keyed by
// "Type.Method", and struct field → package-local type.
func (u *unit) index() {
	for _, f := range u.files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				if d.Recv == nil {
					u.funcs[d.Name.Name] = d
					continue
				}
				v, typ := receiverOf(d)
				if typ == "" {
					continue
				}
				u.recv[d] = [2]string{v, typ}
				u.methods[typ+"."+d.Name.Name] = d
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok || st.Fields == nil {
						continue
					}
					m := map[string]string{}
					for _, fld := range st.Fields.List {
						t := bareTypeName(fld.Type)
						if t == "" {
							continue
						}
						for _, n := range fld.Names {
							m[n.Name] = t
						}
					}
					u.fields[ts.Name.Name] = m
				}
			}
		}
	}
}

// anchors returns this unit's public List declarations, keyed by resource, plus
// the ones it could NOT attribute to any resource.
//
// The second return value is not diagnostics — it is a finding. Attribution is by
// the declared transport type, so a `List` on a type this profile does not
// recognise falls out of the resource set entirely and goes unjudged. That is the
// same hole the predecessors had (they keyed on a mutable label and lost a resource
// to a rename), only one level down: here the census showed it — "more packages
// than resources" — and nothing acted on the gap.
func (u *unit) anchors(p Profile) (map[string][]anchorDecl, []string) {
	out := map[string][]anchorDecl{}
	var unattributed []string
	for _, f := range u.files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "List" || fn.Body == nil || fn.Recv == nil {
				continue
			}
			_, typ := receiverOf(fn)
			res, ok := p.resourceOf(u, typ)
			if !ok {
				unattributed = append(unattributed, fmt.Sprintf("%s (receiver type %s)",
					u.fset.Position(fn.Pos()).String(), quoteType(typ)))
				continue
			}
			out[res] = append(out[res], anchorDecl{
				pos:  u.fset.Position(fn.Pos()).String(),
				fn:   fn,
				unit: u,
			})
		}
	}
	return out, unattributed
}

// quoteType names a receiver type for a message, including the case where the
// receiver is not a plain package-local named type at all.
func quoteType(typ string) string {
	if typ == "" {
		return "<not a package-local named type>"
	}
	return typ
}

// resourceOf maps a receiver TYPE to the resource it serves, or reports that the
// declaration is not a public List transport at all.
func (p Profile) resourceOf(u *unit, typ string) (string, bool) {
	if typ == "" {
		return "", false
	}
	if p.PerPackage {
		// One transport type per package; the package IS the resource.
		if typ != p.ReceiverSuffix {
			return "", false
		}
		return u.name, true
	}
	if typ == p.ReceiverSuffix || !strings.HasSuffix(typ, p.ReceiverSuffix) {
		return "", false
	}
	return toSnake(strings.TrimSuffix(typ, p.ReceiverSuffix)), true
}

// ---------------------------------------------------------------------------
// Call-graph reachability
// ---------------------------------------------------------------------------

// reachableCalls returns every call NAME reachable from fn without leaving fn's
// package. Resolution is syntactic and deliberately narrow:
//
//   - `f(…)`                → the package-level func f, if there is one;
//   - `<recv>.M(…)`         → method M on the receiver's own type;
//   - `<recv>.<field>.M(…)` → method M on the package-local type of that field
//     (the handler → use-case hop of vpc and nlb);
//   - anything else         → the NAME is recorded but not followed.
//
// Recording the name of an unfollowed call is what makes `authzfilter.
// FilterVisiblePage(…)` count: it leads into another package, so there is nothing
// local to walk into, but that List reaches it is exactly the point.
//
// The walk is scoped to what List CALLS rather than to the file it sits in, so a
// filtered sibling method — or a filter-typed struct field — cannot vouch for a
// List that dropped its own filter.
func (u *unit) reachableCalls(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	seen := map[*ast.FuncDecl]bool{}
	u.walk(fn, names, seen)
	return names
}

func (u *unit) walk(fn *ast.FuncDecl, names map[string]bool, seen map[*ast.FuncDecl]bool) {
	if fn == nil || fn.Body == nil || seen[fn] {
		return
	}
	seen[fn] = true
	recvVar, recvType := u.recv[fn][0], u.recv[fn][1]

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch f := unwrapIndex(call.Fun).(type) {
		case *ast.Ident:
			names[f.Name] = true
			u.walk(u.funcs[f.Name], names, seen)
		case *ast.SelectorExpr:
			names[f.Sel.Name] = true
			switch x := f.X.(type) {
			case *ast.Ident:
				if recvVar != "" && x.Name == recvVar {
					u.walk(u.methods[recvType+"."+f.Sel.Name], names, seen)
				}
			case *ast.SelectorExpr:
				inner, ok := x.X.(*ast.Ident)
				if !ok || recvVar == "" || inner.Name != recvVar {
					return true
				}
				if ft := u.fields[recvType][x.Sel.Name]; ft != "" {
					u.walk(u.methods[ft+"."+f.Sel.Name], names, seen)
				}
			}
		}
		return true
	})
}

// ---------------------------------------------------------------------------
// AST helpers
// ---------------------------------------------------------------------------

// receiverOf returns the receiver's variable name and bare type name. The variable
// name is returned only so `<recv>.field.M(…)` can be resolved; it is NEVER part of
// how a declaration is identified.
func receiverOf(fn *ast.FuncDecl) (varName, typeName string) {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return "", ""
	}
	fld := fn.Recv.List[0]
	if len(fld.Names) == 1 {
		varName = fld.Names[0].Name
	}
	return varName, bareTypeName(fld.Type)
}

// bareTypeName strips pointers, generic instantiation and parentheses, returning ""
// for anything that is not a plain package-local named type (qualified types, maps,
// funcs, inline interfaces).
func bareTypeName(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.SelectorExpr:
			return "" // a type from another package: not resolvable here
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

// unwrapIndex strips generic instantiation from a call's function expression, so
// `filterVisible[T](…)` and `filterVisible(…)` resolve alike.
func unwrapIndex(e ast.Expr) ast.Expr {
	for {
		switch t := e.(type) {
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		default:
			return e
		}
	}
}

func anyOf(names map[string]bool, want []string) bool {
	for _, w := range want {
		if names[w] {
			return true
		}
	}
	return false
}

func orList(xs []string) string {
	switch len(xs) {
	case 0:
		return "any per-object filter"
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " or " + xs[len(xs)-1]
}

// toSnake converts an exported Go type stem to snake_case (MachineType → machine_type).
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

// finish prints the census — on every path, including the ones that could not read
// anything — then the findings, and turns findings into an error.
func finish(p Profile, rep Report, out io.Writer) (Report, error) {
	_, _ = fmt.Fprintf(out,
		"audit-list-filter[%s]: examined %d file(s) in %d package(s), %d resource(s) "+
			"(%d checked, %d whitelisted, %d List declaration(s) attributed to no resource)\n",
		p.Service, rep.Files, rep.Packages, len(rep.Resources),
		len(rep.Checked), len(rep.Whitelisted), len(rep.Unattributed))
	if len(rep.Checked) > 0 {
		_, _ = fmt.Fprintf(out, "audit-list-filter[%s]: checked %s\n", p.Service, strings.Join(rep.Checked, ", "))
	}
	if len(rep.Whitelisted) > 0 {
		_, _ = fmt.Fprintf(out, "audit-list-filter[%s]: whitelisted %s\n", p.Service, strings.Join(rep.Whitelisted, ", "))
	}
	if len(rep.Findings) == 0 {
		_, _ = fmt.Fprintf(out, "audit-list-filter[%s]: OK\n", p.Service)
		return rep, nil
	}
	for _, f := range rep.Findings {
		_, _ = fmt.Fprintf(out, "audit-list-filter[%s]: %s\n", p.Service, f)
	}
	_, _ = fmt.Fprint(out, explanation)
	return rep, fmt.Errorf("audit-list-filter[%s]: %d finding(s)", p.Service, len(rep.Findings))
}

const explanation = `
Every public List<Resource> RPC must read its page from the service's own DB and
then narrow that page to the rows the caller may actually see — a batched
per-object check against kacho-iam (AuthorizeService.BatchCheck, on the same
relation Get enforces). Project scope alone does NOT answer "may this caller see
THESE objects": without the per-object filter every project member sees every row
of the project.

Enumerating all allowed ids (ListAllowedIDs/ListObjects) is not a substitute: that
answer is capped server-side with no continuation token, so a tenant's own rows
fall outside the cap and become invisible.

Whitelist a cluster-catalog resource with --allow=<resource> if the cluster-wide
surface is intentional — and drop the entry once the resource is gone, since an
exclusion with no subject only hides its successor.
`
