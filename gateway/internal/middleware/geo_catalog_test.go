// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// TestPermissionCatalog_Geo_S5_PublicReadExempt (GEO-1 F6, GEO-1-20/21) — the geo
// public read RPCs (RegionService / ZoneService Get+List) are project-scope EXEMPT:
// authN-only. Region/Zone is an admin-curated global placement catalog every
// authenticated tenant must read to launch any placeable resource, so the gateway
// waives the per-RPC FGA Check (`<exempt>`) — a project-scope Check would 403 a
// zero-binding tenant (GEO-1-20). authN (JWT) still applies (GEO-1-21). This is the
// documented exception recorded in security.md (mirrors the iam JWKS-route note).
// Regressing any of the four back to a real `required_relation` would re-block a
// zero-binding tenant from discovering zones.
func TestPermissionCatalog_Geo_S5_PublicReadExempt(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	for _, fqn := range []string{
		"kacho.cloud.geo.v1.RegionService/Get",
		"kacho.cloud.geo.v1.RegionService/List",
		"kacho.cloud.geo.v1.ZoneService/Get",
		"kacho.cloud.geo.v1.ZoneService/List",
	} {
		t.Run(fqn, func(t *testing.T) {
			entry, ok := c.Lookup(fqn)
			require.True(t, ok, "fqn missing from embedded catalog: %s", fqn)
			assert.True(t, entry.IsExempt(), "geo public read must be <exempt> (project-scope EXEMPT) on %s", fqn)
			assert.Equal(t, "<exempt>", entry.Permission, "geo public read permission must be <exempt> on %s", fqn)
			assert.Empty(t, entry.RequiredRelation, "geo public read must carry no required_relation on %s", fqn)
			assert.Empty(t, entry.ScopeExtractor.ObjectType, "geo public read must carry no scope object_type on %s", fqn)
			assert.Empty(t, entry.ScopeExtractor.FromRequestField, "geo public read must carry no scope from_request_field on %s", fqn)
		})
	}
}

// TestPermissionCatalog_Geo_S5_InternalAdminSystemAdmin — the geo Internal* admin
// CRUD + GetInternal RPCs must be gated by `system_admin` on the cluster singleton,
// mirroring compute InternalZone/InternalRegion. Regressing any to viewer/<exempt>
// would let non-admins mutate or read the Internal projection (raw status + infra°)
// of the geography catalog.
func TestPermissionCatalog_Geo_S5_InternalAdminSystemAdmin(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	want := []struct {
		fqn  string
		perm string
	}{
		{"kacho.cloud.geo.v1.InternalRegionService/Create", "geo.regions.create"},
		{"kacho.cloud.geo.v1.InternalRegionService/Update", "geo.regions.update"},
		{"kacho.cloud.geo.v1.InternalRegionService/Delete", "geo.regions.delete"},
		{"kacho.cloud.geo.v1.InternalRegionService/GetInternal", "geo.regions.getInternal"},
		{"kacho.cloud.geo.v1.InternalZoneService/Create", "geo.zones.create"},
		{"kacho.cloud.geo.v1.InternalZoneService/Update", "geo.zones.update"},
		{"kacho.cloud.geo.v1.InternalZoneService/Delete", "geo.zones.delete"},
		{"kacho.cloud.geo.v1.InternalZoneService/GetInternal", "geo.zones.getInternal"},
	}
	for _, w := range want {
		t.Run(w.fqn, func(t *testing.T) {
			entry, ok := c.Lookup(w.fqn)
			require.True(t, ok, "fqn missing from embedded catalog: %s", w.fqn)
			assert.Equal(t, w.perm, entry.Permission, "permission identifier on %s", w.fqn)
			assert.Equal(t, "system_admin", entry.RequiredRelation, "geo admin CRUD must be system_admin on %s", w.fqn)
			assert.Equal(t, "cluster", entry.ScopeExtractor.ObjectType, "geo admin scope object_type must be cluster on %s", w.fqn)
			assert.Equal(t, "*", entry.ScopeExtractor.FromRequestField, "geo admin scope from_request_field must be '*' on %s", w.fqn)
			assert.False(t, entry.IsExempt(), "geo admin CRUD must NOT be <exempt> on %s", w.fqn)
		})
	}
}

// TestRestRouter_Geo_S5_PathFQN (GEO-1 F5/F6, GEO-1-16/17) — the REST route table
// must resolve geo paths to the right gRPC FQNs:
//   - public reads GET /geo/v1/{regions,zones}[/{id}] → RegionService/ZoneService;
//   - admin CRUD + GetInternal live on the SELF-DESCRIBING /geo/v1/internal/…
//     segment → InternalRegionService/InternalZoneService. The /internal/ segment is
//     what routes these onto the cluster-internal sub-mux and gets them 404'd on the
//     external listener (ban #6, GEO-1-17).
// Without these the authz middleware cannot map geo requests to catalog entries and
// every geo REST call is denied.
func TestRestRouter_Geo_S5_PathFQN(t *testing.T) {
	r := middleware.NewRestRouter()

	cases := []struct {
		method, path, fqn string
	}{
		// public read-discovery
		{"GET", "/geo/v1/regions", "kacho.cloud.geo.v1.RegionService/List"},
		{"GET", "/geo/v1/regions/ru-central1", "kacho.cloud.geo.v1.RegionService/Get"},
		{"GET", "/geo/v1/zones", "kacho.cloud.geo.v1.ZoneService/List"},
		{"GET", "/geo/v1/zones/ru-central1-a", "kacho.cloud.geo.v1.ZoneService/Get"},
		// admin CRUD + GetInternal on the internal segment
		{"POST", "/geo/v1/internal/regions", "kacho.cloud.geo.v1.InternalRegionService/Create"},
		{"PATCH", "/geo/v1/internal/regions/ru-central1", "kacho.cloud.geo.v1.InternalRegionService/Update"},
		{"DELETE", "/geo/v1/internal/regions/ru-central1", "kacho.cloud.geo.v1.InternalRegionService/Delete"},
		{"GET", "/geo/v1/internal/regions/ru-central1", "kacho.cloud.geo.v1.InternalRegionService/GetInternal"},
		{"POST", "/geo/v1/internal/zones", "kacho.cloud.geo.v1.InternalZoneService/Create"},
		{"PATCH", "/geo/v1/internal/zones/ru-central1-a", "kacho.cloud.geo.v1.InternalZoneService/Update"},
		{"DELETE", "/geo/v1/internal/zones/ru-central1-a", "kacho.cloud.geo.v1.InternalZoneService/Delete"},
		{"GET", "/geo/v1/internal/zones/ru-central1-a", "kacho.cloud.geo.v1.InternalZoneService/GetInternal"},
	}
	for _, tc := range cases {

		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			got, ok := r.Resolve(tc.method, tc.path)
			require.True(t, ok, "no route for %s %s", tc.method, tc.path)
			assert.Equal(t, tc.fqn, got)
		})
	}
}

// TestRestRouter_Geo_S5_InternalPathNotPublicCRUD (GEO-1-17) — admin CRUD must NOT
// be reachable on the PUBLIC /geo/v1/{regions,zones} path any more (it moved to
// /geo/v1/internal/…). A POST/PATCH/DELETE on the public path must not resolve to an
// Internal* mutation FQN — otherwise the external listener would carry a mutation
// route for the admin catalog.
func TestRestRouter_Geo_S5_InternalPathNotPublicCRUD(t *testing.T) {
	r := middleware.NewRestRouter()

	for _, tc := range []struct{ method, path string }{
		{"POST", "/geo/v1/regions"},
		{"PATCH", "/geo/v1/regions/ru-central1"},
		{"DELETE", "/geo/v1/regions/ru-central1"},
		{"POST", "/geo/v1/zones"},
		{"PATCH", "/geo/v1/zones/ru-central1-a"},
		{"DELETE", "/geo/v1/zones/ru-central1-a"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			got, ok := r.Resolve(tc.method, tc.path)
			if ok {
				assert.NotContains(t, got, "InternalRegionService",
					"admin CRUD must not resolve on the public path %s %s", tc.method, tc.path)
				assert.NotContains(t, got, "InternalZoneService",
					"admin CRUD must not resolve on the public path %s %s", tc.method, tc.path)
			}
		})
	}
}
