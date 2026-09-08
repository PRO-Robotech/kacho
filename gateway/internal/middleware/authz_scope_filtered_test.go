// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// authz_scope_filtered_test.go — the third lane a catalog row can declare.
//
// A scope-filtered RPC is one whose authorization cannot be reduced to a single
// (relation, object) pair before the call runs: the caller names the inputs and
// the answer concerns many objects, each with its own owner. The owning service
// reads its page and asks the model per element. The edge therefore authenticates
// and runs NO Check.
//
// The whole point of giving that lane its own token — instead of reusing
// `<exempt>` — is that `<exempt>` carries a SECOND meaning: an Internal* RPC that
// arrives on the cluster-internal listener is admitted with no principal extracted
// at all, on the strength of network position. The tests below pin both halves:
// no Check runs, and the principal is still required — including on the internal
// listener, where `<exempt>` would have waived it.

const (
	scopeFilteredEntry = `{"fqn":"kacho.cloud.vpc.v1.NetworkService/List","permission":"vpc.networks.list","scope_filtered":true,"required_acr_min":"1"}`
	// The same FQN in both lanes, so the internal-listener contrast below compares
	// like with like.
	internalScopeFiltered = `{"fqn":"kaname.cloud.iam.v1.InternalIAMService/Check","permission":"iam.internal.check","scope_filtered":true,"required_acr_min":"1"}`
)

// TestScopeFiltered_CatalogEntryDecodes — the runtime catalog must carry the lane
// off the wire, and must not confuse it with `<exempt>`: the two take different
// paths through the decision pipeline.
func TestScopeFiltered_CatalogEntryDecodes(t *testing.T) {
	cat := buildCatalog(t, scopeFilteredEntry)
	entry, ok := cat.Lookup("kacho.cloud.vpc.v1.NetworkService/List")
	require.True(t, ok, "the row must be present")
	assert.True(t, entry.ScopeFiltered, "scope_filtered must decode off the catalog JSON")
	assert.False(t, entry.IsExempt(), "a scope-filtered row is NOT exempt — it keeps a real permission")
	assert.Equal(t, "vpc.networks.list", entry.Permission)
	assert.Equal(t, "1", entry.RequiredACRMin, "the step-up floor still applies")
}

// TestScopeFiltered_Authenticated_AllowsWithoutCheck — an authenticated caller
// reaches the handler and NO authorization Check is made. Asserting the call
// COUNT is the load-bearing part: a Check that happens to pass would look
// identical from the outside while re-introducing exactly the always-true
// question this lane replaces.
func TestScopeFiltered_Authenticated_AllowsWithoutCheck(t *testing.T) {
	checker := &fakeChecker{allowed: false} // must never be consulted
	mw := buildAuthzMiddleware(t, buildCatalog(t, scopeFilteredEntry), checker)

	called := false
	handler := func(ctx context.Context, req any) (any, error) { called = true; return "ok", nil }

	_, err := mw.Unary()(withTokenMD("usr_scopefiltered", "user"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/List"},
		handler)

	require.NoError(t, err)
	assert.True(t, called, "an authenticated caller must reach the owning service, which does the narrowing")
	assert.Equal(t, int64(0), checker.calls.Load(),
		"a scope-filtered RPC must ask the model NOTHING at the edge — there is no single object to ask about")
}

// TestScopeFiltered_Unauthenticated_Rejected — a service-side per-object filter is
// meaningless without a principal: with no credentials the caller gets
// Unauthenticated, not an empty page.
func TestScopeFiltered_Unauthenticated_Rejected(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := buildAuthzMiddleware(t, buildCatalog(t, scopeFilteredEntry), checker)

	called := false
	handler := func(ctx context.Context, req any) (any, error) { called = true; return "ok", nil }

	_, err := mw.Unary()(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/List"},
		handler)

	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err),
		"no credentials on a scope-filtered RPC is an authentication failure, not a denial")
	assert.False(t, called, "the owning service must not be reached without a principal")
}

// TestScopeFiltered_InternalOrigin_StillRequiresPrincipal — the delta against
// `<exempt>`, stated as behaviour.
//
// `<exempt>` admits an Internal* RPC on the cluster-internal listener WITHOUT
// extracting a principal, because the internal callers of those RPCs (gateway
// self-call, drainer, port-forward admin) carry no user token. That admission
// rests on network position, which is not a credential — anything that can reach
// the internal port or hold a port-forward gets in. A scope-filtered RPC must not
// inherit it: its narrowing is per-principal, so an absent principal is a failure.
func TestScopeFiltered_InternalOrigin_StillRequiresPrincipal(t *testing.T) {
	newMW := func(entry string) (*middleware.AuthzMiddleware, *fakeChecker) {
		checker := &fakeChecker{allowed: true}
		rr := middleware.NewRestRouter()
		mw := buildAuthzMiddleware(t, buildCatalog(t, entry), checker, func(c *middleware.AuthzMiddlewareConfig) {
			c.RestRouter = rr
			c.Resources = middleware.NewResourceExtractor(rr.PathTemplates())
		})
		return mw, checker
	}

	serve := func(mw *middleware.AuthzMiddleware) (int, bool) {
		reached := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = true
			w.WriteHeader(http.StatusOK)
		})
		r := httptest.NewRequest(http.MethodPost, "/iam/v1/internal/iam:check", nil)
		r = r.WithContext(listenerorigin.WithInternal(r.Context()))
		w := httptest.NewRecorder()
		mw.HTTP(next).ServeHTTP(w, r)
		return w.Code, reached
	}

	// Baseline — the `<exempt>` lane admits the anonymous internal caller. This is
	// the existing, deliberate behaviour; it is asserted here so the contrast below
	// cannot silently become a tautology if that behaviour ever changes.
	exemptMW, _ := newMW(internalCheckExempt)
	codeExempt, reachedExempt := serve(exemptMW)
	assert.Equal(t, http.StatusOK, codeExempt, "<exempt> Internal* on the internal listener is admitted with no principal")
	assert.True(t, reachedExempt)

	// The lane under test — same FQN, same listener, no principal — is refused.
	sfMW, sfChecker := newMW(internalScopeFiltered)
	codeSF, reachedSF := serve(sfMW)
	assert.Equal(t, http.StatusUnauthorized, codeSF,
		"a scope-filtered Internal* RPC must NOT inherit the exempt network-position admission")
	assert.False(t, reachedSF, "the owning service must not be reached without a principal")
	assert.Equal(t, int64(0), sfChecker.calls.Load(), "and no Check is made either way")
}
