// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import (
	"encoding/base64"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestValidateListPagination locks the List pagination guard that runs BEFORE the
// listauthz empty-grant short-circuit — a malformed page_token / out-of-range
// page_size must be InvalidArgument regardless of grant state (the bug: compute List
// returned 200 {[]} for a garbage token / page_size>1000 when the caller's grant was
// empty, diverging from vpc + the api-convention).
func TestValidateListPagination(t *testing.T) {
	validToken := base64.RawURLEncoding.EncodeToString([]byte("1700000000000000000:epd0000000000000000"))
	cases := []struct {
		name    string
		p       Pagination
		wantErr bool
	}{
		{"empty token + default size", Pagination{PageToken: "", PageSize: 0}, false},
		{"size within range", Pagination{PageSize: 1000}, false},
		{"valid token", Pagination{PageToken: validToken, PageSize: 10}, false},
		{"size over max", Pagination{PageSize: 1001}, true},
		{"negative size", Pagination{PageSize: -1}, true},
		{"garbage token (not base64)", Pagination{PageToken: "not-a-real-token!!"}, true},
		{"base64 but no colon separator", Pagination{PageToken: base64.RawURLEncoding.EncodeToString([]byte("nocolonhere"))}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateListPagination(tc.p)
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
