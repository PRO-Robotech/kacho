// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr_test

import (
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
)

// TestToStatusKeepsFieldViolationDetails pins the guarantee every use-case now
// relies on: a rich validation error handed to the mapper UNCHANGED comes out with
// its google.rpc.BadRequest detail intact.
//
// This is load-bearing, not decorative. pkg/validate puts the offending field name
// ONLY in the details — the message stays the generic "invalid argument" — so a
// mapper that recognised the error and rebuilt a fresh status.Error(code, text)
// would silently strip the one machine-readable part of the answer. Today the
// pass-through happens because a bare validate error carries no ports sentinel and
// falls to the status.FromError arm; this test makes that an asserted property
// rather than an accident of the switch order.
func TestToStatusKeepsFieldViolationDetails(t *testing.T) {
	err := serviceerr.ToStatus(validate.Description("description", strings.Repeat("x", 257)))

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("ToStatus returned a non-status error: %v", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", st.Code())
	}

	var fields []string
	for _, d := range st.Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			fields = append(fields, v.GetField())
		}
	}
	if len(fields) != 1 || fields[0] != "description" {
		t.Fatalf("field violations = %v, want exactly [description]", fields)
	}
}
