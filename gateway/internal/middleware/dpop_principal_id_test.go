// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dpop_principal_id_test.go — the DPoP header-injection path must derive the
// principal with the SAME rule as the legacy auth.HTTP Hydra path
// (principalFromVerifiedToken): the kacho_principal_* claims, and nothing else.
// Otherwise DPoP.Wrap (inner handler) overwrites the principal headers auth.HTTP
// set, and the downstream FGA subject becomes user:<oidc-sub> instead of
// user:<kacho-id> — a lockout / inconsistent-subject bug (CWE-287 / OWASP A07).
//
// "The same rule" cuts both ways, and the second half is what this file used to
// get wrong: a token that carries no identifier does not get one invented from
// `sub`. principalFromVerifiedToken refuses that input; so does this path now.
package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newDPoPProbeMiddleware — слой в наименьшей сборке, достаточной для инъекции
// заголовков. Метод, а не свободная функция, потому что доклад о непереданном
// способе подтверждения идёт в окно ЭТОГО слоя; предмет проб ниже от этого не
// меняется.
func newDPoPProbeMiddleware(t *testing.T) *DPoPMiddleware {
	t.Helper()
	return &DPoPMiddleware{
		logger:              slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		authMethodsUnusable: newIntrospectionFailureReporter(0, nil),
	}
}

func TestInjectVerifiedTokenHeaders_PrefersKachoPrincipalIDOverSub(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/iam/v1/users/x", nil)
	vt := &VerifiedToken{
		Subject: "zitadel-uuid-999", // raw OIDC sub — must NOT win
		ACR:     "1",
		ExtClaims: map[string]any{
			"kacho_principal_type": "user",
			"kacho_principal_id":   "usr_abc123", // canonical kacho id — must win
		},
	}
	newDPoPProbeMiddleware(t).injectVerifiedTokenHeaders(r, vt)

	if got := r.Header.Get("X-Kacho-Principal-Id"); got != "usr_abc123" {
		t.Fatalf("X-Kacho-Principal-Id = %q, want kacho id usr_abc123 (not raw sub)", got)
	}
	if got := r.Header.Get("Grpc-Metadata-X-Kacho-Principal-Id"); got != "usr_abc123" {
		t.Fatalf("Grpc-Metadata-X-Kacho-Principal-Id = %q, want usr_abc123", got)
	}
	if got := r.Header.Get("X-Kacho-Principal-Type"); got != "user" {
		t.Fatalf("X-Kacho-Principal-Type = %q, want user", got)
	}
}

// TestInjectVerifiedTokenHeaders_TopLevelClaimWins — kacho_principal_id promoted
// to the top-level claim set (Hydra allowed_top_level_claims) is also honored.
func TestInjectVerifiedTokenHeaders_TopLevelClaimWins(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/iam/v1/users/x", nil)
	vt := &VerifiedToken{
		Subject: "sub-raw",
		Claims: map[string]any{
			"kacho_principal_type": "service_account",
			"kacho_principal_id":   "sa_xyz",
		},
	}
	newDPoPProbeMiddleware(t).injectVerifiedTokenHeaders(r, vt)
	if got := r.Header.Get("X-Kacho-Principal-Id"); got != "sa_xyz" {
		t.Fatalf("X-Kacho-Principal-Id = %q, want sa_xyz", got)
	}
	if got := r.Header.Get("X-Kacho-Principal-Type"); got != "service_account" {
		t.Fatalf("X-Kacho-Principal-Type = %q, want service_account", got)
	}
}

// TestInjectVerifiedTokenHeaders_NoClaims_NamesNobody — a token carrying only
// the provider's `sub` names no principal of this platform, and this path does
// not invent one. It used to: the assertion here was `sub-only`, described as
// "backward compatible", and it is the whole reason a claim set that withholds
// the identifier on purpose could still arrive as a caller.
//
// Nothing is lost by refusing: resolving an unclaimed `sub` is the job of the
// authN layer ahead of this one, which does a real lookup instead of a
// substitution, and whose headers this path must therefore leave alone.
func TestInjectVerifiedTokenHeaders_NoClaims_NamesNobody(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/iam/v1/users/x", nil)
	vt := &VerifiedToken{Subject: "sub-only", ACR: "2"}
	newDPoPProbeMiddleware(t).injectVerifiedTokenHeaders(r, vt)
	if got := r.Header.Get("X-Kacho-Principal-Id"); got != "" {
		t.Fatalf("X-Kacho-Principal-Id = %q, want no principal at all", got)
	}
	if got := r.Header.Get("Grpc-Metadata-X-Kacho-Principal-Id"); got != "" {
		t.Fatalf("Grpc-Metadata-X-Kacho-Principal-Id = %q, want no principal at all", got)
	}
	if got := r.Header.Get("X-Kacho-Token-Acr"); got != "2" {
		t.Fatalf("X-Kacho-Token-Acr = %q, want the token's own context to survive", got)
	}
}

// TestInjectVerifiedTokenHeaders_TypeWithoutID_NamesNobody — the reduced claim
// set exactly: it says WHAT kind of principal the token was minted for and
// deliberately withholds WHICH one. A type without an identifier is not a
// principal, and the type alone must not be forwarded either — a downstream
// reader that sees a principal type reasonably assumes there is a principal.
func TestInjectVerifiedTokenHeaders_TypeWithoutID_NamesNobody(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/iam/v1/users/x", nil)
	vt := &VerifiedToken{
		Subject: "provider-subject-7f3a",
		ACR:     "2",
		ExtClaims: map[string]any{
			"kacho_principal_type": "user",
		},
	}
	newDPoPProbeMiddleware(t).injectVerifiedTokenHeaders(r, vt)
	if got := r.Header.Get("X-Kacho-Principal-Id"); got != "" {
		t.Fatalf("X-Kacho-Principal-Id = %q, want no principal (the token names none)", got)
	}
	if got := r.Header.Get("X-Kacho-Principal-Type"); got != "" {
		t.Fatalf("X-Kacho-Principal-Type = %q, want no principal type without a principal", got)
	}
}
