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
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
)

// TestMapRepoErrFailureBandsCharacterization records the code AND the exact wire
// text MapRepoErr produces on every failure band, for a bare sentinel and for a
// sentinel carrying the caller's contract text.
//
// The message tone is part of the Kachō contract ("<Resource> %s not found" and
// friends), and hide-existence needs a deny to read byte-for-byte like a real
// miss, so drift here is an existence oracle rather than a cosmetic change. This
// test exists so that reordering the mapper has to prove it moved nothing.
func TestMapRepoErrFailureBandsCharacterization(t *testing.T) {
	cases := []struct {
		name     string
		in       error
		wantCode codes.Code
		wantMsg  string
	}{
		{"not_found/bare", serviceerr.ErrNotFound, codes.NotFound, "not found"},
		{"not_found/wrapped", fmt.Errorf("%w: Subnet sub-1 not found", serviceerr.ErrNotFound), codes.NotFound, "Subnet sub-1 not found"},

		{"already_exists/bare", serviceerr.ErrAlreadyExists, codes.AlreadyExists, "already exists"},
		{"already_exists/wrapped", fmt.Errorf("%w: Subnet sub-1 already exists", serviceerr.ErrAlreadyExists), codes.AlreadyExists, "Subnet sub-1 already exists"},

		{"failed_precondition/bare", serviceerr.ErrFailedPrecondition, codes.FailedPrecondition, "failed precondition"},
		{"failed_precondition/wrapped", fmt.Errorf("%w: network is not empty", serviceerr.ErrFailedPrecondition), codes.FailedPrecondition, "network is not empty"},

		{"failed_precondition/pool_not_resolved/bare", serviceerr.ErrPoolNotResolved, codes.FailedPrecondition, "no address pool resolved"},
		{"failed_precondition/pool_not_resolved/wrapped", fmt.Errorf("%w: zone zone-1", serviceerr.ErrPoolNotResolved), codes.FailedPrecondition, "zone zone-1"},

		{"invalid_argument/bare", serviceerr.ErrInvalidArg, codes.InvalidArgument, "invalid argument"},
		{"invalid_argument/wrapped", fmt.Errorf("%w: invalid subnet id 'zzz'", serviceerr.ErrInvalidArg), codes.InvalidArgument, "invalid subnet id 'zzz'"},

		{"aborted/bare", serviceerr.ErrConflict, codes.Aborted, "conflicting concurrent update"},
		{"aborted/wrapped", fmt.Errorf("%w: SQLSTATE 40001 on subnets", serviceerr.ErrConflict), codes.Aborted, "conflicting concurrent update"},

		{"internal/bare", serviceerr.ErrInternal, codes.Internal, "internal database error"},
		{"internal/wrapped", fmt.Errorf("%w: pgx: dial tcp 10.0.0.7:5432", serviceerr.ErrInternal), codes.Internal, "internal database error"},

		{"unavailable/status", status.Error(codes.Unavailable, "geo unavailable"), codes.Unavailable, "geo unavailable"},
		{"passthrough/permission_denied", status.Error(codes.PermissionDenied, "denied"), codes.PermissionDenied, "denied"},

		{"unclassified/raw", errors.New("dial tcp 10.0.0.7:5432: connect: connection refused"), codes.Internal, "internal database error"},
		{"unclassified/unknown_coded_status", status.Error(codes.Unknown, "boom"), codes.Internal, "internal database error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serviceerr.MapRepoErr(tc.in)
			st, ok := status.FromError(got)
			if !ok {
				t.Fatalf("MapRepoErr returned a non-status error: %v", got)
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
		if got := serviceerr.MapRepoErr(nil); got != nil {
			t.Errorf("MapRepoErr(nil) = %v, want nil", got)
		}
	})
}

// TestMapRepoErrLeakSafeFailureBandsCharacterization does the same for the
// internal-listener variant, whose message policy is deliberately stricter: the
// wrapped tail is dropped and only the bare sentinel text goes on the wire.
func TestMapRepoErrLeakSafeFailureBandsCharacterization(t *testing.T) {
	const tag = "address pool admin error"

	cases := []struct {
		name     string
		in       error
		wantCode codes.Code
		wantMsg  string
	}{
		{"not_found/bare", serviceerr.ErrNotFound, codes.NotFound, "not found"},
		{"not_found/wrapped", fmt.Errorf("%w: AddressPool ap-1 not found", serviceerr.ErrNotFound), codes.NotFound, "not found"},

		{"already_exists/bare", serviceerr.ErrAlreadyExists, codes.AlreadyExists, "already exists"},
		{"already_exists/wrapped", fmt.Errorf("%w: AddressPool ap-1 already exists", serviceerr.ErrAlreadyExists), codes.AlreadyExists, "already exists"},

		{"failed_precondition/bare", serviceerr.ErrFailedPrecondition, codes.FailedPrecondition, "failed precondition"},
		{"failed_precondition/wrapped", fmt.Errorf("%w: pool is not empty", serviceerr.ErrFailedPrecondition), codes.FailedPrecondition, "failed precondition"},

		{"failed_precondition/pool_not_resolved", serviceerr.ErrPoolNotResolved, codes.FailedPrecondition, "no address pool resolved"},

		{"invalid_argument/bare", serviceerr.ErrInvalidArg, codes.InvalidArgument, "invalid argument"},
		{"invalid_argument/wrapped", fmt.Errorf("%w: invalid pool id 'zzz'", serviceerr.ErrInvalidArg), codes.InvalidArgument, "invalid argument"},

		{"aborted", serviceerr.ErrConflict, codes.Aborted, "conflicting concurrent update"},

		{"internal/bare", serviceerr.ErrInternal, codes.Internal, "internal database error"},
		{"internal/wrapped", fmt.Errorf("%w: pgx: dial tcp 10.0.0.7:5432", serviceerr.ErrInternal), codes.Internal, "internal database error"},

		{"unavailable/status", status.Error(codes.Unavailable, "geo unavailable"), codes.Unavailable, "geo unavailable"},
		{"passthrough/permission_denied", status.Error(codes.PermissionDenied, "denied"), codes.PermissionDenied, "denied"},

		{"unclassified/raw", errors.New("dial tcp 10.0.0.7:5432: connect: connection refused"), codes.Internal, tag},
		{"unclassified/unknown_coded_status", status.Error(codes.Unknown, "boom"), codes.Internal, tag},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serviceerr.MapRepoErrLeakSafe(tc.in, tag)
			st, ok := status.FromError(got)
			if !ok {
				t.Fatalf("MapRepoErrLeakSafe returned a non-status error: %v", got)
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
		if got := serviceerr.MapRepoErrLeakSafe(nil, tag); got != nil {
			t.Errorf("MapRepoErrLeakSafe(nil) = %v, want nil", got)
		}
	})

	t.Run("empty_tag_falls_back", func(t *testing.T) {
		got := serviceerr.MapRepoErrLeakSafe(errors.New("raw"), "")
		if status.Code(got) != codes.Internal || status.Convert(got).Message() != "internal database error" {
			t.Errorf("got %v, want Internal \"internal database error\"", got)
		}
	})
}

// TestMapRepoErrPreservesDetailsThroughSentinelWrap locks the property itself:
// a rich validator error wrapped onto a service sentinel with %w must reach the
// client with its google.rpc.BadRequest field violation intact.
//
// pkg/validate puts the offending field name ONLY in the details — the message
// stays the generic "invalid argument" — so a mapper that recognises the sentinel
// and rebuilds a fresh status.Error(code, text) silently drops the one
// machine-readable part of the answer. That has to be a property of the mapper,
// not something every future author is expected to remember.
func TestMapRepoErrPreservesDetailsThroughSentinelWrap(t *testing.T) {
	for _, m := range []struct {
		name string
		fn   func(error) error
	}{
		{"MapRepoErr", serviceerr.MapRepoErr},
		{"MapRepoErrLeakSafe", func(err error) error { return serviceerr.MapRepoErrLeakSafe(err, "internal error") }},
	} {
		t.Run(m.name, func(t *testing.T) {
			rich := coreerrors.InvalidArgument().
				AddFieldViolation("cidrBlock", "cidrBlock must be a valid CIDR").
				Err()
			wrapped := fmt.Errorf("%w: %w", serviceerr.ErrInvalidArg, rich)

			got := m.fn(wrapped)

			st, ok := status.FromError(got)
			if !ok {
				t.Fatalf("%s returned a non-status error: %v", m.name, got)
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
			if len(fields) != 1 || fields[0] != "cidrBlock" {
				t.Fatalf("field violations = %v, want exactly [cidrBlock]", fields)
			}
		})
	}
}
