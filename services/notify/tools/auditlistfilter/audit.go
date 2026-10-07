// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditlistfilter is the public-List gate of notify: a listing RPC must
// hand back only what its caller may see.
//
// # Why this analyser is not a profile of corelib/listfiltergate
//
// listfiltergate resolves calls inside ONE package — the transport package it is
// anchored at. That fits services whose transport carries the decision (compute's
// handler runs the page through the narrower; vpc's handler reads the parent
// first). notify is laid out the other way round: internal/handler is a thin
// adapter whose every method is one delegation to a use-case in its own package,
// and the access decision — "may this caller read the scope named in the request"
// — is made THERE (замысел issue-2924 З16). Pointed at the handler, listfiltergate
// sees no evidence for any shape, and there is no shape it could be told that
// would be true: a ParentGate on the delegation call would let the page read vouch
// for its own gate. So the evidence is followed across the delegation, into the
// use-case, and judged where it lives — the same reason registry and storage carry
// analysers of their own.
//
// # What is judged
//
//   - every List* method on the transport is attributed to a resource by its
//     DECLARED receiver type (suffix Handler), never by a variable name; a List*
//     on any other receiver of the transport package is a finding, not a skip;
//   - every listing has a declaration, every declaration has a listing;
//   - ScopeGate: the use-case checks the scope named in the request, acts on the
//     verdict, and only AFTER that reads the page — of the SAME scope it checked;
//   - AdminSurface: an internal-listener listing whose relation the carrier's
//     authorization link checks per RPC; the evidence is the compiled descriptor's
//     options (a relation, a scope extractor, not exempt), not prose;
//   - the enumerate-then-narrow ban is DERIVED from the declared authorization
//     clients (a method whose first non-error result is []string enumerates) and
//     applied to everything a listing use-case reaches inside this service;
//   - every listnarrow.New in the service is a declared narrower site, and the
//     client each narrower is built over is the declared verdict client — proven
//     at the call or, when the client arrives as a parameter, at every caller.
//
// # Census
//
// Every run prints what it opened — files, packages, listings, narrower sites,
// derived ban — on every path, including the refusing one. "Zero findings" must
// not be reachable from "zero read": a missing transport is ErrNotInspected, not OK.
package auditlistfilter

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// ErrNotInspected — the tree the gate was pointed at could not be read; distinct
// from findings, because a gate that opened nothing proved nothing.
var ErrNotInspected = errors.New("the tree could not be inspected")

// ErrFindings — the tree was read and at least one finding stands.
var ErrFindings = errors.New("audit-list-filter findings")

// Shape — how one listing's visibility is decided.
type Shape int

const (
	// ScopeGate — the page lives inside the scope the request names (a project,
	// an account). The use-case checks that scope, returns on a denial, and only
	// then reads the page of that same scope.
	ScopeGate Shape = iota
	// AdminSurface — an internal-listener listing of the whole installation with
	// no per-object owners to narrow to; the per-RPC relation on the cluster is
	// what settles access. Evidence: the compiled method options.
	AdminSurface
)

func (s Shape) String() string {
	switch s {
	case ScopeGate:
		return "ScopeGate"
	case AdminSurface:
		return "AdminSurface"
	}
	return "unknown"
}

// Func names a package-level function by import path, so the alias a file gives
// the package does not change what the gate sees.
type Func struct {
	Pkg  string
	Name string
}

func (f Func) String() string { return lastSegment(f.Pkg) + "." + f.Name }

// Listing — the enforcement declared for one listing method.
type Listing struct {
	Shape Shape
	// Gate — the scope check (ScopeGate).
	Gate Func
	// Read — the page read (ScopeGate); ReadScopeArg — position of the scope in it.
	Read         Func
	ReadScopeArg int
	// RPC — full method name whose options carry the relation (AdminSurface).
	RPC string
	// Reason — why there is nothing per object to narrow (AdminSurface).
	Reason string
}

// TypeSource — a declared type answering authorization questions, by module path.
type TypeSource struct {
	Pkg  string
	Type string
}

// Layout — how a service is laid out, as the analyser reads it.
type Layout struct {
	Service        string
	TransportDir   string // relative to the service root
	ReceiverSuffix string
	Listings       map[string]Listing
	// NarrowerCtor — the narrower constructor whose call sites are censused.
	NarrowerCtor Func
	// NarrowerSites — files (relative to the service root) allowed to call it.
	NarrowerSites []string
	// NarrowerClient — the constructor of the verdict client a narrower is built over.
	NarrowerClient Func
	// AuthzSources — types whose method sets derive the enumeration ban.
	AuthzSources []TypeSource
	// ModulePath — the module the service lives in.
	ModulePath string
}

// Options — one run.
type Options struct {
	// ServiceRoot — services/notify (the directory holding internal/ and cmd/).
	ServiceRoot string
	// ModuleRoot — the directory holding go.mod; found by walking up when empty.
	ModuleRoot string
	// RPCOptions — reads the authorization options of a compiled method. Nil
	// means the descriptors linked into this binary.
	RPCOptions func(fullName string) (RPCAuthz, error)
}

// RPCAuthz — the authorization options of one compiled method.
type RPCAuthz struct {
	Permission       string
	RequiredRelation string
	ScopeObjectType  string
	ScopeField       string
}

// Report — what one run saw.
type Report struct {
	Files         int
	Packages      int
	Listings      []string
	Undeclared    []string
	Admin         []string
	Unattributed  []string
	NarrowerCalls []string
	Banned        []string
	SourceMethods map[string]int
	Findings      []string
}

func (r *Report) findingf(format string, args ...any) {
	r.Findings = append(r.Findings, fmt.Sprintf(format, args...))
}

// Audit judges the tree and prints its census. It returns ErrNotInspected when the
// transport could not be read and ErrFindings when findings stand.
func Audit(p Layout, o Options, out io.Writer) (Report, error) {
	rep := Report{SourceMethods: map[string]int{}}
	a, err := newAnalyser(p, o)
	if err != nil {
		fmt.Fprintf(out, "audit-list-filter[%s]: NOT INSPECTED — %v\n", p.Service, err)
		return rep, fmt.Errorf("%w: %v", ErrNotInspected, err)
	}
	transport, err := a.pkg(filepath.Join(a.serviceRoot, p.TransportDir))
	if err != nil || len(transport.files) == 0 {
		if err == nil {
			err = fmt.Errorf("transport %s holds no non-test .go file", p.TransportDir)
		}
		fmt.Fprintf(out, "audit-list-filter[%s]: NOT INSPECTED — %v (zero findings must not be reachable from zero reads)\n", p.Service, err)
		return rep, fmt.Errorf("%w: %v", ErrNotInspected, err)
	}

	banned := a.deriveBan(&rep)
	a.judgeListings(transport, banned, &rep)
	a.judgeNarrowers(&rep)

	rep.Files, rep.Packages = a.files, len(a.pkgs)
	printCensus(p, rep, out)
	if len(rep.Findings) > 0 {
		return rep, ErrFindings
	}
	return rep, nil
}

func printCensus(p Layout, rep Report, out io.Writer) {
	pre := "audit-list-filter[" + p.Service + "]: "
	fmt.Fprintf(out, "%sexamined %d file(s) in %d package(s), %d listing method(s) (%d undeclared, %d admin-surface, %d declaration(s) attributed to no resource)\n",
		pre, rep.Files, rep.Packages, len(rep.Listings), len(rep.Undeclared), len(rep.Admin), len(rep.Unattributed))
	fmt.Fprintf(out, "%sjudged %s\n", pre, strings.Join(rep.Listings, ", "))
	fmt.Fprintf(out, "%snarrower sites %d (declared %d): %s\n", pre, len(rep.NarrowerCalls), len(p.NarrowerSites), strings.Join(rep.NarrowerCalls, ", "))
	srcs := make([]string, 0, len(rep.SourceMethods))
	for s := range rep.SourceMethods {
		srcs = append(srcs, s)
	}
	sort.Strings(srcs)
	fmt.Fprintf(out, "%senumerate-then-narrow ban — %d call(s) %v derived from %d source(s)\n", pre, len(rep.Banned), rep.Banned, len(srcs))
	for _, s := range srcs {
		fmt.Fprintf(out, "%s  source %s: %d method(s)\n", pre, s, rep.SourceMethods[s])
	}
	if len(rep.Findings) == 0 {
		fmt.Fprintf(out, "%sOK\n", pre)
		return
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(out, "%sFINDING %s\n", pre, f)
	}
	fmt.Fprintf(out, "%s%d finding(s)\n", pre, len(rep.Findings))
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

type file struct {
	path    string
	ast     *ast.File
	imports map[string]string // alias → import path
}

type pkg struct {
	dir     string
	path    string // import path
	files   []*file
	funcs   map[string]*ast.FuncDecl
	methods map[string]*ast.FuncDecl // "Type.Method"
	types   map[string]*ast.TypeSpec
	fileOf  map[ast.Node]*file // decl → file
}

type analyser struct {
	p           Layout
	o           Options
	serviceRoot string
	moduleRoot  string
	fset        *token.FileSet
	pkgs        map[string]*pkg // by dir
	files       int
}

func newAnalyser(p Layout, o Options) (*analyser, error) {
	if info, err := os.Stat(o.ServiceRoot); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("service root %q is absent", o.ServiceRoot)
	}
	sr, err := filepath.Abs(o.ServiceRoot)
	if err != nil {
		return nil, err
	}
	mr := o.ModuleRoot
	if mr == "" {
		mr, err = moduleRootOf(sr)
		if err != nil {
			return nil, err
		}
	}
	mr, err = filepath.Abs(mr)
	if err != nil {
		return nil, err
	}
	return &analyser{p: p, o: o, serviceRoot: sr, moduleRoot: mr, fset: token.NewFileSet(), pkgs: map[string]*pkg{}}, nil
}

func moduleRootOf(dir string) (string, error) {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
	}
}

// dirOf maps an import path of this module to its directory ("" when foreign).
func (a *analyser) dirOf(importPath string) string {
	if importPath == a.p.ModulePath {
		return a.moduleRoot
	}
	rel, ok := strings.CutPrefix(importPath, a.p.ModulePath+"/")
	if !ok {
		return ""
	}
	return filepath.Join(a.moduleRoot, filepath.FromSlash(rel))
}

func (a *analyser) importPathOf(dir string) string {
	rel, err := filepath.Rel(a.moduleRoot, dir)
	if err != nil || rel == "." {
		return a.p.ModulePath
	}
	return a.p.ModulePath + "/" + filepath.ToSlash(rel)
}

// pkg parses every non-test .go file directly inside dir, once. Comments are not
// retained: the gate judges code, never prose about code.
func (a *analyser) pkg(dir string) (*pkg, error) {
	if p, ok := a.pkgs[dir]; ok {
		return p, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	p := &pkg{dir: dir, path: a.importPathOf(dir), funcs: map[string]*ast.FuncDecl{},
		methods: map[string]*ast.FuncDecl{}, types: map[string]*ast.TypeSpec{}, fileOf: map[ast.Node]*file{}}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, perr := parser.ParseFile(a.fset, path, nil, 0)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, perr)
		}
		pf := &file{path: path, ast: f, imports: importsOf(f)}
		p.files = append(p.files, pf)
		a.files++
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				p.fileOf[d] = pf
				if t := recvType(d); t != "" {
					p.methods[t+"."+d.Name.Name] = d
				} else {
					p.funcs[d.Name.Name] = d
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					if ts, ok := s.(*ast.TypeSpec); ok {
						p.types[ts.Name.Name] = ts
						p.fileOf[ts] = pf
					}
				}
			}
		}
	}
	a.pkgs[dir] = p
	return p, nil
}

func importsOf(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, is := range f.Imports {
		path, err := strconv.Unquote(is.Path.Value)
		if err != nil {
			continue
		}
		alias := lastSegment(path)
		if is.Name != nil {
			alias = is.Name.Name
		}
		m[alias] = path
	}
	return m
}

func lastSegment(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func recvType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	return bareName(fn.Recv.List[0].Type)
}

func recvVar(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

func bareName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return bareName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.ParenExpr:
		return bareName(t.X)
	}
	return ""
}

// resolves reports whether call invokes f, resolved through the file's imports.
func resolves(call *ast.CallExpr, fl *file, f Func) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != f.Name {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && fl.imports[x.Name] == f.Pkg
}

// ---------------------------------------------------------------------------
// Enumeration ban
// ---------------------------------------------------------------------------

// deriveBan reads the method sets of the declared authorization sources: a method
// whose first non-error result is []string PRODUCES identifiers, i.e. enumerates.
func (a *analyser) deriveBan(rep *Report) map[string]bool {
	banned := map[string]bool{}
	if len(a.p.AuthzSources) == 0 {
		rep.findingf("no authorization source declared — the enumerate-then-narrow ban has nothing to be derived from, so it bans nothing")
	}
	for _, src := range a.p.AuthzSources {
		key := lastSegment(src.Pkg) + "." + src.Type
		dir := a.dirOf(src.Pkg)
		if dir == "" {
			rep.findingf("authorization source %s is outside module %s — its method set cannot be read", key, a.p.ModulePath)
			continue
		}
		p, err := a.pkg(dir)
		if err != nil || p.types[src.Type] == nil {
			rep.findingf("authorization source %s does not resolve (%v) — a source that stopped existing takes its whole derived ban with it", key, err)
			continue
		}
		n := 0
		for k, fn := range p.methods {
			t, m, _ := strings.Cut(k, ".")
			if t != src.Type {
				continue
			}
			n++
			if enumerates(fn.Type) {
				banned[m] = true
			}
		}
		rep.SourceMethods[key] = n
		if n == 0 {
			rep.findingf("authorization source %s declares no method — the derivation read nothing", key)
		}
	}
	for m := range banned {
		rep.Banned = append(rep.Banned, m)
	}
	sort.Strings(rep.Banned)
	return banned
}

func enumerates(ft *ast.FuncType) bool {
	if ft.Results == nil {
		return false
	}
	for _, r := range ft.Results.List {
		if id, ok := r.Type.(*ast.Ident); ok && id.Name == "error" {
			continue
		}
		arr, ok := r.Type.(*ast.ArrayType)
		if !ok || arr.Len != nil {
			return false
		}
		id, ok := arr.Elt.(*ast.Ident)
		return ok && id.Name == "string"
	}
	return false
}

// ---------------------------------------------------------------------------
// Listings
// ---------------------------------------------------------------------------

func (a *analyser) judgeListings(tr *pkg, banned map[string]bool, rep *Report) {
	type decl struct {
		key string
		fn  *ast.FuncDecl
	}
	var found []decl
	for k, fn := range tr.methods {
		typ, m, _ := strings.Cut(k, ".")
		if !strings.HasPrefix(m, "List") || !ast.IsExported(m) {
			continue
		}
		stem, ok := strings.CutSuffix(typ, a.p.ReceiverSuffix)
		if !ok || stem == "" {
			rep.Unattributed = append(rep.Unattributed, k)
			rep.findingf("%s — a listing method on a transport type not named *%s: it is attributed to no resource and goes unjudged; name the type so it is recognised (%s)",
				k, a.p.ReceiverSuffix, a.pos(fn))
			continue
		}
		found = append(found, decl{snake(stem) + "." + m, fn})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].key < found[j].key })
	for _, d := range found {
		rep.Listings = append(rep.Listings, d.key)
	}
	sort.Strings(rep.Unattributed)
	if len(found) == 0 && len(rep.Unattributed) == 0 {
		rep.findingf("no listing method found in %s (%d file(s)) — the gate examined nothing, so it proved nothing", a.p.TransportDir, len(tr.files))
	}

	declared := make([]string, 0, len(a.p.Listings))
	for k := range a.p.Listings {
		declared = append(declared, k)
	}
	sort.Strings(declared)
	for _, k := range declared {
		if !contains(rep.Listings, k) {
			rep.findingf("declared listing %q matches no method in %s — the declaration outlived its subject; drop it, or the next method of that name inherits a claim nobody checked", k, a.p.TransportDir)
		}
	}

	for _, d := range found {
		l, ok := a.p.Listings[d.key]
		if !ok {
			rep.Undeclared = append(rep.Undeclared, d.key)
			rep.findingf("%s — a listing method with no declared enforcement: nothing states how this page is narrowed, so nothing checks that it is (%s)", d.key, a.pos(d.fn))
			continue
		}
		switch l.Shape {
		case AdminSurface:
			rep.Admin = append(rep.Admin, d.key)
			a.judgeAdmin(d.key, l, rep)
		case ScopeGate:
			uc, ucFile, err := a.delegate(tr, d.fn)
			if err != nil {
				rep.findingf("%s — %v (%s)", d.key, err, a.pos(d.fn))
				continue
			}
			a.judgeScopeGate(d.key, l, uc, ucFile, rep)
			a.judgeBan(d.key, uc, ucFile, banned, rep)
		default:
			rep.findingf("%s — unknown shape %d", d.key, l.Shape)
		}
	}
}

func (a *analyser) pos(n ast.Node) string {
	p := a.fset.Position(n.Pos())
	rel, err := filepath.Rel(a.serviceRoot, p.Filename)
	if err != nil {
		rel = p.Filename
	}
	return fmt.Sprintf("%s:%d", filepath.ToSlash(rel), p.Line)
}

// delegate resolves a thin transport method — `return <recv>.<f1>…<fn>.<M>(…)` —
// to the use-case method it hands the request to.
func (a *analyser) delegate(tr *pkg, fn *ast.FuncDecl) (*ast.FuncDecl, *file, error) {
	notThin := errors.New("transport method does not delegate to exactly one use-case: the decision is " +
		"judged only in the use-case, so a transport that decides itself is outside this analyser's view")
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return nil, nil, notThin
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil, nil, notThin
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok {
		return nil, nil, notThin
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, nil, notThin
	}
	var chain []string
	x := sel.X
	for {
		s, ok := x.(*ast.SelectorExpr)
		if !ok {
			break
		}
		chain = append([]string{s.Sel.Name}, chain...)
		x = s.X
	}
	root, ok := x.(*ast.Ident)
	if !ok || root.Name != recvVar(fn) || len(chain) == 0 {
		return nil, nil, notThin
	}
	// Walk the field chain through package-local struct types; the last field's
	// type is the use-case, named by a package qualifier of the declaring file.
	cur := recvType(fn)
	for i, field := range chain {
		ts := tr.types[cur]
		if ts == nil {
			return nil, nil, fmt.Errorf("type %s is not declared in the transport package", cur)
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return nil, nil, fmt.Errorf("type %s is not a struct of the transport package", cur)
		}
		ft := fieldType(st, field)
		if ft == nil {
			return nil, nil, fmt.Errorf("type %s has no field %s", cur, field)
		}
		if i < len(chain)-1 {
			next := bareName(ft)
			if next == "" {
				return nil, nil, fmt.Errorf("field %s.%s is not a transport-local type", cur, field)
			}
			cur = next
			continue
		}
		q, ok := stripStar(ft).(*ast.SelectorExpr)
		if !ok {
			return nil, nil, fmt.Errorf("field %s.%s is not a use-case of another package", cur, field)
		}
		alias, ok := q.X.(*ast.Ident)
		if !ok {
			return nil, nil, notThin
		}
		path := tr.fileOf[ts].imports[alias.Name]
		dir := a.dirOf(path)
		if dir == "" {
			return nil, nil, fmt.Errorf("use-case %s.%s is outside module %s", alias.Name, q.Sel.Name, a.p.ModulePath)
		}
		up, err := a.pkg(dir)
		if err != nil {
			return nil, nil, fmt.Errorf("use-case package %s: %v", path, err)
		}
		m := up.methods[q.Sel.Name+"."+sel.Sel.Name]
		if m == nil || m.Body == nil {
			return nil, nil, fmt.Errorf("use-case %s.%s has no method %s", path, q.Sel.Name, sel.Sel.Name)
		}
		return m, up.fileOf[m], nil
	}
	return nil, nil, notThin
}

func stripStar(e ast.Expr) ast.Expr {
	if s, ok := e.(*ast.StarExpr); ok {
		return s.X
	}
	return e
}

func fieldType(st *ast.StructType, name string) ast.Expr {
	for _, f := range st.Fields.List {
		for _, n := range f.Names {
			if n.Name == name {
				return f.Type
			}
		}
	}
	return nil
}

// judgeScopeGate — the scope is checked, the verdict is acted on, and only after
// that is the page of the SAME scope read.
func (a *analyser) judgeScopeGate(key string, l Listing, uc *ast.FuncDecl, fl *file, rep *Report) {
	gateIdx, readIdx := -1, -1
	var gateCall, readCall *ast.CallExpr
	for i, st := range uc.Body.List {
		if gateIdx < 0 {
			if c := actedOnGate(st, fl, l.Gate); c != nil {
				gateIdx, gateCall = i, c
			}
		}
		if readIdx < 0 {
			ast.Inspect(st, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok && readCall == nil && resolves(c, fl, l.Read) {
					readCall = c
				}
				return readCall == nil
			})
			if readCall != nil {
				readIdx = i
			}
		}
	}
	at := a.pos(uc)
	if readIdx < 0 {
		rep.findingf("%s — declared ScopeGate reading the page with %s, but the use-case never calls it: the declaration describes a read that does not happen (%s)", key, l.Read, at)
		return
	}
	if gateIdx < 0 {
		called := false
		ast.Inspect(uc.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && resolves(c, fl, l.Gate) {
				called = true
			}
			return !called
		})
		if called {
			rep.findingf("%s — calls %s but never acts on its verdict: a denial on the scope does not stop the page being read (%s)", key, l.Gate, at)
		} else {
			rep.findingf("%s — declared ScopeGate on %s, but the use-case reads the page without checking the scope named in the request (%s)", key, l.Gate, at)
		}
		return
	}
	if gateIdx > readIdx {
		rep.findingf("%s — %s runs AFTER the page is read by %s: the page of a scope the caller may not read has already been fetched (%s)", key, l.Gate, l.Read, at)
		return
	}
	if l.ReadScopeArg >= len(readCall.Args) {
		rep.findingf("%s — %s takes no argument at position %d, so the scope it reads cannot be compared with the one checked (%s)", key, l.Read, l.ReadScopeArg, at)
		return
	}
	scope, ok := readCall.Args[l.ReadScopeArg].(*ast.Ident)
	if !ok {
		rep.findingf("%s — the scope passed to %s is not a plain variable, so it cannot be shown to be the one %s checked (%s)", key, l.Read, l.Gate, at)
		return
	}
	same := false
	for _, arg := range gateCall.Args {
		ast.Inspect(arg, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == scope.Name {
				same = true
			}
			return !same
		})
	}
	if !same {
		rep.findingf("%s — %s checks a scope other than %q, the one %s reads: the check is made, and it is not about this page (%s)", key, l.Gate, scope.Name, l.Read, at)
	}
}

// actedOnGate returns the gate call when st is `if err := Gate(…); err != nil { …; return … }`.
func actedOnGate(st ast.Stmt, fl *file, gate Func) *ast.CallExpr {
	ifs, ok := st.(*ast.IfStmt)
	if !ok || ifs.Init == nil {
		return nil
	}
	as, ok := ifs.Init.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return nil
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok || !resolves(call, fl, gate) {
		return nil
	}
	errVar, ok := as.Lhs[0].(*ast.Ident)
	if !ok || errVar.Name == "_" {
		return nil
	}
	cond, ok := ifs.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ {
		return nil
	}
	lhs, ok := cond.X.(*ast.Ident)
	rhs, ok2 := cond.Y.(*ast.Ident)
	if !ok || !ok2 || lhs.Name != errVar.Name || rhs.Name != "nil" {
		return nil
	}
	if n := len(ifs.Body.List); n == 0 {
		return nil
	} else if _, ok := ifs.Body.List[n-1].(*ast.ReturnStmt); !ok {
		return nil
	}
	return call
}

// judgeBan — nothing a listing use-case reaches inside this service may call an
// enumerating method of an authorization source.
func (a *analyser) judgeBan(key string, uc *ast.FuncDecl, fl *file, banned map[string]bool, rep *Report) {
	if len(banned) == 0 {
		return
	}
	hits := map[string]bool{}
	seen := map[*ast.FuncDecl]bool{}
	var walk func(fn *ast.FuncDecl, fl *file, p *pkg)
	walk = func(fn *ast.FuncDecl, fl *file, p *pkg) {
		if fn == nil || fn.Body == nil || seen[fn] {
			return
		}
		seen[fn] = true
		rv, rt := recvVar(fn), recvType(fn)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch f := c.Fun.(type) {
			case *ast.Ident:
				walk(p.funcs[f.Name], fl, p)
			case *ast.SelectorExpr:
				if banned[f.Sel.Name] {
					hits[f.Sel.Name] = true
				}
				x, ok := f.X.(*ast.Ident)
				if !ok {
					return true
				}
				if x.Name == rv {
					m := p.methods[rt+"."+f.Sel.Name]
					walk(m, p.fileOf[m], p)
					return true
				}
				path, ok := fl.imports[x.Name]
				if !ok || !strings.HasPrefix(path, a.importPathOf(a.serviceRoot)+"/") {
					return true
				}
				q, err := a.pkg(a.dirOf(path))
				if err != nil {
					return true
				}
				if g := q.funcs[f.Sel.Name]; g != nil {
					walk(g, q.fileOf[g], q)
				}
			}
			return true
		})
	}
	up, err := a.pkg(filepath.Dir(fl.path))
	if err != nil {
		rep.findingf("%s — use-case package unreadable: %v", key, err)
		return
	}
	walk(uc, fl, up)
	for _, m := range sortedKeys(hits) {
		rep.findingf("%s — reaches %s, which answers with a SET OF IDENTIFIERS: the page is taken from an enumeration that has a ceiling and no continuation, so the caller's own rows fall outside it at live rights (%s)", key, m, a.pos(uc))
	}
}

func (a *analyser) judgeAdmin(key string, l Listing, rep *Report) {
	if strings.TrimSpace(l.Reason) == "" {
		rep.findingf("%s — declared AdminSurface with no Reason: the one shape whose narrowing is not in this tree must say why", key)
	}
	if l.RPC == "" {
		rep.findingf("%s — declared AdminSurface with no RPC named, so its per-RPC relation cannot be read", key)
		return
	}
	read := a.o.RPCOptions
	if read == nil {
		read = compiledRPCAuthz
	}
	opts, err := read(l.RPC)
	if err != nil {
		rep.findingf("%s — the options of %s could not be read: %v", key, l.RPC, err)
		return
	}
	for _, f := range AdminEvidence(opts) {
		rep.findingf("%s — %s: %s", key, l.RPC, f)
	}
}

// AdminEvidence — what an admin listing's compiled options must carry for the
// per-RPC relation to be the thing that settles access.
func AdminEvidence(o RPCAuthz) []string {
	var out []string
	if o.Permission == "" || o.Permission == "<exempt>" {
		out = append(out, fmt.Sprintf("permission is %q — the RPC is exempt from the per-RPC check, so nothing settles access to the whole installation's list", o.Permission))
	}
	if o.RequiredRelation == "" {
		out = append(out, "carries no required_relation — there is no relation for the carrier's link to check")
	}
	if o.ScopeObjectType != "cluster" || o.ScopeField != "*" {
		out = append(out, fmt.Sprintf("scope extractor resolves %q from %q, not the cluster singleton — an installation-wide list checked against a narrower object", o.ScopeObjectType, o.ScopeField))
	}
	return out
}

// ---------------------------------------------------------------------------
// Narrower sites
// ---------------------------------------------------------------------------

type callSite struct {
	call *ast.CallExpr
	fl   *file
	encl *ast.FuncDecl
	p    *pkg
}

// judgeNarrowers — every narrower construction is a declared site, and the client
// it is built over is the declared verdict client.
func (a *analyser) judgeNarrowers(rep *Report) {
	var all []callSite
	err := filepath.WalkDir(a.serviceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if n := d.Name(); path != a.serviceRoot && (n == "testdata" || strings.HasPrefix(n, ".")) {
			return filepath.SkipDir
		}
		p, perr := a.pkg(path)
		if perr != nil {
			return perr
		}
		for _, fl := range p.files {
			for _, decl := range fl.ast.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if c, ok := n.(*ast.CallExpr); ok {
						all = append(all, callSite{c, fl, fn, p})
					}
					return true
				})
			}
		}
		return nil
	})
	if err != nil {
		rep.findingf("service tree walk failed: %v — narrower sites were not censused", err)
		return
	}
	declared := map[string]bool{}
	for _, s := range a.p.NarrowerSites {
		declared[s] = true
	}
	hit := map[string]bool{}
	for _, cs := range all {
		if !resolves(cs.call, cs.fl, a.p.NarrowerCtor) {
			continue
		}
		rel, _ := filepath.Rel(a.serviceRoot, cs.fl.path)
		rel = filepath.ToSlash(rel)
		rep.NarrowerCalls = append(rep.NarrowerCalls, rel)
		hit[rel] = true
		if !declared[rel] {
			rep.findingf("%s — %s outside the declared narrower sites %v: a narrower whose client nobody vetted", a.pos(cs.call), a.p.NarrowerCtor, a.p.NarrowerSites)
			continue
		}
		a.judgeNarrowerClient(cs, all, rep)
	}
	sort.Strings(rep.NarrowerCalls)
	for _, s := range a.p.NarrowerSites {
		if !hit[s] {
			rep.findingf("declared narrower site %s calls %s nowhere — the declaration outlived its subject", s, a.p.NarrowerCtor)
		}
	}
	if len(rep.NarrowerCalls) == 0 {
		rep.findingf("no call of %s found in the service — the narrower census read nothing", a.p.NarrowerCtor)
	}
}

func (a *analyser) judgeNarrowerClient(cs callSite, all []callSite, rep *Report) {
	if len(cs.call.Args) == 0 {
		rep.findingf("%s — %s without a client argument", a.pos(cs.call), a.p.NarrowerCtor)
		return
	}
	arg := cs.call.Args[0]
	if c, ok := arg.(*ast.CallExpr); ok && resolves(c, cs.fl, a.p.NarrowerClient) {
		return
	}
	id, ok := arg.(*ast.Ident)
	idx := paramIndex(cs.encl, idName(id, ok))
	if idx < 0 {
		rep.findingf("%s — the narrower is built over %s, which is neither %s nor a parameter of %s: the client it asks cannot be shown to be the verdict client",
			a.pos(cs.call), exprString(arg), a.p.NarrowerClient, cs.encl.Name.Name)
		return
	}
	// The client arrives as a parameter: every caller of the enclosing function must
	// hand it the verdict client.
	encl := Func{Pkg: cs.p.path, Name: cs.encl.Name.Name}
	callers := 0
	for _, o := range all {
		if !resolves(o.call, o.fl, encl) {
			continue
		}
		callers++
		if idx >= len(o.call.Args) {
			continue
		}
		if c, ok := o.call.Args[idx].(*ast.CallExpr); ok && resolves(c, o.fl, a.p.NarrowerClient) {
			continue
		}
		rep.findingf("%s — %s is called with client %s, not %s", a.pos(o.call), encl, exprString(o.call.Args[idx]), a.p.NarrowerClient)
	}
	if callers == 0 {
		rep.findingf("%s — the narrower takes its client from a parameter of %s, and nothing in the service calls %s: the client cannot be shown to be %s",
			a.pos(cs.call), encl, encl, a.p.NarrowerClient)
	}
}

func idName(id *ast.Ident, ok bool) string {
	if !ok {
		return ""
	}
	return id.Name
}

func paramIndex(fn *ast.FuncDecl, name string) int {
	if name == "" || fn.Type.Params == nil {
		return -1
	}
	i := 0
	for _, f := range fn.Type.Params.List {
		if len(f.Names) == 0 {
			i++
			continue
		}
		for _, n := range f.Names {
			if n.Name == name {
				return i
			}
			i++
		}
	}
	return -1
}

func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.CallExpr:
		return exprString(t.Fun) + "(…)"
	}
	return fmt.Sprintf("%T", e)
}

// ---------------------------------------------------------------------------

func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
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
