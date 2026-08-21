// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/compute/internal/check"
)

// Целевая (Design-B) карта enforcement-relation'ов для kacho-compute: per-RPC
// Check резолвит action на verb-bearing relation (`v_get`/`v_update`/`v_delete`/
// `v_list`), а не на tier. object-self RPC (Extract → id ресурса) флипается на v_*;
// parent-scoped Create (Extract → project) остаётся tier `editor`; top-level
// project-List остаётся `viewer` (visibility — через iam ListObjects union).

var verbGetRPCs = []string{
	"/kacho.cloud.compute.v1.InstanceService/Get",
	"/kacho.cloud.compute.v1.InstanceService/GetSerialPortOutput",
}

var verbUpdateRPCs = []string{
	"/kacho.cloud.compute.v1.InstanceService/Update",
	"/kacho.cloud.compute.v1.InstanceService/Start",
	"/kacho.cloud.compute.v1.InstanceService/Stop",
	"/kacho.cloud.compute.v1.InstanceService/Restart",
	"/kacho.cloud.compute.v1.InstanceService/AttachDisk",
	"/kacho.cloud.compute.v1.InstanceService/DetachDisk",
	"/kacho.cloud.compute.v1.InstanceService/AttachNetworkInterface",
	"/kacho.cloud.compute.v1.InstanceService/DetachNetworkInterface",
	"/kacho.cloud.compute.v1.InstanceService/SimulateMaintenanceEvent",
}

var verbDeleteRPCs = []string{
	"/kacho.cloud.compute.v1.InstanceService/Delete",
}

var verbListOnResourceRPCs = []string{
	"/kacho.cloud.compute.v1.InstanceService/ListOperations",
}

var createChildRPCs = []string{
	"/kacho.cloud.compute.v1.InstanceService/Create",
}

var projectListRPCs = []string{
	"/kacho.cloud.compute.v1.InstanceService/List",
}

func TestPermissionMap_VerbBearing_Get_VGet(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range verbGetRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "v_get", e.Relation, "%s: object-self read must enforce v_get (Design B)", rpc)
	}
}

func TestPermissionMap_VerbBearing_Update_VUpdate(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range verbUpdateRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "v_update", e.Relation, "%s: object-self mutation must enforce v_update (Design B)", rpc)
	}
}

func TestPermissionMap_VerbBearing_Delete_VDelete(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range verbDeleteRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "v_delete", e.Relation, "%s: object-self delete must enforce v_delete (Design B)", rpc)
	}
}

func TestPermissionMap_VerbBearing_ListOnResource_VList(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range verbListOnResourceRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "v_list", e.Relation, "%s: object-self list-on-resource must enforce v_list (Design B)", rpc)
	}
}

func TestPermissionMap_VerbBearing_CreateChild_StaysEditor(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range createChildRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "editor", e.Relation, "%s: create-child stays tier editor on parent project (F-7)", rpc)
	}
}

func TestPermissionMap_VerbBearing_ProjectList_StaysViewer(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range projectListRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "viewer", e.Relation, "%s: top-level project List stays viewer (visibility via iam ListObjects union)", rpc)
	}
}

// TestPermissionMap_VerbBearing_MachineTypeCatalogCarriesNoVerb — чтение каталога
// типов машин глаголом не гейтится вовсе: полоса `<exempt>`. Предмет
// пробы — что оно НЕ флипается в verb-несущую форму (F-8): ни `v_get`, ни
// `v_list` здесь появиться не вправе, потому что кластер глаголов не несёт.
// Прежняя редакция требовала `viewer` — отношение, не производимое ни одним
// посевом, из-за чего каталог отвечал отказом всем.
func TestPermissionMap_VerbBearing_MachineTypeCatalogCarriesNoVerb(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range []string{
		"/kacho.cloud.compute.v1.MachineTypeService/Get",
		"/kacho.cloud.compute.v1.MachineTypeService/List",
	} {
		e, ok := m[rpc]
		if !ok {
			continue // `<exempt>` в карту проверок не попадает
		}
		require.NotContainsf(t, e.Relation, "v_",
			"%s: чтение кластерного каталога не вправе нести глагол (F-8)", rpc)
		require.Emptyf(t, e.Relation,
			"%s: полоса `<exempt>` — отношения быть не может", rpc)
	}
}

// TestPermissionMap_VerbBearing_InternalUnchanged — internal catalog-admin RPC
// остаются system_admin@cluster (cluster — не verb-bearing, F-8).
func TestPermissionMap_VerbBearing_InternalUnchanged(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range []string{
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Create",
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Update",
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Delete",
	} {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "system_admin", e.Relation, "%s: internal catalog-admin relation unchanged (F-8)", rpc)
	}
}

func TestPermissionMap_VerbBearing_NoTierLeftOnObjectSelf(t *testing.T) {
	m := check.PermissionMap()
	objectSelf := append(append(append(append([]string{}, verbGetRPCs...), verbUpdateRPCs...), verbDeleteRPCs...), verbListOnResourceRPCs...)
	for _, rpc := range objectSelf {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.NotEqualf(t, "viewer", e.Relation, "%s: object-self must not stay on tier viewer", rpc)
		require.NotEqualf(t, "editor", e.Relation, "%s: object-self must not stay on tier editor", rpc)
	}
}
