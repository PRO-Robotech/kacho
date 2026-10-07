// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package auditlistfilter

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// moduleRoot — the directory holding go.mod above this test file.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — the tree was never opened")
	}
	root, err := moduleRootOf(filepath.Dir(self))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// copiedPaths — what one audit reads: the service and the two authorization
// adapters it derives the ban from.
var copiedPaths = []string{"services/notify", "pkg/listnarrow/narrowiam", "pkg/authz/authziam"}

// treeCopy copies the non-test Go sources the audit reads into a fresh module
// root, so an injection changes one fact of the REAL tree and nothing else.
func treeCopy(t *testing.T) string {
	t.Helper()
	src := moduleRoot(t)
	dst := t.TempDir()
	n := 0
	for _, rel := range copiedPaths {
		err := filepath.WalkDir(filepath.Join(src, rel), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			r, _ := filepath.Rel(src, path)
			raw, rerr := os.ReadFile(path) // #nosec G304 -- path walked under this module's own root
			if rerr != nil {
				return rerr
			}
			if merr := os.MkdirAll(filepath.Dir(filepath.Join(dst, r)), 0o750); merr != nil {
				return merr
			}
			n++
			return os.WriteFile(filepath.Join(dst, r), raw, 0o600)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if n == 0 {
		t.Fatal("copied 0 files — an injection on an empty copy proves nothing")
	}
	return dst
}

// inject replaces exactly one occurrence of old in the copied file.
func inject(t *testing.T, root, rel, old, repl string) {
	t.Helper()
	p := filepath.Join(root, rel)
	raw, err := os.ReadFile(p) // #nosec G304 -- test copy under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if c := strings.Count(string(raw), old); c != 1 {
		t.Fatalf("injection anchor %q occurs %d times in %s — the injection would not change exactly one fact", old, c, rel)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(raw), old, repl, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, root string) (Report, error, string) {
	t.Helper()
	var out bytes.Buffer
	rep, err := Audit(Profile, Options{ServiceRoot: filepath.Join(root, "services/notify"), ModuleRoot: root}, &out)
	return rep, err, out.String()
}

func wantFinding(t *testing.T, root string, parts ...string) {
	t.Helper()
	rep, err, out := run(t, root)
	if !errors.Is(err, ErrFindings) {
		t.Fatalf("injection not caught: err=%v\n%s", err, out)
	}
	for _, f := range rep.Findings {
		ok := true
		for _, p := range parts {
			if !strings.Contains(f, p) {
				ok = false
			}
		}
		if ok {
			return
		}
	}
	t.Fatalf("no finding names %q:\n%s", parts, out)
}

// TestAudit_RealTreeIsJudgedAndClean — the committed layout passes, and the census
// says what was judged: three listings, two narrower sites, two ban sources.
func TestAudit_RealTreeIsJudgedAndClean(t *testing.T) {
	var out bytes.Buffer
	rep, err := Audit(Profile, Options{ServiceRoot: filepath.Join(moduleRoot(t), "services/notify")}, &out)
	t.Log(out.String())
	if err != nil {
		t.Fatalf("real tree: %v", err)
	}
	if got := strings.Join(rep.Listings, " "); got != "internal_notice.List public_notice.List public_notice.ListByAccount" {
		t.Fatalf("judged listings %q", got)
	}
	if len(rep.NarrowerCalls) != 2 || len(rep.SourceMethods) != 2 {
		t.Fatalf("narrower sites %v, ban sources %v", rep.NarrowerCalls, rep.SourceMethods)
	}
	if !strings.Contains(out.String(), "3 listing method(s) (") {
		t.Fatal("census line lost the form the tree census reads (`N listing method(s) (`)")
	}
}

// TestAudit_TwinCopyIsSilent — the copy the injections start from is clean: every
// red below comes from its one injected fact, not from the copying.
func TestAudit_TwinCopyIsSilent(t *testing.T) {
	rep, err, out := run(t, treeCopy(t))
	if err != nil || len(rep.Findings) != 0 {
		t.Fatalf("unmodified copy: %v\n%s", err, out)
	}
}

const (
	listUC    = "services/notify/internal/apps/kacho/api/publicnotice/list/list.go"
	byAccUC   = "services/notify/internal/apps/kacho/api/publicnotice/listbyaccount/listbyaccount.go"
	transport = "services/notify/internal/handler/notice.go"
	gateStmt  = "\tif err := authzcheck.RequireScope(ctx, u.deps.Checker, publicnotice.RelationRead, publicnotice.Scope(scope)); err != nil {\n\t\treturn nil, err\n\t}\n"
	readStmt  = "\treturn publicnotice.Page(ctx, u.deps, scope, pg)\n"
)

func TestAudit_InjectedMissingScopeGateIsFound(t *testing.T) {
	root := treeCopy(t)
	inject(t, root, listUC, gateStmt, "")
	wantFinding(t, root, "public_notice.List —", "reads the page without checking the scope")
}

func TestAudit_InjectedDiscardedVerdictIsFound(t *testing.T) {
	root := treeCopy(t)
	inject(t, root, byAccUC, gateStmt,
		"\t_ = authzcheck.RequireScope(ctx, u.deps.Checker, publicnotice.RelationRead, publicnotice.Scope(scope))\n")
	wantFinding(t, root, "public_notice.ListByAccount —", "never acts on its verdict")
}

func TestAudit_InjectedGateAfterReadIsFound(t *testing.T) {
	root := treeCopy(t)
	inject(t, root, listUC, gateStmt+readStmt,
		"\tresp, err := publicnotice.Page(ctx, u.deps, scope, pg)\n"+
			"\tif err := authzcheck.RequireScope(ctx, u.deps.Checker, publicnotice.RelationRead, publicnotice.Scope(scope)); err != nil {\n\t\treturn nil, err\n\t}\n"+
			"\treturn resp, err\n")
	wantFinding(t, root, "public_notice.List —", "AFTER the page is read")
}

func TestAudit_InjectedGateOnAnotherScopeIsFound(t *testing.T) {
	root := treeCopy(t)
	inject(t, root, listUC, gateStmt,
		"\tother := scope\n"+strings.Replace(gateStmt, "Scope(scope)", "Scope(other)", 1))
	wantFinding(t, root, "public_notice.List —", "checks a scope other than \"scope\"")
}

func TestAudit_InjectedUndeclaredListingIsFound(t *testing.T) {
	root := treeCopy(t)
	inject(t, root, transport,
		"func (s *publicNoticeHandler) GetByAccount(",
		"func (s *publicNoticeHandler) ListArchived(ctx context.Context, r *notifyv1.ListNoticesRequest) (*notifyv1.ListNoticesResponse, error) {\n\treturn s.uc.List.Execute(ctx, r)\n}\n\nfunc (s *publicNoticeHandler) GetByAccount(")
	wantFinding(t, root, "public_notice.ListArchived —", "no declared enforcement")
}

func TestAudit_InjectedTransportTypeOffConventionIsFound(t *testing.T) {
	root := treeCopy(t)
	p := filepath.Join(root, transport)
	raw, err := os.ReadFile(p) // #nosec G304 -- test copy under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.ReplaceAll(string(raw), "publicNoticeHandler", "publicNoticeServer")), 0o600); err != nil {
		t.Fatal(err)
	}
	wantFinding(t, root, "publicNoticeServer.List —", "attributed to no resource")
	wantFinding(t, root, `declared listing "public_notice.List" matches no method`)
}

func TestAudit_InjectedDecidingTransportIsFound(t *testing.T) {
	root := treeCopy(t)
	inject(t, root, transport,
		"\treturn s.uc.ListByAccount.Execute(ctx, r)\n",
		"\tif r == nil {\n\t\treturn nil, nil\n\t}\n\treturn s.uc.ListByAccount.Execute(ctx, r)\n")
	wantFinding(t, root, "public_notice.ListByAccount —", "does not delegate to exactly one use-case")
}

func TestAudit_InjectedThirdNarrowerSiteIsFound(t *testing.T) {
	root := treeCopy(t)
	inject(t, root, "services/notify/cmd/notify-probe/serve.go",
		"\treturn n, func() { _ = conn.Close() }, nil\n",
		"\t_ = listnarrow.New(narrowiam.New(conn), listnarrow.Config{})\n\treturn n, func() { _ = conn.Close() }, nil\n")
	// The same declared file now holds two calls — still declared. A call in an
	// undeclared file is the finding:
	root2 := treeCopy(t)
	inject(t, root2, "services/notify/cmd/notify-api/serve.go",
		"\tnarrower, err := authzwiring.NewListNarrower(narrowiam.New(conn), cfg.ListFilter(), rt.Now)\n",
		"\t_ = listnarrow.New(narrowiam.New(conn), listnarrow.Config{})\n\tnarrower, err := authzwiring.NewListNarrower(narrowiam.New(conn), cfg.ListFilter(), rt.Now)\n")
	inject(t, root2, "services/notify/cmd/notify-api/serve.go",
		"\t\"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowiam\"\n",
		"\t\"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowiam\"\n\t\"github.com/PRO-Robotech/corelib/listnarrow\"\n")
	wantFinding(t, root2, "cmd/notify-api/serve.go:", "outside the declared narrower sites")
	if rep, err, out := run(t, root); err != nil || len(rep.NarrowerCalls) != 3 {
		t.Fatalf("second call in a declared site: err=%v calls=%v\n%s", err, rep.NarrowerCalls, out)
	}
}

func TestAudit_InjectedForeignNarrowerClientIsFound(t *testing.T) {
	root := treeCopy(t)
	inject(t, root, "services/notify/cmd/notify-api/serve.go",
		"authzwiring.NewListNarrower(narrowiam.New(conn),",
		"authzwiring.NewListNarrower(nil,")
	wantFinding(t, root, "NewListNarrower is called with client nil", "narrowiam.New")
}

func TestAudit_InjectedEnumeratingMethodReachedIsFound(t *testing.T) {
	root := treeCopy(t)
	inject(t, root, "pkg/authz/authziam/check.go",
		"type checkClient struct {",
		"func (c *checkClient) ListAllowed(subject string) ([]string, error) { return nil, nil }\n\ntype checkClient struct {")
	inject(t, root, listUC, readStmt,
		"\t_ = u.deps.Checker.(interface{ ListAllowed(string) ([]string, error) })\n\tif c, ok := u.deps.Checker.(interface{ ListAllowed(string) ([]string, error) }); ok {\n\t\t_, _ = c.ListAllowed(\"\")\n\t}\n"+readStmt)
	wantFinding(t, root, "public_notice.List —", "reaches ListAllowed")
}

func TestAudit_MissingTransportIsNotInspected(t *testing.T) {
	root := treeCopy(t)
	if err := os.RemoveAll(filepath.Join(root, "services/notify/internal/handler")); err != nil {
		t.Fatal(err)
	}
	_, err, out := run(t, root)
	if !errors.Is(err, ErrNotInspected) || !strings.Contains(out, "NOT INSPECTED") {
		t.Fatalf("absent transport must not read as clean or as findings: err=%v\n%s", err, out)
	}
}

// TestAdminEvidence_BothWays — the compiled options of the real admin listing
// pass; each injected option fails with its own reason.
func TestAdminEvidence_BothWays(t *testing.T) {
	real, err := compiledRPCAuthz("kacho.cloud.notify.v1.InternalNoticeService.List")
	if err != nil {
		t.Fatal(err)
	}
	if f := AdminEvidence(real); len(f) != 0 {
		t.Fatalf("compiled InternalNoticeService.List options rejected: %v (%+v)", f, real)
	}
	pub, err := compiledRPCAuthz("kacho.cloud.notify.v1.NoticeService.List")
	if err != nil {
		t.Fatal(err)
	}
	if f := AdminEvidence(pub); len(f) == 0 {
		t.Fatalf("the exempt public List passed as an admin surface: %+v", pub)
	}
	for name, mut := range map[string]func(*RPCAuthz){
		"exempt":     func(o *RPCAuthz) { o.Permission = "<exempt>" },
		"relation":   func(o *RPCAuthz) { o.RequiredRelation = "" },
		"scope type": func(o *RPCAuthz) { o.ScopeObjectType = "project" },
		"scope from": func(o *RPCAuthz) { o.ScopeField = "project_id" },
	} {
		o := real
		mut(&o)
		if f := AdminEvidence(o); len(f) != 1 {
			t.Errorf("%s: want exactly one finding, got %v", name, f)
		}
	}
}
