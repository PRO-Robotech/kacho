// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package revocationwindowgate is the analyser that keeps the delay between
// "the grant was withdrawn" and "the service stops honouring it" a DECIDED
// number rather than an emergent one.
//
// # The subject
//
// Every backend service caches positive authorization verdicts. Only positive
// ones: a fresh GRANT is therefore visible immediately, and it is only a
// WITHDRAWAL that waits. What it waits for is the entry's time to live, because
// no other path removes it — so the cache's TTL *is* the revocation window, and
// each service picked one independently.
//
// That is a security parameter. It was never declared as one. Six services
// carried a window, five of them 5s and one 2s, each stated in its own comment
// in its own file, and no single place said what the window is allowed to be or
// why. A number nobody chose cannot be reviewed, cannot be argued with, and
// cannot be noticed when it changes.
//
// # What it refuses
//
// Four separable failures, because they have different fixes:
//
//   - a construction site whose window is NOT declared in the policy census. A
//     seventh service caching verdicts would otherwise inherit a window by
//     accident and be measured by nothing;
//   - a site that holds a window WITHOUT NAMING a cache at all — see
//     implicit.go. Everything else in this package measures services that build
//     a cache, which is a strictly narrower question than "which services have a
//     window", and the difference was a way to hold one silently;
//   - a declared window that EXCEEDS the policy ceiling. The ceiling is the
//     promise; a service quietly widening its own window breaks it;
//   - a census entry whose declared value no longer matches what the service's
//     source actually says. This is the direction that makes the gate bite: an
//     operator who changes a default and does not touch the policy gets a red
//     naming both numbers, so the change becomes a decision instead of a drift.
//
// # What it must stay silent about
//
// A cache that is not an authorization-verdict cache — the project-existence
// cache, the zone/region projection cache — is not this subject. Those cache
// FACTS about peer resources, not DECISIONS about access, and withdrawing a
// grant does not make a zone stop existing. Recognition is therefore by the
// declared knob name, and the gate reports what it matched so a rename is
// visible as a premise failure rather than as silence.
//
// # Why this parses instead of grepping
//
// The window is written three different ways across the tree — a typed
// duration literal, a quoted duration string, a count of milliseconds in a
// struct tag — and each lives next to prose that contains the same digits. A
// text search for "5s" finds the sentence explaining the window as readily as
// the value, and stays green when the value alone changes. Parsing asks for the
// value that the program will actually use.
//
// # Census
//
// The report states files parsed, sites matched, and census entries checked, on
// every path. "Nothing was declared" and "nothing was read" must not print the
// same way: a gate aimed at a moved tree has to say so rather than pass.
//
// # Its own premise
//
// The premise is that a verdict cache is recognisable by the declared name of
// the knob that sizes it. If files parse and not one site matches, the knobs
// were renamed or the caches moved, and the gate reports that instead of
// success over a walk that judged nothing.
package revocationwindowgate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Site is one construction site of an authorization-verdict cache: the file it
// was found in, the service it belongs to, and the window the program will use.
type Site struct {
	Service string
	Knob    string
	File    string
	Line    int
	Window  time.Duration
	// Inherited is true when the site passes a non-positive TTL and therefore
	// takes the corelib default. Such a site has no number of its own; the
	// number it uses is the policy's, which is exactly the shape we want.
	Inherited bool
}

// Report is the outcome, including what was examined.
type Report struct {
	FilesParsed  int
	SitesMatched int
	Sites        []Site
	Findings     []string
}

// Findingf appends a finding.
func (r *Report) Findingf(format string, args ...any) {
	r.Findings = append(r.Findings, fmt.Sprintf(format, args...))
}

// knobNames — the declared names that size an authorization-VERDICT cache.
//
// Recognition is by name because the shapes differ: these appear as viper keys,
// as envconfig struct tags, and as Go field names. A cache of peer FACTS
// (project existence, zone projection) is deliberately absent from this map —
// see the package doc.
var knobNames = map[string]string{
	// per-RPC Check verdict cache (pkg/authz.Cache), both listeners
	"authz.cache-ttl":                "check",
	"authz.cache.ttl":                "check",
	"KACHO_REGISTRY_AUTHZ_CACHE_TTL": "check",
	// per-object List visibility cache (internal/authzfilter)
	"authz.list-filter.cache-ttl":            "list-filter",
	"KACHO_COMPUTE_LIST_FILTER_CACHE_TTL_MS": "list-filter",
	"KACHO_STORAGE_LIST_FILTER_CACHE_TTL_MS": "list-filter",
}

// KnobNames returns the recognised knob names, sorted — so a caller can state
// the premise it is relying on.
func KnobNames() []string {
	out := make([]string, 0, len(knobNames))
	for k := range knobNames {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// KnobFamily reports which cache family a knob sizes ("" when unrecognised).
func KnobFamily(knob string) string { return knobNames[knob] }

// ScanFile parses one Go file and appends every recognised site to rep.
//
// service is the service the file belongs to, as the caller resolved it from
// the path; the analyser does not guess it from package names, which are not
// unique across the tree.
func ScanFile(rep *Report, service, path, src string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	rep.FilesParsed++

	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			// viper: v.SetDefault("<knob>", <value>)
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "SetDefault" || len(node.Args) != 2 {
				return true
			}
			key, ok := stringLit(node.Args[0])
			if !ok {
				return true
			}
			if _, recognised := knobNames[key]; !recognised {
				return true
			}
			d, ok := durationOf(node.Args[1])
			if !ok {
				return true
			}
			rep.SitesMatched++
			rep.Sites = append(rep.Sites, Site{
				Service: service, Knob: key, File: path,
				Line: fset.Position(node.Pos()).Line, Window: d,
			})
		case *ast.StructType:
			// envconfig: Field time.Duration `envconfig:"<KNOB>" default:"<v>"`
			for _, fld := range node.Fields.List {
				if fld.Tag == nil {
					continue
				}
				raw, err := strconv.Unquote(fld.Tag.Value)
				if err != nil {
					continue
				}
				tag := reflect.StructTag(raw)
				knob := tag.Get("envconfig")
				if _, recognised := knobNames[knob]; !recognised {
					continue
				}
				def := tag.Get("default")
				if def == "" {
					continue
				}
				d, ok := parseWindow(knob, def)
				if !ok {
					continue
				}
				rep.SitesMatched++
				rep.Sites = append(rep.Sites, Site{
					Service: service, Knob: knob, File: path,
					Line: fset.Position(fld.Pos()).Line, Window: d,
				})
			}
		}
		return true
	})
	return nil
}

// stringLit returns the value of a basic string literal.
func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// durationOf resolves the value a viper default was given, in the two shapes
// the tree uses: a quoted duration string ("5s") and a typed multiplication
// (5*time.Second).
func durationOf(e ast.Expr) (time.Duration, bool) {
	if s, ok := stringLit(e); ok {
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, false
		}
		return d, true
	}
	bin, ok := e.(*ast.BinaryExpr)
	if !ok || bin.Op != token.MUL {
		return 0, false
	}
	n, ok := intLit(bin.X)
	if !ok {
		return 0, false
	}
	unit, ok := timeUnit(bin.Y)
	if !ok {
		return 0, false
	}
	return time.Duration(n) * unit, true
}

func intLit(e ast.Expr) (int64, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, false
	}
	n, err := strconv.ParseInt(lit.Value, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// timeUnit resolves a `time.<Unit>` selector to its duration.
func timeUnit(e ast.Expr) (time.Duration, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return 0, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "time" {
		return 0, false
	}
	switch sel.Sel.Name {
	case "Nanosecond":
		return time.Nanosecond, true
	case "Microsecond":
		return time.Microsecond, true
	case "Millisecond":
		return time.Millisecond, true
	case "Second":
		return time.Second, true
	case "Minute":
		return time.Minute, true
	}
	return 0, false
}

// parseWindow reads a struct-tag default. A knob whose name ends in _MS states
// a count of milliseconds; anything else states a Go duration. The unit is
// taken from the KNOB NAME rather than guessed from the digits, so "5000" is
// never silently read as five thousand seconds.
func parseWindow(knob, def string) (time.Duration, bool) {
	if strings.HasSuffix(knob, "_MS") {
		n, err := strconv.ParseInt(def, 10, 64)
		if err != nil {
			return 0, false
		}
		return time.Duration(n) * time.Millisecond, true
	}
	d, err := time.ParseDuration(def)
	if err != nil {
		return 0, false
	}
	return d, true
}

// InheritSite is a construction site that passes a NON-POSITIVE ttl and
// therefore takes the policy default. Such a service has no number of its own,
// which is precisely why it has to be recorded: "this service's window is the
// policy default" is a fact about the tree, and a fact nobody wrote down is one
// nobody can notice changing.
type InheritSite struct {
	Service string
	File    string
	Line    int
}

// ScanInherit parses one Go file and appends every `NewCache(<non-positive
// literal>)` construction site.
//
// It matches the literal argument only. A site passing a VARIABLE (registry's
// `authzCache(opts.CacheTTL)`) is deliberately not an inherit site: its window
// comes from a knob, and the knob is what the census then has to declare. That
// distinction is the whole point — "has no number" and "has a number I did not
// read" must not collapse into the same answer.
func ScanInherit(service, path, src string) ([]InheritSite, int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, 0, fmt.Errorf("parse %s: %w", path, err)
	}
	var out []InheritSite
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		// authz.NewCache(x) or NewCache(x)
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if fn.Sel.Name != "NewCache" {
				return true
			}
		case *ast.Ident:
			if fn.Name != "NewCache" {
				return true
			}
		default:
			return true
		}
		n0, ok := intLit(call.Args[0])
		if !ok || n0 > 0 {
			return true
		}
		out = append(out, InheritSite{
			Service: service, File: path, Line: fset.Position(call.Pos()).Line,
		})
		return true
	})
	return out, 1, nil
}

// ScanConstructors reports whether the file constructs a corelib verdict cache
// at all — `NewCache` / `NewCacheWithLimit`, with ANY argument.
//
// This is the census that does not depend on knowing the knob's name. The
// knob-name list is a closed vocabulary and therefore cannot see a service that
// arrives with a new one; asking instead "does this service build a verdict
// cache" needs no vocabulary, so a service that BUILDS one is caught without
// anybody extending a list.
//
// # What this question cannot see, and where that is answered
//
// This doc used to claim more than the question can deliver: that a seventh
// service is caught "the day it lands". It is caught the day it BUILDS a cache.
// A service could hold a window without building anything — the interceptor
// took its cache as a field, and an unset field was filled in by the
// constructor itself. Such a service named no cache anywhere, so no walk over
// its sources could find one: this census read the file, counted it as
// examined, and pronounced it clean.
//
// The gap is now closed on two levels, neither of them here:
//
//   - ScanImplicitSites (implicit.go) requires every InterceptorOptions literal
//     to NAME the cache field, so the coordinate is reported at test time;
//   - authz.NewInterceptor refuses to start on an unnamed cache, whatever shape
//     the options were assembled in.
//
// The correction is recorded rather than quietly rewritten, because the part
// worth remembering is not the missing check. It is that a check PROMISED to
// catch what it could not see — and a promise like that is worse than silence,
// being exactly what one relies on when deciding no further guard is needed.
func ScanConstructors(path, src string) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "authz" {
			return true
		}
		if sel.Sel.Name == "NewCache" || sel.Sel.Name == "NewCacheWithLimit" {
			found = true
		}
		return true
	})
	return found, nil
}
