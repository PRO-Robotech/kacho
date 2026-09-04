// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package vpcv1

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// The vpc address contract used to promise DNS on both sides of the wire:
// CreateAddressRequest field 11 and UpdateAddressRequest field 8 took
// `dns_record_specs` (repeated DnsRecordSpec), and the Address resource itself
// returned `dns_records` (repeated DnsRecord) in field 20.
//
// None of it was read or written. No handler, use-case or repository of kacho-vpc
// ever referenced any of those fields or messages: a caller could send a fully formed
// record spec, get a success back, and no DNS record existed anywhere — while the
// output field came back empty on every Get and List because nothing could fill it.
// There is no table, no column, no worker and no reconciler behind any of it.
//
// It was also not implementable where it stood. A DNS record is not an attribute of
// an address: it needs a zone somebody owns, a record set with its own lifecycle, TTL
// and propagation semantics, and its own authorization — a domain, with its own
// acceptance. `dns_zone_id` even referred to a zone resource this platform does not
// have. Carried on Address it promised callers a feature nobody owned, and the
// published documentation passed that promise on ("поле зарезервировано; обработка отложена").
//
// So it is withdrawn rather than implemented, input and output together — withdrawing
// only the input would leave the resource still advertising records nothing can ever
// produce. Every field number AND every field name stays RESERVED, so a peer still
// sending the old field cannot have its bytes reinterpreted as something else later.
// The two messages go with them: once no field references them, keeping them
// advertises a shape the service cannot honour. When DNS is built it will be its own
// domain with its own messages.

// vacatedDnsFields maps each message that used to carry a DNS knob to the field it
// occupied. Those numbers and names must stay reserved.
var vacatedDnsFields = map[string]struct {
	number protoreflect.FieldNumber
	name   protoreflect.Name
}{
	"CreateAddressRequest": {11, "dns_record_specs"},
	"UpdateAddressRequest": {8, "dns_record_specs"},
	"Address":              {20, "dns_records"},
}

// withdrawnDnsMessages are the message names that must no longer be declared.
var withdrawnDnsMessages = []protoreflect.Name{"DnsRecordSpec", "DnsRecord"}

// vpcMessages walks every message of the kacho.cloud.vpc.v1 package.
func vpcMessages(t *testing.T, visit func(protoreflect.MessageDescriptor)) {
	t.Helper()
	seen := 0
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "kacho.cloud.vpc.v1" {
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
		t.Fatal("no kacho.cloud.vpc.v1 messages found in the global registry — " +
			"the walk found nothing, so it would have asserted nothing")
	}
}

// TestNoMessageDeclaresDnsFields — no message of the vpc domain declares a DNS field,
// on the request side or on the resource. Walking the package rather than a hand-kept
// list means a future message cannot quietly reintroduce one.
func TestNoMessageDeclaresDnsFields(t *testing.T) {
	banned := map[protoreflect.Name]string{
		"dns_record_specs": "dnsRecordSpecs",
		"dns_records":      "dnsRecords",
	}
	vpcMessages(t, func(md protoreflect.MessageDescriptor) {
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			for proto, json := range banned {
				if f.Name() == proto || f.JSONName() == json {
					t.Errorf("%s declares DNS field %q (number %d): DNS is a domain of "+
						"its own — zones, record sets, TTL, propagation — not an attribute "+
						"of an address, and no code in the service reads or writes it",
						md.Name(), f.Name(), f.Number())
				}
			}
		}
	})
}

// TestVacatedDnsNumbersStayReserved — the withdrawal is announced in the contract
// itself: both the name and the number each message held are reserved, so the slot
// cannot be reused with different semantics against a peer still sending the old one.
func TestVacatedDnsNumbersStayReserved(t *testing.T) {
	checked := map[string]bool{}

	vpcMessages(t, func(md protoreflect.MessageDescriptor) {
		name := string(md.Name())
		want, ok := vacatedDnsFields[name]
		if !ok {
			return
		}
		checked[name] = true

		names := md.ReservedNames()
		haveName := false
		for i := 0; i < names.Len(); i++ {
			if names.Get(i) == want.name {
				haveName = true
			}
		}
		if !haveName {
			t.Errorf("%s does not reserve the name %q", name, want.name)
		}

		ranges := md.ReservedRanges()
		haveNum := false
		for i := 0; i < ranges.Len(); i++ {
			r := ranges.Get(i)
			if want.number >= r[0] && want.number < r[1] {
				haveNum = true
			}
		}
		if !haveNum {
			t.Errorf("%s does not reserve field number %d", name, want.number)
		}
	})

	for name := range vacatedDnsFields {
		if !checked[name] {
			t.Errorf("%s was never visited — the message this test names does not exist "+
				"in the package, so the assertion about it never ran", name)
		}
	}
}

// TestDnsMessagesAreGone — the messages the withdrawn fields pointed at are removed
// too. Leaving them behind would keep advertising a shape the service has no way to
// honour, and would invite a new field to reach for them.
func TestDnsMessagesAreGone(t *testing.T) {
	vpcMessages(t, func(md protoreflect.MessageDescriptor) {
		for _, gone := range withdrawnDnsMessages {
			if md.Name() == gone {
				t.Errorf("kacho.cloud.vpc.v1 still declares message %s: no field "+
					"references it and the service implements no DNS behaviour",
					md.FullName())
			}
		}
	})
}
