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
// Both directions are locked by fixtures in audit_test.go, and against copies of the
// real tree in census_test.go.
//
// # Census
//
// Every run states what it opened — handler files, composition-root files, List RPCs
// discovered, judged and unjudged — on every path, including the one where it refuses.
// The gate used to answer a clean tree with the four characters "OK", which reads the
// same whether it judged five RPCs or never found the tree at all; and when the tree
// was absent it answered with the operating system's word for a missing path and said
// nothing about having inspected nothing. "Zero findings" has to be unreachable from
// "zero read", and the number that makes it so has to be printed.
//
// # Why there is no whitelist here
//
// The sibling gates carry --allow=<resource> for a cluster catalog whose rows are not
// project-scoped, and treat an entry matching nothing as a finding — an exclusion
// lives only while it has a subject. Registry needs no such list: enforcement is
// DECLARED per RPC, so the two failure modes are already covered from both ends. A
// List RPC nobody declared is a finding (it cannot be excluded by omission), and a
// declaration whose handler is gone is a finding (it expires by itself). The census
// therefore reports unjudged RPCs as a number that is never quietly non-zero: every
// one of them is also a finding.
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
	"strings"
)

// ErrNotInspected reports that the gate could not read the tree it was pointed at.
// It is distinct from "the tree has findings": a gate that could not open the code
// has proven nothing, and its caller exits differently to say so.
var ErrNotInspected = errors.New("the tree could not be inspected")

// shape is how a List RPC is expected to enforce visibility.
type shape int

const (
	// rowFilter — the page holds separately-authorizable objects; each row is
	// checked and the response is built from the filtered slice.
	rowFilter shape = iota
	// objectGate — the page lives inside a single authorizable object; that object
	// is checked before the page is read.
	objectGate
	// scopeProperty — сужать НЕЧЕГО, и это свойство ответа, а не послабление.
	//
	// Строки ответа не являются объектами с ИНДИВИДУАЛЬНЫМИ владельцами: они
	// описывают сам объект области, названный запросом (квоты проекта — такое же
	// его свойство, как имя или метки). Вопрос ровно один — читаем ли этот объект
	// вызывающему, — и его решает извлечение области действия на крае. Построчная
	// проверка отсекала бы ноль строк и создавала бы вид контроля.
	//
	// Форма ОБЯЗАНА оставаться редкой: она снимает построчный вопрос, а не
	// отвечает на него. Ставить её там, где у строк есть владельцы, значит
	// открыть проект целиком каждому его участнику.
	scopeProperty
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
	// Чтение квот проекта. Строка квоты — свойство проекта, а не объект с
	// владельцем: сужать нечего, и доступ решает `viewer` на проекте через
	// извлечение области действия на крае (`registry.quotas.list`).
	//
	// Запись истекает со своим методом: снимите RPC — и она станет находкой
	// (перепись `Expired` выше).
	"QuotaHandler.List": {scopeProperty, "", "project named by the request, at the edge"},
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

// Options configures one audit run.
type Options struct {
	// Root is the kacho-registry service root (the directory holding internal/…).
	Root string
}

// Report is the census of one run together with its findings: what the gate opened,
// what it found there, and what it judged. It is printed on every path — see the
// package comment on why "OK" alone is not an answer.
type Report struct {
	HandlerFiles int      // non-test .go files parsed under internal/handler
	RootFiles    int      // non-test .go files parsed under cmd (composition root)
	Listings     []string // List RPCs discovered, "<Receiver>.<Method>", sorted
	Checked      []string // discovered AND declared — the ones actually judged
	Unjudged     []string // discovered, enforcement not declared — each is also a finding
	Expired      []string // declared, handler gone — each is also a finding
	Findings     []string // one line per finding; non-empty ⇒ the gate fails
	// Notes — заявленные исключения, у которых проверена ПРЕДПОСЫЛКА (например: nil
	// достижим только под ручкой, чей отказ в production закреплён тестом). Не роняют
	// вердикт, но печатаются числом: исключение, которого не видно, неотличимо от
	// пропущенной находки, а исключение без предмета обязано становиться находкой.
	Notes []string
}

// Audit inspects the tree at o.Root, writes its census and findings to out, and
// returns the report plus the verdict. A non-nil error wrapping ErrNotInspected means
// the tree could not be read at all — which is a refusal, not a pass.
func Audit(o Options, out io.Writer) (Report, error) {
	root := o.Root
	if root == "" {
		root = "."
	}
	rep, err := analyze(root)
	return finish(rep, out, err)
}

// Analyze inspects the kacho-registry tree rooted at serviceRoot and returns one
// finding per violation, sorted. An empty result means the invariant holds. It is the
// census-free form, kept for callers that only want the verdict; the runnable gate
// goes through Audit so that what it examined is stated.
func Analyze(serviceRoot string) ([]string, error) {
	rep, err := analyze(serviceRoot)
	if err != nil {
		return nil, err
	}
	return rep.Findings, nil
}

// analyze does the work and fills in the census as it goes.
func analyze(serviceRoot string) (Report, error) {
	var rep Report
	fset := token.NewFileSet()

	handlerDir := filepath.Join(serviceRoot, "internal", "handler")
	handlerFiles, parsed, err := parseDir(fset, handlerDir)
	rep.HandlerFiles = parsed
	if err != nil {
		rep.Findings = append(rep.Findings, err.Error())
		// The detail is already on the findings line; the verdict only says which kind
		// of failure this is, so the reader is not shown the same sentence twice.
		return rep, ErrNotInspected
	}

	listMethods, authzMethods := collectMethods(handlerFiles)
	for name := range listMethods {
		rep.Listings = append(rep.Listings, name)
		if _, declared := enforcement[name]; declared {
			rep.Checked = append(rep.Checked, name)
		} else {
			rep.Unjudged = append(rep.Unjudged, name)
		}
	}
	for name := range enforcement {
		if _, ok := listMethods[name]; !ok {
			rep.Expired = append(rep.Expired, name)
		}
	}
	sort.Strings(rep.Listings)
	sort.Strings(rep.Checked)
	sort.Strings(rep.Unjudged)
	sort.Strings(rep.Expired)

	// A handler package that parses but declares no List at all is the same failure
	// one level in: files were read, the surface was not found, and without this the
	// verdict would rest entirely on the declaration table happening to be non-empty.
	if len(rep.Listings) == 0 {
		rep.Findings = append(rep.Findings, fmt.Sprintf(
			"%s: %d file(s) parsed and not one List RPC among them — nothing was judged, so "+
				"nothing can be vouched for", handlerDir, parsed))
	}

	rep.Findings = append(rep.Findings, checkSurfaceIsDeclared(fset, listMethods)...)
	rep.Findings = append(rep.Findings, checkHelpersExist(authzMethods)...)

	for name, fd := range listMethods {
		r, declared := enforcement[name]
		if !declared {
			continue // already reported by checkSurfaceIsDeclared
		}
		rep.Findings = append(rep.Findings, checkListMethod(fset, name, fd, r)...)
	}

	rep.Findings = append(rep.Findings, checkFiltersFailClosed(fset, authzMethods)...)

	cmdDir := filepath.Join(serviceRoot, "cmd")
	wiring, wiringNotes, rootFiles, werr := checkHandlerIsWired(fset, cmdDir)
	rep.RootFiles = rootFiles
	if werr != nil {
		rep.Findings = append(rep.Findings, werr.Error())
		sort.Strings(rep.Findings)
		return rep, ErrNotInspected
	}
	rep.Findings = append(rep.Findings, wiring...)
	rep.Notes = append(rep.Notes, wiringNotes...)
	sort.Strings(rep.Notes)

	sort.Strings(rep.Findings)
	return rep, nil
}

// parseDir parses every non-test .go file directly inside dir and returns them plus
// the number parsed. Comments are NOT retained: the gate must judge code, never prose
// about code.
func parseDir(fset *token.FileSet, dir string) ([]*ast.File, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("%s could not be read (%v) — nothing was inspected, so "+
			"nothing can be vouched for; a gate pointed at the wrong tree must not be "+
			"indistinguishable from a clean one", dir, err)
	}
	var out []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if perr != nil {
			return nil, len(out), fmt.Errorf("parse %s: %w", name, perr)
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, 0, fmt.Errorf("no Go sources under %s — nothing was inspected, so nothing can be vouched for", dir)
	}
	return out, len(out), nil
}

// finish prints the census, then the findings, and turns them into the verdict.
// analyzeErr is non-nil when the tree could not be read; the census still goes out,
// because the number that explains a refusal is the number of files read.
func finish(rep Report, out io.Writer, analyzeErr error) (Report, error) {
	_, _ = fmt.Fprintf(out,
		"audit-list-filter: examined %d handler file(s) and %d composition-root file(s); "+
			"%d List RPC(s) (%d checked, %d unjudged)\n",
		rep.HandlerFiles, rep.RootFiles, len(rep.Listings), len(rep.Checked), len(rep.Unjudged))
	if len(rep.Checked) > 0 {
		_, _ = fmt.Fprintf(out, "audit-list-filter: checked %s\n", strings.Join(rep.Checked, ", "))
	}
	if len(rep.Unjudged) > 0 {
		_, _ = fmt.Fprintf(out, "audit-list-filter: unjudged %s\n", strings.Join(rep.Unjudged, ", "))
	}
	if len(rep.Expired) > 0 {
		_, _ = fmt.Fprintf(out, "audit-list-filter: declared with no handler %s\n", strings.Join(rep.Expired, ", "))
	}

	// Заявленные исключения печатаются числом ДО находок: исключение, которого не
	// видно, неотличимо от пропущенной находки.
	_, _ = fmt.Fprintf(out, "audit-list-filter: %d declared exception(s) with a verified premise\n", len(rep.Notes))
	for _, n := range rep.Notes {
		_, _ = fmt.Fprintf(out, "audit-list-filter: note: %s\n", n)
	}

	for _, f := range rep.Findings {
		_, _ = fmt.Fprintf(out, "audit-list-filter: %s\n", f)
	}
	if analyzeErr != nil {
		return rep, analyzeErr
	}
	if len(rep.Findings) == 0 {
		_, _ = fmt.Fprintln(out, "audit-list-filter: OK")
		return rep, nil
	}
	_, _ = fmt.Fprint(out, explanation)
	// The caller adds the gate's prefix; carrying one here too would print it twice.
	return rep, fmt.Errorf("%d finding(s)", len(rep.Findings))
}

// explanation is the closing note printed under a failing run.
const explanation = `
Every public List must hand back only the objects its caller may see.
In kacho-registry that decision is made in the handler, in one of two shapes:
  - a page of separately-authorizable objects (registries, repositories,
    repo-scoped operations) is filtered row by row, and the response is built
    from the filtered slice;
  - a page that lives inside one object (tags, referrers) is settled by
    checking that object before the page is read.
Enumerating every allowed id is not a substitute: that enumeration is capped
server-side with no continuation token, so the caller's own objects fall
outside the cap and disappear.
`

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
func checkSurfaceIsDeclared(fset *token.FileSet, list map[string]*ast.FuncDecl) []string {
	var out []string
	for name, fd := range list {
		if _, ok := enforcement[name]; !ok {
			out = append(out, fmt.Sprintf(
				"%s: a List RPC with no declared listauthz enforcement — declare in "+
					"tools/auditlistfilter what its caller is checked against before it ships%s",
				name, at(fset, fd.Pos())))
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
		// Форма «свойство области» помощника не имеет by construction: сужать
		// нечего, значит и звать нечего. Требовать от неё имени метода значило бы
		// требовать существования кода, который ничего не делает.
		if r.shape == scopeProperty {
			continue
		}
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
	out := checkNoEnumeration(fset, name, fd)
	switch r.shape {
	case rowFilter:
		return append(out, checkRowFilter(fset, name, fd, r)...)
	case objectGate:
		return append(out, checkObjectGate(fset, name, fd, r)...)
	case scopeProperty:
		// Проверять здесь нечего ПО ПОСТРОЕНИЮ: форма объявляет, что построчного
		// вопроса нет. Единственное, что остаётся обязательным, — запрет
		// перечисления, и он уже применён выше, ДО этого переключателя.
		return out
	}
	return out
}

// checkNoEnumeration rejects "list everything the subject may see, then narrow".
func checkNoEnumeration(fset *token.FileSet, name string, fd *ast.FuncDecl) []string {
	var out []string
	for _, call := range callsNamed(fd.Body, bannedEnumeration) {
		out = append(out, fmt.Sprintf(
			"%s: calls %s — enumerating every allowed id is capped server-side with no "+
				"continuation token, so the caller's own objects fall outside the cap and "+
				"disappear; check the page that was read instead%s",
			name, call, at(fset, fd.Pos())))
	}
	return out
}

// checkRowFilter asserts the page is filtered per object AND that the filtered slice
// is the one the response is built from.
func checkRowFilter(fset *token.FileSet, name string, fd *ast.FuncDecl, r rule) []string {
	where := at(fset, fd.Pos())
	assigns := authzCallAssignments(fd.Body, r.helper)
	if len(assigns) == 0 {
		return []string{fmt.Sprintf(
			"%s: the page is returned without a per-object visibility filter — it must "+
				"pass through h.%s.%s (%s)%s",
			name, authzField, r.helper, r.object, where)}
	}

	ranged := rangedIdents(fd.Body)
	var out []string
	for _, as := range assigns {
		result := assignedName(as)
		if result == "" {
			out = append(out, fmt.Sprintf(
				"%s: the result of h.%s.%s is discarded, so the unfiltered page is what "+
					"the caller receives%s", name, authzField, r.helper, at(fset, as.Pos())))
			continue
		}
		if !ranged[result] {
			out = append(out, fmt.Sprintf(
				"%s: h.%s.%s runs but its result %q is never the collection the response "+
					"is built from — the unfiltered page is what the caller receives%s",
				name, authzField, r.helper, result, at(fset, as.Pos())))
		}
		for _, in := range identArgs(as.Rhs[0].(*ast.CallExpr)) {
			if ranged[in] {
				out = append(out, fmt.Sprintf(
					"%s: the response is built from %q, the unfiltered page handed TO "+
						"h.%s.%s — build it from the filtered result%s",
					name, in, authzField, r.helper, at(fset, as.Pos())))
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
				"pass through h.%s.%s (%s)%s",
			name, authzField, r.helper, r.object, at(fset, fd.Pos()))}
	}

	var out []string

	gatePos := token.NoPos
	for _, as := range assigns {
		if pos := as.Pos(); gatePos == token.NoPos || pos < gatePos {
			gatePos = pos
		}
	}
	for _, call := range bare {
		if pos := call.Pos(); gatePos == token.NoPos || pos < gatePos {
			gatePos = pos
		}
	}

	// Признак «вердикт проверен» берётся ПОСЛЕ вызова гейта и только из ветви, которая
	// покидает метод. Прежде он собирался из ВСЕГО тела: множество всех сравнений
	// `<ident> != nil` где угодно, включая ветку ДРУГОГО вызова и позицию ДО гейта. А
	// имя в Go переиспользуется — `err` есть почти в каждом методе, — поэтому признак
	// выполнялся у метода, который вердикт гейта не смотрел вовсе. Проверка формы без
	// содержания: она держалась на совпадении имени, а не на том, что отказ
	// останавливает ответ.
	checked := map[string]bool{}
	for _, id := range errorTestedLeavingAfter(fd.Body, gatePos) {
		checked[id] = true
	}
	acted := false
	for _, as := range assigns {
		if n := assignedName(as); n != "" && checked[n] {
			acted = true
		}
	}
	if !acted {
		out = append(out, fmt.Sprintf(
			"%s: h.%s.%s is called but its verdict is never tested — a denial does not "+
				"stop the response%s", name, authzField, r.helper, at(fset, gatePos)))
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
func checkFiltersFailClosed(fset *token.FileSet, authz map[string]*ast.FuncDecl) []string {
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
					"vouch for%s", name, at(fset, ret.Pos())))
		})
	}
	return out
}

// checkHandlerIsWired asserts the composition root builds the public handler with a
// real authorizer. A nil one is breakglass: every filter above becomes a no-op. The
// second result is how many files it read, for the census.
func checkHandlerIsWired(fset *token.FileSet, cmdDir string) ([]string, []string, int, error) {
	var files []*ast.File
	var notes []string
	err := filepath.WalkDir(cmdDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
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
		return nil, nil, len(files), fmt.Errorf("%s could not be walked (%v) — the composition root "+
			"was not inspected, so nothing can be vouched for: every List filter is a no-op "+
			"when the handler is built without an authorizer, and that is decided here",
			cmdDir, err)
	}
	if len(files) == 0 {
		return nil, nil, 0, fmt.Errorf("no Go sources under %s — the composition root was not "+
			"inspected, so nothing can be vouched for", cmdDir)
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
			if len(call.Args) < 2 {
				return true
			}
			// АРГУМЕНТ ОТСЛЕЖИВАЕТСЯ КАК ПЕРЕМЕННАЯ. Прежде здесь распознавался только
			// литерал `nil` — конструкция, которую никто не пишет: настоящий корень
			// передаёт переменную. Проверка выглядела включённой и не могла сработать ни
			// разу. Теперь идентификатор разрешается в объемлющей функции: объявление без
			// инициализатора, присваивание только внутри условия, присваивание nil.
			reachable, how := argNilReachable(f, call, 1)
			if !reachable {
				return true
			}
			msg := fmt.Sprintf(
				"%s: NewRegistryHandler can be wired with a nil authorizer (%s) — that is "+
					"breakglass, and it turns every per-object List filter into a no-op",
				fset.Position(call.Pos()).String(), how)
			if how == howConditional && productionRefusalPinned(cmdDir) {
				// ЗАЯВЛЕННОЕ, ПРОВЕРЯЕМОЕ исключение: nil достижим только под ручкой,
				// которую загрузочный страж отвергает в production, и этот отказ
				// закреплён тестом в том же пакете. Исключение не вечное и не на слово:
				// исчезнет тест — запись станет находкой. Печатается числом, поэтому
				// невидимой быть не может.
				notes = append(notes, msg+" — не фатально, пока отказ ручки в production "+
					"закреплён тестом композиционного корня (имя содержит Breakglass и Production)")
				return true
			}
			out = append(out, msg)
			return true
		})
	}
	if !found {
		out = append(out, fmt.Sprintf(
			"%s: no call to NewRegistryHandler — the public handler, and with it every "+
				"List filter, is never wired", cmdDir))
	}
	return out, notes, len(files), nil
}

// howLiteral / howAssignedNil / howConditional — как именно nil достижим у аргумента.
const (
	howLiteral     = "a literal nil at the call site"
	howAssignedNil = "assigned nil in the enclosing function"
	howConditional = "declared without an initializer and assigned only inside a conditional"
)

// argNilReachable resolves argument argIdx of call and reports whether it can be nil.
// A direct constructor call cannot; an identifier is traced inside the function that
// contains the call: `var x T` with every assignment nested in a conditional leaves nil
// reachable on the path where the condition does not hold.
func argNilReachable(f *ast.File, call *ast.CallExpr, argIdx int) (bool, string) {
	if argIdx >= len(call.Args) {
		return false, ""
	}
	arg := call.Args[argIdx]
	if isNil(arg) {
		return true, howLiteral
	}
	id, ok := arg.(*ast.Ident)
	if !ok {
		return false, ""
	}
	fd := enclosingFunc(f, call.Pos())
	if fd == nil || fd.Body == nil {
		return false, ""
	}

	declaredNoInit := false
	assignedNil := false
	unconditional := false

	// Верхний уровень тела функции: только здесь присваивание безусловно.
	for _, st := range fd.Body.List {
		if st.Pos() > call.Pos() {
			break
		}
		switch v := st.(type) {
		case *ast.DeclStmt:
			gd, ok := v.Decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, n := range vs.Names {
					if n.Name != id.Name {
						continue
					}
					if len(vs.Values) == 0 {
						declaredNoInit = true
					} else if isNil(vs.Values[0]) {
						assignedNil = true
					} else {
						unconditional = true
					}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				n, ok := lhs.(*ast.Ident)
				if !ok || n.Name != id.Name {
					continue
				}
				if i < len(v.Rhs) && isNil(v.Rhs[i]) {
					assignedNil = true
				} else {
					unconditional = true
				}
			}
		}
	}

	// Присваивание nil где угодно (в том числе внутри условия) делает nil достижимым.
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			l, ok := lhs.(*ast.Ident)
			if ok && l.Name == id.Name && i < len(as.Rhs) && isNil(as.Rhs[i]) {
				assignedNil = true
			}
		}
		return true
	})

	switch {
	case assignedNil:
		return true, howAssignedNil
	case declaredNoInit && !unconditional:
		return true, howConditional
	default:
		return false, ""
	}
}

// enclosingFunc returns the function declaration whose body contains pos.
func enclosingFunc(f *ast.File, pos token.Pos) *ast.FuncDecl {
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		if fd.Body.Pos() <= pos && pos <= fd.Body.End() {
			return fd
		}
	}
	return nil
}

// productionRefusalPinned reports whether the composition-root package pins, by test,
// that the knob which can leave the authorizer nil is REFUSED in production. This is
// the premise of the non-fatal note above: if the pin disappears, the note must become
// a finding, so the exclusion cannot outlive its subject.
func productionRefusalPinned(cmdDir string) bool {
	pinned := false
	_ = filepath.WalkDir(cmdDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// #nosec G304,G122 -- гейт CI читает СВОЙ ЖЕ рабочий каталог: путь приходит из
		// обхода дерева этого репозитория, а не из запроса. Правила видят обход и
		// переменную в имени файла; иного входа у инструмента нет по построению.
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(line, "func Test") {
				continue
			}
			if strings.Contains(line, "Breakglass") && strings.Contains(line, "Production") {
				pinned = true
			}
		}
		return nil
	})
	return pinned
}

// ---------------------------------------------------------------------------
// AST helpers
// ---------------------------------------------------------------------------

// at renders a source coordinate for a finding. A red that names the rule but not the
// place leaves the reader to find it, and the place is what makes the finding
// actionable.
func at(fset *token.FileSet, pos token.Pos) string {
	if pos == token.NoPos {
		return ""
	}
	return "\n  at " + fset.Position(pos).String()
}

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
// errorTestedLeavingAfter returns the identifiers compared `!= nil` in an `if` that
// (a) stands AFTER `after` and (b) leaves the method from its body (return/panic).
// Both conditions matter. Without (a) a comparison belonging to an earlier, unrelated
// call satisfies the signal, and Go reuses `err` everywhere. Without (b) a branch that
// merely logs the denial and falls through counts as acting on it, while the page is
// returned anyway — which is the very thing this gate exists to forbid.
func errorTestedLeavingAfter(body *ast.BlockStmt, after token.Pos) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if after != token.NoPos && ifs.Pos() <= after {
			return true
		}
		bin, ok := ifs.Cond.(*ast.BinaryExpr)
		if !ok || bin.Op != token.NEQ {
			return true
		}
		id, ok := bin.X.(*ast.Ident)
		if !ok || !isNil(bin.Y) {
			return true
		}
		if !leavesMethod(ifs.Body) {
			return true
		}
		out = append(out, id.Name)
		return true
	})
	return out
}

// leavesMethod reports whether the block certainly ends the method (a return, or a
// panic). A branch that logs and continues does not.
func leavesMethod(b *ast.BlockStmt) bool {
	found := false
	ast.Inspect(b, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.ReturnStmt:
			found = true
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "panic" {
				found = true
			}
		}
		return !found
	})
	return found
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
