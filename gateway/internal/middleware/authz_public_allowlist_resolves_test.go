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
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// The contract side of the comparison. protoregistry.GlobalFiles only holds
	// descriptors for packages LINKED into this binary, so every proto package
	// the gateway serves is blank-imported here. An entry whose package is
	// missing from this list does not read as "dead" — it reads as "the gate
	// cannot see it" and fails with a different message (see resolveNotLinked).
	_ "google.golang.org/grpc/health/grpc_health_v1"
	_ "google.golang.org/grpc/reflection/grpc_reflection_v1"
	_ "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"

	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/apigateway/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

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

// contractIndex — everything this binary can prove about the served contract,
// plus the census numbers that make "0 findings" distinguishable from "0 read".
type contractIndex struct {
	packages map[string]struct{}
	files    int
	services int
	methods  int
}

func buildContractIndex() *contractIndex {
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
		return resolveNotLinked, fmt.Sprintf("proto package %q is not linked into this test binary", pkg)
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
	ci := buildContractIndex()
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
	t.Logf("census: %d allowlist entr(y/ies) read; compared against %d method(s) in %d service(s) "+
		"across %d linked proto package(s) (%d descriptor file(s))",
		len(entries), ci.methods, ci.services, len(ci.packages), ci.files)

	if len(entries) == 0 {
		// A legitimate end state, but it is NOT the same outcome as "checked N,
		// all fine" — say so, or an emptied list looks like a passing audit.
		t.Log("census: the allowlist is EMPTY — this gate had nothing to check " +
			"(that is a different fact from 'every entry resolved')")
		return
	}

	var dead, blind []string
	for _, fqn := range entries {
		switch r, why := ci.resolve(fqn); r {
		case resolveOK:
		case resolveNotLinked:
			blind = append(blind, fmt.Sprintf("%s — %s", fqn, why))
		default:
			dead = append(dead, fmt.Sprintf("%s — %s", fqn, why))
		}
	}

	// Blind is reported separately and first: it is a defect in THIS gate, not a
	// verdict about the entry. Merging the two would let "the gate cannot see
	// package X" be mistaken for "entry X is dead" and get the entry deleted.
	if len(blind) > 0 {
		t.Errorf("GATE IS BLIND to %d of %d entr(y/ies) — blank-import the generated package "+
			"for each, then re-run. This is NOT a finding that they are dead:\n  %s",
			len(blind), len(entries), strings.Join(blind, "\n  "))
	}
	if len(dead) > 0 {
		t.Errorf("%d of %d allowlist entr(y/ies) name an RPC that does not exist in the served contract. "+
			"Each waives authN AND authZ at decide() step 1, so it must name something real or be removed:\n  %s",
			len(dead), len(entries), strings.Join(dead, "\n  "))
	}
}

// TestAllowlistGate_Injection_DeadEntryIsCaughtAndNamed — injection (a).
//
// Feed the predicate an entry of exactly the shape a careless author would add,
// in a package that IS linked, and require that it is caught AND that the
// message carries the coordinate. A gate that reddens without naming the entry
// sends the reader back to the list to guess.
func TestAllowlistGate_Injection_DeadEntryIsCaughtAndNamed(t *testing.T) {
	ci := buildContractIndex()

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
	ci := buildContractIndex()

	for _, live := range []string{
		// Twin of the injected kacho entries — same package, real methods.
		"kacho.cloud.iam.v1.UserService/Get",
		"kacho.cloud.iam.v1.ProjectService/List",
		// Twin from the grpc-go family, and one that is served but unimplemented
		// (Watch): "exists in the contract" is the property, not "returns OK".
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
	ci := buildContractIndex()

	r, why := ci.resolve("no.such.proto.package.SomeService/SomeMethod")
	if r != resolveNotLinked {
		t.Fatalf("an entry in an unlinked package must be reported as %s, got %s (%s) — "+
			"otherwise a blind gate would get a live entry deleted as 'dead'",
			resolveNotLinked, r, why)
	}
	if !strings.Contains(why, "not linked") {
		t.Errorf("the blind-spot reason must say so plainly, got %q", why)
	}
}
