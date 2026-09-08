// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// authz_role_legacy_project_scope_test.go — RoleService/Create is account- XOR
// project-scoped, and the proto keeps the LEGACY account_id/project_id pair
// accepted for back-compat when `definition_tier` is omitted
// (role_service.proto CreateRoleRequest.definition_tier: "the legacy
// account_id/project_id are still accepted for back-compat"). The iam use-case
// honours it (create.go XOR switch + DB CHECK roles_definition_tier_xor).
//
// The gateway's catalog scope_extractor, however, names ONE field —
// `account_id` — so a legacy PROJECT-scoped Create (account_id empty,
// project_id set) extracted to the `account:*` wildcard, which
// AuthorizeService.Check rejects as "no path: unscoped resource" → 403 BEFORE
// the backend ever saw the request. The contract promised a path the edge made
// unreachable.
//
// Locked here: the legacy `project_id` alternative resolves the authz scope to
// `project:<project_id>` — the SAME anchor the backend acts on (anti-BOLA parity
// with the account arm) — on both the typed gRPC and the raw-JSON REST arm, and
// ONLY for the FQNs that declare it. Precedence: definition_tier > account_id >
// project_id.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// unscopedDenyChecker mirrors the real iam AuthorizeService.Check behaviour that
// produced the reported 403: a wildcard resource id is rejected outright ("no
// path: unscoped resource"), every concrete scope is allowed. It turns the
// scope-resolution bug into an OBSERVABLE status code instead of a mere argument
// assertion (testing.md — regression-lock on the observable).
type unscopedDenyChecker struct{ last *middleware.AuthzCheckInput }

func (c *unscopedDenyChecker) Check(_ context.Context, in middleware.AuthzCheckInput) (middleware.AuthzCheckResult, error) {
	cp := in
	c.last = &cp
	if in.ResourceID == "*" || in.ResourceID == "" {
		return middleware.AuthzCheckResult{
			Allowed:     false,
			DenyReasons: []string{"no path: unscoped resource"},
			CheckedAt:   time.Now(),
		}, nil
	}
	return middleware.AuthzCheckResult{Allowed: true, CheckedAt: time.Now()}, nil
}

func roleCreateHTTPMiddleware(t *testing.T, checker middleware.AuthorizeChecker) http.Handler {
	t.Helper()
	router := &fakeRestRouter{m: map[string]string{
		"POST /iam/v1/roles": "kaname.cloud.iam.v1.RoleService/Create",
	}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, roleCreateEntry), checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = router
	})
	return mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
}

func roleCreateRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/roles", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Kacho-Principal-Id", "usr_author")
	r.Header.Set("X-Kacho-Principal-Type", "user")
	r.Header.Set("X-Kacho-Token-Acr", "2")
	return r
}

// TestAuthz_HTTP_RoleCreate_LegacyProjectIdResolvesScope — the REST arm: a
// legacy project-scoped Create must Check `project:<project_id>`, not the
// `account:*` wildcard.
func TestAuthz_HTTP_RoleCreate_LegacyProjectIdResolvesScope(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	h := roleCreateHTTPMiddleware(t, checker)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, roleCreateRequest(`{"name":"reader","projectId":"prj_beta"}`))

	require.Equal(t, http.StatusOK, w.Code)
	in := checker.lastInput.Load()
	require.NotNil(t, in, "checker must be called")
	assert.Equal(t, "project", in.ResourceType,
		"legacy project-scoped Role.Create must Check the project object type")
	assert.Equal(t, "prj_beta", in.ResourceID,
		"legacy project-scoped Role.Create must Check the project the backend will act on, not a wildcard")
}

// TestAuthz_HTTP_RoleCreate_LegacyProjectId_NotDeniedAsUnscoped — the reported
// symptom, at the observable level: with the real "unscoped → no path" checker
// the request must reach the backend (200), not be 403'd at the edge.
func TestAuthz_HTTP_RoleCreate_LegacyProjectId_NotDeniedAsUnscoped(t *testing.T) {
	checker := &unscopedDenyChecker{}
	served := false
	router := &fakeRestRouter{m: map[string]string{
		"POST /iam/v1/roles": "kaname.cloud.iam.v1.RoleService/Create",
	}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, roleCreateEntry), checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = router
	})
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, roleCreateRequest(`{"name":"reader","projectId":"prj_beta"}`))

	assert.Equal(t, http.StatusOK, w.Code,
		"the legacy project_id back-compat promise must be reachable through the gateway")
	assert.True(t, served, "the backend must be reached — the scope is resolvable, not unscoped")
}

// TestAuthz_HTTP_RoleCreate_LegacyProjectId_StillEnforced — the fix must not
// become a bypass: a deny on the resolved project scope still blocks.
func TestAuthz_HTTP_RoleCreate_LegacyProjectId_StillEnforced(t *testing.T) {
	checker := &fakeChecker{allowed: false, reasons: []string{"no path"}}
	served := false
	router := &fakeRestRouter{m: map[string]string{
		"POST /iam/v1/roles": "kaname.cloud.iam.v1.RoleService/Create",
	}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, roleCreateEntry), checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = router
	})
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, roleCreateRequest(`{"name":"reader","projectId":"prj_beta"}`))

	assert.False(t, served, "a denied project scope must not reach the backend")
	assert.Equal(t, http.StatusForbidden, w.Code)
	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "project:prj_beta", in.ResourceType+":"+in.ResourceID,
		"the deny must have been decided on the resolved project scope")
}

// TestAuthz_RoleCreate_NoAnchor_StaysUnscopedDeny — the third arm of the scope
// contract, locked because role_service.proto now states it: with NEITHER
// definition_tier NOR account_id NOR project_id there is nothing to authorize
// against, so the edge fail-closes on `account:*` ("no path: unscoped
// resource"). The use-case's XOR INVALID_ARGUMENT is therefore NOT observable
// for the empty-scope input — a caller sees 403, not 400. Anything else here
// would mean the gateway had started authorizing an unscoped mutation.
func TestAuthz_RoleCreate_NoAnchor_StaysUnscopedDeny(t *testing.T) {
	checker := &unscopedDenyChecker{}
	served := false
	router := &fakeRestRouter{m: map[string]string{
		"POST /iam/v1/roles": "kaname.cloud.iam.v1.RoleService/Create",
	}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, roleCreateEntry), checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = router
	})
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, roleCreateRequest(`{"name":"noscope"}`))

	assert.False(t, served, "an unscoped mutation must not reach the backend")
	assert.Equal(t, http.StatusForbidden, w.Code,
		"no anchor → account:* → fail-closed 403; the XOR 400 is not reachable for this input")
	require.NotNil(t, checker.last)
	assert.Equal(t, "account:*", checker.last.ResourceType+":"+checker.last.ResourceID)
}

// TestAuthz_GRPC_RoleCreate_LegacyProjectIdResolvesScope — typed gRPC arm.
func TestAuthz_GRPC_RoleCreate_LegacyProjectIdResolvesScope(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := buildAuthzMiddleware(t, buildCatalog(t, roleCreateEntry), checker)
	_, err := mw.Unary()(withTokenMD("usr_author", "user"),
		&iamv1.CreateRoleRequest{Name: "reader", ProjectId: "prj_beta"},
		&grpc.UnaryServerInfo{FullMethod: "/kaname.cloud.iam.v1.RoleService/Create"},
		func(ctx context.Context, req any) (any, error) { return "ok", nil })
	require.NoError(t, err)

	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "project", in.ResourceType)
	assert.Equal(t, "prj_beta", in.ResourceID)
}

// TestAuthz_RoleCreate_AccountIdWins_WhenBothScopesSet — the XOR violation is the
// BACKEND's to reject (INVALID_ARGUMENT). The edge must keep checking the
// catalog's account scope, so the caller cannot dodge the account check by
// appending a project they happen to own.
func TestAuthz_RoleCreate_AccountIdWins_WhenBothScopesSet(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := buildAuthzMiddleware(t, buildCatalog(t, roleCreateEntry), checker)
	_, err := mw.Unary()(withTokenMD("usr_attacker", "user"),
		&iamv1.CreateRoleRequest{Name: "reader", AccountId: "acc_victim", ProjectId: "prj_attacker"},
		&grpc.UnaryServerInfo{FullMethod: "/kaname.cloud.iam.v1.RoleService/Create"},
		func(ctx context.Context, req any) (any, error) { return "ok", nil })
	require.NoError(t, err)

	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "account:acc_victim", in.ResourceType+":"+in.ResourceID,
		"project_id must never displace a present account_id scope")
}

// TestAuthz_RoleCreate_AccountIdWins_WhenBothScopesSet_HTTP — REST arm of the same
// precedence rule (this is the arm that reads raw, undecoded client JSON).
func TestAuthz_RoleCreate_AccountIdWins_WhenBothScopesSet_HTTP(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	h := roleCreateHTTPMiddleware(t, checker)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, roleCreateRequest(`{"name":"reader","accountId":"acc_victim","projectId":"prj_attacker"}`))

	require.Equal(t, http.StatusOK, w.Code)
	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "account:acc_victim", in.ResourceType+":"+in.ResourceID,
		"project_id must never displace a present account_id scope")
}

// TestAuthz_RoleCreate_DefinitionTierWins_OverLegacyProjectId — the proto
// precedence ("when definition_tier is set it takes precedence") holds against
// the legacy field too, not just against account_id.
func TestAuthz_RoleCreate_DefinitionTierWins_OverLegacyProjectId(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := buildAuthzMiddleware(t, buildCatalog(t, roleCreateEntry), checker)
	_, err := mw.Unary()(withTokenMD("usr_author", "user"),
		&iamv1.CreateRoleRequest{
			Name:           "reader",
			ProjectId:      "prj_legacy",
			DefinitionTier: &iamv1.DefinitionTier{TierType: "iam.account", TierId: "acc_alpha"},
		},
		&grpc.UnaryServerInfo{FullMethod: "/kaname.cloud.iam.v1.RoleService/Create"},
		func(ctx context.Context, req any) (any, error) { return "ok", nil })
	require.NoError(t, err)

	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "account:acc_alpha", in.ResourceType+":"+in.ResourceID,
		"definition_tier must take precedence over the legacy project_id")
}

// TestAuthz_RoleCreate_DefinitionTierWins_OverLegacyProjectId_HTTP — REST arm.
func TestAuthz_RoleCreate_DefinitionTierWins_OverLegacyProjectId_HTTP(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	h := roleCreateHTTPMiddleware(t, checker)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, roleCreateRequest(
		`{"name":"reader","projectId":"prj_legacy","definitionTier":{"tierType":"iam.account","tierId":"acc_alpha"}}`))

	require.Equal(t, http.StatusOK, w.Code)
	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "account:acc_alpha", in.ResourceType+":"+in.ResourceID,
		"definition_tier must take precedence over the legacy project_id")
}

// TestAuthz_HTTP_ProjectIdInBody_DoesNotRedirectScope_UnrelatedFQN — the SECURITY
// binding, same class as the definition_tier one: `projectId` is read from raw,
// unauthenticated client JSON before grpc-gateway drops unknown keys. Smuggled
// into an RPC that does not declare the legacy pair, it must NOT move the Check
// off the catalog scope (a caller-chosen authz scope is a BOLA primitive —
// security.md §object-scoped authz).
func TestAuthz_HTTP_ProjectIdInBody_DoesNotRedirectScope_UnrelatedFQN(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	router := &fakeRestRouter{m: map[string]string{
		"POST /iam/v1/users:invite": "kaname.cloud.iam.v1.UserService/Invite",
	}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, inviteEntry), checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = router
	})
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	body := `{"accountId":"acc_victim","email":"x@example.com","projectId":"prj_attacker"}`
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/users:invite", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Kacho-Principal-Id", "usr_attacker")
	r.Header.Set("X-Kacho-Principal-Type", "user")
	r.Header.Set("X-Kacho-Token-Acr", "2")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "account:acc_victim", in.ResourceType+":"+in.ResourceID,
		"client-supplied projectId must not re-point the authz scope of an RPC that does not declare it")
}

// TestAuthz_HTTP_ProjectIdInBody_UnrelatedFQN_StillDeniesOnCatalogScope — the
// observable end of the same binding: the smuggled field must not turn a deny
// into a 200.
func TestAuthz_HTTP_ProjectIdInBody_UnrelatedFQN_StillDeniesOnCatalogScope(t *testing.T) {
	checker := &unscopedDenyChecker{}
	router := &fakeRestRouter{m: map[string]string{
		"POST /iam/v1/users:invite": "kaname.cloud.iam.v1.UserService/Invite",
	}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, inviteEntry), checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = router
	})
	served := false
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	}))

	// No accountId → the catalog scope stays the `account:*` wildcard, which the
	// checker denies. The smuggled projectId must not rescue it.
	body := `{"email":"x@example.com","projectId":"prj_attacker"}`
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/users:invite", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Kacho-Principal-Id", "usr_attacker")
	r.Header.Set("X-Kacho-Principal-Type", "user")
	r.Header.Set("X-Kacho-Token-Acr", "2")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.False(t, served, "an unscoped Invite must stay denied")
	assert.Equal(t, http.StatusForbidden, w.Code)
	require.NotNil(t, checker.last)
	assert.Equal(t, "account:*", checker.last.ResourceType+":"+checker.last.ResourceID,
		"the deny must have been decided on the catalog scope, not on the smuggled projectId")
}

// TestAuthz_GRPC_ProjectIdBearingMessage_OnUnrelatedFQN_KeepsCatalogScope — proto
// arm of the FQN binding (defence in depth for any future message that gains the
// legacy pair).
func TestAuthz_GRPC_ProjectIdBearingMessage_OnUnrelatedFQN_KeepsCatalogScope(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	const otherEntry = `{"fqn":"kaname.cloud.iam.v1.RoleService/CreateLike","permission":"iam.roles.create","required_relation":"editor","scope_extractor":{"object_type":"account","from_request_field":"account_id"},"required_acr_min":"1","risk_level":"HIGH"}`
	mw := buildAuthzMiddleware(t, buildCatalog(t, otherEntry), checker)
	_, err := mw.Unary()(withTokenMD("usr_attacker", "user"),
		&iamv1.CreateRoleRequest{Name: "reader", ProjectId: "prj_attacker"},
		&grpc.UnaryServerInfo{FullMethod: "/kaname.cloud.iam.v1.RoleService/CreateLike"},
		func(ctx context.Context, req any) (any, error) { return "ok", nil })
	require.NoError(t, err)

	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "account:*", in.ResourceType+":"+in.ResourceID,
		"project_id must not re-scope an FQN that does not declare the legacy pair")
}
