// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// introspection_classification_test.go — "the provider did not answer" and "the
// address does not serve this" are different facts and must not share a branch.
//
// A server that is down comes back. An address that does not serve introspection
// never will: every request pays the round-trip, gets the same non-answer, and —
// if the two are merged — is waved through on the strength of a failure that is
// permanent. That is how a revocation check ends up switched off while looking
// switched on, so the classification is pinned here rather than left to the
// caller's reading of an error string.
package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// newStatusServer answers every request with a fixed status and body.
func newStatusServer(status int, contentType, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func introspectAgainst(t *testing.T, url string) error {
	t.Helper()
	c, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: url,
		TTL:                   time.Minute,
	})
	require.NoError(t, err)
	_, ierr := c.Introspect(context.Background(), "jti-x", "raw-token")
	return ierr
}

// The endpoint is not there. This is the shape of the original defect — an
// address derived from the public issuer, where introspection is not served —
// and it must be reported as configuration, never as a passing hiccup.
func TestIntrospection_NotFound_IsMisconfiguration(t *testing.T) {
	srv := newStatusServer(http.StatusNotFound, "text/plain", "404 page not found")
	defer srv.Close()
	err := introspectAgainst(t, srv.URL)
	require.Error(t, err)
	assert.ErrorIs(t, err, middleware.ErrIntrospectionMisconfigured,
		"a missing endpoint is a wrong address, and no number of retries turns it into an answer")
}

// Wrong verb allowed at this path — again an address that is not the endpoint.
func TestIntrospection_MethodNotAllowed_IsMisconfiguration(t *testing.T) {
	srv := newStatusServer(http.StatusMethodNotAllowed, "text/plain", "")
	defer srv.Close()
	assert.ErrorIs(t, introspectAgainst(t, srv.URL), middleware.ErrIntrospectionMisconfigured)
}

// The admin API is reached but refuses us. If it wants credentials we do not
// send, that is our configuration to fix — not a reason to let requests past.
func TestIntrospection_Unauthorized_IsMisconfiguration(t *testing.T) {
	srv := newStatusServer(http.StatusUnauthorized, "application/json", `{"error":"unauthorized"}`)
	defer srv.Close()
	assert.ErrorIs(t, introspectAgainst(t, srv.URL), middleware.ErrIntrospectionMisconfigured)
}

// 200 with something that is not an introspection response — an ingress or an
// SPA answering on the address. Parsing failure here is proof of a wrong address.
func TestIntrospection_HTMLBody_IsMisconfiguration(t *testing.T) {
	srv := newStatusServer(http.StatusOK, "text/html", "<!doctype html><html><body>hi</body></html>")
	defer srv.Close()
	assert.ErrorIs(t, introspectAgainst(t, srv.URL), middleware.ErrIntrospectionMisconfigured)
}

// The provider is up but unwell. This one genuinely passes, so it keeps the
// documented soft-fail — and must NOT be classified as configuration.
func TestIntrospection_ServerError_IsTransient(t *testing.T) {
	srv := newStatusServer(http.StatusBadGateway, "text/plain", "bad gateway")
	defer srv.Close()
	err := introspectAgainst(t, srv.URL)
	require.Error(t, err)
	assert.NotErrorIs(t, err, middleware.ErrIntrospectionMisconfigured,
		"an unwell provider recovers; treating it as configuration would refuse traffic on a hiccup")
	assert.NotErrorIs(t, err, middleware.ErrTokenInactive)
}

// Rate limiting is the provider pushing back, not a wrong address.
func TestIntrospection_TooManyRequests_IsTransient(t *testing.T) {
	srv := newStatusServer(http.StatusTooManyRequests, "application/json", `{"error":"slow down"}`)
	defer srv.Close()
	assert.NotErrorIs(t, introspectAgainst(t, srv.URL), middleware.ErrIntrospectionMisconfigured)
}

// Nothing listening — transient by nature.
func TestIntrospection_Unreachable_IsTransient(t *testing.T) {
	srv := newStatusServer(http.StatusOK, "application/json", `{"active":true}`)
	url := srv.URL
	srv.Close() // nothing answers on this port any more
	assert.NotErrorIs(t, introspectAgainst(t, url), middleware.ErrIntrospectionMisconfigured)
}

// A well-formed answer is still a well-formed answer — the classification must
// not swallow the one case that matters.
func TestIntrospection_ProperAnswer_IsUnaffected(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"active": false})
	srv := newStatusServer(http.StatusOK, "application/json", string(body))
	defer srv.Close()
	err := introspectAgainst(t, srv.URL)
	assert.ErrorIs(t, err, middleware.ErrTokenInactive)
	assert.NotErrorIs(t, err, middleware.ErrIntrospectionMisconfigured)
}

// The per-call budget is the caller's, not a constant buried in the transport:
// a provider that never answers must not pin a request-handling goroutine for
// longer than the configured budget.
func TestIntrospection_HonoursConfiguredTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: srv.URL,
		TTL:                   time.Minute,
		Timeout:               100 * time.Millisecond,
	})
	require.NoError(t, err)

	start := time.Now()
	_, ierr := c.Introspect(context.Background(), "jti-slow", "raw")
	elapsed := time.Since(start)

	require.Error(t, ierr)
	assert.NotErrorIs(t, ierr, middleware.ErrIntrospectionMisconfigured)
	assert.Less(t, elapsed, time.Second,
		"the configured budget must bound the wait; a stalled provider cannot hold the request")
	// Deadline overruns are the one failure a caller must never read as an answer.
	assert.True(t, errors.Is(ierr, context.DeadlineExceeded) || elapsed < time.Second)
}

// The transport cannot establish trust with what answered. This is the shape of
// an operator HARDENING the address — moving it from plaintext to TLS — against a
// provider whose certificate this process has no reason to trust.
//
// It must be classified as configuration, and here is why that is not a matter of
// taste: on the "provider did not answer" branch the request PASSES. If a failed
// handshake landed there, the act of hardening the address would switch the
// revocation check off across the fleet, and the only trace would be one log line
// per window while every request sailed through unchecked. A permanent condition
// that no retry resolves belongs on the branch that refuses.
func TestIntrospection_TLSTrustFailure_IsMisconfiguration(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":true}`))
	}))
	defer srv.Close()

	// A default client: the server's certificate is signed by the test's own
	// throwaway CA, which the system roots do not carry.
	err := introspectAgainst(t, srv.URL)
	require.Error(t, err)
	assert.ErrorIs(t, err, middleware.ErrIntrospectionMisconfigured,
		"an untrusted certificate answers every retry identically — treating it as a "+
			"passing hiccup means hardening the address silently disables the check")
}

// The mirror image: TLS spoken at a plaintext listener. Same class, same answer.
func TestIntrospection_TLSAgainstPlaintextEndpoint_IsMisconfiguration(t *testing.T) {
	srv := newStatusServer(http.StatusOK, "application/json", `{"active":true}`)
	defer srv.Close()

	err := introspectAgainst(t, "https"+strings.TrimPrefix(srv.URL, "http"))
	require.Error(t, err)
	assert.ErrorIs(t, err, middleware.ErrIntrospectionMisconfigured)
}

// A scheme no HTTP transport can speak is configuration too — it never resolves.
func TestIntrospection_UnsupportedScheme_IsMisconfiguration(t *testing.T) {
	err := introspectAgainst(t, "ftp://provider.invalid/admin/oauth2/introspect")
	require.Error(t, err)
	assert.ErrorIs(t, err, middleware.ErrIntrospectionMisconfigured)
}

// And the boundary the classification must NOT cross: nothing listening is a
// passing condition — a provider restarting comes back on its own.
func TestIntrospection_ConnectionRefused_StaysTransient(t *testing.T) {
	srv := newStatusServer(http.StatusOK, "application/json", `{"active":true}`)
	url := srv.URL
	srv.Close() // the port is now closed

	err := introspectAgainst(t, url)
	require.Error(t, err)
	assert.NotErrorIs(t, err, middleware.ErrIntrospectionMisconfigured,
		"a provider that is down comes back; refusing traffic for the duration would "+
			"take the API down with it")
}
