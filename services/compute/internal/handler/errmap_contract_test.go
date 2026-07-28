// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coreerrors "github.com/PRO-Robotech/kacho/pkg/errors"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/serviceerr"
)

// TestInternalMapErrFailureBandsCharacterization records the code AND the exact
// wire text internalMapErr produces on every failure band. Its message policy is
// deliberately stricter than the public mapper's: the wrapped tail is dropped and
// only the bare sentinel text reaches the cluster-internal listener.
//
// The tone is still contract, and the internal listener is not exempt from the
// no-leak rule, so this test exists to make a reorder prove it moved nothing.
func TestInternalMapErrFailureBandsCharacterization(t *testing.T) {
	const tag = "internal disk error"

	cases := []struct {
		name     string
		in       error
		wantCode codes.Code
		wantMsg  string
	}{
		{"not_found/bare", serviceerr.ErrNotFound, codes.NotFound, "not found"},
		{"not_found/wrapped", fmt.Errorf("%w: Disk dsk-1 not found", serviceerr.ErrNotFound), codes.NotFound, "not found"},

		{"already_exists/bare", serviceerr.ErrAlreadyExists, codes.AlreadyExists, "already exists"},
		{"already_exists/wrapped", fmt.Errorf("%w: Disk dsk-1 already exists", serviceerr.ErrAlreadyExists), codes.AlreadyExists, "already exists"},

		{"failed_precondition/bare", serviceerr.ErrFailedPrecondition, codes.FailedPrecondition, "failed precondition"},
		{"failed_precondition/wrapped", fmt.Errorf("%w: disk is attached", serviceerr.ErrFailedPrecondition), codes.FailedPrecondition, "failed precondition"},

		{"invalid_argument/bare", serviceerr.ErrInvalidArg, codes.InvalidArgument, "invalid argument"},
		{"invalid_argument/wrapped", fmt.Errorf("%w: invalid disk id 'zzz'", serviceerr.ErrInvalidArg), codes.InvalidArgument, "invalid argument"},

		{"internal/sentinel", serviceerr.ErrInternal, codes.Internal, tag},
		{"internal/wrapped", fmt.Errorf("%w: pgx: dial tcp 10.0.0.7:5432", serviceerr.ErrInternal), codes.Internal, tag},

		{"unavailable/status", status.Error(codes.Unavailable, "geo unavailable"), codes.Unavailable, "geo unavailable"},
		{"passthrough/permission_denied", status.Error(codes.PermissionDenied, "denied"), codes.PermissionDenied, "denied"},

		{"unclassified/raw", errors.New("dial tcp 10.0.0.7:5432: connect: connection refused"), codes.Internal, tag},
		{"unclassified/unknown_coded_status", status.Error(codes.Unknown, "boom"), codes.Internal, tag},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := internalMapErr(tag, tc.in)
			st, ok := status.FromError(got)
			if !ok {
				t.Fatalf("internalMapErr returned a non-status error: %v", got)
			}
			if st.Code() != tc.wantCode {
				t.Errorf("code = %v, want %v", st.Code(), tc.wantCode)
			}
			if st.Message() != tc.wantMsg {
				t.Errorf("message = %q, want %q", st.Message(), tc.wantMsg)
			}
		})
	}

	t.Run("nil", func(t *testing.T) {
		if got := internalMapErr(tag, nil); got != nil {
			t.Errorf("internalMapErr(nil) = %v, want nil", got)
		}
	})

	t.Run("empty_tag_falls_back", func(t *testing.T) {
		got := internalMapErr("", errors.New("raw"))
		if status.Code(got) != codes.Internal || status.Convert(got).Message() != "internal error" {
			t.Errorf("got %v, want Internal \"internal error\"", got)
		}
	})
}

// TestInternalMapErrPreservesDetailsThroughSentinelWrap locks the property
// itself: a rich validator error wrapped onto a service sentinel with %w must
// reach the client with its google.rpc.BadRequest field violation intact.
//
// pkg/validate puts the offending field name ONLY in the details — the message
// stays the generic "invalid argument" — so a mapper that recognises the sentinel
// and rebuilds a fresh status.Error(code, text) silently drops the one
// machine-readable part of the answer. That has to be a property of the mapper,
// not something every future author is expected to remember.
func TestInternalMapErrPreservesDetailsThroughSentinelWrap(t *testing.T) {
	rich := coreerrors.InvalidArgument().
		AddFieldViolation("sizeBytes", "sizeBytes must be positive").
		Err()
	wrapped := fmt.Errorf("%w: %w", serviceerr.ErrInvalidArg, rich)

	got := internalMapErr("internal disk error", wrapped)

	st, ok := status.FromError(got)
	if !ok {
		t.Fatalf("internalMapErr returned a non-status error: %v", got)
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
	if len(fields) != 1 || fields[0] != "sizeBytes" {
		t.Fatalf("field violations = %v, want exactly [sizeBytes]", fields)
	}
}
