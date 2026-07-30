// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

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
// The number AND the name stay reserved, so a slot a peer might once have populated
// cannot be reused with different semantics later. The nested rule message goes with
// the field: once no field references it, keeping it advertises a shape the service has
// no way to honour (the same standard applied to the withdrawn vpc DNS messages —
// pkg/api/kacho/cloud/vpc/v1/address_dns_contract_test.go).

// vacatedPlacementField — the message that carried the knob and the slot it occupied.
var vacatedPlacementField = struct {
	message protoreflect.Name
	number  protoreflect.FieldNumber
	name    protoreflect.Name
}{"PlacementPolicy", 2, "host_affinity_rules"}

// withdrawnPlacementMessages must no longer be declared on the public surface.
var withdrawnPlacementMessages = []protoreflect.Name{"HostAffinityRule"}

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

// TestVacatedHostAffinitySlotStaysReserved — the withdrawal is announced in the
// contract itself: both the name and the number stay reserved, so the slot cannot be
// reused with different semantics.
func TestVacatedHostAffinitySlotStaysReserved(t *testing.T) {
	visited := false
	computeMessages(t, func(md protoreflect.MessageDescriptor) {
		if md.Name() != vacatedPlacementField.message {
			return
		}
		visited = true

		names := md.ReservedNames()
		haveName := false
		for i := 0; i < names.Len(); i++ {
			if names.Get(i) == vacatedPlacementField.name {
				haveName = true
			}
		}
		if !haveName {
			t.Errorf("%s does not reserve the name %q", md.FullName(), vacatedPlacementField.name)
		}

		ranges := md.ReservedRanges()
		haveNum := false
		for i := 0; i < ranges.Len(); i++ {
			r := ranges.Get(i)
			if vacatedPlacementField.number >= r[0] && vacatedPlacementField.number < r[1] {
				haveNum = true
			}
		}
		if !haveNum {
			t.Errorf("%s does not reserve field number %d", md.FullName(),
				vacatedPlacementField.number)
		}
	})
	if !visited {
		t.Errorf("%s was never visited — the message this test names is not declared in "+
			"the package, so the assertion about it never ran",
			vacatedPlacementField.message)
	}
}

// TestHostAffinityMessagesAreGone — the message the withdrawn field pointed at is
// removed too. Leaving it behind keeps advertising a shape the service cannot honour
// and invites a new field to reach for it. Scoped like the field ban: declaring such a
// message on the internal contour is lawful, declaring it anywhere else is not.
func TestHostAffinityMessagesAreGone(t *testing.T) {
	internal := internalReachable(t)
	computeMessages(t, func(md protoreflect.MessageDescriptor) {
		if internal[md.FullName()] {
			return
		}
		for _, gone := range withdrawnPlacementMessages {
			if md.Name() == gone {
				t.Errorf("kacho.cloud.compute.v1 still declares message %s: no field "+
					"references it and the service implements no host-affinity behaviour",
					md.FullName())
			}
		}
	})
}
