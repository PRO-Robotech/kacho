// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"encoding/base64"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestValidatePagination locks the List pagination guard that runs BEFORE the listauthz
// empty-grant short-circuit — a malformed page_token / out-of-range page_size must be
// InvalidArgument regardless of grant state (the systemic bug shared with compute).
func TestValidatePagination(t *testing.T) {
	validToken := base64.RawURLEncoding.EncodeToString([]byte("2026-07-17T00:00:00Z\x00nlb0000000000000000"))
	cases := []struct {
		name      string
		pageToken string
		pageSize  int64
		wantErr   bool
	}{
		{"empty token + default size", "", 0, false},
		{"size within range", "", 1000, false},
		{"valid token", validToken, 10, false},
		{"size over max", "", 1001, true},
		{"negative size", "", -1, true},
		{"garbage token (not base64)", "not-a-real-token!!", 0, true},
		{"base64 but no NUL separator", base64.RawURLEncoding.EncodeToString([]byte("noseparator")), 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePagination(tc.pageToken, tc.pageSize)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if got := status.Code(err); got != codes.InvalidArgument {
					t.Fatalf("expected InvalidArgument, got %v", got)
				}
			} else if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
