// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// introspection_stampede_test.go — what a failing provider COSTS.
//
// The revocation check now runs on the layer every authenticated request passes
// through, so the price of asking is paid per request rather than never. That
// makes one number load-bearing: how many round-trips a given token costs while
// the provider is unwell. These tests measure that number. They do not assert
// that a function was called — a stampede is a quantity, and a test that cannot
// count cannot see one.
//
// Two halves, deliberately pinned apart because they fail differently:
//
//   - a burst arriving TOGETHER on a cold entry must collapse to one round-trip
//     (nothing has been remembered yet, so only in-flight sharing can help);
//   - questions arriving ONE AFTER ANOTHER inside the window must cost nothing
//     (the flight is over, so only a remembered failure can help).
//
// Either half alone leaves the other shape at full price.
package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// burstSettle — how long the test waits for a burst that should NOT arrive.
// Proving that nothing else reached the provider means waiting for it not to;
// this is that wait. Without in-flight sharing every member of the burst lands
// well inside it, so the failing case fails loudly rather than marginally.
const burstSettle = 200 * time.Millisecond

// heldServer answers with `status` (and `body`) but only after the test releases
// it, so a burst aimed at it is genuinely concurrent: no member can be served
// from an entry a faster sibling already wrote.
func heldServer(t *testing.T, status int, body string) (url string, hits *atomic.Int32, release func()) {
	t.Helper()
	hits = &atomic.Int32{}
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		<-gate
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	var once sync.Once
	return srv.URL, hits, func() { once.Do(func() { close(gate) }) }
}

// burst runs n concurrent Introspect calls for the same jti and returns their errors.
func burst(c *middleware.IntrospectionCache, n int, jti string) func() []error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = c.Introspect(context.Background(), jti, "raw-token")
		}()
	}
	close(start)
	return func() []error { wg.Wait(); return errs }
}

// A provider that is failing must not be asked once per request. Thirty-two
// requests carrying the SAME credential, arriving while the first question is
// still open, is one question — not thirty-two held goroutines and thirty-two
// connections aimed at something already unwell.
func TestIntrospection_FailingProvider_ConcurrentBurstIsOneRoundTrip(t *testing.T) {
	url, hits, release := heldServer(t, http.StatusBadGateway, `{"error":"bad gateway"}`)

	c, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: url,
		TTL:                   time.Minute,
		Timeout:               5 * time.Second,
	})
	require.NoError(t, err)

	wait := burst(c, 32, "jti-storm")

	require.Eventually(t, func() bool { return hits.Load() >= 1 }, 5*time.Second, time.Millisecond,
		"the first question never reached the provider")
	time.Sleep(burstSettle) // see burstSettle: waiting for what must not arrive
	release()

	for _, e := range wait() {
		require.Error(t, e, "a failing provider must still report failure to every caller")
		assert.NotErrorIs(t, e, middleware.ErrTokenInactive,
			"a provider that did not answer has not said the token is dead")
	}
	assert.Equal(t, int32(1), hits.Load(),
		"32 concurrent requests for one token cost %d round-trips to an already-unwell provider; "+
			"a burst on a cold entry must collapse to one", hits.Load())
}

// The other half: once the flight is over, the next question must be answered
// from what was already learned. Without this, a single client polling at any
// rate re-opens a connection to the sick provider on every request.
func TestIntrospection_FailingProvider_SecondQuestionInWindowCostsNothing(t *testing.T) {
	srv, hits := failingServer(t, http.StatusBadGateway)

	c, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: srv,
		TTL:                   time.Minute,
	})
	require.NoError(t, err)

	_, err1 := c.Introspect(context.Background(), "jti-poll", "raw-token")
	require.Error(t, err1)
	require.Equal(t, int32(1), hits.Load())

	_, err2 := c.Introspect(context.Background(), "jti-poll", "raw-token")
	require.Error(t, err2, "the verdict must not change: an unanswered question still passes on its own")
	assert.NotErrorIs(t, err2, middleware.ErrTokenInactive)
	assert.NotErrorIs(t, err2, middleware.ErrIntrospectionMisconfigured)
	assert.Equal(t, int32(1), hits.Load(),
		"the same question inside the window cost another round-trip (%d total)", hits.Load())
}

// Cold start, provider healthy: N first-ever requests for one token are still
// one question. This half is invisible to the negative cache — there is nothing
// remembered yet, and every member of the burst misses.
func TestIntrospection_ColdStart_ConcurrentBurstIsOneRoundTrip(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"active": true, "sub": "usr_alice",
		"exp": time.Now().Add(15 * time.Minute).Unix(),
	})
	url, hits, release := heldServer(t, http.StatusOK, string(body))

	c, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: url,
		TTL:                   time.Minute,
		Timeout:               5 * time.Second,
	})
	require.NoError(t, err)

	wait := burst(c, 32, "jti-cold")

	require.Eventually(t, func() bool { return hits.Load() >= 1 }, 5*time.Second, time.Millisecond,
		"the first question never reached the provider")
	time.Sleep(burstSettle)
	release()

	for _, e := range wait() {
		require.NoError(t, e, "every caller in the burst must receive the live verdict")
	}
	assert.Equal(t, int32(1), hits.Load(),
		"32 concurrent first requests for one token cost %d round-trips; a cold start must be shared",
		hits.Load())
}

// A remembered failure must expire. The window exists to stop a stampede, not to
// stop asking: it is time the control is not enforcing, so it must be materially
// shorter than the window we accept for an answer the provider actually gave.
// Two seconds on the injected clock, against a positive TTL of an hour, is the
// observable form of "separate, and shorter".
func TestIntrospection_RememberedFailure_Expires(t *testing.T) {
	srv, hits := failingServer(t, http.StatusBadGateway)

	base := time.Unix(1_700_000_000, 0)
	var mu sync.Mutex
	nowT := base
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return nowT }
	advance := func(d time.Duration) { mu.Lock(); nowT = nowT.Add(d); mu.Unlock() }

	c, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: srv,
		TTL:                   time.Hour,
		Now:                   clock,
	})
	require.NoError(t, err)

	_, e1 := c.Introspect(context.Background(), "jti-window", "raw-token")
	require.Error(t, e1)
	require.Equal(t, int32(1), hits.Load())

	_, _ = c.Introspect(context.Background(), "jti-window", "raw-token")
	require.Equal(t, int32(1), hits.Load(), "inside the window the provider must not be asked again")

	advance(2 * time.Second)
	_, e3 := c.Introspect(context.Background(), "jti-window", "raw-token")
	require.Error(t, e3)
	assert.Equal(t, int32(2), hits.Load(),
		"after the window the question must be asked again; a failure that never expires is a "+
			"revocation check switched off for as long as the process lives")
}

// A wrong address is not a fact about a token. Asking again with a DIFFERENT
// credential cannot produce a different answer, so it is remembered once for the
// process — and, because the verdict refuses rather than passes, widening it that
// way can only refuse more, never let more through.
//
// And it must not stick: a certificate rotated or a path restored on the
// provider's side is repaired without restarting this process, so the refusal has
// to lapse on its own.
func TestIntrospection_WrongAddress_RememberedOnceForTheProcess_AndLapses(t *testing.T) {
	srv, hits := failingServer(t, http.StatusNotFound)

	base := time.Unix(1_700_000_000, 0)
	var mu sync.Mutex
	nowT := base
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return nowT }
	advance := func(d time.Duration) { mu.Lock(); nowT = nowT.Add(d); mu.Unlock() }

	c, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: srv,
		TTL:                   time.Hour,
		Now:                   clock,
	})
	require.NoError(t, err)

	_, e1 := c.Introspect(context.Background(), "jti-first", "raw-token")
	require.ErrorIs(t, e1, middleware.ErrIntrospectionMisconfigured)
	require.Equal(t, int32(1), hits.Load())

	// A different token, never seen before. The address is the same address.
	_, e2 := c.Introspect(context.Background(), "jti-second", "raw-other")
	assert.ErrorIs(t, e2, middleware.ErrIntrospectionMisconfigured,
		"the verdict must not soften: a wrong address still refuses")
	assert.Equal(t, int32(1), hits.Load(),
		"a second token re-asked an address already known to be wrong (%d round-trips); "+
			"the answer cannot depend on which credential is offered", hits.Load())

	advance(2 * time.Second)
	_, e3 := c.Introspect(context.Background(), "jti-third", "raw-third")
	assert.ErrorIs(t, e3, middleware.ErrIntrospectionMisconfigured)
	assert.Equal(t, int32(2), hits.Load(),
		"the refusal never lapsed; an address repaired on the provider's side would need a restart")
}

// The price of the fix must not be paid by the path that works. Distinct tokens
// are distinct questions and must fly together: if sharing were keyed on anything
// coarser than the credential, these calls would serialise and this test would
// never see all of them arrive.
func TestIntrospection_DistinctTokens_AreNotSerialised(t *testing.T) {
	const n = 8
	body, _ := json.Marshal(map[string]any{
		"active": true, "sub": "usr_alice",
		"exp": time.Now().Add(15 * time.Minute).Unix(),
	})

	hits := &atomic.Int32{}
	var arrived sync.WaitGroup
	arrived.Add(n)
	allArrived := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		arrived.Done()
		// Hold until every sibling has arrived. Serialised callers never get here.
		<-allArrived
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	go func() { arrived.Wait(); once.Do(func() { close(allArrived) }) }()

	c, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: srv.URL,
		TTL:                   time.Minute,
		Timeout:               5 * time.Second,
	})
	require.NoError(t, err)

	done := make(chan error, n)
	for i := range n {
		go func() {
			_, ierr := c.Introspect(context.Background(), "jti-distinct-"+string(rune('a'+i)), "raw-token")
			done <- ierr
		}()
	}
	for range n {
		select {
		case e := <-done:
			require.NoError(t, e)
		case <-time.After(5 * time.Second):
			t.Fatalf("distinct tokens serialised behind one another: only %d of %d questions reached the provider",
				hits.Load(), n)
		}
	}
	assert.Equal(t, int32(n), hits.Load(), "each distinct token is its own question")
}

// A live answer the provider actually gave stays usable even after the address
// goes bad. It was a genuine answer when it was given, and it is still inside its
// own window — refusing it would turn a configuration fault into an outage wider
// than the fault itself, and would slow the path that is working.
func TestIntrospection_LiveAnswer_SurvivesTheAddressGoingBad(t *testing.T) {
	good, _ := json.Marshal(map[string]any{
		"active": true, "sub": "usr_alice",
		"exp": time.Now().Add(15 * time.Minute).Unix(),
	})
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(good)
			return
		}
		http.NotFound(w, &http.Request{}) // the address stops serving introspection
	}))
	defer srv.Close()

	c, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: srv.URL,
		TTL:                   time.Hour,
	})
	require.NoError(t, err)

	res, e1 := c.Introspect(context.Background(), "jti-live", "raw-token")
	require.NoError(t, e1)
	require.True(t, res.Active)

	_, e2 := c.Introspect(context.Background(), "jti-other", "raw-other")
	require.ErrorIs(t, e2, middleware.ErrIntrospectionMisconfigured)

	res3, e3 := c.Introspect(context.Background(), "jti-live", "raw-token")
	require.NoError(t, e3, "a live answer inside its own window must still be served")
	assert.True(t, res3.Active)
	assert.Equal(t, int32(2), hits.Load(), "serving a cached live answer must cost no round-trip")
}

// failingServer answers every request with a fixed status, counting arrivals.
func failingServer(t *testing.T, status int) (url string, hits *atomic.Int32) {
	t.Helper()
	hits = &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"no"}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, hits
}
