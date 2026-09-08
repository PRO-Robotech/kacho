// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// authz_definition_tier_fqn_binding_test.go — the `definition_tier` authz-scope
// override MUST be bound to the FQNs that actually declare the anchor.
//
// The override (redesign-2026 F4) supersedes the catalog's static
// account_id/project_id scope extraction. On the HTTP path it is read out of the
// RAW JSON REQUEST BODY, i.e. from unauthenticated client input, BEFORE
// grpc-gateway decodes (and silently drops) unknown keys. Applied without an FQN
// binding, ANY JSON-bodied RPC could be re-scoped by the caller:
//
//	POST /iam/v1/users:invite
//	{"accountId":"acc_victim","definitionTier":{"tierType":"iam.project",
//	 "tierId":"prj_attacker"}}
//
// → the gateway would Check `editor@project:prj_attacker` (which the attacker
// holds) instead of `editor@account:acc_victim`, and the backend — which drops
// the unknown `definitionTier` key — would still invite into the VICTIM account.
// A caller-chosen authz scope is a BOLA/privilege-escalation primitive
// (security.md §object-scoped authz).
//
// Contract locked here: only the RPCs whose request message declares
// `definition_tier` (today exactly RoleService/Create) may have their scope
// resolved from it; every other FQN keeps the catalog scope regardless of what
// the body contains.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// inviteEntry — catalog row for UserService/Invite: account-scoped via the
// request's `account_id`. It declares NO definition_tier anchor (the request
// message has no such field).
const inviteEntry = `{"fqn":"kaname.cloud.iam.v1.UserService/Invite","permission":"iam.users.invite","required_relation":"editor","scope_extractor":{"object_type":"account","from_request_field":"account_id"},"required_acr_min":"2","risk_level":"HIGH"}`

// roleCreateEntry — catalog row for RoleService/Create: the ONE RPC whose
// request message carries `definition_tier` (the canonical scope anchor).
const roleCreateEntry = `{"fqn":"kaname.cloud.iam.v1.RoleService/Create","permission":"iam.roles.create","required_relation":"editor","scope_extractor":{"object_type":"account","from_request_field":"account_id"},"required_acr_min":"1","risk_level":"HIGH"}`

// TestAuthz_HTTP_DefinitionTierInBody_DoesNotRedirectScope_UnrelatedFQN — the
// escalation primitive: a `definitionTier` object smuggled into the body of an
// RPC that does not declare the anchor must NOT move the FGA Check off the
// catalog scope.
func TestAuthz_HTTP_DefinitionTierInBody_DoesNotRedirectScope_UnrelatedFQN(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	router := &fakeRestRouter{m: map[string]string{
		"POST /iam/v1/users:invite": "kaname.cloud.iam.v1.UserService/Invite",
	}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, inviteEntry), checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = router
	})
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	body := `{"accountId":"acc_victim","email":"x@example.com",` +
		`"definitionTier":{"tierType":"iam.project","tierId":"prj_attacker"}}`
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/users:invite", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Kacho-Principal-Id", "usr_attacker")
	r.Header.Set("X-Kacho-Principal-Type", "user")
	r.Header.Set("X-Kacho-Token-Acr", "2")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	in := checker.lastInput.Load()
	require.NotNil(t, in, "checker must be called")
	assert.Equal(t, "account", in.ResourceType,
		"client-supplied definitionTier must NOT re-type the authz scope of an RPC that does not declare the anchor")
	assert.Equal(t, "acc_victim", in.ResourceID,
		"client-supplied definitionTier must NOT re-point the authz scope to a caller-chosen id")
}

// TestAuthz_HTTP_DefinitionTierInBody_UnrelatedFQN_StillDeniesOnCatalogScope —
// the observable end-to-end effect: with the Check denying on the CATALOG scope,
// the smuggled anchor must not turn the request into a 200.
func TestAuthz_HTTP_DefinitionTierInBody_UnrelatedFQN_StillDeniesOnCatalogScope(t *testing.T) {
	checker := &fakeChecker{allowed: false, reasons: []string{"no path"}}
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

	body := `{"accountId":"acc_victim","email":"x@example.com",` +
		`"definitionTier":{"tierType":"iam.project","tierId":"prj_attacker"}}`
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/users:invite", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Kacho-Principal-Id", "usr_attacker")
	r.Header.Set("X-Kacho-Principal-Type", "user")
	r.Header.Set("X-Kacho-Token-Acr", "2")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.False(t, served, "handler must not be reached on a denied scope")
	assert.Equal(t, http.StatusForbidden, w.Code)
	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "account:acc_victim", in.ResourceType+":"+in.ResourceID,
		"the denied Check must have run against the catalog scope, not the smuggled anchor")
}

// TestAuthz_HTTP_RoleCreate_DefinitionTierResolvesScope — the LEGITIMATE F4 path
// stays intact: RoleService/Create declares the anchor, so its body-carried
// definitionTier still supersedes the legacy account_id extraction.
func TestAuthz_HTTP_RoleCreate_DefinitionTierResolvesScope(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	router := &fakeRestRouter{m: map[string]string{
		"POST /iam/v1/roles": "kaname.cloud.iam.v1.RoleService/Create",
	}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, roleCreateEntry), checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = router
	})
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	body := `{"name":"reader","definitionTier":{"tierType":"iam.project","tierId":"prj_beta"}}`
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/roles", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Kacho-Principal-Id", "usr_author")
	r.Header.Set("X-Kacho-Principal-Type", "user")
	r.Header.Set("X-Kacho-Token-Acr", "2")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "project", in.ResourceType, "F4 anchor must still re-type the scope for RoleService/Create")
	assert.Equal(t, "prj_beta", in.ResourceID, "F4 anchor must still re-point the scope for RoleService/Create")
}

// TestAuthz_GRPC_RoleCreate_DefinitionTierResolvesScope — same for the typed
// gRPC path.
func TestAuthz_GRPC_RoleCreate_DefinitionTierResolvesScope(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := buildAuthzMiddleware(t, buildCatalog(t, roleCreateEntry), checker)
	_, err := mw.Unary()(withTokenMD("usr_author", "user"),
		&iamv1.CreateRoleRequest{
			Name:           "reader",
			DefinitionTier: &iamv1.DefinitionTier{TierType: "iam.account", TierId: "acc_alpha"},
		},
		&grpc.UnaryServerInfo{FullMethod: "/kaname.cloud.iam.v1.RoleService/Create"},
		func(ctx context.Context, req any) (any, error) { return "ok", nil })
	require.NoError(t, err)

	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "account", in.ResourceType)
	assert.Equal(t, "acc_alpha", in.ResourceID)
}

// TestAuthz_GRPC_DefinitionTierBearingMessage_OnUnrelatedFQN_KeepsCatalogScope —
// proto-path arm of the same binding: even a request message that DOES carry the
// anchor must not move the scope when it arrives on an FQN that does not declare
// it (defence in depth for any future message that gains the field).
func TestAuthz_GRPC_DefinitionTierBearingMessage_OnUnrelatedFQN_KeepsCatalogScope(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	// Catalog row for a DIFFERENT FQN, account-scoped from account_id.
	const otherEntry = `{"fqn":"kaname.cloud.iam.v1.RoleService/CreateLike","permission":"iam.roles.create","required_relation":"editor","scope_extractor":{"object_type":"account","from_request_field":"account_id"},"required_acr_min":"1","risk_level":"HIGH"}`
	mw := buildAuthzMiddleware(t, buildCatalog(t, otherEntry), checker)
	_, err := mw.Unary()(withTokenMD("usr_attacker", "user"),
		&iamv1.CreateRoleRequest{
			Name:           "reader",
			AccountId:      "acc_victim",
			DefinitionTier: &iamv1.DefinitionTier{TierType: "iam.project", TierId: "prj_attacker"},
		},
		&grpc.UnaryServerInfo{FullMethod: "/kaname.cloud.iam.v1.RoleService/CreateLike"},
		func(ctx context.Context, req any) (any, error) { return "ok", nil })
	require.NoError(t, err)

	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, "account", in.ResourceType,
		"anchor must not re-type the scope on an FQN that does not declare it")
	assert.Equal(t, "acc_victim", in.ResourceID,
		"anchor must not re-point the scope on an FQN that does not declare it")
}
