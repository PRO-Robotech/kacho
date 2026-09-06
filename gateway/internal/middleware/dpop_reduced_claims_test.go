// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// dpop_reduced_claims_test.go — a token that names no principal must not
// become one.
//
// The reduced claim set is reduced ON PURPOSE: it says what KIND of principal a
// token was minted for and deliberately withholds WHICH principal, and that
// omission is the whole reason it authorizes nothing. The proof-of-possession
// path used to answer the omission by supplying the raw OIDC `sub` in its place.
// A supplied identifier is indistinguishable downstream from a claimed one: it
// lands in the principal headers, is read back as `ext_claims.kacho_principal_*`,
// and matches the FIRST subject rule — the one reserved for a token that stated
// its principal outright. So a request that named nobody arrived as somebody, on
// a surface every authenticated caller can reach.
//
// The locks below are on what the CALLER sees, not on what a function returned:
// the request is refused and the backend never runs. The control case — the same
// token with the identifier present — is served, so the refusal is demonstrably
// about the missing identifier and not about a path that stopped working.

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// exemptRoute — a real route whose catalog entry is `<exempt>`: authorization
// asks nothing, so ANY authenticated caller is served. Exemption from the
// authorization question is not exemption from being someone, which makes this
// the surface a fabricated subject reaches first — and therefore the one worth
// pinning.
const exemptRoute = "https://api.kacho.cloud/geo/v1/zones"

// reducedClaims — the shape the platform actually mints for this path: a
// principal TYPE and no principal id. `sub` carries the provider's own subject,
// which is not this platform's identifier for anyone.
func reducedClaims() jwt.MapClaims {
	c := standardClaims()
	c["sub"] = "provider-subject-7f3a"
	c["ext_claims"] = map[string]any{
		"kacho_external_id":    "krt_id_xxx",
		"kacho_active_account": "acc_a1b2",
		"kacho_principal_type": "user",
	}
	return c
}

// namedClaims — the same token, saying who it is.
func namedClaims() jwt.MapClaims {
	c := reducedClaims()
	ext, _ := c["ext_claims"].(map[string]any)
	ext["kacho_principal_id"] = "usr_alice_acc_a1b2"
	return c
}

// buildProductionAuthNChain wires the two layers in the order the composition
// root mounts them: proof-of-possession establishes the principal, then
// authorization reads it. The backend echoes what reached it.
func buildProductionAuthNChain(t *testing.T, fix *jwksFixture, checker *fakeChecker) (http.Handler, *bool) {
	t.Helper()

	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	replay := middleware.NewDPoPReplayCache(middleware.DPoPReplayCacheConfig{
		MaxEntries: 64, TTL: 60 * time.Second,
	})
	dpopValidator, err := middleware.NewDPoPValidator(middleware.DPoPValidatorConfig{
		ReplayCache: replay, IatFreshness: 60 * time.Second,
	})
	require.NoError(t, err)

	dpopMW, err := middleware.NewDPoPMiddleware(middleware.DPoPMiddlewareConfig{
		Verifier: stepUpVerifier(t, fix),
		DPoP:     dpopValidator,
		StepUp:   middleware.NewStepUpGate(time.Now),

		PermissionLookup: middleware.NewCatalogPermissionLookup(catalog),
		RestRouter:       middleware.NewRestRouter(),
		Logger:           slog.New(slog.DiscardHandler),
		APIDomain:        "api.kacho.cloud",
	})
	require.NoError(t, err)

	authzMW := buildAuthzMiddleware(t, catalog, checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = middleware.NewRestRouter()
	})

	reached := false
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"principal_id":   r.Header.Get("X-Kacho-Principal-Id"),
			"principal_type": r.Header.Get("X-Kacho-Principal-Type"),
			"acr":            r.Header.Get("X-Kacho-Token-Acr"),
		})
	})
	return dpopMW.Wrap(authzMW.HTTP(backend)), &reached
}

func getWithToken(t *testing.T, h http.Handler, url, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The debt. A verified token carrying a principal TYPE and no principal
// IDENTIFIER must not be served as a principal — not even on the surface that
// asks nothing of whoever is asking, because "asks nothing of you" still
// requires there to be a you.
func TestReducedClaims_VerifiedButUnnamed_IsNotServedAsASubject(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")
	checker := &fakeChecker{allowed: true}
	handler, reached := buildProductionAuthNChain(t, fix, checker)

	rec := getWithToken(t, handler, exemptRoute, fix.sign(t, reducedClaims()))

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"a token that names no principal was served (%d); the provider's own subject is not "+
			"this platform's name for anyone, and supplying it invents a caller", rec.Code)
	assert.False(t, *reached, "the backend ran for a request that named nobody")
	assert.NotContains(t, rec.Body.String(), "provider-subject-7f3a",
		"the provider subject reached the backend as a principal")
}

// The control: the identical token, saying who it is, is served — so the refusal
// above is about the missing identifier and not about a path that broke. It is
// served under the name it CLAIMS, never under `sub`.
func TestReducedClaims_NamedPrincipal_IsServedUnderItsOwnName(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")
	checker := &fakeChecker{allowed: true}
	handler, reached := buildProductionAuthNChain(t, fix, checker)

	rec := getWithToken(t, handler, exemptRoute, fix.sign(t, namedClaims()))

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.True(t, *reached, "a token that names its principal must still be served")

	var got map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "usr_alice_acc_a1b2", got["principal_id"],
		"the principal must be the one the token names, never the provider's subject")
	assert.Equal(t, "user", got["principal_type"])
	assert.NotEqual(t, "provider-subject-7f3a", got["principal_id"])
}

// The mechanism, at the layer where the substitution lived: what the NEXT
// handler receives. Absence of an identifier must travel as absence — the
// principal headers are simply not written — while the token's own descriptive
// context (which says nothing about who anyone is) still travels, because the
// authN layer that runs before this one may have resolved a principal properly
// and must not lose the token's audit context along with the fabricated name.
func TestReducedClaims_LeaveNoPrincipalOnTheOnwardRequest(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")
	mw := buildStepUpMiddleware(t, stepUpVerifier(t, fix))

	var seen *http.Request
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, exemptRoute, nil)
	req.Header.Set("Authorization", "Bearer "+fix.sign(t, reducedClaims()))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.NotNil(t, seen, "the request never reached the next handler")
	assert.Empty(t, seen.Header.Get("X-Kacho-Principal-Id"),
		"an identifier was written for a token that carries none")
	assert.Empty(t, seen.Header.Get("Grpc-Metadata-X-Kacho-Principal-Id"),
		"the bridged form was written for a token that carries none")
	assert.Empty(t, seen.Header.Get("X-Kacho-Principal-Type"),
		"a principal type without a principal is still not a principal")
	assert.Equal(t, "2", seen.Header.Get("X-Kacho-Token-Acr"),
		"the token's own context describes the token, not who anyone is, and must survive")
}
