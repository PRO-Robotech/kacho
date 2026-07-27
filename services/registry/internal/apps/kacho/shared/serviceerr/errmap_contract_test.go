// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr_test

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coreerrors "github.com/PRO-Robotech/kacho/pkg/errors"
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/shared/serviceerr"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// TestToStatusFailureBandsCharacterization records the code AND the exact wire
// text ToStatus produces on every failure band, for a bare sentinel and for a
// sentinel carrying the caller's contract text.
//
// The message tone is part of the Kachō contract ("<Resource> %s not found" and
// friends), and hide-existence needs a deny to read byte-for-byte like a real
// miss, so drift here is an existence oracle rather than a cosmetic change. This
// test exists so that reordering the mapper has to prove it moved nothing.
func TestToStatusFailureBandsCharacterization(t *testing.T) {
	cases := []struct {
		name     string
		in       error
		wantCode codes.Code
		wantMsg  string
	}{
		{"not_found/bare", regerrors.ErrNotFound, codes.NotFound, "not found"},
		{"not_found/wrapped", fmt.Errorf("%w: Repository rep-1 not found", regerrors.ErrNotFound), codes.NotFound, "Repository rep-1 not found"},

		{"already_exists/bare", regerrors.ErrAlreadyExists, codes.AlreadyExists, "already exists"},
		{"already_exists/wrapped", fmt.Errorf("%w: Repository rep-1 already exists", regerrors.ErrAlreadyExists), codes.AlreadyExists, "Repository rep-1 already exists"},

		{"failed_precondition/bare", regerrors.ErrFailedPrecondition, codes.FailedPrecondition, "failed precondition"},
		{"failed_precondition/wrapped", fmt.Errorf("%w: registry is not empty", regerrors.ErrFailedPrecondition), codes.FailedPrecondition, "registry is not empty"},

		{"invalid_argument/bare", regerrors.ErrInvalidArg, codes.InvalidArgument, "invalid argument"},
		{"invalid_argument/wrapped", fmt.Errorf("%w: invalid registry id 'zzz'", regerrors.ErrInvalidArg), codes.InvalidArgument, "invalid registry id 'zzz'"},

		{"unavailable/bare", regerrors.ErrUnavailable, codes.Unavailable, "unavailable"},
		{"unavailable/wrapped", fmt.Errorf("%w: geo peer unreachable", regerrors.ErrUnavailable), codes.Unavailable, "geo peer unreachable"},

		{"internal/bare", regerrors.ErrInternal, codes.Internal, "internal database error"},
		{"internal/wrapped", fmt.Errorf("%w: pgx: dial tcp 10.0.0.7:5432", regerrors.ErrInternal), codes.Internal, "internal database error"},

		{"passthrough/permission_denied", status.Error(codes.PermissionDenied, "denied"), codes.PermissionDenied, "denied"},

		{"unclassified/raw", errors.New("dial tcp 10.0.0.7:5432: connect: connection refused"), codes.Internal, "internal database error"},
		{"unclassified/unknown_coded_status", status.Error(codes.Unknown, "boom"), codes.Internal, "internal database error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serviceerr.ToStatus(tc.in)
			st, ok := status.FromError(got)
			if !ok {
				t.Fatalf("ToStatus returned a non-status error: %v", got)
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
		if got := serviceerr.ToStatus(nil); got != nil {
			t.Errorf("ToStatus(nil) = %v, want nil", got)
		}
	})
}

// TestToStatusPreservesDetailsThroughSentinelWrap locks the property itself:
// a rich validator error wrapped onto a service sentinel with %w must reach the
// client with its google.rpc.BadRequest field violation intact.
//
// pkg/validate puts the offending field name ONLY in the details — the message
// stays the generic "invalid argument" — so a mapper that recognises the sentinel
// and rebuilds a fresh status.Error(code, text) silently drops the one
// machine-readable part of the answer. That has to be a property of the mapper,
// not something every future author is expected to remember.
func TestToStatusPreservesDetailsThroughSentinelWrap(t *testing.T) {
	rich := coreerrors.InvalidArgument().
		AddFieldViolation("repositoryName", "repositoryName must match ^[a-z0-9]+$").
		Err()
	wrapped := fmt.Errorf("%w: %w", regerrors.ErrInvalidArg, rich)

	got := serviceerr.ToStatus(wrapped)

	st, ok := status.FromError(got)
	if !ok {
		t.Fatalf("ToStatus returned a non-status error: %v", got)
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
	if len(fields) != 1 || fields[0] != "repositoryName" {
		t.Fatalf("field violations = %v, want exactly [repositoryName]", fields)
	}
}
