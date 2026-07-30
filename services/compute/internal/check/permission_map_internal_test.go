// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/services/compute/internal/check"
)

// catalogAdminMutations — internal catalog-admin RPC, которые ОБЯЗАНЫ быть
// замаплены на FGA-relation `system_admin` @ `cluster:cluster_kacho_root`
// (proto-аннотация `required_relation=system_admin`, object_type=cluster в
// internal_catalog_service.proto). Эти RPC живут на internal listener'е :9091 —
// он гоняет тот же authzIntr, что и public, поэтому каждая catalog-мутация должна
// резолвиться в Check, а не пропускаться methodIsInternal-фолбэком.
// Internal{Zone,Region}Service serving removed — Geography is owned by kacho-geo;
// InternalDiskTypeService removed — block storage is owned by kacho-storage.
// InternalMachineTypeService is the one compute-owned catalog admin left (every
// internal RPC MUST be mapped or it fails closed "rpc not mapped" — MachineType was
// omitted once, 403'ing the admin-seed and cascading into instance/list-filter which
// reference machineTypeId).
var catalogAdminMutations = []string{
	"/kacho.cloud.compute.v1.InternalMachineTypeService/Create",
	"/kacho.cloud.compute.v1.InternalMachineTypeService/Update",
	"/kacho.cloud.compute.v1.InternalMachineTypeService/Delete",
}

// TestPermissionMap_CatalogAdmin_SystemAdminOnCluster — каждая catalog-admin
// мутация замаплена → relation "system_admin", object "cluster:cluster_kacho_root".
func TestPermissionMap_CatalogAdmin_SystemAdminOnCluster(t *testing.T) {
	m := check.PermissionMap()
	for _, fullMethod := range catalogAdminMutations {
		entry, ok := m[fullMethod]
		require.Truef(t, ok, "catalog-admin RPC %s must be present in PermissionMap (internal listener runs authz Check)", fullMethod)
		require.Equalf(t, "system_admin", entry.Relation, "%s: required_relation must be system_admin (proto annotation)", fullMethod)
		require.NotNilf(t, entry.Extract, "%s: must carry an ObjectExtractor", fullMethod)
		require.Falsef(t, entry.Public, "%s: must NOT be Public — it is relation-gated", fullMethod)

		objType, objID, err := entry.Extract(nil)
		require.NoErrorf(t, err, "%s: cluster-scoped extractor must not error on any request", fullMethod)
		require.Equalf(t, "cluster", objType, "%s: object_type must be cluster", fullMethod)
		require.Equalf(t, "cluster_kacho_root", objID, "%s: object_id must be cluster singleton", fullMethod)
	}
}

// catalogPublicReads — the PUBLIC read-only catalog RPCs (MachineType Get/List).
// These run authz Check on the public listener too; an unmapped one fails closed
// "rpc not mapped". MachineTypeService/Get+List were omitted once → the public
// catalog reads used across the machine-type + instance/list-filter suites (and
// Instance.Create machineTypeId resolve) 403'd.
var catalogPublicReads = []string{
	"/kacho.cloud.compute.v1.MachineTypeService/Get",
	"/kacho.cloud.compute.v1.MachineTypeService/List",
}

// TestPermissionMap_CatalogReads_ViewerOnCluster — each public catalog read is
// mapped → relation "viewer", object "cluster:cluster_kacho_root".
func TestPermissionMap_CatalogReads_ViewerOnCluster(t *testing.T) {
	m := check.PermissionMap()
	for _, fullMethod := range catalogPublicReads {
		entry, ok := m[fullMethod]
		require.Truef(t, ok, "catalog read RPC %s must be present in PermissionMap (public listener runs authz Check)", fullMethod)
		require.Equalf(t, "viewer", entry.Relation, "%s: required_relation must be viewer", fullMethod)
		require.NotNilf(t, entry.Extract, "%s: must carry an ObjectExtractor", fullMethod)
		objType, objID, err := entry.Extract(nil)
		require.NoErrorf(t, err, "%s: cluster-scoped extractor must not error", fullMethod)
		require.Equalf(t, "cluster", objType, "%s: object_type must be cluster", fullMethod)
		require.Equalf(t, "cluster_kacho_root", objID, "%s: object_id must be cluster singleton", fullMethod)
	}
}

// TestPermissionMap_CatalogAdmin_EnforcedByInterceptor — sanity at interceptor
// level: a mapped catalog mutation routes to a real Check (system_admin on
// cluster) rather than being skipped, and deny fail-closes with PermissionDenied.
func TestPermissionMap_CatalogAdmin_EnforcedByInterceptor(t *testing.T) {
	intr, calls := newTestInterceptor(t, func(_ context.Context, subject, relation, object string) (bool, error) {
		require.Equal(t, "user:usr_admin", subject)
		require.Equal(t, "system_admin", relation)
		require.Equal(t, "cluster:cluster_kacho_root", object)
		return true, nil
	})
	uIntr := intr.Unary()
	called := false
	handler := func(ctx context.Context, req any) (any, error) { called = true; return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InternalMachineTypeService/Create"}
	ctx := principalCtx("user", "usr_admin")

	resp, err := uIntr(ctx, struct{}{}, info, handler)
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
	require.True(t, called)
	require.Equal(t, 1, *calls, "catalog mutation must trigger exactly one Check, not be bypassed")
}

// TestPermissionMap_InternalWatch_NotExemptFromAuthorization — the outbox stream
// must be marked as authorised at the DATA level, never as exempt.
//
// # Why this test says the opposite of what it used to
//
// It previously required `Public == true` and called that the exempt mechanism. That
// requirement was wrong, and the reason is worth writing down so it is not restored
// by someone reading only the diff.
//
// `Public` makes the interceptor answer allow BEFORE the subject is read — the
// subject is not extracted and the model is not consulted. That is defensible for
// the two RPCs that sit next to it, `OperationService.Get/Cancel`, because those
// authorise on the data: the owning principal is a predicate in the SQL WHERE
// clause, so a non-owner gets NotFound (see internal/handler/operation_handler.go).
// The stream had no such predicate. Its request carries neither a subject nor a
// project, and its payload is the whole resource snapshot, so `Public` on it meant
// no authorisation at all — one call returned the change journal of every tenant.
// The comment claiming parity with the operation service was therefore false: the
// mechanism was the same, the thing that made it safe was absent.
//
// `ScopeFiltered` is the correct marker: the interceptor still performs no
// single-object Check — it cannot, because the rows belong to individually-owned
// objects the caller never names, so there is no one object to ask about — and the
// handler narrows the stream per row instead
// (internal/handler/internal_watch_handler.go). It also puts the RPC under the
// production boot guard for the per-object filter, which `Public` did not.
//
// The entry must still EXIST: the pinned corelib authz.Interceptor has no
// name-based "methodIsInternal" fallback, so an unmapped RPC — stream included —
// fails closed as `PermissionDenied (rpc not mapped)`.
func TestPermissionMap_InternalWatch_NotExemptFromAuthorization(t *testing.T) {
	m := check.PermissionMap()
	entry, ok := m["/kacho.cloud.compute.v1.InternalWatchService/Watch"]
	require.True(t, ok,
		"InternalWatchService/Watch must be present in PermissionMap (no methodIsInternal fallback exists in the pinned corelib)")
	require.False(t, entry.Public,
		"Watch must NOT be Public: that answers allow before the subject is even read, and unlike "+
			"OperationService.Get/Cancel this RPC has no owner predicate to authorise on instead")
	require.True(t, entry.ScopeFiltered,
		"Watch must be ScopeFiltered: rows belong to individually-owned objects the caller does not "+
			"name, so authorisation happens per row in the handler — and the marker is what puts the "+
			"RPC under the production boot guard for that filter")
}
