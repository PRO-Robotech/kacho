// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authz_public_allowlist_resolves_test.go — the gate that makes an entry of
// DefaultPublicAllowlist() naming a non-existent RPC fail the build.
//
// WHY A GATE AND NOT A REVIEW. An entry here is not a setting: phaseAllowlist is
// step 1 of decide(), ahead of phaseSubject (step 4, the 401), so membership
// waives authN *and* authZ. An entry that resolves to nothing is not merely
// inert — it is an unreviewable line that reads like a decision, and the next
// person to add a bypass copies its shape. A hand-written exemption list has to
// expire on its own, or it inherits the next blind spot.
//
// WHY IT CALLS THE FUNCTION INSTEAD OF READING THE FILE. The gate's input is the
// value DefaultPublicAllowlist() returns, so a name written in a comment cannot
// satisfy it. That is not a stylistic preference: at the revision this gate was
// written, a naive scan of quoted string literals in authz_public_allowlist.go
// yielded 14 tokens where the slice has 4 — the extra 10 came from the prose
// that explains which RPCs are deliberately NOT listed. A textual gate would
// have counted those explanations as entries and, worse, would have been
// satisfied by an FQN that only ever appeared in a comment.
package middleware_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// The contract side of the comparison. protoregistry.GlobalFiles only holds
	// descriptors for packages LINKED into this binary, so every proto package
	// the gateway serves is blank-imported here. An entry whose package is
	// missing from this set is excused as "the gate cannot see it" ONLY when
	// some .proto in the tree declares that package (declaredProtoPackages);
	// otherwise it is a typo in the package half and is reported dead. Resolving
	// here is necessary but not sufficient — auditAllowlist additionally
	// requires a kacho entry to be routed on the gRPC edge.
	_ "google.golang.org/grpc/health/grpc_health_v1"
	_ "google.golang.org/grpc/reflection/grpc_reflection_v1"
	_ "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"

	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// resolution — the three outcomes an entry can have. They are kept apart on
// purpose: "I looked and it is not there" and "I could not look" are different
// facts, and collapsing them would let a blind gate report a clean tree.
type resolution int

const (
	resolveOK        resolution = iota // the method exists in a linked package
	resolveMissing                     // the package is linked; the service/method is not in it
	resolveNotLinked                   // the package is absent from this binary — gate is blind here
	resolveMalformed                   // not in <proto.package>.<Service>/<Method> form
)

func (r resolution) String() string {
	switch r {
	case resolveOK:
		return "resolved"
	case resolveMissing:
		return "missing"
	case resolveNotLinked:
		return "package-not-linked"
	default:
		return "malformed"
	}
}

// protoPackageDecl matches a package declaration in a .proto source line, after
// comments have been stripped. Reading the declaration rather than the file path
// matters for the same reason the gate reads the returned slice rather than the
// file text: a package name written in a comment must not count.
var protoPackageDecl = regexp.MustCompile(`^\s*package\s+([A-Za-z0-9_.]+)\s*;`)

// declaredProtoPackages walks the repository's proto tree and returns every
// package that a .proto file actually DECLARES.
//
// This is the evidence base for the blind/dead split. Without it, the gate
// decided "the gate cannot see this package" purely from the package being
// absent from the linked registry — which is also true of a typo, so a
// misspelled `kacho.cloud.iam.v2.…` was excused as a gate defect and the reader
// was told to go link a package that does not exist. A package is only credited
// as "real but unlinked" when some .proto in the tree declares it.
func declaredProtoPackages(t *testing.T) (map[string]struct{}, int) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "proto")
	pkgs := map[string]struct{}{}
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".proto") {
			return nil
		}
		scanned++
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, line := range strings.Split(string(src), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			if m := protoPackageDecl.FindStringSubmatch(line); m != nil {
				pkgs[m[1]] = struct{}{}
			}
		}
		return nil
	})
	require.NoError(t, err, "the proto tree must be readable — without it the blind/dead split has no evidence base")
	return pkgs, scanned
}

// contractIndex — everything this binary can prove about the served contract,
// plus the census numbers that make "0 findings" distinguishable from "0 read".
type contractIndex struct {
	packages map[string]struct{} // proto packages LINKED into this binary
	declared map[string]struct{} // proto packages DECLARED anywhere in the tree
	protos   int
	files    int
	services int
	methods  int
}

func buildContractIndex(t *testing.T) *contractIndex {
	t.Helper()
	ci := &contractIndex{packages: map[string]struct{}{}}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		ci.files++
		ci.packages[string(fd.Package())] = struct{}{}
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			ci.services++
			ci.methods += svcs.Get(i).Methods().Len()
		}
		return true
	})
	ci.declared, ci.protos = declaredProtoPackages(t)
	return ci
}

// resolve answers, for one allowlist entry, whether the gRPC method it names
// exists. The returned string is the human-readable reason and always names the
// entry's own coordinates, so a red gate is actionable without reading code.
func (ci *contractIndex) resolve(fqn string) (resolution, string) {
	// Canonical form first. phaseAllowlist keys m.allow on the FQN with the
	// leading slash of gRPC's FullMethod already stripped, so an entry carrying
	// a slash or stray whitespace can never match a routed call — it is dead in
	// production. Rejecting it up front matters because the package-linkage
	// branch below would otherwise classify " grpc.health.v1.Health/Check" as
	// "the gate cannot see this package", i.e. as a defect in the GATE rather
	// than in the entry, and a blind-spot report does not get an entry fixed.
	if fqn != strings.TrimSpace(fqn) {
		return resolveMalformed, fmt.Sprintf("%q carries leading/trailing whitespace and can never match a routed call", fqn)
	}
	if strings.HasPrefix(fqn, "/") {
		return resolveMalformed, fmt.Sprintf("%q keeps the leading slash of gRPC FullMethod; entries are keyed without it", fqn)
	}

	slash := strings.IndexByte(fqn, '/')
	if slash <= 0 || slash == len(fqn)-1 {
		return resolveMalformed, fmt.Sprintf("%q is not in <proto.package>.<Service>/<Method> form", fqn)
	}
	pkgSvc, method := fqn[:slash], fqn[slash+1:]
	dot := strings.LastIndexByte(pkgSvc, '.')
	if dot <= 0 {
		return resolveMalformed, fmt.Sprintf("%q carries no proto package segment", fqn)
	}
	pkg := pkgSvc[:dot]

	if _, ok := ci.packages[pkg]; !ok {
		// Absent from the registry is only blindness if the package REALLY
		// EXISTS. Otherwise it is a typo in the package half of the entry —
		// dead in production, and excusing it as a gate defect would send the
		// reader off to link a package nobody ever wrote.
		if _, real := ci.declared[pkg]; !real {
			return resolveMissing, fmt.Sprintf(
				"no .proto in the tree declares package %q (and it is not a linked grpc-go package) — the package half of %q is wrong",
				pkg, fqn)
		}
		return resolveNotLinked, fmt.Sprintf("proto package %q is declared in the tree but not linked into this test binary", pkg)
	}
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(pkgSvc))
	if err != nil {
		return resolveMissing, fmt.Sprintf("linked package %q contains no service %q", pkg, pkgSvc)
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return resolveMissing, fmt.Sprintf("%q is a %T, not a service", pkgSvc, d)
	}
	if sd.Methods().ByName(protoreflect.Name(method)) == nil {
		return resolveMissing, fmt.Sprintf("service %q has no method %q", pkgSvc, method)
	}
	return resolveOK, ""
}

// TestDefaultPublicAllowlist_EveryEntryResolvesToAServedMethod — THE GATE.
//
// It fails the build when an entry of the authN+authZ bypass list names a
// method that is not in the contract this binary serves.
func TestDefaultPublicAllowlist_EveryEntryResolvesToAServedMethod(t *testing.T) {
	ci := buildContractIndex(t)
	entries := middleware.DefaultPublicAllowlist()

	// ── Premise 1: there is a contract to compare against at all. ──────────
	// Without this, an empty registry (nothing linked, build tags, a future
	// refactor of the gen packages) would make every entry unverifiable and the
	// gate would report a clean tree while having read nothing.
	if ci.services == 0 || ci.methods == 0 {
		t.Fatalf("GATE CANNOT RUN: protoregistry holds %d file(s)/%d service(s)/%d method(s) — "+
			"nothing was linked, so a pass here would mean 'read nothing', not 'found nothing'",
			ci.files, ci.services, ci.methods)
	}

	// ── Premise 2: the predicate actually resolves, one control per family. ─
	// These are positive controls for the gate itself: if the lookup mechanism
	// breaks (descriptor names change, registry API changes), the gate must say
	// "I am broken", never "the tree is clean".
	for _, control := range []string{
		"grpc.health.v1.Health/Check",        // grpc-go family
		"kacho.cloud.iam.v1.UserService/Get", // kacho family
	} {
		if r, why := ci.resolve(control); r != resolveOK {
			t.Fatalf("GATE IS BROKEN: control %q must resolve, got %s (%s) — "+
				"the lookup mechanism no longer works, so no verdict below is trustworthy",
				control, r, why)
		}
	}

	// ── Census. "0 findings" must be distinguishable from "0 read". ────────
	census := fmt.Sprintf("census: %d allowlist entr(y/ies) read; compared against %d method(s) in %d service(s) "+
		"across %d linked proto package(s) (%d descriptor file(s)); %d .proto file(s) scanned declaring %d package(s)",
		len(entries), ci.methods, ci.services, len(ci.packages), ci.files, ci.protos, len(ci.declared))
	t.Log(census)

	// ── Premise 3: there is something to check. ────────────────────────────
	// An emptied list used to be a silent PASS, and CI runs `go test` without
	// -v, so t.Log above is invisible on green: "checked 4, all fine" and
	// "checked nothing" printed the identical `ok`. Empty is also not a
	// legitimate state here — kubelet probes carry no token and depend on the
	// health entries — so absence is asserted rather than merely logged.
	require.NotEmpty(t, entries, "the allowlist is EMPTY: this gate then checks nothing while still printing ok. %s", census)
	require.Contains(t, entries, "grpc.health.v1.Health/Check",
		"the health probe entry is gone; liveness probes carry no bearer token, so this is a posture change, not a cleanup. %s", census)

	dead, blind := auditAllowlist(ci, entries)

	// Blind is reported separately and first: it is a defect in THIS gate, not a
	// verdict about the entry. Merging the two would let "the gate cannot see
	// package X" be mistaken for "entry X is dead" and get the entry deleted.
	//
	// Deliberately NOT phrased as "link the package and re-run": that turns a
	// finding into a pass, which is how an exemption list stops expiring. The
	// oracle is widened only after establishing that the gateway actually
	// routes the RPC.
	if len(blind) > 0 {
		t.Errorf("GATE IS BLIND to %d of %d entr(y/ies). This is NOT a finding that they are dead — and it is "+
			"NOT resolved by linking the package until the entry turns green: first establish that the gateway "+
			"routes the RPC at all.\n  %s\n%s", len(blind), len(entries), strings.Join(blind, "\n  "), census)
	}
	if len(dead) > 0 {
		t.Errorf("%d of %d allowlist entr(y/ies) name an RPC that does not exist in the served contract. "+
			"Each waives authN AND authZ at decide() step 1, so it must name something real or be removed:\n  %s\n%s",
			len(dead), len(entries), strings.Join(dead, "\n  "), census)
	}
}

// auditAllowlist is the gate's reporting body, split out so a test can drive it
// to red with a synthetic list. Left inline, the bucketing and both failure
// messages were reachable only by hand-editing the production list — i.e. the
// part of the gate that actually reports was itself never exercised.
func auditAllowlist(ci *contractIndex, entries []string) (dead, blind []string) {
	for _, fqn := range entries {
		switch r, why := ci.resolve(fqn); r {
		case resolveOK:
			// Resolving in the proto contract is necessary, not sufficient: a
			// kacho RPC that the gRPC edge does not route cannot be reached
			// through this bypass either, so an entry for one is dead weight
			// that reads like a decision. grpc.* entries are natively
			// registered and are not in the edge routing table by design.
			if strings.HasPrefix(fqn, "kacho.") && !allowlist.IsAllowed("/"+fqn) {
				dead = append(dead, fmt.Sprintf(
					"%s — resolves in the proto contract but is not routed on the gRPC edge (absent from allowlist.AllowedMethods)", fqn))
			}
		case resolveNotLinked:
			blind = append(blind, fmt.Sprintf("%s — %s", fqn, why))
		default:
			dead = append(dead, fmt.Sprintf("%s — %s", fqn, why))
		}
	}
	return dead, blind
}

// TestAllowlistGate_Injection_DeadEntryIsCaughtAndNamed — injection (a).
//
// Feed the predicate an entry of exactly the shape a careless author would add,
// in a package that IS linked, and require that it is caught AND that the
// message carries the coordinate. A gate that reddens without naming the entry
// sends the reader back to the list to guess.
func TestAllowlistGate_Injection_DeadEntryIsCaughtAndNamed(t *testing.T) {
	ci := buildContractIndex(t)

	for _, injected := range []string{
		"kacho.cloud.iam.v1.GhostService/Vanish",   // service absent from a linked package
		"kacho.cloud.iam.v1.UserService/Evaporate", // service present, method absent
	} {
		r, why := ci.resolve(injected)
		if r != resolveMissing {
			t.Fatalf("injected dead entry %q must be caught as %s, got %s (%s)",
				injected, resolveMissing, r, why)
		}
		// Naming: the reason must contain enough of the entry to locate it.
		if !strings.Contains(why, "UserService") && !strings.Contains(why, "GhostService") {
			t.Errorf("the reason for %q must name the offending service, got %q", injected, why)
		}
	}

	// Near-misses of a REAL entry. A minimal probe (a made-up service in a
	// linked package) is not representative: the shapes actually produced by a
	// copy-paste are a live FQN with the FullMethod slash still attached, stray
	// whitespace, or the wrong case. Each is dead in production because
	// phaseAllowlist compares strings, so each must be reported as a defect in
	// the ENTRY — never as blindness of the gate, which is a report nobody acts
	// on by fixing the list.
	for _, nearMiss := range []string{
		"/grpc.health.v1.Health/Check",       // FullMethod form, leading slash kept
		" grpc.health.v1.Health/Check",       // leading whitespace
		"grpc.health.v1.Health/Check ",       // trailing whitespace
		"grpc.health.v1.Health/check",        // wrong method case
		"kacho.cloud.iam.v1.userservice/Get", // wrong service case
	} {
		switch r, why := ci.resolve(nearMiss); r {
		case resolveOK:
			t.Errorf("near-miss %q must NOT pass — phaseAllowlist would never match it", nearMiss)
		case resolveNotLinked:
			t.Errorf("near-miss %q was reported as gate-blindness (%s); it is a dead entry, "+
				"and reporting it as a gate defect gets the gate 'fixed' instead of the list", nearMiss, why)
		}
	}
}

// TestAllowlistGate_Injection_LegitimateTwinIsSilent — injection (b).
//
// The same shape, in the same packages, on methods that really are served: the
// gate must say nothing. Without this half, the gate could be catching the FORM
// of an entry rather than its substance, and the first legitimate addition would
// redden the build and get the gate switched off.
func TestAllowlistGate_Injection_LegitimateTwinIsSilent(t *testing.T) {
	ci := buildContractIndex(t)

	for _, live := range []string{
		// Twin of the injected kacho entries — same package, real methods.
		"kacho.cloud.iam.v1.UserService/Get",
		"kacho.cloud.iam.v1.ProjectService/List",
		// Twins from the grpc-go family. Two of these are deliberately NOT on
		// the production list any more — Watch because the edge answers it
		// Unimplemented, ServerReflection because it moved to the cluster-
		// internal listener — and they stay here precisely because of that:
		// this gate's property is "the name exists in the served contract", and
		// a name it must accept is best represented by one that resolves while
		// being disqualified on some OTHER ground. Whether an entry is actually
		// ANSWERED is a different question with a different probe:
		// cmd/api-gateway/public_allowlist_answered_test.go.
		"grpc.health.v1.Health/Check",
		"grpc.health.v1.Health/Watch",
		"grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
	} {
		if r, why := ci.resolve(live); r != resolveOK {
			t.Errorf("legitimate entry %q must pass silently, got %s (%s)", live, r, why)
		}
	}
}

// TestAllowlistGate_BlindnessIsNotMistakenForDeath — the two negative outcomes
// must stay distinguishable. An unlinked package must produce resolveNotLinked
// (a defect in the gate) and never resolveMissing (a verdict on the entry).
func TestAllowlistGate_BlindnessIsNotMistakenForDeath(t *testing.T) {
	ci := buildContractIndex(t)

	// A package that REALLY EXISTS but is not linked here. Declared is seeded
	// directly rather than hunting for an incidentally-unlinked package in the
	// tree: that would make the case vacuous the day someone links it.
	const realButUnlinked = "kacho.cloud.notlinkedhere.v1"
	ci.declared[realButUnlinked] = struct{}{}

	r, why := ci.resolve(realButUnlinked + ".SomeService/SomeMethod")
	if r != resolveNotLinked {
		t.Fatalf("an entry in a REAL but unlinked package must be reported as %s, got %s (%s) — "+
			"otherwise a blind gate would get a live entry deleted as 'dead'",
			resolveNotLinked, r, why)
	}
	if !strings.Contains(why, "not linked") {
		t.Errorf("the blind-spot reason must say so plainly, got %q", why)
	}
}

// TestAllowlistGate_ReportingBodyActuallyBuckets — drives auditAllowlist, the
// part of the gate that reports, with a synthetic list.
//
// Until this existed, every injection exercised ci.resolve only: the bucketing
// and both failure messages were reachable solely by hand-editing the production
// list, so the reporting half of the gate had no automated proof it worked at
// all. A gate whose verdict path is never executed is the shape of gate this
// whole file exists to catch.
func TestAllowlistGate_ReportingBodyActuallyBuckets(t *testing.T) {
	ci := buildContractIndex(t)
	const realButUnlinked = "kacho.cloud.notlinkedhere.v1"
	ci.declared[realButUnlinked] = struct{}{}

	dead, blind := auditAllowlist(ci, []string{
		"grpc.health.v1.Health/Check",                 // healthy — neither bucket
		"kacho.cloud.iam.v1.GhostService/Vanish",      // dead: no such service
		"kacho.cloud.iam.v1.InternalIAMService/Check", // dead: real RPC, not routed on the gRPC edge
		realButUnlinked + ".SomeService/SomeMethod",   // blind: real package, unlinked
	})

	require.Len(t, blind, 1, "exactly the real-but-unlinked entry belongs in blind, got %v", blind)
	require.Len(t, dead, 2, "the absent service and the un-routed RPC both belong in dead, got %v", dead)

	// The messages must carry the coordinate; a bucket count nobody can act on
	// is no better than a silent pass.
	require.Contains(t, strings.Join(dead, "\n"), "GhostService")
	require.Contains(t, strings.Join(dead, "\n"), "not routed on the gRPC edge")
	require.Contains(t, blind[0], realButUnlinked)

	// And the healthy entry must not have been swept into either bucket.
	require.NotContains(t, strings.Join(append(dead, blind...), "\n"), "grpc.health.v1.Health/Check")
}

// TestAllowlistGate_MistypedPackageIsDeadNotBlindness — the dangerous direction,
// and the one the gate originally got backwards.
//
// Blindness is an excuse ("not a finding, go link the package"). Membership of
// that bucket was decided purely by absence from the linked registry, which is
// equally true of a typo — so mistyping the SERVICE half was correctly reported
// dead while mistyping the PACKAGE half was excused, and the reader was sent to
// link a package nobody ever wrote. Version bumps and case slips land squarely
// in the package half, so this is the likelier authoring error of the two.
func TestAllowlistGate_MistypedPackageIsDeadNotBlindness(t *testing.T) {
	ci := buildContractIndex(t)

	for _, mistyped := range []string{
		"kacho.cloud.iam.v2.UserService/Get",     // version typo — no such package
		"Kacho.cloud.iam.v1.UserService/Get",     // wrong-case package
		"kacho.cloud.iam.v1.UserService.Get/Get", // dot where the slash belongs
	} {
		switch r, why := ci.resolve(mistyped); r {
		case resolveNotLinked:
			t.Errorf("mistyped entry %q was excused as gate-blindness (%s) — it is dead in production, "+
				"and the blindness message tells the reader to link a package that does not exist", mistyped, why)
		case resolveOK:
			t.Errorf("mistyped entry %q must not resolve", mistyped)
		}
	}
}
