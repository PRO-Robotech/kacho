// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// introspection_cache.go — provider-backed token introspection with negative-TTL
// LRU cache.
//
// Purpose: even when the access token's signature + claims pass local
// verification, the token may have been revoked server-side (admin logout,
// CAEP push, back-channel logout). The identity provider's introspection
// endpoint is the authoritative answer, but asking on every request costs a
// round-trip, so a tiny LRU with TTL = min(5s, exp-now) means a fresh access
// token round-trips at most every 5s.
//
// The endpoint lives on the provider's ADMIN API (`/admin/oauth2/introspect`),
// not on the public OAuth2 API — a distinct Service and port. Its address is
// configuration and is never derived; see config.ResolvedHydraIntrospectionURL
// and the boot guard in cmd/api-gateway/revocation_validation.go.
//
// Negative caching: when introspection returns `active=false`, we still cache
// the result (under the same TTL) — repeated requests from a compromised
// client shouldn't hammer Hydra.
//
// Invalidation: callers (session-revocations watcher) call Invalidate(jti)
// when a Postgres LISTEN/NOTIFY arrives. This is the primary path; the TTL
// is a backstop for the case where the NOTIFY connection drops.
//
// The eviction/TTL/LRU mechanics live in the shared internal/lrucache
// primitive (same as dpop_replay_cache.go and authz_cache.go); this file
// carries only the introspection-specific policy: the exp-bounded per-entry
// TTL clamp, negative caching, and the write-after-invalidate generation guard
// wired through lrucache.PutIfGenWithTTL.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/lrucache"
)

// ErrTokenInactive — the provider reported `active=false`; bubble up to caller
// and terminate request with 401.
var ErrTokenInactive = errors.New("token is not active (revoked or expired upstream)")

// ErrIntrospectionMisconfigured — the address answered, but what answered is not
// an introspection endpoint: the path is absent, the verb is refused, we are not
// authorised to ask, or the body is not an introspection response.
//
// This is deliberately a SEPARATE fact from "the provider did not answer". An
// unwell provider recovers on its own; a wrong address does not, so every
// request pays the round-trip and receives the same non-answer forever. Merged
// into one branch and waved through, that is a revocation check which reports as
// present and enforces nothing — the caller of Introspect must be able to tell
// the two apart, so the distinction lives in the error rather than in whatever
// the caller manages to infer from an error string.
var ErrIntrospectionMisconfigured = errors.New("introspection endpoint is misconfigured")

// defaultIntrospectionTimeout bounds one introspection round-trip.
//
// This is a blocking step on the request path, so the budget is what an unwell
// provider can cost a request-handling goroutine. Introspection is a single
// intra-cluster POST backed by one indexed lookup — tens of milliseconds when
// healthy — and the cache means a given token pays it at most once per TTL.
// A second is roughly ten times the healthy case: ample for a cold connection or
// a stalled lookup, and short enough that a provider brown-out cannot pin the
// gateway's capacity waiting on answers no caller is still there to receive.
const defaultIntrospectionTimeout = time.Second

// IntrospectionResult — minimal RFC 7662 section 2.2 response shape. Hydra returns
// many more fields; we keep only what downstream needs.
type IntrospectionResult struct {
	Active   bool   `json:"active"`
	Subject  string `json:"sub,omitempty"`
	Scope    string `json:"scope,omitempty"`
	ExpiryAt int64  `json:"exp,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	TokenUse string `json:"token_use,omitempty"`
}

// IntrospectionCache — LRU + TTL over the shared lrucache primitive. The
// authz-specific policy retained here is the exp-bounded per-entry TTL clamp and
// negative caching; the eviction/TTL mechanics live in the primitive.
type IntrospectionCache struct {
	url        string
	httpClient *http.Client
	ttl        time.Duration
	timeout    time.Duration
	now        func() time.Time

	// HTTP basic auth for the admin introspection endpoint, for a provider
	// deployment that fronts its admin API with one. Ory Hydra's own admin API
	// carries no authentication — it is protected by not being routable — so this
	// stays empty in this platform's profiles.
	basicUser string
	basicPass string

	cache *lrucache.Cache[string, IntrospectionResult]
}

// IntrospectionCacheConfig — construction parameters.
type IntrospectionCacheConfig struct {
	HydraIntrospectionURL string
	HTTPClient            *http.Client
	MaxEntries            int
	TTL                   time.Duration
	// Timeout bounds one round-trip to the provider. Zero → defaultIntrospectionTimeout.
	Timeout       time.Duration
	Now           func() time.Time
	BasicAuthUser string
	BasicAuthPass string
}

// NewIntrospectionCache constructs a cache. Returns error on empty URL.
func NewIntrospectionCache(cfg IntrospectionCacheConfig) (*IntrospectionCache, error) {
	if cfg.HydraIntrospectionURL == "" {
		return nil, errors.New("introspection cache: HydraIntrospectionURL is required")
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 10000
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 5 * time.Second
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultIntrospectionTimeout
	}
	hc := cfg.HTTPClient
	if hc == nil {
		// One budget, applied at both layers: the transport deadline and the
		// per-call context below. Two different numbers here would mean the
		// effective wait is whichever the reader did not look at.
		hc = &http.Client{Timeout: cfg.Timeout}
	}
	return &IntrospectionCache{
		url:        cfg.HydraIntrospectionURL,
		httpClient: hc,
		ttl:        cfg.TTL,
		timeout:    cfg.Timeout,
		now:        now,
		basicUser:  cfg.BasicAuthUser,
		basicPass:  cfg.BasicAuthPass,
		cache:      lrucache.New[string, IntrospectionResult](cfg.MaxEntries, cfg.TTL, now),
	}, nil
}

// Introspect returns the cached or freshly-fetched introspection result.
//
// Three outcomes the caller must distinguish:
//   - nil — the provider says the token is live.
//   - ErrTokenInactive — the provider says it is not. Reject the request.
//   - ErrIntrospectionMisconfigured — what answered is not an introspection
//     endpoint. This never resolves by itself, so it must not be waved through.
//
// Any other error means the provider did not answer this time, which passes.
//
// Key is the access-token JTI — small, opaque, never logged. We never pass
// the raw token through the cache map (defence-in-depth against memory dump
// disclosure).
func (c *IntrospectionCache) Introspect(ctx context.Context, jti, rawToken string) (IntrospectionResult, error) {
	if jti == "" {
		return IntrospectionResult{}, errors.New("introspect: jti required")
	}
	if rawToken == "" {
		return IntrospectionResult{}, errors.New("introspect: raw token required")
	}

	// 1. Cache hit?
	if r, ok := c.cache.Get(jti); ok {
		if !r.Active {
			return r, ErrTokenInactive
		}
		return r, nil
	}

	// Snapshot the invalidation generation BEFORE the (slow) Hydra fetch. A
	// force-logout revocation that calls Invalidate(jti) while this introspection
	// is in flight bumps the generation, and the generation-checked store below
	// is dropped — so a positive result computed against pre-revocation state can
	// never re-populate the just-flushed jti and survive for the full TTL
	// (write-after-invalidate guard; CWE-362 + CWE-613). Mirrors the sibling
	// decision cache (authz_cache.go putIfGen).
	gen := c.cache.Generation()

	// 2. Fetch from Hydra.
	res, err := c.fetchHydra(ctx, rawToken)
	if err != nil {
		return IntrospectionResult{}, err
	}

	// 3. Store negative + positive — TTL bounded by exp.
	// If exp is already in the past we treat the token as inactive and skip
	// caching the (stale) positive result. Defense: an attacker may not race
	// past the introspection result before exp slips by; we re-introspect on
	// next call so Hydra's fresh `active=false` is reflected immediately.
	ttl := c.ttl
	if res.ExpiryAt > 0 {
		// Route the exp clamp through the injectable clock (c.now), not the real
		// wall clock, so the TTL derivation is deterministic under test and
		// consistent with the get()/put() expiry checks that already use c.now().
		untilExp := time.Unix(res.ExpiryAt, 0).Sub(c.now())
		if untilExp <= 0 {
			res.Active = false
			return res, ErrTokenInactive
		}
		if untilExp < ttl {
			ttl = untilExp
		}
	}
	c.cache.PutIfGenWithTTL(jti, res, ttl, gen)

	if !res.Active {
		return res, ErrTokenInactive
	}
	return res, nil
}

// Invalidate removes the cached entry for jti AND bumps the invalidation
// generation. Called by the session_revocations LISTEN/NOTIFY handler to honor
// force-logout immediately (≤ 1s SLA). The generation bump is what closes the
// write-after-invalidate race: even when no entry is currently cached for jti
// (revocation arrives while an introspection is still in flight), an in-flight
// positive result for jti is dropped by the PutIfGenWithTTL guard in Introspect.
// InvalidateWhere bumps the generation even when zero entries match.
func (c *IntrospectionCache) Invalidate(jti string) {
	c.cache.InvalidateWhere(func(k string) bool { return k == jti })
}

// Len returns current cache size; used by tests / observability.
func (c *IntrospectionCache) Len() int { return c.cache.Len() }

func (c *IntrospectionCache) fetchHydra(ctx context.Context, rawToken string) (IntrospectionResult, error) {
	// The budget belongs to this call, not to the inbound request: a caller that
	// arrives with a generous deadline must not be able to hold a gateway
	// goroutine on a stalled provider for longer than the configured wait.
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	form := url.Values{}
	form.Set("token", rawToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, strings.NewReader(form.Encode()))
	if err != nil {
		// A URL this transport cannot even build a request for is configuration.
		return IntrospectionResult{}, fmt.Errorf("%w: build request: %w", ErrIntrospectionMisconfigured, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if c.basicUser != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Nothing answered: refused connection, DNS, timeout. All pass on their
		// own, so none of them is configuration.
		return IntrospectionResult{}, fmt.Errorf("introspect do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if transientStatus(resp.StatusCode) {
			return IntrospectionResult{}, fmt.Errorf("introspect status=%d body=%q", resp.StatusCode, string(body))
		}
		// Anything else — no such path, verb refused, not authorised to ask,
		// request shape rejected — describes the address, not the moment. It
		// answers every retry identically.
		return IntrospectionResult{}, fmt.Errorf("%w: status=%d body=%q",
			ErrIntrospectionMisconfigured, resp.StatusCode, string(body))
	}
	var out IntrospectionResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		// A 200 that is not an introspection response — an ingress default page,
		// an SPA, some other service on the port. The address is wrong.
		return IntrospectionResult{}, fmt.Errorf("%w: decode response: %w", ErrIntrospectionMisconfigured, err)
	}
	return out, nil
}

// transientStatus reports whether a non-200 status describes a passing condition
// of a provider that IS the introspection endpoint, rather than an address that
// is not one. Only these recover without anyone changing configuration:
// server-side faults, the provider asking us to slow down, and a request the
// provider timed out on its own side.
func transientStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	case http.StatusNotImplemented:
		// "This server does not implement that" is a statement about the address,
		// not about the moment — it is the one 5xx that never recovers on its own.
		return false
	}
	return code >= 500
}
