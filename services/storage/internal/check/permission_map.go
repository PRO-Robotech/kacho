// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// FGA-скоупинг для kacho-storage — зеркалит permission-catalog api-gateway
// (defense-in-depth: ОБА листенера резолвят один и тот же scope-object).
//
// Семантика per-RPC (issue #62 fix — раньше ВСЁ гейтилось на cluster-синглтоне,
// из-за чего project-scoped `editor` (есть `project:<p>#viewer/editor`, но нет
// cluster-грантов) получал 403 на List/Get/Create/Update/Delete СВОЕГО проекта;
// bootstrap-суиты это маскировали через iam cluster-admin short-circuit):
//   - List/Create               → parent scope `project:<project_id>`, tier viewer/editor;
//   - Get/Update/Delete/ListOps  → object-self `storage_<res>:<res_id>`, tier viewer/editor;
//   - DiskType Get/List          → viewer на cluster singleton (глобальный read-only каталог);
//   - InternalVolume Attach/Detach/GetInternal → object-self `storage_volume:<volume_id>`;
//   - InternalVolume ListAttachments → viewer на cluster (cluster-wide internal listing);
//   - InternalDiskType CRUD      → system_admin на cluster singleton (admin каталог).
//
// Смысл гейта здесь — AuthN+AuthZ на ОБОИХ листенерах (internal :9091 НЕ освобождён,
// security.md): defense-in-depth поверх mTLS. Авторитетный object-scoped scope_extractor
// дублируется в permission-catalog api-gateway; тексты relation/object совпадают,
// поэтому оба Check выносят одно и то же решение. cluster_kacho_root — ClusterSingletonID
// из kacho-iam, один на деплой. owner-tuple materialization (fgaproxy RegisterResource) —
// см. internal/fgaregister; project-editor per-object доступ на storage-объекты
// материализуется iam-реконсайлером (см. iam authzmap/feed_registry storage-типы).
const (
	objectTypeProject  = "project"
	objectTypeVolume   = "storage_volume"
	objectTypeSnapshot = "storage_snapshot"
	objectTypeImage    = "storage_image"

	objectTypeCluster      = "cluster"
	clusterSingletonObject = "cluster_kacho_root"

	relationViewer      = "viewer"
	relationEditor      = "editor"
	relationSystemAdmin = "system_admin"
)

// staticClusterCatalog — extractor, всегда возвращающий (cluster, cluster_kacho_root).
// Используется для глобального read-only каталога (DiskType), admin-CRUD
// (InternalDiskType) и cluster-wide internal listing (ListAttachments).
func staticClusterCatalog() authz.ObjectExtractor {
	return func(any) (string, string, error) {
		return objectTypeCluster, clusterSingletonObject, nil
	}
}

// viewerCluster / adminCluster — cluster-singleton tier entries (каталог / admin).
// Object/project-scoped entries строятся через scoped() ниже.
func viewerCluster(perm string) authz.RPCEntry {
	return authz.RPCEntry{Relation: relationViewer, Extract: staticClusterCatalog(), Permission: perm}
}

func adminCluster(perm string) authz.RPCEntry {
	return authz.RPCEntry{Relation: relationSystemAdmin, Extract: staticClusterCatalog(), Permission: perm}
}

// scoped — object/project-scoped entry: relation + (objectType, id-extractor).
func scoped(relation, objectType, perm string, extractID func(req any) (string, error)) authz.RPCEntry {
	return authz.RPCEntry{
		Relation:   relation,
		Extract:    authz.StaticExtractor(objectType, extractID),
		Permission: perm,
	}
}

// PermissionMap сопоставляет каждый storage-RPC → требуемое relation + extractor.
func PermissionMap() authz.RPCMap {
	return authz.RPCMap{
		// ---- VolumeService (public :9090) ----
		"/kacho.cloud.storage.v1.VolumeService/List": scoped(relationViewer, objectTypeProject, "storage.volumes.list",
			func(req any) (string, error) { return req.(*storagev1.ListVolumesRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.VolumeService/Create": scoped(relationEditor, objectTypeProject, "storage.volumes.create",
			func(req any) (string, error) { return req.(*storagev1.CreateVolumeRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.VolumeService/Get": scoped(relationViewer, objectTypeVolume, "storage.volumes.get",
			func(req any) (string, error) { return req.(*storagev1.GetVolumeRequest).GetVolumeId(), nil }),
		"/kacho.cloud.storage.v1.VolumeService/ListOperations": scoped(relationViewer, objectTypeVolume, "storage.volumes.listOperations",
			func(req any) (string, error) { return req.(*storagev1.ListVolumeOperationsRequest).GetVolumeId(), nil }),
		"/kacho.cloud.storage.v1.VolumeService/Update": scoped(relationEditor, objectTypeVolume, "storage.volumes.update",
			func(req any) (string, error) { return req.(*storagev1.UpdateVolumeRequest).GetVolumeId(), nil }),
		"/kacho.cloud.storage.v1.VolumeService/Delete": scoped(relationEditor, objectTypeVolume, "storage.volumes.delete",
			func(req any) (string, error) { return req.(*storagev1.DeleteVolumeRequest).GetVolumeId(), nil }),

		// ---- SnapshotService (public :9090) ----
		"/kacho.cloud.storage.v1.SnapshotService/List": scoped(relationViewer, objectTypeProject, "storage.snapshots.list",
			func(req any) (string, error) { return req.(*storagev1.ListSnapshotsRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.SnapshotService/Create": scoped(relationEditor, objectTypeProject, "storage.snapshots.create",
			func(req any) (string, error) { return req.(*storagev1.CreateSnapshotRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.SnapshotService/Get": scoped(relationViewer, objectTypeSnapshot, "storage.snapshots.get",
			func(req any) (string, error) { return req.(*storagev1.GetSnapshotRequest).GetSnapshotId(), nil }),
		"/kacho.cloud.storage.v1.SnapshotService/Update": scoped(relationEditor, objectTypeSnapshot, "storage.snapshots.update",
			func(req any) (string, error) { return req.(*storagev1.UpdateSnapshotRequest).GetSnapshotId(), nil }),
		"/kacho.cloud.storage.v1.SnapshotService/Delete": scoped(relationEditor, objectTypeSnapshot, "storage.snapshots.delete",
			func(req any) (string, error) { return req.(*storagev1.DeleteSnapshotRequest).GetSnapshotId(), nil }),

		// ---- DiskTypeService (public :9090, read-only global catalog) ----
		"/kacho.cloud.storage.v1.DiskTypeService/Get":  viewerCluster("storage.diskTypes.get"),
		"/kacho.cloud.storage.v1.DiskTypeService/List": viewerCluster("storage.diskTypes.list"),

		// ---- ImageService (public :9090) ----
		"/kacho.cloud.storage.v1.ImageService/List": scoped(relationViewer, objectTypeProject, "storage.images.list",
			func(req any) (string, error) { return req.(*storagev1.ListImagesRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.ImageService/Create": scoped(relationEditor, objectTypeProject, "storage.images.create",
			func(req any) (string, error) { return req.(*storagev1.CreateImageRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.ImageService/Get": scoped(relationViewer, objectTypeImage, "storage.images.get",
			func(req any) (string, error) { return req.(*storagev1.GetImageRequest).GetImageId(), nil }),
		"/kacho.cloud.storage.v1.ImageService/ListOperations": scoped(relationViewer, objectTypeImage, "storage.images.listOperations",
			func(req any) (string, error) { return req.(*storagev1.ListImageOperationsRequest).GetImageId(), nil }),
		"/kacho.cloud.storage.v1.ImageService/Update": scoped(relationEditor, objectTypeImage, "storage.images.update",
			func(req any) (string, error) { return req.(*storagev1.UpdateImageRequest).GetImageId(), nil }),
		"/kacho.cloud.storage.v1.ImageService/Delete": scoped(relationEditor, objectTypeImage, "storage.images.delete",
			func(req any) (string, error) { return req.(*storagev1.DeleteImageRequest).GetImageId(), nil }),

		// ---- InternalVolumeService (:9091 attach-координация) ----
		"/kacho.cloud.storage.v1.InternalVolumeService/Attach": scoped(relationEditor, objectTypeVolume, "storage.volumes.attach",
			func(req any) (string, error) { return req.(*storagev1.AttachVolumeRequest).GetVolumeId(), nil }),
		"/kacho.cloud.storage.v1.InternalVolumeService/Detach": scoped(relationEditor, objectTypeVolume, "storage.volumes.detach",
			func(req any) (string, error) { return req.(*storagev1.DetachVolumeRequest).GetVolumeId(), nil }),
		"/kacho.cloud.storage.v1.InternalVolumeService/GetInternal": scoped(relationViewer, objectTypeVolume, "storage.volumes.getInternal",
			func(req any) (string, error) { return req.(*storagev1.GetInternalVolumeRequest).GetVolumeId(), nil }),
		"/kacho.cloud.storage.v1.InternalVolumeService/ListAttachments": viewerCluster("storage.volumes.listAttachments"),

		// ---- InternalDiskTypeService (:9091 admin CRUD, system_admin) ----
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Create": adminCluster("storage.diskTypes.create"),
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Update": adminCluster("storage.diskTypes.update"),
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Delete": adminCluster("storage.diskTypes.delete"),

		// ---- LRO Operation-envelope (ReBAC-exempt, owner-scoped в handler) ----
		// Async-мутации возвращают Operation, клиент поллит OperationService.Get.
		// Public:true снимает per-RPC ReBAC-Check (нет object type storage_operation),
		// но anti-anon (principal-цепочка) и ownership-gate в handler (GetOwned/
		// CancelOwned, no-leak NotFound) сохраняются. Зеркалит geo/vpc.
		"/kacho.cloud.operation.OperationService/Get":    {Public: true},
		"/kacho.cloud.operation.OperationService/Cancel": {Public: true},
	}
}
