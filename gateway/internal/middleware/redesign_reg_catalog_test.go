// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// TestPermissionCatalog_RedesignReg — the embedded permission catalog must carry
// every redesign-2026 RPC registered on the api-gateway (public + internal
// projection), so the per-RPC authz middleware resolves them (a missing entry ⇒
// `catalog: no entry for method` ⇒ fail-closed AUTHZ_DENIED). Values mirror the
// proto authz annotations (source of truth).
func TestPermissionCatalog_RedesignReg(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	want := []struct {
		fqn, perm, rel, objType, fromField string
	}{
		// compute MachineTypeService — public read (cluster-viewer, geo-parity).
		{"kacho.cloud.compute.v1.MachineTypeService/Get", "compute.machineTypes.get", "viewer", "cluster", "*"},
		{"kacho.cloud.compute.v1.MachineTypeService/List", "compute.machineTypes.list", "viewer", "cluster", "*"},
		// compute InternalMachineTypeService — admin CRUD (system_admin on cluster).
		{"kacho.cloud.compute.v1.InternalMachineTypeService/Create", "compute.machineTypes.create", "system_admin", "cluster", "*"},
		{"kacho.cloud.compute.v1.InternalMachineTypeService/Update", "compute.machineTypes.update", "system_admin", "cluster", "*"},
		{"kacho.cloud.compute.v1.InternalMachineTypeService/Delete", "compute.machineTypes.delete", "system_admin", "cluster", "*"},
		// storage ImageService — public CRUD + ListOperations (object-scoped anti-BOLA).
		{"kacho.cloud.storage.v1.ImageService/Get", "storage.images.get", "viewer", "storage_image", "image_id"},
		{"kacho.cloud.storage.v1.ImageService/List", "storage.images.list", "viewer", "project", "project_id"},
		{"kacho.cloud.storage.v1.ImageService/Create", "storage.images.create", "editor", "project", "project_id"},
		{"kacho.cloud.storage.v1.ImageService/Update", "storage.images.update", "editor", "storage_image", "image_id"},
		{"kacho.cloud.storage.v1.ImageService/Delete", "storage.images.delete", "editor", "storage_image", "image_id"},
		{"kacho.cloud.storage.v1.ImageService/ListOperations", "storage.images.listOperations", "viewer", "storage_image", "image_id"},
		// storage InternalImageService — infra projection (internal-only).
		{"kacho.cloud.storage.v1.InternalImageService/GetInternal", "storage.images.getInternal", "viewer", "storage_image", "image_id"},
		// vpc NetworkService :verb supernet growth/shrink (object-scoped v_update).
		{"kacho.cloud.vpc.v1.NetworkService/AddCidrBlocks", "vpc.network_cidr_blockses.addCidrBlocks", "v_update", "vpc_network", "network_id"},
		{"kacho.cloud.vpc.v1.NetworkService/RemoveCidrBlocks", "vpc.network_cidr_blockses.removeCidrBlocks", "v_update", "vpc_network", "network_id"},
		// iam AccessBindingService unified List (cluster-viewer gate) + soft-revoke.
		{"kacho.cloud.iam.v1.AccessBindingService/List", "iam.access_bindings.list", "viewer", "cluster", "*"},
		{"kacho.cloud.iam.v1.AccessBindingService/Revoke", "iam.access_bindings.revoke", "v_delete", "iam_access_binding", "access_binding_id"},
	}
	for _, w := range want {
		t.Run(w.fqn, func(t *testing.T) {
			entry, ok := c.Lookup(w.fqn)
			require.True(t, ok, "fqn missing from embedded catalog: %s", w.fqn)
			assert.Equal(t, w.perm, entry.Permission, "permission on %s", w.fqn)
			assert.Equal(t, w.rel, entry.RequiredRelation, "required_relation on %s", w.fqn)
			assert.Equal(t, w.objType, entry.ScopeExtractor.ObjectType, "scope object_type on %s", w.fqn)
			assert.Equal(t, w.fromField, entry.ScopeExtractor.FromRequestField, "scope from_request_field on %s", w.fqn)
			assert.False(t, entry.IsExempt(), "%s must NOT be <exempt>", w.fqn)
		})
	}
}

// TestRestRouter_RedesignReg — the REST route table must resolve every new
// redesign REST path to the correct gRPC FQN so the authz middleware can map it to
// its catalog entry (a missing route ⇒ every authenticated call on that path is
// denied). InternalImageService.GetInternal is a default unbound-route (no
// google.api.http) and thus intentionally NOT in the REST table.
func TestRestRouter_RedesignReg(t *testing.T) {
	r := middleware.NewRestRouter()

	cases := []struct {
		method, path, fqn string
	}{
		{"GET", "/compute/v1/machineTypes", "kacho.cloud.compute.v1.MachineTypeService/List"},
		{"GET", "/compute/v1/machineTypes/mt-1", "kacho.cloud.compute.v1.MachineTypeService/Get"},
		{"POST", "/compute/v1/internal/machineTypes", "kacho.cloud.compute.v1.InternalMachineTypeService/Create"},
		{"PATCH", "/compute/v1/internal/machineTypes/mt-1", "kacho.cloud.compute.v1.InternalMachineTypeService/Update"},
		{"DELETE", "/compute/v1/internal/machineTypes/mt-1", "kacho.cloud.compute.v1.InternalMachineTypeService/Delete"},
		{"GET", "/storage/v1/images", "kacho.cloud.storage.v1.ImageService/List"},
		{"POST", "/storage/v1/images", "kacho.cloud.storage.v1.ImageService/Create"},
		{"GET", "/storage/v1/images/img-1", "kacho.cloud.storage.v1.ImageService/Get"},
		{"PATCH", "/storage/v1/images/img-1", "kacho.cloud.storage.v1.ImageService/Update"},
		{"DELETE", "/storage/v1/images/img-1", "kacho.cloud.storage.v1.ImageService/Delete"},
		{"GET", "/storage/v1/images/img-1/operations", "kacho.cloud.storage.v1.ImageService/ListOperations"},
		{"POST", "/vpc/v1/networks/net-1:add-cidr-blocks", "kacho.cloud.vpc.v1.NetworkService/AddCidrBlocks"},
		{"POST", "/vpc/v1/networks/net-1:remove-cidr-blocks", "kacho.cloud.vpc.v1.NetworkService/RemoveCidrBlocks"},
		{"GET", "/iam/v1/accessBindings", "kacho.cloud.iam.v1.AccessBindingService/List"},
		{"POST", "/iam/v1/accessBindings/acb-1:revoke", "kacho.cloud.iam.v1.AccessBindingService/Revoke"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			got, ok := r.Resolve(tc.method, tc.path)
			require.True(t, ok, "no route for %s %s", tc.method, tc.path)
			assert.Equal(t, tc.fqn, got)
		})
	}
}
