// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/check"
)

// TestPermissionMap_ImageService_Mapped — регрессия против backend-map gap класса
// (ImageService был пропущен в PermissionMap, хотя proto его несёт и gateway-catalog
// имел записи → corelib authz fail-closed "rpc not mapped" на весь image-suite,
// 108 newman-падений). Тот же класс: compute InternalMachineType / vpc cidr-blocks /
// registry Repository-overlay. Каждый served RPC ОБЯЗАН быть в map (fail-closed).
func TestPermissionMap_ImageService_Mapped(t *testing.T) {
	m := check.PermissionMap()
	// proto image_service.proto: Get/List/ListOperations=viewer, Create/Update/Delete=editor.
	want := map[string]string{
		"/kacho.cloud.storage.v1.ImageService/Get":            "viewer",
		"/kacho.cloud.storage.v1.ImageService/List":           "viewer",
		"/kacho.cloud.storage.v1.ImageService/ListOperations": "viewer",
		"/kacho.cloud.storage.v1.ImageService/Create":         "editor",
		"/kacho.cloud.storage.v1.ImageService/Update":         "editor",
		"/kacho.cloud.storage.v1.ImageService/Delete":         "editor",
	}
	for fullMethod, rel := range want {
		entry, ok := m[fullMethod]
		require.Truef(t, ok, "%s must be present in PermissionMap (unmapped RPC fail-closes 'rpc not mapped')", fullMethod)
		require.Equalf(t, rel, entry.Relation, "%s: required_relation must be %s (proto annotation)", fullMethod, rel)
		require.NotNilf(t, entry.Extract, "%s: must carry an ObjectExtractor", fullMethod)
	}
}

// TestPermissionMap_CoreServices_Mapped — sanity: Volume/Snapshot/DiskType core RPCs
// present (they were already mapped; this locks the whole tenant-facing surface so a
// future service-add doesn't silently drop one).
func TestPermissionMap_CoreServices_Mapped(t *testing.T) {
	m := check.PermissionMap()
	for _, fullMethod := range []string{
		"/kacho.cloud.storage.v1.VolumeService/Create",
		"/kacho.cloud.storage.v1.VolumeService/Get",
		"/kacho.cloud.storage.v1.SnapshotService/Create",
		"/kacho.cloud.storage.v1.DiskTypeService/List",
	} {
		_, ok := m[fullMethod]
		require.Truef(t, ok, "%s must be present in PermissionMap", fullMethod)
	}
}
