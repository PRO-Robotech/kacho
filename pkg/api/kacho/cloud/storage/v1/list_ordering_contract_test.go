// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package storagev1

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// A List RPC in this domain reads its page with a keyset cursor over
// (created_at, id): the page token IS that pair, and the WHERE clause compares
// against it. A caller-chosen ordering has no cursor to travel on — the token would
// keep describing a position in an order the query no longer produces — and an
// ordering by any unindexed column turns a page read into a full scan. So
// `order_by` was never implementable as declared.
//
// It was also never read. No handler, use-case or repository in this repo ever
// referenced it: every value a caller sent was accepted, length-checked against
// "<=100" and thrown away. Its documented default ("id asc") did not even describe
// the order the query produced ((created_at, id) ascending), so the one part of the
// comment a caller could have relied on was wrong too.
//
// Removing it is a breaking change, made deliberately: the field number stays
// RESERVED so it can never return meaning something else on the wire. A descending
// listing, if it is ever wanted, is its own deliberate contract with a closed set of
// values — not a free-form string that could only ever legally hold one of two.

// vacatedOrderBy maps each List…Request that used to carry the sort knob to the
// field number it occupied. Those numbers must stay reserved.
var vacatedOrderBy = map[string]protoreflect.FieldNumber{
	"ListVolumesRequest":   5,
	"ListSnapshotsRequest": 5,
	"ListImagesRequest":    5,
}

// storageMessages walks every message of the kacho.cloud.storage.v1 package.
func storageMessages(t *testing.T, visit func(protoreflect.MessageDescriptor)) {
	t.Helper()
	seen := 0
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "kacho.cloud.storage.v1" {
			return true
		}
		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			seen++
			visit(msgs.Get(i))
		}
		return true
	})
	if seen == 0 {
		t.Fatal("no kacho.cloud.storage.v1 messages found in the global registry — " +
			"the walk found nothing, so it would have asserted nothing")
	}
}

// TestListRequestsOfferNoSortKnob — no List…Request of the storage domain declares
// a sort field.
func TestListRequestsOfferNoSortKnob(t *testing.T) {
	storageMessages(t, func(md protoreflect.MessageDescriptor) {
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
// itself: both the name and the number it held are reserved, so the slot cannot be
// silently reused with different semantics by a wire peer still sending the old one.
func TestVacatedOrderByNumbersStayReserved(t *testing.T) {
	checked := map[string]bool{}

	storageMessages(t, func(md protoreflect.MessageDescriptor) {
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
