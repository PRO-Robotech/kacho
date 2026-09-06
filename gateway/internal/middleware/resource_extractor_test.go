// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

func TestResourceExtractor_FromProto_StringField(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{
			ObjectType:       "project",
			FromRequestField: "subject",
		},
	}
	req := &iamv1.AuthorizeCheckRequest{Subject: "user:usr_abc"}
	id, ok := e.ExtractFromProto(req, entry)
	require.True(t, ok)
	assert.Equal(t, "user:usr_abc", id.String())
	assert.False(t, id.IsWildcard())
}

func TestResourceExtractor_FromProto_ResourceRefMessage(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{
			ObjectType:       "project",
			FromRequestField: "resource",
		},
	}
	// ListSubjectsRequest has `resource` of type ResourceRef.
	req := &iamv1.ListSubjectsRequest{
		Resource: &iamv1.ResourceRef{Type: "project", Id: "prj_billing_42"},
		Action:   "iam.authorize.listSubjects",
	}
	id, ok := e.ExtractFromProto(req, entry)
	require.True(t, ok)
	assert.Equal(t, "prj_billing_42", id.String())
}

func TestResourceExtractor_FromProto_MissingField_Wildcard(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{
			FromRequestField: "nonexistent_field",
		},
	}
	req := &iamv1.AuthorizeCheckRequest{Subject: "user:usr_abc"}
	id, ok := e.ExtractFromProto(req, entry)
	require.True(t, ok)
	assert.True(t, id.IsWildcard())
}

func TestResourceExtractor_FromProto_EmptyField_Wildcard(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: ""},
	}
	id, ok := e.ExtractFromProto(&iamv1.AuthorizeCheckRequest{}, entry)
	require.True(t, ok)
	assert.True(t, id.IsWildcard())
}

func TestResourceExtractor_FromProto_StarField_Wildcard(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "*"},
	}
	id, ok := e.ExtractFromProto(&iamv1.AuthorizeCheckRequest{}, entry)
	require.True(t, ok)
	assert.True(t, id.IsWildcard())
}

func TestResourceExtractor_FromProto_NilRequest(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "subject"},
	}
	id, ok := e.ExtractFromProto(nil, entry)
	require.True(t, ok)
	assert.True(t, id.IsWildcard())
}

func TestResourceExtractor_FromHTTP_PathTemplate(t *testing.T) {
	e := middleware.NewResourceExtractor(map[string]string{
		"kaname.cloud.iam.v1.ProjectService/Get": "/iam/v1/projects/{project_id}",
	})
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{
			ObjectType:       "project",
			FromRequestField: "project_id",
		},
	}
	r := httptest.NewRequest(http.MethodGet, "/iam/v1/projects/prj_alpha", nil)
	id, conflict := e.ExtractFromHTTP(r, "kaname.cloud.iam.v1.ProjectService/Get", entry)
	require.Nil(t, conflict)
	assert.Equal(t, "prj_alpha", id.String())
}

// --- Which source the scope comes from is decided by the route contract ---
//
// grpc-gateway binds the handler's request message from the body when the method
// carries one (`body: "*"`, the form every Kachō route with a body uses) and from
// the query string when it does not. The extractor reads from the same place, so
// the object the edge asks the model about is the object the handler acts on.

func TestResourceExtractor_FromHTTP_NoBody_QueryIsTheSource(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "project_id"},
	}
	// A list narrows by parent through the query string; that is the source
	// grpc-gateway binds it from, so it is the source we check.
	r := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks?projectId=prj_x", nil)
	id, conflict := e.ExtractFromHTTP(r, "kacho.cloud.vpc.v1.NetworkService/List", entry)
	require.Nil(t, conflict)
	assert.Equal(t, "prj_x", id.String())
}

func TestResourceExtractor_FromHTTP_BodyBearing_BodyIsTheSource(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "project_id"},
	}
	body := `{"projectId":"prj_body","name":"n"}`
	r := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	id, conflict := e.ExtractFromHTTP(r, "kacho.cloud.vpc.v1.NetworkService/Create", entry)
	require.Nil(t, conflict)
	assert.Equal(t, "prj_body", id.String())
	rest, _ := io.ReadAll(r.Body)
	assert.Equal(t, body, string(rest), "body must be restored for the handler")
}

// A query parameter on a body-bearing route names nothing the handler will read
// — grpc-gateway does not parse the query for a `body: "*"` binding — so it
// cannot become the scope. A caller that named it only there is contradicting
// the route, and gets told so instead of a puzzling unscoped-resource denial.
func TestResourceExtractor_FromHTTP_BodyBearing_QueryOnly_IsNotTheScope(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "project_id"},
	}
	r := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks?projectId=prj_query",
		strings.NewReader(`{"name":"n"}`))
	r.Header.Set("Content-Type", "application/json")
	id, conflict := e.ExtractFromHTTP(r, "kacho.cloud.vpc.v1.NetworkService/Create", entry)
	require.NotNil(t, conflict, "naming the scope only where the handler cannot read it is a contradiction")
	assert.Equal(t, "project_id", conflict.Field)
	assert.True(t, id.IsWildcard(),
		"a query parameter the handler never sees must not become the checked scope")
}

// Two sources, two different values: no winner is correct, so the request is
// reported as contradictory for the caller to refuse.
func TestResourceExtractor_FromHTTP_BodyBearing_DisagreeingSources_Conflict(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "project_id"},
	}
	r := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks?projectId=prj_query",
		strings.NewReader(`{"projectId":"prj_body"}`))
	r.Header.Set("Content-Type", "application/json")
	id, conflict := e.ExtractFromHTTP(r, "kacho.cloud.vpc.v1.NetworkService/Create", entry)
	require.NotNil(t, conflict, "disagreeing scope sources must be reported, not silently ordered")
	assert.Equal(t, "project_id", conflict.Field)
	assert.Equal(t, "prj_body", id.String(),
		"the reported id stays the handler-visible one")
}

// Echoing the same value in both places is redundant, not contradictory.
func TestResourceExtractor_FromHTTP_BodyBearing_AgreeingSources_NoConflict(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "project_id"},
	}
	r := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks?projectId=prj_same",
		strings.NewReader(`{"projectId":"prj_same"}`))
	r.Header.Set("Content-Type", "application/json")
	id, conflict := e.ExtractFromHTTP(r, "kacho.cloud.vpc.v1.NetworkService/Create", entry)
	require.Nil(t, conflict)
	assert.Equal(t, "prj_same", id.String())
}

// The generic `scope_id` query key is declared by no route contract: no handler
// binds a field from it, so it names nothing and re-points nothing. Its own
// catalog field (`BatchAuthorizeCheckRequest.scope_id`) is read from the body
// like any other, which the second half of this test pins.
func TestResourceExtractor_FromHTTP_GenericScopeIDQueryKey_IsNotASource(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "some_field"},
	}
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/authorize:batchCheck?scope_id=prj_x",
		strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	id, conflict := e.ExtractFromHTTP(r, "X/Y", entry)
	require.Nil(t, conflict)
	assert.True(t, id.IsWildcard(), "scope_id is not a scope source for a field named otherwise")

	scoped := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "scope_id"},
	}
	r2 := httptest.NewRequest(http.MethodPost, "/iam/v1/authorize:batchCheck",
		strings.NewReader(`{"scopeId":"prj_batch"}`))
	r2.Header.Set("Content-Type", "application/json")
	id2, conflict2 := e.ExtractFromHTTP(r2, "kaname.cloud.iam.v1.AuthorizeService/BatchCheck", scoped)
	require.Nil(t, conflict2)
	assert.Equal(t, "prj_batch", id2.String())
}

// The two halves of one scope — object type and object id — are read from the
// same source, so they can never describe different objects.
func TestResourceExtractor_ScopeTypeFromHTTP_FollowsTheSameSource(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)

	r := httptest.NewRequest(http.MethodGet, "/iam/v1/accessBindings:listByScope?resourceType=account", nil)
	typ, conflict := e.ScopeTypeFromHTTP(r, "resource_type")
	require.Nil(t, conflict)
	assert.Equal(t, "account", typ, "a request without a body binds its type from the query")

	rb := httptest.NewRequest(http.MethodPost, "/iam/v1/x?resourceType=account",
		strings.NewReader(`{"resourceType":"project"}`))
	rb.Header.Set("Content-Type", "application/json")
	typ2, conflict2 := e.ScopeTypeFromHTTP(rb, "resource_type")
	require.NotNil(t, conflict2, "a disagreeing scope type is the same contradiction as a disagreeing id")
	assert.Equal(t, "resource_type", conflict2.Field)
	assert.Equal(t, "project", typ2)
}

func TestResourceExtractor_FromHTTP_NoMatch_Wildcard(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{
			FromRequestField: "missing",
		},
	}
	r := httptest.NewRequest(http.MethodGet, "/iam/v1/something", nil)
	id, conflict := e.ExtractFromHTTP(r, "X/Y", entry)
	require.Nil(t, conflict)
	assert.True(t, id.IsWildcard())
}

func TestResourceExtractor_FromHTTP_NilRequest(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "subject"},
	}
	id, conflict := e.ExtractFromHTTP(nil, "X/Y", entry)
	require.Nil(t, conflict)
	assert.True(t, id.IsWildcard())
}

// Extraction of a scalar string field (`network_id`) off a real domain proto
// message — the production path always hands the extractor a proto.Message.
func TestResourceExtractor_FromProto_StringField_NetworkID(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "network_id"},
	}
	req := &vpcv1.CreateSubnetRequest{NetworkId: "enp_x", Name: "sn"}
	id, ok := e.ExtractFromProto(req, entry)
	require.True(t, ok)
	assert.Equal(t, "enp_x", id.String())
}

// A non-proto request is unreachable on the production authz path (ProtoReq is
// always a proto.Message); the extractor fails closed to the wildcard scope.
func TestResourceExtractor_FromProto_NonProto_Wildcard(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "network_id"},
	}
	id, ok := e.ExtractFromProto(struct{ NetworkID string }{NetworkID: "enp_x"}, entry)
	require.True(t, ok)
	assert.True(t, id.IsWildcard())
}

// --- redesign-2026 F4: Role definition_tier scope resolution (MIGRATE) ---
//
// The anchor resolution is FQN-BOUND: only the RPCs whose request message
// declares `definition_tier` (today RoleService/Create) may be re-scoped by it.
// roleCreateFQN is that FQN; the unrelated-FQN arm is locked below.
const roleCreateFQN = "kaname.cloud.iam.v1.RoleService/Create"

func TestResourceExtractor_DefinitionTierScope_Account(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	req := &iamv1.CreateRoleRequest{
		Name:           "reader",
		DefinitionTier: &iamv1.DefinitionTier{TierType: "iam.account", TierId: "acc_alpha"},
	}
	ot, id, ok := e.ResolveDefinitionTierScope(roleCreateFQN, req)
	require.True(t, ok)
	assert.Equal(t, "account", ot)
	assert.Equal(t, "acc_alpha", id)
}

func TestResourceExtractor_DefinitionTierScope_Project(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	req := &iamv1.CreateRoleRequest{
		Name:           "reader",
		DefinitionTier: &iamv1.DefinitionTier{TierType: "iam.project", TierId: "prj_beta"},
	}
	ot, id, ok := e.ResolveDefinitionTierScope(roleCreateFQN, req)
	require.True(t, ok)
	assert.Equal(t, "project", ot)
	assert.Equal(t, "prj_beta", id)
}

// iam.cluster (system roles are seeded, never API-created) and unknown types are
// NOT resolved — the caller keeps the legacy scope and the iam handler surfaces
// the canonical INVALID_ARGUMENT.
func TestResourceExtractor_DefinitionTierScope_ClusterAndUnknown_NotResolved(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	for _, tt := range []string{"iam.cluster", "iam.bogus", ""} {
		req := &iamv1.CreateRoleRequest{DefinitionTier: &iamv1.DefinitionTier{TierType: tt, TierId: "x"}}
		_, _, ok := e.ResolveDefinitionTierScope(roleCreateFQN, req)
		assert.Falsef(t, ok, "tierType %q must not resolve", tt)
	}
}

// A legacy account_id-only request (no definition_tier) → not resolved, caller
// falls through to the catalog's static account_id extraction.
func TestResourceExtractor_DefinitionTierScope_LegacyNoTier_NotResolved(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	req := &iamv1.CreateRoleRequest{Name: "reader", AccountId: "acc_alpha"}
	_, _, ok := e.ResolveDefinitionTierScope(roleCreateFQN, req)
	assert.False(t, ok)
}

func TestResourceExtractor_DefinitionTierScope_HTTP_JSONBody(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	body := `{"name":"reader","definitionTier":{"tierType":"iam.account","tierId":"acc_gamma"},"rules":[]}`
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/roles", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	ot, id, ok := e.ResolveDefinitionTierScopeHTTP(roleCreateFQN, r)
	require.True(t, ok)
	assert.Equal(t, "account", ot)
	assert.Equal(t, "acc_gamma", id)
	// body restored for the downstream handler
	rest, _ := io.ReadAll(r.Body)
	assert.Equal(t, body, string(rest))
}

// TestResourceExtractor_DefinitionTierScope_UnrelatedFQN_NotResolved — the FQN
// binding: an RPC that does not declare the anchor is never re-scoped by it, on
// EITHER arm (typed proto and raw-JSON HTTP). Without the binding a caller could
// smuggle `definitionTier` into any JSON body and choose their own authz scope.
func TestResourceExtractor_DefinitionTierScope_UnrelatedFQN_NotResolved(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	const otherFQN = "kaname.cloud.iam.v1.UserService/Invite"

	req := &iamv1.CreateRoleRequest{
		Name:           "reader",
		DefinitionTier: &iamv1.DefinitionTier{TierType: "iam.project", TierId: "prj_attacker"},
	}
	_, _, ok := e.ResolveDefinitionTierScope(otherFQN, req)
	assert.False(t, ok, "proto arm: anchor must not resolve on an FQN that does not declare it")

	body := `{"accountId":"acc_victim","definitionTier":{"tierType":"iam.project","tierId":"prj_attacker"}}`
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/users:invite", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	_, _, ok = e.ResolveDefinitionTierScopeHTTP(otherFQN, r)
	assert.False(t, ok, "HTTP arm: anchor must not resolve on an FQN that does not declare it")
}

// The method decides which source is authoritative, so it must be read the same
// way the router read it when it picked the route — and therefore the catalog
// row. RestRouter.Resolve upper-cases; an unusually-cased method must not route
// as a create while being scoped as a read.
func TestResourceExtractor_FromHTTP_MethodCasing_DoesNotChangeTheSource(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "project_id"},
	}
	r := httptest.NewRequest("post", "/vpc/v1/networks?projectId=prj_query",
		strings.NewReader(`{"projectId":"prj_body"}`))
	r.Header.Set("Content-Type", "application/json")
	id, conflict := e.ExtractFromHTTP(r, "kacho.cloud.vpc.v1.NetworkService/Create", entry)
	require.NotNil(t, conflict, "an unusually-cased body-bearing method is still body-bearing")
	assert.Equal(t, "prj_body", id.String())
}

// An unrecognised method is treated as body-bearing: guessing "bodyless" would
// hand the scope back to the query string on a route whose handler reads the
// body. Guessing this way costs at most an unresolved scope, i.e. a denial.
func TestResourceExtractor_FromHTTP_UnknownMethod_IsTreatedAsBodyBearing(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "project_id"},
	}
	r := httptest.NewRequest("PROPFIND", "/vpc/v1/networks?projectId=prj_query", nil)
	id, conflict := e.ExtractFromHTTP(r, "kacho.cloud.vpc.v1.NetworkService/Create", entry)
	require.NotNil(t, conflict)
	assert.True(t, id.IsWildcard(),
		"an unrecognised method must not take its scope from the query string")
}

// The REST mux registers its JSON marshaler under the wildcard media type, so
// the handler decodes the body as JSON whatever the request labelled it. The
// edge must read the same body: gating on Content-Type would let a caller choose
// a media type that hides the scope from the check and shows it to the handler.
func TestResourceExtractor_FromHTTP_ContentTypeDoesNotDecideWhatIsRead(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "project_id"},
	}
	for _, ct := range []string{
		"application/json",
		"Application/JSON",                  // legal casing (RFC 9110), lower-cased by the mux
		"application/x-www-form-urlencoded", // still decoded as JSON by the wildcard marshaler
		"",                                  // absent
	} {
		r := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks",
			strings.NewReader(`{"projectId":"prj_body"}`))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		id, conflict := e.ExtractFromHTTP(r, "kacho.cloud.vpc.v1.NetworkService/Create", entry)
		require.Nilf(t, conflict, "Content-Type %q", ct)
		assert.Equalf(t, "prj_body", id.String(),
			"Content-Type %q must not change which scope the check runs against", ct)
	}
}

// A body larger than the inspection cap yields no scope at the edge — but it
// must still reach the handler INTACT. Buffering a prefix and dropping the rest
// silently corrupts every oversized request.
func TestResourceExtractor_FromHTTP_OversizedBody_IsNotTruncated(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "project_id"},
	}
	padding := strings.Repeat("x", 1<<20)
	body := `{"projectId":"prj_body","note":"` + padding + `"}`
	r := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	id, conflict := e.ExtractFromHTTP(r, "kacho.cloud.vpc.v1.NetworkService/Create", entry)
	require.Nil(t, conflict)
	assert.True(t, id.IsWildcard(), "a body too large to inspect must yield no scope")

	rest, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	assert.Equal(t, len(body), len(rest), "the handler must still receive the whole body")
	assert.Equal(t, body, string(rest))
}
