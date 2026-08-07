// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package paginationordergate keeps two things from happening to a page request
// before anyone has judged whether it is well formed: being coerced, and being
// answered by the access decision.
//
// # The subject
//
// A list RPC is asked two independent questions in one call — is the REQUEST well
// formed, and is the CALLER allowed to see the rows. The first has one correct
// answer for every caller. `api-conventions.md` states the order for exactly that
// reason: format-validate → authz → repo. Reverse it and the same garbage cursor
// gets INVALID_ARGUMENT for a caller with grants and an empty page, or a denial, for
// a caller without them — the caller cannot tell "your cursor is broken" from "you
// may not look here", and neither can the operator reading the logs.
//
// The quieter half is about `page_size`. Out of range must be REFUSED, not silently
// coerced ("значение вне диапазона отвергается, а не clamp'ится"). A saturating
// conversion applied to the raw request destroys the evidence BEFORE any validator
// sees it: a negative page size becomes 0, 0 means "apply the default", and the
// caller is served a page it never asked for and is never told. The validator is
// still present, still correct, still documented — and can no longer fire. That is
// [[checks-with-form-but-no-substance]] one level down: the check is not missing, it
// is unreachable.
//
// # The two rules, and why each is decidable HERE
//
// Both rules judge facts that are settled inside the one body, so neither depends on
// following a delegation:
//
//   - COERCION. A saturating conversion of the raw page size that is not preceded,
//     in the same body, by a validator of that raw value. No downstream validator
//     can refuse a value that no longer exists, so no hop could clear this.
//   - ORDER. An access decision taken before the page request is handed to anything
//     that could judge it. The decision has already happened here; what a later
//     callee validates cannot un-take it.
//
// ORDER deliberately separates a GATE from a FILTER, and the difference is
// positional, not nominal. `h.authz.namespaceGate(...)` standing in front of the read
// decides access first — a finding. `h.authz.filterRegistries(ctx, items)` standing
// after it narrows rows the read already produced, and the read is where the format
// question was answered — silent. Both are calls through the same field, with names
// no vocabulary could tell apart; only their position relative to the call that
// receives the page separates them.
//
// # What it deliberately does NOT judge, and why that is stated rather than hidden
//
// A thin transport that hands the raw values to a use-case which validates them
// first is CORRECT — geo, storage, vpc and nlb are laid out that way, and every
// per-resource ListOperations reaches `operations.ListForCaller`, which validates
// the page before the ownership short-circuit. Demanding the validator in the
// handler would report all of them, and a gate whose findings are mostly layering
// style gets switched off.
//
// The price is a real blind spot, and it is named rather than papered over: a thin
// handler delegating to a use-case that validates NOTHING is not caught by this
// gate. Closing it needs the callee resolved by type, which this analyser does not
// do. What holds that half today is the per-service list gates plus the use-case
// tests; what would hold it mechanically is a follow-up, not a claim made here.
//
// # Why this parses instead of grepping
//
//   - comments are not code. The paragraph explaining why the format check comes
//     first sits directly above the call; a text search finds it and stays green
//     after the call itself is deleted;
//   - order is a question about positions inside one body, not about which line
//     comes first in a file holding a dozen methods;
//   - "the validator was handed the RAW getter" is a question about an expression.
//     `shared.ValidatePagination(f.PageToken, f.PageSize)` on an already-coerced
//     struct is textually a validator call in the right place and validates nothing
//     the caller sent. On the branch this gate landed on, 14 iam list RPCs coerced
//     the raw page size before anyone judged it, and in six of them a correct,
//     documented validator sat downstream and could not fire for any negative value.
//     A gate keyed on the call's NAME would have called all six clean.
//
// The shape of the class is worth naming: `RoleService.List` had been fixed on its
// own (issue #184, validate first, then convert) and its fourteen siblings had not.
// One instance closed, the class alive — which is why this is a gate and not a diff.
//
// # Census and premises
//
// The report states what was examined on every path — roots, files parsed, methods
// with a receiver, paginated methods judged, coercions of a page value seen, access
// decisions seen — so "no findings" is never reachable from "nothing read".
//
// Two premises are checked and reported, because each is a fact about the tree that
// can drift:
//
//  1. a paginated request is recognisable by the generated getters GetPageSize /
//     GetPageToken. Zero paginated methods judged means protoc's accessor names
//     changed, and the gate says so instead of passing;
//  2. an in-handler access decision is recognisable as a call reached through a
//     receiver field named `authz` (registry: h.authz.namespaceGate / checkRepo /
//     checkRepository / filterRepos). Zero such calls anywhere in the walk means the
//     convention moved and the ORDER rule has lost its subject.
package paginationordergate

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

// The protoc-generated accessors that mark a request as paginated. The spelling is
// the wire contract's, not a local habit.
const (
	getPageSize  = "GetPageSize"
	getPageToken = "GetPageToken"
)

// authzFieldName is premise 2: the receiver field through which a handler reaches
// its access decision.
const authzFieldName = "authz"

// isPageValidator recognises a call that answers the page-format question:
// `validate.PageSize`, or anything named Validate…Page…/Validate…Pagination…
// (ValidatePageToken, ValidatePagination, ValidateRepoListPage).
//
// The name is the CHEAP half of the test and is deliberately generous — services
// name their wrappers differently and a closed list would go stale silently. The
// load-bearing half is the argument: the call only counts when the RAW getter
// travels into it (carriesRawGetter). A perfectly-named validator handed a value
// someone already coerced still counts for nothing.
// Регистр НЕ учитывается, и это не косметика. Прежний предикат требовал заглавной
// `Validate`, поэтому идиоматичный неэкспортируемый хелпер `validatePagination`
// не опознавался вовсе: один и тот же хендлер с ПРАВИЛЬНЫМ порядком операций давал
// две находки `access-before-format`, если валидатор назван со строчной, и ноль —
// если с заглавной (доказано инъекцией на ревизии d24476c1, issue #111). Смещение
// шло в сторону ЛОЖНОЙ находки, а гейт, который краснеет на верном коде, снимают
// быстрее, чем чинят, — и вместе с ним уходит настоящая проверка.
//
// Несущая половина теста от этого не меняется: имя — дешёвая половина, а считается
// вызов только когда в него едет СЫРОЙ геттер (carriesRawGetter). Идеально
// названный валидатор, которому подали уже приведённое значение, не значит ничего.
func isPageValidator(name string) bool {
	if name == "PageSize" { // pkg/validate.PageSize — the platform predicate
		return true
	}
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "validate") &&
		(strings.Contains(lower, "page") || strings.Contains(lower, "pagination"))
}

// Rule names the reason a method was reported.
type Rule string

const (
	// RuleCoercion — the raw page size was saturated before it was judged.
	RuleCoercion Rule = "coercion-before-judgement"
	// RuleOrder — the access decision was taken before the format question.
	RuleOrder Rule = "access-before-format"
)

// Finding is one method whose page-format answer is no longer independent.
type Finding struct {
	Pos     string // file:line:col of the offending call
	Method  string // Receiver.Method
	Subject string // "page_size" | "page_token"
	Rule    Rule
	Detail  string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s (%s) [%s] — %s", f.Pos, f.Method, f.Subject, f.Rule, f.Detail)
}

// Report carries the findings AND the census of what produced them.
type Report struct {
	Roots      []string
	Files      int
	Methods    int // methods with a receiver — the candidate set
	Paginated  int // of those, the ones that read a pagination getter
	Coercions  int // saturating conversions of a raw page value seen
	AuthzCalls int // calls reached through a field named `authz` seen anywhere
	Findings   []Finding
}

// Census is the sentence the gate prints on every path, clean runs included.
func (r Report) Census() string {
	return fmt.Sprintf(
		"paginationordergate: %d root(s) [%s], %d file(s) parsed, %d method(s) with a receiver, "+
			"%d paginated method(s) judged, %d coercion(s) of a raw page value, "+
			"%d call(s) through an `%s` field, %d finding(s)",
		len(r.Roots), strings.Join(r.Roots, " "), r.Files, r.Methods, r.Paginated,
		r.Coercions, r.AuthzCalls, authzFieldName, len(r.Findings))
}

// PremiseFailures returns the premises that no longer hold. Non-empty means the walk
// cannot vouch for anything — a finding, not a pass.
func (r Report) PremiseFailures() []string {
	var out []string
	if r.Files == 0 {
		out = append(out, "no files parsed: the roots do not point at the tree")
	}
	if r.Files > 0 && r.Paginated == 0 {
		out = append(out, "no paginated method judged: "+getPageSize+"/"+getPageToken+
			" no longer name the generated accessors, so the subject set is empty")
	}
	if r.Files > 0 && r.AuthzCalls == 0 {
		out = append(out, "no call reached through a field named `"+authzFieldName+
			"`: the ORDER rule has lost its subject, so an in-handler access decision "+
			"is no longer recognisable")
	}
	return out
}

// Analyse walks the roots and judges every paginated method it finds.
func Analyse(roots ...string) (Report, error) {
	rep := Report{Roots: roots}
	fset := token.NewFileSet()

	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipDir(d.Name()) {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if perr != nil {
				return fmt.Errorf("parse %s: %w", path, perr)
			}
			rep.Files++
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || fn.Body == nil {
					continue
				}
				rep.Methods++
				rep.AuthzCalls += countAuthzCalls(fn)
				m := judge(fset, fn)
				if !m.paginated {
					continue
				}
				rep.Paginated++
				rep.Coercions += m.coercions
				rep.Findings = append(rep.Findings, m.findings...)
			}
			return nil
		})
		if err != nil {
			return rep, err
		}
	}
	sort.Slice(rep.Findings, func(i, j int) bool {
		if rep.Findings[i].Pos != rep.Findings[j].Pos {
			return rep.Findings[i].Pos < rep.Findings[j].Pos
		}
		return rep.Findings[i].Subject < rep.Findings[j].Subject
	})
	return rep, nil
}

// skipDir keeps the walk off generated stubs and vendored trees. The generated
// pb.go files DECLARE the getters; judging them would be judging protoc.
func skipDir(name string) bool {
	switch name {
	case "vendor", "node_modules", ".git", "testdata", "gen", "ui-future":
		return true
	}
	return false
}

// verdict is one method's analysis.
type verdict struct {
	paginated bool
	coercions int
	findings  []Finding
}

func judge(fset *token.FileSet, fn *ast.FuncDecl) verdict {
	params := paramNames(fn)
	recv := receiverName(fn)

	var (
		readsSize, readsToken bool
		vSize, vToken         token.Pos
		clampPos              token.Pos
		clampName             string
		authzPos              token.Pos
		authzName             string
		handOffPos            token.Pos
		coercions             int
	)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, which, ok := paginationGetterCall(call, params); ok {
			switch which {
			case getPageSize:
				readsSize = true
			case getPageToken:
				readsToken = true
			}
			return true
		}
		// The function's name, whether it is package-qualified (`validate.PageSize`)
		// or package-local (`validatePageSize`). Both spellings occur: registry keeps
		// its page predicates inside the use-case package. Judging only the qualified
		// form would report a handler whose validator simply lives next door.
		name, sel := funcName(call)
		if name == "" {
			return true
		}

		carriesSize := carriesRawGetter(call, params, getPageSize)
		carriesToken := carriesRawGetter(call, params, getPageToken)

		if isPageValidator(name) {
			if carriesSize {
				vSize = earliest(vSize, call.Pos())
			}
			if carriesToken {
				vToken = earliest(vToken, call.Pos())
			}
		}
		if strings.HasPrefix(name, "Clamp") && carriesSize {
			coercions++
			if p := earliest(clampPos, call.Pos()); p != clampPos {
				clampPos, clampName = p, name
			}
		}
		// Only a selector can be reached through the receiver, so a bare identifier
		// is neither an access decision nor a hand-off.
		if recv == "" || sel == nil {
			return true
		}
		switch {
		case reachesThroughField(sel.X, recv, authzFieldName):
			if p := earliest(authzPos, call.Pos()); p != authzPos {
				authzPos, authzName = p, exprPath(sel)
			}
		case (carriesSize || carriesToken) && reachesReceiver(sel.X, recv):
			// The page request is handed to a collaborator: from here on the format
			// question travels with it, and this analyser stops following.
			handOffPos = earliest(handOffPos, call.Pos())
		}
		return true
	})

	if !readsSize && !readsToken {
		return verdict{}
	}

	v := verdict{paginated: true, coercions: coercions}
	method := receiverTypeName(fn) + "." + fn.Name.Name

	// COERCION — decidable here, and no hop can clear it.
	if clampPos.IsValid() && (!vSize.IsValid() || vSize > clampPos) {
		v.findings = append(v.findings, Finding{
			Pos:     fset.Position(clampPos).String(),
			Method:  method,
			Subject: "page_size",
			Rule:    RuleCoercion,
			Detail: fmt.Sprintf("%s saturates the raw page size before any validator judges it; "+
				"an out-of-range value becomes an in-range one and is served silently. "+
				"Validate the raw request value first (validate.PageSize), then convert", clampName),
		})
	}

	// ORDER — the decision is already taken in this body, and taken FIRST. An
	// `authz` call that follows the hand-off is a row filter over a page the read
	// already produced, and the read is where the format question was answered.
	if authzPos.IsValid() && (!handOffPos.IsValid() || authzPos < handOffPos) {
		if readsSize && (!vSize.IsValid() || vSize > authzPos) {
			v.findings = append(v.findings, Finding{
				Pos:     fset.Position(authzPos).String(),
				Method:  method,
				Subject: "page_size",
				Rule:    RuleOrder,
				Detail: fmt.Sprintf("%s decides access before page_size is judged; the answer to a "+
					"malformed page then depends on what the caller may see", authzName),
			})
		}
		if readsToken && (!vToken.IsValid() || vToken > authzPos) {
			v.findings = append(v.findings, Finding{
				Pos:     fset.Position(authzPos).String(),
				Method:  method,
				Subject: "page_token",
				Rule:    RuleOrder,
				Detail: fmt.Sprintf("%s decides access before page_token is judged; the same garbage "+
					"cursor then answers differently depending on the caller's grants", authzName),
			})
		}
	}
	return v
}

// funcName returns the called function's name plus the selector it came from, or
// ("", nil) when the callee is not a plain name at all (a call through a value in a
// map, a func literal). The selector is nil for a package-local call.
func funcName(call *ast.CallExpr) (string, *ast.SelectorExpr) {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		return fn.Sel.Name, fn
	case *ast.Ident:
		return fn.Name, nil
	default:
		return "", nil
	}
}

// countAuthzCalls feeds premise 2: how many calls in this method are reached
// through a field named `authz`, paginated or not.
func countAuthzCalls(fn *ast.FuncDecl) int {
	recv := receiverName(fn)
	if recv == "" {
		return 0
	}
	n := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && reachesThroughField(sel.X, recv, authzFieldName) {
			n++
		}
		return true
	})
	return n
}

// earliest returns the earlier of two positions, treating the invalid one as later.
func earliest(cur, candidate token.Pos) token.Pos {
	if !cur.IsValid() || candidate < cur {
		return candidate
	}
	return cur
}

// reachesReceiver reports whether the call's target is rooted at the receiver —
// `h.uc`, `h.list`, or the receiver itself.
func reachesReceiver(expr ast.Expr, recv string) bool {
	root, ok := rootIdent(expr)
	return ok && root == recv
}

// reachesThroughField reports whether expr is `<recv>.<field>` (possibly nested),
// i.e. the call is made on that field of the receiver.
func reachesThroughField(expr ast.Expr, recv, field string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != field {
		return false
	}
	root, ok := rootIdent(sel.X)
	return ok && root == recv
}

// rootIdent unwraps a selector chain to its leftmost identifier.
func rootIdent(e ast.Expr) (string, bool) {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name, true
		case *ast.SelectorExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		default:
			return "", false
		}
	}
}

// exprPath renders a selector chain for a message ("h.authz.namespaceGate").
func exprPath(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprPath(x.X) + "." + x.Sel.Name
	default:
		return "?"
	}
}

// paginationGetterCall matches `<param>.GetPageSize()` / `<param>.GetPageToken()`.
func paginationGetterCall(call *ast.CallExpr, params map[string]bool) (string, string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	if sel.Sel.Name != getPageSize && sel.Sel.Name != getPageToken {
		return "", "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || !params[id.Name] {
		return "", "", false
	}
	return id.Name, sel.Sel.Name, true
}

// carriesRawGetter reports whether the raw getter call appears anywhere inside the
// call's ARGUMENTS — directly, wrapped in a conversion (`int64(req.GetPageSize())`),
// or as a field of a composite literal (`Pagination{PageSize: req.GetPageSize()}`).
//
// What it deliberately does NOT accept is a value that merely once held the getter's
// result. `shared.ValidatePagination(f.PageToken, f.PageSize)` on a struct someone
// already coerced names a validator and validates nothing the caller sent; that
// distinction is the whole reason this gate parses.
func carriesRawGetter(call *ast.CallExpr, params map[string]bool, getter string) bool {
	found := false
	for _, a := range call.Args {
		ast.Inspect(a, func(n ast.Node) bool {
			if found {
				return false
			}
			inner, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if _, which, ok := paginationGetterCall(inner, params); ok && which == getter {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// paramNames collects the method's parameter identifiers: the request message is one
// of them, and a getter on anything else is not this request's page.
func paramNames(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	if fn.Type.Params == nil {
		return out
	}
	for _, f := range fn.Type.Params.List {
		for _, n := range f.Names {
			if n.Name != "_" {
				out[n.Name] = true
			}
		}
	}
	return out
}

// receiverName returns the receiver VARIABLE name ("h"), empty when unnamed.
func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 || len(fn.Recv.List[0].Names) == 0 {
		return ""
	}
	if n := fn.Recv.List[0].Names[0].Name; n != "_" {
		return n
	}
	return ""
}

// receiverTypeName returns the receiver's TYPE name, star removed.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	e := fn.Recv.List[0].Type
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "?"
}
