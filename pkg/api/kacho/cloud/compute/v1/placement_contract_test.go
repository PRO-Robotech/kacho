// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package computev1

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Host affinity is withdrawn from the compute contract.
//
// Two reasons, and either one alone would be enough.
//
// It was never read. No handler, use-case or repository of kacho-compute referenced
// the field, and no migration ever stored it: a caller could send a fully formed rule
// pinning a machine to a host and get a success back with nothing applied anywhere.
// A request field the service does not look at cannot be accepted silently — the
// lawful outcomes are implement it, refuse it by name, or withdraw it, and the default
// is to withdraw (`api-conventions.md`, "Принято-и-проигнорировано — ЗАПРЕЩЕНО").
//
// And it stood on the wrong surface. A host-affinity rule names hosts and host groups,
// which is placement physics; by platform rule that class of data lives ONLY in the
// `Internal*` API on the cluster-internal listener, never on the public one
// (`security.md`, "Инфра-чувствительные данные"). So the public field was both
// unimplemented and in the wrong lane, and implementing it where it stood was not an
// option. If placement management is wanted it is introduced on the internal contour
// with its own acceptance.
//
// THE CARRIER GOES TOO, AND NOTHING IS RESERVED — because there is no slot to reserve.
//
// The first pass withdrew the field and reserved its number and name inside
// `PlacementPolicy`. Re-measured by TYPE rather than by name, that carrier turned out to
// be a full orphan: no field of any message in any package has type `PlacementPolicy`,
// and no code outside the generated stubs mentions the type at all, so nothing can
// construct or read one. Its two remaining fields (`placement_group_id`,
// `placement_group_partition`) are therefore unreadable as well — not by policy, by
// construction.
//
// `placement_group_id` is a HOMONYM, and that is what made the first reading look
// contradictory. Four other messages declare a field of the same name, and three of them
// are alive: `Instance` (41), `CreateInstanceRequest` (38) and `UpdateInstanceRequest`
// (22) — read as a struct field by internal/handler, carried by protoconv, stored in a
// column and named in the update-mask known-set. The fifth, `DiskPlacementPolicy` (1),
// is a different message again, reachable through Relocate. Only the copy INSIDE the
// carrier is dead. A predicate keyed on the field NAME cannot tell these apart; one keyed
// on the type can, which is why the conclusion is drawn from the type.
//
// Reserving numbers inside a deleted message is impossible, and here it is also
// pointless: numbers are reserved in the message that DECLARES the field, and a message
// no field points at never appeared on any wire — no peer can have sent field 2 of
// `PlacementPolicy`, because no request or response ever contained a `PlacementPolicy`.
// So the removal takes the reservation with it and loses nothing. Same standard already
// applied in this tree to the withdrawn vpc DNS messages
// (pkg/api/kacho/cloud/vpc/v1/address_dns_contract_test.go).

// withdrawnPlacementMessages must no longer be declared outside the internal contour.
var withdrawnPlacementMessages = []protoreflect.Name{"HostAffinityRule", "PlacementPolicy"}

// livePlacementGroupIdHolders — the messages whose `placement_group_id` is ALIVE, with
// the number each holds. Locked because the withdrawal above removes a field of the very
// same name: whoever next reads "placement_group_id was withdrawn" must not take these
// with it. Instance placement is configured through them.
var livePlacementGroupIdHolders = map[protoreflect.Name]protoreflect.FieldNumber{
	"Instance":              41,
	"CreateInstanceRequest": 38,
	"UpdateInstanceRequest": 22,
}

// The walk over the package — including nested messages, which is where the withdrawn
// rule message was declared — is the shared `computeMessages` helper of this package
// (list_ordering_contract_test.go); its recursion was added for exactly this reason.

// internalReachable — the set of message full-names reachable from a service whose name
// begins with `Internal` (cluster-internal listener, ban #6), expanded transitively over
// message-typed fields.
//
// WHY THE GATE IS SCOPED THIS WAY AND NOT PACKAGE-WIDE. The rule is not "the words host
// affinity are forbidden in compute" — it is "placement physics does not appear where
// tenants reach it; it lives in the Internal* API" (security.md). A package-wide ban
// would fail the day placement management is introduced where it belongs, and the next
// contributor would delete the gate rather than the field. So the ban covers everything
// EXCEPT the internal contour.
//
// Note what that means for a message reachable from NEITHER kind of service — which is
// what PlacementPolicy is today, an orphan no field points at. The ban still applies to
// it, and deliberately: an orphan is not the internal placement API, it is dead shape,
// and dead shape carrying host affinity is exactly what was withdrawn here.
//
// The premise is stated so it can be re-checked: the set is computed from the descriptors
// at run time, not from a hand-kept list, and the test refuses to pass on an empty set.
func internalReachable(t *testing.T) map[protoreflect.FullName]bool {
	t.Helper()
	byName := map[protoreflect.FullName]protoreflect.MessageDescriptor{}
	computeMessages(t, func(md protoreflect.MessageDescriptor) {
		byName[md.FullName()] = md
	})

	out := map[protoreflect.FullName]bool{}
	var queue []protoreflect.FullName
	services := 0
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "kacho.cloud.compute.v1" {
			return true
		}
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			sd := svcs.Get(i)
			if !strings.HasPrefix(string(sd.Name()), "Internal") {
				continue
			}
			services++
			methods := sd.Methods()
			for j := 0; j < methods.Len(); j++ {
				m := methods.Get(j)
				queue = append(queue, m.Input().FullName(), m.Output().FullName())
			}
		}
		return true
	})
	for len(queue) > 0 {
		name := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if out[name] {
			continue
		}
		md, ok := byName[name]
		if !ok {
			// Types from other packages (operation.Operation, google.*) are not part of
			// this domain's own contract surface.
			continue
		}
		out[name] = true
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			if f.Kind() == protoreflect.MessageKind || f.Kind() == protoreflect.GroupKind {
				queue = append(queue, f.Message().FullName())
			}
		}
	}
	if services == 0 || len(out) == 0 {
		t.Fatalf("internal surface came out empty (services=%d, messages=%d) — the gate "+
			"would then ban nothing, or ban everything, and either way it would not be "+
			"asserting what it claims", services, len(out))
	}
	t.Logf("внутренняя поверхность compute: сервисов %d, сообщений %d", services, len(out))
	return out
}

// TestNoMessageDeclaresHostAffinity — no message outside compute's internal contour
// declares a host-affinity field. Computing the exempt set instead of naming one message
// means a future message cannot quietly reintroduce the knob where callers reach it,
// while introducing placement management on the internal listener — where it belongs —
// does not trip a gate written to keep it off the public one.
func TestNoMessageDeclaresHostAffinity(t *testing.T) {
	banned := map[protoreflect.Name]string{
		"host_affinity_rules": "hostAffinityRules",
		"host_affinity_rule":  "hostAffinityRule",
	}
	internal := internalReachable(t)
	computeMessages(t, func(md protoreflect.MessageDescriptor) {
		if internal[md.FullName()] {
			return
		}
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			for protoName, jsonName := range banned {
				if f.Name() == protoName || f.JSONName() == jsonName {
					t.Errorf("%s declares host-affinity field %q (number %d): pinning a "+
						"machine to a host is placement physics — it belongs to the "+
						"Internal* API on the cluster-internal listener, and no code in "+
						"the service reads it", md.FullName(), f.Name(), f.Number())
				}
			}
		}
	})
}

// TestLivePlacementGroupIdSurvives — the three live `placement_group_id` fields are
// still declared, each with its own number.
//
// This is the lock the homonym needs. The withdrawal above removes a field spelled
// exactly the same, so a later sweep reading "placement_group_id is gone from compute"
// could take the live ones with it and silently disable instance placement — the fields
// the handler reads, protoconv carries and the update-mask known-set names. The test
// fails if any of them disappears or changes number.
func TestLivePlacementGroupIdSurvives(t *testing.T) {
	found := map[protoreflect.Name]protoreflect.FieldNumber{}
	computeMessages(t, func(md protoreflect.MessageDescriptor) {
		want, ok := livePlacementGroupIdHolders[md.Name()]
		if !ok {
			return
		}
		f := md.Fields().ByName("placement_group_id")
		if f == nil {
			t.Errorf("%s no longer declares placement_group_id: this is the LIVE field "+
				"(handler reads it, protoconv carries it, the update-mask known-set names "+
				"it) — not the withdrawn homonym inside the removed carrier", md.FullName())
			return
		}
		found[md.Name()] = f.Number()
		if f.Number() != want {
			t.Errorf("%s.placement_group_id moved from %d to %d: the number is wire "+
				"identity, it does not move", md.FullName(), want, f.Number())
		}
	})
	for name := range livePlacementGroupIdHolders {
		if _, ok := found[name]; !ok {
			t.Errorf("%s was never visited — the message this test names is not declared "+
				"in the package, so the assertion about it never ran", name)
		}
	}
}

// TestWithdrawnPlacementMessagesAreGone — the nested rule message AND its orphan
// carrier are removed. Leaving it behind keeps advertising a shape the service cannot honour
// and invites a new field to reach for it. Scoped like the field ban: declaring such a
// message on the internal contour is lawful, declaring it anywhere else is not.
func TestWithdrawnPlacementMessagesAreGone(t *testing.T) {
	internal := internalReachable(t)
	computeMessages(t, func(md protoreflect.MessageDescriptor) {
		if internal[md.FullName()] {
			return
		}
		for _, gone := range withdrawnPlacementMessages {
			if md.Name() == gone {
				t.Errorf("kacho.cloud.compute.v1 still declares message %s: no field of "+
					"any message has this type, so nothing can construct or read one — "+
					"keeping it advertises a shape the service cannot honour",
					md.FullName())
			}
		}
	})
}
