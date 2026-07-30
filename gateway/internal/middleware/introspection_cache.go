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
// What a FAILING provider costs. Since the check moved onto the authN layer, it
// runs on every authenticated request, so "how often do we ask" stopped being
// free. An answer is amortised by the cache above, but a non-answer was not
// cached and concurrent questions about one token were not shared — so an
// unwell provider was asked once per request, each question holding a
// request-handling goroutine and a connection for the whole per-call budget,
// all of it aimed at something already struggling. Three mechanisms, because the
// shapes fail differently and none of them covers the others:
//
//   - questions arriving ONE AFTER ANOTHER about one token are answered from a
//     briefly remembered failure (introspectionFailureWindow);
//   - questions arriving TOGETHER on a cold entry are collapsed into one
//     round-trip by the in-flight group — nothing is remembered yet, so a cache
//     is no help to them at all;
//   - questions about MANY DIFFERENT tokens are bounded by the service-wide
//     breaker (introspection_breaker.go). Both mechanisms above are keyed on the
//     token, so during an outage every live credential is a first question that
//     misses both, and N tokens cost N round-trips. The breaker is the only one
//     of the three that changes WHOM we stop asking rather than how often, which
//     is why the reasoning for it is written out at length in its own file.
//
// A wrong ADDRESS is remembered differently from an unreachable provider, and
// deliberately so; see rememberMisconfigured.
//
// Invalidation: the TTL is what bounds how long a revocation goes unnoticed —
// worst case one window (default 5s) — because NOTHING in this process calls
// Invalidate today. The method and the write-after-invalidate guard in
// Introspect exist for a push path (a session-revocations LISTEN/NOTIFY
// watcher) that is not wired here; this header used to describe that push as
// the primary path, which read as a promise the deployment does not keep. Read
// the TTL as the only bound, and size it accordingly.
//
// The eviction/TTL/LRU mechanics live in the shared internal/lrucache
// primitive (same as dpop_replay_cache.go and authz_cache.go); this file
// carries only the introspection-specific policy: the exp-bounded per-entry
// TTL clamp, negative caching, and the write-after-invalidate generation guard
// wired through lrucache.PutIfGenWithTTL.
package middleware

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

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

// introspectionFailureWindow — how long a question the provider did not answer
// is remembered, so the next one is answered without another round-trip.
//
// It is a SEPARATE window from the one for an answer the provider gave (5s by
// default), and much shorter, because the two buy different things. The positive
// window amortises a round-trip over a verdict we actually hold. This one holds
// no verdict at all: every moment of it is a moment the check is not asking, laid
// on top of a control that is already not enforcing, so it is kept to the least
// that still removes the stampede.
//
// One second is the same order as the per-call budget: a stalled provider costs a
// given token at most one held round-trip per second instead of one per request —
// at any request rate — and a provider that comes back is noticed within a second
// of doing so.
const introspectionFailureWindow = time.Second

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

	// failures — questions the provider did not answer, remembered briefly and
	// PER TOKEN. Kept in its own cache rather than as a marker value in the one
	// above: an answer and the absence of one expire on different clocks, and
	// Len() must keep meaning "answers held" for the callers that read it.
	//
	// Per token, not per process, because this verdict PASSES the request.
	// Widening it beyond the token that actually failed would stop the check
	// asking about tokens it never tried — that is the opposite of what a
	// stampede guard is for.
	failures *lrucache.Cache[string, error]

	// flight collapses concurrent questions about the SAME token into a single
	// round-trip. It is not an alternative to the negative cache above: on a cold
	// entry nothing has been remembered yet, so every member of a burst misses,
	// and only in-flight sharing can help. Keyed on the token, so distinct
	// credentials remain distinct questions and never serialise.
	flight singleflight.Group

	// misconfigured* — "what answered is not an introspection endpoint", held
	// once for the whole process with an expiry. See rememberMisconfigured for
	// why this one is NOT per token.
	misMu    sync.Mutex
	misErr   error
	misUntil time.Time

	// breaker — the service-wide bound on questions to a provider that is
	// answering nobody. The two caches above are per token and cannot see that
	// shape; this one can, at the price of withholding the question from tokens
	// it never tried. That price and why it is worth paying are argued in
	// introspection_breaker.go.
	breaker *introspectionBreaker
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
		failures:   lrucache.New[string, error](cfg.MaxEntries, introspectionFailureWindow, now),
		breaker: newIntrospectionBreaker(
			introspectionBreakerThreshold, introspectionBreakerCooldown, now),
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

	// 1. An answer we already hold.
	if r, ok := c.cache.Get(jti); ok {
		if !r.Active {
			return r, ErrTokenInactive
		}
		return r, nil
	}

	// 2. The address itself is known to be wrong. Checked AFTER the answer cache
	// on purpose: an answer the provider gave is still a genuine answer inside
	// its own window, and refusing it would widen a configuration fault into an
	// outage larger than the fault. Checked BEFORE asking, because asking again
	// cannot change it — see rememberMisconfigured.
	if err, ok := c.misconfiguredVerdict(); ok {
		return IntrospectionResult{}, err
	}

	// 3. This token asked a moment ago and the provider did not answer. Same
	// verdict, no second round-trip to something already unwell.
	if err, ok := c.failures.Get(jti); ok {
		return IntrospectionResult{}, err
	}

	// 4. Ask — once per token, however many callers are waiting on the answer,
	// and only while the service-wide breaker is letting questions through
	// (step 5, inside ask).
	return c.ask(ctx, jti, rawToken)
}

// ask performs the round-trip, sharing it with every other caller asking about
// the same token at the same moment.
func (c *IntrospectionCache) ask(ctx context.Context, jti, rawToken string) (IntrospectionResult, error) {
	// A caller that has already gone away starts nothing: the point of this file
	// is to reduce the number of questions, not to keep asking on behalf of
	// requests nobody is waiting for.
	//
	// Checked BEFORE the breaker permit below, so a departing caller cannot claim
	// the half-open probe and then not use it.
	if err := ctx.Err(); err != nil {
		return IntrospectionResult{}, err
	}

	// 5. The provider is answering nobody. Withholding the question here — rather
	// than per token, as everything above does — is what bounds the cost of an
	// outage; it passes the request exactly as a real non-answer would.
	//
	// The permit is taken immediately before the round-trip that reports its
	// outcome: fetchAndRecord runs under a context stripped of cancellation and
	// always files recordAnswered or recordUnanswered, so a claimed half-open
	// probe can never be dropped on the floor.
	if permit, verdict := c.breaker.allow(); !permit {
		return IntrospectionResult{}, verdict
	}

	ch := c.flight.DoChan(jti, func() (any, error) {
		// The shared round-trip belongs to no single caller. Whoever happened to
		// start it must not be able to cancel the answer everyone else is waiting
		// for by walking away, so its cancellation is dropped here — the values
		// (request-scoped correlation) are kept. It still cannot run long:
		// fetchHydra applies the configured per-call budget on top.
		return c.fetchAndRecord(context.WithoutCancel(ctx), jti, rawToken)
	})

	select {
	case <-ctx.Done():
		// This caller left. The shared question carries on for whoever is still
		// waiting, and this caller gets its own cancellation — exactly what it
		// got back when every request asked for itself.
		return IntrospectionResult{}, ctx.Err()
	case r := <-ch:
		res, _ := r.Val.(IntrospectionResult)
		return res, r.Err
	}
}

// fetchAndRecord asks the provider once and files what came back.
func (c *IntrospectionCache) fetchAndRecord(ctx context.Context, jti, rawToken string) (IntrospectionResult, error) {
	// Snapshot the invalidation generations BEFORE the (slow) Hydra fetch. A
	// force-logout revocation that calls Invalidate(jti) while this introspection
	// is in flight bumps them, and the generation-checked stores below are
	// dropped — so neither a positive result computed against pre-revocation
	// state, nor a remembered non-answer that would let the next request past
	// unasked, can re-populate the just-flushed jti and survive its window
	// (write-after-invalidate guard; CWE-362 + CWE-613). Mirrors the sibling
	// decision cache (authz_cache.go putIfGen).
	gen := c.cache.Generation()
	failGen := c.failures.Generation()

	res, err := c.fetchHydra(ctx, rawToken)
	if err != nil {
		if errors.Is(err, ErrIntrospectionMisconfigured) {
			// Something answered — it just was not an introspection endpoint. That
			// is a reachable address, so as far as the breaker is concerned the
			// question was answered; the refusal is carried by the wrong-address
			// memory, which Introspect consults before the breaker anyway.
			c.breaker.recordAnswered()
			c.rememberMisconfigured(err)
			return IntrospectionResult{}, err
		}
		c.breaker.recordUnanswered(err)
		c.failures.PutIfGenWithTTL(jti, err, introspectionFailureWindow, failGen)
		return IntrospectionResult{}, err
	}
	// The provider answered. Whether it called the token live or dead, it is
	// reachable, so any breaker state established by earlier silences is stale.
	c.breaker.recordAnswered()

	// Store negative + positive — TTL bounded by exp.
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

// rememberMisconfigured files "what answered is not an introspection endpoint"
// ONCE FOR THE PROCESS, with an expiry.
//
// Not per token, because it is not a fact about a token. The address is the same
// address whichever credential is offered, so asking again with a different one
// cannot produce a different answer — a per-token memory would make every token
// pay to learn the same thing. Holding it process-wide is safe in a way the
// unanswered case is not: this verdict only ever REFUSES (the caller answers
// Unavailable and serves nothing), so widening it can never let a request past
// unchecked. Widening the unanswered verdict the same way would do exactly that,
// which is why the two are remembered differently.
//
// With an expiry, because "does not heal by itself" means nobody here can heal
// it — not that nothing can. A certificate rotated or a path restored on the
// provider's side is a repair made without touching this process, and a refusal
// that outlived it would be an outage of our own making. So the verdict lapses
// and is re-established by one question, rather than sticking until a restart.
func (c *IntrospectionCache) rememberMisconfigured(err error) {
	c.misMu.Lock()
	defer c.misMu.Unlock()
	c.misErr = err
	c.misUntil = c.now().Add(introspectionFailureWindow)
}

// misconfiguredVerdict reports the process-wide wrong-address verdict while it is
// still live.
func (c *IntrospectionCache) misconfiguredVerdict() (error, bool) {
	c.misMu.Lock()
	defer c.misMu.Unlock()
	if c.misErr == nil || !c.now().Before(c.misUntil) {
		return nil, false
	}
	return c.misErr, true
}

// Invalidate removes the cached entry for jti AND bumps the invalidation
// generation. It is the hook a session_revocations LISTEN/NOTIFY handler would
// use to honour a force-logout without waiting out the TTL; no such handler is
// wired in this process today, so nothing calls it in production and the TTL
// alone bounds the window (see the file header). The generation bump closes the
// write-after-invalidate race: even when no entry is currently cached for jti
// (revocation arrives while an introspection is still in flight), an in-flight
// positive result for jti is dropped by the PutIfGenWithTTL guard in Introspect.
// InvalidateWhere bumps the generation even when zero entries match.
//
// A remembered non-answer for the same jti is dropped too: it would let the next
// request past without asking, which is precisely what a force-logout must not
// permit.
func (c *IntrospectionCache) Invalidate(jti string) {
	c.cache.InvalidateWhere(func(k string) bool { return k == jti })
	c.failures.InvalidateWhere(func(k string) bool { return k == jti })
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
	// #nosec G704 -- адрес берётся из настроек процесса (cfg.HydraIntrospectionURL,
	// проверяется при старте и не может быть пустым), а не из запроса: подставить его
	// вызывающему нечем. Правило видит "переменная в адресе" и не различает источник.
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
	// #nosec G704 -- тот же адрес из настроек процесса, что и у построения запроса выше.
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if permanentTransportFailure(err) {
			return IntrospectionResult{}, fmt.Errorf("%w: %w", ErrIntrospectionMisconfigured, err)
		}
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

// permanentTransportFailure reports whether a transport-level failure describes
// the ADDRESS rather than the moment — i.e. one that answers every retry
// identically until somebody changes configuration.
//
// This distinction is load-bearing in an unobvious direction. The "did not
// answer" branch PASSES the request, so a permanent failure filed there switches
// the revocation check off for as long as it lasts, which is forever. The case
// that made it worth naming: an operator moves the address from plaintext to TLS
// — the correct instinct — and the handshake fails because this process trusts
// system roots and the provider's certificate comes from an internal CA. Filed as
// a hiccup, that hardening step would silently disable the control across the
// fleet, visible only as one log line per window. Filed here, it refuses loudly
// and gets fixed.
//
// Deliberately NOT included: refused connections, DNS misses, timeouts. A
// provider that is restarting, or a name that has not propagated yet, comes back
// on its own; refusing traffic for the duration would take the API down with the
// identity provider.
func permanentTransportFailure(err error) bool {
	var (
		unknownAuthority x509.UnknownAuthorityError
		hostnameErr      x509.HostnameError
		invalidCert      x509.CertificateInvalidError
		recordHeader     tls.RecordHeaderError
		certVerify       *tls.CertificateVerificationError
	)
	switch {
	case errors.As(err, &unknownAuthority), // certificate signed by an unknown CA
		errors.As(err, &hostnameErr), // certificate is for another name
		errors.As(err, &invalidCert), // expired / not yet valid / bad usage
		errors.As(err, &certVerify),  // verification failed as a whole
		errors.As(err, &recordHeader),
		errors.Is(err, http.ErrSchemeMismatch): // TLS spoken at a plaintext listener
		return true
	}
	// A scheme no HTTP transport can speak is configuration by construction.
	var urlErr *url.Error
	if errors.As(err, &urlErr) && strings.Contains(urlErr.Err.Error(), "unsupported protocol scheme") {
		return true
	}
	return false
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
