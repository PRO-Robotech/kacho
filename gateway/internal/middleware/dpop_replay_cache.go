// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dpop_replay_cache.go — the record of seen DPoP `jti` values kept in the
// memory of THIS process.
//
// RFC 9449 section 11.1 requires the resource server to reject a DPoP proof
// whose `jti` has already been seen within a window comparable to
// `iat`-freshness. This type is ONE of the two implementations of
// middleware.DPoPReplayGuard (see that port's doc comment): it holds the
// record in an LRU over the shared internal/lrucache primitive — default
// capacity 100k, entry TTL 120s = 2× the iat-freshness window. The other
// implementation holds the record in a table shared by the whole fleet.
//
// What this cache promises, and where the promise ends: single presentation
// holds for THIS replica. A captured proof replayed against a sibling pod
// finds no entry here and passes. The `iat` freshness check bounds how long
// such a proof stays usable at all — it does not make single presentation a
// property of the fleet, and nothing here should be read as claiming that it
// does.
//
// The choice between the two lanes is made at the composition root
// (gateway/cmd/api-gateway/main.go, the DPoP arm), never here: it follows the
// same store selection that backs `Idempotency-Key`, and startup validation
// refuses this in-memory lane when the declared fleet is larger than one
// replica. Which configuration knob each lane reads is documented on the
// gateway architecture page (gateway/docs/content/architecture/authn.mdx,
// «Однократность предъявления») and is not restated here.
//
// Set semantics: the jti is the key, the value is empty. Replay detection uses
// a single atomic add-if-absent (lrucache.AddIfAbsent) so that (a) the
// presence-check and insert share one critical section — no check-then-act race
// — and (b) a repeated jti is NOT kept warm in the LRU (a losing add does not
// refresh recency), so an attacker replaying a proof must not extend its own
// entry's lifetime.
package middleware

import (
	"errors"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/lrucache"
)

// ErrDPoPReplay — sentinel; DPoP jti was already used within the freshness
// window. Mapped to `WWW-Authenticate: DPoP error="invalid_dpop_proof"`.
var ErrDPoPReplay = errors.New("dpop jti already used (replay detected)")

// DPoPReplayCache — bounded LRU with per-entry TTL, backed by lrucache.
type DPoPReplayCache struct {
	c *lrucache.Cache[string, struct{}]
}

// DPoPReplayCacheConfig — construction parameters.
type DPoPReplayCacheConfig struct {
	MaxEntries int
	TTL        time.Duration
	Now        func() time.Time // tests override
}

// NewDPoPReplayCache constructs a replay cache. Non-positive sizes fall back to
// safe defaults (never silently disable replay protection).
func NewDPoPReplayCache(cfg DPoPReplayCacheConfig) *DPoPReplayCache {
	maxEntries := cfg.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 100000
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 120 * time.Second
	}
	return &DPoPReplayCache{c: lrucache.New[string, struct{}](maxEntries, ttl, cfg.Now)}
}

// Add registers a new jti. Returns ErrDPoPReplay if the jti is already present
// (within TTL); otherwise stores it and returns nil. TTL and capacity eviction
// are handled by the underlying cache (capacity eviction is the DoS guard — an
// attacker cannot flood the cache to age out a real jti any faster than the LRU
// bound allows).
//
// The presence-check and insert are a single atomic add-if-absent under one
// lock hold (lrucache.AddIfAbsent), NOT a check-then-act Peek+Put pair: this is
// the in-memory analogue of an atomic CAS `INSERT … ON CONFLICT` and closes the
// TOCTOU window that would otherwise let concurrent replays of one captured
// proof (identical jti) all pass replay detection (RFC 9449
// section 11.1). An already-present jti is not kept warm — a replay must not extend its
// own entry's lifetime (AddIfAbsent does not refresh recency on a losing add).
//
// Thread-safe under concurrent goroutines; concurrent Adds of the same jti
// resolve to exactly one winner.
func (c *DPoPReplayCache) Add(jti string) error {
	if jti == "" {
		return errors.New("empty jti")
	}
	if !c.c.AddIfAbsent(jti, struct{}{}) {
		return ErrDPoPReplay
	}
	return nil
}

// Len returns the current number of entries. Used by tests / observability.
func (c *DPoPReplayCache) Len() int { return c.c.Len() }

// Purge removes all entries. Used by tests. NOT wired to config reload:
// dropping seen-proof state would re-open the replay window, so the replay
// cache is intentionally excluded from the SIGHUP authz-config reload.
func (c *DPoPReplayCache) Purge() { c.c.Invalidate() }
