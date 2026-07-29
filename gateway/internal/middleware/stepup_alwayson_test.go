// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// stepup_alwayson_test.go — the per-RPC authentication floor has to be applied by
// the layer EVERY request passes through, and the token context it depends on has
// to be written there too.
//
// Both used to live only in the sender-constrained-token middleware, which mounts
// behind a feature toggle. A toggle is a legitimate home for verifying a proof of
// possession — that is a property of SOME tokens. Whether the caller
// authenticated strongly enough for THIS call is a property of every token, and
// the same is true of the token context the cluster-internal arm reads: with a
// single producer inside an unmounted middleware, the header never left the
// gateway, so the internal floor evaluated an absent value on every request.
//
// The neighbouring stepup_wiring_test.go drives the middleware directly and
// proves the gate CAN fire. That is a different statement from "it runs", and the
// difference is exactly what went unnoticed. These cases drive the always-mounted
// authN layer instead.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// alwaysOnAuth wires the authN interceptor the way the composition root must:
// a JWKS verifier plus the step-up arm backed by the embedded catalog and the
// generated REST route table.
func alwaysOnAuth(t *testing.T, fix *jwksFixture) *middleware.AuthInterceptor {
	t.Helper()
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	return middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "",
		&countingLookup{subj: middleware.Subject{Type: "user", ID: "should-not-be-used"}},
		authTestLogger(),
	).
		WithVerifier(rs256Verifier(t, fix)).
		WithStepUp(
			middleware.NewStepUpGate(nil),
			middleware.NewCatalogPermissionLookup(catalog),
			middleware.NewRestRouter(),
		)
}

// alwaysOnClaims — a Hydra-shaped human token at the given assurance level.
func alwaysOnClaims(acr string) jwt.MapClaims {
	c := hydraClaims("user", "usr_alice_acc_a1b2")
	c["acr"] = acr
	return c
}

// serveREST runs one request through the interceptor's HTTP arm and reports what
// the backend saw.
func serveREST(t *testing.T, auth *middleware.AuthInterceptor, method, url, token string) (*httptest.ResponseRecorder, *http.Request, bool) {
	t.Helper()
	var seen *http.Request
	hit := false
	handler := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		seen = r
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(method, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, seen, hit
}

// callUnary runs one call through the interceptor's gRPC arm.
func callUnary(t *testing.T, auth *middleware.AuthInterceptor, fullMethod, token string) error {
	t.Helper()
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer "+token))
	_, err := auth.Unary()(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: fullMethod},
		func(context.Context, any) (any, error) { return nil, nil })
	return err
}

// A credential-mint RPC declares the raised floor. A human token below it must be
// refused by the always-mounted layer, with the challenge that tells the client
// which ceremony to run.
func TestStepUpAlwaysOn_SensitiveRPC_BelowFloor_Refused(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	rec, _, hit := serveREST(t, auth, http.MethodPost,
		"https://api.kacho.cloud/iam/v1/users/usr-abc/tokens",
		fix.sign(t, alwaysOnClaims("1")))

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"the floor must be applied by the layer every request passes through, not by an unmounted middleware")
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "insufficient_user_authentication")
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `acr_values="2"`)
	assert.False(t, hit, "the backend must not be reached")
}

// Routine resource work is not a posture change: the same token passes, and the
// token context travels on so the cluster-internal arm has something to read.
func TestStepUpAlwaysOn_RoutineRPC_Passes_AndForwardsTokenContext(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	rec, seen, hit := serveREST(t, auth, http.MethodPost,
		"https://api.kacho.cloud/vpc/v1/networks",
		fix.sign(t, alwaysOnClaims("1")))

	require.True(t, hit, "routine resource work carries no raised floor")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "1", seen.Header.Get("X-Kacho-Token-Acr"),
		"the internal floor reads this header; with no producer that runs, it evaluates an absent value forever")
	assert.Equal(t, "1", seen.Header.Get("Grpc-Metadata-X-Kacho-Token-Acr"))
}

// A machine has no interactive ceremony and can never present one. The shared
// rule exempts it; the always-mounted arm must not re-derive a stricter one.
func TestStepUpAlwaysOn_MachinePrincipal_Exempt(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	claims := hydraClaims("service_account", "sva_deployer_a1b2")
	delete(claims, "acr")

	rec, _, hit := serveREST(t, auth, http.MethodPost,
		"https://api.kacho.cloud/iam/v1/users/usr-abc/tokens",
		fix.sign(t, claims))

	assert.True(t, hit, "a service principal is exempt from the interactive floor")
	assert.Equal(t, http.StatusOK, rec.Code)
}

// The native gRPC surface is the same edge. It resolves the method directly, so
// there is no routing excuse for leaving it unguarded.
func TestStepUpAlwaysOn_GRPC_SensitiveRPC_BelowFloor_Refused(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	err := callUnary(t, auth, "/kacho.cloud.iam.v1.UserTokenService/Issue",
		fix.sign(t, alwaysOnClaims("1")))
	require.Error(t, err, "the native surface must apply the same floor")
	assert.Contains(t, err.Error(), "insufficient_user_authentication")
}

// And the same token reaches a routine method on that surface.
func TestStepUpAlwaysOn_GRPC_RoutineRPC_Passes(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	err := callUnary(t, auth, "/kacho.cloud.vpc.v1.NetworkService/Create",
		fix.sign(t, alwaysOnClaims("1")))
	require.NoError(t, err)
}
