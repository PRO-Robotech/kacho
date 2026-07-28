// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocation_check_e2e_test.go — what a caller observes when the revocation
// check cannot do its job.
//
// The check exists so that a token which was signed correctly, and has not yet
// expired, can still be turned off: a sign-out, a revoked machine key, a
// back-channel logout. Whether it works is not visible from a successful
// request — a request succeeds identically whether the check ran, or ran and was
// answered by the wrong server, or was skipped. So the observable pinned here is
// the one that differs: does the request reach the backend at all.
//
// Two failures, two answers:
//
//   - the address does not serve introspection (404, wrong verb, HTML) — a
//     permanent condition that no retry resolves. Requests are refused. This is
//     the only honest response: the alternative is a gateway that reports every
//     request as checked while checking nothing.
//   - the provider is unreachable or unwell (5xx, timeout) — a passing condition.
//     Requests continue, and the process says so on every window, because a
//     control that fails open in silence is a control nobody knows they lost.
package e2e_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// revocationHarness wires the production authN middleware over a stub identity
// provider whose introspection endpoint the test controls, and records whether
// the request ever reached the backend.
type revocationHarness struct {
	handler http.Handler
	logs    *bytes.Buffer
	reached *bool
}

func newRevocationHarness(t *testing.T, hydra *hydraFixture, introspectURL string, logInterval time.Duration) revocationHarness {
	t.Helper()
	verifier, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		JWKSURL: hydra.jwksURL, ExpectedIssuer: testIssuer, ExpectedAudience: testAudience,
	})
	require.NoError(t, err)
	dpopValidator, err := middleware.NewDPoPValidator(middleware.DPoPValidatorConfig{
		ReplayCache: middleware.NewDPoPReplayCache(middleware.DPoPReplayCacheConfig{}),
	})
	require.NoError(t, err)
	introspection, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: introspectURL,
		TTL:                   time.Minute,
		Timeout:               500 * time.Millisecond,
	})
	require.NoError(t, err)

	logs := &bytes.Buffer{}
	mw, err := middleware.NewDPoPMiddleware(middleware.DPoPMiddlewareConfig{
		Verifier:                        verifier,
		DPoP:                            dpopValidator,
		MTLS:                            middleware.NewMTLSBoundValidator(),
		StepUp:                          middleware.NewStepUpGate(time.Now),
		Introspection:                   introspection,
		Logger:                          slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		APIDomain:                       apiDomain,
		IntrospectionFailureLogInterval: logInterval,
	})
	require.NoError(t, err)

	reached := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	return revocationHarness{handler: handler, logs: logs, reached: &reached}
}

// plainBearer mints a valid, unrevoked, non-sender-constrained access token.
func plainBearer(t *testing.T, hydra *hydraFixture, jti string) string {
	t.Helper()
	now := time.Now().Unix()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": testIssuer, "aud": []any{testAudience}, "sub": "usr_alice_acc_a1b2",
		"iat": now, "exp": now + 900, "acr": "2", "jti": jti,
	})
	tok.Header["kid"] = hydra.kid
	signed, err := tok.SignedString(hydra.priv)
	require.NoError(t, err)
	return signed
}

func (h revocationHarness) call(t *testing.T, hydra *hydraFixture, jti string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "https://"+apiDomain+"/iam/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+plainBearer(t, hydra, jti))
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

// The address answers 404 — it is not the introspection endpoint. The request
// must NOT reach the backend: the gateway cannot say the token is still valid,
// so it must not act as if it had.
func TestE2E_Revocation_MisaddressedEndpoint_RefusesRequest(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	notTheEndpoint := httptest.NewServer(http.NotFoundHandler())
	defer notTheEndpoint.Close()

	h := newRevocationHarness(t, hydra, notTheEndpoint.URL, 0)
	rec := h.call(t, hydra, "jti-misaddressed")

	assert.False(t, *h.reached,
		"a request must not be served while the revocation check is aimed at an address "+
			"that cannot answer it — that is the control silently switched off")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"the fault is the gateway's configuration, not the caller's credential, so it must "+
			"not be reported as an authentication problem the caller could fix by logging in again")

	// The caller learns nothing about the deployment; the operator learns everything.
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	msg, _ := body["message"].(string)
	assert.NotContains(t, msg, notTheEndpoint.URL)
	assert.Contains(t, h.logs.String(), "revocation check misconfigured")
}

// An HTML page on the address is the same class — something answers, but it is
// not the endpoint.
func TestE2E_Revocation_HTMLOnEndpoint_RefusesRequest(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	wrongServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><html></html>"))
	}))
	defer wrongServer.Close()

	h := newRevocationHarness(t, hydra, wrongServer.URL, 0)
	rec := h.call(t, hydra, "jti-html")
	assert.False(t, *h.reached)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// The provider is unwell. This one passes on its own, so the documented
// soft-fail stands — but it is reported, every window, at ERROR.
func TestE2E_Revocation_ProviderUnwell_PassesButIsReported(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	unwell := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unwell.Close()

	h := newRevocationHarness(t, hydra, unwell.URL, 0)
	rec := h.call(t, hydra, "jti-unwell")

	assert.True(t, *h.reached, "a passing provider outage keeps traffic flowing (documented soft-fail)")
	assert.Equal(t, http.StatusOK, rec.Code)

	logged := h.logs.String()
	assert.Contains(t, logged, "revocation check unavailable")
	assert.Contains(t, logged, `"level":"ERROR"`,
		"a control that is currently not enforcing is not a WARN — nobody greps for WARN")
	assert.Contains(t, logged, "introspection_failures_total",
		"the report must carry a running count, or an intermittent outage is indistinguishable "+
			"from a permanent one in the log")
}

// The report is rate-limited: a soft-fail happens per request, and an unbounded
// line per request buries the log it is supposed to make legible.
func TestE2E_Revocation_SoftFailReport_IsRateLimited(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	unwell := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unwell.Close()

	h := newRevocationHarness(t, hydra, unwell.URL, time.Hour)
	for i := range 5 {
		rec := h.call(t, hydra, "jti-burst-"+string(rune('a'+i)))
		require.Equal(t, http.StatusOK, rec.Code)
	}
	require.True(t, *h.reached)

	got := strings.Count(h.logs.String(), "revocation check unavailable")
	assert.Equal(t, 1, got, "expected exactly one report per window, got %d", got)
}

// A revoked token is still rejected — the classification must not disturb the
// case the whole control exists for.
func TestE2E_Revocation_RevokedToken_StillRejected(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	revoked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
	}))
	defer revoked.Close()

	h := newRevocationHarness(t, hydra, revoked.URL, 0)
	rec := h.call(t, hydra, "jti-revoked")
	assert.False(t, *h.reached)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "token revoked")
}

// And a live token is served — the fail-closed branch must not swallow the
// happy path.
func TestE2E_Revocation_LiveToken_Served(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true, "sub": "usr_alice_acc_a1b2",
			"exp": time.Now().Add(15 * time.Minute).Unix(),
		})
	}))
	defer live.Close()

	h := newRevocationHarness(t, hydra, live.URL, 0)
	rec := h.call(t, hydra, "jti-live")
	assert.True(t, *h.reached)
	assert.Equal(t, http.StatusOK, rec.Code)
}
