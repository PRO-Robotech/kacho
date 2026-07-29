// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// introspection_breaker_test.go — what a provider that answers NOBODY costs,
// measured in round-trips.
//
// The sibling stampede tests pin the cost of one token: a burst collapses to one
// question, and a repeat inside the window costs none. Both are keyed on the
// credential, so both are silent about the shape that actually arrives during an
// outage — many DIFFERENT live tokens, each of them a first question, each of
// them missing every per-token memory there is. At N distinct tokens that is N
// held goroutines and N connections aimed at something already unwell, which is
// the stampede the per-token work was meant to remove, wearing a different hat.
//
// These tests therefore count round-trips across DISTINCT tokens. They assert a
// quantity, not that a function ran: "the provider was asked" is true in both the
// fixed and the broken version, and only the number tells them apart.
//
// The three properties pinned here, because a breaker that has any one of them
// missing is worse than none at all:
//
//   - it OPENS — a provider answering nobody is asked a bounded number of times,
//     not once per distinct credential;
//   - it CLOSES BACK — recovery is noticed within a bounded window, by one probe,
//     not by a restart and not "eventually";
//   - it still PASSES — withholding the question must not turn an outage of the
//     identity provider into an outage of this API, and must not quietly acquire
//     the power to refuse, which belongs to the wrong-address verdict alone.
package middleware_test

import (
	"context"
	"encoding/json"
	"fmt"
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

// breakerTrip — how many consecutive unanswered questions the cache is expected
// to spend establishing that the provider is answering nobody. Stated here as
// the test's own expectation rather than imported from the implementation: a
// constant read from the code under test would follow it if it changed, and this
// number is exactly what these tests exist to hold still.
const breakerTrip = 5

// testClock is a hand-driven clock, so "a window later" is a fact the test
// states rather than a duration it sleeps through.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock { return &testClock{t: time.Unix(1_700_000_000, 0)} }

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// flippableServer answers every request with whatever status/body is currently
// set, counting arrivals. It is how "the provider comes back" is expressed
// without restarting a listener out from under a live client's connection pool.
type flippableServer struct {
	hits   atomic.Int32
	mu     sync.Mutex
	status int
	body   string
	URL    string
}

func newFlippableServer(t *testing.T, status int, body string) *flippableServer {
	t.Helper()
	fs := &flippableServer{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fs.hits.Add(1)
		fs.mu.Lock()
		status, body := fs.status, fs.body
		fs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	fs.URL = srv.URL
	return fs
}

func (fs *flippableServer) set(status int, body string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.status, fs.body = status, body
}

func (fs *flippableServer) count() int32 { return fs.hits.Load() }

// liveBody is a well-formed "this token is live" introspection answer.
func liveBody(clock *testClock) string {
	b, _ := json.Marshal(map[string]any{
		"active": true, "sub": "usr_alice",
		"exp": clock.now().Add(15 * time.Minute).Unix(),
	})
	return string(b)
}

// askDistinct puts n never-before-seen tokens to the cache, one after another,
// and returns their errors. Distinct on purpose: every per-token memory in this
// file misses, so what remains is exactly the cost this test is about.
func askDistinct(t *testing.T, c *middleware.IntrospectionCache, tag string, n int) []error {
	t.Helper()
	errs := make([]error, n)
	for i := range n {
		jti := fmt.Sprintf("jti-%s-%d", tag, i)
		_, errs[i] = c.Introspect(context.Background(), jti, "raw-"+jti)
	}
	return errs
}

func newBreakerCache(t *testing.T, url string, clock *testClock) *middleware.IntrospectionCache {
	t.Helper()
	c, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: url,
		TTL:                   time.Hour,
		Now:                   clock.now,
		Timeout:               5 * time.Second,
	})
	require.NoError(t, err)
	return c
}

// The headline number. A provider that answers nobody must be asked a bounded
// number of times per window — not once per distinct credential in flight.
//
// This is the one property no per-token mechanism can provide, and saying so
// precisely matters: the negative cache and the in-flight group are both keyed on
// the token, so forty tokens are forty cold entries and forty separate flights.
// Every one of them holds a request goroutine and a connection for the whole
// per-call budget, against something already struggling.
func TestIntrospection_DeadProvider_DistinctTokens_AskBoundedNumberOfTimes(t *testing.T) {
	const tokens = 40
	clock := newTestClock()
	fs := newFlippableServer(t, http.StatusBadGateway, `{"error":"bad gateway"}`)
	c := newBreakerCache(t, fs.URL, clock)

	errs := askDistinct(t, c, "dead", tokens)

	for i, e := range errs {
		require.Error(t, e, "token %d received no error from a provider that answered nobody", i)
	}
	assert.LessOrEqual(t, fs.count(), int32(breakerTrip),
		"%d distinct tokens cost %d round-trips to a provider that is answering nobody; "+
			"the per-token memories cannot see this shape at all, so the number must be bounded "+
			"by the service-wide breaker (at most %d), not by how many credentials happen to be in flight",
		tokens, fs.count(), breakerTrip)
}

// Withholding the question must not acquire the power to refuse. The soft pass
// is the whole reason the unanswered branch is separate from the wrong-address
// one: an identity provider outage must not take this API down with it.
//
// So while the breaker is open the verdict has to remain the SAME verdict a real
// non-answer produces — not "inactive" (nobody said the token is dead) and not
// "misconfigured" (that one refuses, and no number of unanswered questions is
// evidence about the address).
func TestIntrospection_BreakerOpen_StillPasses(t *testing.T) {
	clock := newTestClock()
	fs := newFlippableServer(t, http.StatusBadGateway, `{"error":"bad gateway"}`)
	c := newBreakerCache(t, fs.URL, clock)

	errs := askDistinct(t, c, "soft", breakerTrip+20)
	asked := fs.count()
	require.LessOrEqual(t, asked, int32(breakerTrip), "breaker never opened (%d round-trips)", asked)

	// The tail — the ones answered by the open breaker rather than by the provider.
	for i, e := range errs[breakerTrip:] {
		require.Error(t, e, "an open breaker must still report that the check did not run (token %d)", i)
		assert.NotErrorIs(t, e, middleware.ErrTokenInactive,
			"a question we chose not to ask is not the provider saying the token is dead")
		assert.NotErrorIs(t, e, middleware.ErrIntrospectionMisconfigured,
			"an unanswered provider is not a wrong address; that verdict refuses service and "+
				"must never be reached by counting silences")
	}
}

// It must close back, and within a bounded window — otherwise the breaker trades
// one outage for a longer one, and a provider that recovered in seconds goes
// unnoticed until something restarts.
//
// The recovery is established by ONE probe, and the test says so with numbers on
// both sides of it: nothing during the cooldown, exactly one when it lapses, and
// full service afterwards.
func TestIntrospection_Breaker_ClosesBackAfterRecovery(t *testing.T) {
	clock := newTestClock()
	fs := newFlippableServer(t, http.StatusBadGateway, `{"error":"bad gateway"}`)
	c := newBreakerCache(t, fs.URL, clock)

	askDistinct(t, c, "trip", breakerTrip+5)
	tripped := fs.count()
	require.LessOrEqual(t, tripped, int32(breakerTrip), "breaker never opened (%d round-trips)", tripped)

	// Still open: distinct tokens inside the cooldown cost nothing.
	askDistinct(t, c, "quiet", 15)
	require.Equal(t, tripped, fs.count(),
		"an open breaker asked anyway: %d round-trips after %d", fs.count()-tripped, tripped)

	// The provider comes back, and the cooldown lapses.
	fs.set(http.StatusOK, liveBody(clock))
	clock.advance(2 * time.Second)

	res, err := c.Introspect(context.Background(), "jti-probe", "raw-probe")
	require.NoError(t, err,
		"the probe after the cooldown must reach a recovered provider and return its answer")
	assert.True(t, res.Active)
	require.Equal(t, tripped+1, fs.count(),
		"recovery must be established by exactly one probe, not by %d", fs.count()-tripped)

	// Closed again: every distinct token is its own question once more.
	const after = 6
	errs := askDistinct(t, c, "healthy", after)
	for i, e := range errs {
		require.NoError(t, e, "token %d was refused by a breaker that should have closed", i)
	}
	assert.Equal(t, tripped+1+int32(after), fs.count(),
		"after recovery the check must be asking again for every token; it asked %d times for %d tokens",
		fs.count()-tripped-1, after)
}

// Half-open must let exactly ONE question through. If the cooldown lapsing
// released every waiting caller at once, a provider that is still down would be
// hit by the whole backlog every cooldown — a stampede on a timer, which is what
// the breaker exists to prevent.
func TestIntrospection_BreakerHalfOpen_ProbesOnceThenClosesAgain(t *testing.T) {
	clock := newTestClock()
	fs := newFlippableServer(t, http.StatusBadGateway, `{"error":"bad gateway"}`)
	c := newBreakerCache(t, fs.URL, clock)

	askDistinct(t, c, "trip", breakerTrip+5)
	tripped := fs.count()
	require.LessOrEqual(t, tripped, int32(breakerTrip))

	// Cooldown lapses, provider still dead, 20 distinct tokens arrive.
	clock.advance(2 * time.Second)
	askDistinct(t, c, "probe-storm", 20)

	assert.Equal(t, tripped+1, fs.count(),
		"a lapsed cooldown let %d questions through; half-open is one probe, otherwise the "+
			"breaker just reschedules the stampede", fs.count()-tripped)
}

// A wrong address must keep refusing, and must not be reachable by counting
// silences. These are the two verdicts that differ in what they DO — one passes,
// one refuses — so the breaker must not blur them in either direction: it must
// not soften a refusal into a pass, and unanswered questions must not accumulate
// into a refusal.
func TestIntrospection_Breaker_DoesNotTouchTheWrongAddressVerdict(t *testing.T) {
	clock := newTestClock()
	fs := newFlippableServer(t, http.StatusNotFound, `not found`)
	c := newBreakerCache(t, fs.URL, clock)

	for i := range breakerTrip + 10 {
		_, err := c.Introspect(context.Background(), fmt.Sprintf("jti-bad-%d", i), "raw")
		require.ErrorIs(t, err, middleware.ErrIntrospectionMisconfigured,
			"token %d: a wrong address must keep refusing; a breaker that softens it into a "+
				"pass switches the control off exactly when it is provably broken", i)
	}
}

// The breaker opens on a provider that is answering NOBODY — consecutively. A
// deployment where one question in five fails is unwell, not absent, and the
// answers it does give are the control still working. Tripping on those would
// stop asking on the strength of intermittent noise.
func TestIntrospection_Breaker_DoesNotTripOnIntermittentFailures(t *testing.T) {
	clock := newTestClock()
	fs := newFlippableServer(t, http.StatusBadGateway, `{"error":"bad gateway"}`)
	c := newBreakerCache(t, fs.URL, clock)

	const rounds = 6
	for i := range rounds {
		// A run of failures that stops one short of tripping, then a real answer.
		askDistinct(t, c, fmt.Sprintf("flap-%d", i), breakerTrip-1)
		fs.set(http.StatusOK, liveBody(clock))
		_, err := c.Introspect(context.Background(), fmt.Sprintf("jti-good-%d", i), "raw-good")
		require.NoError(t, err, "round %d: the provider answered and the answer must be served", i)
		fs.set(http.StatusBadGateway, `{"error":"bad gateway"}`)
	}

	want := int32(rounds * breakerTrip) // (breakerTrip-1) failures + 1 answer, per round
	assert.Equal(t, want, fs.count(),
		"an intermittently failing provider must still be asked: expected %d round-trips, got %d — "+
			"a run of silences broken by a real answer is not a provider that is answering nobody",
		want, fs.count())
}
