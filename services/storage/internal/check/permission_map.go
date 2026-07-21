// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// FGA-скоупинг для kacho-storage. Публичные Get/List (Volume/Snapshot/DiskType) —
// viewer-floor; tenant-мутации (Volume/Snapshot Create/Update/Delete) и Internal*
// attach-координация — editor-tier; admin DiskType CRUD (InternalDiskTypeService) —
// system_admin. Все — на cluster-синглтоне (cluster_kacho_root), зеркаля geo:
// object-scoped scope_extractor (project/storage_volume) + owner-tuple emission
// (fgaproxy RegisterResource) — следующий инкремент (SEC-A); авторитетный
// object-scoped scope_extractor живёт в permission-catalog api-gateway.
//
// Смысл гейта здесь — AuthN+AuthZ на ОБОИХ листенерах (internal :9091 НЕ освобождён,
// security.md): defense-in-depth tier-check поверх mTLS. Cluster_kacho_root —
// ClusterSingletonID из kacho-iam, один на деплой.
const (
	objectTypeCluster      = "cluster"
	clusterSingletonObject = "cluster_kacho_root"

	relationViewer      = "viewer"
	relationEditor      = "editor"
	relationSystemAdmin = "system_admin"
)

// staticClusterCatalog — extractor, всегда возвращающий (cluster, cluster_kacho_root).
func staticClusterCatalog() authz.ObjectExtractor {
	return func(any) (string, string, error) {
		return objectTypeCluster, clusterSingletonObject, nil
	}
}

func viewer(perm string) authz.RPCEntry {
	return authz.RPCEntry{Relation: relationViewer, Extract: staticClusterCatalog(), Permission: perm}
}

func editor(perm string) authz.RPCEntry {
	return authz.RPCEntry{Relation: relationEditor, Extract: staticClusterCatalog(), Permission: perm}
}

func admin(perm string) authz.RPCEntry {
	return authz.RPCEntry{Relation: relationSystemAdmin, Extract: staticClusterCatalog(), Permission: perm}
}

// PermissionMap сопоставляет каждый storage-RPC → требуемое relation + extractor.
func PermissionMap() authz.RPCMap {
	return authz.RPCMap{
		// ---- VolumeService (public :9090) ----
		"/kacho.cloud.storage.v1.VolumeService/Get":            viewer("storage.volumes.get"),
		"/kacho.cloud.storage.v1.VolumeService/List":           viewer("storage.volumes.list"),
		"/kacho.cloud.storage.v1.VolumeService/ListOperations": viewer("storage.volumes.listOperations"),
		"/kacho.cloud.storage.v1.VolumeService/Create":         editor("storage.volumes.create"),
		"/kacho.cloud.storage.v1.VolumeService/Update":         editor("storage.volumes.update"),
		"/kacho.cloud.storage.v1.VolumeService/Delete":         editor("storage.volumes.delete"),

		// ---- SnapshotService (public :9090) ----
		"/kacho.cloud.storage.v1.SnapshotService/Get":    viewer("storage.snapshots.get"),
		"/kacho.cloud.storage.v1.SnapshotService/List":   viewer("storage.snapshots.list"),
		"/kacho.cloud.storage.v1.SnapshotService/Create": editor("storage.snapshots.create"),
		"/kacho.cloud.storage.v1.SnapshotService/Update": editor("storage.snapshots.update"),
		"/kacho.cloud.storage.v1.SnapshotService/Delete": editor("storage.snapshots.delete"),

		// ---- DiskTypeService (public :9090, read-only) ----
		"/kacho.cloud.storage.v1.DiskTypeService/Get":  viewer("storage.diskTypes.get"),
		"/kacho.cloud.storage.v1.DiskTypeService/List": viewer("storage.diskTypes.list"),

		// ---- ImageService (public :9090) ----
		// Пропущен в backend-map (был Volume/Snapshot/DiskType, не Image) → corelib
		// authz fail-closed "rpc not mapped" на весь image-suite (108 newman-падений).
		// Тот же класс, что compute InternalMachineType / vpc cidr-blocks / registry
		// Repository-overlay. proto required_relation: Get/List/ListOperations=viewer,
		// Create/Update/Delete=editor. gateway-catalog Image уже имел (6 записей).
		"/kacho.cloud.storage.v1.ImageService/Get":            viewer("storage.images.get"),
		"/kacho.cloud.storage.v1.ImageService/List":           viewer("storage.images.list"),
		"/kacho.cloud.storage.v1.ImageService/ListOperations": viewer("storage.images.listOperations"),
		"/kacho.cloud.storage.v1.ImageService/Create":         editor("storage.images.create"),
		"/kacho.cloud.storage.v1.ImageService/Update":         editor("storage.images.update"),
		"/kacho.cloud.storage.v1.ImageService/Delete":         editor("storage.images.delete"),

		// ---- InternalVolumeService (:9091 attach-координация, writer-tier) ----
		"/kacho.cloud.storage.v1.InternalVolumeService/Attach":          editor("storage.volumes.attach"),
		"/kacho.cloud.storage.v1.InternalVolumeService/Detach":          editor("storage.volumes.detach"),
		"/kacho.cloud.storage.v1.InternalVolumeService/ListAttachments": viewer("storage.volumes.listAttachments"),
		"/kacho.cloud.storage.v1.InternalVolumeService/GetInternal":     viewer("storage.volumes.getInternal"),

		// ---- InternalDiskTypeService (:9091 admin CRUD, system_admin) ----
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Create": admin("storage.diskTypes.create"),
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Update": admin("storage.diskTypes.update"),
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Delete": admin("storage.diskTypes.delete"),

		// ---- LRO Operation-envelope (ReBAC-exempt, owner-scoped в handler) ----
		// Async-мутации возвращают Operation, клиент поллит OperationService.Get.
		// Public:true снимает per-RPC ReBAC-Check (нет object type storage_operation),
		// но anti-anon (principal-цепочка) и ownership-gate в handler (GetOwned/
		// CancelOwned, no-leak NotFound) сохраняются. Зеркалит geo/vpc.
		"/kacho.cloud.operation.OperationService/Get":    {Public: true},
		"/kacho.cloud.operation.OperationService/Cancel": {Public: true},
	}
}
