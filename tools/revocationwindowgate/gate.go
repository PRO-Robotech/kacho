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
// # A seventh holder, invisible to both of this package's questions
//
// The census as first written measured SERVICES, by two predicates that both
// described how a cache is MADE rather than what it HOLDS: the walk started at
// the `services/` directory, and the constructor was looked for by the name
// `authz.NewCache`. The edge (`gateway/`) satisfies neither — it lives outside
// `services/` and builds its decision cache with its own constructor over a
// local LRU. So the process through which every external request passes held a
// full window while contributing zero files to every walk, and its number was
// written down nowhere.
//
// This was not an oversight in the walks. The edge lay outside what they could
// express, and a question that cannot reach its subject reports silence — which
// reads exactly like "clean". Both predicates are now sets: the scan roots
// include the edge, and verdictCacheCtorNames names constructors beyond
// corelib's, matched whether the call is qualified or not.
//
// # Two things that fix did not buy, stated rather than promised away
//
// The first draft of the correction claimed the question "does this process
// build a verdict cache" needed no vocabulary. It does. It is a vocabulary of
// CONSTRUCTOR names instead of knob names, and a cache built under a brand-new
// name is outside it exactly as the edge once was. Saying otherwise would be
// the same failure the section above records — a check PROMISING to catch what
// it cannot see — one iteration later. What the fix does buy is that moving a
// known constructor into a package of its own no longer retires it, because the
// package qualifier is no longer read.
//
// The second: the process is the wrong UNIT for the size question. A process
// already present in the census could add a second window of any magnitude and
// stay green, since "does it build a cache" is answered once per process and
// never again. Size is therefore asked separately, of the WINDOW rather than
// the process, and by SHAPE rather than by list — see KnobSizesAuthzWindow,
// whose controls run in both directions.
//
// The edge differs from the services in one further respect worth keeping
// straight: it is the only holder with a PROACTIVE drop, and it has two lanes —
// the replica that served the mutation flushes at once, and its siblings
// converge by reading the iam subject_change journal, a read THE EDGE ITSELF
// opens (kacho#1024 turned the direction around; the push that used to run from
// the rights owner was an edge from a leaf back to its own consumer). Its TTL is
// therefore a backstop rather than the primary path — and a backstop is still a
// window, still owned by nobody until it is written down.
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
// grant does not make a zone stop existing. The same line separates the edge's
// credential caches — token introspection, Kratos session, DPoP replay — from
// its decision cache: revoking a CREDENTIAL is a different lane with its own
// immediate mechanism, and folding the two together would push someone to shrink
// the grant window hoping to fix a problem it never governed.
//
// Recognition is therefore by what the cache HOLDS, expressed as two named sets
// — the declared knob names and the verdict-cache constructors — and the gate
// reports what it matched, so a rename shows up as a premise failure rather than
// as silence.
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
//
// The shape predicate carries its own premise separately: it must find every
// knob the policy already names and stay silent on knobs that size something
// else. Both halves are asserted, because a discriminator proven in one
// direction only measures nothing.
package revocationwindowgate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"regexp"
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
	"KACHO_COMPUTE_AUTHZ_CACHE_TTL":  "check",
	"KACHO_STORAGE_AUTHZ_CACHE_TTL":  "check",
	"KACHO_GEO_AUTHZ_CACHE_TTL":      "check",
	// per-object List visibility cache (internal/authzfilter)
	"authz.list-filter.cache-ttl":            "list-filter",
	"KACHO_COMPUTE_LIST_FILTER_CACHE_TTL_MS": "list-filter",
	"KACHO_STORAGE_LIST_FILTER_CACHE_TTL_MS": "list-filter",
	// per-request decision cache at the EDGE (gateway/internal/middleware).
	// The edge is the only site with a proactive drop (self-flush on the serving
	// replica plus the edge's own cursor read of the iam subject_change journal),
	// so its TTL is the BACKSTOP window — what remains when that read is behind
	// or down. A backstop is
	// still a window, and an undeclared backstop is still an unowned security
	// parameter, which is why it belongs in the same census as the rest.
	"KACHO_API_GATEWAY_AUTHZ_CACHE_TTL_SECONDS": "edge-decision",
}

// verdictCacheCtorNames — constructor names whose product stores an
// authorization verdict, and which therefore open a revocation window.
//
// Membership is by what the cache HOLDS, not by where it lives and not by how
// it is called: the same name counts qualified (`pkg.NewCache`) and unqualified
// (`newDecisionCache`), because which of the two a call site uses is a fact
// about package layout, not about security posture.
//
// A cache of CREDENTIALS — token introspection, Kratos session, DPoP replay —
// is not a member: revoking a credential is a different lane with its own
// immediate mechanism, and folding the two together pushes people to shrink
// this window hoping to fix a problem it never governed.
//
// This IS a vocabulary, and calling it anything else would repeat the mistake
// this file already records once. A cache built under a brand-new constructor
// name is not seen here — that bound is real, it is stated rather than
// promised away, and it is the reason the size question is asked separately by
// KnobSizesAuthzWindow, which needs no vocabulary at all.
var verdictCacheCtorNames = map[string]bool{
	"NewCache":          true, // pkg/authz (corelib)
	"NewCacheWithLimit": true, // pkg/authz (corelib)
	"newDecisionCache":  true, // gateway/internal/middleware/authz_cache.go
}

// VerdictCacheCtorNames returns the recognised constructor names, sorted — so a
// caller can state the premise it relies on.
func VerdictCacheCtorNames() []string {
	out := make([]string, 0, len(verdictCacheCtorNames))
	for k := range verdictCacheCtorNames {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// authzWindowKnobMarker / ttlKnobMarker — the two halves of the SHAPE by which
// a configuration knob is recognised as sizing an authorization-verdict window,
// without consulting any list of knob names.
//
// This exists because the size question was asked through a closed dictionary:
// a process already present in the census could add a second window of any
// magnitude under a knob nobody had listed, and nothing went red. The unit of
// the census has to be the WINDOW, not the process.
//
// The shape is checked in both directions by
// TestKnobShapePredicateHasControlsBothWays: it must find every knob the policy
// already names, and must stay silent on knobs that size something else
// (session TTL, DPoP replay, HTTP timeouts).
var (
	authzWindowKnobMarker = regexp.MustCompile(`AUTHZ|LIST_FILTER|authz\.|list-filter`)
	ttlKnobMarker         = regexp.MustCompile(`TTL|ttl`)
)

// KnobSizesAuthzWindow reports whether a knob NAME says it sizes an
// authorization-verdict cache window. Name shape only — no list of knobs.
func KnobSizesAuthzWindow(knob string) bool {
	return authzWindowKnobMarker.MatchString(knob) && ttlKnobMarker.MatchString(knob)
}

// ScanWindowKnobNames returns every configuration knob declared in one file
// whose NAME says it sizes an authorization-verdict window — whether or not the
// knob is one the policy already knows about. Both declaration shapes the tree
// uses are read: the viper default and the envconfig struct tag.
func ScanWindowKnobNames(path, src string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	seen := map[string]bool{}
	var out []string
	note := func(k string) {
		if k == "" || seen[k] || !KnobSizesAuthzWindow(k) {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "SetDefault" || len(node.Args) == 0 {
				return true
			}
			if key, kok := stringLit(node.Args[0]); kok {
				note(key)
			}
		case *ast.StructType:
			for _, fld := range node.Fields.List {
				if fld.Tag == nil {
					continue
				}
				raw, uerr := strconv.Unquote(fld.Tag.Value)
				if uerr != nil {
					continue
				}
				note(reflect.StructTag(raw).Get("envconfig"))
			}
		}
		return true
	})
	sort.Strings(out)
	return out, nil
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
// a count of milliseconds, one ending in _SECONDS a count of seconds; anything
// else states a Go duration. The unit is taken from the KNOB NAME rather than
// guessed from the digits, so "5000" is never silently read as five thousand
// seconds — and, symmetrically, the edge's bare "5" is never read as 5ns.
//
// That second half is not hypothetical. Before _SECONDS was recognised, the
// edge knob's default parsed as neither a duration nor a millisecond count, so
// parseWindow returned false and the site was skipped — silently. A census
// whose parser declines to read a declaration does not report a gap; it reports
// nothing at all, which is indistinguishable from the site not existing.
func parseWindow(knob, def string) (time.Duration, bool) {
	if strings.HasSuffix(knob, "_MS") {
		n, err := strconv.ParseInt(def, 10, 64)
		if err != nil {
			return 0, false
		}
		return time.Duration(n) * time.Millisecond, true
	}
	if strings.HasSuffix(knob, "_SECONDS") {
		n, err := strconv.ParseInt(def, 10, 64)
		if err != nil {
			return 0, false
		}
		return time.Duration(n) * time.Second, true
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
//
// # The second thing this question could not see: another implementation
//
// "Does this process build a verdict cache" needs no knob vocabulary, which is
// what makes it strong. But it was asked as "does it call `authz.NewCache`" —
// and that is a vocabulary too, just of constructors instead of knob names. The
// edge (gateway/internal/middleware) caches decisions over its own LRU
// primitive and never calls the corelib constructor, so it answered "no" while
// holding a full window. It was not overlooked by the walk; it was outside what
// the walk could express.
//
// Hence the recognised set below is a SET, named and extensible, rather than
// one hard-coded name — and each member is a constructor whose product stores a
// positive authorization verdict. Caches of credentials (token introspection,
// session, DPoP replay) are deliberately absent: those ride the credential
// revocation lane, which is immediate by a different mechanism entirely. See
// authz.RevocationPolicy, section "Что НЕ ездит по этому окну".
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
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			// A QUALIFIED call. The package qualifier is deliberately NOT
			// examined. Pinning it to "authz" made the answer depend on where
			// the constructor lives rather than on what it builds: moving the
			// very same cache into a package of its own took the process out
			// of the census, silently. Extraction into a package is ordinary
			// refactoring — the tree already does it twice next door — and it
			// must not be able to retire a security parameter.
			found = found || verdictCacheCtorNames[fn.Sel.Name]
		case *ast.Ident:
			// The same name, called unqualified from inside its own package.
			found = found || verdictCacheCtorNames[fn.Name]
		}
		return true
	})
	return found, nil
}
