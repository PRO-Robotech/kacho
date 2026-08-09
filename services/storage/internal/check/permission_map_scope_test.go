// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
	"github.com/PRO-Robotech/kacho/services/storage/internal/check"
)

// TestPermissionMap_ObjectAndProjectScope — regression against
// https://github.com/PRO-Robotech/kacho/issues/62.
//
// The storage in-service authz floor gated EVERY tenant RPC on the cluster
// singleton (`cluster:cluster_kacho_root`) via a static extractor, so a
// project-scoped `editor` principal — который HAS `project:<p>#viewer/editor` but no
// cluster-level grant — was denied 403 on List/Get/Create/Update/Delete of its OWN
// project's resources (the gateway already ALLOWED them at project/object scope).
// The bootstrap (cluster system_admin) suites masked it via the iam cluster-admin
// short-circuit.
//
// The fix mirrors the api-gateway permission-catalog (defense-in-depth: both
// listeners resolve the SAME scope object): List/Create → `project:<project_id>`;
// object-self Get/Update/Delete/ListOperations → `storage_<res>:<res_id>`. The
// tier relation (viewer/editor) is unchanged — only the scope object moves off the
// cluster singleton onto the request's project / target object.
//
// DiskType (global read-only catalog) and InternalDiskType (admin CRUD) legitimately
// stay on the cluster singleton and are asserted so a future edit does not
// accidentally object-scope a catalog RPC.
func TestPermissionMap_ObjectAndProjectScope(t *testing.T) {
	m := check.PermissionMap()

	type want struct {
		objType string
		objID   string
	}
	cases := map[string]struct {
		req  any
		want want
	}{
		// ---- VolumeService: List/Create → project; object-self → storage_volume ----
		"/kacho.cloud.storage.v1.VolumeService/List": {
			req: &storagev1.ListVolumesRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Create": {
			req: &storagev1.CreateVolumeRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Get": {
			req: &storagev1.GetVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Update": {
			req: &storagev1.UpdateVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Delete": {
			req: &storagev1.DeleteVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.VolumeService/ListOperations": {
			req: &storagev1.ListVolumeOperationsRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},

		// ---- SnapshotService ----
		"/kacho.cloud.storage.v1.SnapshotService/List": {
			req: &storagev1.ListSnapshotsRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Create": {
			req: &storagev1.CreateSnapshotRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Get": {
			req: &storagev1.GetSnapshotRequest{SnapshotId: "snp_1"}, want: want{"storage_snapshot", "snp_1"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Update": {
			req: &storagev1.UpdateSnapshotRequest{SnapshotId: "snp_1"}, want: want{"storage_snapshot", "snp_1"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Delete": {
			req: &storagev1.DeleteSnapshotRequest{SnapshotId: "snp_1"}, want: want{"storage_snapshot", "snp_1"},
		},

		// ---- ImageService ----
		"/kacho.cloud.storage.v1.ImageService/List": {
			req: &storagev1.ListImagesRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.ImageService/Create": {
			req: &storagev1.CreateImageRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.ImageService/Get": {
			req: &storagev1.GetImageRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},
		"/kacho.cloud.storage.v1.ImageService/Update": {
			req: &storagev1.UpdateImageRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},
		"/kacho.cloud.storage.v1.ImageService/Delete": {
			req: &storagev1.DeleteImageRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},
		"/kacho.cloud.storage.v1.ImageService/ListOperations": {
			req: &storagev1.ListImageOperationsRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},

		// ---- InternalVolumeService (:9091 attach-coordination) — object-scoped ----
		//
		// Записи ниже покрывают ТОЛЬКО том. Attach/Detach двухобъектные: запрос
		// называет ещё и машину, у которой другой владелец, и право на неё
		// спрашивает use-case (volume.requireInstanceControl) — у RPCEntry второго
		// слота нет и быть не должно, это ответ на один вопрос. Не читать эти строки
		// как «здесь весь гейт»: замок второго вопроса —
		// volume.TestEveryStorageRPCNamingAnInstanceAsksTheModelAboutIt.
		"/kacho.cloud.storage.v1.InternalVolumeService/Attach": {
			req: &storagev1.AttachVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.InternalVolumeService/Detach": {
			req: &storagev1.DetachVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.InternalVolumeService/GetInternal": {
			req: &storagev1.GetInternalVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},

		// ---- InternalImageService (:9091 infra-projection) — object-scoped ----
		"/kacho.cloud.storage.v1.InternalImageService/GetInternal": {
			req: &storagev1.GetInternalImageRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},
	}

	for fullMethod, tc := range cases {
		entry, ok := m[fullMethod]
		require.Truef(t, ok, "%s must be present in PermissionMap", fullMethod)
		require.NotNilf(t, entry.Extract, "%s: must carry an ObjectExtractor", fullMethod)
		objType, objID, err := entry.Extract(tc.req)
		require.NoErrorf(t, err, "%s: extractor error", fullMethod)
		require.Equalf(t, tc.want.objType, objType,
			"%s: object_type must be %q (mirror api-gateway catalog), not cluster", fullMethod, tc.want.objType)
		require.Equalf(t, tc.want.objID, objID, "%s: object_id extraction", fullMethod)
	}
}

// TestPermissionMap_CatalogRPCsStayClusterScoped — DiskType (global read-only
// catalog) + InternalDiskType (admin CRUD) legitimately resolve on the cluster
// singleton; lock it so the #62 object-scoping does not accidentally spread onto a
// catalog/cluster-wide RPC.
//
// InternalVolume.ListAttachments stood in this list and does NOT belong to it — see
// TestPermissionMap_NoTenantDataOnTheClusterSingleton below for why, and
// permission_map_list_attachments_test.go for what replaced it.
func TestPermissionMap_CatalogRPCsStayClusterScoped(t *testing.T) {
	m := check.PermissionMap()
	for _, fullMethod := range []string{
		"/kacho.cloud.storage.v1.DiskTypeService/Get",
		"/kacho.cloud.storage.v1.DiskTypeService/List",
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Create",
	} {
		entry, ok := m[fullMethod]
		require.Truef(t, ok, "%s must be present", fullMethod)
		require.NotNilf(t, entry.Extract, "%s: must carry an extractor", fullMethod)
		objType, objID, err := entry.Extract(struct{}{})
		require.NoErrorf(t, err, "%s: static cluster extractor never errors", fullMethod)
		require.Equalf(t, "cluster", objType, "%s: must stay cluster-scoped", fullMethod)
		require.Equalf(t, "cluster_kacho_root", objID, "%s: cluster singleton", fullMethod)
	}
}

// TestPermissionMap_NoTenantDataOnTheClusterSingleton — обратная сторона предыдущего
// lock'а, и он важнее: на синглтон `cluster` можно вешать ТОЛЬКО глобальный каталог.
//
// `cluster.viewer` включает userset `user:*`, и bootstrap кластера намеренно пишет
// `cluster:<root>#viewer@user:*`, чтобы справочник (регионы, зоны, типы дисков) читал
// любой аутентифицированный субъект. Поэтому cluster-`viewer` на RPC, отдающем данные
// КОНКРЕТНЫХ тенантов, — не проверка, а её видимость: пропускает всех. Ровно так
// ListAttachments отдавал привязки том↔инстанс любых названных инстансов, из чужих
// проектов и аккаунтов.
//
// Тест перечисляет cluster-scoped записи из самой карты, поэтому НОВЫЙ RPC, повешенный
// на синглтон, обязан быть здесь оправдан явно — тихо он не появится.
func TestPermissionMap_NoTenantDataOnTheClusterSingleton(t *testing.T) {
	// Записи, для которых cluster-синглтон — правильный предмет вопроса: глобальный
	// admin-curated каталог, одинаковый для всех тенантов.
	clusterCatalog := map[string]bool{
		"/kacho.cloud.storage.v1.DiskTypeService/Get":            true,
		"/kacho.cloud.storage.v1.DiskTypeService/List":           true,
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Create": true,
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Update": true,
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Delete": true,
	}
	seen := 0
	for fullMethod, entry := range check.PermissionMap() {
		// Тип объекта восстанавливается вызовом extractor'а на НУЛЕВОМ запросе метода
		// (тот же приём, которым сверяется gateway-каталог) — сам
		// extractor хранится замыканием и прочитан быть не может.
		objType, ok := catalogderive.ScopeObjectType(fullMethod, entry)
		if !ok || objType != "cluster" {
			continue // object-scoped запись: её extractor читает поле запроса
		}
		seen++
		require.Truef(t, clusterCatalog[fullMethod],
			"%s anchors its check on the cluster singleton, but only the global disk-type "+
				"catalogue may do that: `cluster:<root>#viewer@user:*` is written by the cluster "+
				"bootstrap, so a cluster-scoped viewer check admits every authenticated subject. "+
				"An RPC returning tenant rows must be object-scoped or ScopeFiltered instead",
			fullMethod)
	}
	// Анти-вакуум: восстановление типа идёт через proto-реестр и на неудаче молча
	// отдаёт «нет ответа». Если бы оно перестало работать, обход не проверил бы НИЧЕГО
	// и остался зелёным. Кластерные записи в карте есть — значит их обязано быть видно.
	require.Equalf(t, len(clusterCatalog), seen,
		"обход увидел %d cluster-scoped записей вместо %d — проверка ничего не утверждает",
		seen, len(clusterCatalog))
}
