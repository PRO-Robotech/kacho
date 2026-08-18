// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listfiltergate

// enumeration_test.go — the derived ban, asserted in BOTH directions on fixtures
// small enough that each case isolates one property.
//
// The property under test is the discrimination itself: a call that PRODUCES a set
// of identifiers is an enumeration, a call that judges identifiers it was handed is
// not. A test that only ever saw the first kind would prove the gate fires, and
// nothing about what it lets through — and "lets through" is the whole risk here,
// because the previous ban let a whole form through for months (#651).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree materialises files under a throwaway root and returns it.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return root
}

// TestDeriveEnumerations_TellsAnAnswerFromAVerdict — one source, every result shape
// the tree actually uses, and the expectation stated per method.
func TestDeriveEnumerations_TellsAnAnswerFromAVerdict(t *testing.T) {
	root := writeTree(t, map[string]string{"internal/clients/port.go": `package clients

type Relations interface {
	// PRODUCES ids → enumeration.
	ListObjects(ctx context.Context, subject, relation, objectType string) ([]string, error)
	// PRODUCES ids, with a continuation alongside → still an enumeration: the token
	// rides with the answer, it is not the answer.
	ListSubjects(ctx context.Context, objectType, id, relation string) ([]string, string, error)
	// JUDGES ids the caller holds → not an enumeration.
	Check(ctx context.Context, subject, relation, object string) (bool, error)
	BatchCheck(ctx context.Context, subject, relation string, objects []string) ([]bool, error)
	VisibleSet(ctx context.Context, subject string, objects []string) (map[string]bool, error)
	// Neither: a tree, and a count.
	Expand(ctx context.Context, object, relation string) (*ExpandTree, error)
	CountVisible(ctx context.Context, subject string) (int, error)
	// No result at all.
	Warm(ctx context.Context)
}
`})

	got := deriveEnumerations(root, []EnumerationSource{{Dir: "internal/clients", Type: "Relations"}})

	if len(got.Findings) != 0 {
		t.Fatalf("derivation reported findings on a healthy source: %v", got.Findings)
	}
	want := []string{"ListObjects", "ListSubjects"}
	if strings.Join(got.Names, ",") != strings.Join(want, ",") {
		t.Fatalf("derived %v; want %v — a verdict must never be read as an enumeration, "+
			"and an enumeration must never be missed", got.Names, want)
	}
	if got.Origin["ListObjects"] != "internal/clients.Relations" {
		t.Fatalf("origin of a derived name must NAME its source; got %q", got.Origin["ListObjects"])
	}
	if len(got.Sources) != 1 || !strings.Contains(got.Sources[0], "8 method(s), 2 enumerating") {
		t.Fatalf("the census must state what was read; got %v", got.Sources)
	}
}

// TestDeriveEnumerations_ReadsMethodsDeclaredOnAConcreteType — the second form the
// tree uses. iam asks the store through an interface and its own database through a
// concrete type; a derivation that read only interfaces would have derived half the
// ban and said nothing about the other half — which is exactly the half #651 is about.
func TestDeriveEnumerations_ReadsMethodsDeclaredOnAConcreteType(t *testing.T) {
	root := writeTree(t, map[string]string{"internal/repo/verdict/asker.go": `package verdict

type Asker struct{ pool *pgxpool.Pool }

func (a *Asker) Allowed(ctx context.Context, subject, object string) (bool, error) { return false, nil }

func (a *Asker) Objects(ctx context.Context, subject, objectType string, relations []string, limit int) ([]string, bool, error) {
	return nil, false, nil
}

// unexported: not a call another package can make.
func (a *Asker) objects(ctx context.Context) ([]string, error) { return nil, nil }

// a different receiver: not this source.
func (o *Other) Objects(ctx context.Context) ([]string, error) { return nil, nil }
`})

	got := deriveEnumerations(root, []EnumerationSource{{Dir: "internal/repo/verdict", Type: "Asker"}})

	if len(got.Findings) != 0 {
		t.Fatalf("unexpected findings: %v", got.Findings)
	}
	if strings.Join(got.Names, ",") != "Objects" {
		t.Fatalf("derived %v; want [Objects] — the receiver TYPE decides membership, not the name", got.Names)
	}
}

// TestDeriveEnumerations_SourceWithNothingLeftToDescribeIsAFinding — the self-expiry.
// A source that stopped resolving takes its whole derived ban with it; silence there
// would leave every listing judged against a narrower ban than the profile declares,
// and the run would still print OK.
func TestDeriveEnumerations_SourceWithNothingLeftToDescribeIsAFinding(t *testing.T) {
	root := writeTree(t, map[string]string{"internal/clients/port.go": `package clients

type Renamed interface {
	ListObjects(ctx context.Context, subject string) ([]string, error)
}
`})

	got := deriveEnumerations(root, []EnumerationSource{
		{Dir: "internal/clients", Type: "Relations"},
		{Dir: "internal/nowhere", Type: "Relations"},
	})

	if len(got.Findings) != 2 {
		t.Fatalf("want a finding for the missing TYPE and one for the missing DIRECTORY; got %v", got.Findings)
	}
	if !strings.Contains(got.Findings[0], "no such type is declared") {
		t.Fatalf("the finding must say the entry has nothing left to describe; got %q", got.Findings[0])
	}
	if !strings.Contains(got.Findings[1], "could not be read") {
		t.Fatalf("an unreadable source must not read as an empty one; got %q", got.Findings[1])
	}
	if len(got.Names) != 0 {
		t.Fatalf("a source that did not resolve must derive nothing; got %v", got.Names)
	}
}

// TestDeriveEnumerations_SourceThatDerivesNothingIsAFinding — the premise check.
// The source was named BECAUSE it answers "which objects". A method set holding no
// such shape means the rule stopped matching the tree, and an EMPTY ban derived in
// silence is worse than a wrong one: nothing says it happened.
func TestDeriveEnumerations_SourceThatDerivesNothingIsAFinding(t *testing.T) {
	root := writeTree(t, map[string]string{"internal/clients/port.go": `package clients

type Relations interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
	// The answer changed form — a declared id type instead of a bare string. The
	// rule no longer matches, and the gate has to SAY so rather than derive nothing.
	ListObjects(ctx context.Context, subject string) ([]ObjectID, error)
}
`})

	got := deriveEnumerations(root, []EnumerationSource{{Dir: "internal/clients", Type: "Relations"}})

	if len(got.Findings) != 1 || !strings.Contains(got.Findings[0], "no enumerating shape at all") {
		t.Fatalf("want the premise check to fire; got %v", got.Findings)
	}
}

// TestAudit_DerivedNameIsBannedInsideANarrowingListing — end to end through Audit,
// in both directions: the same call, banned under a narrowing shape and legitimate
// under StoreQuery, where the enumeration is what the caller asked for.
func TestAudit_DerivedNameIsBannedInsideANarrowingListing(t *testing.T) {
	source := `package clients

type Relations interface {
	Objects(ctx context.Context, subject, objectType string) ([]string, error)
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}
`
	listing := `package user

type Handler struct{ uc *UseCase }

func (h *Handler) List(ctx context.Context) error { return h.uc.Execute(ctx) }

func (u *UseCase) Execute(ctx context.Context) error {
	ids, _ := u.relations.Objects(ctx, "user:u", "iam_user")
	_ = ids
	return nil
}
`
	p := Profile{
		Service:        "fixture-derived",
		AnchorRoot:     "internal/api",
		PerPackage:     true,
		ReceiverSuffix: "Handler",
		Filters:        []string{"VisibleSet"},
		EnumerationSources: []EnumerationSource{
			{Dir: "internal/clients", Type: "Relations"},
		},
	}
	files := map[string]string{
		"internal/clients/port.go":  source,
		"internal/api/user/list.go": listing,
	}

	// Narrowing shape → the call is refused, and the message names the source that
	// made it a ban rather than leaving the reader to guess where it came from.
	out, err := run(t, p, files, map[string]Listing{"user.List": {Shape: RowFilter}})
	if err == nil {
		t.Fatalf("a listing that takes its page from an enumeration must be refused; output:\n%s", out)
	}
	if !strings.Contains(out, "reaches Objects") || !strings.Contains(out, "internal/clients.Relations") {
		t.Fatalf("the finding must name the call AND its source; got:\n%s", out)
	}
	if !strings.Contains(out, "list.go") {
		t.Fatalf("the finding must carry the coordinate of the declaration; got:\n%s", out)
	}

	// The LEGAL twin of the same outward form: the enumeration IS the response. Same
	// call, same tree, different shape — and the gate must be silent. Without this
	// the case above would only prove the gate fires on the token.
	out, err = run(t, p, files, map[string]Listing{
		"user.List": {Shape: StoreQuery, Gate: "authorizeCaller"},
	})
	if err == nil {
		t.Fatalf("StoreQuery still owes ParentGate's evidence; expected a finding about the gate, got:\n%s", out)
	}
	if strings.Contains(out, "reaches Objects") {
		t.Fatalf("the enumeration ban must NOT apply where the enumeration is the response; got:\n%s", out)
	}
}

// TestAudit_CensusStatesHowTheBanWasObtained — a run that derived nothing must not
// read like a run that derived and found nothing to ban. The ban is the part of this
// gate that comes from the tree rather than from a list, so it is printed on every
// path, including the passing one.
func TestAudit_CensusStatesHowTheBanWasObtained(t *testing.T) {
	files := map[string]string{"internal/handler/instance_handler.go": `package handler

type InstanceHandler struct{}

func (h *InstanceHandler) List(ctx context.Context) error {
	rows, _ := h.svc.List(ctx)
	visible, _ := filterVisible(ctx, h.listFilter, rows)
	_ = visible
	return nil
}
`}
	out, err := run(t, flatProfile, files)
	if err != nil {
		t.Fatalf("compliant fixture must pass; got %v\n%s", err, out)
	}
	if !strings.Contains(out, "enumerate-then-narrow ban — 2 call(s)") {
		t.Fatalf("the effective ban must be stated on the passing path; got:\n%s", out)
	}
	if !strings.Contains(out, "no enumeration source declared") {
		t.Fatalf("a profile deriving nothing must SAY so — otherwise it is indistinguishable "+
			"from one whose derivation works; got:\n%s", out)
	}
}
