// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocation_check_e2e_test.go — what a caller observes when a token has been
// turned off, and when the check that would notice cannot do its job.
//
// The check exists so that a token which was signed correctly, and has not yet
// expired, can still be turned off: a sign-out, a revoked machine key, a
// back-channel logout. Whether it works is not visible from a successful
// request — a request succeeds identically whether the check ran, or ran and was
// answered by the wrong server, or was never mounted at all. So the observable
// pinned here is the one that differs: does the request reach the backend.
//
// EVERY case below runs the chain the way a stand runs it: the authN layer that
// is always mounted, with the sender-constrained-token feature OFF. That is the
// point of this file. The check used to live inside the optional layer, which no
// profile switches on, so it had never run on any stand — a control with tests,
// a config guard, deploy wiring, and no reachable code path.
//
// Two failure modes, two answers:
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
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// revocationHarness wires the ALWAYS-MOUNTED authN layer over a stub identity
// provider whose introspection endpoint the test controls, and records whether
// the request ever reached the backend.
//
// The sender-constrained-token toggle is deliberately absent from this wiring:
// these tests must fail if the revocation check ever migrates back behind it.
type revocationHarness struct {
	handler http.Handler
	auth    *middleware.AuthInterceptor
	logs    *bytes.Buffer
	// reached is written from the request goroutine and read by the test; the
	// fan-out case drives several at once, so it is atomic rather than a bool
	// the race detector would (rightly) object to.
	reached *atomic.Bool
}

func newRevocationHarness(t *testing.T, hydra *hydraFixture, introspectURL string, logInterval time.Duration) revocationHarness {
	t.Helper()
	verifier, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{Issuers: []middleware.IssuerKeySet{{Issuer: testIssuer, KeySetURL: hydra.jwksURL, TokenTypes: []string{middleware.LegacyTokenType, middleware.PlatformTokenType}, TolerateAbsentTokenType: true}},

		ExpectedAudience: testAudience,
	})
	require.NoError(t, err)
	introspection, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: introspectURL,
		TTL:                   time.Minute,
		Timeout:               500 * time.Millisecond,
	})
	require.NoError(t, err)

	logs := &bytes.Buffer{}
	// Exactly the composition cmd/api-gateway builds for a stand: production
	// authN, JWKS verifier, revocation check. No DPoP middleware.
	auth := middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "", nil,
		slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	).
		WithVerifier(verifier).
		WithRevocationCheck(introspection, logInterval)

	reached := &atomic.Bool{}
	handler := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	return revocationHarness{handler: handler, auth: auth, logs: logs, reached: reached}
}

// plainBearer mints a valid, non-sender-constrained access token carrying the
// principal claims the token hook adds in production, so the authN layer resolves
// a principal without a subject-lookup round-trip.
func plainBearer(t *testing.T, hydra *hydraFixture, jti string) string {
	t.Helper()
	now := time.Now().Unix()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": testIssuer, "aud": []any{testAudience}, "sub": "usr_alice_acc_a1b2",
		"iat": now, "exp": now + 900, "acr": "2", "jti": jti,
		"kaname_principal_type": "user", "kaname_principal_id": "usr_alice_acc_a1b2",
	})
	tok.Header["kid"] = hydra.kid
	signed, err := tok.SignedString(hydra.priv)
	require.NoError(t, err)
	return signed
}

func (h revocationHarness) call(t *testing.T, hydra *hydraFixture, jti string) *httptest.ResponseRecorder {
	t.Helper()
	return h.callPath(t, hydra, jti, "/iam/v1/users/me")
}

func (h revocationHarness) callPath(t *testing.T, hydra *hydraFixture, jti, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "https://"+apiDomain+path, nil)
	req.Header.Set("Authorization", "Bearer "+plainBearer(t, hydra, jti))
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

// ─────────────────────── the case the control exists for ───────────────────────

// A revoked token, on an ordinary request, with the sender-constrained-token
// toggle OFF — which is every stand. Before the check was untied from that
// toggle this request was served.
func TestE2E_Revocation_RevokedToken_RejectedOnDefaultStand(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	revoked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
	}))
	defer revoked.Close()

	h := newRevocationHarness(t, hydra, revoked.URL, 0)
	rec := h.call(t, hydra, "jti-revoked")

	assert.False(t, h.reached.Load(),
		"a token the provider reports as no longer live must not reach a backend")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "token revoked")
}

// The same question on the gRPC surface. A gap on either surface makes the other
// pointless: whoever holds the revoked credential simply uses the unguarded one.
func TestE2E_Revocation_RevokedToken_RejectedOnGRPCSurface(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	revoked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
	}))
	defer revoked.Close()

	h := newRevocationHarness(t, hydra, revoked.URL, 0)
	_, err := h.unary(t, hydra, "jti-revoked-grpc")

	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

// unary drives the gRPC interceptor with the same credential.
func (h revocationHarness) unary(t *testing.T, hydra *hydraFixture, jti string) (any, error) {
	t.Helper()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer "+plainBearer(t, hydra, jti)))
	served := false
	resp, err := h.auth.Unary()(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/kaname.cloud.iam.v1.UserService/Get"},
		func(context.Context, any) (any, error) { served = true; return "ok", nil })
	if served {
		h.reached.Store(true)
	}
	return resp, err
}

// ───────────────────────── the check cannot answer ─────────────────────────

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

	assert.False(t, h.reached.Load(),
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

// Same fault, gRPC surface: unavailable, not unauthenticated — the caller's
// credential is not what is broken.
func TestE2E_Revocation_MisaddressedEndpoint_RefusesGRPCRequest(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	notTheEndpoint := httptest.NewServer(http.NotFoundHandler())
	defer notTheEndpoint.Close()

	h := newRevocationHarness(t, hydra, notTheEndpoint.URL, 0)
	_, err := h.unary(t, hydra, "jti-misaddressed-grpc")

	require.Error(t, err)
	assert.False(t, h.reached.Load())
	assert.Equal(t, codes.Unavailable, status.Code(err))
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
	assert.False(t, h.reached.Load())
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

	assert.True(t, h.reached.Load(), "a passing provider outage keeps traffic flowing (documented soft-fail)")
	assert.Equal(t, http.StatusOK, rec.Code)

	logged := h.logs.String()
	assert.Contains(t, logged, "revocation check unavailable")
	assert.Contains(t, logged, `"level":"ERROR"`,
		"a control that is currently not enforcing is not a WARN — nobody greps for WARN")
	assert.Contains(t, logged, "introspection_failures_total",
		"the report must carry a running count, or an intermittent outage is indistinguishable "+
			"from a permanent one in the log")
}

// The provider accepts the connection and then says nothing. The per-call budget
// is what keeps that from becoming the caller's problem: the request is answered,
// not held, and it is answered by passing — an identity provider that stops
// responding must not take the API down with it.
func TestE2E_Revocation_ProviderStalls_RequestStillAnswered(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	// Stalls for several times the harness budget (500ms) — long enough that an
	// unbounded wait would be unmistakable, short enough that the suite does not
	// pay for it on teardown.
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer stalled.Close()

	h := newRevocationHarness(t, hydra, stalled.URL, 0)
	started := time.Now()
	rec := h.call(t, hydra, "jti-stalled")
	elapsed := time.Since(started)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, h.reached.Load())
	assert.Less(t, elapsed, 2*time.Second,
		"the budget belongs to the check, not to the caller: %s is a request-handling "+
			"goroutine pinned on a provider nobody is waiting for", elapsed)
	assert.Contains(t, h.logs.String(), "revocation check unavailable")
}

// A verified token with no identifier cannot be asked about — introspection is
// keyed on the jti. Our provider mints JWT access tokens, which carry one, so
// this is not reachable with a credential this platform issued; it is pinned
// because if it ever became reachable the control would stop enforcing, and
// "did not run" must never be indistinguishable from "passed".
func TestE2E_Revocation_TokenWithoutIdentifier_IsReportedNotSilent(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	live := newLiveIntrospection()
	defer live.Close()

	h := newRevocationHarness(t, hydra, live.URL, 0)

	now := time.Now().Unix()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": testIssuer, "aud": []any{testAudience}, "sub": "usr_alice_acc_a1b2",
		"iat": now, "exp": now + 900, "acr": "2",
		"kaname_principal_type": "user", "kaname_principal_id": "usr_alice_acc_a1b2",
	})
	tok.Header["kid"] = hydra.kid
	signed, err := tok.SignedString(hydra.priv)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "https://"+apiDomain+"/iam/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int32(0), atomic.LoadInt32(&live.hits),
		"there is nothing to ask about without an identifier")
	logged := h.logs.String()
	assert.Contains(t, logged, "revocation check skipped: token carries no identifier")
	assert.Contains(t, logged, `"level":"ERROR"`)
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
	require.True(t, h.reached.Load())

	got := strings.Count(h.logs.String(), "revocation check unavailable")
	assert.Equal(t, 1, got, "expected exactly one report per window, got %d", got)
}

// And a live token is served — the fail-closed branch must not swallow the
// happy path.
func TestE2E_Revocation_LiveToken_Served(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	live := newLiveIntrospection()
	defer live.Close()

	h := newRevocationHarness(t, hydra, live.URL, 0)
	rec := h.call(t, hydra, "jti-live")
	assert.True(t, h.reached.Load())
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ─────────────────────────── the exempted paths ───────────────────────────

// Signing out must work with a credential that is already dead — otherwise the
// one action left to a user whose session was revoked elsewhere is the action
// the gateway refuses. The exemption is the shared pre-auth allow-list, so the
// health probes and the session-identity route are covered by the same decision.
//
// The list below used to name an interactive-login route. That route was retired
// with the identity provider it addressed, and the allow-list branch that covered
// it stopped being a prefix — so the case moved to /iam/v1/auth/me, which is the
// route that actually remains exempt. Probing the retired path would have proven
// nothing about the exemption: it would have measured the catch-all.
func TestE2E_Revocation_ExemptPaths_StillServedWithRevokedToken(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	asked := int32(0)
	revoked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&asked, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
	}))
	defer revoked.Close()

	for _, path := range []string{"/oauth/logout", "/healthz", "/readyz", "/iam/v1/auth/me"} {
		h := newRevocationHarness(t, hydra, revoked.URL, 0)
		rec := h.callPath(t, hydra, "jti-exempt", path)
		assert.True(t, h.reached.Load(), "%s must stay reachable with a revoked credential", path)
		assert.Equal(t, http.StatusOK, rec.Code, "path %s", path)
	}
	assert.Equal(t, int32(0), atomic.LoadInt32(&asked),
		"an exempt path must not pay a round-trip to the provider either")
}

// The same exemption must hold when the check is MISADDRESSED: a broken address
// would otherwise take sign-out down with it, which is the one path that has to
// survive a broken deployment.
func TestE2E_Revocation_LogoutSurvives_MisaddressedEndpoint(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	notTheEndpoint := httptest.NewServer(http.NotFoundHandler())
	defer notTheEndpoint.Close()

	h := newRevocationHarness(t, hydra, notTheEndpoint.URL, 0)
	rec := h.callPath(t, hydra, "jti-logout-misaddressed", "/oauth/logout")
	assert.True(t, h.reached.Load())
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ───────────────────────────── what it costs ─────────────────────────────

// newLiveIntrospection answers "active" and counts how many times it was asked.
func newLiveIntrospection() *countingServer {
	cs := &countingServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&cs.hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true, "sub": "usr_alice_acc_a1b2",
			"exp": time.Now().Add(15 * time.Minute).Unix(),
		})
	}))
	cs.Server = srv
	return cs
}

type countingServer struct {
	*httptest.Server
	hits int32
}

// The check is on the request path of every authenticated call, so its cost is
// part of the change. Within one cache window a token pays for ONE round-trip
// however many requests it makes: the price is per token per window, not per
// request. Pinned because a regression here (a key that varies per request, a
// TTL that stops applying) turns a bounded cost into a per-request one.
func TestE2E_Revocation_CostIsPerTokenPerWindow(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	live := newLiveIntrospection()
	defer live.Close()

	h := newRevocationHarness(t, hydra, live.URL, 0)
	const requests = 50
	for range requests {
		require.Equal(t, http.StatusOK, h.call(t, hydra, "jti-hot").Code)
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&live.hits),
		"%d requests on one live token must cost one round-trip while the entry is cached", requests)

	// A second token is a second entry — the bound is per token, and the test
	// says so rather than leaving "1" to be read as "one per process".
	require.Equal(t, http.StatusOK, h.call(t, hydra, "jti-other").Code)
	assert.Equal(t, int32(2), atomic.LoadInt32(&live.hits))
}

// Concurrent first requests for the SAME cold token: there is no single-flight,
// so each in-flight miss asks. This is a measurement, not an aspiration — it is
// pinned so the number is known rather than assumed, and so that adding
// single-flight later is a visible change.
func TestE2E_Revocation_ColdTokenFanOut_IsUnshared(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()
	live := newLiveIntrospection()
	defer live.Close()

	h := newRevocationHarness(t, hydra, live.URL, 0)
	const parallel = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			h.call(t, hydra, "jti-cold")
		}()
	}
	close(start)
	wg.Wait()

	burst := atomic.LoadInt32(&live.hits)
	assert.GreaterOrEqual(t, burst, int32(1), "the check must have run at all")
	assert.LessOrEqual(t, burst, int32(parallel),
		"a cold token costs at most one round-trip per concurrent request")

	// The claim worth locking is not the burst size — it is that the burst ENDS.
	// A further request must be answered from the cache; if it is not, every
	// request pays the provider forever and the measured cost above is fiction.
	require.Equal(t, http.StatusOK, h.call(t, hydra, "jti-cold").Code)
	assert.Equal(t, burst, atomic.LoadInt32(&live.hits),
		"once the entry is written, a subsequent request must cost nothing")
}
