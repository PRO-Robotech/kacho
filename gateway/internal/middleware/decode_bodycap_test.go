// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestKratosWhoamiDecodeBodyBounded locks the audit finding: the Kratos
// /sessions/whoami JSON body must be decoded through a bounded io.LimitReader
// (matching the sibling readers in introspection_cache.go / jwt_verifier.go), so
// a compromised or MITM'd Kratos peer on the plaintext cluster-internal hop
// cannot force json.Decode to materialise a multi-MB scalar in the heap on the
// hot per-request SPA auth path. A response body whose single JSON string value
// exceeds the 1 MiB cap must fail to decode → fail-closed (Active=false), never
// be read in full.
func TestKratosWhoamiDecodeBodyBounded(t *testing.T) {
	const cookie = "ory_kratos_session=abc"
	// email value alone is 2 MiB — a fully-read body would parse to Active=true;
	// a 1 MiB-capped reader truncates the JSON mid-string → decode error.
	huge := strings.Repeat("a", 2<<20)
	body := `{"active":true,"identity":{"id":"id-x","traits":{"email":"` + huge + `"}}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	c := NewKratosClient(srv.URL)
	res := c.Whoami(context.Background(), cookie)

	if res.Active {
		t.Fatalf("oversized whoami body decoded to Active=true (body not bounded); want fail-closed Active=false")
	}
	if res.Email != "" {
		t.Fatalf("oversized whoami body leaked Email of %d bytes; want empty (fail-closed)", len(res.Email))
	}
}
