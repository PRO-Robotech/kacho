// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package tools_regression holds Go-level regression tests for this service's CI
// gates (audit-list-filter, audit-known-failing). Keeping them under
// `go test ./...` means each gate's own detection logic is exercised by the
// standard verification harness — a gate that silently stops catching regressions
// is itself a regression, so its behaviour is locked with fixtures.
//
// Three distinct things are asserted here, and all three are needed:
//   - TestAuditListFilter — the gate's DETECTION LOGIC against synthetic fixtures,
//     in both directions (does it still catch each leak shape, and does it stay
//     silent on the legitimate shape that resembles it?);
//   - TestAuditListFilter_NothingExaminedIsNotOK / _ReportsWhatItExamined — the
//     gate's own premise: "zero findings" must be unreachable from "zero read";
//   - TestAuditListFilter_RealTreePasses — the gate against THIS SERVICE'S REAL
//     tree, through the very command CI issues, inside the ordinary
//     `go test ./...` run.
//
// Both routes to the gate are kept deliberately: `go test` reaches the real tree
// wherever tests run, and `make -C services/storage audit-list-filter` is what CI
// issues (.github/workflows/ci.yaml, job authz-artifacts). Which of the two is
// wired is asserted, not narrated — see ci_gate_wiring_test.go.
package tools_regression

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/tools/auditlistfilter"
)

// scriptDir returns the directory this test file lives in (…/tools), which also
// holds audit-list-filter.sh.
func scriptDir(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(self)
}

// runScript runs one of this directory's gate wrappers exactly as CI would, and
// returns its combined output plus the verdict.
func runScript(t *testing.T, name string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(scriptDir(t), name))
	raw, err := cmd.CombinedOutput()
	return string(raw), err
}

// writeFile writes content to path, creating parent dirs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// runGate materialises a throwaway service tree from the fixture files and runs
// the gate's analyser against it, returning its output plus the verdict (nil ⇒
// clean, non-nil ⇒ the gate fails the tree).
func runGate(t *testing.T, files map[string]string) (string, error) {
	t.Helper()
	return runGateArgs(t, files)
}

// runGateArgs is runGate with an explicit declaration table. The table is passed per
// invocation rather than baked in, because the REAL one describes the real tree: a
// fixture holding one resource would otherwise report every other declaration as an
// expired one — see TestAuditListFilter_ExpiredDeclarationIsAFinding.
func runGateArgs(t *testing.T, files map[string]string, decls ...map[string]auditlistfilter.Listing) (string, error) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFile(t, filepath.Join(root, rel), content)
	}

	var buf strings.Builder
	opts := auditlistfilter.Options{Root: root}
	if len(decls) == 1 {
		opts.Listings = decls[0]
	}
	_, err := auditlistfilter.Audit(opts, &buf)
	return buf.String(), err
}

const (
	// repoNarrows — a public List whose body narrows by project_id (compliant).
	repoNarrows = `package pg

func (r *VolumeRepo) Insert() {
	_ = "INSERT ... project_id = $1"
}

func (r *VolumeRepo) List() {
	_ = "SELECT ... WHERE v.project_id = $1"
}
`
	// repoListDropsNarrowing — the Finding-2 hole: List drops project narrowing,
	// but a `project_id = $` predicate survives in Insert (file-scope grep would
	// give false confidence). A body-scoped gate MUST flag this.
	repoListDropsNarrowing = `package pg

func (r *VolumeRepo) Insert() {
	_ = "INSERT ... project_id = $1"
}

func (r *VolumeRepo) List() {
	_ = "SELECT ... FROM volumes"
}
`
	// ucCompliant — a use-case List that (a) rejects empty projectId and (b) narrows
	// the page it just read to the ids the caller may actually see (per-object).
	ucCompliant = `package volume

func (u *UseCase) List() error {
	if p.ProjectID == "" {
		return errRequired
	}
	vols, next, err := u.reader.List(ctx, p)
	visible, ferr := listnarrow.Page(ctx, u.listFilter,
		authzfilter.ResourceTypeVolume, authzfilter.ActionVolumeList, vols, idOf)
	return nil
}
`
	// ucNoGuard — a use-case List missing the empty-projectId backstop.
	ucNoGuard = `package volume

func (u *UseCase) List() error {
	vols, next, err := u.reader.List(ctx, p)
	visible, ferr := listnarrow.Page(ctx, u.listFilter,
		authzfilter.ResourceTypeVolume, authzfilter.ActionVolumeList, vols, idOf)
	return nil
}
`
	// ucNoPerObjectFilter — THE hole this service shipped with: project scoping is
	// in place (repo narrows by project_id, use-case demands projectId), so the old
	// gate was fully satisfied — yet every project member saw every row, because
	// nothing ever asked whether the caller may see THESE objects.
	ucNoPerObjectFilter = `package volume

func (u *UseCase) List() error {
	if p.ProjectID == "" {
		return errRequired
	}
	vols, next, err := u.reader.List(ctx, p)
	return nil
}
`
	// ucEnumerateAllowedIDs — the OTHER rejected shape: "enumerate everything the
	// subject may see, then narrow the SQL to it". Such an enumeration is capped
	// server-side with no continuation token, so a tenant's own resource silently
	// falls outside the prefix and becomes invisible. The gate must NOT accept it as
	// a per-object filter — and it stays in the fixture on purpose: the shape can be
	// hand-written again long after the RPC that made it convenient is gone.
	ucEnumerateAllowedIDs = `package volume

func (u *UseCase) List() error {
	if p.ProjectID == "" {
		return errRequired
	}
	allowed, err := u.authz.ListAllowedIDs(ctx, subject, resourceType, action)
	vols, next, err := u.reader.ListByIDs(ctx, allowed, p)
	return nil
}
`
	// ucFilterOnlyInComment — prose must never satisfy the gate: a comment naming
	// listnarrow.IDs next to a List that filters nothing is the exact "form without
	// substance" this gate exists to catch.
	ucFilterOnlyInComment = `package volume

func (u *UseCase) List() error {
	if p.ProjectID == "" {
		return errRequired
	}
	// Visibility is narrowed per-object via listnarrow.IDs.
	vols, next, err := u.reader.List(ctx, p)
	return nil
}
`
	// repoNarrowsRenamedReceiver — the SAME compliant adapter, written with the
	// receiver named `repo` instead of `r`. Nothing about the resource changed: it is
	// still a *VolumeRepo with a public List. A gate that keys on the receiver's NAME
	// stops seeing the resource here and reports OK for whatever the use-case does.
	repoNarrowsRenamedReceiver = `package pg

func (repo *VolumeRepo) Insert() {
	_ = "INSERT ... project_id = $1"
}

func (repo *VolumeRepo) List() {
	_ = "SELECT ... WHERE v.project_id = $1"
}
`
	// ucCompliantMentionsListObjects — the mirror case: a compliant List whose
	// comment EXPLAINS why ListObjects is banned must not be flagged for saying so.
	ucCompliantMentionsListObjects = `package volume

func (u *UseCase) List() error {
	if p.ProjectID == "" {
		return errRequired
	}
	vols, next, err := u.reader.List(ctx, p)
	// Never ListAllowedIDs/ListObjects here: the enumeration is capped server-side.
	visible, ferr := listnarrow.Page(ctx, u.listFilter,
		authzfilter.ResourceTypeVolume, authzfilter.ActionVolumeList, vols, idOf)
	return nil
}
`
)

func TestAuditListFilter(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		decls   map[string]auditlistfilter.Listing
		wantErr bool // true ⇒ gate must exit non-zero
	}{
		{
			name: "compliant: repo narrows, use-case requires projectId AND filters per-object",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":          repoNarrows,
				"internal/apps/kacho/api/volume/volume.go": ucCompliant,
			},
			wantErr: false,
		},
		{
			// Core Finding-2 regression: file-scope grep passes (Insert carries the
			// predicate) but the List body itself no longer narrows — must FAIL.
			name: "leak: List body drops project narrowing though Insert keeps predicate",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":          repoListDropsNarrowing,
				"internal/apps/kacho/api/volume/volume.go": ucCompliant,
			},
			wantErr: true,
		},
		{
			// THE blind spot: project scoping present, per-object visibility absent.
			// The gate used to pass this — that is how storage shipped a List that
			// showed every project member every volume/snapshot/image.
			name: "leak: use-case List never asks who may see the page's objects",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":          repoNarrows,
				"internal/apps/kacho/api/volume/volume.go": ucNoPerObjectFilter,
			},
			wantErr: true,
		},
		{
			// enumerate-then-narrow is NOT an accepted substitute for a per-page
			// batched check (ListObjects truncation makes own resources invisible).
			name: "reject: enumerate-all-allowed-ids instead of a per-page batch check",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":          repoNarrows,
				"internal/apps/kacho/api/volume/volume.go": ucEnumerateAllowedIDs,
			},
			wantErr: true,
		},
		{
			// Finding-1 backstop assertion: repo narrows, but the use-case forgot the
			// required-projectId guard — the gate must also catch that.
			name: "leak: use-case List does not require projectId",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":          repoNarrows,
				"internal/apps/kacho/api/volume/volume.go": ucNoGuard,
			},
			wantErr: true,
		},
		{
			// A comment is not an implementation: the gate judges code, not prose.
			name: "leak: per-object filter only mentioned in a comment",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":          repoNarrows,
				"internal/apps/kacho/api/volume/volume.go": ucFilterOnlyInComment,
			},
			wantErr: true,
		},
		{
			// …and the converse: documenting WHY enumeration is banned must not fail
			// a List that does the right thing.
			name: "compliant: comment explains the banned enumeration shape",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":          repoNarrows,
				"internal/apps/kacho/api/volume/volume.go": ucCompliantMentionsListObjects,
			},
			wantErr: false,
		},
		{
			// Missing use-case file ⇒ cannot prove the backstop ⇒ fail closed.
			name: "fail-closed: use-case List file absent",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go": repoNarrows,
			},
			wantErr: true,
		},
		{
			// Identification must key on WHAT the declaration is (a public List on a
			// *…Repo), not on what the receiver happens to be CALLED. Renaming `r` to
			// `repo` is a refactor nobody would flag in review, and it used to make the
			// resource invisible to the gate — the leak below then reported OK.
			name: "blind spot: renaming the receiver must not hide an unfiltered List",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":          repoNarrowsRenamedReceiver,
				"internal/apps/kacho/api/volume/volume.go": ucNoPerObjectFilter,
			},
			wantErr: true,
		},
		{
			// …and the converse, so the fix is not merely "flag everything": the same
			// renamed receiver on a COMPLIANT resource must stay silent.
			name: "compliant: renamed receiver on a fully compliant resource",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":          repoNarrowsRenamedReceiver,
				"internal/apps/kacho/api/volume/volume.go": ucCompliant,
			},
			wantErr: false,
		},
		{
			// The use-case List is found by WHAT it is, not by which file holds it:
			// splitting a package into list.go/create.go is routine (vpc does exactly
			// that), and it must neither hide a leak nor manufacture a false red.
			name: "compliant: use-case List lives in list.go, not <resource>.go",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":        repoNarrows,
				"internal/apps/kacho/api/volume/list.go": ucCompliant,
			},
			wantErr: false,
		},
		{
			// Same split, leaking: the gate must read the package, not one filename.
			name: "leak: use-case List in list.go never filters per-object",
			files: map[string]string{
				"internal/repo/pg/volume_repo.go":        repoNarrows,
				"internal/apps/kacho/api/volume/list.go": ucNoPerObjectFilter,
			},
			wantErr: true,
		},
		{
			// A cluster-catalog listing: declared ClusterScoped, so it need not
			// narrow. Note the declaration is per METHOD — the exclusion cannot
			// silently cover another listing added to this use-case later, which is
			// exactly what --allow=disk_type used to do.
			name: "declared ClusterScoped: disk_type's List need not narrow",
			files: map[string]string{
				"internal/repo/pg/disk_type_repo.go": `package pg

func (r *DiskTypeRepo) List() {
	_ = "SELECT ... FROM disk_types"
}
`,
				"internal/apps/kacho/api/disktype/disktype.go": `package disktype

type UseCase struct{}

func (u *UseCase) List(ctx context.Context) error { return nil }
`,
			},
			decls: map[string]auditlistfilter.Listing{
				"disk_type.List": {Shape: auditlistfilter.ClusterScoped, Reason: "cluster reference data"},
			},
			wantErr: false,
		},
		{
			// The mirror of the case above: ClusterScoped with no reason is a
			// finding. It is the one shape with no code evidence, so an unstated
			// reason makes it indistinguishable from a listing nobody thought about.
			name: "declared ClusterScoped with no reason is a finding",
			files: map[string]string{
				"internal/repo/pg/disk_type_repo.go": `package pg

func (r *DiskTypeRepo) List() {
	_ = "SELECT ... FROM disk_types"
}
`,
				"internal/apps/kacho/api/disktype/disktype.go": `package disktype

type UseCase struct{}

func (u *UseCase) List(ctx context.Context) error { return nil }
`,
			},
			decls: map[string]auditlistfilter.Listing{
				"disk_type.List": {Shape: auditlistfilter.ClusterScoped},
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decls := tc.decls
			if decls == nil {
				// Every fixture in this table models the `volume` resource, so a
				// table naming just that is what the fixture tree actually holds.
				// Falling back to the REAL table would report the other seven
				// declarations as expired and drown the property under test.
				decls = map[string]auditlistfilter.Listing{
					"volume.List": {Shape: auditlistfilter.RowFilter},
				}
			}
			out, err := runGateArgs(t, tc.files, decls)
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Fatalf("gate exit: gotErr=%v wantErr=%v\n--- output ---\n%s", gotErr, tc.wantErr, out)
			}
		})
	}
}

// TestAuditListFilter_NothingExaminedIsNotOK — "zero findings" must be
// distinguishable from "zero read".
//
// Both shapes below used to print OK and exit 0, which is the worst answer a gate
// can give: it is indistinguishable from a clean tree, so a gate pointed at the
// wrong place, or at a tree whose adapters moved, certifies a service it never
// opened. A gate that examined nothing has proven nothing and must say so.
func TestAuditListFilter_NothingExaminedIsNotOK(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{
			// The adapter root does not exist at all (wrong directory, moved layout).
			name:  "adapter root absent",
			files: map[string]string{"internal/apps/kacho/api/volume/volume.go": ucCompliant},
		},
		{
			// The root exists but holds no public List adapter — nothing was judged.
			name:  "adapter root holds no public List",
			files: map[string]string{"internal/repo/pg/doc.go": "package pg\n"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runGate(t, tc.files)
			if err == nil {
				t.Fatalf("gate exited 0 having examined nothing — that is not a pass\n--- output ---\n%s", out)
			}
		})
	}
}

// TestAuditListFilter_ReportsWhatItExamined — the gate must state its census, so a
// passing run can be read as "N resources judged" rather than "the word OK".
func TestAuditListFilter_ReportsWhatItExamined(t *testing.T) {
	out, err := runGateArgs(t, map[string]string{
		"internal/repo/pg/volume_repo.go":          repoNarrows,
		"internal/apps/kacho/api/volume/volume.go": ucCompliant,
	}, map[string]auditlistfilter.Listing{
		"volume.List": {Shape: auditlistfilter.RowFilter},
	})
	if err != nil {
		t.Fatalf("compliant fixture must pass: %v\n--- output ---\n%s", err, out)
	}
	// Обе стороны переписи названы отдельно: сколько страниц ОБЪЯВЛЕНО на
	// транспорте и сколько СУДИМО. Пока это одно число, расхождение между ними
	// видно только отказом — то есть тогда, когда оно уже стоило прогона.
	for _, want := range []string{
		"1 adapter file", "transport file(s)", "1 resource",
		"listing method(s) declared on the transport surface", "1 judged", "volume.List",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("gate output must state what it examined (missing %q)\n--- output ---\n%s", want, out)
		}
	}
}

// TestAuditListFilter_ExpiredDeclarationIsAFinding — an exclusion must expire by
// itself. Once no such method exists the entry describes nothing, and the next method
// to inherit that name inherits an enforcement claim nobody checked.
//
// The subject moved one level down when --allow=<resource> was replaced: that flag
// excluded a whole RESOURCE, so the exclusion written for disk_type would also have
// covered any listing method later added to its use-case. Declarations are per
// method, so an exclusion can no longer take its neighbours with it.
func TestAuditListFilter_ExpiredDeclarationIsAFinding(t *testing.T) {
	files := map[string]string{
		"internal/repo/pg/volume_repo.go":          repoNarrows,
		"internal/apps/kacho/api/volume/volume.go": ucCompliant,
	}
	expired := map[string]auditlistfilter.Listing{
		"volume.List":           {Shape: auditlistfilter.RowFilter},
		"retired_resource.List": {Shape: auditlistfilter.ClusterScoped, Reason: "gone long ago"},
	}
	out, err := runGateArgs(t, files, expired)
	if err == nil {
		t.Fatalf("a declaration matching no method must be reported\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "retired_resource.List") {
		t.Errorf("the finding must name the expired entry\n--- output ---\n%s", out)
	}

	// Converse: an entry that still has a subject must stay silent, so the rule is
	// not merely "flag every declaration".
	files["internal/repo/pg/disk_type_repo.go"] = `package pg

func (r *DiskTypeRepo) List() {
	_ = "SELECT ... FROM disk_types"
}
`
	files["internal/apps/kacho/api/disktype/disktype.go"] = `package disktype

type UseCase struct{}

func (u *UseCase) List(ctx context.Context) error { return nil }
`
	live := map[string]auditlistfilter.Listing{
		"volume.List":    {Shape: auditlistfilter.RowFilter},
		"disk_type.List": {Shape: auditlistfilter.ClusterScoped, Reason: "cluster reference data"},
	}
	if out, err := runGateArgs(t, files, live); err != nil {
		t.Fatalf("a declaration that still matches a method must not be a finding: %v\n--- output ---\n%s", err, out)
	}
}

// TestAuditListFilter_RealTreePasses гоняет гейт против НАСТОЯЩЕГО дерева
// kacho-storage (не фикстур) и ЧЕРЕЗ ту же команду, которую издаёт CI — сам
// скрипт, а не только его Go-анализатор. Так под тестом оказывается вся цепочка:
// обёртка → `go run` → cmd-флаги → разбор. Обронённое сужение по `project_id`,
// пропавший required-projectId backstop или снятый per-object фильтр уронят
// сборку здесь.
//
// Два пути к гейту держатся НАМЕРЕННО, и это не дубль: `make -C services/storage
// audit-list-filter` — то, что издаёт CI (.github/workflows/ci.yaml, job
// authz-artifacts, пять сервисов; проверяется ci_gate_wiring_test.go), а этот
// тест доводит гейт до реального дерева везде, где вообще гоняются тесты.
// Прежняя редакция этого комментария объясняла второй путь иначе — «make-таргет
// не вызывается ни одним CI-job'ом, поэтому go test единственное покрытие». Это
// перестало быть правдой, когда шаг в CI появился, и осталось написанным: ровно
// тот класс, ради которого гейт и стоит. Утверждение больше не живёт в прозе.
//
// Тест НЕ помечен -short-скипом намеренно: unit-джоба CI гоняет именно `-short`, а
// гейт стоит миллисекунды и не требует Docker/Postgres.
func TestAuditListFilter_RealTreePasses(t *testing.T) {
	out, err := runScript(t, "audit-list-filter.sh")
	if err != nil {
		t.Fatalf("audit-list-filter must pass against the real kacho-storage tree: %v\n--- output ---\n%s", err, out)
	}

	// A pass is only worth reading if it says what it judged. "OK" on a tree the
	// gate never opened is the failure mode this asserts against — and it is the
	// production invocation, so the whitelist wiring is asserted here too.
	if strings.Contains(out, "examined 0 ") || strings.Contains(out, ", 0 resource") {
		t.Fatalf("the gate passed having examined nothing — that is not a pass\n--- output ---\n%s", out)
	}
	// The listing-method names are asserted, not just a count: the census used to
	// report resources only, and "4 resources" read the same whether those resources
	// held 4 listing methods or 7. The three it was not looking at — two operation
	// histories and the attachment listing — were invisible in the very line meant
	// to state what had been examined.
	for _, want := range []string{
		"listing method(s)",
		"volume.ListOperations", "volume.ListAttachments", "image.ListOperations",
		"cluster-scoped by declaration disk_type.List",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("real-tree run must report its census (missing %q)\n--- output ---\n%s", want, out)
		}
	}
	t.Log(strings.TrimSpace(out))
}

// TestAuditListFilter_SeesAListingWithNoRepoOfThatName — страница, объявленная на
// транспорте, судится ДАЖЕ когда её репозиторий назвал свой метод иначе.
//
// Это про КЛАСС, а не про один ресурс. Прежняя полоса обнаружения находила ресурс
// по форме `func (*<X>Repo) List(`, и любой следующий ресурс, чей репозиторий зовёт
// своё чтение `ListStates`/`ListPage` — или у которого репозитория нет вовсе, —
// выпадал из поля зрения ВСЕХ проверок разом. Перепись при этом печатала число,
// выглядящее полным: гейт не умеет сообщить о том, чего не видел. Найдено переписью
// дерева (`pkg/listfiltergate`), сравнивающей счёт анализатора с независимым
// обходом; на дне находки лежала ровно одна страница — чтение квот арендатором.
//
// Проба идёт В ОБЕ СТОРОНЫ: без объявления страница обязана стать находкой
// (fail-closed), с объявлением — пройти. Только положительной половины мало: она
// зеленела бы и на гейте, который просто перестал смотреть.
func TestAuditListFilter_SeesAListingWithNoRepoOfThatName(t *testing.T) {
	files := map[string]string{
		"internal/repo/pg/volume_repo.go":          repoNarrows,
		"internal/apps/kacho/api/volume/volume.go": ucCompliant,
		// Репозиторий этой страницы существует, но зовётся иначе, и use-case-пакета
		// у неё нет вовсе — то есть прежней полосе она невидима целиком.
		"internal/repo/pg/quota.go": `package pg

func (r *QuotaRepo) ListStates() {
	_ = "SELECT ... FROM project_resource_quotas WHERE project_id = $1"
}
`,
		"internal/handler/quota_handler.go": `package handler

func (h *QuotaHandler) List(ctx context.Context) error { return nil }
`,
	}

	undeclared := map[string]auditlistfilter.Listing{
		"volume.List": {Shape: auditlistfilter.RowFilter},
	}
	out, err := runGateArgs(t, files, undeclared)
	if err == nil {
		t.Fatalf("страница, объявленная на транспорте, но не объявленная гейту, обязана быть "+
			"находкой — иначе она не судится ничем\n--- вывод ---\n%s", out)
	}
	if !strings.Contains(out, "quota.List") {
		t.Errorf("находка обязана НАЗВАТЬ страницу\n--- вывод ---\n%s", out)
	}
	// Перепись печатает обе стороны: расхождение обязано быть видно числом, а не
	// только отказом.
	for _, want := range []string{"transport file(s)", "declared on the transport surface", "judged"} {
		if !strings.Contains(out, want) {
			t.Errorf("перепись обязана называть обе стороны (нет %q)\n--- вывод ---\n%s", want, out)
		}
	}

	declared := map[string]auditlistfilter.Listing{
		"volume.List": {Shape: auditlistfilter.RowFilter},
		"quota.List": {
			Shape:  auditlistfilter.ClusterScoped,
			Reason: "свойство проекта, индивидуальных владельцев у строк нет",
		},
	}
	if out, err := runGateArgs(t, files, declared); err != nil {
		t.Fatalf("объявленная страница обязана проходить: %v\n--- вывод ---\n%s", err, out)
	}
}

// TestAuditListFilter_InternalListenerIsTheSameResource — второй слушатель того же
// ресурса не заводит второго пространства имён.
//
// `InternalVolumeHandler` — тот же том на внутреннем слушателе, а не отдельный
// ресурс. Не снять приставку значило бы требовать ВТОРОГО объявления для одной
// страницы, и первое же расхождение между ними читалось бы как «разные предметы».
func TestAuditListFilter_InternalListenerIsTheSameResource(t *testing.T) {
	files := map[string]string{
		"internal/repo/pg/volume_repo.go":          repoNarrows,
		"internal/apps/kacho/api/volume/volume.go": ucCompliant,
		"internal/handler/internal_volume_handler.go": `package handler

func (h *InternalVolumeHandler) ListAttachments(ctx context.Context) error { return nil }
`,
	}
	decls := map[string]auditlistfilter.Listing{
		"volume.List": {Shape: auditlistfilter.RowFilter},
		"volume.ListAttachments": {
			Shape:  auditlistfilter.ClusterScoped,
			Reason: "внутренний слушатель, тенантского владельца у строки нет",
		},
	}
	out, err := runGateArgs(t, files, decls)
	if err != nil {
		t.Fatalf("страница внутреннего слушателя обязана судиться под именем СВОЕГО ресурса: %v"+
			"\n--- вывод ---\n%s", err, out)
	}
	if strings.Contains(out, "internal_volume.") {
		t.Errorf("приставка слушателя завела второе имя одному предмету\n--- вывод ---\n%s", out)
	}
}
