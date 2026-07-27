// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coreerrors "github.com/PRO-Robotech/kacho/pkg/errors"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// TestMapDomainErrFailureBandsCharacterization records the code AND the exact
// wire text MapDomainErr produces on every failure band, for a bare sentinel and
// for a sentinel carrying the caller's contract text.
//
// nlb is the reference order (pass-through first, guarded on a meaningful code),
// so this table doubles as the target shape the other services are moved to.
func TestMapDomainErrFailureBandsCharacterization(t *testing.T) {
	cases := []struct {
		name     string
		in       error
		wantCode codes.Code
		wantMsg  string
	}{
		{"not_found/bare", domain.ErrNotFound, codes.NotFound, "not found"},
		{"not_found/wrapped", fmt.Errorf("%w: LoadBalancer lb-1 not found", domain.ErrNotFound), codes.NotFound, "LoadBalancer lb-1 not found"},

		{"already_exists/bare", domain.ErrAlreadyExists, codes.AlreadyExists, "already exists"},
		{"already_exists/wrapped", fmt.Errorf("%w: LoadBalancer lb-1 already exists", domain.ErrAlreadyExists), codes.AlreadyExists, "LoadBalancer lb-1 already exists"},

		{"failed_precondition/bare", domain.ErrFailedPrecondition, codes.FailedPrecondition, "failed precondition"},
		{"failed_precondition/wrapped", fmt.Errorf("%w: load balancer is not empty", domain.ErrFailedPrecondition), codes.FailedPrecondition, "load balancer is not empty"},

		{"invalid_argument/bare", domain.ErrInvalidArg, codes.InvalidArgument, "invalid argument"},
		{"invalid_argument/wrapped", fmt.Errorf("%w: invalid load balancer id 'zzz'", domain.ErrInvalidArg), codes.InvalidArgument, "invalid load balancer id 'zzz'"},

		{"unavailable/bare", domain.ErrUnavailable, codes.Unavailable, "service unavailable"},
		{"unavailable/wrapped", fmt.Errorf("%w: vpc peer unreachable", domain.ErrUnavailable), codes.Unavailable, "vpc peer unreachable"},

		{"internal/bare", domain.ErrInternal, codes.Internal, "internal database error"},
		{"internal/wrapped", fmt.Errorf("%w: pgx: dial tcp 10.0.0.7:5432", domain.ErrInternal), codes.Internal, "internal database error"},

		{"passthrough/permission_denied", status.Error(codes.PermissionDenied, "denied"), codes.PermissionDenied, "denied"},

		{"unclassified/raw", errors.New("dial tcp 10.0.0.7:5432: connect: connection refused"), codes.Internal, "internal error"},
		{"unclassified/unknown_coded_status", status.Error(codes.Unknown, "boom"), codes.Internal, "internal error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MapDomainErr(tc.in)
			st, ok := status.FromError(got)
			if !ok {
				t.Fatalf("MapDomainErr returned a non-status error: %v", got)
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
		if got := MapDomainErr(nil); got != nil {
			t.Errorf("MapDomainErr(nil) = %v, want nil", got)
		}
	})
}

// TestMapDomainErrPreservesDetailsThroughSentinelWrap is the control for the
// property the other services are being brought up to: nlb already checks the
// pass-through first, so a rich validator error wrapped onto a sentinel keeps its
// google.rpc.BadRequest field violation. This test is expected to be green before
// and after the reorder elsewhere — it is what "fixed" looks like.
func TestMapDomainErrPreservesDetailsThroughSentinelWrap(t *testing.T) {
	rich := coreerrors.InvalidArgument().
		AddFieldViolation("port", "port must be in [1..65535]").
		Err()
	wrapped := fmt.Errorf("%w: %w", domain.ErrInvalidArg, rich)

	got := MapDomainErr(wrapped)

	st, ok := status.FromError(got)
	if !ok {
		t.Fatalf("MapDomainErr returned a non-status error: %v", got)
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
	if len(fields) != 1 || fields[0] != "port" {
		t.Fatalf("field violations = %v, want exactly [port]", fields)
	}
}
