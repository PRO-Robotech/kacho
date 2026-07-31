// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package helpers

import (
	"encoding/base64"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestDecodePageToken locks the CODEC: a malformed token must error so the List RPC
// maps it via InvalidPageTokenErr to 400, and a valid round-trip must decode back to
// the same (created_at truncated to ns, id).
//
// What it does NOT lock — and never did, despite the wording it used to carry about
// vpc being the reference for the convention — is WHEN the check runs relative to the
// caller's identity. That ordering is the substance of the convention, and this case
// has no caller in it at all. It is locked in two other places instead:
// list_pagination_order_test.go in each List use-case package (behaviour: the same
// garbage cursor is refused whether or not the caller was identified), and
// internal/repohygiene TestEmptyPageNeverPrecedesPaginationValidation (the tree-wide
// property, so the next such place reddens when it is written).
func TestDecodePageToken(t *testing.T) {
	t.Run("round-trip", func(t *testing.T) {
		want := time.Unix(0, 1_700_000_000_123_456_789).UTC()
		tok := EncodePageToken(want, "net0000000000000000")
		gotT, gotID, err := DecodePageToken(tok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !gotT.Equal(want) || gotID != "net0000000000000000" {
			t.Fatalf("round-trip mismatch: got (%v,%q)", gotT, gotID)
		}
	})
	malformed := map[string]string{
		"not base64":        "not-a-real-token!!",
		"base64 no colon":   base64.RawURLEncoding.EncodeToString([]byte("nocolon")),
		"non-numeric nanos": base64.RawURLEncoding.EncodeToString([]byte("notanumber:net0")),
	}
	for name, tok := range malformed {
		t.Run(name, func(t *testing.T) {
			if _, _, err := DecodePageToken(tok); err == nil {
				t.Fatalf("expected decode error for %q", tok)
			}
		})
	}
	// InvalidPageTokenErr must map any decode error to gRPC InvalidArgument (no raw leak).
	_, _, derr := DecodePageToken("not-a-real-token!!")
	if got := status.Code(InvalidPageTokenErr(derr)); got != codes.InvalidArgument {
		t.Fatalf("InvalidPageTokenErr: expected InvalidArgument, got %v", got)
	}
}
