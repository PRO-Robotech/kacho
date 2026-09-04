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
//     per-object visibility filter (listnarrow.Page /
//     listnarrow.IDs → kacho-iam AuthorizeService.BatchCheck).
//
// (1)+(2) are project scope. They answer "whose project is this", never "may
// this caller see THESE objects" — without (3) every project member sees every
// row of the project, which is precisely how this service shipped its List.
//
// Shape (3) must be "read a page by cursor → batch-check its ids". The inverse —
// ListAllowedIDs/ListObjects ("enumerate everything the subject may see", then
// narrow the SQL to that set) — is rejected explicitly. It was the external
// relations engine that capped such an enumeration server-side with no
// continuation token, so a tenant's own resource silently fell outside the prefix
// and stayed invisible while its grant and its row both existed. The engine is
// gone; the rejection is not, and it never rested on the cap alone — "enumerate
// everything visible" is unbounded by construction, and an answer to it is not a
// page. The shape is gone from every sibling service; the gate names it so it
// cannot return "as an equivalent".
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
// listnarrow.IDs can never satisfy check (3), and one explaining why
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
	// transportRoot holds the gRPC handlers — the surface a caller actually reaches.
	transportRoot = "internal/handler"
)

// projectNarrowRe matches a `project_id = $…` predicate, with or without a table
// alias (`v.project_id = $1`). It is applied ONLY to string literals found inside
// the List body, so neither a comment nor a predicate in a sibling method counts.
var projectNarrowRe = regexp.MustCompile(`(?:[A-Za-z_][A-Za-z0-9_]*\.)?project_id\s*=\s*\$`)

// perObjectFilters are the accepted per-object visibility calls (check 3).
var perObjectFilters = map[string]bool{
	"Page": true, // listnarrow.Page — сужатель общего фундамента
	"IDs":  true, // listnarrow.IDs — он же над голыми идентификаторами
}

// enumerateThenNarrow are the rejected enumerate-all-allowed-ids calls (check 3a).
var enumerateThenNarrow = map[string]bool{
	"ListAllowedIDs": true,
	"ListObjects":    true,
}

// subjectScopers narrow a page by the AUTHENTICATED caller taken from the context,
// rather than by an id the request supplied. operations.ListForCaller is the one
// this service uses.
var subjectScopers = map[string]bool{"ListForCaller": true}

// shape is how one listing method's visibility is decided.
type Shape int

const (
	// RowFilter — the page is read, then narrowed per object.
	RowFilter Shape = iota
	// SubjectScoped — the query is narrowed by the authenticated caller.
	SubjectScoped
	// ClusterScoped — reference data with no per-object grants to narrow to.
	ClusterScoped
)

func (s Shape) String() string {
	switch s {
	case RowFilter:
		return "RowFilter"
	case SubjectScoped:
		return "SubjectScoped"
	case ClusterScoped:
		return "ClusterScoped"
	}
	return "unknown"
}

// Listing is the enforcement declared for one listing method.
type Listing struct {
	Shape  Shape
	Reason string // required for ClusterScoped
}

// listings declares, per use-case listing method, how visibility is decided.
//
// It is total: a listing method with no entry is a finding, and an entry with no
// method is a finding. Both directions matter, and both were open here. The gate
// matched the method name `List` EXACTLY, so ListOperations and ListAttachments —
// three pages this service hands to callers — were outside its view entirely while
// it printed OK. And disk_type was excluded with --allow=disk_type, an exclusion on
// the RESOURCE, which would silently have covered any further listing method added
// to that use-case.
var listings = map[string]Listing{
	"image.List":    {Shape: RowFilter},
	"snapshot.List": {Shape: RowFilter},
	"volume.List":   {Shape: RowFilter},
	"disk_type.List": {
		Shape: ClusterScoped,
		Reason: "DiskType is the `{cluster,*}` viewer reference data: every authenticated caller " +
			"reads every row and there are no per-object grants to narrow to. The exclusion " +
			"expires with its method — retire the RPC and this entry becomes a finding.",
	},

	// Чтение квот арендатором. Сужать НЕЧЕГО: сужение отвечает на вопрос «какие из
	// этих объектов доступны вызывающему», и он осмыслен, пока у строк ответа есть
	// ИНДИВИДУАЛЬНЫЕ владельцы. У квоты их нет — это свойство проекта, как его имя
	// или метки. Проект либо читаем этим вызывающим, либо нет: ровно один вопрос,
	// и его решает `viewer` на проекте через извлечение области действия на крае.
	//
	// Имя формы здесь ШИРЕ её смысла: ответ остаётся project-scoped и
	// cluster-scoped не становится. Сказано вслух, чтобы следующий читатель не
	// вывел из имени, будто квоты видны всему кластеру. Текст и обоснование те же,
	// что у compute/nlb/vpc, — один ответ на один предмет у всех владельцев учёта.
	"quota.List": {
		Shape: ClusterScoped,
		Reason: "Quota rows are a property of the project, not objects with individual owners: " +
			"there is nothing to narrow to. The project-scope Check at the edge (viewer on " +
			"project_id) is what settles access, and the proto carries it. Named ClusterScoped " +
			"only because the gate has no third shape — the answer stays project-scoped. The " +
			"exclusion expires with its method — retire QuotaHandler.List and this entry " +
			"becomes a finding.",
	},

	// Operation histories are narrowed by the caller in the context, not by the
	// resource id the request names.
	"image.ListOperations":    {Shape: SubjectScoped},
	"volume.ListOperations":   {Shape: SubjectScoped},
	"snapshot.ListOperations": {Shape: SubjectScoped},

	// Зарегистрированный бэкенд и ревизия привязки класса не принадлежат арендатору:
	// это отображение продуктового каталога на чужое хранилище, одно на кластер.
	// Пообъектного сужения у их страниц нет, потому что сужать НЕ К ЧЕМУ — владельца
	// у строки не существует. Оба ресурса живут только на внутреннем листенере и
	// гейтятся системным админским отношением, которое подстановочной выдачей не
	// выполнимо, — в отличие от справочного `viewer` на том же типе объекта.
	"storage_backend.List": {
		Shape: ClusterScoped,
		Reason: "StorageBackend is admin-only infrastructure registration on the internal " +
			"listener: it has no tenant owner, so there are no per-object grants to narrow " +
			"to. The exclusion expires with its method — retire the RPC and this entry " +
			"becomes a finding.",
	},
	"disk_type_binding.List": {
		Shape: ClusterScoped,
		Reason: "DiskTypeBinding is an immutable revision mapping a product class onto a " +
			"backend: admin-only, internal listener, no tenant owner and therefore nothing " +
			"to narrow a page to.",
	},

	// ListAttachments asks the rights model about the INSTANCES the caller named,
	// not about the volumes, so the answer is all-or-nothing per instance. It is a
	// row filter over that page.
	"volume.ListAttachments": {Shape: RowFilter},
}

// Options configures one audit run.
type Options struct {
	// Root is the service root (the directory holding internal/…).
	Root string
	// Listings overrides the declaration table. Empty means the real one below.
	//
	// It exists for the gate's own fixture tests: a fixture tree holds one or two
	// resources, so judging it against the real table would report every other
	// declaration as expired. Production never sets it.
	Listings map[string]Listing
}

// Report is the census of one audit run: what was examined, and what was found.
type Report struct {
	AdapterFiles  int      // adapter files parsed under internal/repo/pg
	HandlerFiles  int      // transport files parsed under internal/handler
	Declared      []string // listing methods DECLARED on the transport surface, sorted
	Resources     []string // resources discovered (snake_case), sorted
	Checked       []string // resources actually judged
	Listings      []string // listing methods discovered, "<resource>.<Method>", sorted
	ClusterScoped []string // listing methods declared as needing no narrowing
	Undeclared    []string // listing methods with no declaration — each is also a finding
	Findings      []string // one line per finding; non-empty ⇒ the gate fails
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

	// Every listing method of every resource's use-case, not only the one called
	// List. This is the discovery the gate did not have: `List` is the collection
	// RPC, and the child listings beside it are pages just the same.
	found := map[string]useCaseMethod{}
	for _, res := range rep.Resources {
		ucDir := filepath.Join(root, useCaseRoot, packageDirFor(res))
		ms, err := findUseCaseListings(ucDir)
		if err != nil {
			rep.Findings = append(rep.Findings, fmt.Sprintf(
				"%s — %v (cannot prove the projectId backstop; fail-closed)", res, err))
			continue
		}
		for name, m := range ms {
			found[res+"."+name] = m
		}
	}
	// ВТОРАЯ ПОЛОСА ОБНАРУЖЕНИЯ — по ОБЪЯВЛЕНИЮ НА ТРАНСПОРТЕ, а не по имени
	// метода репозитория.
	//
	// Полоса выше находит ресурс по форме `func (*<X>Repo) List(`, и это её
	// слепота: страница, чей репозиторий назвал метод иначе (`ListStates`,
	// `ListPage`) либо у которой репозитория нет вовсе, не попадает в поле зрения
	// НИ ОДНОЙ проверки, а перепись при этом печатает число, выглядящее полным —
	// она не умеет сообщить о том, чего не видела.
	//
	// Транспортная поверхность от имён репозитория не зависит: страница, которую
	// вызывающий может запросить, объявлена методом `List…` на `*<X>Handler`, и
	// другого способа до неё дойти нет. Поэтому объединение двух полос равно тому,
	// что видит независимая перепись дерева (`pkg/listfiltergate`), — а
	// равенство этих двух чисел и есть проверяемое свойство.
	//
	// Приставка `Internal` у типа обработчика снимается: `InternalVolumeHandler` —
	// второй слушатель ТОГО ЖЕ ресурса, а не отдельный ресурс. Не снять её значило
	// бы завести второе пространство имён для одного предмета, и объявление
	// пришлось бы писать дважды.
	handlerLists, handlerFiles, herr := findHandlerLists(filepath.Join(root, transportRoot))
	rep.HandlerFiles = handlerFiles
	if herr != nil {
		rep.Findings = append(rep.Findings, herr.Error())
		return finish(rep, out)
	}
	for k := range handlerLists {
		rep.Declared = append(rep.Declared, k)
		if _, seen := found[k]; !seen {
			found[k] = handlerLists[k]
		}
	}
	sort.Strings(rep.Declared)

	for k := range found {
		rep.Listings = append(rep.Listings, k)
	}
	sort.Strings(rep.Listings)

	table := o.Listings
	if len(table) == 0 {
		table = listings
	}

	// A declaration lives only while it has a subject.
	for _, k := range sortedKeys(declaredKeysOf(table)) {
		if !contains(rep.Listings, k) {
			rep.Findings = append(rep.Findings, fmt.Sprintf(
				"declared listing %q matches no method under %s — the declaration has nothing left "+
					"to describe; drop it, or the next method of that name inherits an enforcement "+
					"claim nobody checked", k, filepath.Join(root, useCaseRoot)))
		}
	}

	rep.Checked = append(rep.Checked, rep.Resources...)
	for _, key := range rep.Listings {
		m := found[key]
		l, ok := table[key]
		if !ok {
			rep.Undeclared = append(rep.Undeclared, key)
			rep.Findings = append(rep.Findings, fmt.Sprintf(
				"%s — a listing method with no declared enforcement: nothing states how this page is "+
					"narrowed, so nothing checks that it is\n  service: %s\n  declare it in "+
					"tools/auditlistfilter (RowFilter / SubjectScoped / ClusterScoped) — until it is "+
					"stated, the gate fails closed", key, m.file))
			continue
		}
		if l.Shape == ClusterScoped {
			rep.ClusterScoped = append(rep.ClusterScoped, key)
		}
		rep.Findings = append(rep.Findings, checkListing(root, key, l, m, adapters)...)
	}

	return finish(rep, out)
}

// declaredKeysOf returns a declaration table's keys as a set.
func declaredKeysOf(table map[string]Listing) map[string]bool {
	out := map[string]bool{}
	for k := range table {
		out[k] = true
	}
	return out
}

// checkListing judges one listing method against the shape it declares.
//
// The three original checks — the repo adapter narrows by project_id, the use-case
// rejects an empty projectId, the use-case filters the page per object — are the
// RowFilter shape of the resource's `List`, and stay exactly as they were. What is
// new is that the OTHER listing methods are judged at all.
func checkListing(root, key string, l Listing, m useCaseMethod, adapters map[string]listMethod) []string {
	res, method, _ := strings.Cut(key, ".")

	var findings []string
	if l.Shape != ClusterScoped && callsAnyOf(m.fn, enumerateThenNarrow) {
		findings = append(findings, fmt.Sprintf(
			"%s — enumerates allowed ids (ListAllowedIDs/ListObjects) instead of batch-checking the "+
				"page: that answer is capped server-side with no continuation token, so the caller's "+
				"own rows fall outside the cap and disappear\n  service: %s", key, m.file))
	}

	switch l.Shape {
	case RowFilter:
		// The resource's own collection RPC additionally owes the project-scope
		// posture in the adapter and the in-service backstop.
		if method == "List" {
			findings = append(findings, checkResource(root, res, adapters[res])...)
			return findings
		}
		if !callsAnyOf(m.fn, perObjectFilters) {
			findings = append(findings, fmt.Sprintf(
				"%s — declared RowFilter, but does not filter the page per object "+
					"(listnarrow.Page/listnarrow.IDs)\n  service: %s", key, m.file))
		}
	case SubjectScoped:
		if !callsAnyOf(m.fn, subjectScopers) {
			findings = append(findings, fmt.Sprintf(
				"%s — declared SubjectScoped, but nothing it calls reaches ListForCaller: the query "+
					"is not narrowed by the authenticated caller\n  service: %s", key, m.file))
		}
	case ClusterScoped:
		if strings.TrimSpace(l.Reason) == "" {
			findings = append(findings, fmt.Sprintf(
				"%s — declared ClusterScoped with no Reason: this is the one shape with no code "+
					"evidence, so an unstated reason makes it indistinguishable from a listing nobody "+
					"thought about\n  service: %s", key, m.file))
		}
	}
	return findings
}

// useCaseMethod is one listing method of a use-case package.
type useCaseMethod struct {
	file string
	fn   *ast.FuncDecl
}

// findUseCaseListings returns every `func (*UseCase) List…` in the package at dir,
// keyed by method name.
//
// Identification is by PREFIX, matching the form already proven in
// services/registry/tools/auditlistfilter. Its predecessor, findUseCaseList,
// matched `List` exactly and so located one method per package — the other listing
// methods beside it were never opened.
func findUseCaseListings(dir string) (map[string]useCaseMethod, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("use-case package %s absent", dir)
	}
	fset := token.NewFileSet()
	out := map[string]useCaseMethod{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, perr)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, "List") || fn.Body == nil {
				continue
			}
			if receiverTypeName(fn) == "UseCase" {
				out[fn.Name.Name] = useCaseMethod{file: path, fn: fn}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("use-case package %s declares no (*UseCase).List…", dir)
	}
	return out, nil
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
			"%s — use-case List does not filter the page per-object (listnarrow.Page/listnarrow.IDs)\n  service: %s",
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

// findHandlerLists sweeps the transport package for listing methods declared on a
// `*<X>Handler` receiver, keyed "<resource>.<Method>" in the SAME namespace the
// declaration table uses.
//
// Отсутствие каталога — НЕ ошибка: фикстуры гейта транспорта не несут, и требовать
// его от них значило бы проверять форму фикстуры, а не свойство дерева. Зато
// «прочитано ноль файлов» уходит в перепись отдельным числом, поэтому «ноль
// находок» остаётся отличимым от «ноль прочитанного», а равенство объявленного и
// судимого на НАСТОЯЩЕМ дереве держит независимая перепись `pkg/listfiltergate`.
func findHandlerLists(dir string) (map[string]useCaseMethod, int, error) {
	out := map[string]useCaseMethod{}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return out, 0, nil
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		return nil, 0, fmt.Errorf("read %s: %w", dir, rerr)
	}
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
			if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "List") {
				continue
			}
			recv := receiverTypeName(fn)
			if !strings.HasSuffix(recv, "Handler") || recv == "Handler" {
				continue
			}
			res := strings.TrimPrefix(strings.TrimSuffix(recv, "Handler"), "Internal")
			if res == "" {
				continue
			}
			out[toSnake(res)+"."+fn.Name.Name] = useCaseMethod{file: path, fn: fn}
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
	// The listing-method count sits beside the resource count on purpose: the census
	// used to report resources only, so "4 resources" read the same whether those 4
	// resources held 4 listing methods or 7, and the 3 it was not looking at were
	// invisible in the very line meant to state what had been examined.
	//
	// ОБЕ СТОРОНЫ ПЕЧАТАЮТСЯ РЯДОМ: сколько страниц ОБЪЯВЛЕНО на транспорте и
	// сколько СУДИМО. Пока это одно число, расхождение видно только отказом —
	// то есть тогда, когда оно уже стоило прогона; двумя числами оно видно всегда.
	_, _ = fmt.Fprintf(out,
		"audit-list-filter: examined %d adapter file(s) and %d transport file(s), %d resource(s), "+
			"%d listing method(s) declared on the transport surface, %d judged "+
			"(%d undeclared, %d cluster-scoped)\n",
		rep.AdapterFiles, rep.HandlerFiles, len(rep.Resources), len(rep.Declared), len(rep.Listings),
		len(rep.Undeclared), len(rep.ClusterScoped))
	if len(rep.Listings) > 0 {
		_, _ = fmt.Fprintf(out, "audit-list-filter: judged %s\n", strings.Join(rep.Listings, ", "))
	}
	if len(rep.ClusterScoped) > 0 {
		_, _ = fmt.Fprintf(out, "audit-list-filter: cluster-scoped by declaration %s\n",
			strings.Join(rep.ClusterScoped, ", "))
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
