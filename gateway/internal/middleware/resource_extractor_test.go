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

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

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
		"kacho.cloud.iam.v1.ProjectService/Get": "/iam/v1/projects/{project_id}",
	})
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{
			ObjectType:       "project",
			FromRequestField: "project_id",
		},
	}
	r := httptest.NewRequest(http.MethodGet, "/iam/v1/projects/prj_alpha", nil)
	id, ok := e.ExtractFromHTTP(r, "kacho.cloud.iam.v1.ProjectService/Get", entry)
	require.True(t, ok)
	assert.Equal(t, "prj_alpha", id.String())
}

func TestResourceExtractor_FromHTTP_QueryStringFallback(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{
			FromRequestField: "folder_id",
		},
	}
	r := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks?folder_id=fld_x", nil)
	id, ok := e.ExtractFromHTTP(r, "kacho.cloud.vpc.v1.NetworkService/Create", entry)
	require.True(t, ok)
	assert.Equal(t, "fld_x", id.String())
}

func TestResourceExtractor_FromHTTP_ScopeIDFallback(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{
			FromRequestField: "some_field",
		},
	}
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/authorize:batchCheck?scope_id=prj_x", nil)
	id, ok := e.ExtractFromHTTP(r, "X/Y", entry)
	require.True(t, ok)
	assert.Equal(t, "prj_x", id.String())
}

func TestResourceExtractor_FromHTTP_NoMatch_Wildcard(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{
			FromRequestField: "missing",
		},
	}
	r := httptest.NewRequest(http.MethodGet, "/iam/v1/something", nil)
	id, ok := e.ExtractFromHTTP(r, "X/Y", entry)
	require.True(t, ok)
	assert.True(t, id.IsWildcard())
}

func TestResourceExtractor_FromHTTP_NilRequest(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	entry := middleware.CatalogEntry{
		ScopeExtractor: middleware.ScopeExtractor{FromRequestField: "subject"},
	}
	id, ok := e.ExtractFromHTTP(nil, "X/Y", entry)
	require.True(t, ok)
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

func TestResourceExtractor_DefinitionTierScope_Account(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	req := &iamv1.CreateRoleRequest{
		Name:           "reader",
		DefinitionTier: &iamv1.DefinitionTier{TierType: "iam.account", TierId: "acc_alpha"},
	}
	ot, id, ok := e.ResolveDefinitionTierScope(req)
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
	ot, id, ok := e.ResolveDefinitionTierScope(req)
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
		_, _, ok := e.ResolveDefinitionTierScope(req)
		assert.Falsef(t, ok, "tierType %q must not resolve", tt)
	}
}

// A legacy account_id-only request (no definition_tier) → not resolved, caller
// falls through to the catalog's static account_id extraction.
func TestResourceExtractor_DefinitionTierScope_LegacyNoTier_NotResolved(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	req := &iamv1.CreateRoleRequest{Name: "reader", AccountId: "acc_alpha"}
	_, _, ok := e.ResolveDefinitionTierScope(req)
	assert.False(t, ok)
}

func TestResourceExtractor_DefinitionTierScope_HTTP_JSONBody(t *testing.T) {
	e := middleware.NewResourceExtractor(nil)
	body := `{"name":"reader","definitionTier":{"tierType":"iam.account","tierId":"acc_gamma"},"rules":[]}`
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/roles", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	ot, id, ok := e.ResolveDefinitionTierScopeHTTP(r)
	require.True(t, ok)
	assert.Equal(t, "account", ot)
	assert.Equal(t, "acc_gamma", id)
	// body restored for the downstream handler
	rest, _ := io.ReadAll(r.Body)
	assert.Equal(t, body, string(rest))
}
