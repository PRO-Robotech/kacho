// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package softopengate is the analyser that keeps a soft open pass from ever going
// back to being invisible.
//
// # The subject
//
// A per-object List filter may be configured to hand the page back UNFILTERED when
// the authorization peer fails ("fail open"). That escape is defensible only while
// the failure is genuinely temporary. If it does not tell a peer that is briefly
// down apart from a peer we are not addressing correctly at all — no such method,
// credentials not accepted, an answer of a different shape — then a permanent
// misconfiguration becomes the steady state: the filter is enabled, wired, executed
// on every request, and has not narrowed a single page in its whole life.
//
// So a branch that returns the caller's page on such a switch must be OBSERVABLE.
// That means both of:
//
//   - it is logged, and the level is decided by classifying the error (a proven
//     misconfiguration cannot be a warning about something temporary);
//   - it is COUNTED, and the count is readable. A log shows that a pass HAPPENED
//     and can never show that it did NOT: "zero passes over the filter's lifetime"
//     is indistinguishable from "there is no counter" unless a number is read.
//
// # What it refuses, and what it must stay silent about
//
// The gate finds every `if` whose condition reads a fail-open style switch, and
// judges ONLY those branches that hand back the enclosing function's own page
// parameter. A branch of the very same shape that REFUSES — a boot guard reading
// the same switch and returning an error so the process will not start — is not the
// subject and must not be reported. Refusing is the opposite of passing, and a gate
// that could not tell them apart would fire on the guards that make the escape safe.
//
// One delegation hop is followed inside the same package: `return f.openPass(ids,
// err)` still hands back the page, and the observability then legitimately lives in
// the callee. Judging the branch alone would report the tidiest possible version of
// the very thing the gate is asking for.
//
// # Why this parses instead of grepping
//
// Three properties fall out of parsing and none of them survive a text search:
//
//   - comments are not code. The sentence explaining why a soft pass must be loud
//     lives right next to the branch; a text search for "Warn" or "counter" finds it
//     and stays green after the call itself is deleted;
//   - a call is judged by what the branch DOES, not by what its file contains. A
//     logger field on the struct, or a logged sibling branch, must not vouch for a
//     branch that lost its own;
//   - "hands back the page" is a real question about identifiers — whether the
//     returned expression is the function's own slice parameter — not a guess from
//     the text `return ids, nil`.
//
// # Census
//
// The report states what was examined — roots walked, files parsed, switch-reading
// branches seen, soft passes judged — on every path, and treats "nothing examined"
// as a finding. A gate aimed at a moved tree must not read the same as a clean one:
// "zero findings" has to be unreachable from "zero read".
//
// # Its own premise
//
// The gate's premise is that a soft-open switch is recognisable by its declared
// name. If files were parsed and not one branch reads such a switch, the premise no
// longer holds — the knob was renamed, or the filters moved — and the gate says so
// instead of reporting success over a walk that judged nothing.
package softopengate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// switchNames are the selector names that denote "hand the page back on error".
// Recognition is by the DECLARED field name, not by a file or variable naming
// habit: `f.cfg.FailOpen` and `c.Authz.ListFilter.FailOpen` are both this switch.
var switchNames = map[string]bool{
	"FailOpen":           true,
	"ListFilterFailOpen": true,
	"AuthZFailOpen":      true,
	// Имена общего сужателя. `SoftPassOnPeerFailure` — прежний FailOpen под своим
	// предметом; `Breakglass` — вторая ветка, которая тоже ОТДАЁТ страницу, только по
	// другой причине (модели нет вовсе, а не сосед не ответил). Она подпадает под тот
	// же запрет: проход без прибора превращает аварийный режим в тихий штатный, и
	// «им пользуются» становится неотличимо от «им не пользуются».
	"SoftPassOnPeerFailure": true,
	"Breakglass":            true,
}

// logCalls / counterCalls are the executable shapes that make a pass observable.
var (
	logCalls = map[string]bool{
		"Warn": true, "Error": true, "Info": true, "Debug": true,
		"WarnContext": true, "ErrorContext": true, "InfoContext": true, "DebugContext": true,
	}
	counterCalls = map[string]bool{"Add": true, "Inc": true}
)

// observability is what a soft pass must carry. Both are required: the log says a
// pass happened, the counter is what makes "it never happened" readable.
type observability struct {
	logged  bool
	counted bool
}

func (o observability) complete() bool { return o.logged && o.counted }

func (o observability) missing() string {
	switch {
	case !o.logged && !o.counted:
		return "neither logged nor counted"
	case !o.logged:
		return "counted but not logged"
	default:
		return "logged but not counted"
	}
}

func (o observability) merge(other observability) observability {
	return observability{logged: o.logged || other.logged, counted: o.counted || other.counted}
}

// Report is the census plus the findings of one run.
type Report struct {
	Roots        []string // roots walked, sorted
	Files        int      // non-test .go files parsed
	SwitchReads  int      // `if` branches whose condition reads a soft-open switch
	SoftPasses   int      // of those, the ones that hand back the page (the subject)
	Refusals     int      // of those, the ones that refuse instead (not the subject)
	Findings     []string // soft passes that are not observable
	PremiseNotes []string // the gate's own premise failing to hold
}

// OK reports whether the run is clean.
func (r Report) OK() bool { return len(r.Findings) == 0 && len(r.PremiseNotes) == 0 }

// Census renders what was examined, so a passing run states its own scope.
func (r Report) Census() string {
	return fmt.Sprintf(
		"softopengate: %d root(s) [%s], %d file(s) parsed, %d branch(es) read a fail-open switch "+
			"(%d hand back the page and were judged, %d refuse and were not), %d finding(s)",
		len(r.Roots), strings.Join(r.Roots, ", "), r.Files,
		r.SwitchReads, r.SoftPasses, r.Refusals, len(r.Findings))
}

// Run walks the given roots and judges every soft pass it finds.
func Run(roots []string) (Report, error) {
	rep := Report{Roots: append([]string(nil), roots...)}
	sort.Strings(rep.Roots)

	fset := token.NewFileSet()

	// Parse first, judge second: a delegated hop is resolved against the functions
	// declared in the SAME directory, which have to be known before any judging.
	type parsedDir struct {
		files []*ast.File
		funcs map[string]*ast.FuncDecl
	}
	dirs := map[string]*parsedDir{}
	var order []string

	for _, root := range roots {
		paths, err := goFiles(root)
		if err != nil {
			return rep, fmt.Errorf("%s could not be walked (%v) — nothing under it was inspected, "+
				"so nothing about it can be vouched for", root, err)
		}
		for _, path := range paths {
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return rep, fmt.Errorf("%s could not be parsed (%v) — it was not inspected", path, err)
			}
			rep.Files++
			dir := filepath.Dir(path)
			pd := dirs[dir]
			if pd == nil {
				pd = &parsedDir{funcs: map[string]*ast.FuncDecl{}}
				dirs[dir] = pd
				order = append(order, dir)
			}
			pd.files = append(pd.files, f)
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
					pd.funcs[fn.Name.Name] = fn
				}
			}
		}
	}

	sort.Strings(order)
	for _, dir := range order {
		pd := dirs[dir]
		for _, f := range pd.files {
			inspectFile(fset, f, pd.funcs, &rep)
		}
	}

	if rep.Files == 0 {
		rep.PremiseNotes = append(rep.PremiseNotes,
			"no Go source was parsed under any root — the gate examined nothing, "+
				"which must not be mistaken for a clean result")
	}
	if rep.Files > 0 && rep.SwitchReads == 0 {
		rep.PremiseNotes = append(rep.PremiseNotes, fmt.Sprintf(
			"%d file(s) were parsed and not one branch reads a fail-open switch by any known name (%s) — "+
				"the gate's premise no longer holds: either the knob was renamed or the filters moved, "+
				"and in both cases this run proves nothing",
			rep.Files, strings.Join(knownNames(), ", ")))
	}
	sort.Strings(rep.Findings)
	return rep, nil
}

func knownNames() []string {
	out := make([]string, 0, len(switchNames))
	for n := range switchNames {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// goFiles lists non-test .go sources under root, deterministically.
func goFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// inspectFile judges every function in one file.
func inspectFile(fset *token.FileSet, f *ast.File, funcs map[string]*ast.FuncDecl, rep *Report) {
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		slices := sliceParams(fn)
		ast.Inspect(fn.Body, func(m ast.Node) bool {
			ifStmt, ok := m.(*ast.IfStmt)
			if !ok || !readsSwitch(ifStmt.Cond) {
				return true
			}
			rep.SwitchReads++
			if !handsBackPage(ifStmt.Body, slices) {
				// A branch of the same shape that REFUSES (a boot guard) — the
				// opposite of the subject. Reporting it would fire on the very
				// guards that make the escape survivable.
				rep.Refusals++
				return true
			}
			rep.SoftPasses++
			if observabilityOfBranch(ifStmt.Body, funcs).complete() {
				return true
			}
			rep.Findings = append(rep.Findings, fmt.Sprintf(
				"%s: %s hands the caller's page back on a fail-open switch and is %s — "+
					"an unobservable soft pass makes a permanent misconfiguration indistinguishable "+
					"from a healthy filter, and its absence indistinguishable from its silence",
				fset.Position(ifStmt.Pos()), fn.Name.Name,
				observabilityOfBranch(ifStmt.Body, funcs).missing()))
			return true
		})
	}
}

// sliceParams collects the names of the function's own slice parameters — the
// candidates for "the caller's page".
func sliceParams(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	if fn.Type == nil || fn.Type.Params == nil {
		return out
	}
	for _, field := range fn.Type.Params.List {
		if _, isSlice := field.Type.(*ast.ArrayType); !isSlice {
			continue
		}
		for _, name := range field.Names {
			out[name.Name] = true
		}
	}
	return out
}

// readsSwitch reports whether the condition reads a fail-open style selector.
func readsSwitch(cond ast.Expr) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && switchNames[sel.Sel.Name] {
			found = true
		}
		return !found
	})
	return found
}

// handsBackPage reports whether the branch returns one of the function's own slice
// parameters — directly, or as the argument of a delegated call.
func handsBackPage(body *ast.BlockStmt, slices map[string]bool) bool {
	handed := false
	ast.Inspect(body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, res := range ret.Results {
			if ident, ok := res.(*ast.Ident); ok && slices[ident.Name] {
				handed = true
			}
			if call, ok := res.(*ast.CallExpr); ok {
				for _, arg := range call.Args {
					if ident, ok := arg.(*ast.Ident); ok && slices[ident.Name] {
						handed = true
					}
				}
			}
		}
		return !handed
	})
	return handed
}

// observabilityOfBranch judges the branch, following ONE delegation hop into a
// function declared in the same directory.
func observabilityOfBranch(body *ast.BlockStmt, funcs map[string]*ast.FuncDecl) observability {
	obs := observabilityOf(body)
	if obs.complete() {
		return obs
	}
	for _, name := range delegatedCallees(body) {
		callee, ok := funcs[name]
		if !ok {
			continue
		}
		obs = obs.merge(observabilityOf(callee.Body))
		if obs.complete() {
			return obs
		}
	}
	return obs
}

// delegatedCallees names the functions this branch returns the result of.
func delegatedCallees(body *ast.BlockStmt) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, res := range ret.Results {
			call, ok := res.(*ast.CallExpr)
			if !ok {
				continue
			}
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				out = append(out, fun.Sel.Name)
			case *ast.Ident:
				out = append(out, fun.Name)
			}
		}
		return true
	})
	return out
}

// observabilityOf inspects only the EXECUTABLE contents of a body. Comments are not
// walked here at all, so the sentence explaining a branch cannot satisfy it.
func observabilityOf(body *ast.BlockStmt) observability {
	var obs observability
	if body == nil {
		return obs
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
				if logCalls[sel.Sel.Name] {
					obs.logged = true
				}
				if counterCalls[sel.Sel.Name] {
					obs.counted = true
				}
			}
		case *ast.IncDecStmt:
			obs.counted = true
		case *ast.AssignStmt:
			if node.Tok == token.ADD_ASSIGN {
				obs.counted = true
			}
		}
		return true
	})
	return obs
}
