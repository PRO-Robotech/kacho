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
		// compute InternalMachineTypeService — admin CRUD (system_admin on cluster).
		{"kacho.cloud.compute.v1.InternalMachineTypeService/Create", "compute.machineTypes.create", "system_admin", "cluster", "*"},
		{"kacho.cloud.compute.v1.InternalMachineTypeService/Update", "compute.machineTypes.update", "system_admin", "cluster", "*"},
		{"kacho.cloud.compute.v1.InternalMachineTypeService/Delete", "compute.machineTypes.delete", "system_admin", "cluster", "*"},
		// storage ImageService — public CRUD + ListOperations (object-scoped anti-BOLA).
		// Object-self rows take the OBJECT'S VERB; the two rows anchored on the parent
		// project keep the project tier (`editor` to create into it, `viewer` to list
		// what is in it) — that axis is unchanged.
		{"kacho.cloud.storage.v1.ImageService/Get", "storage.images.get", "v_get", "storage_image", "image_id"},
		{"kacho.cloud.storage.v1.ImageService/List", "storage.images.list", "viewer", "project", "project_id"},
		{"kacho.cloud.storage.v1.ImageService/Create", "storage.images.create", "editor", "project", "project_id"},
		{"kacho.cloud.storage.v1.ImageService/Update", "storage.images.update", "v_update", "storage_image", "image_id"},
		{"kacho.cloud.storage.v1.ImageService/Delete", "storage.images.delete", "v_delete", "storage_image", "image_id"},
		{"kacho.cloud.storage.v1.ImageService/ListOperations", "storage.images.listOperations", "v_list", "storage_image", "image_id"},
		// storage InternalImageService — infra projection (internal-only).
		{"kacho.cloud.storage.v1.InternalImageService/GetInternal", "storage.images.getInternal", "viewer", "storage_image", "image_id"},
		// vpc NetworkService :verb supernet growth/shrink (object-scoped v_update).
		{"kacho.cloud.vpc.v1.NetworkService/AddCidrBlocks", "vpc.network_cidr_blocks.addCidrBlocks", "v_update", "vpc_network", "network_id"},
		{"kacho.cloud.vpc.v1.NetworkService/RemoveCidrBlocks", "vpc.network_cidr_blocks.removeCidrBlocks", "v_update", "vpc_network", "network_id"},
		// iam AccessBindingService soft-revoke (object-scoped). The unified List is
		// NOT here: it declares the scope-filtered lane and therefore carries no
		// relation and no scope of its own — see the dedicated case below.
		{"kaname.cloud.iam.v1.AccessBindingService/Revoke", "iam.access_bindings.revoke", "v_delete", "iam_access_binding", "access_binding_id"},
	}
	// Каталоги, читаемые ЛЮБЫМ аутентифицированным, стоят на ОТНОШЕНИИ, у которого
	// есть производитель (#893/#895).
	//
	// Здесь недолго требовалось обратное — полоса `<exempt>` (#892). Довод был
	// верен наполовину: отношение `viewer` на кластере не производил никто, и
	// проверка отвечала отказом каждому, поэтому ни машина, ни том не создавались.
	// Освобождение это чинило, но делало доступ невидимым: он не показывался
	// перечислением выдач и закрывался только выкаткой. Производитель заведён —
	// системная выдача с подстановочным субъектом, — и оба свойства держатся
	// одновременно: читает всякий аутентифицированный, и при этом доступ виден и
	// отзываем.
	for _, fqn := range []string{
		"kacho.cloud.compute.v1.MachineTypeService/Get",
		"kacho.cloud.compute.v1.MachineTypeService/List",
	} {
		t.Run(fqn+" (produced relation)", func(t *testing.T) {
			entry, ok := c.Lookup(fqn)
			require.True(t, ok, "fqn missing from embedded catalog: %s", fqn)
			assert.False(t, entry.IsExempt(),
				"%s — глобальный каталог обязан стоять на отношении, а не на освобождении", fqn)
			assert.Equal(t, "viewer", entry.RequiredRelation,
				"%s: отношение обязано быть тем, которое производит системная выдача", fqn)
			assert.Equal(t, "cluster", entry.ScopeExtractor.ObjectType,
				"%s: область — кластерный синглтон", fqn)
		})
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
			assert.False(t, entry.ScopeFiltered,
				"%s declares an edge check, so it must not also claim the scope-filtered lane", w.fqn)
		})
	}

	// The unified AccessBindings List declares the scope-filtered lane: kaname
	// reads the page and narrows it per row (`viewer ∪ v_list` on each
	// `iam_access_binding`), so there is no single object the edge could check.
	//
	// It previously declared `viewer` on the `cluster` singleton and called that a
	// gate. It was not one: the cluster bootstrap grants `viewer` to `user:*` so
	// every tenant can read the global reference catalogue, so the check passed for
	// every authenticated subject. The row now says what actually happens, and the
	// assertions below pin the difference rather than the wording.
	t.Run("kaname.cloud.iam.v1.AccessBindingService/List", func(t *testing.T) {
		entry, ok := c.Lookup("kaname.cloud.iam.v1.AccessBindingService/List")
		require.True(t, ok)
		assert.Equal(t, "iam.access_bindings.list", entry.Permission)
		assert.True(t, entry.ScopeFiltered)
		assert.False(t, entry.IsExempt(), "scope-filtered still requires an authenticated principal")
		assert.Empty(t, entry.RequiredRelation, "no relation is checked at the edge")
		assert.Empty(t, entry.ScopeExtractor.ObjectType, "no scope is resolved at the edge")
		assert.Equal(t, "1", entry.RequiredACRMin, "the step-up floor is unchanged")
	})
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
		{"GET", "/iam/v1/accessBindings", "kaname.cloud.iam.v1.AccessBindingService/List"},
		{"POST", "/iam/v1/accessBindings/acb-1:revoke", "kaname.cloud.iam.v1.AccessBindingService/Revoke"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			got, ok := r.Resolve(tc.method, tc.path)
			require.True(t, ok, "no route for %s %s", tc.method, tc.path)
			assert.Equal(t, tc.fqn, got)
		})
	}
}
