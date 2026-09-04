// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package computev1

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Same contract, same reasoning as the storage counterpart
// (pkg/api/kacho/cloud/storage/v1/list_ordering_contract_test.go): a List RPC here
// pages with a keyset cursor over (created_at, id), so a caller-chosen ordering has
// no cursor to travel on and an ordering by an unindexed column turns a page read
// into a full scan. `order_by` was declared on four compute List requests, read by
// nothing, and documented with a default ("id asc") that did not describe the order
// the query produced. It is removed, and its number stays reserved.
//
// compute is not an afterthought here: its four copies are the same dead field with
// the same wrong comment, and compute's own api-surface doc and published documentation advertised
// it to callers as a working parameter.

// vacatedOrderBy maps each List…Request that used to carry the sort knob to the
// field number it occupied.
var vacatedOrderBy = map[string]protoreflect.FieldNumber{
	"ListInstancesRequest": 5,
}

// computeMessages walks EVERY message of the kacho.cloud.compute.v1 package, nested
// ones included.
//
// The recursion is load-bearing, not tidiness. A walk over `fd.Messages()` alone sees
// only top-level declarations, so a banned shape declared one level down — and compute
// declares nested messages, e.g. the withdrawn host-affinity rule inside
// PlacementPolicy — is invisible to it: the gate reports success while the thing it
// bans is still in the contract. Every caller of this helper depends on the walk
// covering the whole package, so the coverage lives here once.
func computeMessages(t *testing.T, visit func(protoreflect.MessageDescriptor)) {
	t.Helper()
	seen := 0
	var walk func(protoreflect.MessageDescriptors)
	walk = func(msgs protoreflect.MessageDescriptors) {
		for i := 0; i < msgs.Len(); i++ {
			md := msgs.Get(i)
			seen++
			visit(md)
			walk(md.Messages())
		}
	}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "kacho.cloud.compute.v1" {
			return true
		}
		walk(fd.Messages())
		return true
	})
	if seen == 0 {
		t.Fatal("no kacho.cloud.compute.v1 messages found in the global registry — " +
			"the walk found nothing, so it would have asserted nothing")
	}
	// Перепись — отдельное утверждение: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	t.Logf("осмотрено сообщений kacho.cloud.compute.v1 (включая вложенные): %d", seen)
}

// TestListRequestsOfferNoSortKnob — no List…Request of the compute domain declares
// a sort field.
func TestListRequestsOfferNoSortKnob(t *testing.T) {
	computeMessages(t, func(md protoreflect.MessageDescriptor) {
		name := string(md.Name())
		if !strings.HasPrefix(name, "List") || !strings.HasSuffix(name, "Request") {
			return
		}
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			if f.Name() == "order_by" || f.JSONName() == "orderBy" {
				t.Errorf("%s declares sort field %q (number %d): a keyset cursor over "+
					"(created_at, id) cannot carry a caller-chosen order, and no code in "+
					"the service reads it", name, f.Name(), f.Number())
			}
		}
	})
}

// TestVacatedOrderByNumbersStayReserved — the removal is announced in the contract
// itself: name and number both reserved, so the slot cannot come back meaning
// something else to a wire peer still sending the old one.
func TestVacatedOrderByNumbersStayReserved(t *testing.T) {
	checked := map[string]bool{}

	computeMessages(t, func(md protoreflect.MessageDescriptor) {
		name := string(md.Name())
		num, ok := vacatedOrderBy[name]
		if !ok {
			return
		}
		checked[name] = true

		names := md.ReservedNames()
		haveName := false
		for i := 0; i < names.Len(); i++ {
			if names.Get(i) == "order_by" {
				haveName = true
			}
		}
		if !haveName {
			t.Errorf("%s does not reserve the name %q", name, "order_by")
		}

		ranges := md.ReservedRanges()
		haveNum := false
		for i := 0; i < ranges.Len(); i++ {
			r := ranges.Get(i)
			if num >= r[0] && num < r[1] {
				haveNum = true
			}
		}
		if !haveNum {
			t.Errorf("%s does not reserve field number %d", name, num)
		}
	})

	for name := range vacatedOrderBy {
		if !checked[name] {
			t.Errorf("%s was never visited — the message this test names does not exist "+
				"in the package, so the assertion about it never ran", name)
		}
	}
}
