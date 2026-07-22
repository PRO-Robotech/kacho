// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// stepup_wiring_test.go — verifies that the DPoP HTTP middleware's step-up
// (ACR) gate is actually wired to the permission catalog + REST router, so a
// privileged RPC whose catalog entry demands `required_acr_min=2` rejects a
// token presenting a lower ACR with an RFC 9470 step-up challenge.
//
// Regression guard for the "step-up gate is a dead control" finding: before
// wiring, PermissionLookup defaulted to a no-op and the REST→FQN mapper
// produced "//vpc/v1/networks" (never a catalog key), so the gate could never
// fire on any request.

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// buildStepUpMiddleware wires a production-shaped DPoP middleware whose step-up
// gate is backed by the embedded permission catalog + the generated REST route
// table (exactly as cmd/api-gateway/main.go must wire it).
func buildStepUpMiddleware(t *testing.T, verifier *middleware.JWTVerifier) *middleware.DPoPMiddleware {
	t.Helper()
	replay := middleware.NewDPoPReplayCache(middleware.DPoPReplayCacheConfig{
		MaxEntries: 64, TTL: 60 * time.Second,
	})
	dpop, err := middleware.NewDPoPValidator(middleware.DPoPValidatorConfig{
		ReplayCache: replay, IatFreshness: 60 * time.Second,
	})
	require.NoError(t, err)

	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	mw, err := middleware.NewDPoPMiddleware(middleware.DPoPMiddlewareConfig{
		Verifier:         verifier,
		DPoP:             dpop,
		StepUp:           middleware.NewStepUpGate(time.Now),
		PermissionLookup: middleware.NewCatalogPermissionLookup(catalog),
		RestRouter:       middleware.NewRestRouter(),
		Logger:           slog.Default(),
		APIDomain:        "api.kacho.cloud",
	})
	require.NoError(t, err)
	return mw
}

func stepUpVerifier(t *testing.T, fix *jwksFixture) *middleware.JWTVerifier {
	t.Helper()
	v, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		JWKSURL: fix.url, ExpectedIssuer: testIssuer, ExpectedAudience: testAudience,
	})
	require.NoError(t, err)
	return v
}

// SEC-acr-stepup-refinement (SEC-ACR-01/03/21, I3): a SENSITIVE RPC
// (UserTokenService/Issue — credential mint, required_acr_min=2) called with a
// token presenting acr=1 must STILL be rejected with an RFC 9470 step-up
// challenge — step-up is preserved on the 41-set grant/credential surface.
func TestDPoPStepUp_SensitiveRPC_InsufficientACR_Blocks(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")
	mw := buildStepUpMiddleware(t, stepUpVerifier(t, fix))

	claims := standardClaims()
	claims["acr"] = "1" // password-only, below the required "2"
	token := fix.sign(t, claims)

	backendHit := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backendHit = true
		w.WriteHeader(http.StatusOK)
	}))

	// POST /iam/v1/users/{user_id}/tokens → UserTokenService/Issue (credential mint, acr=2).
	req := httptest.NewRequest(http.MethodPost, "https://api.kacho.cloud/iam/v1/users/usr-abc/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "step-up must reject acr=1 on a sensitive acr>=2 RPC")
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "insufficient_user_authentication",
		"must emit an RFC 9470 step-up challenge")
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `acr_values="2"`)
	assert.False(t, backendHit, "backend handler must not run when step-up is required")
}

// SEC-acr-stepup-refinement (SEC-ACR-08, I5, регрессия #3 — the core UNBLOCK):
// a ROUTINE resource RPC (NetworkService/Create, downgraded to required_acr_min=1)
// called with an ordinary AAL1 token (acr=1) now PASSES the step-up gate and
// reaches the backend — before the refinement this returned 401. This unblocks
// the production-newman user-subject flows (#59).
func TestDPoPStepUp_RoutineRPC_AAL1_Unblocked(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")
	mw := buildStepUpMiddleware(t, stepUpVerifier(t, fix))

	claims := standardClaims()
	claims["acr"] = "1" // password-only / AAL1 — the ordinary non-interactive token
	token := fix.sign(t, claims)

	backendHit := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backendHit = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "https://api.kacho.cloud/vpc/v1/networks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "routine RPC must pass at acr=1 after the refinement (UNBLOCK)")
	assert.True(t, backendHit, "backend handler must run — routine resource-create is not a grant")
}

// SEC-acr-stepup-refinement (SEC-ACR-10, I6, регрессия #4): the AAL1 floor holds
// — a routine RPC with acr=0 (no interactive auth) is STILL rejected. Downgrading
// to "1" does NOT open anonymous access (routine ≠ anonymous, fail-closed).
func TestDPoPStepUp_RoutineRPC_AAL0_StillBlocked(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")
	mw := buildStepUpMiddleware(t, stepUpVerifier(t, fix))

	claims := standardClaims()
	claims["acr"] = "0" // no interactive auth
	token := fix.sign(t, claims)

	backendHit := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backendHit = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "https://api.kacho.cloud/vpc/v1/networks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "routine acr=1 floor must reject acr=0 (routine ≠ anonymous)")
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `acr_values="1"`)
	assert.False(t, backendHit, "backend must not run — AAL1 floor holds")
}

// An unknown/unmapped path (no REST route → no catalog entry) carries no
// requirement and passes through — the gate must not fabricate a requirement
// out of a failed FQN resolution.
func TestDPoPStepUp_UnmappedPath_NoRequirement(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")
	mw := buildStepUpMiddleware(t, stepUpVerifier(t, fix))

	claims := standardClaims()
	claims["acr"] = "1"
	token := fix.sign(t, claims)

	backendHit := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backendHit = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://api.kacho.cloud/no/such/route/xyz", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, backendHit, "unmapped path has no catalog requirement → must pass")
	assert.Equal(t, http.StatusOK, rec.Code)
}
