// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// The subject of the authorization check must be the subject of the action.
//
// Every Kachō REST route that carries a body binds the WHOLE body
// (`google.api.http { post: "…" body: "*" }`), and for that binding grpc-gateway
// builds the handler's request message from the body alone — it does not read
// query parameters. So on a body-bearing route the query string names nothing the
// handler will ever act on, and reading the authorization scope from it would gate
// the call on one project while the handler operated on another.
//
// These tests pin the property at the level where it is observable: what scope the
// edge actually asks the model about, and what a self-contradicting request gets
// back. The unit-level source rules live in resource_extractor_test.go.

// networkCreateEntry mirrors the embedded catalog row for the canonical
// body-bearing create: scope `project:<project_id>`, and `project_id` is a body
// field (it appears in no path placeholder of `POST /vpc/v1/networks`).
const networkCreateEntry = `{"fqn":"kacho.cloud.vpc.v1.NetworkService/Create","permission":"vpc.networks.create","required_relation":"editor","scope_extractor":{"object_type":"project","from_request_field":"project_id"},"required_acr_min":"1"}`

// networkListEntry is the sibling read: no body at all, so `project_id` legitimately
// arrives as a query parameter and the query IS the source grpc-gateway uses.
const networkListEntry = `{"fqn":"kacho.cloud.vpc.v1.NetworkService/List","permission":"vpc.networks.list","required_relation":"v_list","scope_extractor":{"object_type":"project","from_request_field":"project_id"},"required_acr_min":"1"}`

const (
	// scopeCallerCanEdit — a project the caller is an editor of.
	scopeCallerCanEdit = "prj0000000000000cal"
	// scopeHandlerActsOn — the project named in the body, i.e. the one the
	// handler would create the resource in.
	scopeHandlerActsOn = "prj0000000000000act"
)

// scopeSourceMW builds the REST-arm middleware for POST/GET /vpc/v1/networks.
func scopeSourceMW(t *testing.T, checker middleware.AuthorizeChecker) *middleware.AuthzMiddleware {
	t.Helper()
	router := &fakeRestRouter{m: map[string]string{
		"POST /vpc/v1/networks": "kacho.cloud.vpc.v1.NetworkService/Create",
		"GET /vpc/v1/networks":  "kacho.cloud.vpc.v1.NetworkService/List",
	}}
	return buildAuthzMiddleware(t, buildCatalog(t, networkCreateEntry, networkListEntry), checker,
		func(c *middleware.AuthzMiddlewareConfig) {
			c.RestRouter = router
			c.Resources = middleware.NewResourceExtractor(map[string]string{
				"kacho.cloud.vpc.v1.NetworkService/Create": "/vpc/v1/networks",
				"kacho.cloud.vpc.v1.NetworkService/List":   "/vpc/v1/networks",
			})
		})
}

func scopeSourceReq(method, url, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, url, nil)
	} else {
		r = httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("X-Kacho-Principal-Id", "usr_scope_source")
	r.Header.Set("X-Kacho-Principal-Type", "user")
	r.Header.Set("X-Kacho-Token-Acr", "2")
	return r
}

// TestAuthz_HTTP_CreateScope_QueryDoesNotDisplaceBody — the core property.
//
// A create whose query string names one project and whose body names another is
// self-contradicting: the query is invisible to the handler, so honouring it would
// authorize against a scope no part of the request acts on. The request is refused,
// and in no case is the model asked about the query-named project.
func TestAuthz_HTTP_CreateScope_QueryDoesNotDisplaceBody(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := scopeSourceMW(t, checker)
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := scopeSourceReq(http.MethodPost,
		"/vpc/v1/networks?projectId="+scopeCallerCanEdit,
		`{"projectId":"`+scopeHandlerActsOn+`","name":"n1"}`)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if in := checker.lastInput.Load(); in != nil {
		assert.NotEqual(t, scopeCallerCanEdit, in.ResourceID,
			"the check must never run against a scope named only in the query string of a body-bearing request — the handler acts on the body")
	}
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"a request naming its scope in two places that disagree must be refused, not silently resolved by source order")
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(3), body["code"], "refusal must be INVALID_ARGUMENT(3)")
	msg, _ := body["message"].(string)
	assert.Contains(t, msg, "project_id",
		"the refusal must name the field whose two values disagree, so the caller can fix the request")
}

// TestAuthz_HTTP_CreateScope_ComesFromBody — the positive half: with no query
// parameter at all, the scope the model is asked about is the one the body names.
func TestAuthz_HTTP_CreateScope_ComesFromBody(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := scopeSourceMW(t, checker)
	var handlerRan bool
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerRan = true
		w.WriteHeader(http.StatusOK)
	}))

	r := scopeSourceReq(http.MethodPost, "/vpc/v1/networks",
		`{"projectId":"`+scopeHandlerActsOn+`","name":"n1"}`)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	require.True(t, handlerRan, "an allowed create must reach the handler")
	in := checker.lastInput.Load()
	require.NotNil(t, in, "the check must run")
	assert.Equal(t, scopeHandlerActsOn, in.ResourceID,
		"the checked scope must be the one the handler acts on")
	assert.Equal(t, "project", in.ResourceType)
}

// TestAuthz_HTTP_CreateScope_QueryOnlyIsNotAScope — a body-bearing request that
// names its scope ONLY in the query string names it nowhere the handler can see.
// The edge must not adopt it; the call falls through to the unscoped wildcard,
// which the model refuses.
func TestAuthz_HTTP_CreateScope_QueryOnlyIsNotAScope(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := scopeSourceMW(t, checker)
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := scopeSourceReq(http.MethodPost,
		"/vpc/v1/networks?projectId="+scopeCallerCanEdit, `{"name":"n1"}`)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if in := checker.lastInput.Load(); in != nil {
		assert.NotEqual(t, scopeCallerCanEdit, in.ResourceID,
			"a query parameter grpc-gateway will not read must not become the authorization scope")
	}
	assert.NotEqual(t, http.StatusOK, w.Code,
		"a create with no scope the handler can see must not be admitted")
}

// TestAuthz_HTTP_CreateScope_ScopeIDQueryKeyIsNotAScopeSource — the generic
// `scope_id` query key is not a scope source either. It is declared by no route
// contract, so it cannot re-point the check away from what the handler acts on.
func TestAuthz_HTTP_CreateScope_ScopeIDQueryKeyIsNotAScopeSource(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := scopeSourceMW(t, checker)
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := scopeSourceReq(http.MethodPost,
		"/vpc/v1/networks?scope_id="+scopeCallerCanEdit,
		`{"projectId":"`+scopeHandlerActsOn+`","name":"n1"}`)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	in := checker.lastInput.Load()
	require.NotNil(t, in, "the check must run against the body scope")
	assert.Equal(t, scopeHandlerActsOn, in.ResourceID,
		"a generic scope_id query key must not displace the scope the handler acts on")
}

// TestAuthz_HTTP_ListScope_ComesFromQuery — the contract read the other way: a
// request with no body has its scope bound from the query string by grpc-gateway,
// so the query IS the authoritative source and list-by-project keeps working.
func TestAuthz_HTTP_ListScope_ComesFromQuery(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := scopeSourceMW(t, checker)
	var handlerRan bool
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerRan = true
		w.WriteHeader(http.StatusOK)
	}))

	r := scopeSourceReq(http.MethodGet, "/vpc/v1/networks?projectId="+scopeHandlerActsOn, "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	require.True(t, handlerRan, "an allowed list must reach the handler")
	in := checker.lastInput.Load()
	require.NotNil(t, in, "the check must run")
	assert.Equal(t, scopeHandlerActsOn, in.ResourceID,
		"a list has no body; its scope is bound from the query string and must be checked there")
}

// TestAuthz_HTTP_CreateScope_AgreeingSourcesAreAccepted — a redundant but
// consistent query parameter is not a contradiction. Refusing it would break
// callers that echo the scope harmlessly; only disagreement is refused.
func TestAuthz_HTTP_CreateScope_AgreeingSourcesAreAccepted(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := scopeSourceMW(t, checker)
	var handlerRan bool
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerRan = true
		w.WriteHeader(http.StatusOK)
	}))

	r := scopeSourceReq(http.MethodPost,
		"/vpc/v1/networks?projectId="+scopeHandlerActsOn,
		`{"projectId":"`+scopeHandlerActsOn+`","name":"n1"}`)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	require.True(t, handlerRan, "agreeing sources must not be refused")
	in := checker.lastInput.Load()
	require.NotNil(t, in)
	assert.Equal(t, scopeHandlerActsOn, in.ResourceID)
}
