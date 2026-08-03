// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A field-named refusal must carry the field MACHINE-READABLY, not only in prose.
//
// nlb has two lanes that both produce "INVALID_ARGUMENT naming a field":
//
//   - the domain lane (`coreerrors.InvalidArgument().AddFieldViolation(...)`) —
//     emits google.rpc.BadRequest, and the e2e case that reads
//     `details[].fieldViolations[].field` for a dropped `name` passes against it;
//   - this handler lane (`ErrInvalidArg`) — formatted "<field>: <msg>" and NOTHING
//     in details.
//
// So a caller validating one request message got structured violations for a bad
// address and bare prose for an output-only status, on the same call, with no way
// to tell which to expect. `api-conventions.md` is explicit that a client keys on
// the machine-readable detail and does not parse the message text.
//
// The MESSAGE is asserted unchanged alongside: the message tone is part of the
// contract, so this fix adds a detail and must not reword anything.
func TestErrInvalidArg_CarriesFieldViolationAndKeepsMessage(t *testing.T) {
	const field = "targets[0].status"
	const msg = "status is output-only; it is set by RemoveTargets and the drain runner"

	err := ErrInvalidArg(field, msg)

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("not a gRPC status: %v", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", st.Code())
	}
	if want := field + ": " + msg; st.Message() != want {
		t.Fatalf("message changed.\n got: %q\nwant: %q\n"+
			"the message tone is contract; this change adds a detail, it does not reword", st.Message(), want)
	}

	var got []string
	for _, d := range st.Details() {
		br, isBR := d.(*errdetails.BadRequest)
		if !isBR {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			got = append(got, v.GetField())
			if v.GetDescription() != msg {
				t.Fatalf("violation description = %q, want %q", v.GetDescription(), msg)
			}
		}
	}
	if len(got) != 1 || got[0] != field {
		t.Fatalf("field violations = %v, want exactly [%s]\n"+
			"a refusal that names the field only in prose forces the caller to parse the message",
			got, field)
	}
}

// Control in the other direction: the detail must name the field that was actually
// passed, not a constant. Without this a fix that always attached the same violation
// would pass the test above.
func TestErrInvalidArg_ViolationNamesTheFieldGiven(t *testing.T) {
	err := ErrInvalidArg("v4Source.subnetId", "required")
	st, _ := status.FromError(err)
	for _, d := range st.Details() {
		if br, ok := d.(*errdetails.BadRequest); ok {
			for _, v := range br.GetFieldViolations() {
				if v.GetField() != "v4Source.subnetId" {
					t.Fatalf("violation names %q, want v4Source.subnetId", v.GetField())
				}
				return
			}
		}
	}
	t.Fatal("no BadRequest detail attached")
}
