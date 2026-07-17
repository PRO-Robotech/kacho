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

// TestDecodePageToken locks the page_token contract that keeps vpc the reference for
// the "garbage token → InvalidArgument" convention (compute+nlb were fixed to match it):
// a malformed token must error so the List RPC maps it via InvalidPageTokenErr to 400,
// and a valid round-trip must decode back to the same (created_at truncated to ns, id).
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
